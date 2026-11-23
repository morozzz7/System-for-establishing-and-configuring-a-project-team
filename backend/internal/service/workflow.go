package service

// This workflow service is the persistence-free vertical slice for phase 3.6.
// Production persistence can replace the maps without changing the HTTP or ML seams.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/explain"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/mlclient"
	"github.com/teamopt/backend/internal/runinput"
	"github.com/teamopt/backend/internal/validator"
)

type BriefInput struct{ Title, SourceType, Language, Text, RequestedStartWeek, DeadlineWeek string }
type BriefRecord struct {
	BriefID, Title, SourceType, Language, Status string
	RequestedStartWeek, DeadlineWeek             string
	CreatedAt                                    time.Time
	Text                                         string
	Requirements                                 []map[string]any
	Review                                       []map[string]any
	Diagnostics                                  map[string]any
}

var ErrDecisionExists = errors.New("decision already recorded")
var ErrVariantNotFound = errors.New("variant not found")

type WorkflowService struct {
	mu         sync.RWMutex
	ml         *mlclient.Client
	briefs     map[string]*BriefRecord
	runs       map[string]map[string]any
	snapshots  map[string]*runinput.Snapshot
	variants   map[string]map[string]map[string]any // runID -> variantID -> full Variant payload
	details    map[string]map[string]explain.VariantDetail
	varresults map[string]map[string]metrics.VariantResult // runID -> variantID -> authoritative result
	decisions  map[string]map[string]any                   // runID -> decision record
	candidates CandidateSource
	next       int
}

func NewWorkflowService(ml *mlclient.Client) *WorkflowService {
	return &WorkflowService{
		ml:         ml,
		briefs:     map[string]*BriefRecord{},
		runs:       map[string]map[string]any{},
		snapshots:  map[string]*runinput.Snapshot{},
		variants:   map[string]map[string]map[string]any{},
		details:    map[string]map[string]explain.VariantDetail{},
		varresults: map[string]map[string]metrics.VariantResult{},
		decisions:  map[string]map[string]any{},
	}
}

// SetCandidateSource wires the persistent employee pool into runs. The in-memory
// workflow slice keeps its deterministic stand-ins when no source is configured.
func (s *WorkflowService) SetCandidateSource(source CandidateSource) {
	s.candidates = source
}

func (s *WorkflowService) CreateBrief(ctx context.Context, input BriefInput) (*BriefRecord, error) {
	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("brf_%04d", s.next)
	s.mu.Unlock()
	req := map[string]any{"request_id": fmt.Sprintf("req_brief%04d", s.next), "brief_id": id, "locale": input.Language,
		"brief":    map[string]any{"language": input.Language, "text": input.Text, "source_type": input.SourceType},
		"taxonomy": map[string]any{}, "options": map[string]any{"max_requirements": 60, "min_confidence": 0.55, "return_candidates": true, "timeout_ms": 20000}}
	var response struct {
		Requirements []map[string]any `json:"requirements"`
		Review       []map[string]any `json:"requirements_for_review"`
		Diagnostics  map[string]any   `json:"diagnostics"`
		Warnings     []string         `json:"warnings"`
	}
	if err := s.ml.Extract(ctx, req, &response); err != nil {
		return nil, err
	}
	status := "extracted"
	if len(response.Review) > 0 {
		status = "needs_review"
	}
	record := &BriefRecord{
		BriefID: id, Title: input.Title, SourceType: input.SourceType, Language: input.Language,
		Status: status, RequestedStartWeek: input.RequestedStartWeek, DeadlineWeek: input.DeadlineWeek,
		CreatedAt: time.Now().UTC(), Text: input.Text,
		Requirements: response.Requirements, Review: response.Review, Diagnostics: response.Diagnostics,
	}
	if start, deadline, ok := briefTimeline(record); ok {
		record.RequestedStartWeek, record.DeadlineWeek = start, deadline
	}
	s.mu.Lock()
	s.briefs[id] = record
	s.mu.Unlock()
	return record, nil
}

