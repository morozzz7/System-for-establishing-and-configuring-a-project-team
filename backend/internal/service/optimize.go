package service

// This file is the typed view of the ML optimize contract (contracts/ml-openapi.yaml) and
// the conversion layer between the immutable run snapshot (internal/runinput), the solver
// response, the independent validator (internal/validator) and the authoritative metrics
// (internal/metrics).
//
// Typed decoding is deliberate: the ML response is advisory (solver proposes, Go verifies),
// so a schema drift must surface as a decode error instead of a silently empty variant
// list. The advisory `metrics` block of every variant is intentionally not decoded - Go
// recomputes M1-M12 itself.

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
	"github.com/teamopt/backend/internal/validator"
)

// ErrInvalidRunInput marks a run request that cannot be turned into a valid snapshot
// (bad brief requirements, unknown preset, weights that cannot form an objective, ...).
// The HTTP layer maps it to 400 instead of the 502 of a solver failure.
var ErrInvalidRunInput = errors.New("invalid run input")

// ---------- request ----------

type optimizeEffort struct {
	MinPersonDays    float64 `json:"min_person_days"`
	LikelyPersonDays float64 `json:"likely_person_days"`
	MaxPersonDays    float64 `json:"max_person_days"`
	Unit             string  `json:"unit"`
}

type optimizeRequirement struct {
	RequirementID  string         `json:"requirement_id"`
	RoleID         string         `json:"role_id"`
	CompetencyID   *string        `json:"competency_id"`
	Level          int            `json:"level"`
	Criticality    string         `json:"criticality"`
	Headcount      int            `json:"headcount"`
	Effort         optimizeEffort `json:"effort"`
	AllowsExternal bool           `json:"allows_external"`
}

type optimizeHorizon struct {
	StartWeek string   `json:"start_week"`
	EndWeek   string   `json:"end_week"`
	Weeks     []string `json:"weeks"`
}

type optimizeCompetency struct {
	CompetencyID string  `json:"competency_id"`
	Level        int     `json:"level"`
	Confidence   float64 `json:"confidence"`
}

type optimizeCapacityWeek struct {
	Week        string  `json:"week"`
	WorkingDays int     `json:"working_days"`
	FreeHours   float64 `json:"free_hours"`
	Unavailable bool    `json:"unavailable"`
}

type optimizeCandidate struct {
	EmployeeID        string                 `json:"employee_id"`
	Grade             string                 `json:"grade"`
	RoleProfileID     string                 `json:"role_profile_id"`
	EmploymentType    string                 `json:"employment_type"`
	BenchFlag         bool                   `json:"bench_flag"`
	CostCoefficient   float64                `json:"cost_coefficient"`
	ExperienceScore   *float64               `json:"experience_score,omitempty"`
	Competencies      []optimizeCompetency   `json:"competencies"`
	Capacity          []optimizeCapacityWeek `json:"capacity"`
	CurrentProjectIDs []string               `json:"current_project_ids"`
}

type optimizeConstraints struct {
	ForceInclude        []string `json:"force_include"`
	ForceExclude        []string `json:"force_exclude"`
	MaxTeamSize         int      `json:"max_team_size,omitempty"`
	MinTeamSize         int      `json:"min_team_size,omitempty"`
	BudgetCapUnits      float64  `json:"budget_cap_units,omitempty"`
	DeadlineWeek        string   `json:"deadline_week,omitempty"`
	ForbidExternal      bool     `json:"forbid_external"`
	MinBusFactor        int      `json:"min_bus_factor,omitempty"`
	MaxUtilizationRatio float64  `json:"max_utilization_ratio"`
	FreezeMembers       []string `json:"freeze_members"`
}

type optimizeOptions struct {
	NumSearchWorkers  int    `json:"num_search_workers"`
	Deterministic     bool   `json:"deterministic"`
	AllowDeficits     bool   `json:"allow_deficits"`
	ObjectivePreset   string `json:"objective_preset"`
	LogSearchProgress bool   `json:"log_search_progress"`
}

