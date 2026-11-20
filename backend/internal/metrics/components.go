package metrics

import (
	"math"
)

// computeStaticComponents fills every metric that does not depend on the other variants
// of the run: M1, M2, M3, M4 (optional), M7, M8, M9, M10, M11 plus their diagnostics.
func (c *variantComputation) computeStaticComponents() {
	c.computeCoverage()   // M1
	c.computeEffort()     // M2 + Go-computed deficits
	c.computeLevelFit()   // M3
	c.computeExperience() // M4 (dropped without project history)
	c.computeLoadHealth() // M7
	c.computePortfolio()  // M8
	c.computeBusFactor()  // M9
	c.computeFocus()      // M10
	c.computeBench()      // M11
}

// ---------- M1 competency coverage ----------
// cov_r = max_{i in T} min(1, lvl_ir / L_r); M1 = sum(w_r * cov_r) / sum(w_r).
// A role-only requirement has no competency level, so it is covered (cov = 1) as soon
// as a member holds the required role profile.
func (c *variantComputation) computeCoverage() {
	weighted, weightSum := 0.0, 0.0
	mustTotal, mustCovered := 0, 0
	for _, req := range c.requirements {
		cov := 0.0
		for _, empID := range c.members {
			cand, ok := c.candidates[empID]
			if !ok {
				continue
			}
			if req.CompetencyID == nil {
				if cand.HasRole(req.RoleID) {
					cov = 1
				}
				continue
			}
			level := cand.CompetencyLevels[*req.CompetencyID]
			cov = math.Max(cov, math.Min(1, float64(level)/float64(req.Level)))
		}
		weighted += req.CriticalityWeight() * cov
		weightSum += req.CriticalityWeight()
		if req.IsMust() {
			mustTotal++
			if cov >= 1 {
				mustCovered++
			}
		}
	}
	m1 := 1.0
	if weightSum > 0 {
		m1 = weighted / weightSum
	}
	c.values.M1 = clamp01(m1)
	c.detail.MustCoverage = floatPtr(1)
	if mustTotal > 0 {
		c.detail.MustCoverage = floatPtr(float64(mustCovered) / float64(mustTotal))
	}
}

// ---------- M2 effort coverage ----------
// covered_r = min(E_likely, sum(hours/8 * min(1, lvl/L))); M2 = sum(covered_r)/sum(E).
func (c *variantComputation) computeEffort() {
	covered, total := 0.0, 0.0
	for _, req := range c.requirements {
		raw := c.coveredPersonDays[req.RequirementID]
		clamped := math.Min(raw, req.Effort.LikelyPersonDays)
		covered += clamped
		total += req.Effort.LikelyPersonDays
		deficit := req.Effort.LikelyPersonDays - clamped
		if deficit > 1e-9 {
			c.deficits = append(c.deficits, Deficit{
				RequirementID: req.RequirementID,
				CompetencyID:  req.CompetencyID,
				RoleID:        req.RoleID,
				PersonDays:    round6(deficit),
				// A must gap cannot be closed internally without escalation
				// (docs/METRICS.md Level 2: external_cooperation_flag).
				ExternalCooperationFlag: req.IsMust(),
			})
		}
	}
	m2 := 1.0
	if total > 0 {
		m2 = covered / total
	}
	c.values.M2 = clamp01(m2)
}

// ---------- M3 level fit ----------
// fit_ir = 1 - |lvl-L|/4 with under-qualification x1.5; M3 is the effort-weighted mean.
func (c *variantComputation) computeLevelFit() {
	numerator, denominator, overqualified := 0.0, 0.0, 0.0
	for _, a := range c.proposal.Assignments {
		if a.Hours <= 0 {
			continue
		}
		req, ok := c.requirement(a.RequirementID)
		if !ok {
			continue
		}
		personDays := a.Hours / HoursPerDay
		fit := 1.0
		if req.CompetencyID != nil {
			cand, ok := c.candidates[a.EmployeeID]
			if !ok {
				fit = 0 // unknown competence: never assume a good fit
			} else {
				level := cand.CompetencyLevels[*req.CompetencyID]
				fit = levelFit(level, req.Level)
				if level-req.Level >= 2 {
					overqualified += personDays
				}
			}
		}
		numerator += personDays * fit
		denominator += personDays
	}
	m3 := 1.0
	if denominator > 0 {
		m3 = numerator / denominator
	}
	c.values.M3 = clamp01(m3)
	share := 0.0
	if denominator > 0 {
		share = overqualified / denominator
	}
	c.detail.OverqualifiedShare = floatPtr(clamp01(share))
}