func (s *WorkflowService) ListBriefs() []*BriefRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*BriefRecord, 0, len(s.briefs))
	for _, b := range s.briefs {
		out = append(out, b)
	}
	return out
}
func (s *WorkflowService) GetBrief(id string) (*BriefRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.briefs[id]
	return b, ok
}
func (s *WorkflowService) ConfirmBrief(id string) (*BriefRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.briefs[id]
	if ok {
		b.Status = "confirmed"
		b.Requirements = append(b.Requirements, b.Review...)
		b.Review = nil
	}
	return b, ok
}

// weightsPresets are the composite-score presets of contracts/reference.yaml
// (docs/METRICS.md "Starting weight presets"). They sum to 1.0.
var weightsPresets = map[string]map[string]float64{
	"fast": {
		"M1": 0.15, "M2": 0.15, "M3": 0.08, "M4": 0.07, "M5": 0.28, "M6": 0.04,
		"M7": 0.05, "M8": 0.06, "M9": 0.03, "M10": 0.03, "M11": 0.02, "M12": 0.04,
	},
	"economical": {
		"M1": 0.15, "M2": 0.12, "M3": 0.10, "M4": 0.05, "M5": 0.08, "M6": 0.27,
		"M7": 0.06, "M8": 0.06, "M9": 0.03, "M10": 0.04, "M11": 0.03, "M12": 0.01,
	},
	"balanced": {
		"M1": 0.15, "M2": 0.13, "M3": 0.10, "M4": 0.08, "M5": 0.15, "M6": 0.10,
		"M7": 0.08, "M8": 0.08, "M9": 0.05, "M10": 0.04, "M11": 0.02, "M12": 0.02,
	},
	"low_risk": {
		"M1": 0.12, "M2": 0.10, "M3": 0.08, "M4": 0.07, "M5": 0.10, "M6": 0.05,
		"M7": 0.12, "M8": 0.10, "M9": 0.10, "M10": 0.04, "M11": 0.02, "M12": 0.10,
	},
}

// CreateRun is the run pipeline of AGENTS.md:
// brief -> immutable snapshot -> ML optimize -> independent validator -> authoritative
// metrics -> contract-shaped TeamRun.
//
// The response never contains the solver's advisory metric estimates: every metric value,
// duration, cost and deficit in the payload is recomputed by Go from the stored snapshot
// and the variant's weekly assignments.
func (s *WorkflowService) CreateRun(ctx context.Context, briefID string, input map[string]any) (map[string]any, error) {
	b, ok := s.GetBrief(briefID)
	if !ok {
		return nil, fmt.Errorf("%w: brief %s not found", ErrInvalidRunInput, briefID)
	}
	if b.Status != "confirmed" && len(b.Review) > 0 {
		return nil, fmt.Errorf("%w: brief %s requires review", ErrInvalidRunInput, briefID)
	}

	s.mu.Lock()
	s.next++
	seq := s.next
	s.mu.Unlock()
	runID := fmt.Sprintf("run_%04d", seq)
	createdAt := time.Now().UTC()

	data, err := s.runData(ctx, b, runID, input, createdAt)
	if err != nil {
		return nil, err
	}
	payload, err := s.computeAndStore(ctx, b.BriefID, runID, "", data, createdAt)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// computeAndStore runs the shared pipeline of AGENTS.md:
// immutable snapshot -> ML optimize -> independent validator -> authoritative metrics ->
// contract-shaped TeamRun, and stores the run, its snapshot, full variant payloads and
// explainer details. Both create and recalculation reuse it (ADR-007 §3). When parentRunID
// is non-empty the payload is marked as a recalculation run (contracts TeamRun.parent_run_id).
func (s *WorkflowService) computeAndStore(ctx context.Context, briefID, runID, parentRunID string, data runinput.Data, createdAt time.Time) (map[string]any, error) {
	snapshot, err := runinput.Build(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRunInput, err.Error())
	}

	optRequest, err := buildOptimizeRequest(snapshot, fmt.Sprintf("req_run%s", runID[len("run_"):]))
	if err != nil {
		return nil, err
	}
	var optResponse optimizeResponse
	if err := s.ml.Optimize(ctx, optRequest, &optResponse); err != nil {
		return nil, err
	}

	// "Solver proposes, Go verifies": the validator is the rejecting seam, the metrics
	// package is the only source of numbers. Variants rejected here never reach the user.
	proposals := proposalsFromResponse(optResponse, snapshot)
	validatorInput := buildValidatorInput(snapshot)
	v := validator.New()
	accepted := make([]metrics.Proposal, 0, len(proposals))
	validatorRejected := 0
	for _, proposal := range proposals {
		validatorInput.Variant = variantForValidator(proposal, runID, optResponse)
		result, err := v.Validate(validatorInput)
		if err != nil {
			return nil, fmt.Errorf("validate variant %s: %w", proposal.VariantID, err)
		}
		if !result.GatePass {
			validatorRejected++
			slog.WarnContext(ctx, "validator rejected proposed variant",
				"run_id", runID,
				"variant_id", proposal.VariantID,
				"violations", result.Violations,
				"reason_codes", result.ReasonCodes,
				"details", result.Details,
			)
			continue
		}
		accepted = append(accepted, proposal)
	}

	computed, rejectedByGate, err := computeServed(snapshot, accepted)
	if err != nil {
		return nil, err
	}
	validatorRejected += rejectedByGate

	payloads, details := buildVariantViews(snapshot, accepted, computed, optResponse, runID)
	payload := runPayload(runID, briefID, data, computed.Variants, computed.Set, optResponse, validatorRejected, createdAt)
	if parentRunID != "" {
		payload["parent_run_id"] = parentRunID
	}

	varResults := make(map[string]metrics.VariantResult, len(computed.Variants))
	for _, vr := range computed.Variants {
		varResults[vr.VariantID] = vr
	}

	s.mu.Lock()
	s.runs[runID] = payload
	s.snapshots[runID] = snapshot
	s.variants[runID] = payloads
	s.details[runID] = details
	s.varresults[runID] = varResults
	s.mu.Unlock()
	return payload, nil
}

