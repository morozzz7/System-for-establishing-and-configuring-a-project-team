package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/teamopt/backend/internal/runinput"
)

// TestComputeRunProperties is a seeded property test over randomly shaped runs: 200
// independent problem instances of the same generator, each with a fixed seed, so a
// failure is always reproducible.
//
// Checked properties (docs/METRICS.md Level 1 and the package doc):
//   - every metric value, every diagnostic and the score stay inside [0, 1];
//   - utilization diagnostics never exceed the documented display cap;
//   - a variant that fails the gate scores exactly 0, a passing variant may not;
//   - the score is the renormalized weighted mean of the served metric values;
//   - cost, deficits and durations are non-negative and P50 <= P80;
//   - the same input produces byte-identical JSON (determinism).
func TestComputeRunProperties(t *testing.T) {
	const iterations = 200
	for i := 0; i < iterations; i++ {
		seed := int64(1000 + i)
		in := generatedRun(t, seed)
		result, err := ComputeRun(in)
		if err != nil {
			t.Fatalf("iteration %d (seed %d): %v", i, seed, err)
		}
		weights := in.Snapshot.Weights()
		rosterSizes := make(map[string]int, len(in.Proposals))
		for _, p := range in.Proposals {
			rosterSizes[p.VariantID] = len(p.Members)
		}

		for _, v := range result.Variants {
			prefix := fmt.Sprintf("iteration %d (seed %d) variant %s", i, seed, v.VariantID)
			for _, key := range Keys {
				value, present := v.Metrics.value(key)
				if !present {
					continue
				}
				if math.IsNaN(value) || value < 0 || value > 1 {
					t.Fatalf("%s: %s = %v is outside [0,1]", prefix, key, value)
				}
			}
			if v.Metrics.Score < 0 || v.Metrics.Score > 1 {
				t.Fatalf("%s: score %v is outside [0,1]", prefix, v.Metrics.Score)
			}
			if !v.GatePass && v.Metrics.Score != 0 {
				t.Fatalf("%s: gate failure must force score 0, got %v", prefix, v.Metrics.Score)
			}
			if v.GatePass {
				checkScoreIdentity(t, prefix, v.Metrics, weights)
			}

			if v.CostUnits < 0 || math.IsNaN(v.CostUnits) {
				t.Fatalf("%s: cost %v must be non-negative", prefix, v.CostUnits)
			}
			for _, d := range v.Deficits {
				if d.PersonDays <= 0 || math.IsNaN(d.PersonDays) {
					t.Fatalf("%s: deficit %#v must be positive", prefix, d)
				}
			}
			if v.DeficitCount != len(v.Deficits) {
				t.Fatalf("%s: deficit_count %d != %d", prefix, v.DeficitCount, len(v.Deficits))
			}
			if v.ScheduleError == "" {
				if v.Duration.P50Weeks < 0 || v.Duration.P80Weeks < v.Duration.P50Weeks-1e-9 {
					t.Fatalf("%s: duration P50 %v / P80 %v is inconsistent",
						prefix, v.Duration.P50Weeks, v.Duration.P80Weeks)
				}
				if v.Metrics.M5 < 0 || v.Metrics.M5 > 1 {
					t.Fatalf("%s: M5 = %v is outside [0,1]", prefix, v.Metrics.M5)
				}
			}

			// Diagnostic caps: utilization may legitimately exceed 1 (overload), but the
			// contract reports it clipped at 1.5.
			if v.Details.PeakUtilization != nil && (*v.Details.PeakUtilization < 0 || *v.Details.PeakUtilization > 1.5) {
				t.Fatalf("%s: peak_utilization %v is outside [0,1.5]", prefix, *v.Details.PeakUtilization)
			}
			if v.Details.MeanUtilization != nil && (*v.Details.MeanUtilization < 0 || *v.Details.MeanUtilization > 1.5) {
				t.Fatalf("%s: mean_utilization %v is outside [0,1.5]", prefix, *v.Details.MeanUtilization)
			}
			if v.Details.FragmentationShare != nil && (*v.Details.FragmentationShare < 0 || *v.Details.FragmentationShare > 1) {
				t.Fatalf("%s: fragmentation_share %v is outside [0,1]", prefix, *v.Details.FragmentationShare)
			}
			if v.Details.NOpt != nil && *v.Details.NOpt < 1 {
				t.Fatalf("%s: n_opt %d must be >= 1", prefix, *v.Details.NOpt)
			}
			if want, ok := rosterSizes[v.VariantID]; !ok || v.TeamSize != want {
				t.Fatalf("%s: team_size %d, want %d", prefix, v.TeamSize, want)
			}
		}

		// Determinism: the same input (rebuilt from the same data) must give identical
		// JSON, including map ordering of the pairwise diagnostics.
		again, err := ComputeRun(in)
		if err != nil {
			t.Fatalf("iteration %d: second computation failed: %v", i, err)
		}
		first, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("iteration %d: marshal: %v", i, err)
		}
		second, err := json.Marshal(again)
		if err != nil {
			t.Fatalf("iteration %d: marshal: %v", i, err)
		}
		if string(first) != string(second) {
			t.Fatalf("iteration %d (seed %d): output is not deterministic", i, seed)
		}
		rebuilt := generatedRun(t, seed)
		third, err := ComputeRun(rebuilt)
		if err != nil {
			t.Fatalf("iteration %d: rebuild failed: %v", i, err)
		}
		thirdJSON, err := json.Marshal(third)
		if err != nil {
			t.Fatalf("iteration %d: marshal rebuild: %v", i, err)
		}
		if string(first) != string(thirdJSON) {
			t.Fatalf("iteration %d (seed %d): equal input gave a different result", i, seed)
		}
	}
}

