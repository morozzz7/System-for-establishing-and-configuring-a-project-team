// Package metrics computes the authoritative team metrics M1-M12 and the composite
// score of docs/METRICS.md for every variant proposed by the ML optimizer.
//
// "Solver proposes, Go verifies" (AGENTS.md): the ML service returns advisory metrics
// next to every variant, but they are never trusted. This package derives every number
// from the immutable run snapshot (internal/runinput) and the variant's weekly
// assignments, using only the deterministic schedule engine (internal/schedule) and the
// fixed seed of the run. There is no database, no HTTP, no wall clock and no unseeded
// randomness, so the same snapshot plus seed always produces byte-identical output.
//
// Documented interpretations (ADR-007 §1, where METRICS.md leaves room):
//   - a_irw = hours / (working_days * hours_per_day); contributions use 8 hours per day
//     and 5 working days per week (ADR-002).
//   - Effort basis: the G3 gate uses min_person_days (conservative), M2, the reported
//     deficits and M10's n_opt use likely_person_days. A variant that covers min but not
//     likely is feasible and truthfully reports a positive deficit.
//   - M2 contribution per assignment is hours/8 * min(1, level/L), clamped per
//     requirement at E_likely.
//   - M5 = P(T <= D) when a deadline exists, otherwise T_best/T50 with T_best the lowest
//     P50 among gate-passing variants of the same run.
//   - M6 = 1 - (C - C_min)/(C_ref - C_min) with C_min minimal and C_ref the budget (when
//     set above C_min) or the most expensive gate-passing variant.
//   - M7 u = (assigned_hours + variant_hours)/norm_hours; absence is lost time, not load.
//   - M8 = exp(-slip_working_days/tau) from the schedule engine's portfolio model.
//   - M9 counts must requirements with at least two qualified members; a role-only
//     requirement is qualified by role profile.
//   - M10 fragmentation is the share of assignment rows below 20% of a 40 hour week
//     (< 8 h), context overhead uses the candidates' current project count and n_opt
//     uses the reference duration T_ref (deadline week, else the whole horizon).
//   - M11 = bench hours assigned to the variant / bench hours available in the horizon;
//     0 when no bench capacity exists (no bench contribution can be claimed).
//   - M12 zeroes one member's assignments in the two consecutive weeks with the highest
//     assigned hours, recomputes T80 with the same seed and counts the scenario as
//     surviving when no must requirement loses its last qualified member and
//     T80_new <= 1.2 * T80_base.
//   - M4 is dropped when no candidate has an experience score; weights are renormalized
//     over the metrics that are present, and Gate = 0 forces Score = 0.
package metrics

import (
	"github.com/teamopt/backend/internal/runinput"
)

// Version is reported as RunDiagnostics.metrics_version.
const Version = "m1-m12@2026-11"

// Planning constants copied from contracts/reference.yaml (ADR-002). They are part of
// the formulas, not tunables: changing them changes the metric definitions.
const (
	// HoursPerDay converts assignment hours to person-days.
	HoursPerDay = 8.0
	// WorkdaysPerWeek is the length of a planning week in working days.
	WorkdaysPerWeek = 5.0
	// WeeklyNormHours is the nominal week used by M10's fragmentation threshold.
	WeeklyNormHours = 40.0
	// FragmentationThresholdHours is "load < 20%" of M10 (0.2 * 40 h).
	FragmentationThresholdHours = 0.2 * WeeklyNormHours
	// DefaultMonteCarloRuns is the schedule engine sample size (METRICS M5, N = 2000).
	DefaultMonteCarloRuns = 2000
	// DefaultPortfolioTauWorkingDays is tau of M8.
	DefaultPortfolioTauWorkingDays = 10.0
	// UtilizationBandLow/High bound the "60-90% band" diagnostic of M7.
	UtilizationBandLow  = 0.6
	UtilizationBandHigh = 0.9
	// DropoutWeeks is the length of the M12 dropout window.
	DropoutWeeks = 2
	// DropoutTolerance is the allowed T80 growth of a surviving dropout (M12).
	DropoutTolerance = 1.2
	// DiversityThreshold is the minimum pairwise roster Jaccard distance of the variant
	// set (METRICS "Rules for the set of 2-3 variants", rule 3).
	DiversityThreshold = 0.25
	// dominanceTolerance absorbs float noise in the Pareto comparison.
	dominanceTolerance = 1e-9
	// reportPrecision is the number of decimals of every reported metric value.
	reportPrecision = 6
)

