package schedule

import (
	"reflect"
	"testing"
)

func TestCalculateIsDeterministicAndUsesCapacity(t *testing.T) {
	in := Input{
		Requirements:   []Requirement{{ID: "req_api", Effort: Effort{Min: 5, Likely: 5, Max: 5}}},
		Assignments:    []Assignment{{EmployeeID: "emp_a", RequirementID: "req_api", Week: "2026-W01", Hours: 40}},
		Capacity:       []Capacity{{EmployeeID: "emp_a", Week: "2026-W01", FreeHours: 40}},
		MonteCarloRuns: 200, Seed: 42,
	}
	a, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same input and seed must be byte-stable: %#v != %#v", a, b)
	}
	if a.Duration.P50WorkingDays != 5 || a.Duration.P80WorkingDays != 5 {
		t.Fatalf("unexpected duration: %#v", a.Duration)
	}

	// Ranged effort of several requirements: the per-requirement samples come from one
	// RNG stream, so they must not depend on Go's randomized map iteration order.
	ranged := Input{
		Requirements: []Requirement{
			{ID: "req_api", Effort: Effort{Min: 2, Likely: 6, Max: 12}},
			{ID: "req_ui", Effort: Effort{Min: 1, Likely: 3, Max: 9}},
			{ID: "req_qa", Effort: Effort{Min: 3, Likely: 4, Max: 7}},
		},
		Capacity: []Capacity{
			{EmployeeID: "emp_a", Week: "2026-W01", FreeHours: 40},
			{EmployeeID: "emp_a", Week: "2026-W02", FreeHours: 40},
			{EmployeeID: "emp_b", Week: "2026-W01", FreeHours: 40},
		},
		Assignments: []Assignment{
			{EmployeeID: "emp_a", RequirementID: "req_api", Week: "2026-W01", Hours: 40},
			{EmployeeID: "emp_a", RequirementID: "req_qa", Week: "2026-W02", Hours: 40},
			{EmployeeID: "emp_b", RequirementID: "req_ui", Week: "2026-W01", Hours: 40},
		},
		MonteCarloRuns: 200, Seed: 42,
	}
	first, err := Calculate(ranged)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		again, err := Calculate(ranged)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("attempt %d: ranged effort is not deterministic: %#v != %#v",
				attempt, first.Duration, again.Duration)
		}
	}
}

func TestCalculateDeadlineAndPortfolioImpact(t *testing.T) {
	deadline := 2.0
	in := Input{
		Requirements:  []Requirement{{ID: "req_api", Effort: Effort{Min: 8, Likely: 8, Max: 8}}},
		Assignments:   []Assignment{{EmployeeID: "emp_a", RequirementID: "req_api", Week: "2026-W01", Hours: 40}},
		Capacity:      []Capacity{{EmployeeID: "emp_a", Week: "2026-W01", FreeHours: 40}},
		Portfolio:     []PortfolioProject{{ProjectID: "prj_old", RemainingHours: 80, WeeklyCapacity: 40, Critical: true, EmployeeIDs: []string{"emp_a"}}},
		DeadlineWeeks: &deadline, MonteCarloRuns: 200, Seed: 7, PortfolioTauWorkingDays: 10,
	}
	r, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Duration.POnTime == nil || *r.Duration.POnTime != 1 {
		t.Fatalf("expected on-time probability 1, got %#v", r.Duration.POnTime)
	}
	if r.PortfolioImpact.TotalSlipDays != 5 || r.PortfolioImpact.CriticalAffected != 1 {
		t.Fatalf("unexpected portfolio impact: %#v", r.PortfolioImpact)
	}
}

func TestPortfolioSlipIsAttributedOnlyToProjectsSharingStaff(t *testing.T) {
	in := Input{
		Requirements:  []Requirement{{ID: "req_api", Effort: Effort{Min: 8, Likely: 8, Max: 8}}},
		Assignments:   []Assignment{{EmployeeID: "emp_a", RequirementID: "req_api", Week: "2026-W01", Hours: 40}},
		Capacity:      []Capacity{{EmployeeID: "emp_a", Week: "2026-W01", FreeHours: 40}},
		Portfolio: []PortfolioProject{
			{ProjectID: "prj_shared", RemainingHours: 80, WeeklyCapacity: 40, EmployeeIDs: []string{"emp_a"}},
			{ProjectID: "prj_unrelated", RemainingHours: 80, WeeklyCapacity: 40, EmployeeIDs: []string{"emp_b"}},
		},
		MonteCarloRuns: 100, Seed: 7, PortfolioTauWorkingDays: 10,
	}
	r, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.PortfolioImpact.TotalSlipDays != 5 || r.PortfolioImpact.ProjectsAffected != 1 {
		t.Fatalf("unrelated project was charged: %#v", r.PortfolioImpact)
	}
	if _, ok := r.PortfolioImpact.ProjectSlipDays["prj_unrelated"]; ok {
		t.Fatalf("unrelated project must have no slip: %#v", r.PortfolioImpact.ProjectSlipDays)
	}
}

func TestCalculateRejectsNoProgress(t *testing.T) {
	_, err := Calculate(Input{Requirements: []Requirement{{ID: "req_api", Effort: Effort{Min: 1, Likely: 1, Max: 1}}}, Capacity: []Capacity{{EmployeeID: "emp_a", Week: "2026-W01", Unavailable: true}}})
	if err == nil {
		t.Fatal("expected no-progress error")
	}
}
