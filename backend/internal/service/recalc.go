package service

// Recalculation (ADR-007 §3): create a NEW run derived from the stored snapshot of the
// parent, applying overrides as a per-field layer on the parent's RunConstraints. The
// parent run, its snapshot, variants and audit entries stay immutable. Overrides that
// are invalid or contradictory are rejected individually; the remaining set is re-solved
// and recomputed, and the effect is reported with measured facts.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// ErrRunNotFound is wrapped when a recalculate/inputs/variant target does not exist.
var ErrRunNotFound = errors.New("run not found")

// ErrRecalcInfeasible marks a recalculation whose resulting constraint set the solver
// proved infeasible (G4 is never relaxed; contracts RecalculateResponse reason
// SOLVER_INFEASIBLE). The HTTP layer maps it to 422 with the blocking constraint.
var ErrRecalcInfeasible = errors.New("recalculation infeasible")

// ErrInvalidOverride marks malformed override lists (handler maps to 400/422).
var ErrInvalidOverride = errors.New("invalid constraint override")

// ConstraintOverride mirrors contracts ConstraintOverride. `value` is a number for every
// type (the frontend encodes forbid_external as 1/0).
type ConstraintOverride struct {
	Type          string   `json:"type"`
	EmployeeID    string   `json:"employee_id"`
	RequirementID string   `json:"requirement_id"`
	Value         *float64 `json:"value"`
	Week          string   `json:"week"`
	EmployeeIDs   []string `json:"employee_ids"`
	Reason        string   `json:"reason"`
}

// RecalculateRequest mirrors contracts RecalculateRequest.
type RecalculateRequest struct {
	BaseVariantID string               `json:"base_variant_id"`
	Preset        string               `json:"preset"`
	Weights       map[string]float64   `json:"weights"`
	K             *int                 `json:"k"`
	Seed          *int64               `json:"seed"`
	TimeLimitMS   *int                 `json:"time_limit_ms"`
	Overrides     []ConstraintOverride `json:"overrides"`
}

// RejectedOverride mirrors contracts RejectedOverride.
type RejectedOverride struct {
	Index       int      `json:"index"`
	ReasonCodes []string `json:"reason_codes"`
	Detail      string   `json:"detail,omitempty"`
}

// validConstraintTypes mirrors contracts ConstraintType.
var validConstraintTypes = map[string]bool{
	"force_include": true, "force_exclude": true, "max_team_size": true, "min_team_size": true,
	"budget_cap": true, "deadline": true, "forbid_external": true, "freeze_members": true,
	"min_bus_factor": true, "max_utilization": true,
}

