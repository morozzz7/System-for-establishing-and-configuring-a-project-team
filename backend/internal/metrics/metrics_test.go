package metrics

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/teamopt/backend/internal/runinput"
)

// TestComputeRunHandComputedMetrics checks every metric of docs/METRICS.md against
// arithmetic that can be reproduced on paper from the fixture comment.
func TestComputeRunHandComputedMetrics(t *testing.T) {
	result, err := ComputeRun(fixtureRunInput(t, true))
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}
	if len(result.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(result.Variants))
	}
	a1, ok := result.VariantByID("var_a1")
	if !ok {
		t.Fatalf("var_a1 missing")
	}
	b1, ok := result.VariantByID("var_b1")
	if !ok {
		t.Fatalf("var_b1 missing")
	}

	tests := []struct {
		metric   string
		variant  string
		got      float64
		expected float64
		why      string
	}{
		// ---------- var_a1 ----------
		// cov_a = min(1, 5/3) = 1 (emp_aa01); cov_b = 1 (emp_bb02 holds qa_engineer).
		// M1 = (1.0*1 + 0.5*1) / 1.5 = 1.
		{"M1", "var_a1", a1.Metrics.M1, 1, "(1*1 + 0.5*1)/1.5"},
		// req_a: 40 h * q=min(1,5/3)=1 -> 5 pd (clamped at E=5); req_b: 16 h -> 2 pd.
		// M2 = (5+2)/(5+2) = 1.
		{"M2", "var_a1", a1.Metrics.M2, 1, "7 pd covered / 7 pd likely"},
		// req_a rows: fit = 1 - (5-3)/4 = 0.5 (5 pd); req_b rows: role-only fit = 1 (2 pd).
		// M3 = (5*0.5 + 2*1) / 7 = 4.5/7.
		{"M3", "var_a1", a1.Metrics.M3, 0.642857, "(5*0.5 + 2*1)/7 = 4.5/7"},
		// Only emp_aa01 has history: exp 0.5, pd = 40/8 = 5 -> M4 = 5*0.5/5 = 0.5.
		{"M4", "var_a1", deref(a1.Metrics.M4), 0.5, "5 pd * 0.5 / 5 pd"},
		// No deadline -> M5 = T_best/T50 = 2/2 = 1 (T_best is var_a1's own P50).
		{"M5", "var_a1", a1.Metrics.M5, 1, "T_best 2 / T50 2"},
		// C = 40 h*1.9 + 16 h*1.4 = 76 + 22.4 = 98.4; C_min = 51.2; C_ref = 98.4.
		// M6 = 1 - (98.4-51.2)/(98.4-51.2) = 0.
		{"M6", "var_a1", a1.Metrics.M6, 0, "1 - 47.2/47.2"},
		// u: aa01 30/40=0.75, (8+10)/40=0.45; bb02 (32+8)/40=1.0, 8/40=0.2.
		// overheat = (0+0+1+0)/4 = 0.25 -> M7 = 0.75.
		{"M7", "var_a1", a1.Metrics.M7, 0.75, "1 - 1/4"},
		// pulled = min(30,40)+min(10,32)+min(8,8)+min(8,40) = 56 h -> slip = 56/40*5 = 7 d.
		// M8 = exp(-7/10).
		{"M8", "var_a1", a1.Metrics.M8, 0.496585, "exp(-7/10)"},
		// must req_a: only emp_aa01 has lang_go >= 3 -> N=1 < 2 -> M9 = 0/1.
		{"M9", "var_a1", a1.Metrics.M9, 0, "0 of 1 must requirements have two qualified members"},
		// frag = 0/4 (no row below 8 h); ctx = min(1,(0.5-1)/3) = 0;
		// n_opt = ceil(7/(2*5*0.8)) = 1; size_pen = (2-1)/1 = 1 -> M10 = 1-0.3.
		{"M10", "var_a1", a1.Metrics.M10, 0.7, "1 - 0.3*1"},
		// bench available = 72 (aa01) + 80 (cc03) + 48 (dd04) = 200 h; assigned = 40 h.
		{"M11", "var_a1", a1.Metrics.M11, 0.2, "40/200"},
		// Dropout aa01 loses the only lang_go holder; dropout bb02 pushes req_b never to
		// finish (T80 3 > 1.2*2) -> 0 of 2 survive.
		{"M12", "var_a1", a1.Metrics.M12, 0, "0/2 scenarios survive"},
		// score = 0.2*1 + 0.1*1 + 0.1*(4.5/7) + 0.1*0.5 + 0.1*1 + 0.1*0 + 0.1*0.75
		//       + 0.05*exp(-0.7) + 0.05*0 + 0.05*0.7 + 0.025*0.2 + 0.025*0
		//       = 0.65411495... (weights sum to 1.0, every metric present).
		{"score", "var_a1", a1.Metrics.Score, 0.654115, "weighted mean over 12 present metrics"},

		// ---------- var_b1 ----------
		// cov_a: emp_cc03 level 3 vs required 3 -> 1; cov_b: emp_dd04 holds qa_engineer -> 1.
		{"M1", "var_b1", b1.Metrics.M1, 1, "(1*1 + 0.5*1)/1.5"},
		// req_a: 40 h -> 5 pd; req_b: 8 h -> 1 pd. M2 = (5+1)/(5+2) = 6/7.
		{"M2", "var_b1", b1.Metrics.M2, 0.857143, "6/7"},
		// req_a row: level 3 vs 3 -> fit 1 (5 pd); req_b: role-only fit 1 (1 pd) -> M3 = 1.
		{"M3", "var_b1", b1.Metrics.M3, 1, "7 pd * fit 1 / 7 pd"},
		// Members with history: emp_cc03 exp 0.1 (5 pd) only -> M4 = 0.1.
		{"M4", "var_b1", deref(b1.Metrics.M4), 0.1, "5 pd * 0.1 / 5 pd"},
		// T50 = 3 (req_b never finishes), T_best = 2 -> M5 = 2/3.
		{"M5", "var_b1", b1.Metrics.M5, 0.666667, "2/3"},
		// C = 40*1.0 + 8*1.4 = 51.2 = C_min -> M6 = 1.
		{"M6", "var_b1", b1.Metrics.M6, 1, "1 - 0/(47.2)"},
		// cc03: 40/40=1.0, 0; dd04: (16+8)/40=0.6, 16/40=0.4 -> overheat 1/4 -> M7 = 0.75.
		{"M7", "var_b1", b1.Metrics.M7, 0.75, "1 - 1/4"},
		// pulled = min(40,40)+min(8,24) = 48 h -> slip = 48/40*5 = 6 d -> exp(-6/10).
		{"M8", "var_b1", b1.Metrics.M8, 0.548812, "exp(-6/10)"},
		// only emp_cc03 has lang_go >= 3 -> N=1 -> M9 = 0.
		{"M9", "var_b1", b1.Metrics.M9, 0, "0 of 1"},
		// frag = 0/2 (40 h and 8 h, neither < 8); current projects per member: cc03 0,
		// dd04 2 -> mean 1 -> ctx = (1-1)/3 = 0; n_opt = ceil(7/8) = 1;
		// size_pen = (2-1)/1 = 1 -> M10 = 1 - 0.3*1 = 0.7.
		{"M10", "var_b1", b1.Metrics.M10, 0.7, "1 - 0.3*1"},
		// bench assigned = 40 (cc03) + 8 (dd04) = 48 h -> 48/200.
		{"M11", "var_b1", b1.Metrics.M11, 0.24, "48/200"},
		// dropout cc03 loses the only lang_go holder; dropout dd04 keeps T80 3 <= 1.2*3.
		{"M12", "var_b1", b1.Metrics.M12, 0.5, "1/2 scenarios survive"},
		// score = 0.2*1 + 0.1*(6/7) + 0.1*1 + 0.1*0.1 + 0.1*(2/3) + 0.1*1 + 0.1*0.75
		//       + 0.05*exp(-0.6) + 0.05*0 + 0.05*0.7 + 0.025*0.24 + 0.025*0.5
		//       = 0.71832158... -> 0.718322
		{"score", "var_b1", b1.Metrics.Score, 0.718322, "weighted mean over 12 present metrics"},
	}

	for _, tc := range tests {
		t.Run(tc.variant+"_"+tc.metric, func(t *testing.T) {
			if diff := tc.expected - tc.got; diff > 1e-6 || diff < -1e-6 {
				t.Fatalf("%s of %s = %v, want %v (%s)", tc.metric, tc.variant, tc.got, tc.expected, tc.why)
			}
		})
	}

	// Every metric of the contract is present when history exists.
	if a1.Metrics.M4 == nil {
		t.Fatalf("M4 must be present when a candidate carries an experience score")
	}
}