// ScheduleConfig carries the deterministic schedule parameters of the run.
type ScheduleConfig struct {
	// MonteCarloRuns is the number of Monte Carlo iterations (default 2000).
	MonteCarloRuns int
	// Seed is the fixed seed; 0 means "use the snapshot seed".
	Seed int64
	// PortfolioTauWorkingDays is tau of M8 (default 10 working days).
	PortfolioTauWorkingDays float64
}

// ProposalMember is one employee of a proposed variant.
type ProposalMember struct {
	EmployeeID string
	RoleID     string
	Grade      string
}

// ProposalAssignment is one weekly assignment row of a proposed variant.
type ProposalAssignment struct {
	EmployeeID    string
	RequirementID string
	Week          string
	Hours         float64
}

// MLDeficit is an advisory deficit declared by the solver. It is the only way a variant
// can satisfy G3 without covering the minimum effort, so it is an input of the gate and
// not a metric.
type MLDeficit struct {
	CompetencyID   *string
	RoleID         string
	RequirementIDs []string
	PersonDays     float64
}

// Proposal is one variant proposed by the ML optimizer.
type Proposal struct {
	VariantID   string
	Label       string
	Members     []ProposalMember
	Assignments []ProposalAssignment
	MLDeficits  []MLDeficit
}

// RunInput is everything ComputeRun needs: the immutable snapshot, the proposals and the
// deterministic schedule configuration.
type RunInput struct {
	Snapshot  *runinput.Snapshot
	Proposals []Proposal
	Schedule  ScheduleConfig
}

// MetricValues mirrors contracts MetricValues. M4 is a pointer: it is absent when the
// snapshot has no project-history signal (docs/METRICS.md M4).
type MetricValues struct {
	M1    float64  `json:"M1"`
	M2    float64  `json:"M2"`
	M3    float64  `json:"M3"`
	M4    *float64 `json:"M4,omitempty"`
	M5    float64  `json:"M5"`
	M6    float64  `json:"M6"`
	M7    float64  `json:"M7"`
	M8    float64  `json:"M8"`
	M9    float64  `json:"M9"`
	M10   float64  `json:"M10"`
	M11   float64  `json:"M11"`
	M12   float64  `json:"M12"`
	Score float64  `json:"score"`
}

// value returns a metric by key and whether it is present.
func (v MetricValues) value(key string) (float64, bool) {
	switch key {
	case "M1":
		return v.M1, true
	case "M2":
		return v.M2, true
	case "M3":
		return v.M3, true
	case "M4":
		if v.M4 == nil {
			return 0, false
		}
		return *v.M4, true
	case "M5":
		return v.M5, true
	case "M6":
		return v.M6, true
	case "M7":
		return v.M7, true
	case "M8":
		return v.M8, true
	case "M9":
		return v.M9, true
	case "M10":
		return v.M10, true
	case "M11":
		return v.M11, true
	case "M12":
		return v.M12, true
	}
	return 0, false
}

// Keys are the metric keys in their canonical order.
var Keys = []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10", "M11", "M12"}

// MetricDetails mirrors the diagnostic sub-metrics of contracts MetricDetails
// (additionalProperties: true). Every field is a pointer so "not applicable" is
// distinguishable from a real zero.
type MetricDetails struct {
	MustCoverage             *float64 `json:"must_coverage,omitempty"`
	OverqualifiedShare       *float64 `json:"overqualified_share,omitempty"`
	PeakUtilization          *float64 `json:"peak_utilization,omitempty"`
	MeanUtilization          *float64 `json:"mean_utilization,omitempty"`
	UtilizationBand6090Share *float64 `json:"utilization_band_60_90_share,omitempty"`
	ProjectsAffected         *int     `json:"projects_affected,omitempty"`
	CriticalProjectsAffected *int     `json:"critical_projects_affected,omitempty"`
	MinBusFactor             *int     `json:"min_bus_factor,omitempty"`
	SinglePointsOfFailure    []string `json:"single_points_of_failure,omitempty"`
	FragmentationShare       *float64 `json:"fragmentation_share,omitempty"`
	ConcurrentProjectsMean   *float64 `json:"concurrent_projects_mean,omitempty"`
	NOpt                     *int     `json:"n_opt,omitempty"`
	BenchHoursAssigned       *float64 `json:"bench_hours_assigned,omitempty"`
	BenchHoursAvailable      *float64 `json:"bench_hours_available,omitempty"`
	CostMinUnits             *float64 `json:"cost_min_units,omitempty"`
	CostRefUnits             *float64 `json:"cost_ref_units,omitempty"`
	TBestWeeks               *float64 `json:"t_best_weeks,omitempty"`
	PortfolioSlipDays        *float64 `json:"portfolio_slip_days,omitempty"`
	ResilienceScore          *float64 `json:"resilience_score,omitempty"`
	// DroppedMetrics lists metrics that could not be computed (M4 without history).
	DroppedMetrics []string `json:"dropped_metrics,omitempty"`
}

