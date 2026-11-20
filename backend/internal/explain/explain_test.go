package explain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// testSnapshot builds a hand-computable snapshot with one deficit-producing requirement
// and two relevant-but-excluded candidates (below level and absent).
func testSnapshot(t *testing.T) *runinput.Snapshot {
	t.Helper()
	langGo := "lang_go"
	data := runinput.Data{
		RunID: "run_e001", BriefID: "brf_e001", Preset: "balanced", K: 2, Seed: 7,
		TimeLimitMS: 4000,
		Horizon:     []string{"2026-W10", "2026-W11"},
		Weights:     map[string]float64{"M1": 0.2, "M2": 0.1, "M3": 0.1, "M4": 0.1, "M5": 0.1, "M6": 0.1, "M7": 0.1, "M8": 0.05, "M9": 0.05, "M10": 0.05, "M11": 0.025, "M12": 0.025},
		Requirements: []runinput.Requirement{
			{RequirementID: "req_a", RoleID: "backend_engineer", CompetencyID: &langGo, Level: 5, Criticality: "must", Headcount: 1, Effort: runinput.Effort{MinPersonDays: 8, LikelyPersonDays: 12, MaxPersonDays: 16}, AllowsExternal: true},
			{RequirementID: "req_b", RoleID: "qa_engineer", CompetencyID: nil, Level: 2, Criticality: "should", Headcount: 1, Effort: runinput.Effort{MinPersonDays: 2, LikelyPersonDays: 2, MaxPersonDays: 2}, AllowsExternal: true},
		},
		Candidates: []runinput.Candidate{
			// emp_aa: senior backend, fully free -> selected.
			{EmployeeID: "emp_aa01", Grade: "S", CostCoefficient: 1.9, RoleProfileID: "backend_engineer", EmploymentType: "internal", BenchFlag: true, CompetencyLevels: map[string]int{"lang_go": 5},
				Capacity: []runinput.CapacityRow{capRow("2026-W10", 0, 40), capRow("2026-W11", 0, 40)}},
			// emp_bb: QA role, some free capacity -> selected for req_b.
			{EmployeeID: "emp_bb02", Grade: "M", CostCoefficient: 1.4, RoleProfileID: "qa_engineer", EmploymentType: "internal", CompetencyLevels: map[string]int{},
				Capacity: []runinput.CapacityRow{capRow("2026-W10", 32, 8), capRow("2026-W11", 0, 40)}},
			// emp_cc: relevant (lang_go) but level 3 < required 5 -> excluded below level.
			{EmployeeID: "emp_cc03", Grade: "J", CostCoefficient: 1.0, RoleProfileID: "backend_engineer", EmploymentType: "internal", BenchFlag: true, CompetencyLevels: map[string]int{"lang_go": 3},
				Capacity: []runinput.CapacityRow{capRow("2026-W10", 0, 40), capRow("2026-W11", 0, 40)}},
			// emp_dd: relevant (qa role) but absent -> excluded absent.
			{EmployeeID: "emp_dd04", Grade: "M", CostCoefficient: 1.4, RoleProfileID: "qa_engineer", EmploymentType: "internal", CompetencyLevels: map[string]int{},
				Capacity: []runinput.CapacityRow{absentRow("2026-W10", 40), absentRow("2026-W11", 40)}},
		},
		Constraints: runinput.Constraints{MaxTeamSize: 3, MinTeamSize: 1, MaxUtilizationRatio: 0.95},
		CreatedAt:   time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC),
	}
	snap, err := runinput.Build(data)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	return snap
}

func capRow(week string, assigned, free float64) runinput.CapacityRow {
	return runinput.CapacityRow{Week: week, WorkingDays: 5, NormHours: 40, AssignedHours: assigned, FreeHours: free}
}
func absentRow(week string, norm float64) runinput.CapacityRow {
	return runinput.CapacityRow{Week: week, WorkingDays: 5, NormHours: norm, FreeHours: 0, Unavailable: true}
}

// testProposal is a variant with a real deficit (80 h = 10 pd of work vs 12 pd likely)
// and a two-member roster, so teamSizeCounterfactual is computable.
func testProposal() metrics.Proposal {
	return metrics.Proposal{
		VariantID: "var_v1",
		Label:     "V1",
		Members: []metrics.ProposalMember{
			{EmployeeID: "emp_aa01", RoleID: "backend_engineer", Grade: "S"},
			{EmployeeID: "emp_bb02", RoleID: "qa_engineer", Grade: "M"},
		},
		Assignments: []metrics.ProposalAssignment{
			{EmployeeID: "emp_aa01", RequirementID: "req_a", Week: "2026-W10", Hours: 40},
			{EmployeeID: "emp_aa01", RequirementID: "req_a", Week: "2026-W11", Hours: 40},
			{EmployeeID: "emp_bb02", RequirementID: "req_b", Week: "2026-W10", Hours: 8},
			{EmployeeID: "emp_bb02", RequirementID: "req_b", Week: "2026-W11", Hours: 8},
		},
	}
}