// TestComputeRunHandComputedDetails checks the diagnostics, duration, cost and deficits.
func TestComputeRunHandComputedDetails(t *testing.T) {
	result, err := ComputeRun(fixtureRunInput(t, true))
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}
	a1, _ := result.VariantByID("var_a1")
	b1, _ := result.VariantByID("var_b1")

	// var_a1: req_a finishes at 1 + 10/10 = 2.0 (30 h then 10 h of 40 h);
	// req_b finishes at 1 + 8/8 = 2.0 -> P50 = P80 = 2 weeks = 10 working days.
	if a1.Duration.P50Weeks != 2 || a1.Duration.P80Weeks != 2 {
		t.Fatalf("var_a1 duration = %v/%v, want 2/2", a1.Duration.P50Weeks, a1.Duration.P80Weeks)
	}
	if a1.Duration.P50WorkingDays != 10 || a1.Duration.P80WorkingDays != 10 {
		t.Fatalf("var_a1 working days = %d/%d, want 10/10", a1.Duration.P50WorkingDays, a1.Duration.P80WorkingDays)
	}
	if a1.Duration.DeadlineWeek != "" || a1.Duration.POnTime != nil || a1.Duration.SlackWeeks != nil {
		t.Fatalf("no deadline: deadline fields must be absent, got %#v", a1.Duration)
	}
	if a1.Duration.MonteCarlo == nil || a1.Duration.MonteCarlo.Runs != 200 || a1.Duration.MonteCarlo.Seed != 42 {
		t.Fatalf("monte carlo info missing: %#v", a1.Duration.MonteCarlo)
	}

	// var_b1: req_a finishes at 40/40 = 1.0; req_b gets only 8 of 16 h -> never finishes
	// inside the horizon -> completion 3 -> P50 = P80 = 3 weeks = 15 working days.
	if b1.Duration.P50Weeks != 3 || b1.Duration.P80Weeks != 3 {
		t.Fatalf("var_b1 duration = %v/%v, want 3/3", b1.Duration.P50Weeks, b1.Duration.P80Weeks)
	}
	if b1.Duration.P80WorkingDays != 15 {
		t.Fatalf("var_b1 P80 working days = %d, want 15", b1.Duration.P80WorkingDays)
	}

	if a1.CostUnits != 98.4 {
		t.Fatalf("var_a1 cost = %v, want 98.4", a1.CostUnits)
	}
	if b1.CostUnits != 51.2 {
		t.Fatalf("var_b1 cost = %v, want 51.2", b1.CostUnits)
	}

	// var_a1 covers both requirements at likely effort -> no deficit.
	if a1.DeficitCount != 0 || len(a1.Deficits) != 0 {
		t.Fatalf("var_a1 must have no deficit, got %#v", a1.Deficits)
	}
	// var_b1 covers 1 of 2 likely person-days for the should requirement req_b.
	if b1.DeficitCount != 1 {
		t.Fatalf("var_b1 deficit count = %d, want 1", b1.DeficitCount)
	}
	if b1.Deficits[0].RequirementID != reqB || b1.Deficits[0].PersonDays != 1 {
		t.Fatalf("var_b1 deficit = %#v, want req_b with 1 person-day", b1.Deficits[0])
	}
	if b1.Deficits[0].ExternalCooperationFlag {
		t.Fatalf("a should-requirement deficit must not raise the external flag")
	}

	if !a1.GatePass || !b1.GatePass {
		t.Fatalf("both variants must pass the gate: %#v %#v", a1.Gate, b1.Gate)
	}

	details := a1.Details
	checkFloat(t, "must_coverage", ptr(details.MustCoverage), 1)
	checkFloat(t, "overqualified_share", ptr(details.OverqualifiedShare), 0.714286)
	checkFloat(t, "peak_utilization", ptr(details.PeakUtilization), 1)
	checkFloat(t, "mean_utilization", ptr(details.MeanUtilization), 0.6)
	checkFloat(t, "utilization_band_60_90_share", ptr(details.UtilizationBand6090Share), 0.25)
	checkInt(t, "projects_affected", ptrInt(details.ProjectsAffected), 1)
	checkInt(t, "critical_projects_affected", ptrInt(details.CriticalProjectsAffected), 1)
	checkInt(t, "min_bus_factor", ptrInt(details.MinBusFactor), 1)
	checkFloat(t, "fragmentation_share", ptr(details.FragmentationShare), 0)
	checkFloat(t, "concurrent_projects_mean", ptr(details.ConcurrentProjectsMean), 0.5)
	checkInt(t, "n_opt", ptrInt(details.NOpt), 1)
	checkFloat(t, "bench_hours_assigned", ptr(details.BenchHoursAssigned), 40)
	checkFloat(t, "bench_hours_available", ptr(details.BenchHoursAvailable), 200)
	checkFloat(t, "cost_min_units", ptr(details.CostMinUnits), 51.2)
	checkFloat(t, "cost_ref_units", ptr(details.CostRefUnits), 98.4)
	checkFloat(t, "t_best_weeks", ptr(details.TBestWeeks), 2)
	checkFloat(t, "portfolio_slip_days", ptr(details.PortfolioSlipDays), 7)
	checkFloat(t, "resilience_score", ptr(details.ResilienceScore), 0)
	if !reflect.DeepEqual(details.SinglePointsOfFailure, []string{"lang_go"}) {
		t.Fatalf("single_points_of_failure = %v, want [lang_go]", details.SinglePointsOfFailure)
	}
	if len(details.DroppedMetrics) != 0 {
		t.Fatalf("no metric may be dropped when history exists: %v", details.DroppedMetrics)
	}

	bDetails := b1.Details
	checkFloat(t, "b1 fragmentation_share", ptr(bDetails.FragmentationShare), 0)
	checkFloat(t, "b1 concurrent_projects_mean", ptr(bDetails.ConcurrentProjectsMean), 1)
	checkFloat(t, "b1 mean_utilization", ptr(bDetails.MeanUtilization), 0.5)
	checkFloat(t, "b1 bench_hours_assigned", ptr(bDetails.BenchHoursAssigned), 48)
	checkFloat(t, "b1 portfolio_slip_days", ptr(bDetails.PortfolioSlipDays), 6)
	checkFloat(t, "b1 resilience_score", ptr(bDetails.ResilienceScore), 0.5)
	checkFloat(t, "b1 overqualified_share", ptr(bDetails.OverqualifiedShare), 0)
}

