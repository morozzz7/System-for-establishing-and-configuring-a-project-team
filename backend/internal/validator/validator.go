// Package validator independently verifies team variants against hard constraints G1-G5.
// It is the "Go verifies" side of ADR-001: the ML optimizer proposes variants,
// and this package drops any variant that violates a hard gate before it reaches the user.
package validator

import (
	"fmt"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/domain"
)

// Reason codes produced by the validator. They are a subset of the codes defined in
// contracts/openapi.yaml and are never invented by an LLM.
const (
	reasonG1Unavailable       = "G1_UNAVAILABLE_ASSIGNED"
	reasonG2Overload          = "G2_OVERLOAD"
	reasonG3MustUncovered     = "G3_MUST_UNCOVERED"
	reasonG4ForceInclude      = "G4_FORCE_INCLUDE_VIOLATED"
	reasonG4ForceExclude      = "G4_FORCE_EXCLUDE_VIOLATED"
	reasonG4TeamSizeExceeded  = "G4_TEAM_SIZE_EXCEEDED"
	reasonG4BudgetExceeded    = "G4_BUDGET_EXCEEDED"
	reasonG5UnknownEmployee   = "G5_UNKNOWN_EMPLOYEE"
	reasonG5UnknownRequirement = "G5_UNKNOWN_REQUIREMENT"
	reasonG5InvalidWeek       = "G5_INVALID_WEEK"
	reasonG5InvalidRole       = "G5_INVALID_ROLE"
)

// Validator validates team variants. It has no mutable state and is safe for concurrent use.
type Validator struct{}

// New creates a Validator with default settings.
func New() *Validator {
	return &Validator{}
}

// Validate checks a variant against G1-G5. It returns a ValidationResult with counts,
// diagnostics and reason codes. An error is returned only for internal failures, never
// for a violating variant (the variant is simply marked GatePass=false).
func (v *Validator) Validate(input ValidateInput) (ValidationResult, error) {
	res := ValidationResult{
		Violations: GateViolations{},
		GatePass:   true,
	}

	// G5 first: unknown IDs or invalid weeks make G1/G2 lookups unreliable,
	// but we still run the other gates to collect a complete report.
	v.validateG5(input, &res)
	v.validateG1(input, &res)
	v.validateG2(input, &res)
	v.validateG3(input, &res)
	v.validateG4(input, &res)

	res.GatePass = res.Violations.G1 == 0 &&
		res.Violations.G2 == 0 &&
		res.Violations.G3 == 0 &&
		res.Violations.G4 == 0 &&
		res.Violations.G5 == 0 &&
		res.Violations.UncoveredMustNotInDeficit == 0

	// Deterministic ordering of reason codes and details for tests and stable responses.
	sort.Strings(res.ReasonCodes)
	sort.Strings(res.Details)

	return res, nil
}

// ---------- G1: unavailability ----------
// A person-week is unavailable when the capacity matrix reports zero available minutes
// for that week (absence or fully booked by prior work). Assigning any positive hours
// there is a G1 violation. Weeks that are merely overloaded (available > 0 but exceeded)
// are handled by G2. G1 is counted per person-week to match METRICS.md "person-weeks
// with a>0 while unavailable".
func (v *Validator) validateG1(input ValidateInput, res *ValidationResult) {
	caps := buildCapacityLookup(input.CapacityMatrix)

	seen := make(map[string]bool)
	for _, a := range input.Variant.WeeklyAssignments {
		if a.Hours <= 0 {
			continue
		}
		row, ok := caps.get(a.EmployeeID, a.Week)
		if !ok {
			// Unknown employee/week is counted in G5, not here.
			continue
		}
		if row.AvailableMinutes == 0 {
			key := a.EmployeeID + "|" + a.Week
			if seen[key] {
				continue
			}
			seen[key] = true
			res.Violations.G1++
			res.addReason(reasonG1Unavailable)
			res.addDetail(fmt.Sprintf("G1: %s assigned %.1fh in %s with 0 available minutes",
				a.EmployeeID, a.Hours, a.Week))
		}
	}
}