// ---------- M4 experience relevance ----------
// exp_i comes from the snapshot (cosine similarity of the brief with the employee's
// completed projects, computed by the ML service at snapshot time). Without project
// history the metric is dropped and the weights are renormalized elsewhere.
func (c *variantComputation) computeExperience() {
	hasSignal := false
	for _, cand := range c.candidates {
		if cand.ExperienceScore != nil {
			hasSignal = true
			break
		}
	}
	if !hasSignal {
		c.dropMetric("M4")
		return
	}
	numerator, denominator := 0.0, 0.0
	for _, empID := range c.members {
		cand, ok := c.candidates[empID]
		if !ok || cand.ExperienceScore == nil {
			continue
		}
		personDays := c.memberHours[empID] / HoursPerDay
		numerator += personDays * (*cand.ExperienceScore)
		denominator += personDays
	}
	if denominator <= 0 {
		c.dropMetric("M4")
		return
	}
	value := clamp01(numerator / denominator)
	c.values.M4 = &value
}

// dropMetric records a metric that cannot be computed from the snapshot.
func (c *variantComputation) dropMetric(key string) {
	for _, existing := range c.detail.DroppedMetrics {
		if existing == key {
			return
		}
	}
	c.detail.DroppedMetrics = append(c.detail.DroppedMetrics, key)
}

// ---------- M7 load health ----------
// u_iw = (already_assigned_hours + variant_hours) / norm_hours; absence is lost time,
// not load. overheat = mean(min(1, max(0, u-0.9)/0.1)), M7 = 1 - overheat.
func (c *variantComputation) computeLoadHealth() {
	horizon := c.snapshot.Horizon()
	personWeeks := 0
	sum, peak, band, overheat := 0.0, 0.0, 0, 0.0
	for _, empID := range c.members {
		cand, ok := c.candidates[empID]
		if !ok {
			continue
		}
		rows := make(map[string]struct {
			assigned float64
			norm     float64
		}, len(cand.Capacity))
		for _, row := range cand.Capacity {
			rows[row.Week] = struct {
				assigned float64
				norm     float64
			}{row.AssignedHours, row.NormHours}
		}
		for _, week := range horizon {
			row, ok := rows[week]
			if !ok || row.norm <= 0 {
				continue // utilization is undefined without a norm
			}
			utilization := (row.assigned + c.empWeekHours[empID][week]) / row.norm
			personWeeks++
			sum += utilization
			if utilization > peak {
				peak = utilization
			}
			if utilization >= UtilizationBandLow && utilization <= UtilizationBandHigh {
				band++
			}
			overheat += math.Min(1, math.Max(0, (utilization-UtilizationBandHigh)/(1-UtilizationBandHigh)))
		}
	}
	m7 := 1.0
	mean := 0.0
	if personWeeks > 0 {
		m7 = 1 - overheat/float64(personWeeks)
		mean = sum / float64(personWeeks)
	}
	c.values.M7 = clamp01(m7)
	c.detail.PeakUtilization = floatPtr(clampMax(peak, 1.5))
	c.detail.MeanUtilization = floatPtr(clampMax(mean, 1.5))
	bandShare := 0.0
	if personWeeks > 0 {
		bandShare = float64(band) / float64(personWeeks)
	}
	c.detail.UtilizationBand6090Share = floatPtr(clamp01(bandShare))
}

// ---------- M8 portfolio impact ----------
// M8 = exp(-sum_p(slip_p) / tau) with tau = 10 working days, from the schedule engine.
func (c *variantComputation) computePortfolio() {
	if c.scheduleErr != nil {
		c.values.M8 = 1
		c.detail.ProjectsAffected = intPtr(0)
		c.detail.CriticalProjectsAffected = intPtr(0)
		c.detail.PortfolioSlipDays = floatPtr(0)
		return
	}
	impact := c.baseSchedule.PortfolioImpact
	c.values.M8 = clamp01(impact.Score)
	c.detail.ProjectsAffected = intPtr(impact.ProjectsAffected)
	c.detail.CriticalProjectsAffected = intPtr(impact.CriticalAffected)
	c.detail.PortfolioSlipDays = floatPtr(impact.TotalSlipDays)
}

// ---------- M9 bus factor ----------
// N_r = members with lvl >= L_r; M9 = |{r in must : N_r >= 2}| / |must|.
func (c *variantComputation) computeBusFactor() {
	mustTotal, twice := 0, 0
	minN := 0
	var spof []string
	for _, req := range c.requirements {
		if !req.IsMust() {
			continue
		}
		mustTotal++
		qualified := 0
		for _, empID := range c.members {
			if c.qualified(empID, req) {
				qualified++
			}
		}
		if qualified >= 2 {
			twice++
		}
		if mustTotal == 1 || qualified < minN {
			minN = qualified
		}
		if qualified <= 1 {
			key := req.RoleID
			if req.CompetencyID != nil {
				key = *req.CompetencyID
			}
			spof = append(spof, key)
		}
	}
	m9 := 1.0
	if mustTotal > 0 {
		m9 = float64(twice) / float64(mustTotal)
		c.detail.MinBusFactor = intPtr(minN)
	}
	c.values.M9 = clamp01(m9)
	c.detail.SinglePointsOfFailure = spof
}

