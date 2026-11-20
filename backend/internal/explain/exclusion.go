package explain

import (
	"math"
	"sort"

	"github.com/teamopt/backend/internal/runinput"
)

// expensiveCoefficient is the cost-coefficient threshold above which a candidate is
// considered "expensive" for the exclusion precedence. It matches the Lead (L) grade
// coefficient in contracts/reference.yaml so the rule has a documented, stable source.
const expensiveCoefficient = 2.6

// RelevantCandidate reports whether a candidate's role profile or any competency matches
// at least one requirement (ADR-007 §2 "relevant excluded candidate").
func RelevantCandidate(snap *runinput.Snapshot, candidate runinput.Candidate) bool {
	for _, req := range snap.Requirements() {
		if candidate.HasRole(req.RoleID) {
			return true
		}
		if req.CompetencyID != nil && candidate.CompetencyLevels[*req.CompetencyID] > 0 {
			return true
		}
	}
	return false
}

// CandidateExclusions derives the deterministic exclusion verdicts for every relevant
// candidate of the snapshot that is NOT on the team, in the documented precedence order:
// user force exclude -> absent -> overloaded -> below required level -> team-size limit ->
// no free capacity -> portfolio protection -> expensive -> data incomplete.
func CandidateExclusions(snap *runinput.Snapshot, detail VariantDetail) []Exclusion {
	member := make(map[string]bool, len(detail.Members))
	for _, m := range detail.Members {
		member[m.EmployeeID] = true
	}
	teamSizeLimit := snap.Constraints().MaxTeamSize
	currentTeamSize := len(detail.Members)

	excluded := make([]Exclusion, 0)
	for _, candidate := range snap.Candidates() {
		if member[candidate.EmployeeID] || !RelevantCandidate(snap, candidate) {
			continue
		}
		excluded = append(excluded, excludeCandidate(snap, candidate, teamSizeLimit, currentTeamSize))
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].EmployeeID < excluded[j].EmployeeID })
	return excluded
}

func excludeCandidate(snap *runinput.Snapshot, candidate runinput.Candidate, teamSizeLimit, currentTeamSize int) Exclusion {
	cons := snap.Constraints()
	if contains(cons.ForceExclude, candidate.EmployeeID) {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_USER_FORCE"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID},
		}
	}
	absent, absentWeeks := candidoidAbsent(candidate)
	if absent {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_ABSENT"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID, "absent_weeks": absentWeeks},
		}
	}
	if pct, overloaded := candidoidOverload(candidate); overloaded {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_OVERLOADED"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID, "overload_pct": math.Round(pct*100) / 100},
		}
	}
	if below := candidateBelowRequiredLevel(snap, candidate); below {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_BELOW_REQUIRED_LEVEL"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID},
		}
	}
	if teamSizeLimit > 0 && currentTeamSize >= teamSizeLimit {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_TEAM_SIZE_LIMIT"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID, "max_team_size": teamSizeLimit, "team_size": currentTeamSize},
		}
	}
	if noFree, free := candidoidNoFreeCapacity(candidate); noFree {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_NO_FREE_CAPACITY"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID, "free_hours": free},
		}
	}
	if protected := candidatePortfolioProtected(snap, candidate); protected {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_PORTFOLIO_PROTECTION"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID},
		}
	}
	if candidate.CostCoefficient >= expensiveCoefficient {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_EXPENSIVE"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID, "cost_coefficient": candidate.CostCoefficient},
		}
	}
	if candidateDataIncomplete(snap, candidate) {
		return Exclusion{
			EmployeeID:  candidate.EmployeeID,
			ReasonCodes: []string{"EXCLUDE_DATA_INCOMPLETE"},
			Facts:       map[string]any{"employee_id": candidate.EmployeeID},
		}
	}
	// Fallback: relevant but not picked for the current optimized roster. This is the
	// residual "not in this variant" case (still covered, with an honest reason).
	return Exclusion{
		EmployeeID:  candidate.EmployeeID,
		ReasonCodes: []string{"EXCLUDE_NO_FREE_CAPACITY", "EXCLUDE_TEAM_SIZE_LIMIT"},
		Facts:       map[string]any{"employee_id": candidate.EmployeeID},
	}
}

// candidateDataIncomplete reports a candidate whose snapshot evidence is so thin that no
// inclusion reason can be honestly fabricated.
func candidateDataIncomplete(snap *runinput.Snapshot, candidate runinput.Candidate) bool {
	_ = snap
	return len(candidate.CompetencyLevels) == 0 && candidate.ExperienceScore == nil && len(candidate.Capacity) == 0
}

func candidoidAbsent(candidate runinput.Candidate) (bool, int) {
	weeks := 0
	for _, row := range candidate.Capacity {
		if row.Unavailable || row.AbsentHours > 0 {
			weeks++
		}
	}
	return weeks > 0, weeks
}

func candidoidOverload(candidate runinput.Candidate) (float64, bool) {
	maxPct := 0.0
	for _, row := range candidate.Capacity {
		if row.NormHours <= 0 {
			continue
		}
		loaded := (row.AssignedHours + row.AbsentHours) / row.NormHours
		if loaded > 1 {
			pct := (loaded - 1) * 100
			if pct > maxPct {
				maxPct = pct
			}
		}
	}
	return maxPct, maxPct > 0
}

// candidateBelowRequiredLevel reports whether the candidate cannot meet the required
// level of at least one requirement they are relevant to, and is qualified for none at
// or above the required level.
func candidateBelowRequiredLevel(snap *runinput.Snapshot, candidate runinput.Candidate) bool {
	meetsAny := false
	belowRelevant := false
	for _, req := range snap.Requirements() {
		if !RelevantCandidateFor(req, candidate) {
			continue
		}
		if req.CompetencyID == nil {
			if candidate.HasRole(req.RoleID) {
				meetsAny = true
				continue
			}
		} else {
			lvl := candidate.CompetencyLevels[*req.CompetencyID]
			if lvl >= req.Level {
				meetsAny = true
				continue
			}
			belowRelevant = true
		}
	}
	// Only "below the required level" when the candidate is relevant but is not actually
	// qualified for any relevant requirement.
	return belowRelevant && !meetsAny
}

func RelevantCandidateFor(req runinput.Requirement, candidate runinput.Candidate) bool {
	if candidate.HasRole(req.RoleID) {
		return true
	}
	if req.CompetencyID != nil && candidate.CompetencyLevels[*req.CompetencyID] > 0 {
		return true
	}
	return false
}

func candidoidNoFreeCapacity(candidate runinput.Candidate) (bool, float64) {
	free := 0.0
	for _, row := range candidate.Capacity {
		free += row.FreeHours
	}
	return free <= 0, math.Round(free*100) / 100
}

func candidatePortfolioProtected(snap *runinput.Snapshot, candidate runinput.Candidate) bool {
	if len(candidate.CurrentProjectIDs) == 0 {
		return false
	}
	portfolio := make(map[string]bool, len(snap.Portfolio()))
	for _, p := range snap.Portfolio() {
		portfolio[p.ProjectID] = true
	}
	for _, id := range candidate.CurrentProjectIDs {
		if portfolio[id] {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