// computeServed computes the authoritative metrics of the validator-accepted variants and
// removes the ones Go still refuses to serve: a variant that fails the recomputed G1-G5
// gate or has no computable duration is not shown to the user (AGENTS.md hard rule 6).
//
// Dropping a variant changes the run-relative references (M5's T_best, M6's C_min/C_ref),
// so the run is recomputed once without it. The second pass is a defense in depth: it only
// happens when the validator and the metrics gate disagree, which is a defect signal.
func computeServed(snapshot *runinput.Snapshot, accepted []metrics.Proposal) (metrics.RunResult, int, error) {
	proposals := accepted
	rejected := 0
	for attempt := 0; ; attempt++ {
		computed, err := metrics.ComputeRun(metrics.RunInput{
			Snapshot:  snapshot,
			Proposals: proposals,
			Schedule: metrics.ScheduleConfig{
				MonteCarloRuns:          metrics.DefaultMonteCarloRuns,
				Seed:                    snapshot.Seed(),
				PortfolioTauWorkingDays: metrics.DefaultPortfolioTauWorkingDays,
			},
		})
		if err != nil {
			return metrics.RunResult{}, rejected, fmt.Errorf("compute metrics: %w", err)
		}
		servable := make(map[string]bool, len(computed.Variants))
		for _, variant := range computed.Variants {
			if variant.GatePass && variant.ScheduleError == "" {
				servable[variant.VariantID] = true
			}
		}
		kept := make([]metrics.Proposal, 0, len(proposals))
		for _, proposal := range proposals {
			if servable[proposal.VariantID] {
				kept = append(kept, proposal)
			}
		}
		if len(kept) == len(proposals) || attempt > 0 {
			return computed, rejected, nil
		}
		rejected += len(proposals) - len(kept)
		proposals = kept
	}
}

// GetSnapshot returns the immutable snapshot a run was computed from (for audit and
// recalculation with a constraint override).
func (s *WorkflowService) GetSnapshot(runID string) (*runinput.Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[runID]
	return snapshot, ok
}

func (s *WorkflowService) GetRun(id string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	return r, ok
}