// TestM4DroppedRenormalizesWeights proves the ADR-007 rule: without project history M4
// is dropped, weights are renormalized over the metrics that remain and the score is
// still exactly reproducible from the served values.
func TestM4DroppedRenormalizesWeights(t *testing.T) {
	full, err := ComputeRun(fixtureRunInput(t, true))
	if err != nil {
		t.Fatalf("compute run with history: %v", err)
	}
	dropped, err := ComputeRun(fixtureRunInput(t, false))
	if err != nil {
		t.Fatalf("compute run without history: %v", err)
	}

	a1Full, _ := full.VariantByID("var_a1")
	a1, _ := dropped.VariantByID("var_a1")
	if a1.Metrics.M4 != nil {
		t.Fatalf("M4 must be dropped when no candidate has an experience score")
	}
	if !reflect.DeepEqual(a1.Details.DroppedMetrics, []string{"M4"}) {
		t.Fatalf("dropped_metrics = %v, want [M4]", a1.Details.DroppedMetrics)
	}
	// Dropping M4 must not change any other component.
	if a1.Metrics.M1 != a1Full.Metrics.M1 || a1.Metrics.M2 != a1Full.Metrics.M2 ||
		a1.Metrics.M3 != a1Full.Metrics.M3 || a1.Metrics.M5 != a1Full.Metrics.M5 ||
		a1.Metrics.M6 != a1Full.Metrics.M6 || a1.Metrics.M7 != a1Full.Metrics.M7 ||
		a1.Metrics.M8 != a1Full.Metrics.M8 || a1.Metrics.M9 != a1Full.Metrics.M9 ||
		a1.Metrics.M10 != a1Full.Metrics.M10 || a1.Metrics.M11 != a1Full.Metrics.M11 ||
		a1.Metrics.M12 != a1Full.Metrics.M12 {
		t.Fatalf("dropping M4 changed another metric: %#v vs %#v", a1.Metrics, a1Full.Metrics)
	}

	// var_a1: sum(w*M) without M4 = 0.65411495 - 0.1*0.5 = 0.60411495; the weights that
	// remain sum to 0.9 -> score = 0.60411495 / 0.9 = 0.67123883... -> 0.671239.
	if diff := a1.Metrics.Score - 0.671239; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("renormalized score = %v, want 0.671239", a1.Metrics.Score)
	}
	// var_b1: sum(w*M) without M4 = 0.7183216 - 0.1*0.1 = 0.7083216 -> /0.9 = 0.787024.
	b1, _ := dropped.VariantByID("var_b1")
	if diff := b1.Metrics.Score - 0.787024; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("renormalized score = %v, want 0.787024", b1.Metrics.Score)
	}

	// The identity that makes the score auditable: score * sum(present weights)
	// == sum(present weight * metric).
	weights := fixtureWeights()
	sumWeight, sumWeighted := 0.0, 0.0
	for _, key := range Keys {
		value, present := a1.Metrics.value(key)
		if !present {
			continue
		}
		sumWeight += weights[key]
		sumWeighted += weights[key] * value
	}
	if diff := a1.Metrics.Score*sumWeight - sumWeighted; diff > 1e-5 || diff < -1e-5 {
		t.Fatalf("score is not the renormalized weighted mean: %v*%v != %v", a1.Metrics.Score, sumWeight, sumWeighted)
	}

	// The dropped metric must not appear in the serialized vector.
	encoded, err := json.Marshal(a1.Metrics)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}
	if _, present := raw["M4"]; present {
		t.Fatalf("dropped M4 must be absent from the JSON: %s", encoded)
	}
	fullEncoded, err := json.Marshal(a1Full.Metrics)
	if err != nil {
		t.Fatalf("marshal full metrics: %v", err)
	}
	var fullRaw map[string]any
	if err := json.Unmarshal(fullEncoded, &fullRaw); err != nil {
		t.Fatalf("unmarshal full metrics: %v", err)
	}
	if _, present := fullRaw["M4"]; !present {
		t.Fatalf("present M4 must be serialized: %s", fullEncoded)
	}
}