// ---------- M10 focus ----------
// M10 = 1 - (0.4*frag + 0.3*ctx + 0.3*size_pen).
func (c *variantComputation) computeFocus() {
	// frag = share of assignment rows below 20% of a 40 hour week (< 8 h).
	rows, fragmented := 0, 0
	for _, a := range c.proposal.Assignments {
		if a.Hours <= 0 {
			continue
		}
		rows++
		if a.Hours < FragmentationThresholdHours {
			fragmented++
		}
	}
	frag := 0.0
	if rows > 0 {
		frag = float64(fragmented) / float64(rows)
	}

	// ctx = min(1, (mean concurrent projects per member - 1) / 3).
	projects, counted := 0.0, 0
	for _, empID := range c.members {
		cand, ok := c.candidates[empID]
		if !ok {
			continue
		}
		projects += float64(len(cand.CurrentProjectIDs))
		counted++
	}
	meanProjects := 0.0
	if counted > 0 {
		meanProjects = projects / float64(counted)
	}
	ctx := clamp01((meanProjects - 1) / 3)

	// n_opt = ceil(sum(E_likely) / (T_ref * workdays_per_week * 0.8)).
	totalLikely := 0.0
	for _, req := range c.requirements {
		totalLikely += req.Effort.LikelyPersonDays
	}
	tRef := float64(len(c.snapshot.Horizon()))
	if deadline := c.snapshot.Constraints().DeadlineWeek; deadline != "" {
		if weeks := c.weeksToDeadline(); weeks > 0 {
			tRef = weeks
		}
	}
	nOpt := 1
	if tRef > 0 {
		nOpt = int(math.Ceil(totalLikely / (tRef * WorkdaysPerWeek * 0.8)))
	}
	if nOpt < 1 {
		nOpt = 1
	}
	sizePenalty := math.Min(1, math.Max(0, float64(len(c.members)-nOpt)/float64(nOpt)))

	m10 := 1 - (0.4*frag + 0.3*ctx + 0.3*sizePenalty)
	c.values.M10 = clamp01(m10)
	c.detail.FragmentationShare = floatPtr(clamp01(frag))
	c.detail.ConcurrentProjectsMean = floatPtr(meanProjects)
	c.detail.NOpt = intPtr(nOpt)
}

// ---------- M11 bench utilization ----------
// bench hours assigned to the variant / bench hours available in the horizon (clipped).
func (c *variantComputation) computeBench() {
	assigned := 0.0
	for _, empID := range c.members {
		cand, ok := c.candidates[empID]
		if ok && cand.BenchFlag {
			assigned += c.memberHours[empID]
		}
	}
	available := 0.0
	for _, cand := range c.candidates {
		if !cand.BenchFlag {
			continue
		}
		for _, row := range cand.Capacity {
			if !row.Unavailable && row.FreeHours > 0 {
				available += row.FreeHours
			}
		}
	}
	m11 := 0.0
	if available > 0 {
		m11 = clamp01(assigned / available)
	}
	c.values.M11 = clamp01(m11)
	c.detail.BenchHoursAssigned = floatPtr(assigned)
	c.detail.BenchHoursAvailable = floatPtr(available)
}

// ---------- M5 schedule ----------
// M5 = P(T <= D) when a deadline exists, otherwise T_best / T50.
func (c *variantComputation) finalizeScheduleMetrics(tBest float64, tBestSet bool) {
	if c.scheduleErr != nil {
		c.values.M5 = 0
		return
	}
	duration := c.baseSchedule.Duration
	if c.snapshot.Constraints().DeadlineWeek != "" && duration.POnTime != nil {
		c.values.M5 = clamp01(*duration.POnTime)
		return
	}
	if tBestSet && tBest > 0 && duration.P50Weeks > 0 {
		c.values.M5 = clamp01(tBest / duration.P50Weeks)
		c.detail.TBestWeeks = floatPtr(tBest)
		return
	}
	c.values.M5 = 0
}

// ---------- M6 cost ----------
// M6 = clamp(1 - (C - C_min)/(C_ref - C_min), 0, 1). A run whose variants all cost the
// same has C_ref == C_min; every variant is then the cheapest one and M6 = 1.
func (c *variantComputation) finalizeCost(cMin, cRef float64, ok bool) {
	m6 := 1.0
	if ok {
		if cRef > cMin {
			m6 = clamp01(1 - (c.cost-cMin)/(cRef-cMin))
		}
		c.detail.CostMinUnits = floatPtr(cMin)
		c.detail.CostRefUnits = floatPtr(cRef)
	}
	c.values.M6 = clamp01(m6)
}

