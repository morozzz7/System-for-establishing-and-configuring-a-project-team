package metrics

import (
	"reflect"
	"testing"
)

// TestRosterJaccardDistance checks rule 3 of docs/METRICS.md ("variants differ in
// roster: Jaccard distance >= 0.25") on hand-computed rosters.
func TestRosterJaccardDistance(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []string
		expected float64
		why      string
	}{
		{"identical", []string{"emp_a", "emp_b"}, []string{"emp_b", "emp_a"}, 0, "same set, only order differs"},
		{"disjoint", []string{"emp_a", "emp_b"}, []string{"emp_c"}, 1, "1 - 0/3"},
		{"partial", []string{"emp_a", "emp_b"}, []string{"emp_b", "emp_c"}, 0.666667, "1 - 1/3"},
		{"subset", []string{"emp_a", "emp_b"}, []string{"emp_a"}, 0.5, "1 - 1/2"},
		{"empty vs empty", nil, nil, 0, "an undefined distance is reported as 0"},
		{"empty vs one", nil, []string{"emp_a"}, 1, "1 - 0/1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RosterJaccardDistance(tc.a, tc.b)
			if diff := got - tc.expected; diff > 1e-6 || diff < -1e-6 {
				t.Fatalf("distance = %v, want %v (%s)", got, tc.expected, tc.why)
			}
			// The distance is symmetric.
			reverse := RosterJaccardDistance(tc.b, tc.a)
			if diff := reverse - got; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("distance is not symmetric: %v vs %v", got, reverse)
			}
		})
	}
}

// fullMetrics returns a metric vector where every metric has the same value.
func fullMetrics(value float64) MetricValues {
	v := value
	return MetricValues{
		M1: value, M2: value, M3: value, M4: &v, M5: value, M6: value,
		M7: value, M8: value, M9: value, M10: value, M11: value, M12: value,
		Score: value,
	}
}

// TestDominates covers the Pareto comparison behind rule 2 ("variants are non-dominated
// on the vector (M1..M12); otherwise the weakest is replaced").
func TestDominates(t *testing.T) {
	better := fullMetrics(0.8)
	worse := fullMetrics(0.5)

	mixed := fullMetrics(0.5)
	mixed.M1 = 1 // better on M1, worse everywhere else

	same := fullMetrics(0.8)

	noM4 := better
	noM4.M4 = nil

	tests := []struct {
		name     string
		a, b     MetricValues
		expected bool
		why      string
	}{
		{"better everywhere", better, worse, true, "a >= b on all metrics and > on most"},
		{"equal", same, better, false, "Pareto domination needs at least one strict improvement"},
		{"mixed", mixed, better, false, "a is worse on M2..M12"},
		{"reverse", worse, better, false, "a is worse on every metric"},
		{"missing metric is ignored", noM4, better, false, "the only difference (M4) is not shared"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Dominates(tc.a, tc.b); got != tc.expected {
				t.Fatalf("Dominates = %v, want %v (%s)", got, tc.expected, tc.why)
			}
		})
	}

	// A variant that fills a missing metric cannot dominate on a metric pair it never
	// shared: with M4 absent on both sides only M1..M3,M5..M12 decide.
	m4only := fullMetrics(0.8)
	m4only.M4 = nil
	withM4 := fullMetrics(0.8)
	if Dominates(m4only, withM4) {
		t.Fatalf("equal shared metrics must not dominate")
	}
	m4only.M1 = 0.9
	if !Dominates(m4only, withM4) {
		t.Fatalf("a strict improvement on a shared metric must dominate")
	}
	if Dominates(withM4, m4only) {
		t.Fatalf("a variant that is worse on a shared metric must not dominate")
	}
}

func variantResult(id string, gatePass bool, metrics MetricValues) VariantResult {
	return VariantResult{VariantID: id, GatePass: gatePass, Metrics: metrics}
}

