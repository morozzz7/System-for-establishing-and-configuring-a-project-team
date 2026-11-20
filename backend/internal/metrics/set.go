package metrics

import (
	"sort"
)

// RosterJaccardDistance is the roster distance of two variants:
// 1 - |A∩B| / |A∪B| (1 = disjoint rosters, 0 = identical rosters).
func RosterJaccardDistance(a, b []string) float64 {
	setA := make(map[string]bool, len(a))
	for _, id := range a {
		setA[id] = true
	}
	setB := make(map[string]bool, len(b))
	for _, id := range b {
		setB[id] = true
	}
	intersection := 0
	for id := range setA {
		if setB[id] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return 1 - float64(intersection)/float64(union)
}

// Dominates reports whether metric vector a Pareto-dominates b: a >= b on every metric
// present in both vectors and strictly better on at least one (docs/METRICS.md rule 2,
// higher is better). Metrics missing from either vector are ignored, so a variant that
// dropped M4 is neither rewarded nor punished for the missing value.
func Dominates(a, b MetricValues) bool {
	shared := 0
	better := false
	for _, key := range Keys {
		av, aOK := a.value(key)
		bv, bOK := b.value(key)
		if !aOK || !bOK {
			continue
		}
		shared++
		if av < bv-dominanceTolerance {
			return false
		}
		if av > bv+dominanceTolerance {
			better = true
		}
	}
	return shared > 0 && better
}

// dominatedVariants returns the sorted ids of the gate-passing variants that another
// gate-passing variant Pareto-dominates. Gate-failed variants are reported separately:
// they are not part of a comparable set (docs/METRICS.md rule 1).
func dominatedVariants(variants []VariantResult) []string {
	out := []string{}
	for i := range variants {
		if !variants[i].GatePass {
			continue
		}
		for j := range variants {
			if i == j || !variants[j].GatePass {
				continue
			}
			if Dominates(variants[j].Metrics, variants[i].Metrics) {
				out = append(out, variants[i].VariantID)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// buildSetDiagnostics recomputes the run-level rules of docs/METRICS.md
// ("Rules for the set of 2-3 variants") from the authoritative metric vectors and the
// rosters. A diversity claim of the solver is never trusted.
func buildSetDiagnostics(variants []VariantResult, rosters map[string][]string) VariantSetDiagnostics {
	out := VariantSetDiagnostics{
		PairwiseJaccard:      map[string]float64{},
		DominatedVariantIDs:  dominatedVariants(variants),
		GateFailedVariantIDs: []string{},
	}
	for _, v := range variants {
		if !v.GatePass {
			out.GateFailedVariantIDs = append(out.GateFailedVariantIDs, v.VariantID)
		}
	}
	sort.Strings(out.GateFailedVariantIDs)

	ids := make([]string, 0, len(variants))
	for _, v := range variants {
		ids = append(ids, v.VariantID)
	}
	sort.Strings(ids)

	minDistance := 0.0
	first := true
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			distance := round6(RosterJaccardDistance(rosters[ids[i]], rosters[ids[j]]))
			out.PairwiseJaccard[ids[i]+"|"+ids[j]] = distance
			if first || distance < minDistance {
				minDistance = distance
				first = false
			}
		}
	}
	out.MinPairwiseJaccard = minDistance

	// Rule 3 applies to pairs, so a single feasible variant cannot violate it
	// (docs/METRICS.md allows "fewer variants with explicit deficits" on shortage).
	// An empty set satisfies the rule vacuously yet serves the user nothing, so it is
	// reported as not satisfying the variant-set rules.
	hasFeasible := len(ids) > len(out.GateFailedVariantIDs)
	out.DiversityRuleOK = hasFeasible && len(out.GateFailedVariantIDs) == 0 &&
		(len(ids) < 2 || minDistance >= DiversityThreshold)
	return out
}
