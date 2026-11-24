package metrics

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func shiftTestConfig() ScheduleConfig {
	return ScheduleConfig{MonteCarloRuns: 200, Seed: 42, PortfolioTauWorkingDays: 10}
}

// The fixture effort points are degenerate (5/5/5 and 2/2/2 pd), so the schedule is
// hand-computable. variantA1 covers req_a with 30 h in W10 + 10 h in W11 (finishes at
// week 2) and req_b with 8 h in each week (finishes at week 2): base P50 = P80 = 2.
// Adding 8 h to req_a uses emp_aa01's remaining W10 capacity (30+8=38 h in W10, 10 h in
// W11 -> finish 1 + 2/10 = 1.2). Adding 8 h to req_b uses emp_bb02's remaining W11
// capacity (W10 is full: 8/8; W11 becomes 16 h -> finish 1 + 8/16 = 1.5). The new
// bottleneck is req_b, so P50 = P80 = 1.5 and the shift is 0.5 weeks.
func TestEstimateDeficitShiftHandComputed(t *testing.T) {
	snap := fixtureSnapshot(t, true)
	base, shifted, err := EstimateDeficitShift(snap, variantA1(), map[string]float64{
		reqB: 8, reqA: 8, // inserted out of order on purpose: placement must sort ids
	}, shiftTestConfig())
	if err != nil {
		t.Fatalf("estimate deficit shift: %v", err)
	}

	assertDuration(t, "base", base, 2, 2, 10, 10)
	assertDuration(t, "shifted", shifted, 1.5, 1.5, 8, 8)

	shift := base.P50Weeks - shifted.P50Weeks
	if math.Abs(shift-0.5) > 1e-6 {
		t.Fatalf("duration shift = %v, want 0.5 weeks", shift)
	}
	// The extra effort is 16 h / 8 = 2 person-days; the real schedule shift is 0.5
	// weeks. A person-days division would return 2 and be wrong.
	if math.Abs(shift-2) < 1e-6 {
		t.Fatal("duration shift must not be the person-days figure")
	}
	if base.MonteCarlo == nil || shifted.MonteCarlo == nil ||
		base.MonteCarlo.Seed != shifted.MonteCarlo.Seed ||
		base.MonteCarlo.Runs != shifted.MonteCarlo.Runs {
		t.Fatalf("base and shifted must use the same seed/config: %#v vs %#v", base.MonteCarlo, shifted.MonteCarlo)
	}
}

func assertDuration(t *testing.T, label string, got VariantDuration, p50, p80 float64, p50Days, p80Days int) {
	t.Helper()
	if math.Abs(got.P50Weeks-p50) > 1e-6 || math.Abs(got.P80Weeks-p80) > 1e-6 {
		t.Fatalf("%s P50/P80 = %v/%v, want %v/%v", label, got.P50Weeks, got.P80Weeks, p50, p80)
	}
	if got.P50WorkingDays != p50Days || got.P80WorkingDays != p80Days {
		t.Fatalf("%s working days = %v/%v, want %v/%v", label, got.P50WorkingDays, got.P80WorkingDays, p50Days, p80Days)
	}
}

func TestEstimateDeficitShiftDeterministic(t *testing.T) {
	snap := fixtureSnapshot(t, true)
	proposal := variantA1()
	cfg := shiftTestConfig()
	extraA := map[string]float64{reqA: 8, reqB: 8}
	extraB := make(map[string]float64)
	for _, id := range []string{reqB, reqA} {
		extraB[id] = 8
	}

	for i := 0; i < 5; i++ {
		baseA, shiftedA, err := EstimateDeficitShift(snap, proposal, extraA, cfg)
		if err != nil {
			t.Fatalf("estimate (a): %v", err)
		}
		baseB, shiftedB, err := EstimateDeficitShift(snap, proposal, extraB, cfg)
		if err != nil {
			t.Fatalf("estimate (b): %v", err)
		}
		if !reflect.DeepEqual(baseA, baseB) || !reflect.DeepEqual(shiftedA, shiftedB) {
			t.Fatalf("non-deterministic shift:\nbase %#v / %#v\nshifted %#v / %#v", baseA, baseB, shiftedA, shiftedB)
		}
	}
}

func TestEstimateDeficitShiftNoCapacitySentinel(t *testing.T) {
	snap := fixtureSnapshot(t, true)
	// emp_aa01 is the only member qualified for req_a and has 10 + 22 = 32 h of
	// remaining capacity. 1000 h cannot be placed, so no duration may be produced.
	_, _, err := EstimateDeficitShift(snap, variantA1(), map[string]float64{reqA: 1000}, shiftTestConfig())
	if !errors.Is(err, ErrNoCapacityForCounterfactual) {
		t.Fatalf("err = %v, want ErrNoCapacityForCounterfactual", err)
	}
}

func TestEstimateDeficitShiftEmptyExtraReturnsBaseTwice(t *testing.T) {
	snap := fixtureSnapshot(t, true)
	base, shifted, err := EstimateDeficitShift(snap, variantA1(), nil, shiftTestConfig())
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !reflect.DeepEqual(base, shifted) {
		t.Fatalf("empty extra hours: base %#v != shifted %#v", base, shifted)
	}
	assertDuration(t, "base", base, 2, 2, 10, 10)
}