// Recalculate creates a new run derived from parent runID (ADR-007 §3). The response is
// the contract RecalculateResponse shape with measured effect facts.
func (s *WorkflowService) Recalculate(ctx context.Context, runID string, req RecalculateRequest) (map[string]any, error) {
	if len(req.Overrides) == 0 {
		return nil, fmt.Errorf("%w: at least one override is required", ErrInvalidRunInput)
	}
	if len(req.Overrides) > 30 {
		return nil, fmt.Errorf("%w: at most 30 overrides per recalculation", ErrInvalidRunInput)
	}
	s.mu.RLock()
	repo := s.stores
	inMemoryParent, inMemoryOK := s.snapshots[runID]
	s.mu.RUnlock()
	var parent *runinput.Snapshot
	if repo != nil && repo.Snapshots != nil {
		raw, err := repo.Snapshots.GetData(ctx, runID)
		if err != nil {
			return nil, fmt.Errorf("%w: parent run %s: %v", ErrRunNotFound, runID, err)
		}
		var data runinput.Data
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("%w: parent snapshot %s: %v", ErrInvalidRunInput, runID, err)
		}
		parent, err = runinput.Build(data)
		if err != nil {
			return nil, fmt.Errorf("%w: parent snapshot %s: %v", ErrInvalidRunInput, runID, err)
		}
	} else if inMemoryOK {
		parent = inMemoryParent
	} else {
		return nil, fmt.Errorf("%w: parent run %s", ErrRunNotFound, runID)
	}

	// 1. Overrides are a per-field layer on the parent's constraints.
	constraints, rejected := applyOverrides(parent, req.Overrides)
	applied := len(req.Overrides) - len(rejected)

	// 2. Derive a new immutable snapshot from the parent (parent stays immutable).
	data := parent.Data()
	s.mu.Lock()
	s.next++
	seq := s.next
	s.mu.Unlock()
	newRunID := s.newRunID(seq)
	data.RunID = newRunID
	data.Constraints = constraints
	data.CreatedAt = time.Now().UTC()
	if req.Preset != "" {
		data.Preset = req.Preset
	}
	if len(req.Weights) > 0 {
		data.Weights = req.Weights
	}
	if req.K != nil {
		data.K = *req.K
	}
	if req.Seed != nil {
		data.Seed = *req.Seed
	}
	if req.TimeLimitMS != nil {
		data.TimeLimitMS = *req.TimeLimitMS
	}

	newPayload, err := s.computeAndStore(ctx, parent.BriefID(), newRunID, runID, data, data.CreatedAt)
	if err != nil {
		return nil, err
	}
	if repo != nil && repo.Overrides != nil {
		if err := repo.Overrides.CreateBatch(ctx, newRunID, domainOverridesFrom(newRunID, req.Overrides)); err != nil {
			return nil, fmt.Errorf("persist constraint overrides: %w", err)
		}
	}
	if _, err := s.auditEvent(ctx, "recalculation_started", "run", newRunID,
		fmt.Sprintf("Recalculation of %s: %d applied, %d rejected overrides", runID, applied, len(rejected)),
		map[string]any{"parent_run_id": runID, "applied": applied, "rejected": len(rejected)}); err != nil {
		return nil, err
	}

	// 3. SOLVER_INFEASIBLE: no relaxed solution, report the blocking constraint set.
	if newStatus(newPayload) == "failed" && solverStatus(newPayload) == "INFEASIBLE" {
		return nil, &infeasibleError{runID: newRunID, constraints: constraints}
	}

	// 4. Measure the effect against the base variant (diff baseline only, not a freeze).
	baseRoster := s.VariantRosters(runID)[req.BaseVariantID]
	baseP50 := 0.0
	if req.BaseVariantID != "" {
		if v, ok := s.GetVariant(runID, req.BaseVariantID); ok {
			if d, ok := v["duration"].(map[string]any); ok {
				baseP50, _ = d["p50_weeks"].(float64)
			}
		}
	}
	newPrimaryID, newRoster, newP50 := s.primaryVariant(newRunID)
	changed := changedMembers(baseRoster, newRoster)
	jaccard := round3(metrics.RosterJaccardDistance(baseRoster, newRoster))

	appliedCode := "RECALC_NO_EFFECT"
	if changed > 0 || math.Abs(newP50-baseP50) > 1e-6 {
		appliedCode = "RECALC_APPLIED"
	}
	reasonCodes := []string{appliedCode}
	if len(rejected) > 0 {
		reasonCodes = append(reasonCodes, "OVERRIDE_REJECTED")
	}

	response := map[string]any{
		"run_id":             newRunID,
		"parent_run_id":      runID,
		"status":             newStatus(newPayload),
		"applied_overrides":  applied,
		"rejected_overrides": rejected,
		"reason_codes":       reasonCodes,
		"facts": map[string]any{
			"changed_members":    changed,
			"roster_jaccard":     jaccard,
			"p50_weeks_before":   round3(baseP50),
			"p50_weeks_after":    round3(newP50),
			"base_variant_id":    req.BaseVariantID,
			"new_variant_id":     newPrimaryID,
			"overrides_applied":  applied,
			"overrides_rejected": len(rejected),
			"seed":               data.Seed,
		},
	}
	// Return the response in its exact JSON shape so service and HTTP tests observe the
	// same float64/[]any types the wire carries and determinism comparisons are stable.
	return jsonMap(response), nil
}

// jsonMap round-trips a value through JSON so int/string slices become the float64/[]any
// shapes a decoded HTTP body has. It never changes the logical values.
func jsonMap(v any) map[string]any {
	encoded, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(encoded, &out)
	return out
}

type infeasibleError struct {
	runID       string
	constraints runinput.Constraints
}

func (e *infeasibleError) Error() string {
	return fmt.Sprintf("%v: %s, blocking constraints = %s", ErrRecalcInfeasible, e.runID, blockingConstraints(e.constraints))
}