// TestGateFailureZeroesScore proves Score = Gate * sum(lambda*M): a single G4 violation
// makes the variant non-existent for the user, whatever the metric vector says.
func TestGateFailureZeroesScore(t *testing.T) {
	data := fixtureData(true)
	data.Constraints.ForceExclude = []string{"emp_aa01"}
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
	a1, _ := result.VariantByID("var_a1")
	if a1.GatePass {
		t.Fatalf("force-exclude of a member must fail the gate")
	}
	if a1.Gate.G4 != 1 {
		t.Fatalf("G4 = %d, want 1", a1.Gate.G4)
	}
	if a1.Metrics.Score != 0 {
		t.Fatalf("gate failure must force score 0, got %v", a1.Metrics.Score)
	}
	// The metric vector is still computed and truthful (the UI shows it next to the
	// reason codes), only the score is gated.
	if a1.Metrics.M1 == 0 && a1.Metrics.M2 == 0 {
		t.Fatalf("metric vector must still be computed: %#v", a1.Metrics)
	}
	b1, _ := result.VariantByID("var_b1")
	if !b1.GatePass || b1.Metrics.Score == 0 {
		t.Fatalf("unaffected variant must keep its score: %#v", b1.Gate)
	}
	if !reflect.DeepEqual(result.Set.GateFailedVariantIDs, []string{"var_a1"}) {
		t.Fatalf("gate_failed_variant_ids = %v, want [var_a1]", result.Set.GateFailedVariantIDs)
	}
	if result.Set.DiversityRuleOK {
		t.Fatalf("a gate failure must break the diversity/variant-set rule")
	}
}