type optimizeRequest struct {
	RequestID           string                `json:"request_id"`
	RunID               string                `json:"run_id"`
	Seed                int64                 `json:"seed"`
	TimeLimitMS         int                   `json:"time_limit_ms"`
	K                   int                   `json:"k"`
	MinJaccardDiversity float64               `json:"min_jaccard_diversity"`
	Horizon             optimizeHorizon       `json:"horizon"`
	Weights             map[string]float64    `json:"weights"`
	Requirements        []optimizeRequirement `json:"requirements"`
	Candidates          []optimizeCandidate   `json:"candidates"`
	Constraints         optimizeConstraints   `json:"constraints"`
	Options             optimizeOptions       `json:"options"`
}

// ---------- response ----------

type optimizeSolver struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	WallTimeMS    int    `json:"wall_time_ms"`
	NumWorkers    int    `json:"num_workers"`
	Seed          int64  `json:"seed"`
	Deterministic bool   `json:"deterministic"`
}

type optimizeMember struct {
	EmployeeID      string   `json:"employee_id"`
	RoleID          string   `json:"role_id"`
	AllocatedHours  float64  `json:"allocated_hours"`
	AssignmentShare float64  `json:"assignment_share"`
	ReasonCodes     []string `json:"reason_codes"`
}

type optimizeAssignment struct {
	EmployeeID    string  `json:"employee_id"`
	RequirementID string  `json:"requirement_id"`
	Week          string  `json:"week"`
	Hours         float64 `json:"hours"`
}

type optimizeDeficit struct {
	CompetencyID            *string  `json:"competency_id"`
	RoleID                  *string  `json:"role_id"`
	RequirementIDs          []string `json:"requirement_ids"`
	PersonDays              float64  `json:"person_days"`
	ExternalCooperationFlag bool     `json:"external_cooperation_flag"`
	ReasonCodes             []string `json:"reason_codes"`
}

// optimizeVariant is one solver proposal. The advisory `metrics` block of the contract is
// not modelled: the field is dropped on decode and never reaches the user.
type optimizeVariant struct {
	VariantID               string               `json:"variant_id"`
	Label                   string               `json:"label"`
	Members                 []optimizeMember     `json:"members"`
	Assignments             []optimizeAssignment `json:"assignments"`
	Deficits                []optimizeDeficit    `json:"deficits"`
	UnscheduledRequirements []string             `json:"unscheduled_requirements"`
	ReasonCodes             []string             `json:"reason_codes"`
}

type optimizeResponse struct {
	RequestID                string            `json:"request_id"`
	RunID                    string            `json:"run_id"`
	Solver                   optimizeSolver    `json:"solver"`
	Variants                 []optimizeVariant `json:"variants"`
	DeterministicFingerprint string            `json:"deterministic_fingerprint"`
	Diagnostics              map[string]any    `json:"diagnostics"`
	ReasonCodes              []string          `json:"reason_codes"`
}

// solverStatuses is the closed set of contracts SolverStatus values.
var solverStatuses = map[string]bool{
	"OPTIMAL": true, "FEASIBLE": true, "INFEASIBLE": true,
	"UNKNOWN": true, "TIMEOUT": true, "MODEL_INVALID": true, "ERROR": true,
}

// solverStatus sanitizes the solver status reported by the ML service.
func (r optimizeResponse) solverStatus() string {
	if solverStatuses[r.Solver.Status] {
		return r.Solver.Status
	}
	return "UNKNOWN"
}

// ---------- snapshot -> optimize request ----------

// minJaccardDiversity is the roster diversity floor sent to the solver
// (contracts/reference.yaml variant_set_rules.min_jaccard_diversity). The same rule is
// re-checked by internal/metrics after the fact.
const minJaccardDiversity = 0.25

