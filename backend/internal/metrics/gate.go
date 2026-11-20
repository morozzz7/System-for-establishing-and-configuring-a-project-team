package metrics

import (
	"sort"

	"github.com/teamopt/backend/internal/domain"
)

// computeGate independently recomputes the hard-constraint gate G1-G5 of
// docs/METRICS.md Level 0. It never relaxes a constraint: a violated gate makes the
// variant non-existent for the user and forces Score = 0.
//
// internal/validator remains the rejecting seam of the pipeline (it produces the
// reason codes and details served to users); this package recomputes the same hard
// facts because Gate is a factor of the composite score and because M5 is defined over
// gate-passing variants only.
func (c *variantComputation) computeGate() {
	gate := GateViolations{}
	horizon := make(map[string]bool, len(c.snapshot.Horizon()))
	for _, week := range c.snapshot.Horizon() {
		horizon[week] = true
	}

	// ---------- G5: data integrity ----------
	knownRequirement := make(map[string]bool, len(c.requirements))
	for _, r := range c.requirements {
		knownRequirement[r.RequirementID] = true
	}
	for _, m := range c.proposal.Members {
		if !domain.EmployeeIDRe.MatchString(m.EmployeeID) {
			gate.G5++
			continue
		}
		if _, ok := c.candidates[m.EmployeeID]; !ok {
			gate.G5++
		}
		if m.RoleID != "" && !domain.RoleIDRe.MatchString(m.RoleID) {
			gate.G5++
		}
	}
	for _, a := range c.proposal.Assignments {
		if !domain.EmployeeIDRe.MatchString(a.EmployeeID) {
			gate.G5++
		} else if _, ok := c.candidates[a.EmployeeID]; !ok {
			gate.G5++
		}
		if !knownRequirement[a.RequirementID] {
			gate.G5++
		}
		if !domain.ValidateWeekKey(a.Week) || !horizon[a.Week] {
			gate.G5++
		}
	}

	// ---------- G1: assignment during absence/unavailability ----------
	seenPersonWeek := make(map[string]bool)
	for _, a := range c.proposal.Assignments {
		if a.Hours <= 0 {
			continue
		}
		row, ok := c.capacityRow(a.EmployeeID, a.Week)
		if !ok {
			continue // unknown employee/week is a G5 defect
		}
		if !row.Unavailable && row.FreeHours > 0 {
			continue
		}
		key := a.EmployeeID + "|" + a.Week
		if seenPersonWeek[key] {
			continue
		}
		seenPersonWeek[key] = true
		gate.G1++
	}

	// ---------- G2: overload ----------
	maxOverloadPct := 0.0
	members := make([]string, 0, len(c.empWeekHours))
	for empID := range c.empWeekHours {
		members = append(members, empID)
	}
	sort.Strings(members)
	for _, empID := range members {
		for week, hours := range c.empWeekHours[empID] {
			row, ok := c.capacityRow(empID, week)
			if !ok {
				continue // unknown week: counted in G5
			}
			if row.Unavailable || row.FreeHours <= 0 {
				continue // fully unavailable weeks are G1, never double-counted
			}
			if hours <= row.FreeHours {
				continue
			}
			gate.G2++
			limit := row.NormHours
			if limit <= 0 {
				limit = row.FreeHours
			}
			if limit > 0 {
				if pct := (hours - row.FreeHours) / limit * 100; pct > maxOverloadPct {
					maxOverloadPct = pct
				}
			}
		}
	}
	if gate.G2 > 0 {
		gate.MaxOverloadPct = round6(maxOverloadPct)
	}

	// ---------- G3: every must requirement covered or declared a deficit ----------
	// Effort basis: min_person_days, the conservative lower bound (ADR-007 §1). A
	// variant that covers min but not likely stays feasible and reports the deficit.
	declared := c.declaredDeficits()
	for _, req := range c.requirements {
		if !req.IsMust() {
			continue
		}
		if c.coveredPersonDays[req.RequirementID] >= req.Effort.MinPersonDays {
			continue
		}
		if declared[req.RequirementID] {
			continue
		}
		gate.G3++
		gate.UncoveredMustNotInDeficit++
	}

	// ---------- G4: user constraints ----------
	memberSet := c.memberSet
	for _, empID := range c.snapshot.Constraints().ForceInclude {
		if !memberSet[empID] {
			gate.G4++
		}
	}
	for _, empID := range c.snapshot.Constraints().FreezeMembers {
		if !memberSet[empID] {
			gate.G4++
		}
	}
	for _, empID := range c.snapshot.Constraints().ForceExclude {
		if memberSet[empID] {
			gate.G4++
		}
	}
	constraints := c.snapshot.Constraints()
	teamSize := len(c.members)
	if constraints.MaxTeamSize > 0 && teamSize > constraints.MaxTeamSize {
		gate.G4++
	}
	if constraints.MinTeamSize > 0 && teamSize < constraints.MinTeamSize {
		gate.G4++
	}
	if constraints.BudgetCapUnits > 0 && c.cost > constraints.BudgetCapUnits {
		gate.G4++
	}

	c.gate = gate
}

// declaredDeficits returns the requirement ids the solver declared as deficits. Go's
// own deficit list (M2) is an output, not an excuse: a variant cannot pass G3 just
// because Go would report the gap it created.
func (c *variantComputation) declaredDeficits() map[string]bool {
	out := make(map[string]bool, len(c.proposal.MLDeficits))
	for _, d := range c.proposal.MLDeficits {
		if len(d.RequirementIDs) > 0 {
			for _, id := range d.RequirementIDs {
				out[id] = true
			}
			continue
		}
		// A role/competency level deficit without explicit ids declares every
		// requirement it can describe.
		for _, req := range c.requirements {
			switch {
			case d.CompetencyID != nil && req.CompetencyID != nil && *d.CompetencyID == *req.CompetencyID:
				out[req.RequirementID] = true
			case d.CompetencyID == nil && d.RoleID != "" && d.RoleID == req.RoleID:
				out[req.RequirementID] = true
			}
		}
	}
	return out
}
