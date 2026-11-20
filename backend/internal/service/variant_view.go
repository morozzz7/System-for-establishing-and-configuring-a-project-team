package service

// Full variant views and immutable run-input accessors (contracts/openapi.yaml Variant,
// RunInputs). They are built once when a run is computed and stored, so GET endpoints
// serve the exact, authoritative numbers without recomputation drift (ADR-003 §6).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/explain"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// buildVariantViews constructs the full contract Variant payload and the explainer detail
// for every validator-accepted variant of a run. It is deterministic: member fit, share
// and reason codes are derived from the snapshot, never copied from the solver.
func buildVariantViews(snap *runinput.Snapshot, accepted []metrics.Proposal, computed metrics.RunResult, resp optimizeResponse, runID string) (map[string]map[string]any, map[string]explain.VariantDetail) {
	payloads := make(map[string]map[string]any, len(accepted))
	details := make(map[string]explain.VariantDetail, len(accepted))
	memberCodesByVariant := memberReasonCodesByVariant(resp)
	for _, proposal := range accepted {
		result, ok := computed.VariantByID(proposal.VariantID)
		if !ok {
			continue
		}
		detail := variantDetail(snap, proposal, memberCodesByVariant[proposal.VariantID])
		payloads[proposal.VariantID] = fullVariantPayload(snap, runID, proposal, result, detail)
		details[proposal.VariantID] = detail
	}
	return payloads, details
}

// variantDetail computes the per-member fit/share/reason codes and weekly assignments the
// explainer and the full Variant payload need.
func variantDetail(snap *runinput.Snapshot, proposal metrics.Proposal, memberCodes map[string][]string) explain.VariantDetail {
	detail := explain.VariantDetail{
		VariantID:         proposal.VariantID,
		Members:           make([]explain.MemberDetail, 0, len(proposal.Members)),
		WeeklyAssignments: make([]explain.WeeklyAssignment, 0, len(proposal.Assignments)),
		ReasonCodes:       nil,
	}
	hoursByMember := make(map[string]float64, len(proposal.Members))
	for _, a := range proposal.Assignments {
		if a.Hours > 0 {
			hoursByMember[a.EmployeeID] += a.Hours
		}
	}
	for _, m := range proposal.Members {
		detail.Members = append(detail.Members, explain.MemberDetail{
			EmployeeID:      m.EmployeeID,
			RoleID:          m.RoleID,
			Grade:           m.Grade,
			AllocatedHours:  hoursByMember[m.EmployeeID],
			AssignmentShare: assignmentShare(snap, m.EmployeeID, hoursByMember[m.EmployeeID]),
			CompetenceFit:   competenceFit(snap, proposal, m.EmployeeID),
			ReasonCodes:     memberCodes[m.EmployeeID],
		})
	}
	for _, a := range proposal.Assignments {
		if a.Hours <= 0 {
			continue
		}
		detail.WeeklyAssignments = append(detail.WeeklyAssignments, explain.WeeklyAssignment{
			EmployeeID:    a.EmployeeID,
			RequirementID: a.RequirementID,
			Week:          a.Week,
			Hours:         a.Hours,
			Share:         weeklyShare(snap, a.EmployeeID, a.Week, a.Hours),
		})
	}
	sort.Slice(detail.Members, func(i, j int) bool { return detail.Members[i].EmployeeID < detail.Members[j].EmployeeID })
	sort.Slice(detail.WeeklyAssignments, func(i, j int) bool {
		if detail.WeeklyAssignments[i].EmployeeID != detail.WeeklyAssignments[j].EmployeeID {
			return detail.WeeklyAssignments[i].EmployeeID < detail.WeeklyAssignments[j].EmployeeID
		}
		if detail.WeeklyAssignments[i].Week != detail.WeeklyAssignments[j].Week {
			return detail.WeeklyAssignments[i].Week < detail.WeeklyAssignments[j].Week
		}
		return detail.WeeklyAssignments[i].RequirementID < detail.WeeklyAssignments[j].RequirementID
	})
	return detail
}