// buildOptimizeRequest derives the ML request from the immutable snapshot, so the request
// the solver saw is exactly the request the metrics are recomputed against.
func buildOptimizeRequest(snap *runinput.Snapshot, requestID string) (optimizeRequest, error) {
	horizon := snap.Horizon()
	if len(horizon) == 0 {
		return optimizeRequest{}, fmt.Errorf("%w: the snapshot horizon is empty", ErrInvalidRunInput)
	}
	req := optimizeRequest{
		RequestID:           requestID,
		RunID:               snap.RunID(),
		Seed:                snap.Seed(),
		TimeLimitMS:         snap.TimeLimitMS(),
		K:                   snap.K(),
		MinJaccardDiversity: minJaccardDiversity,
		Horizon: optimizeHorizon{
			StartWeek: horizon[0],
			EndWeek:   horizon[len(horizon)-1],
			Weeks:     append([]string(nil), horizon...),
		},
		Weights:      snap.Weights(),
		Requirements: make([]optimizeRequirement, 0, len(snap.Requirements())),
		Candidates:   make([]optimizeCandidate, 0, len(snap.Candidates())),
	}

	for _, r := range snap.Requirements() {
		effort := r.Effort
		// contracts/ml-openapi.yaml EffortRange: 0.5 <= person-days <= 2000. A smaller
		// estimate is a broken extraction, not something to round silently.
		if effort.MinPersonDays < 0.5 || effort.MaxPersonDays > 2000 {
			return optimizeRequest{}, fmt.Errorf(
				"%w: requirement %s effort %.2f..%.2f person-days is outside the solver range 0.5..2000",
				ErrInvalidRunInput, r.RequirementID, effort.MinPersonDays, effort.MaxPersonDays)
		}
		req.Requirements = append(req.Requirements, optimizeRequirement{
			RequirementID: r.RequirementID,
			RoleID:        r.RoleID,
			CompetencyID:  r.CompetencyID,
			Level:         r.Level,
			Criticality:   r.Criticality,
			Headcount:     r.Headcount,
			Effort: optimizeEffort{
				MinPersonDays:    effort.MinPersonDays,
				LikelyPersonDays: effort.LikelyPersonDays,
				MaxPersonDays:    effort.MaxPersonDays,
				Unit:             "person_days",
			},
			AllowsExternal: r.AllowsExternal,
		})
	}

	for _, c := range snap.Candidates() {
		competencies := make([]optimizeCompetency, 0, len(c.CompetencyLevels))
		ids := make([]string, 0, len(c.CompetencyLevels))
		for id := range c.CompetencyLevels {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			competencies = append(competencies, optimizeCompetency{
				CompetencyID: id, Level: c.CompetencyLevels[id], Confidence: 1,
			})
		}
		weeks := make([]optimizeCapacityWeek, 0, len(horizon))
		for _, row := range c.Capacity {
			weeks = append(weeks, optimizeCapacityWeek{
				Week: row.Week, WorkingDays: row.WorkingDays,
				FreeHours: row.FreeHours, Unavailable: row.Unavailable || row.FreeHours <= 0,
			})
		}
		projects := append([]string(nil), c.CurrentProjectIDs...)
		if projects == nil {
			projects = []string{}
		}
		req.Candidates = append(req.Candidates, optimizeCandidate{
			EmployeeID:        c.EmployeeID,
			Grade:             c.Grade,
			RoleProfileID:     c.RoleProfileID,
			EmploymentType:    c.EmploymentType,
			BenchFlag:         c.BenchFlag,
			CostCoefficient:   c.CostCoefficient,
			ExperienceScore:   c.ExperienceScore,
			Competencies:      competencies,
			Capacity:          weeks,
			CurrentProjectIDs: projects,
		})
	}

	cons := snap.Constraints()
	maxUtilization := cons.MaxUtilizationRatio
	if maxUtilization <= 0 {
		maxUtilization = 0.95
	}
	forceInclude := append([]string{}, cons.ForceInclude...)
	forceExclude := append([]string{}, cons.ForceExclude...)
	freeze := append([]string{}, cons.FreezeMembers...)
	req.Constraints = optimizeConstraints{
		ForceInclude:        forceInclude,
		ForceExclude:        forceExclude,
		MaxTeamSize:         cons.MaxTeamSize,
		MinTeamSize:         cons.MinTeamSize,
		BudgetCapUnits:      cons.BudgetCapUnits,
		DeadlineWeek:        cons.DeadlineWeek,
		ForbidExternal:      cons.ForbidExternal,
		MinBusFactor:        cons.MinBusFactor,
		MaxUtilizationRatio: maxUtilization,
		FreezeMembers:       freeze,
	}
	req.Options = optimizeOptions{
		NumSearchWorkers:  1,
		Deterministic:     true,
		AllowDeficits:     true,
		ObjectivePreset:   snap.Preset(),
		LogSearchProgress: false,
	}
	return req, nil
}