// runData builds the run snapshot input from the confirmed brief and the request body.
// When a CandidateSource is configured the pool comes from the employee, competency,
// assignment and capacity tables; the deterministic stand-ins remain the fallback for the
// in-memory vertical slice used by unit tests.
func (s *WorkflowService) runData(ctx context.Context, b *BriefRecord, runID string, input map[string]any, createdAt time.Time) (runinput.Data, error) {
	weeks, err := horizonWeeks(input, b)
	if err != nil {
		return runinput.Data{}, err
	}
	preset, err := runPreset(input)
	if err != nil {
		return runinput.Data{}, err
	}
	weights, err := runWeights(input, preset)
	if err != nil {
		return runinput.Data{}, err
	}
	constraints, err := runConstraints(input)
	if err != nil {
		return runinput.Data{}, err
	}
	requirements, err := requirementsFromBrief(b.Requirements)
	if err != nil {
		return runinput.Data{}, err
	}

	candidates := demoCandidates(requirements, weeks)
	if s.candidates != nil {
		candidates, err = s.candidates.Candidates(ctx, requirements, weeks, candidateFilterFromInput(input))
		if err != nil {
			return runinput.Data{}, err
		}
		if len(candidates) == 0 {
			return runinput.Data{}, fmt.Errorf("%w: no active employees match the brief requirements", ErrInvalidRunInput)
		}
	}

	return runinput.Data{
		RunID:        runID,
		BriefID:      b.BriefID,
		Preset:       preset,
		K:            runK(input),
		Seed:         int64Option(input, "seed", 42),
		TimeLimitMS:  runTimeLimit(input),
		Horizon:      weeks,
		Weights:      weights,
		Requirements: requirements,
		Candidates:   candidates,
		Constraints:  constraints,
		Portfolio:    []runinput.PortfolioProject{},
		CreatedAt:    createdAt,
	}, nil
}

// demoGrades and demoCostCoefficients keep the demo candidates anonymous: cost is a
// coefficient by grade, never a personal rate.
const (
	demoGrade           = "M"
	demoCostCoefficient = 1.4
	demoWeeklyNormHours = 40.0
)

// demoCandidates derives one synthetic, fully available candidate per requirement.
func demoCandidates(requirements []runinput.Requirement, weeks []string) []runinput.Candidate {
	out := make([]runinput.Candidate, 0, len(requirements))
	for i, requirement := range requirements {
		levels := map[string]int{}
		if requirement.CompetencyID != nil {
			levels[*requirement.CompetencyID] = 5
		}
		capacity := make([]runinput.CapacityRow, 0, len(weeks))
		for _, week := range weeks {
			capacity = append(capacity, runinput.CapacityRow{
				Week: week, WorkingDays: 5, NormHours: demoWeeklyNormHours,
				AssignedHours: 0, AbsentHours: 0, FreeHours: demoWeeklyNormHours,
			})
		}
		out = append(out, runinput.Candidate{
			EmployeeID:        fmt.Sprintf("emp_demo%04d", i+1),
			Grade:             demoGrade,
			CostCoefficient:   demoCostCoefficient,
			RoleProfileID:     requirement.RoleID,
			EmploymentType:    "internal",
			BenchFlag:         true,
			ExperienceScore:   nil,
			CurrentProjectIDs: []string{},
			CompetencyLevels:  levels,
			Capacity:          capacity,
		})
	}
	return out
}