// ---------- G2: overload ----------
// For each (employee, week), sum the new variant assignment hours and check that the
// total does not exceed the free capacity reported by the capacity matrix.
func (v *Validator) validateG2(input ValidateInput, res *ValidationResult) {
	caps := buildCapacityLookup(input.CapacityMatrix)

	// Aggregate new assignment minutes per employee-week.
	type key struct {
		employeeID string
		week       string
	}
	newMinutes := make(map[key]int)
	for _, a := range input.Variant.WeeklyAssignments {
		if a.Hours <= 0 {
			continue
		}
		k := key{employeeID: a.EmployeeID, week: a.Week}
		newMinutes[k] += int(math.Round(a.Hours * 60))
	}

	maxOverloadPct := 0.0
	for k, added := range newMinutes {
		row, ok := caps.get(k.employeeID, k.week)
		if !ok {
			continue
		}
		if row.AvailableMinutes <= 0 {
			// Fully unavailable weeks are G1; do not double-count as G2.
			continue
		}
		if added > row.AvailableMinutes {
			res.Violations.G2++
			res.addReason(reasonG2Overload)
			effectiveLimit := row.AvailableMinutes + row.AssignedMinutes + row.AbsentMinutes
			excess := added - row.AvailableMinutes
			pct := overloadPct(excess, effectiveLimit)
			if pct > maxOverloadPct {
				maxOverloadPct = pct
			}
			res.addDetail(fmt.Sprintf(
				"G2: %s in %s assigned %d min but only %d min available (overload %.1f%%)",
				k.employeeID, k.week, added, row.AvailableMinutes, pct))
		}
	}

	if res.Violations.G2 > 0 {
		res.Violations.MaxOverloadPct = maxOverloadPct
	}
}

// ---------- G3: must-requirement coverage ----------
// Every must requirement must either be covered by assigned competent employees or
// explicitly listed as a deficit. Coverage uses the M2 formula from METRICS.md.
func (v *Validator) validateG3(input ValidateInput, res *ValidationResult) {
	reqByID := make(map[string]Requirement, len(input.Requirements))
	for _, r := range input.Requirements {
		reqByID[r.RequirementID] = r
	}

	candByID := make(map[string]RunInputCandidate, len(input.Candidates))
	for _, c := range input.Candidates {
		candByID[c.EmployeeID] = c
	}

	deficitIDs := make(map[string]bool, len(input.Variant.Deficits))
	for _, d := range input.Variant.Deficits {
		deficitIDs[d.RequirementID] = true
	}

	// Aggregate covered person-days per requirement, accounting for competence fit.
	coveredPD := make(map[string]float64)
	for _, a := range input.Variant.WeeklyAssignments {
		if a.Hours <= 0 {
			continue
		}
		req, ok := reqByID[a.RequirementID]
		if !ok {
			continue
		}
		cand, ok := candByID[a.EmployeeID]
		if !ok {
			continue
		}
		lvl, hasComp := cand.CompetencyLevels[req.CompetencyID]
		if !hasComp {
			continue
		}
		q := math.Min(1.0, float64(lvl)/float64(req.Level))
		personDays := a.Hours / 8.0 * q // 8 working hours per day, simplified.
		coveredPD[req.RequirementID] += personDays
	}

	for _, req := range input.Requirements {
		if req.Criticality != "must" {
			continue
		}
		if coveredPD[req.RequirementID] >= req.Effort.LikelyPersonDays {
			continue
		}
		if deficitIDs[req.RequirementID] {
			continue
		}
		res.Violations.G3++
		res.Violations.UncoveredMustNotInDeficit++
		res.addReason(reasonG3MustUncovered)
		res.addDetail(fmt.Sprintf(
			"G3: must requirement %s covered %.1fpd but needs %.1fpd and is not listed as deficit",
			req.RequirementID, coveredPD[req.RequirementID], req.Effort.LikelyPersonDays))
	}
}