func (e *infeasibleError) Unwrap() error { return ErrRecalcInfeasible }

// blockingConstraints names the constraints that could not be satisfied, for the Problem
// detail of SOLVER_INFEASIBLE (contracts RecalculateResponse/Problem).
func blockingConstraints(c runinput.Constraints) string {
	pinned := uniqueNonEmpty(append(append([]string{}, c.ForceInclude...), c.FreezeMembers...), c.ForceExclude)
	parts := []string{}
	if len(c.ForceInclude) > 0 {
		parts = append(parts, fmt.Sprintf("force_include=%v", c.ForceInclude))
	}
	if c.MinTeamSize > 0 {
		parts = append(parts, fmt.Sprintf("min_team_size=%d", c.MinTeamSize))
	}
	if c.MaxTeamSize > 0 {
		parts = append(parts, fmt.Sprintf("max_team_size=%d", c.MaxTeamSize))
	}
	if c.DeadlineWeek != "" {
		parts = append(parts, "deadline="+c.DeadlineWeek)
	}
	if c.BudgetCapUnits > 0 {
		parts = append(parts, fmt.Sprintf("budget_cap=%.2f", c.BudgetCapUnits))
	}
	if c.ForbidExternal {
		parts = append(parts, "forbid_external")
	}
	if len(pinned) > 0 && c.MaxTeamSize > 0 && len(pinned) > c.MaxTeamSize {
		parts = append(parts, fmt.Sprintf("pinned=%d > max_team_size=%d", len(pinned), c.MaxTeamSize))
	}
	if len(parts) == 0 {
		return "no constraints set"
	}
	return fmt.Sprintf("{%s}", joinParts(parts))
}