// TestM5UsesDeadlineOnTimeProbability covers the other M5 interpretation: with a
// deadline, M5 = P(T <= D) instead of T_best/T50.
func TestM5UsesDeadlineOnTimeProbability(t *testing.T) {
	data := fixtureData(true)
	data.Constraints.DeadlineWeek = "2026-W11" // index 1 -> 2 planning weeks
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
	a1, _ := result.VariantByID("var_a1")
	b1, _ := result.VariantByID("var_b1")

	// var_a1 finishes exactly in week 2 -> every sample is on time -> M5 = 1.
	if a1.Duration.POnTime == nil || *a1.Duration.POnTime != 1 || a1.Metrics.M5 != 1 {
		t.Fatalf("var_a1 on-time = %v, M5 = %v, want 1/1", a1.Duration.POnTime, a1.Metrics.M5)
	}
	// slack = (D - T80)/D = (2-2)/2 = 0.
	if a1.Duration.SlackWeeks == nil || *a1.Duration.SlackWeeks != 0 {
		t.Fatalf("var_a1 slack = %v, want 0", a1.Duration.SlackWeeks)
	}
	// var_b1 finishes in week 3 -> no sample is on time -> M5 = 0, slack = (2-3)/2 = -0.5.
	if b1.Duration.POnTime == nil || *b1.Duration.POnTime != 0 || b1.Metrics.M5 != 0 {
		t.Fatalf("var_b1 on-time = %v, M5 = %v, want 0/0", b1.Duration.POnTime, b1.Metrics.M5)
	}
	if b1.Duration.SlackWeeks == nil || *b1.Duration.SlackWeeks != -0.5 {
		t.Fatalf("var_b1 slack = %v, want -0.5", b1.Duration.SlackWeeks)
	}
	// No T_best reference is needed (and reported) when a deadline exists.
	if a1.Details.TBestWeeks != nil || b1.Details.TBestWeeks != nil {
		t.Fatalf("t_best_weeks must be absent when a deadline is set")
	}
	if a1.Duration.DeadlineWeek != "2026-W11" {
		t.Fatalf("deadline_week = %q, want 2026-W11", a1.Duration.DeadlineWeek)
	}
}