// ---------- G4: user constraints ----------
func (v *Validator) validateG4(input ValidateInput, res *ValidationResult) {
	memberIDs := make(map[string]bool, len(input.Variant.Members))
	for _, m := range input.Variant.Members {
		memberIDs[m.EmployeeID] = true
	}

	// Force include: all required employees must be members.
	for _, empID := range input.Constraints.ForceInclude {
		if !memberIDs[empID] {
			res.Violations.G4++
			res.addReason(reasonG4ForceInclude)
			res.addDetail(fmt.Sprintf("G4: force-include %s not in variant members", empID))
		}
	}
	for _, empID := range input.Constraints.FreezeMembers {
		if !memberIDs[empID] {
			res.Violations.G4++
			res.addReason(reasonG4ForceInclude)
			res.addDetail(fmt.Sprintf("G4: freeze-member %s not in variant members", empID))
		}
	}

	// Force exclude: none of the excluded employees may be members.
	for _, empID := range input.Constraints.ForceExclude {
		if memberIDs[empID] {
			res.Violations.G4++
			res.addReason(reasonG4ForceExclude)
			res.addDetail(fmt.Sprintf("G4: force-exclude %s is in variant members", empID))
		}
	}

	// Team size limits.
	teamSize := len(input.Variant.Members)
	if input.Constraints.MaxTeamSize > 0 && teamSize > input.Constraints.MaxTeamSize {
		res.Violations.G4++
		res.addReason(reasonG4TeamSizeExceeded)
		res.addDetail(fmt.Sprintf("G4: team size %d exceeds max %d", teamSize, input.Constraints.MaxTeamSize))
	}
	if input.Constraints.MinTeamSize > 0 && teamSize < input.Constraints.MinTeamSize {
		res.Violations.G4++
		res.addReason(reasonG4TeamSizeExceeded)
		res.addDetail(fmt.Sprintf("G4: team size %d below min %d", teamSize, input.Constraints.MinTeamSize))
	}

	// Budget cap.
	if input.Constraints.BudgetCapUnits > 0 {
		cost := v.variantCost(input)
		if cost > input.Constraints.BudgetCapUnits {
			res.Violations.G4++
			res.addReason(reasonG4BudgetExceeded)
			res.addDetail(fmt.Sprintf("G4: cost %.2f exceeds budget %.2f", cost, input.Constraints.BudgetCapUnits))
		}
	}
}

// variantCost computes the dimensionless cost of a variant.
// It prefers the pre-computed AllocatedHours on VariantMember; otherwise it falls back
// to summing weekly assignment hours per member.
func (v *Validator) variantCost(input ValidateInput) float64 {
	candByID := make(map[string]RunInputCandidate, len(input.Candidates))
	for _, c := range input.Candidates {
		candByID[c.EmployeeID] = c
	}

	hoursByMember := make(map[string]float64)
	for _, m := range input.Variant.Members {
		if m.AllocatedHours > 0 {
			hoursByMember[m.EmployeeID] = m.AllocatedHours
		}
	}
	// Fallback to summing assignments for members without AllocatedHours.
	for _, a := range input.Variant.WeeklyAssignments {
		if _, ok := hoursByMember[a.EmployeeID]; !ok {
			hoursByMember[a.EmployeeID] += a.Hours
		}
	}

	var total float64
	for empID, hours := range hoursByMember {
		cost := 1.0
		if c, ok := candByID[empID]; ok && c.CostCoefficient > 0 {
			cost = c.CostCoefficient
		} else if g := gradeCost(empID, input.Variant.Members); g > 0 {
			cost = g
		}
		total += hours * cost
	}
	return total
}

// gradeCost returns the cost coefficient implied by a member's grade, or 0 if unknown.
// It is only a fallback when the candidate snapshot does not contain a cost coefficient.
func gradeCost(employeeID string, members []VariantMember) float64 {
	for _, m := range members {
		if m.EmployeeID != employeeID {
			continue
		}
		switch m.Grade {
		case "J":
			return 1.0
		case "M":
			return 1.4
		case "S":
			return 1.9
		case "L":
			return 2.6
		case "P":
			return 3.4
		}
	}
	return 0
}