// MonteCarloInfo mirrors contracts MonteCarloInfo.
type MonteCarloInfo struct {
	Runs int   `json:"runs"`
	Seed int64 `json:"seed"`
}

// VariantDuration mirrors contracts VariantDuration.
type VariantDuration struct {
	P50Weeks       float64         `json:"p50_weeks"`
	P80Weeks       float64         `json:"p80_weeks"`
	P50WorkingDays int             `json:"p50_working_days"`
	P80WorkingDays int             `json:"p80_working_days"`
	SlackWeeks     *float64        `json:"slack_weeks,omitempty"`
	DeadlineWeek   string          `json:"deadline_week,omitempty"`
	POnTime        *float64        `json:"p_on_time,omitempty"`
	MonteCarlo     *MonteCarloInfo `json:"monte_carlo,omitempty"`
}

// GateViolations mirrors contracts GateViolations. Every count must be 0 for a variant
// to reach the user (docs/METRICS.md Level 0).
type GateViolations struct {
	G1                        int     `json:"G1"`
	G2                        int     `json:"G2"`
	G3                        int     `json:"G3"`
	G4                        int     `json:"G4"`
	G5                        int     `json:"G5"`
	MaxOverloadPct            float64 `json:"max_overload_pct,omitempty"`
	UncoveredMustNotInDeficit int     `json:"uncovered_must_not_in_deficit,omitempty"`
}

// Pass reports whether the variant satisfies every gate.
func (g GateViolations) Pass() bool {
	return g.G1 == 0 && g.G2 == 0 && g.G3 == 0 && g.G4 == 0 && g.G5 == 0 &&
		g.UncoveredMustNotInDeficit == 0
}

// Deficit is a Go-computed effort shortage for one requirement (METRICS Level 2 input).
type Deficit struct {
	RequirementID           string  `json:"requirement_id"`
	CompetencyID            *string `json:"competency_id,omitempty"`
	RoleID                  string  `json:"role_id"`
	PersonDays              float64 `json:"person_days"`
	ExternalCooperationFlag bool    `json:"external_cooperation_flag"`
}

// VariantResult is the authoritative computation for one variant.
type VariantResult struct {
	VariantID    string
	Label        string
	TeamSize     int
	GatePass     bool
	Gate         GateViolations
	Metrics      MetricValues
	Details      MetricDetails
	Duration     VariantDuration
	CostUnits    float64
	Deficits     []Deficit
	DeficitCount int
	// ScheduleError is set when the schedule engine could not produce a duration
	// (e.g. a variant without any usable assignment). It is never serialized; the
	// service drops such variants instead of serving a made-up duration.
	ScheduleError string `json:"-"`
}

// VariantSetDiagnostics mirrors contracts VariantSetDiagnostics: the run-level rules of
// METRICS.md "Rules for the set of 2-3 variants", recomputed by Go.
type VariantSetDiagnostics struct {
	PairwiseJaccard      map[string]float64 `json:"pairwise_jaccard,omitempty"`
	MinPairwiseJaccard   float64            `json:"min_pairwise_jaccard"`
	DominatedVariantIDs  []string           `json:"dominated_variant_ids"`
	GateFailedVariantIDs []string           `json:"gate_failed_variant_ids"`
	DiversityRuleOK      bool               `json:"diversity_rule_ok"`
}

// RunResult is the authoritative output of ComputeRun.
type RunResult struct {
	Variants []VariantResult
	Set      VariantSetDiagnostics
}

// VariantByID returns the result of one variant.
func (r RunResult) VariantByID(variantID string) (VariantResult, bool) {
	for _, v := range r.Variants {
		if v.VariantID == variantID {
			return v, true
		}
	}
	return VariantResult{}, false
}
