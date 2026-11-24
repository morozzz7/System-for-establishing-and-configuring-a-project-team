package metrics

import (
	"testing"

	"github.com/teamopt/backend/internal/runinput"
)

// TestBudgetCapFiresG4InMetrics pins the ordering of Pass 1: the gate reads c.cost, so
// cost must be derived before G4 is evaluated. Hand-computed costs from the fixture:
// var_a1 = 40 h * 1.9 + 16 h * 1.4 = 98.4; var_b1 = 40 h * 1.0 + 8 h * 1.4 = 51.2.
// With cap 80 only var_a1 exceeds the budget.
func TestBudgetCapFiresG4InMetrics(t *testing.T) {
	data := fixtureData(true)
	data.Constraints.BudgetCapUnits = 80
	snap, err := runinput.Build(data)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	result, err := ComputeRun(RunInput{
		Snapshot:  snap,
		Proposals: []Proposal{variantA1(), variantB1()},
		Schedule:  ScheduleConfig{MonteCarloRuns: 200, Seed: 42},
	})
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}

	overBudget, _ := result.VariantByID("var_a1")
	if overBudget.CostUnits != 98.4 {
		t.Fatalf("over-budget cost = %v, want 98.4", overBudget.CostUnits)
	}
	if overBudget.Gate.G4 != 1 {
		t.Fatalf("G4 = %d, want 1 for a variant above the cap", overBudget.Gate.G4)
	}
	if overBudget.GatePass {
		t.Fatalf("an over-budget variant must fail the gate")
	}
	if overBudget.Metrics.Score != 0 {
		t.Fatalf("gate failure must force score 0, got %v", overBudget.Metrics.Score)
	}

	withinBudget, _ := result.VariantByID("var_b1")
	if withinBudget.CostUnits != 51.2 {
		t.Fatalf("within-budget cost = %v, want 51.2", withinBudget.CostUnits)
	}
	if !withinBudget.GatePass || withinBudget.Gate.G4 != 0 {
		t.Fatalf("a variant below the cap must pass G4: %#v", withinBudget.Gate)
	}
	if withinBudget.Metrics.Score == 0 {
		t.Fatalf("the passing variant must keep a positive score")
	}
}