// ---------- G5: data integrity ----------
func (v *Validator) validateG5(input ValidateInput, res *ValidationResult) {
	reqIDs := make(map[string]bool, len(input.Requirements))
	for _, r := range input.Requirements {
		reqIDs[r.RequirementID] = true
	}
	candIDs := make(map[string]bool, len(input.Candidates))
	for _, c := range input.Candidates {
		candIDs[c.EmployeeID] = true
	}
	weekSet := make(map[string]bool, len(input.Horizon))
	for _, w := range input.Horizon {
		weekSet[w] = true
	}

	for _, m := range input.Variant.Members {
		if !domain.EmployeeIDRe.MatchString(m.EmployeeID) || !candIDs[m.EmployeeID] {
			res.Violations.G5++
			res.addReason(reasonG5UnknownEmployee)
			res.addDetail(fmt.Sprintf("G5: member %s is not a known active candidate", m.EmployeeID))
		}
		if m.RoleID != "" && !domain.RoleIDRe.MatchString(m.RoleID) {
			res.Violations.G5++
			res.addReason(reasonG5InvalidRole)
			res.addDetail(fmt.Sprintf("G5: member %s has invalid role_id %s", m.EmployeeID, m.RoleID))
		}
	}

	for _, a := range input.Variant.WeeklyAssignments {
		if !domain.EmployeeIDRe.MatchString(a.EmployeeID) || !candIDs[a.EmployeeID] {
			res.Violations.G5++
			res.addReason(reasonG5UnknownEmployee)
			res.addDetail(fmt.Sprintf("G5: assignment references unknown employee %s", a.EmployeeID))
		}
		if a.RequirementID != "" && !reqIDs[a.RequirementID] {
			res.Violations.G5++
			res.addReason(reasonG5UnknownRequirement)
			res.addDetail(fmt.Sprintf("G5: assignment references unknown requirement %s", a.RequirementID))
		}
		if !domain.ValidateWeekKey(a.Week) || !weekSet[a.Week] {
			res.Violations.G5++
			res.addReason(reasonG5InvalidWeek)
			res.addDetail(fmt.Sprintf("G5: assignment week %q is invalid or outside horizon", a.Week))
		}
	}
}

// ---------- helpers ----------

type capacityLookup struct {
	rows map[string]map[string]domain.WeeklyCapacity
}

func buildCapacityLookup(matrix map[string][]domain.WeeklyCapacity) capacityLookup {
	l := capacityLookup{rows: make(map[string]map[string]domain.WeeklyCapacity, len(matrix))}
	for empID, weeks := range matrix {
		if l.rows[empID] == nil {
			l.rows[empID] = make(map[string]domain.WeeklyCapacity, len(weeks))
		}
		for _, w := range weeks {
			l.rows[empID][w.Week] = w
		}
	}
	return l
}

func (l capacityLookup) get(employeeID, week string) (domain.WeeklyCapacity, bool) {
	byWeek, ok := l.rows[employeeID]
	if !ok {
		return domain.WeeklyCapacity{}, false
	}
	row, ok := byWeek[week]
	return row, ok
}

func overloadPct(excessMinutes, effectiveLimitMinutes int) float64 {
	if excessMinutes <= 0 {
		return 0
	}
	if effectiveLimitMinutes <= 0 {
		return 10000.0 // cap at a large but finite number.
	}
	return float64(excessMinutes) / float64(effectiveLimitMinutes) * 100.0
}

func (res *ValidationResult) addReason(code string) {
	for _, c := range res.ReasonCodes {
		if c == code {
			return
		}
	}
	res.ReasonCodes = append(res.ReasonCodes, code)
}

func (res *ValidationResult) addDetail(d string) {
	res.Details = append(res.Details, d)
}
