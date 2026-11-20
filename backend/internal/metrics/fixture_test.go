package metrics

import (
	"testing"
	"time"

	"github.com/teamopt/backend/internal/runinput"
)

// The tests in this package use one fully hand-computable scenario so that every
// expected value can be derived on paper from docs/METRICS.md.
//
// Horizon: 2026-W10, 2026-W11 (2 weeks).
//
// Requirements (person-days):
//
//	req_a  backend_engineer / lang_go / L3 / must   / effort 5/5/5   (E_likely = 5)
//	req_b  qa_engineer      / role-only / L2 / should / effort 2/2/2   (E_likely = 2)
//
// The three effort points are equal on purpose: the triangular distribution then
// degenerates, so the Monte Carlo duration is exactly hand-computable (5 pd = 40 h and
// 2 pd = 16 h of deterministic work). The distribution itself is covered by
// internal/schedule; the three-point range is exercised by the property test below.
//
// Candidates (all data synthetic; cost is a coefficient by grade):
//
//	emp_aa01  backend_engineer  S  1.9  bench  exp 0.5  projects [prj_p1]  lang_go 5
//	          capacity W10 norm 40 assigned 0  free 40 | W11 norm 40 assigned 8  free 32
//	emp_bb02  qa_engineer       M  1.4  no bench         projects []         -
//	          capacity W10 norm 40 assigned 32 free 8  | W11 norm 40 assigned 0  free 40
//	emp_cc03  backend_engineer  J  1.0  bench  exp 0.1  projects []         lang_go 3
//	          capacity W10/W11 norm 40 assigned 0 free 40
//	emp_dd04  qa_engineer       M  1.4  bench  -        projects [prj_p2 prj_p3] -
//	          capacity W10/W11 norm 40 assigned 16 free 24
//
// Portfolio: prj_p1 remaining 80 h, weekly capacity 40 h, critical.
const (
	reqA = "req_a"
	reqB = "req_b"
)

func fixtureRequirements() []runinput.Requirement {
	langGo := "lang_go"
	return []runinput.Requirement{
		{
			RequirementID: reqA, RoleID: "backend_engineer", CompetencyID: &langGo,
			Level: 3, Criticality: "must", Headcount: 1,
			Effort:         runinput.Effort{MinPersonDays: 5, LikelyPersonDays: 5, MaxPersonDays: 5},
			AllowsExternal: true,
		},
		{
			RequirementID: reqB, RoleID: "qa_engineer", CompetencyID: nil,
			Level: 2, Criticality: "should", Headcount: 1,
			Effort:         runinput.Effort{MinPersonDays: 2, LikelyPersonDays: 2, MaxPersonDays: 2},
			AllowsExternal: true,
		},
	}
}

func capacity(rows ...runinput.CapacityRow) []runinput.CapacityRow { return rows }

func week(weekKey string, assigned, free float64) runinput.CapacityRow {
	return runinput.CapacityRow{
		Week: weekKey, WorkingDays: 5, NormHours: 40,
		AssignedHours: assigned, FreeHours: free,
	}
}

func fixtureCandidates(withExperience bool) []runinput.Candidate {
	exp := func(v float64) *float64 {
		if !withExperience {
			return nil
		}
		value := v
		return &value
	}
	return []runinput.Candidate{
		{
			EmployeeID: "emp_aa01", Grade: "S", CostCoefficient: 1.9,
			RoleProfileID: "backend_engineer", EmploymentType: "internal", BenchFlag: true,
			ExperienceScore: exp(0.5), CurrentProjectIDs: []string{"prj_p1"},
			CompetencyLevels: map[string]int{"lang_go": 5},
			Capacity: capacity(
				week("2026-W10", 0, 40),
				week("2026-W11", 8, 32),
			),
		},
		{
			EmployeeID: "emp_bb02", Grade: "M", CostCoefficient: 1.4,
			RoleProfileID: "qa_engineer", EmploymentType: "internal", BenchFlag: false,
			ExperienceScore: nil, CurrentProjectIDs: []string{},
			CompetencyLevels: map[string]int{},
			Capacity: capacity(
				week("2026-W10", 32, 8),
				week("2026-W11", 0, 40),
			),
		},
		{
			EmployeeID: "emp_cc03", Grade: "J", CostCoefficient: 1.0,
			RoleProfileID: "backend_engineer", EmploymentType: "internal", BenchFlag: true,
			ExperienceScore: exp(0.1), CurrentProjectIDs: []string{},
			CompetencyLevels: map[string]int{"lang_go": 3},
			Capacity: capacity(
				week("2026-W10", 0, 40),
				week("2026-W11", 0, 40),
			),
		},
		{
			EmployeeID: "emp_dd04", Grade: "M", CostCoefficient: 1.4,
			RoleProfileID: "qa_engineer", EmploymentType: "internal", BenchFlag: true,
			ExperienceScore: nil, CurrentProjectIDs: []string{"prj_p2", "prj_p3"},
			CompetencyLevels: map[string]int{},
			Capacity: capacity(
				week("2026-W10", 16, 24),
				week("2026-W11", 16, 24),
			),
		},
	}
}