// ---------- dedicated snapshots for M9 and M12 ----------

func capacityWeeks(weeks []string, assigned map[string]float64) []runinput.CapacityRow {
	rows := make([]runinput.CapacityRow, 0, len(weeks))
	for _, week := range weeks {
		used := assigned[week]
		rows = append(rows, runinput.CapacityRow{
			Week: week, WorkingDays: 5, NormHours: 40,
			AssignedHours: used, FreeHours: 40 - used,
		})
	}
	return rows
}

func simpleWeights() map[string]float64 {
	return map[string]float64{
		"M1": 0.15, "M2": 0.13, "M3": 0.10, "M4": 0.08, "M5": 0.15, "M6": 0.10,
		"M7": 0.08, "M8": 0.08, "M9": 0.05, "M10": 0.04, "M11": 0.02, "M12": 0.02,
	}
}

// TestM9BusFactor counts must requirements with at least two qualified members and
// treats a role-only requirement as qualified by role profile.
func TestM9BusFactor(t *testing.T) {
	langGo := "lang_go"
	weeks := []string{"2026-W10"}
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_t0009", BriefID: "brf_t0009", Preset: "balanced", K: 2, Seed: 7,
		Horizon: weeks, Weights: simpleWeights(),
		Requirements: []runinput.Requirement{
			{RequirementID: "req_go", RoleID: "backend_engineer", CompetencyID: &langGo, Level: 3,
				Criticality: "must", Headcount: 1,
				Effort: runinput.Effort{MinPersonDays: 2, LikelyPersonDays: 2, MaxPersonDays: 2}, AllowsExternal: true},
			{RequirementID: "req_qa", RoleID: "qa_engineer", Level: 2, Criticality: "must", Headcount: 1,
				Effort: runinput.Effort{MinPersonDays: 1, LikelyPersonDays: 1, MaxPersonDays: 1}, AllowsExternal: true},
		},
		Candidates: []runinput.Candidate{
			{EmployeeID: "emp_p001", Grade: "S", CostCoefficient: 1.9, RoleProfileID: "backend_engineer",
				EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{"lang_go": 4}, Capacity: capacityWeeks(weeks, nil)},
			{EmployeeID: "emp_p002", Grade: "M", CostCoefficient: 1.4, RoleProfileID: "backend_engineer",
				EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{"lang_go": 3}, Capacity: capacityWeeks(weeks, nil)},
			{EmployeeID: "emp_q001", Grade: "M", CostCoefficient: 1.4, RoleProfileID: "qa_engineer",
				EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{}, Capacity: capacityWeeks(weeks, nil)},
			{EmployeeID: "emp_q002", Grade: "J", CostCoefficient: 1.0, RoleProfileID: "qa_engineer",
				EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{}, Capacity: capacityWeeks(weeks, nil)},
		},
		Constraints: runinput.Constraints{MaxTeamSize: 4, MaxUtilizationRatio: 0.95},
		CreatedAt:   time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	assignments := []ProposalAssignment{
		// 16 h = 2 person-days of req_go (min = likely = 2).
		{EmployeeID: "emp_p001", RequirementID: "req_go", Week: "2026-W10", Hours: 16},
		// 8 h = 1 person-day of req_qa.
		{EmployeeID: "emp_q001", RequirementID: "req_qa", Week: "2026-W10", Hours: 8},
	}
	result, err := ComputeRun(RunInput{
		Snapshot: snap,
		Proposals: []Proposal{
			{
				VariantID: "var_short", Label: "short",
				Members: []ProposalMember{
					{EmployeeID: "emp_p001", RoleID: "backend_engineer", Grade: "S"},
					{EmployeeID: "emp_p002", RoleID: "backend_engineer", Grade: "M"},
					{EmployeeID: "emp_q001", RoleID: "qa_engineer", Grade: "M"},
				},
				Assignments: assignments,
			},
			{
				VariantID: "var_full", Label: "full",
				Members: []ProposalMember{
					{EmployeeID: "emp_p001", RoleID: "backend_engineer", Grade: "S"},
					{EmployeeID: "emp_p002", RoleID: "backend_engineer", Grade: "M"},
					{EmployeeID: "emp_q001", RoleID: "qa_engineer", Grade: "M"},
					{EmployeeID: "emp_q002", RoleID: "qa_engineer", Grade: "J"},
				},
				Assignments: assignments,
			},
		},
		Schedule: ScheduleConfig{MonteCarloRuns: 200, Seed: 7},
	})
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}

	// req_go: p001 and p002 are both >= level 3 -> bus factor 2.
	// req_qa: only q001 holds the role -> bus factor 1.
	short, _ := result.VariantByID("var_short")
	if short.Metrics.M9 != 0.5 {
		t.Fatalf("M9 = %v, want 0.5 (1 of 2 must requirements covered twice)", short.Metrics.M9)
	}
	checkInt(t, "min_bus_factor", ptrInt(short.Details.MinBusFactor), 1)
	if !reflect.DeepEqual(short.Details.SinglePointsOfFailure, []string{"qa_engineer"}) {
		t.Fatalf("single_points_of_failure = %v, want [qa_engineer]", short.Details.SinglePointsOfFailure)
	}

	full, _ := result.VariantByID("var_full")
	if full.Metrics.M9 != 1 {
		t.Fatalf("M9 = %v, want 1 (both must requirements covered twice)", full.Metrics.M9)
	}
	checkInt(t, "min_bus_factor", ptrInt(full.Details.MinBusFactor), 2)
	if len(full.Details.SinglePointsOfFailure) != 0 {
		t.Fatalf("no single point of failure expected, got %v", full.Details.SinglePointsOfFailure)
	}
}