// requirementsFromBrief normalizes the extracted requirement maps into snapshot
// requirements. Anything that cannot be mapped is reported, never guessed.
func requirementsFromBrief(items []map[string]any) ([]runinput.Requirement, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: the brief has no requirements to plan", ErrInvalidRunInput)
	}
	out := make([]runinput.Requirement, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		reqID, _ := item["requirement_id"].(string)
		if reqID == "" {
			return nil, fmt.Errorf("%w: requirements[%d] has no requirement_id", ErrInvalidRunInput, i)
		}
		if seen[reqID] {
			return nil, fmt.Errorf("%w: requirement_id %s is duplicated", ErrInvalidRunInput, reqID)
		}
		seen[reqID] = true
		role, _ := item["role"].(map[string]any)
		roleID, _ := role["role_id"].(string)
		if roleID == "" {
			return nil, fmt.Errorf("%w: requirement %s has no role_id", ErrInvalidRunInput, reqID)
		}
		var competency *string
		if comp, ok := item["competency"].(map[string]any); ok {
			if id, _ := comp["competency_id"].(string); id != "" {
				value := id
				competency = &value
			}
		}
		level := intOption(item, "level", 0)
		if level < 1 || level > 5 {
			return nil, fmt.Errorf("%w: requirement %s has level %d outside 1..5", ErrInvalidRunInput, reqID, level)
		}
		criticality, _ := item["criticality"].(string)
		if _, ok := runinput.CriticalityWeights[criticality]; !ok {
			return nil, fmt.Errorf("%w: requirement %s has criticality %q", ErrInvalidRunInput, reqID, criticality)
		}
		headcount := intOption(item, "headcount", 0)
		if headcount < 1 {
			headcount = 1
		}
		effort, err := effortFromBrief(item["effort"])
		if err != nil {
			return nil, fmt.Errorf("requirement %s: %w", reqID, err)
		}
		allowsExternal := true
		if value, ok := item["allows_external"].(bool); ok {
			allowsExternal = value
		}
		out = append(out, runinput.Requirement{
			RequirementID:  reqID,
			RoleID:         roleID,
			CompetencyID:   competency,
			Level:          level,
			Criticality:    criticality,
			Headcount:      headcount,
			Effort:         effort,
			AllowsExternal: allowsExternal,
		})
	}
	return out, nil
}

// effortFromBrief reads the three-point estimate. A single number is never accepted
// (AGENTS.md hard rule 5).
func effortFromBrief(raw any) (runinput.Effort, error) {
	effort, ok := raw.(map[string]any)
	if !ok {
		return runinput.Effort{}, fmt.Errorf("%w: effort is missing", ErrInvalidRunInput)
	}
	min := floatOption(effort, "min_person_days", 0)
	likely := floatOption(effort, "likely_person_days", 0)
	max := floatOption(effort, "max_person_days", 0)
	if min <= 0 || likely <= 0 || max <= 0 {
		return runinput.Effort{}, fmt.Errorf("%w: effort must be positive in all three points", ErrInvalidRunInput)
	}
	if min > likely || likely > max {
		return runinput.Effort{}, fmt.Errorf("%w: effort must satisfy min <= likely <= max", ErrInvalidRunInput)
	}
	return runinput.Effort{MinPersonDays: min, LikelyPersonDays: likely, MaxPersonDays: max}, nil
}

// isoWeekInText finds ISO week mentions in brief text. Planning weeks are cited explicitly
// in technical briefs; taking the first and last is deterministic and needs no LLM.
var isoWeekInText = regexp.MustCompile(`\b20\d{2}-W\d{2}\b`)

// briefTimeline returns the planning window recorded on a brief: explicit upload fields first,
// then the first and last ISO week mentioned in the brief text.
func briefTimeline(b *BriefRecord) (string, string, bool) {
	start, deadline := b.RequestedStartWeek, b.DeadlineWeek
	if !domain.ValidateWeekKey(start) {
		start = ""
	}
	if !domain.ValidateWeekKey(deadline) {
		deadline = ""
	}
	mentioned := make([]string, 0, 2)
	for _, week := range isoWeekInText.FindAllString(b.Text, -1) {
		if domain.ValidateWeekKey(week) {
			mentioned = append(mentioned, week)
		}
	}
	if start == "" && len(mentioned) > 0 {
		start = mentioned[0]
	}
	if deadline == "" && len(mentioned) > 0 {
		deadline = mentioned[len(mentioned)-1]
	}
	if start == "" || deadline == "" {
		return "", "", false
	}
	if start > deadline {
		start, deadline = deadline, start
	}
	return start, deadline, true
}

// addWeeks returns the ISO week key n weeks after start (n may be negative).
func addWeeks(start string, n int) string {
	year, week, err := domain.ParseWeek(start)
	if err != nil {
		return start
	}
	monday := domain.WeekMonday(year, week).AddDate(0, 0, n*7)
	endYear, endWeek := monday.ISOWeek()
	return domain.FormatWeek(endYear, endWeek)
}