// ---------- M12 resilience ----------
// For every member: zero that member's assignments in the two consecutive weeks with
// the highest assigned hours, recompute T80 with the same seed and survive when no must
// requirement loses its last qualified member and T80_new <= 1.2 * T80_base.
func (c *variantComputation) finalizeResilience() {
	baseP80 := c.baseSchedule.Duration.P80Weeks
	if c.scheduleErr != nil || baseP80 <= 0 || len(c.members) == 0 {
		c.values.M12 = 0
		c.detail.ResilienceScore = floatPtr(0)
		return
	}
	survived := 0
	for _, empID := range c.members {
		if c.losesLastQualifiedMember(empID) {
			continue
		}
		result, err := c.scheduleFor(c.dropoutAssignments(empID))
		if err != nil {
			continue
		}
		if result.Duration.P80Weeks <= DropoutTolerance*baseP80+1e-12 {
			survived++
		}
	}
	m12 := float64(survived) / float64(len(c.members))
	c.values.M12 = clamp01(m12)
	c.detail.ResilienceScore = floatPtr(m12)
}

// dropoutWindow returns the two consecutive horizon weeks (one for a single-week
// horizon) in which the member has the most assigned hours. Ties resolve to the
// earliest window, which keeps the simulation deterministic.
func (c *variantComputation) dropoutWindow(employeeID string) map[string]bool {
	horizon := c.snapshot.Horizon()
	weeks := 1
	if len(horizon) >= DropoutWeeks {
		weeks = DropoutWeeks
	}
	hours := c.empWeekHours[employeeID]
	bestStart := 0
	bestSum := -1.0
	for i := 0; i+weeks <= len(horizon); i++ {
		sum := 0.0
		for j := 0; j < weeks; j++ {
			sum += hours[horizon[i+j]]
		}
		if sum > bestSum {
			bestSum = sum
			bestStart = i
		}
	}
	window := make(map[string]bool, weeks)
	for j := 0; j < weeks; j++ {
		window[horizon[bestStart+j]] = true
	}
	return window
}

// dropoutAssignments returns the assignment rows with the member's dropout window removed.
func (c *variantComputation) dropoutAssignments(employeeID string) []ProposalAssignment {
	window := c.dropoutWindow(employeeID)
	out := make([]ProposalAssignment, 0, len(c.proposal.Assignments))
	for _, a := range c.proposal.Assignments {
		if a.EmployeeID == employeeID && window[a.Week] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// losesLastQualifiedMember reports whether dropping the member leaves a must requirement
// without any qualified member (the G3-equivalent condition of M12).
func (c *variantComputation) losesLastQualifiedMember(employeeID string) bool {
	for _, req := range c.requirements {
		if !req.IsMust() || !c.qualified(employeeID, req) {
			continue
		}
		remaining := 0
		for _, other := range c.members {
			if other == employeeID {
				continue
			}
			if c.qualified(other, req) {
				remaining++
			}
		}
		if remaining == 0 {
			return true
		}
	}
	return false
}

// ---------- composite score ----------
// Score = Gate * sum(lambda_k * M_k) with lambda renormalized over present metrics.
func (c *variantComputation) finalizeScore(weights map[string]float64) {
	// Report every component with fixed precision first, so the served score is
	// exactly reproducible from the served metric_values.
	c.values.M1 = round6(clamp01(c.values.M1))
	c.values.M2 = round6(clamp01(c.values.M2))
	c.values.M3 = round6(clamp01(c.values.M3))
	if c.values.M4 != nil {
		value := round6(clamp01(*c.values.M4))
		c.values.M4 = &value
	}
	c.values.M5 = round6(clamp01(c.values.M5))
	c.values.M6 = round6(clamp01(c.values.M6))
	c.values.M7 = round6(clamp01(c.values.M7))
	c.values.M8 = round6(clamp01(c.values.M8))
	c.values.M9 = round6(clamp01(c.values.M9))
	c.values.M10 = round6(clamp01(c.values.M10))
	c.values.M11 = round6(clamp01(c.values.M11))
	c.values.M12 = round6(clamp01(c.values.M12))

	score := 0.0
	if c.gate.Pass() {
		numerator, denominator := 0.0, 0.0
		for _, key := range Keys {
			value, present := c.values.value(key)
			if !present {
				continue
			}
			weight := weights[key]
			if weight <= 0 {
				continue
			}
			numerator += weight * value
			denominator += weight
		}
		if denominator > 0 {
			score = clamp01(numerator / denominator)
		}
	}
	c.values.Score = round6(clamp01(score))
}