// TestM12DropoutMonotonicity proves the resilience direction: dropping a redundant
// member survives, dropping the only qualified holder does not.
func TestM12DropoutMonotonicity(t *testing.T) {
	langGo := "lang_go"
	weeks := []string{"2026-W10", "2026-W11", "2026-W12"}
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_t0012", BriefID: "brf_t0012", Preset: "balanced", K: 2, Seed: 11,
		Horizon: weeks, Weights: simpleWeights(),
		Requirements: []runinput.Requirement{
			{RequirementID: "req_x", RoleID: "backend_engineer", CompetencyID: &langGo, Level: 3,
				Criticality: "must", Headcount: 1,
				Effort: runinput.Effort{MinPersonDays: 3, LikelyPersonDays: 3, MaxPersonDays: 3}, AllowsExternal: true},
		},
		Candidates: []runinput.Candidate{
			{EmployeeID: "emp_r001", Grade: "S", CostCoefficient: 1.9, RoleProfileID: "backend_engineer",
				EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{"lang_go": 4}, Capacity: capacityWeeks(weeks, nil)},
			{EmployeeID: "emp_r002", Grade: "M", CostCoefficient: 1.4, RoleProfileID: "backend_engineer",
				EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{"lang_go": 4}, Capacity: capacityWeeks(weeks, nil)},
		},
		Constraints: runinput.Constraints{MaxTeamSize: 2, MaxUtilizationRatio: 0.95},
		CreatedAt:   time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	result, err := ComputeRun(RunInput{
		Snapshot: snap,
		Proposals: []Proposal{
			{
				// Only member and only qualified holder: its dropout loses the must
				// requirement -> scenario fails.
				VariantID: "var_solo", Label: "solo",
				Members: []ProposalMember{{EmployeeID: "emp_r001", RoleID: "backend_engineer", Grade: "S"}},
				Assignments: []ProposalAssignment{
					{EmployeeID: "emp_r001", RequirementID: "req_x", Week: "2026-W10", Hours: 40},
				},
			},
			{
				// r002 is redundant (it holds the competency but the work is done by
				// r001 in week 1). Base: 24 h of 45 h progress -> 0.533 weeks.
				// Dropout r002: 45 -> 40 h progress -> 0.6 <= 1.2*0.533 -> survives.
				// Dropout r001: 45 -> 5 h progress -> 4.8 weeks -> fails.
				VariantID: "var_pair", Label: "pair",
				Members: []ProposalMember{
					{EmployeeID: "emp_r001", RoleID: "backend_engineer", Grade: "S"},
					{EmployeeID: "emp_r002", RoleID: "backend_engineer", Grade: "M"},
				},
				Assignments: []ProposalAssignment{
					{EmployeeID: "emp_r001", RequirementID: "req_x", Week: "2026-W10", Hours: 40},
					{EmployeeID: "emp_r002", RequirementID: "req_x", Week: "2026-W10", Hours: 5},
				},
			},
		},
		Schedule: ScheduleConfig{MonteCarloRuns: 200, Seed: 11},
	})
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}

	solo, _ := result.VariantByID("var_solo")
	if solo.Metrics.M12 != 0 {
		t.Fatalf("M12 of the solo variant = %v, want 0 (its dropout loses the only must holder)", solo.Metrics.M12)
	}
	if diff := solo.Duration.P80Weeks - 0.6; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("solo P80 = %v, want 0.6 (24 h of 40 h weekly progress)", solo.Duration.P80Weeks)
	}

	pair, _ := result.VariantByID("var_pair")
	if diff := pair.Duration.P80Weeks - 0.533333; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("pair P80 = %v, want 0.533333 (24 h of 45 h weekly progress)", pair.Duration.P80Weeks)
	}
	if pair.Metrics.M12 != 0.5 {
		t.Fatalf("M12 of the pair = %v, want 0.5 (the redundant dropout survives)", pair.Metrics.M12)
	}
	if pair.Metrics.M12 <= solo.Metrics.M12 {
		t.Fatalf("resilience must increase when the team has a redundant qualified member")
	}
}