// checkScoreIdentity proves the served score is exactly the renormalized weighted mean
// of the served metric values (the auditability requirement of AGENTS.md rule 4).
func checkScoreIdentity(t *testing.T, prefix string, metrics MetricValues, weights map[string]float64) {
	t.Helper()
	sumWeight, sumWeighted := 0.0, 0.0
	for _, key := range Keys {
		value, present := metrics.value(key)
		if !present || weights[key] <= 0 {
			continue
		}
		sumWeight += weights[key]
		sumWeighted += weights[key] * value
	}
	if sumWeight == 0 {
		t.Fatalf("%s: no metric carries a positive weight", prefix)
	}
	expected := sumWeighted / sumWeight
	if diff := metrics.Score - expected; diff > 1e-5 || diff < -1e-5 {
		t.Fatalf("%s: score %v is not the renormalized weighted mean %v", prefix, metrics.Score, expected)
	}
}

// generatedRun builds one random but valid run for the property test. Everything is
// derived from the seed, so the instance is reproducible; all identifiers are synthetic.
func generatedRun(t *testing.T, seed int64) RunInput {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))

	horizon := make([]string, 0, 4)
	weeks := 2 + rng.Intn(3)
	for i := 0; i < weeks; i++ {
		horizon = append(horizon, fmt.Sprintf("2026-W%02d", 10+i))
	}

	competencies := []string{"lang_go", "lang_python", "db_postgres", "qa_automation"}
	roles := []string{"backend_engineer", "qa_engineer", "data_engineer"}
	criticalities := []string{"must", "should", "nice"}

	reqCount := 1 + rng.Intn(3)
	requirements := make([]runinput.Requirement, 0, reqCount)
	for i := 0; i < reqCount; i++ {
		min := 1 + float64(rng.Intn(4))
		likely := min + float64(rng.Intn(3))
		max := likely + float64(rng.Intn(3))
		var competency *string
		if rng.Intn(4) != 0 {
			id := competencies[rng.Intn(len(competencies))]
			competency = &id
		}
		requirements = append(requirements, runinput.Requirement{
			RequirementID:  fmt.Sprintf("req_%d", i),
			RoleID:         roles[rng.Intn(len(roles))],
			CompetencyID:   competency,
			Level:          1 + rng.Intn(5),
			Criticality:    criticalities[rng.Intn(len(criticalities))],
			Headcount:      1,
			Effort:         runinput.Effort{MinPersonDays: min, LikelyPersonDays: likely, MaxPersonDays: max},
			AllowsExternal: rng.Intn(2) == 0,
		})
	}

	grades := map[int]string{0: "J", 1: "M", 2: "S", 3: "L"}
	coefficients := map[int]float64{0: 1.0, 1: 1.4, 2: 1.9, 3: 2.6}
	candidateCount := 2 + rng.Intn(5)
	candidates := make([]runinput.Candidate, 0, candidateCount)
	for i := 0; i < candidateCount; i++ {
		gradeIdx := rng.Intn(4)
		levels := map[string]int{}
		for _, competency := range competencies {
			if rng.Intn(3) == 0 {
				levels[competency] = 1 + rng.Intn(5)
			}
		}
		rows := make([]runinput.CapacityRow, 0, len(horizon))
		for _, week := range horizon {
			norm := 40.0
			assigned := float64(rng.Intn(5)) * 8
			absent := 0.0
			unavailable := false
			if rng.Intn(6) == 0 {
				// A whole week off: unavailable, no free hours.
				absent = norm
				assigned = 0
				unavailable = true
			}
			free := math.Max(0, norm-assigned-absent)
			rows = append(rows, runinput.CapacityRow{
				Week: week, WorkingDays: 5, NormHours: norm,
				AssignedHours: assigned, AbsentHours: absent, FreeHours: free,
				Unavailable: unavailable,
			})
		}
		var experience *float64
		if rng.Intn(3) != 0 {
			value := math.Min(1, math.Max(0, rng.Float64()))
			experience = &value
		}
		projects := make([]string, 0, 3)
		for p := 0; p < rng.Intn(3); p++ {
			projects = append(projects, fmt.Sprintf("prj_g%d%02d", p, i))
		}
		candidates = append(candidates, runinput.Candidate{
			EmployeeID:        fmt.Sprintf("emp_g%03d", i),
			Grade:             grades[gradeIdx],
			CostCoefficient:   coefficients[gradeIdx],
			RoleProfileID:     roles[rng.Intn(len(roles))],
			EmploymentType:    "internal",
			BenchFlag:         rng.Intn(2) == 0,
			ExperienceScore:   experience,
			CurrentProjectIDs: projects,
			CompetencyLevels:  levels,
			Capacity:          rows,
		})
	}

	constraints := runinput.Constraints{
		MaxTeamSize:         1 + rng.Intn(candidateCount),
		MaxUtilizationRatio: 1,
	}
	switch rng.Intn(5) {
	case 0:
		constraints.DeadlineWeek = horizon[rng.Intn(len(horizon))]
	case 1:
		constraints.BudgetCapUnits = 200 + float64(rng.Intn(400))
	case 2:
		constraints.ForceExclude = []string{candidates[rng.Intn(len(candidates))].EmployeeID}
	}

	portfolio := make([]runinput.PortfolioProject, 0, 2)
	for p := 0; p < rng.Intn(3); p++ {
		portfolio = append(portfolio, runinput.PortfolioProject{
			ProjectID:      fmt.Sprintf("prj_p%03d", p),
			RemainingHours: 40 + float64(rng.Intn(120)),
			WeeklyCapacity: 20 + float64(rng.Intn(40)),
			Critical:       rng.Intn(2) == 0,
		})
	}

	data := runinput.Data{
		RunID:        "run_prop001",
		BriefID:      "brf_prop001",
		Preset:       "balanced",
		K:            2,
		Seed:         seed,
		TimeLimitMS:  2000,
		Horizon:      horizon,
		Weights:      simpleWeights(),
		Requirements: requirements,
		Candidates:   candidates,
		Constraints:  constraints,
		Portfolio:    portfolio,
		CreatedAt:    time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	}
	snap, err := runinput.Build(data)
	if err != nil {
		t.Fatalf("seed %d: build snapshot: %v", seed, err)
	}

	// Two variants with overlapping-but-different rosters and random weekly hours.
	proposals := make([]Proposal, 0, 2)
	for v := 0; v < 2; v++ {
		members := make([]ProposalMember, 0, 2)
		assignments := make([]ProposalAssignment, 0, 8)
		start := v % candidateCount
		for m := 0; m < 2 && m < candidateCount; m++ {
			cand := candidates[(start+m)%candidateCount]
			members = append(members, ProposalMember{
				EmployeeID: cand.EmployeeID, RoleID: cand.RoleProfileID, Grade: cand.Grade,
			})
			for _, req := range requirements {
				for _, week := range horizon {
					if rng.Intn(3) == 0 {
						continue
					}
					hours := float64(4+rng.Intn(5)) * 4 // 16..32 h
					assignments = append(assignments, ProposalAssignment{
						EmployeeID: cand.EmployeeID, RequirementID: req.RequirementID,
						Week: week, Hours: hours,
					})
				}
			}
		}
		proposals = append(proposals, Proposal{
			VariantID:   fmt.Sprintf("var_g%d", v),
			Label:       fmt.Sprintf("G%d", v),
			Members:     members,
			Assignments: assignments,
		})
	}

	return RunInput{
		Snapshot:  snap,
		Proposals: proposals,
		Schedule:  ScheduleConfig{MonteCarloRuns: 100, Seed: seed, PortfolioTauWorkingDays: 10},
	}
}
