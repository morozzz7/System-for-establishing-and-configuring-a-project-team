package metrics

import (
	"fmt"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/runinput"
	"github.com/teamopt/backend/internal/schedule"
)

// ComputeRun computes the authoritative metrics, duration and deficits of every
// proposed variant and the run-level variant-set diagnostics.
//
// The computation is two-pass because two metrics are defined relative to the rest of
// the run: M5 needs T_best (the lowest P50 among gate-passing variants) and M6 needs
// C_min/C_ref (cheapest / reference cost among gate-passing variants).
func ComputeRun(in RunInput) (RunResult, error) {
	if in.Snapshot == nil {
		return RunResult{}, fmt.Errorf("metrics: run snapshot is required")
	}
	cfg := normalizeConfig(in.Schedule, in.Snapshot.Seed())

	comps := make([]*variantComputation, 0, len(in.Proposals))
	seen := make(map[string]bool, len(in.Proposals))
	for _, p := range in.Proposals {
		if p.VariantID == "" {
			return RunResult{}, fmt.Errorf("metrics: variant_id is required for every proposal")
		}
		if seen[p.VariantID] {
			return RunResult{}, fmt.Errorf("metrics: duplicate proposal variant_id %q", p.VariantID)
		}
		seen[p.VariantID] = true
		comps = append(comps, newVariantComputation(in.Snapshot, p, cfg))
	}

	// Pass 1: everything that does not depend on the other variants of the run.
	// Cost is computed before the gate because G4 checks the budget cap against c.cost.
	for _, c := range comps {
		c.computeCost()
		c.computeGate()
		c.computeSchedule()
		c.computeStaticComponents()
	}

	// Pass 2: run-relative metrics, resilience and the composite score.
	tBest, tBestSet := bestP50(comps)
	cMin, cRef, costRefSet := costReferences(comps, in.Snapshot.Constraints().BudgetCapUnits)
	for _, c := range comps {
		c.finalizeScheduleMetrics(tBest, tBestSet)
		c.finalizeCost(cMin, cRef, costRefSet)
		c.finalizeResilience()
		c.finalizeScore(in.Snapshot.Weights())
	}

	out := RunResult{Variants: make([]VariantResult, 0, len(comps))}
	rosters := make(map[string][]string, len(comps))
	for _, c := range comps {
		out.Variants = append(out.Variants, c.result())
		rosters[c.proposal.VariantID] = c.members
	}
	out.Set = buildSetDiagnostics(out.Variants, rosters)
	return out, nil
}

func normalizeConfig(cfg ScheduleConfig, snapshotSeed int64) ScheduleConfig {
	out := cfg
	if out.MonteCarloRuns <= 0 {
		out.MonteCarloRuns = DefaultMonteCarloRuns
	}
	if out.Seed == 0 {
		out.Seed = snapshotSeed
	}
	if out.PortfolioTauWorkingDays <= 0 {
		out.PortfolioTauWorkingDays = DefaultPortfolioTauWorkingDays
	}
	return out
}

// variantComputation holds the derived working data of one variant. Nothing here is
// exported: the public result is built by result().
type variantComputation struct {
	snapshot *runinput.Snapshot
	proposal Proposal
	cfg      ScheduleConfig

	candidates   map[string]runinput.Candidate
	requirements []runinput.Requirement

	members      []string
	memberSet    map[string]bool
	empWeekHours map[string]map[string]float64
	memberHours  map[string]float64

	cost         float64
	gate         GateViolations
	baseSchedule schedule.Result
	scheduleErr  error

	// coveredPersonDays is the M2 numerator per requirement before clamping.
	coveredPersonDays map[string]float64
	deficits          []Deficit
	detail            MetricDetails
	values            MetricValues
}

func newVariantComputation(snap *runinput.Snapshot, p Proposal, cfg ScheduleConfig) *variantComputation {
	c := &variantComputation{
		snapshot:          snap,
		proposal:          p,
		cfg:               cfg,
		candidates:        make(map[string]runinput.Candidate, len(snap.Candidates())),
		requirements:      append([]runinput.Requirement(nil), snap.Requirements()...),
		memberSet:         make(map[string]bool, len(p.Members)),
		empWeekHours:      make(map[string]map[string]float64, len(p.Members)),
		memberHours:       make(map[string]float64, len(p.Members)),
		coveredPersonDays: make(map[string]float64, len(snap.Requirements())),
	}
	for _, cand := range snap.Candidates() {
		c.candidates[cand.EmployeeID] = cand
	}
	for _, m := range p.Members {
		if c.memberSet[m.EmployeeID] {
			continue
		}
		c.memberSet[m.EmployeeID] = true
		c.members = append(c.members, m.EmployeeID)
	}
	sort.Strings(c.members)
	for _, a := range p.Assignments {
		if a.Hours <= 0 {
			continue
		}
		if c.empWeekHours[a.EmployeeID] == nil {
			c.empWeekHours[a.EmployeeID] = make(map[string]float64)
		}
		c.empWeekHours[a.EmployeeID][a.Week] += a.Hours
		c.memberHours[a.EmployeeID] += a.Hours
		c.coveredPersonDays[a.RequirementID] += a.Hours / HoursPerDay * c.quality(a.EmployeeID, a.RequirementID)
	}
	return c
}