// competenceFit is the effort-weighted mean of min(1, lvl/L) over the member's
// assignments; a role-only requirement counts as a perfect fit (the role is what makes
// the assignment admissible).
func competenceFit(snap *runinput.Snapshot, proposal metrics.Proposal, employeeID string) float64 {
	numerator, denominator := 0.0, 0.0
	for _, a := range proposal.Assignments {
		if a.EmployeeID != employeeID || a.Hours <= 0 {
			continue
		}
		req, ok := snap.RequirementByID(a.RequirementID)
		if !ok {
			continue
		}
		q := 1.0
		if req.CompetencyID != nil {
			if cand, ok := snap.CandidateByID(employeeID); ok {
				lvl := cand.CompetencyLevels[*req.CompetencyID]
				q = math.Min(1, float64(lvl)/float64(req.Level))
			}
		}
		numerator += a.Hours * q
		denominator += a.Hours
	}
	if denominator <= 0 {
		return 0
	}
	return round6(numerator / denominator)
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// assignmentShare is the member's total allocation relative to available capacity over the
// weeks they are assigned (contracts VariantMember.assignment_share, 0..1).
func assignmentShare(snap *runinput.Snapshot, employeeID string, hours float64) float64 {
	if hours <= 0 {
		return 0
	}
	candidate, ok := snap.CandidateByID(employeeID)
	if !ok {
		return 1
	}
	total := 0.0
	for _, row := range candidate.Capacity {
		if !row.Unavailable && row.FreeHours > 0 {
			total += row.FreeHours
		}
	}
	if total <= 0 {
		return 1
	}
	return round6(math.Min(1, hours/total))
}

// weeklyShare is one week's assignment share (hours / free capacity).
func weeklyShare(snap *runinput.Snapshot, employeeID, week string, hours float64) float64 {
	candidate, ok := snap.CandidateByID(employeeID)
	if !ok {
		return 1
	}
	for _, row := range candidate.Capacity {
		if row.Week == week {
			if row.FreeHours <= 0 {
				return 0
			}
			return round6(math.Min(1, hours/row.FreeHours))
		}
	}
	return 0
}

// memberReasonCodesByVariant reads the solver's per-member reason codes by variant (the
// solver proposes inclusion reasons; Go re-validates the roster but the reasons are a
// proposal we surface, not a number we trust).
func memberReasonCodesByVariant(resp optimizeResponse) map[string]map[string][]string {
	out := make(map[string]map[string][]string, len(resp.Variants))
	for _, v := range resp.Variants {
		codes := make(map[string][]string, len(v.Members))
		for _, m := range v.Members {
			if len(m.ReasonCodes) > 0 {
				codes[m.EmployeeID] = append([]string(nil), m.ReasonCodes...)
			}
		}
		out[v.VariantID] = codes
	}
	return out
}

// fullVariantPayload shapes the authoritative contract Variant for one served variant.
func fullVariantPayload(snap *runinput.Snapshot, runID string, proposal metrics.Proposal, result metrics.VariantResult, detail explain.VariantDetail) map[string]any {
	members := make([]any, 0, len(detail.Members))
	for _, m := range detail.Members {
		members = append(members, map[string]any{
			"employee_id":      m.EmployeeID,
			"role_id":          m.RoleID,
			"grade":            m.Grade,
			"allocated_hours":  round6(m.AllocatedHours),
			"assignment_share": m.AssignmentShare,
			"competence_fit":   round6(m.CompetenceFit),
			"reason_codes":     memberReasonCodes(detail, snap, m),
		})
	}
	weekly := make([]any, 0, len(detail.WeeklyAssignments))
	for _, a := range detail.WeeklyAssignments {
		weekly = append(weekly, map[string]any{
			"employee_id":    a.EmployeeID,
			"requirement_id": a.RequirementID,
			"week":           a.Week,
			"hours":          round6(a.Hours),
			"share":          a.Share,
		})
	}
	return map[string]any{
		"variant_id":          result.VariantID,
		"run_id":              runID,
		"label":               result.Label,
		"gate_pass":           result.GatePass,
		"gate_violations":     result.Gate,
		"score":               result.Metrics.Score,
		"team_size":           result.TeamSize,
		"cost_units":          result.CostUnits,
		"duration":            result.Duration,
		"metric_values":       result.Metrics,
		"metric_details":      result.Details,
		"members":             members,
		"weekly_assignments":  weekly,
		"deficits":            contractDeficits(result.Deficits),
		"active_constraints":  activeConstraints(snap),
		"excluded_candidates": excludedCandidates(snap, detail),
		"diversity_jaccard":   map[string]any{},
		"reason_codes":        summaryReasonCodes(snap, detail, memberReasonCodesForVariant(detail.VariantID, detail)),
	}
}

// contractDeficits maps Go-computed deficits to the contract Deficit shape. Names fall
// back to the role because this vertical slice has no taxonomy repository.
func contractDeficits(deficits []metrics.Deficit) []any {
	out := make([]any, 0, len(deficits))
	for _, d := range deficits {
		name := d.RoleID
		competencyID := ""
		if d.CompetencyID != nil {
			competencyID = *d.CompetencyID
			name = competencyID
		}
		actions := []any{}
		if d.ExternalCooperationFlag {
			actions = append(actions, map[string]any{"action": "contractor", "reason_codes": []string{"DEFICIT_EXTERNAL_CONTRACTOR"}})
		} else {
			actions = append(actions, map[string]any{"action": "upskill", "reason_codes": []string{"DEFICIT_UPSKILL_INTERNAL"}})
		}
		out = append(out, map[string]any{
			"competency_id":             competencyID,
			"competency_name_ru":        name,
			"requirement_ids":           []string{d.RequirementID},
			"person_days":               d.PersonDays,
			"weeks":                     round6(d.PersonDays / float64(len(requirementListFor(d)))), // weeks soft is a heuristic; keep 1 + fraction
			"external_cooperation_flag": d.ExternalCooperationFlag,
			"recommended_actions":       actions,
		})
	}
	return out
}

func requirementListFor(d metrics.Deficit) []any {
	return []any{d.RequirementID}
}

// activeConstraints surfaces every set G4 constraint of the snapshot (contract
// ActiveConstraint), each with a deterministic reason code.
func activeConstraints(snap *runinput.Snapshot) []any {
	cons := snap.Constraints()
	out := []any{}
	if len(cons.ForceInclude) > 0 {
		out = append(out, constraint("force_include", cons.ForceInclude, "user", []string{"CONSTRAINT_ACTIVE_FORCE_INCLUDE"}))
	}
	if len(cons.ForceExclude) > 0 {
		out = append(out, constraint("force_exclude", cons.ForceExclude, "user", []string{"CONSTRAINT_ACTIVE_FORCE_EXCLUDE"}))
	}
	if cons.MaxTeamSize > 0 {
		out = append(out, constraint("max_team_size", cons.MaxTeamSize, "policy", []string{"CONSTRAINT_ACTIVE_TEAM_SIZE"}))
	}
	if cons.MinTeamSize > 0 {
		out = append(out, constraint("min_team_size", cons.MinTeamSize, "user", []string{"CONSTRAINT_ACTIVE_TEAM_SIZE"}))
	}
	if cons.BudgetCapUnits > 0 {
		out = append(out, constraint("budget_cap", cons.BudgetCapUnits, "user", []string{"CONSTRAINT_ACTIVE_BUDGET"}))
	}
	if cons.DeadlineWeek != "" {
		out = append(out, constraint("deadline", cons.DeadlineWeek, "user", []string{"CONSTRAINT_ACTIVE_DEADLINE"}))
	}
	if cons.MinBusFactor > 0 {
		out = append(out, constraint("min_bus_factor", cons.MinBusFactor, "policy", []string{"CONSTRAINT_ACTIVE_BUS_FACTOR"}))
	}
	if cons.MaxUtilizationRatio > 0 {
		out = append(out, constraint("max_utilization", cons.MaxUtilizationRatio, "policy", []string{"CONSTRAINT_BINDING_CAPACITY"}))
	}
	return out
}

func constraint(typ string, value any, source string, codes []string) map[string]any {
	return map[string]any{"type": typ, "value": value, "source": source, "reason_codes": codes}
}

// excludedCandidates maps the explainer's deterministic exclusions to the contract
// ExcludedCandidate shape.
func excludedCandidates(snap *runinput.Snapshot, detail explain.VariantDetail) []any {
	exclusions := explain.CandidateExclusions(snap, detail)
	out := make([]any, 0, len(exclusions))
	for _, ex := range exclusions {
		out = append(out, map[string]any{
			"employee_id":  ex.EmployeeID,
			"reason_codes": ex.ReasonCodes,
			"detail":       ex.Facts,
		})
	}
	return out
}

func memberReasonCodes(detail explain.VariantDetail, snap *runinput.Snapshot, m explain.MemberDetail) []string {
	if len(m.ReasonCodes) > 0 {
		return m.ReasonCodes
	}
	// Deterministic fallback derived from the snapshot when the solver gave none.
	codes := []string{"INCLUDE_REQUIRED_ROLE"}
	if cand, ok := snap.CandidateByID(m.EmployeeID); ok {
		if cand.BenchFlag {
			codes = append(codes, "INCLUDE_BENCH")
		}
		if containsStr(snap.Constraints().ForceInclude, m.EmployeeID) {
			codes = append(codes, "INCLUDE_USER_FORCE")
		}
	}
	return codes
}

func memberReasonCodesForVariant(_ string, detail explain.VariantDetail) map[string]bool {
	out := make(map[string]bool)
	for _, m := range detail.Members {
		for _, c := range m.ReasonCodes {
			out[c] = true
		}
	}
	return out
}

// summaryReasonCodes reduces per-member codes to a stable variant-level set for
// contracts Variant.reason_codes, including constraint/no-net codes.
func summaryReasonCodes(snap *runinput.Snapshot, detail explain.VariantDetail, memberCodes map[string]bool) []string {
	set := map[string]bool{
		"INCLUDE_REQUIRED_ROLE": len(detail.Members) > 0,
	}
	if memberCodes["INCLUDE_BENCH"] {
		set["INCLUDE_BENCH"] = true
	}
	cons := snap.Constraints()
	if cons.MaxTeamSize > 0 {
		set["CONSTRAINT_ACTIVE_TEAM_SIZE"] = true
	}
	if cons.DeadlineWeek != "" {
		set["CONSTRAINT_ACTIVE_DEADLINE"] = true
	}
	out := make([]string, 0, len(set))
	for code, ok := range set {
		if ok {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ---------- accessors ----------

// GetRunInputs returns the immutable RunInputs of a run (contract shape) from its stored
// snapshot: everything the solver saw plus the capacity digests for audit (ADR-003 §6).
func (s *WorkflowService) GetRunInputs(runID string) (map[string]any, bool) {
	s.mu.RLock()
	snapshot, ok := s.snapshots[runID]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	snapshotID := "snp_" + snapshot.SnapshotSHA256()[:8]
	horizon := snapshot.Horizon()
	candidates := make([]any, 0, len(snapshot.Candidates()))
	capacityRows := 0
	for _, c := range snapshot.Candidates() {
		capacityRows += len(c.Capacity)
		compIDs := make([]string, 0, len(c.CompetencyLevels))
		for id := range c.CompetencyLevels {
			compIDs = append(compIDs, id)
		}
		sort.Strings(compIDs)
		encoded, _ := json.Marshal(c.Capacity)
		sum := sha256.Sum256(encoded)
		digest := hex.EncodeToString(sum[:])
		candidates = append(candidates, map[string]any{
			"employee_id":      c.EmployeeID,
			"grade":            c.Grade,
			"cost_coefficient": c.CostCoefficient,
			"competency_ids":   compIDs,
			"capacity_digest":  digest,
			"capacity_weeks":   len(c.Capacity),
		})
	}
	out := map[string]any{
		"run_id":            runID,
		"snapshot_id":       snapshotID,
		"created_at":        snapshot.CreatedAt(),
		"snapshot_sha256":   snapshot.SnapshotSHA256(),
		"horizon":           map[string]any{"start_week": horizon[0], "end_week": horizon[len(horizon)-1], "weeks": horizon},
		"weights":           snapshot.Weights(),
		"constraints":       snapshot.Constraints(),
		"requirement_count": len(snapshot.Requirements()),
		"candidate_count":   len(snapshot.Candidates()),
		"capacity_rows":     capacityRows,
		"candidates":        candidates,
	}
	return jsonMap(out), true
}

// GetVariant returns the stored full Variant payload of a run, or ok=false when the run
// or the variant does not exist.
func (s *WorkflowService) GetVariant(runID, variantID string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if variants, runOK := s.variants[runID]; runOK {
		if variant, ok := variants[variantID]; ok {
			return jsonMap(variant), true
		}
	}
	return nil, false
}

// GetDetail returns the explainer detail of one variant (for Generate).
func (s *WorkflowService) GetDetail(runID, variantID string) (explain.VariantDetail, *runinput.Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, snapOK := s.snapshots[runID]
	details, runOK := s.details[runID]
	if !snapOK || !runOK {
		return explain.VariantDetail{}, nil, false
	}
	detail, ok := details[variantID]
	return detail, snapshot, ok
}

// VariantRosters returns the employee roster of every stored variant of a run (for the
// recalculation diff baseline and diversity facts).
func (s *WorkflowService) VariantRosters(runID string) map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]string)
	for variantID, variants := range s.variants[runID] {
		members, _ := variants["members"].([]any)
		roster := make([]string, 0, len(members))
		for _, m := range members {
			if mm, ok := m.(map[string]any); ok {
				if id, ok := mm["employee_id"].(string); ok {
					roster = append(roster, id)
				}
			}
		}
		sort.Strings(roster)
		out[variantID] = roster
	}
	return out
}