// applyOverrides mirrors frontend/src/mocks/runs.ts applyOverrides: per-field layer on
// the parent constraints. Invalid/contradictory overrides are rejected individually.
func applyOverrides(parent *runinput.Snapshot, overrides []ConstraintOverride) (runinput.Constraints, []RejectedOverride) {
	cons := parent.Constraints()
	cons.ForceInclude = append([]string{}, parent.Constraints().ForceInclude...)
	cons.ForceExclude = append([]string{}, parent.Constraints().ForceExclude...)
	cons.FreezeMembers = append([]string{}, parent.Constraints().FreezeMembers...)

	pool := make(map[string]bool, len(parent.Candidates()))
	for _, c := range parent.Candidates() {
		pool[c.EmployeeID] = true
	}
	rejected := []RejectedOverride{}
	add := func(index int, codes []string, detail string) {
		rejected = append(rejected, RejectedOverride{Index: index, ReasonCodes: codes, Detail: detail})
	}

	for i, o := range overrides {
		if !validConstraintTypes[o.Type] {
			add(i, []string{"VALIDATION_FAILED"}, "unknown override type "+o.Type)
			continue
		}
		switch o.Type {
		case "force_include":
			emp := o.EmployeeID
			if emp == "" || !domain.EmployeeIDRe.MatchString(emp) {
				add(i, []string{"VALIDATION_FAILED"}, "invalid employee_id")
			} else if !pool[emp] {
				add(i, []string{"OVERRIDE_REJECTED"}, emp+" not in the run input snapshot")
			} else if candidateUnavailable(parent, emp) {
				add(i, []string{"OVERRIDE_VIOLATES_G1"}, emp+" has no free capacity in the horizon")
			} else if containsStr(cons.ForceExclude, emp) {
				add(i, []string{"OVERRIDE_VIOLATES_G4"}, emp+" is already force-excluded")
			} else {
				cons.ForceInclude = appendUnique(cons.ForceInclude, emp)
			}
		case "force_exclude":
			emp := o.EmployeeID
			if emp == "" || !domain.EmployeeIDRe.MatchString(emp) {
				add(i, []string{"VALIDATION_FAILED"}, "invalid employee_id")
			} else if !pool[emp] {
				add(i, []string{"OVERRIDE_REJECTED"}, emp+" not in the run input snapshot")
			} else if containsStr(cons.ForceInclude, emp) {
				add(i, []string{"OVERRIDE_VIOLATES_G4"}, emp+" is already force-included")
			} else {
				cons.ForceExclude = appendUnique(cons.ForceExclude, emp)
				cons.FreezeMembers = without(cons.FreezeMembers, emp)
			}
		case "freeze_members":
			ids := o.EmployeeIDs
			if len(ids) == 0 {
				add(i, []string{"VALIDATION_FAILED"}, "no employee ids for freeze_members")
				continue
			}
			allValid := true
			for _, id := range ids {
				if !domain.EmployeeIDRe.MatchString(id) {
					allValid = false
					break
				}
			}
			if !allValid {
				add(i, []string{"VALIDATION_FAILED"}, "invalid employee id in freeze_members")
			} else {
				unknown := false
				for _, id := range ids {
					if !pool[id] {
						unknown = true
					}
				}
				if unknown {
					add(i, []string{"OVERRIDE_REJECTED"}, "freeze_members contains employees outside the snapshot")
				} else if overlaps(ids, cons.ForceExclude) {
					add(i, []string{"OVERRIDE_VIOLATES_G4"}, "a frozen member is simultaneously force-excluded")
				} else {
					cons.FreezeMembers = mergeUnique(cons.FreezeMembers, ids)
				}
			}
		case "max_team_size":
			v := intValue(o.Value)
			if o.Value == nil || !isInteger(*o.Value) || v < 1 || v > 50 {
				add(i, []string{"VALIDATION_FAILED"}, "max_team_size must be an integer in 1..50")
			} else if pinned := pinnedCount(cons); v < pinned {
				add(i, []string{"OVERRIDE_VIOLATES_G4"}, fmt.Sprintf("pinned %d exceeds limit %d", pinned, v))
			} else if cons.MinTeamSize > 0 && v < cons.MinTeamSize {
				add(i, []string{"OVERRIDE_VIOLATES_G4"}, "max_team_size below min_team_size")
			} else {
				cons.MaxTeamSize = v
			}
		case "min_team_size":
			v := intValue(o.Value)
			if o.Value == nil || !isInteger(*o.Value) || v < 1 || v > 50 {
				add(i, []string{"VALIDATION_FAILED"}, "min_team_size must be an integer in 1..50")
			} else if cons.MaxTeamSize > 0 && v > cons.MaxTeamSize {
				add(i, []string{"OVERRIDE_VIOLATES_G4"}, "min_team_size above max_team_size")
			} else {
				cons.MinTeamSize = v
			}
		case "budget_cap":
			if o.Value == nil || math.IsNaN(*o.Value) || *o.Value < 0 {
				add(i, []string{"VALIDATION_FAILED"}, "budget_cap must be a non-negative number")
			} else {
				cons.BudgetCapUnits = *o.Value
			}
		case "deadline":
			week := o.Week
			if week == "" || !domain.ValidateWeekKey(week) {
				add(i, []string{"VALIDATION_FAILED"}, "deadline must be an ISO week key YYYY-Www")
			} else if !containsStr(parent.Horizon(), week) {
				add(i, []string{"OVERRIDE_REJECTED"}, "deadline outside the planning horizon")
			} else {
				cons.DeadlineWeek = week
			}
		case "forbid_external":
			if o.Value == nil || (*o.Value != 0 && *o.Value != 1) {
				add(i, []string{"VALIDATION_FAILED"}, "forbid_external takes 1 or 0")
			} else {
				cons.ForbidExternal = *o.Value == 1
			}
		case "min_bus_factor":
			v := intValue(o.Value)
			if o.Value == nil || !isInteger(*o.Value) || v < 1 || v > 5 {
				add(i, []string{"VALIDATION_FAILED"}, "min_bus_factor must be an integer in 1..5")
			} else {
				cons.MinBusFactor = v
			}
		case "max_utilization":
			if o.Value == nil || *o.Value < 0.5 || *o.Value > 1 {
				add(i, []string{"VALIDATION_FAILED"}, "max_utilization must be in 0.5..1")
			} else {
				cons.MaxUtilizationRatio = *o.Value
			}
		}
	}
	return cons, rejected
}