// horizonWeeks reads the requested horizon. When the request omits it, the window comes from
// the brief; the three-week default applies only when neither source provides one. Weeks are
// sorted chronologically because ISO week keys sort lexicographically.
func horizonWeeks(input map[string]any, b *BriefRecord) ([]string, error) {
	raw, ok := input["horizon"].(map[string]any)
	if !ok {
		start, deadline, hasWindow := briefTimeline(b)
		if !hasWindow {
			return []string{"2026-W10", "2026-W11", "2026-W12"}, nil
		}
		if start == deadline {
			deadline = addWeeks(start, 11)
		}
		weeks, err := domain.WeekRange(start, deadline)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid brief horizon %s..%s: %v", ErrInvalidRunInput, start, deadline, err)
		}
		return weeks, nil
	}
	startWeek, _ := raw["start_week"].(string)
	endWeek, _ := raw["end_week"].(string)
	if startWeek != "" || endWeek != "" {
		briefStart, briefEnd, _ := briefTimeline(b)
		if startWeek == "" {
			if briefStart != "" && (endWeek == "" || briefStart <= endWeek) {
				startWeek = briefStart
			} else if endWeek != "" {
				startWeek = addWeeks(endWeek, -11)
			}
		}
		if endWeek == "" {
			if briefEnd != "" && briefEnd >= startWeek {
				endWeek = briefEnd
			} else if startWeek != "" {
				endWeek = addWeeks(startWeek, 11)
			}
		}
		weeks, err := domain.WeekRange(startWeek, endWeek)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid horizon range %s..%s: %v", ErrInvalidRunInput, startWeek, endWeek, err)
		}
		if len(weeks) == 0 {
			return nil, fmt.Errorf("%w: horizon.weeks must not be empty", ErrInvalidRunInput)
		}
		return weeks, nil
	}

	list, _ := raw["weeks"].([]any)
	weeks := make([]string, 0, len(list))
	for i, entry := range list {
		week, _ := entry.(string)
		if !domain.ValidateWeekKey(week) {
			return nil, fmt.Errorf("%w: horizon.weeks[%d] = %q is not an ISO week key", ErrInvalidRunInput, i, week)
		}
		weeks = append(weeks, week)
	}
	if len(weeks) == 0 {
		return nil, fmt.Errorf("%w: horizon.weeks must not be empty", ErrInvalidRunInput)
	}
	sort.Strings(weeks)
	for i := 1; i < len(weeks); i++ {
		if weeks[i] == weeks[i-1] {
			return nil, fmt.Errorf("%w: horizon.weeks contains %s twice", ErrInvalidRunInput, weeks[i])
		}
	}
	return weeks, nil
}

func runPreset(input map[string]any) (string, error) {
	preset, _ := input["preset"].(string)
	if preset == "" {
		return "balanced", nil
	}
	if !runinput.Presets[preset] {
		return "", fmt.Errorf("%w: preset %q must be one of fast, economical, balanced, low_risk", ErrInvalidRunInput, preset)
	}
	return preset, nil
}

// requiredWeightKeys are the components the solver objective requires (contracts
// ml-openapi.yaml OptimizeWeights.required).
var requiredWeightKeys = []string{"M1", "M2", "M3", "M5", "M6", "M7", "M8", "M9", "M10", "M11"}

// runWeights returns the objective weights: the preset unless the request overrides them.
// Either way the weights are normalized to sum 1.0, because both the solver contract and
// the composite score treat them as a distribution.
func runWeights(input map[string]any, preset string) (map[string]float64, error) {
	weights := make(map[string]float64, len(weightsPresets[preset]))
	for key, value := range weightsPresets[preset] {
		weights[key] = value
	}
	if raw, ok := input["weights"].(map[string]any); ok && len(raw) > 0 {
		weights = make(map[string]float64, len(raw))
		for key, value := range raw {
			weight, ok := value.(float64)
			if !ok {
				return nil, fmt.Errorf("%w: weights[%s] must be a number", ErrInvalidRunInput, key)
			}
			if weight < 0 || weight > 1 {
				return nil, fmt.Errorf("%w: weights[%s] = %v must be in [0,1]", ErrInvalidRunInput, key, weight)
			}
			if weight == 0 {
				continue
			}
			weights[key] = weight
		}
	}
	sum := 0.0
	for _, weight := range weights {
		sum += weight
	}
	if sum <= 0 {
		return nil, fmt.Errorf("%w: weights must sum to a positive value", ErrInvalidRunInput)
	}
	for _, key := range requiredWeightKeys {
		if weights[key] <= 0 {
			return nil, fmt.Errorf("%w: weights[%s] must be positive for the solver objective", ErrInvalidRunInput, key)
		}
	}
	for key, weight := range weights {
		weights[key] = weight / sum
	}
	return weights, nil
}