// requirement returns the requirement with the given id.
func (c *variantComputation) requirement(requirementID string) (runinput.Requirement, bool) {
	for _, r := range c.requirements {
		if r.RequirementID == requirementID {
			return r, true
		}
	}
	return runinput.Requirement{}, false
}

// quality is q_ir = min(1, lvl_ir / L_r); a role-only requirement has no competency
// level, so a member assigned to it contributes at full quality (the role match is what
// makes the assignment admissible in the first place).
func (c *variantComputation) quality(employeeID, requirementID string) float64 {
	req, ok := c.requirement(requirementID)
	if !ok {
		return 0
	}
	cand, ok := c.candidates[employeeID]
	if !ok {
		return 0
	}
	if req.CompetencyID == nil {
		return 1
	}
	lvl := cand.CompetencyLevels[*req.CompetencyID]
	if lvl <= 0 {
		return 0
	}
	return math.Min(1, float64(lvl)/float64(req.Level))
}

// levelFit is fit_ir of M3 with the asymmetric under-qualification penalty (x1.5).
func levelFit(level, required int) float64 {
	diff := level - required
	var fit float64
	if diff >= 0 {
		fit = 1 - float64(diff)/4.0
	} else {
		fit = 1 - 1.5*float64(-diff)/4.0
	}
	return clamp01(fit)
}

// candidateLevel returns the member's level in a requirement's competency and whether
// the member is qualified for the requirement at the required level. A role-only
// requirement is qualified by the role profile (M9 / G3).
func (c *variantComputation) qualified(employeeID string, req runinput.Requirement) bool {
	cand, ok := c.candidates[employeeID]
	if !ok {
		return false
	}
	if req.CompetencyID == nil {
		return cand.HasRole(req.RoleID)
	}
	return cand.CompetencyLevels[*req.CompetencyID] >= req.Level
}

// capacityRow returns the snapshot capacity row of an employee in a week.
func (c *variantComputation) capacityRow(employeeID, week string) (runinput.CapacityRow, bool) {
	cand, ok := c.candidates[employeeID]
	if !ok {
		return runinput.CapacityRow{}, false
	}
	for _, row := range cand.Capacity {
		if row.Week == week {
			return row, true
		}
	}
	return runinput.CapacityRow{}, false
}

// computeCost fills c.cost: C = sum(person_hours * cost_coefficient(grade)).
// The coefficient comes from the snapshot candidate; a member that is not a candidate
// (a G5 violation) contributes the neutral coefficient 1.
func (c *variantComputation) computeCost() {
	total := 0.0
	for _, empID := range c.members {
		coefficient := 1.0
		if cand, ok := c.candidates[empID]; ok && cand.CostCoefficient > 0 {
			coefficient = cand.CostCoefficient
		}
		total += c.memberHours[empID] * coefficient
	}
	c.cost = total
}

// computeSchedule runs the deterministic schedule engine for the proposed assignments.
func (c *variantComputation) computeSchedule() {
	c.baseSchedule, c.scheduleErr = c.scheduleFor(c.proposal.Assignments)
}

// scheduleFor builds a schedule.Input from the snapshot and the given assignment rows
// and runs the engine. It is also used by the M12 dropout simulation.
func (c *variantComputation) scheduleFor(assignments []ProposalAssignment) (schedule.Result, error) {
	in := schedule.Input{
		Requirements:            make([]schedule.Requirement, 0, len(c.requirements)),
		Assignments:             make([]schedule.Assignment, 0, len(assignments)),
		Capacity:                make([]schedule.Capacity, 0, len(c.members)*len(c.snapshot.Horizon())),
		Portfolio:               make([]schedule.PortfolioProject, 0, len(c.snapshot.Portfolio())),
		MonteCarloRuns:          c.cfg.MonteCarloRuns,
		Seed:                    c.cfg.Seed,
		PortfolioTauWorkingDays: c.cfg.PortfolioTauWorkingDays,
	}
	for _, r := range c.requirements {
		in.Requirements = append(in.Requirements, schedule.Requirement{
			ID:     r.RequirementID,
			Effort: schedule.Effort{Min: r.Effort.MinPersonDays, Likely: r.Effort.LikelyPersonDays, Max: r.Effort.MaxPersonDays},
		})
	}
	for _, a := range assignments {
		if a.Hours <= 0 {
			continue
		}
		in.Assignments = append(in.Assignments, schedule.Assignment{
			EmployeeID: a.EmployeeID, RequirementID: a.RequirementID, Week: a.Week, Hours: a.Hours,
		})
	}
	for _, empID := range c.members {
		cand, ok := c.candidates[empID]
		if !ok {
			continue
		}
		for _, row := range cand.Capacity {
			in.Capacity = append(in.Capacity, schedule.Capacity{
				EmployeeID:  empID,
				Week:        row.Week,
				FreeHours:   row.FreeHours,
				Unavailable: row.Unavailable || row.FreeHours <= 0,
			})
		}
	}
	for _, p := range c.snapshot.Portfolio() {
		in.Portfolio = append(in.Portfolio, schedule.PortfolioProject{
			ProjectID: p.ProjectID, RemainingHours: p.RemainingHours,
			WeeklyCapacity: p.WeeklyCapacity, Critical: p.Critical,
		})
	}
	in.DeadlineWeeks = c.deadlineWeeks()
	return schedule.Calculate(in)
}

