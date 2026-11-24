package service

// Contract-shaped recommended actions for deficits (contracts/openapi.yaml). Every
// field is derived from snapshot facts or a documented formula; optional fields are
// pointers so an unsourced value is omitted instead of being guessed.

import (
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// ActionType values of contracts/openapi.yaml.
const (
	actionContractor   = "contractor"
	actionHire         = "hire"
	actionMoveDeadline = "move_deadline"
	actionUpskill      = "upskill"
	actionReallocate   = "reallocate"
)

// RecommendedAction mirrors contracts RecommendedAction. The Go pipeline has no
// generated DTOs yet and no existing Go type matched, so the minimal contract-shaped
// struct lives in this file. It is wire-compatible with the contract schema.
type RecommendedAction struct {
	Action             string   `json:"action"`
	EstimatedCostUnits *float64 `json:"estimated_cost_units,omitempty"`
	TimeToStartWeeks   *float64 `json:"time_to_start_weeks,omitempty"`
	FromProjectID      string   `json:"from_project_id,omitempty"`
	ReasonCodes        []string `json:"reason_codes"`
}

// BuildDeficitActions maps authoritative facts to the actions of METRICS.md Level 2.
//
// Decision rules:
//   - No candidate of the snapshot has free capacity: external cooperation. A must gap
//     can be filled by a contractor or a hire; moving the deadline is an option when the
//     run has a deadline. External actions are suppressed by constraints.forbid_external
//     or when the requirement does not allow external cooperation.
//   - Free capacity exists: close the gap internally by upskilling or reallocating. The
//     reallocate action names the first (canonical order) current project of a qualified
//     candidate who has free capacity.
//
// Cost model (documented assumption): estimated_cost_units = missing person-days *
// metrics.HoursPerDay * grade cost coefficient of the relevant snapshot candidate. For
// external actions the reference is the lowest coefficient among candidates qualified
// for the requirement. The coefficient is an internal-grade proxy, not a vendor rate;
// when no qualified candidate exists the cost field is omitted. time_to_start_weeks has
// no documented sourcing rule, so it is always omitted.
func BuildDeficitActions(d metrics.Deficit, snap *runinput.Snapshot, result metrics.VariantResult) []RecommendedAction {
	if snap == nil {
		return nil
	}
	req, hasReq := snap.RequirementByID(d.RequirementID)
	must := hasReq && req.IsMust()

	qualified := make([]runinput.Candidate, 0, len(snap.Candidates()))
	var referenceCost *float64
	for _, cand := range snap.Candidates() { // canonical employee-id order
		if !hasReq || !candidateQualified(cand, req) {
			continue
		}
		if cand.CostCoefficient <= 0 {
			continue
		}
		qualified = append(qualified, cand)
	}
	if len(qualified) > 0 {
		lowest := qualified[0].CostCoefficient
		for _, cand := range qualified[1:] {
			if cand.CostCoefficient < lowest {
				lowest = cand.CostCoefficient
			}
		}
		referenceCost = actionCost(d.PersonDays, lowest)
	}

	var capacityCandidate *runinput.Candidate
	var reallocateFrom *runinput.Candidate
	for _, cand := range snap.Candidates() {
		if !hasFreeCapacity(cand) {
			continue
		}
		if capacityCandidate == nil {
			copy := cand
			capacityCandidate = &copy
		}
		if reallocateFrom == nil && hasReq && candidateQualified(cand, req) && len(cand.CurrentProjectIDs) > 0 {
			copy := cand
			reallocateFrom = &copy
		}
	}

	actions := make([]RecommendedAction, 0, 3)
	if capacityCandidate == nil {
		allowsExternal := hasReq && req.AllowsExternal && d.ExternalCooperationFlag &&
			!snap.Constraints().ForbidExternal
		externalCodes := []string{"DEFICIT_EXTERNAL_CONTRACTOR"}
		if must {
			externalCodes = append(externalCodes, "DEFICIT_MUST_ROLE")
		}
		if allowsExternal {
			actions = append(actions, recommended(actionContractor, externalCodes, referenceCost, ""))
			if must {
				actions = append(actions, recommended(actionHire, externalCodes, referenceCost, ""))
			}
		}
		if result.Duration.DeadlineWeek != "" {
			actions = append(actions, recommended(actionMoveDeadline, []string{"DEFICIT_DEADLINE_SHIFT"}, nil, ""))
		}
		return actions
	}

	actions = append(actions, recommended(actionUpskill,
		[]string{"DEFICIT_UPSKILL_INTERNAL"}, actionCost(d.PersonDays, capacityCandidate.CostCoefficient), ""))
	if reallocateFrom != nil {
		actions = append(actions, recommended(actionReallocate,
			[]string{"TRADEOFF_LOAD_VS_SPEED"}, actionCost(d.PersonDays, reallocateFrom.CostCoefficient),
			reallocateFrom.CurrentProjectIDs[0]))
	}
	return actions
}

func candidateQualified(cand runinput.Candidate, req runinput.Requirement) bool {
	if req.CompetencyID != nil {
		return cand.CompetencyLevel(*req.CompetencyID) >= req.Level
	}
	return cand.HasRole(req.RoleID)
}

func hasFreeCapacity(cand runinput.Candidate) bool {
	for _, row := range cand.Capacity {
		if !row.Unavailable && row.FreeHours > 0 {
			return true
		}
	}
	return false
}

func actionCost(personDays, coefficient float64) *float64 {
	if personDays <= 0 || coefficient <= 0 {
		return nil
	}
	value := round6(personDays * metrics.HoursPerDay * coefficient)
	return &value
}

func recommended(action string, codes []string, cost *float64, fromProjectID string) RecommendedAction {
	return RecommendedAction{
		Action:             action,
		EstimatedCostUnits: cost,
		FromProjectID:      fromProjectID,
		ReasonCodes:        append([]string(nil), codes...),
	}
}