// ---------- solver response -> metrics proposals ----------

// proposalsFromResponse turns the decoded solver variants into the metrics package's
// proposal type. Member grades come from the snapshot, never from the solver.
func proposalsFromResponse(resp optimizeResponse, snap *runinput.Snapshot) []metrics.Proposal {
	proposals := make([]metrics.Proposal, 0, len(resp.Variants))
	for _, v := range resp.Variants {
		proposal := metrics.Proposal{
			VariantID:   v.VariantID,
			Label:       v.Label,
			Members:     make([]metrics.ProposalMember, 0, len(v.Members)),
			Assignments: make([]metrics.ProposalAssignment, 0, len(v.Assignments)),
			MLDeficits:  make([]metrics.MLDeficit, 0, len(v.Deficits)),
		}
		for _, m := range v.Members {
			grade := ""
			if cand, ok := snap.CandidateByID(m.EmployeeID); ok {
				grade = cand.Grade
			}
			proposal.Members = append(proposal.Members, metrics.ProposalMember{
				EmployeeID: m.EmployeeID, RoleID: m.RoleID, Grade: grade,
			})
		}
		for _, a := range v.Assignments {
			proposal.Assignments = append(proposal.Assignments, metrics.ProposalAssignment{
				EmployeeID: a.EmployeeID, RequirementID: a.RequirementID,
				Week: a.Week, Hours: a.Hours,
			})
		}
		for _, d := range v.Deficits {
			proposal.MLDeficits = append(proposal.MLDeficits, metrics.MLDeficit{
				CompetencyID:   d.CompetencyID,
				RoleID:         derefString(d.RoleID),
				RequirementIDs: append([]string(nil), d.RequirementIDs...),
				PersonDays:     d.PersonDays,
			})
		}
		proposals = append(proposals, proposal)
	}
	return proposals
}

// ---------- validator input ----------