func testDetail() VariantDetail {
	return VariantDetail{
		VariantID: "var_v1",
		Members: []MemberDetail{
			{EmployeeID: "emp_aa01", RoleID: "backend_engineer", Grade: "S", AllocatedHours: 80, AssignmentShare: 1, CompetenceFit: 1},
			{EmployeeID: "emp_bb02", RoleID: "qa_engineer", Grade: "M", AllocatedHours: 16, AssignmentShare: 1, CompetenceFit: 1},
		},
		WeeklyAssignments: []WeeklyAssignment{
			{EmployeeID: "emp_aa01", RequirementID: "req_a", Week: "2026-W10", Hours: 40, Share: 1},
			{EmployeeID: "emp_aa01", RequirementID: "req_a", Week: "2026-W11", Hours: 40, Share: 1},
			{EmployeeID: "emp_bb02", RequirementID: "req_b", Week: "2026-W10", Hours: 8, Share: 1},
			{EmployeeID: "emp_bb02", RequirementID: "req_b", Week: "2026-W11", Hours: 8, Share: 1},
		},
		ReasonCodes: []string{"INCLUDE_REQUIRED_ROLE"},
	}
}

func testInput(t *testing.T) Input {
	t.Helper()
	snap := testSnapshot(t)
	res, err := metrics.ComputeRun(metrics.RunInput{
		Snapshot:  snap,
		Proposals: []metrics.Proposal{testProposal()},
		Schedule:  metrics.ScheduleConfig{MonteCarloRuns: 200, Seed: 7, PortfolioTauWorkingDays: 10},
	})
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}
	if len(res.Variants) != 1 {
		t.Fatalf("variants = %d, want 1", len(res.Variants))
	}
	return Input{RunID: "run_e001", Locale: "ru", Snapshot: snap, Variant: res.Variants[0], Detail: testDetail(), Schedule: metrics.ScheduleConfig{MonteCarloRuns: 200, Seed: 7, PortfolioTauWorkingDays: 10}}
}

// TestGenerateCoverage asserts the ADR-007 §2 total coverage: every included member,
// every relevant excluded candidate and every deficit has an ExplanationFactor.
func TestGenerateCoverage(t *testing.T) {
	in := testInput(t)
	exp, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(exp.Included) != 2 {
		t.Fatalf("included factors = %d, want 2 (both members)", len(exp.Included))
	}
	// emp_cc03 (below level) and emp_dd04 (absent) are relevant and excluded.
	if len(exp.Excluded) != 2 {
		t.Fatalf("excluded factors = %d, want 2, got %#v", len(exp.Excluded), exp.Excluded)
	}
	if len(exp.Deficits) != 1 {
		t.Fatalf("deficit factors = %d, want 1 (req_a deficit of 2 pd)", len(exp.Deficits))
	}
	if len(exp.Counterfactuals) == 0 {
		t.Fatalf("no counterfactual emitted")
	}
}

// TestFaithfulness re-derives every number of every generated text from its facts.
func TestFaithfulness(t *testing.T) {
	exp, err := Generate(testInput(t))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	check := func(what string, text string, facts map[string]any) {
		t.Helper()
		if unsourced := Faithfulness(text, facts); len(unsourced) > 0 {
			t.Errorf("%s text %q has unsourced numbers %v", what, text, unsourced)
		}
	}
	for _, f := range exp.Included {
		check("included", f.Text, f.Facts)
	}
	for _, f := range exp.Excluded {
		check("excluded", f.Text, f.Facts)
	}
	for _, f := range exp.Deficits {
		check("deficit", f.Text, f.Facts)
	}
	check("tradeoff", exp.Tradeoff.Text, exp.Tradeoff.Facts)
	for _, c := range exp.Counterfactuals {
		check("counterfactual", c.Text, c.Facts)
	}
}

// TestDeterminism asserts that two Generate calls yield byte-identical JSON.
func TestDeterminism(t *testing.T) {
	in := testInput(t)
	a, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate (2nd): %v", err)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("explanations differ across identical inputs:\n%s\nvs\n%s", ja, jb)
	}
}

// TestRUTemplateRenders checks the RU template path and that English also renders.
func TestRUTemplateRenders(t *testing.T) {
	in := testInput(t)
	a, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.Locale != "ru" {
		t.Fatalf("locale = %q, want ru", a.Locale)
	}
	if a.Headline == "" || len(a.Included) == 0 || a.Included[0].Text == "" {
		t.Fatalf("RU text not rendered: %#v", a.Headline)
	}
	in.Locale = "en"
	b, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate en: %v", err)
	}
	if b.Locale != "en" || b.Headline == "" {
		t.Fatalf("EN text not rendered: %#v", b.Headline)
	}
}

// TestAtLeastOneRerunCounterfactual asserts the >= 95% rerun target is reachable: at
// least one counterfactual is verified by an actual metrics/schedule recomputation.
func TestAtLeastOneRerunCounterfactual(t *testing.T) {
	exp, err := Generate(testInput(t))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rerun := 0
	for _, c := range exp.Counterfactuals {
		if c.VerifiedBy == "rerun" {
			rerun++
			if c.RunID == "" {
				t.Errorf("rerun counterfactual %q has no run_id", c.Text)
			}
		}
	}
	if rerun == 0 {
		t.Fatalf("no counterfactual verified by rerun: %#v", exp.Counterfactuals)
	}
}

// TestFaithfulnessCheckerRejectsFreetext ensures the checker itself fails when a fake
// text embeds a number that has no fact source (the negative control qa-eval relies on).
func TestFaithfulnessCheckerRejectsFreetext(t *testing.T) {
	unsourced := Faithfulness("Скоринг 0.987 заявлен без источника", map[string]any{"score": 0.9})
	if len(unsourced) == 0 {
		t.Fatalf("0.987 should be flagged as unsourced")
	}
	if Faithfulness("Скоринг 0.9", map[string]any{"score": 0.9}) != nil {
		t.Fatalf("0.9 should be sourced")
	}
}