// TestComputeRunIsDeterministic proves that the same snapshot plus seed gives
// byte-identical JSON, even when the snapshot is rebuilt from equal input.
func TestComputeRunIsDeterministic(t *testing.T) {
	first, err := ComputeRun(fixtureRunInput(t, true))
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}
	second, err := ComputeRun(fixtureRunInput(t, true))
	if err != nil {
		t.Fatalf("compute run: %v", err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("same input must be byte identical:\n%s\n%s", firstJSON, secondJSON)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("results differ structurally")
	}

	// A different seed changes the Monte Carlo samples of a ranged effort, but never
	// the structure of the result.
	other := fixtureRunInput(t, true)
	other.Schedule.Seed = 43
	third, err := ComputeRun(other)
	if err != nil {
		t.Fatalf("compute run with other seed: %v", err)
	}
	if len(third.Variants) != len(first.Variants) {
		t.Fatalf("variant count must not depend on the seed")
	}
}

// TestComputeRunRejectsInvalidInput covers the guard rails of the API.
func TestComputeRunRejectsInvalidInput(t *testing.T) {
	if _, err := ComputeRun(RunInput{}); err == nil {
		t.Fatalf("a run without a snapshot must be rejected")
	}
	in := fixtureRunInput(t, true)
	in.Proposals = append(in.Proposals, in.Proposals[0])
	if _, err := ComputeRun(in); err == nil {
		t.Fatalf("duplicate variant ids must be rejected")
	}
	in = fixtureRunInput(t, true)
	in.Proposals[0].VariantID = ""
	if _, err := ComputeRun(in); err == nil {
		t.Fatalf("an empty variant id must be rejected")
	}
}

// TestComputeRunEmptyRunIsStable covers a run without proposals (solver returned none).
func TestComputeRunEmptyRunIsStable(t *testing.T) {
	result, err := ComputeRun(RunInput{Snapshot: fixtureSnapshot(t, true)})
	if err != nil {
		t.Fatalf("compute empty run: %v", err)
	}
	if len(result.Variants) != 0 {
		t.Fatalf("expected no variants")
	}
	if result.Set.DiversityRuleOK {
		t.Fatalf("an empty variant set cannot satisfy the variant-set rules")
	}
	if len(result.Set.DominatedVariantIDs) != 0 || len(result.Set.GateFailedVariantIDs) != 0 {
		t.Fatalf("empty diagnostics expected: %#v", result.Set)
	}
}

// ---------- small assertion helpers ----------

func deref(value *float64) float64 {
	if value == nil {
		return -1 // a missing metric never equals an expected value
	}
	return *value
}

func ptr(value *float64) float64 { return deref(value) }

func ptrInt(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func checkFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if diff := got - want; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func checkInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}