// fixtureWeights sum to 1.0 (checked by runinput.Build).
func fixtureWeights() map[string]float64 {
	return map[string]float64{
		"M1": 0.2, "M2": 0.1, "M3": 0.1, "M4": 0.1, "M5": 0.1, "M6": 0.1,
		"M7": 0.1, "M8": 0.05, "M9": 0.05, "M10": 0.05, "M11": 0.025, "M12": 0.025,
	}
}

func fixtureData(withExperience bool) runinput.Data {
	return runinput.Data{
		RunID: "run_t0001", BriefID: "brf_t0001", Preset: "balanced", K: 2, Seed: 42,
		TimeLimitMS:  5000,
		Horizon:      []string{"2026-W10", "2026-W11"},
		Weights:      fixtureWeights(),
		Requirements: fixtureRequirements(),
		Candidates:   fixtureCandidates(withExperience),
		Constraints: runinput.Constraints{
			MaxTeamSize: 3, MinTeamSize: 1, MaxUtilizationRatio: 0.95,
		},
		Portfolio: []runinput.PortfolioProject{
			{ProjectID: "prj_p1", RemainingHours: 80, WeeklyCapacity: 40, Critical: true},
		},
		CreatedAt: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	}
}

func fixtureSnapshot(t *testing.T, withExperience bool) *runinput.Snapshot {
	t.Helper()
	snap, err := runinput.Build(fixtureData(withExperience))
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	return snap
}

// variantA1 covers req_a with one senior backend engineer (level 5 vs required 3, i.e.
// over-qualified) and req_b with one QA engineer holding the role.
func variantA1() Proposal {
	return Proposal{
		VariantID: "var_a1",
		Label:     "A1",
		Members: []ProposalMember{
			{EmployeeID: "emp_aa01", RoleID: "backend_engineer", Grade: "S"},
			{EmployeeID: "emp_bb02", RoleID: "qa_engineer", Grade: "M"},
		},
		Assignments: []ProposalAssignment{
			{EmployeeID: "emp_aa01", RequirementID: reqA, Week: "2026-W10", Hours: 30},
			{EmployeeID: "emp_aa01", RequirementID: reqA, Week: "2026-W11", Hours: 10},
			{EmployeeID: "emp_bb02", RequirementID: reqB, Week: "2026-W10", Hours: 8},
			{EmployeeID: "emp_bb02", RequirementID: reqB, Week: "2026-W11", Hours: 8},
		},
	}
}

// variantB1 is a cheaper, slower roster with a disjoint roster (Jaccard distance 1).
func variantB1() Proposal {
	return Proposal{
		VariantID: "var_b1",
		Label:     "B1",
		Members: []ProposalMember{
			{EmployeeID: "emp_cc03", RoleID: "backend_engineer", Grade: "J"},
			{EmployeeID: "emp_dd04", RoleID: "qa_engineer", Grade: "M"},
		},
		Assignments: []ProposalAssignment{
			{EmployeeID: "emp_cc03", RequirementID: reqA, Week: "2026-W10", Hours: 40},
			{EmployeeID: "emp_dd04", RequirementID: reqB, Week: "2026-W10", Hours: 8},
		},
	}
}

func fixtureRunInput(t *testing.T, withExperience bool) RunInput {
	t.Helper()
	return RunInput{
		Snapshot:  fixtureSnapshot(t, withExperience),
		Proposals: []Proposal{variantA1(), variantB1()},
		Schedule:  ScheduleConfig{MonteCarloRuns: 200, Seed: 42, PortfolioTauWorkingDays: 10},
	}
}