// TestBuildSetDiagnostics recomputes the run-level rules from rosters and metric vectors.
func TestBuildSetDiagnostics(t *testing.T) {
	fast := fullMetrics(0.9)
	fast.M6 = 0.1 // cheap but with a poor cost score: trade-off kept explicit
	risky := fullMetrics(0.7)
	risky.M5 = 1 // faster than "fast" on schedule, so neither dominates

	variants := []VariantResult{
		variantResult("var_a", true, fast),
		variantResult("var_b", true, risky),
	}
	rosters := map[string][]string{
		"var_a": {"emp_a", "emp_b"},
		"var_b": {"emp_c", "emp_d"},
	}
	out := buildSetDiagnostics(variants, rosters)
	if len(out.DominatedVariantIDs) != 0 {
		t.Fatalf("neither variant dominates the other: %v", out.DominatedVariantIDs)
	}
	if len(out.GateFailedVariantIDs) != 0 {
		t.Fatalf("no gate failure expected: %v", out.GateFailedVariantIDs)
	}
	if out.MinPairwiseJaccard != 1 {
		t.Fatalf("min_pairwise_jaccard = %v, want 1 (disjoint rosters)", out.MinPairwiseJaccard)
	}
	if got, ok := out.PairwiseJaccard["var_a|var_b"]; !ok || got != 1 {
		t.Fatalf("pairwise_jaccard = %v, want var_a|var_b = 1", out.PairwiseJaccard)
	}
	if !out.DiversityRuleOK {
		t.Fatalf("two disjoint, non-dominated variants satisfy the variant-set rules")
	}

	// A dominated variant is reported (rule 2), the set itself stays diverse.
	variants = append(variants, variantResult("var_c", true, fullMetrics(0.2)))
	rosters["var_c"] = []string{"emp_c"}
	out = buildSetDiagnostics(variants, rosters)
	if !reflect.DeepEqual(out.DominatedVariantIDs, []string{"var_c"}) {
		t.Fatalf("dominated_variant_ids = %v, want [var_c]", out.DominatedVariantIDs)
	}
	if out.MinPairwiseJaccard != 0.5 {
		t.Fatalf("min_pairwise_jaccard = %v, want 0.5 (var_b vs var_c: 1 - 1/2)", out.MinPairwiseJaccard)
	}
	if !out.DiversityRuleOK {
		t.Fatalf("domination does not violate the roster diversity rule")
	}

	// Identical rosters violate rule 3 (Jaccard distance 0 < 0.25).
	rosters["var_c"] = []string{"emp_c", "emp_d"}
	out = buildSetDiagnostics(variants, rosters)
	if out.MinPairwiseJaccard != 0 {
		t.Fatalf("min_pairwise_jaccard = %v, want 0", out.MinPairwiseJaccard)
	}
	if out.DiversityRuleOK {
		t.Fatalf("two variants with the same roster are one variant (rule 3)")
	}

	// A gate-failed variant breaks rule 1 even when the rosters differ.
	variants[1] = variantResult("var_b", false, risky)
	out = buildSetDiagnostics(variants, rosters)
	if !reflect.DeepEqual(out.GateFailedVariantIDs, []string{"var_b"}) {
		t.Fatalf("gate_failed_variant_ids = %v, want [var_b]", out.GateFailedVariantIDs)
	}
	if out.DiversityRuleOK {
		t.Fatalf("a gate failure must break the variant-set rules")
	}

	// An empty set satisfies no rule: it serves the user nothing.
	out = buildSetDiagnostics(nil, nil)
	if out.DiversityRuleOK {
		t.Fatalf("an empty variant set cannot satisfy the variant-set rules")
	}
	if out.MinPairwiseJaccard != 0 || len(out.PairwiseJaccard) != 0 {
		t.Fatalf("empty diagnostics expected: %#v", out)
	}

	// A single feasible variant is allowed on shortage (explicit deficits) and does not
	// violate the pairwise diversity rule.
	out = buildSetDiagnostics([]VariantResult{variantResult("var_a", true, fast)}, rosters)
	if !out.DiversityRuleOK {
		t.Fatalf("a single feasible variant does not violate the pairwise rule")
	}
}