func runConstraints(input map[string]any) (runinput.Constraints, error) {
	constraints := runinput.Constraints{MaxUtilizationRatio: 0.95}
	raw, ok := input["constraints"].(map[string]any)
	if !ok {
		return constraints, nil
	}
	var err error
	constraints.ForceInclude, err = idList(raw, "force_include")
	if err != nil {
		return runinput.Constraints{}, err
	}
	constraints.ForceExclude, err = idList(raw, "force_exclude")
	if err != nil {
		return runinput.Constraints{}, err
	}
	constraints.FreezeMembers, err = idList(raw, "freeze_members")
	if err != nil {
		return runinput.Constraints{}, err
	}
	constraints.MaxTeamSize = intOption(raw, "max_team_size", constraints.MaxTeamSize)
	constraints.MinTeamSize = intOption(raw, "min_team_size", constraints.MinTeamSize)
	constraints.MinBusFactor = intOption(raw, "min_bus_factor", constraints.MinBusFactor)
	constraints.BudgetCapUnits = floatOption(raw, "budget_cap_units", constraints.BudgetCapUnits)
	constraints.MaxUtilizationRatio = floatOption(raw, "max_utilization_ratio", constraints.MaxUtilizationRatio)
	if deadline, ok := raw["deadline_week"].(string); ok && deadline != "" {
		if !domain.ValidateWeekKey(deadline) {
			return runinput.Constraints{}, fmt.Errorf("%w: deadline_week %q is not an ISO week key", ErrInvalidRunInput, deadline)
		}
		constraints.DeadlineWeek = deadline
	}
	if forbid, ok := raw["forbid_external"].(bool); ok {
		constraints.ForbidExternal = forbid
	}
	return constraints, nil
}

func idList(raw map[string]any, key string) ([]string, error) {
	value, ok := raw[key]
	if !ok || value == nil {
		return []string{}, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: constraints.%s must be an array", ErrInvalidRunInput, key)
	}
	out := make([]string, 0, len(list))
	for i, entry := range list {
		id, ok := entry.(string)
		if !ok || !domain.EmployeeIDRe.MatchString(id) {
			return nil, fmt.Errorf("%w: constraints.%s[%d] is not an employee id", ErrInvalidRunInput, key, i)
		}
		out = append(out, id)
	}
	return out, nil
}

func runK(input map[string]any) int {
	k := intOption(input, "k", 2)
	if k < 2 {
		return 2 // contracts OptimizeRequest.k: the solver returns 2..3 variants
	}
	if k > 3 {
		return 3
	}
	return k
}

func runTimeLimit(input map[string]any) int {
	limit := intOption(input, "time_limit_ms", 10000)
	if limit < 1000 {
		return 1000
	}
	if limit > 60000 {
		return 60000
	}
	return limit
}

func intOption(raw map[string]any, key string, fallback int) int {
	switch value := raw[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	}
	return fallback
}

func floatOption(raw map[string]any, key string, fallback float64) float64 {
	switch value := raw[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	}
	return fallback
}

func int64Option(raw map[string]any, key string, fallback int64) int64 {
	switch value := raw[key].(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	}
	return fallback
}

