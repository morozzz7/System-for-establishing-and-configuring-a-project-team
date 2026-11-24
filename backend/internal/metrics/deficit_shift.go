package metrics

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/runinput"
	"github.com/teamopt/backend/internal/schedule"
)

// ErrNoCapacityForCounterfactual means the hypothetical hours that would close a deficit
// internally cannot be placed in any eligible team member's free capacity. Callers must
// omit the duration shift and explain the reason instead of inventing a number
// (AGENTS.md rule 4, docs/METRICS.md Level 2).
var ErrNoCapacityForCounterfactual = errors.New("metrics: no free capacity for deficit counterfactual")

// EstimateDeficitShift reruns the authoritative schedule engine twice with the same
// config and seed: once for the proposed assignments and once after placing the given
// extra hours as additional assignments. The returned durations are the before/after
// P50/P80; the duration shift of docs/METRICS.md Level 2 is
// max(0, base.P50Weeks - shifted.P50Weeks). Closing effort internally can only add weekly
// progress, so shifted is never later than base.
//
// Placement is deterministic: requirement ids are sorted, eligible members are the
// proposal's members in canonical (employee id) order, and weeks follow the snapshot
// horizon. Only a member qualified for the requirement (competency level >= L_r, or the
// role profile for a role-only requirement) may receive the extra hours, so the
// hypothetical work is actually deliverable. Hours are packed into the free capacity
// that remains after the proposal's own assignments; if it runs out, the counterfactual
// is undefined and ErrNoCapacityForCounterfactual is returned.
func EstimateDeficitShift(snap *runinput.Snapshot, proposal Proposal, extraHoursByRequirement map[string]float64, cfg ScheduleConfig) (base, shifted VariantDuration, err error) {
	if snap == nil {
		return VariantDuration{}, VariantDuration{}, fmt.Errorf("metrics: snapshot is required for deficit shift")
	}
	normalized := normalizeConfig(cfg, snap.Seed())
	c := newVariantComputation(snap, proposal, normalized)

	baseSchedule, err := c.scheduleFor(proposal.Assignments)
	if err != nil {
		return VariantDuration{}, VariantDuration{}, fmt.Errorf("metrics: base schedule for deficit shift: %w", err)
	}
	base = durationFor(c, baseSchedule)

	requirementIDs := make([]string, 0, len(extraHoursByRequirement))
	for id, hours := range extraHoursByRequirement {
		if hours > 0 {
			requirementIDs = append(requirementIDs, id)
		}
	}
	sort.Strings(requirementIDs)
	if len(requirementIDs) == 0 {
		return base, base, nil
	}

	remaining := remainingShiftCapacity(c)
	shiftedAssignments := append([]ProposalAssignment(nil), proposal.Assignments...)
	for _, requirementID := range requirementIDs {
		req, ok := c.requirement(requirementID)
		if !ok {
			return VariantDuration{}, VariantDuration{}, fmt.Errorf("metrics: deficit shift references unknown requirement %q", requirementID)
		}
		need := extraHoursByRequirement[requirementID]
		for _, employeeID := range c.members {
			if need <= 0 {
				break
			}
			if !c.qualified(employeeID, req) {
				continue
			}
			for _, week := range snap.Horizon() {
				if need <= 0 {
					break
				}
				free := remaining[employeeID][week]
				if free <= 0 {
					continue
				}
				hours := math.Min(need, free)
				shiftedAssignments = append(shiftedAssignments, ProposalAssignment{
					EmployeeID: employeeID, RequirementID: requirementID, Week: week, Hours: hours,
				})
				remaining[employeeID][week] -= hours
				need -= hours
			}
		}
		if need > 0 {
			return VariantDuration{}, VariantDuration{}, fmt.Errorf("%w: requirement %q lacks %v hours of qualified free capacity", ErrNoCapacityForCounterfactual, requirementID, need)
		}
	}

	shiftedSchedule, err := c.scheduleFor(shiftedAssignments)
	if err != nil {
		return VariantDuration{}, VariantDuration{}, fmt.Errorf("metrics: shifted schedule for deficit shift: %w", err)
	}
	shifted = durationFor(c, shiftedSchedule)
	return base, shifted, nil
}

// remainingShiftCapacity is the free capacity of each member-week after the proposal's
// own assignments. The schedule engine clamps assignment hours to FreeHours, so only this
// remainder can carry real extra progress.
func remainingShiftCapacity(c *variantComputation) map[string]map[string]float64 {
	used := make(map[string]float64)
	for _, a := range c.proposal.Assignments {
		if a.Hours > 0 {
			used[a.EmployeeID+"|"+a.Week] += a.Hours
		}
	}
	out := make(map[string]map[string]float64, len(c.members))
	for _, employeeID := range c.members {
		cand, ok := c.candidates[employeeID]
		if !ok {
			continue
		}
		for _, row := range cand.Capacity {
			if row.Unavailable || row.FreeHours <= 0 {
				continue
			}
			free := row.FreeHours - used[employeeID+"|"+row.Week]
			if free <= 0 {
				continue
			}
			if out[employeeID] == nil {
				out[employeeID] = make(map[string]float64)
			}
			out[employeeID][row.Week] = free
		}
	}
	return out
}

// durationFor reuses result() so the counterfactual durations carry exactly the same
// rounding, deadline, on-time and Monte Carlo metadata as the served Variant.duration.
func durationFor(c *variantComputation, result schedule.Result) VariantDuration {
	c.baseSchedule = result
	return c.result().Duration
}