// candidateUnavailable reports whether the candidate has no free capacity at all in the
// horizon (force- including it would violate G1).
func candidateUnavailable(snap *runinput.Snapshot, employeeID string) bool {
	candidate, ok := snap.CandidateByID(employeeID)
	if !ok {
		return true
	}
	for _, row := range candidate.Capacity {
		if !row.Unavailable && row.FreeHours > 0 {
			return false
		}
	}
	return true
}

// primaryVariant returns the highest-scoring served variant of a run, its employee
// roster and its P50 duration. Ties resolve to the lexicographically-first variant id for
// determinism.
func (s *WorkflowService) primaryVariant(runID string) (string, []string, float64) {
	payload, ok := s.GetRun(runID)
	if !ok {
		return "", nil, 0
	}
	variants, _ := payload["variants"].([]any)
	bestID := ""
	bestScore := -1.0
	for _, raw := range variants {
		v, _ := raw.(map[string]any)
		id, _ := v["variant_id"].(string)
		score, _ := v["score"].(float64)
		if id == "" {
			continue
		}
		if bestID == "" || score > bestScore+1e-12 || (math.Abs(score-bestScore) <= 1e-12 && id < bestID) {
			bestID, bestScore = id, score
		}
	}
	roster := s.VariantRosters(runID)[bestID]
	p50 := 0.0
	if v, ok := s.GetVariant(runID, bestID); ok {
		if d, ok := v["duration"].(map[string]any); ok {
			p50, _ = d["p50_weeks"].(float64)
		}
	}
	return bestID, roster, p50
}

func newStatus(payload map[string]any) string {
	if payload == nil {
		return "failed"
	}
	status, _ := payload["status"].(string)
	return status
}

func solverStatus(payload map[string]any) string {
	if payload == nil {
		return "UNKNOWN"
	}
	if diag, ok := payload["diagnostics"].(map[string]any); ok {
		st, _ := diag["solver_status"].(string)
		return st
	}
	return "UNKNOWN"
}

// GetExplanation returns the deterministic Explanation for one variant (ADR-007 §2),
// built from the stored authoritative metrics result and explainer detail, so the run's
// T_best/C_min references are preserved (it never recomputes run-relative metrics).
func (s *WorkflowService) GetExplanation(runID, variantID, locale string) (map[string]any, bool) {
	s.mu.RLock()
	repo := s.stores
	s.mu.RUnlock()
	if repo != nil && repo.Explanations != nil {
		stored, err := repo.Explanations.Get(context.Background(), runID, storedVariantID(runID, variantID), locale)
		if err == nil {
			if out, ok := decodeJSONMap(stored.Payload); ok {
				return out, true
			}
		}
	}
	s.mu.RLock()
	snapshot := s.snapshots[runID]
	result := s.varresults[runID][variantID]
	detail := s.details[runID][variantID]
	s.mu.RUnlock()
	if snapshot == nil || result.VariantID == "" || detail.VariantID == "" {
		return nil, false
	}
	exp, err := generateExplanation(runID, variantID, locale, snapshot, result, detail)
	if err != nil {
		return nil, false
	}
	return exp, true
}

// ---------- small helpers ----------

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func changedMembers(a, b []string) int {
	left := strSet(a)
	right := strSet(b)
	changed := 0
	for id := range left {
		if !right[id] {
			changed++
		}
	}
	for id := range right {
		if !left[id] {
			changed++
		}
	}
	return changed
}

func strSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

func appendUnique(list []string, id string) []string {
	if containsStr(list, id) {
		return list
	}
	return append(list, id)
}

func mergeUnique(list []string, ids []string) []string {
	for _, id := range ids {
		list = appendUnique(list, id)
	}
	return list
}

func without(list []string, id string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}

func overlaps(a, b []string) bool {
	set := strSet(b)
	for _, v := range a {
		if set[v] {
			return true
		}
	}
	return false
}

func uniqueNonEmpty(ids []string, exclude []string) []string {
	exSet := strSet(exclude)
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if id == "" || exSet[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func pinnedCount(c runinput.Constraints) int {
	return len(uniqueNonEmpty(append(append([]string{}, c.ForceInclude...), c.FreezeMembers...), c.ForceExclude))
}

func intValue(v *float64) int {
	if v == nil {
		return 0
	}
	return int(*v)
}

func isInteger(v float64) bool {
	return v == math.Trunc(v)
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