// runPayload shapes the authoritative result as contracts TeamRun. Every number comes from
// internal/metrics; the solver's advisory metrics are not part of the payload.
func runPayload(
	runID, briefID string,
	data runinput.Data,
	variants []metrics.VariantResult,
	set metrics.VariantSetDiagnostics,
	resp optimizeResponse,
	validatorRejected int,
	createdAt time.Time,
) map[string]any {
	horizon := data.Horizon
	variantPayload := make([]any, 0, len(variants))
	status := "succeeded"
	if len(variants) == 0 {
		status = "failed"
	}
	for _, variant := range variants {
		if variant.DeficitCount > 0 {
			status = "partial"
		}
		variantPayload = append(variantPayload, map[string]any{
			"variant_id":     variant.VariantID,
			"label":          variant.Label,
			"gate_pass":      variant.GatePass,
			"gate":           variant.Gate,
			"score":          variant.Metrics.Score,
			"team_size":      variant.TeamSize,
			"duration":       variant.Duration,
			"cost_units":     variant.CostUnits,
			"metric_values":  variant.Metrics,
			"metric_details": variant.Details,
			"deficit_count":  variant.DeficitCount,
			"reason_codes":   []string{},
		})
	}
	if validatorRejected > 0 && status == "succeeded" {
		status = "partial"
	}
	return map[string]any{
		"run_id":        runID,
		"brief_id":      briefID,
		"status":        status,
		"preset":        data.Preset,
		"k":             data.K,
		"seed":          data.Seed,
		"time_limit_ms": data.TimeLimitMS,
		"horizon": map[string]any{
			"start_week": horizon[0],
			"end_week":   horizon[len(horizon)-1],
			"weeks":      horizon,
		},
		"created_at":  createdAt,
		"finished_at": time.Now().UTC(),
		"diagnostics": map[string]any{
			"solver_status":      resp.solverStatus(),
			"solve_time_ms":      resp.Solver.WallTimeMS,
			"ml_status":          "ok",
			"ml_attempts":        1,
			"validator_rejected": validatorRejected,
			"fallback_used":      false,
			"metrics_version":    metrics.Version,
			"contract_version":   "v1",
		},
		"variants":     variantPayload,
		"variant_set":  set,
		"reason_codes": append([]string{}, resp.ReasonCodes...),
	}
}

type DecisionInput struct {
	VariantID    string           `json:"variant_id"`
	DecidedBy    string           `json:"decided_by"`
	Comment      string           `json:"comment,omitempty"`
	EditsApplied []map[string]any `json:"edits_applied,omitempty"`
}

type DecisionResponse struct {
	RunID        string    `json:"run_id"`
	VariantID    string    `json:"variant_id"`
	DecidedAt    time.Time `json:"decided_at"`
	DecidedBy    string    `json:"decided_by"`
	EditsCount   int       `json:"edits_count"`
	AuditEventID string    `json:"audit_event_id"`
}

func (s *WorkflowService) RecordDecision(runID string, input DecisionInput) (*DecisionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, ok := s.runs[runID]
	if !ok {
		return nil, ErrRunNotFound
	}
	if _, exists := s.decisions[runID]; exists {
		return nil, ErrDecisionExists
	}
	if input.VariantID == "" {
		return nil, fmt.Errorf("%w: variant_id is required", ErrInvalidRunInput)
	}
	variants, _ := run["variants"].([]map[string]any)
	found := false
	for _, v := range variants {
		if v["variant_id"] == input.VariantID {
			found = true
			break
		}
	}
	if !found && s.variants[runID] != nil {
		if _, ok := s.variants[runID][input.VariantID]; ok {
			found = true
		}
	}
	if !found {
		return nil, ErrVariantNotFound
	}

	decidedBy := input.DecidedBy
	if decidedBy == "" {
		decidedBy = "system"
	}
	s.next++
	auditID := fmt.Sprintf("aud_%04d", s.next)
	now := time.Now().UTC()

	resp := &DecisionResponse{
		RunID:        runID,
		VariantID:    input.VariantID,
		DecidedAt:    now,
		DecidedBy:    decidedBy,
		EditsCount:   len(input.EditsApplied),
		AuditEventID: auditID,
	}
	s.decisions[runID] = map[string]any{
		"run_id":         runID,
		"variant_id":     input.VariantID,
		"decided_at":     now,
		"decided_by":     decidedBy,
		"edits_count":    len(input.EditsApplied),
		"audit_event_id": auditID,
		"comment":        input.Comment,
	}
	return resp, nil
}

func (s *WorkflowService) GetDecision(runID string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.decisions[runID]
	return d, ok
}