// deadlineWeeks converts the deadline week of the constraints into the schedule engine's
// unit (weeks from the start of the horizon; finishing a week is index+1).
// runinput guarantees the deadline lies inside the horizon.
func (c *variantComputation) deadlineWeeks() *float64 {
	deadline := c.snapshot.Constraints().DeadlineWeek
	if deadline == "" {
		return nil
	}
	horizon := c.snapshot.Horizon()
	for i, week := range horizon {
		if week == deadline {
			weeks := float64(i + 1)
			return &weeks
		}
	}
	// Defensive: a deadline outside the horizon is treated as the horizon end because
	// the engine cannot simulate beyond it.
	weeks := float64(len(horizon))
	if deadline < horizon[0] {
		weeks = 0
	}
	return &weeks
}

// bestP50 returns T_best = lowest P50 among gate-passing variants with a real duration.
func bestP50(comps []*variantComputation) (float64, bool) {
	best := 0.0
	found := false
	for _, c := range comps {
		if !c.gate.Pass() || c.scheduleErr != nil {
			continue
		}
		p50 := c.baseSchedule.Duration.P50Weeks
		if p50 <= 0 {
			continue
		}
		if !found || p50 < best {
			best = p50
			found = true
		}
	}
	return best, found
}

// costReferences returns C_min and C_ref of M6. C_ref is the budget cap when the run has
// one above C_min, otherwise the most expensive gate-passing variant (METRICS M6).
// gateCosts is false when no variant passes the gate; all variants are then used so the
// metric stays defined.
func costReferences(comps []*variantComputation, budget float64) (cMin, cRef float64, ok bool) {
	pool := make([]*variantComputation, 0, len(comps))
	for _, c := range comps {
		if c.gate.Pass() {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		pool = comps
	}
	if len(pool) == 0 {
		return 0, 0, false
	}
	cMin = pool[0].cost
	cRef = pool[0].cost
	for _, c := range pool {
		if c.cost < cMin {
			cMin = c.cost
		}
		if c.cost > cRef {
			cRef = c.cost
		}
	}
	if budget > cMin {
		cRef = budget
	}
	return cMin, cRef, true
}

// result builds the public, serializable result of the variant.
func (c *variantComputation) result() VariantResult {
	duration := VariantDuration{
		P50Weeks:       round6(c.baseSchedule.Duration.P50Weeks),
		P80Weeks:       round6(c.baseSchedule.Duration.P80Weeks),
		P50WorkingDays: c.baseSchedule.Duration.P50WorkingDays,
		P80WorkingDays: c.baseSchedule.Duration.P80WorkingDays,
		MonteCarlo:     &MonteCarloInfo{Runs: c.cfg.MonteCarloRuns, Seed: c.cfg.Seed},
	}
	if deadline := c.snapshot.Constraints().DeadlineWeek; deadline != "" {
		duration.DeadlineWeek = deadline
		if p := c.baseSchedule.Duration.POnTime; p != nil {
			value := round6(*p)
			duration.POnTime = &value
		}
		if horizon := c.weeksToDeadline(); horizon > 0 {
			slack := round6((horizon - c.baseSchedule.Duration.P80Weeks) / horizon)
			duration.SlackWeeks = &slack
		}
	}
	return VariantResult{
		VariantID:     c.proposal.VariantID,
		Label:         c.proposal.Label,
		TeamSize:      len(c.members),
		GatePass:      c.gate.Pass(),
		Gate:          c.gate,
		Metrics:       c.values,
		Details:       c.detail,
		Duration:      duration,
		CostUnits:     round6(c.cost),
		Deficits:      append([]Deficit(nil), c.deficits...),
		DeficitCount:  len(c.deficits),
		ScheduleError: errorString(c.scheduleErr),
	}
}

// weeksToDeadline returns the deadline distance in weeks from the horizon start.
func (c *variantComputation) weeksToDeadline() float64 {
	if ptr := c.deadlineWeeks(); ptr != nil {
		return *ptr
	}
	return 0
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ---------- shared helpers ----------

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

func clampMax(v, limit float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(limit, v))
}

// round6 rounds a reported value to 6 decimals (same precision the ML service uses) so
// that the API never exposes float noise. Score is computed from rounded values, so
// every number of a variant is derivable from the served metric_values.
func round6(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1e6) / 1e6
}

func floatPtr(v float64) *float64 {
	value := round6(v)
	return &value
}

func intPtr(v int) *int { return &v }