// buildValidatorInput snapshots everything the independent G1-G5 check needs. The
// validator is the rejecting seam: a variant that fails it never reaches the user.
func buildValidatorInput(snap *runinput.Snapshot) validator.ValidateInput {
	constraints := snap.Constraints()
	in := validator.ValidateInput{
		Requirements: make([]validator.Requirement, 0, len(snap.Requirements())),
		Candidates:   make([]validator.RunInputCandidate, 0, len(snap.Candidates())),
		Constraints: validator.RunConstraints{
			ForceInclude:   append([]string(nil), constraints.ForceInclude...),
			ForceExclude:   append([]string(nil), constraints.ForceExclude...),
			MaxTeamSize:    constraints.MaxTeamSize,
			MinTeamSize:    constraints.MinTeamSize,
			BudgetCapUnits: constraints.BudgetCapUnits,
			FreezeMembers:  append([]string(nil), constraints.FreezeMembers...),
		},
		CapacityMatrix: make(map[string][]domain.WeeklyCapacity, len(snap.Candidates())),
		Horizon:        append([]string(nil), snap.Horizon()...),
	}
	for _, r := range snap.Requirements() {
		competencyID := ""
		if r.CompetencyID != nil {
			competencyID = *r.CompetencyID
		}
		in.Requirements = append(in.Requirements, validator.Requirement{
			RequirementID: r.RequirementID,
			RoleID:        r.RoleID,
			CompetencyID:  competencyID,
			Level:         r.Level,
			Criticality:   r.Criticality,
			Effort: validator.RequirementEffort{
				MinPersonDays:    r.Effort.MinPersonDays,
				LikelyPersonDays: r.Effort.LikelyPersonDays,
				MaxPersonDays:    r.Effort.MaxPersonDays,
			},
		})
	}
	for _, c := range snap.Candidates() {
		levels := make(map[string]int, len(c.CompetencyLevels))
		for id, level := range c.CompetencyLevels {
			levels[id] = level
		}
		in.Candidates = append(in.Candidates, validator.RunInputCandidate{
			EmployeeID: c.EmployeeID, Grade: c.Grade, RoleProfileID: c.RoleProfileID,
			CostCoefficient: c.CostCoefficient, CompetencyLevels: levels,
		})
		weeks := make([]domain.WeeklyCapacity, 0, len(c.Capacity))
		for _, row := range c.Capacity {
			norm := int(math.Round(row.NormHours * 60))
			assigned := int(math.Round(row.AssignedHours * 60))
			absent := int(math.Round(row.AbsentHours * 60))
			available := int(math.Round(row.FreeHours * 60))
			if available < 0 {
				available = 0
			}
			ratio := 0.0
			if norm > 0 {
				ratio = float64(assigned+absent) / float64(norm)
			}
			weeks = append(weeks, domain.WeeklyCapacity{
				EmployeeID: c.EmployeeID, Week: row.Week,
				AvailableMinutes: available, AssignedMinutes: assigned,
				AbsentMinutes: absent, NormMinutes: norm, UtilizationRatio: ratio,
			})
		}
		in.CapacityMatrix[c.EmployeeID] = weeks
	}
	return in
}

// variantForValidator builds the validator's view of one proposal.
func variantForValidator(proposal metrics.Proposal, runID string, resp optimizeResponse) validator.TeamVariant {
	var reasonCodes []string
	for _, v := range resp.Variants {
		if v.VariantID == proposal.VariantID {
			reasonCodes = append([]string(nil), v.ReasonCodes...)
			break
		}
	}
	hoursByMember := make(map[string]float64, len(proposal.Members))
	for _, a := range proposal.Assignments {
		if a.Hours > 0 {
			hoursByMember[a.EmployeeID] += a.Hours
		}
	}
	out := validator.TeamVariant{
		VariantID:         proposal.VariantID,
		RunID:             runID,
		Members:           make([]validator.VariantMember, 0, len(proposal.Members)),
		WeeklyAssignments: make([]validator.WeeklyAssignment, 0, len(proposal.Assignments)),
		Deficits:          make([]validator.VariantDeficit, 0, len(proposal.MLDeficits)),
		ReasonCodes:       reasonCodes,
	}
	for _, m := range proposal.Members {
		allocated := hoursByMember[m.EmployeeID]
		share := 0.0
		if allocated > 0 {
			share = 1
		}
		out.Members = append(out.Members, validator.VariantMember{
			EmployeeID: m.EmployeeID, RoleID: m.RoleID, Grade: m.Grade,
			AllocatedHours: allocated, AssignmentShare: share,
		})
	}
	for _, a := range proposal.Assignments {
		out.WeeklyAssignments = append(out.WeeklyAssignments, validator.WeeklyAssignment{
			EmployeeID: a.EmployeeID, RequirementID: a.RequirementID,
			Week: a.Week, Hours: a.Hours,
		})
	}
	// The solver's declared deficits are the only ones G3 accepts: Go-computed deficits
	// are an output of the metrics package, not an input of the gate.
	for _, d := range proposal.MLDeficits {
		for _, reqID := range d.RequirementIDs {
			out.Deficits = append(out.Deficits, validator.VariantDeficit{
				RequirementID: reqID, PersonDays: d.PersonDays,
			})
		}
	}
	return out
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
