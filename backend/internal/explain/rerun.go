package explain

import (
	"fmt"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// ProposalFromDetail reconstructs the metrics.Proposal of a variant from its detail, so
// the metrics/schedule engine can be re-run for a counterfactual.
func ProposalFromDetail(detail VariantDetail) metrics.Proposal {
	p := metrics.Proposal{
		VariantID:   detail.VariantID,
		Label:       detail.VariantID,
		Members:     make([]metrics.ProposalMember, 0, len(detail.Members)),
		Assignments: make([]metrics.ProposalAssignment, 0, len(detail.WeeklyAssignments)),
	}
	for _, m := range detail.Members {
		p.Members = append(p.Members, metrics.ProposalMember{
			EmployeeID: m.EmployeeID, RoleID: m.RoleID, Grade: m.Grade,
		})
	}
	for _, a := range detail.WeeklyAssignments {
		p.Assignments = append(p.Assignments, metrics.ProposalAssignment{
			EmployeeID: a.EmployeeID, RequirementID: a.RequirementID, Week: a.Week, Hours: a.Hours,
		})
	}
	return p
}

// computeSingle re-runs the authoritative metrics/schedule for a single variant over a
// (possibly derived) snapshot. It is the "rerun" that verifies a counterfactual.
func computeSingle(snap *runinput.Snapshot, detail VariantDetail, cfg metrics.ScheduleConfig) (metrics.VariantResult, error) {
	res, err := metrics.ComputeRun(metrics.RunInput{
		Snapshot:  snap,
		Proposals: []metrics.Proposal{ProposalFromDetail(detail)},
		Schedule:  cfg,
	})
	if err != nil {
		return metrics.VariantResult{}, err
	}
	if len(res.Variants) == 0 {
		return metrics.VariantResult{}, fmt.Errorf("no variant computed")
	}
	v := res.Variants[0]
	if v.ScheduleError != "" {
		return v, fmt.Errorf("schedule: %s", v.ScheduleError)
	}
	return v, nil
}

// derivedSnapshotRelief builds a copy of the snapshot in which an employee is released
// from `delta` existing (other-project) assigned hours in one week. Releasing load is the
// "release from project X" counterfactual of AGENTS.md; it never violates runinput
// validation because only assigned hours are reduced and free hours are recomputed.
func derivedSnapshotRelief(snap *runinput.Snapshot, employeeID, week string, delta float64) (*runinput.Snapshot, error) {
	data := snap.Data()
	for c := range data.Candidates {
		if data.Candidates[c].EmployeeID != employeeID {
			continue
		}
		for r := range data.Candidates[c].Capacity {
			row := data.Candidates[c].Capacity[r]
			if row.Week != week {
				continue
			}
			row.AssignedHours = math.Max(0, row.AssignedHours-delta)
			row.FreeHours = row.NormHours - row.AssignedHours - row.AbsentHours
			if row.FreeHours < 0 {
				row.FreeHours = 0
			}
			data.Candidates[c].Capacity[r] = row
		}
	}
	return runinput.Build(data)
}

// constrainedSlot returns the employee and week with the most existing load that can be
// released (AssignedHours > 0), preferring the week with the least free hours. It returns
// empty strings when there is nothing to release.
func constrainedSlot(snap *runinput.Snapshot, detail VariantDetail) (string, string) {
	members := make(map[string]bool, len(detail.Members))
	for _, m := range detail.Members {
		members[m.EmployeeID] = true
	}
	bestEmp, bestWeek := "", ""
	bestScore := math.Inf(1)
	for _, c := range snap.Candidates() {
		if !members[c.EmployeeID] {
			continue
		}
		for _, row := range c.Capacity {
			if row.AssignedHours <= 0 {
				continue
			}
			free := row.FreeHours
			if row.Unavailable {
				free = 0
			}
			// The most constrained week is the one with the least free headroom.
			if free < bestScore {
				bestScore = free
				bestEmp, bestWeek = c.EmployeeID, row.Week
			}
		}
	}
	return bestEmp, bestWeek
}

// round2 rounds a counterfactual fact to two decimals so text numbers and fact numbers
// match exactly for the faithfulness checker.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// capacityReliefCounterfactual emits a rerun-verified COUNTERFACTUAL_CAPACITY: releasing
// the most-loaded member from other work shortens (or leaves) the P50. It returns ok=false
// when there is no load to release, so the caller can fall back to an estimated one.
func capacityReliefCounterfactual(snap *runinput.Snapshot, detail VariantDetail, cfg metrics.ScheduleConfig, runID, locale string) (Counterfactual, bool) {
	empID, week := constrainedSlot(snap, detail)
	if empID == "" {
		return Counterfactual{}, false
	}
	base, err := computeSingle(snap, detail, cfg)
	if err != nil {
		return Counterfactual{}, false
	}
	delta := 8.0
	afterSnap, err := derivedSnapshotRelief(snap, empID, week, delta)
	if err != nil {
		return Counterfactual{}, false
	}
	after, err := computeSingle(afterSnap, detail, cfg)
	if err != nil {
		return Counterfactual{}, false
	}
	before := round2(base.Duration.P50Weeks)
	aft := round2(after.Duration.P50Weeks)
	facts := map[string]any{
		"employee_id":      empID,
		"week":             weekLabel(week),
		"week_number":      weekNum(week),
		"released_hours":   delta,
		"p50_weeks_before": before,
		"p50_weeks_after":  aft,
		"delta_weeks":      round2(before - aft),
	}
	text := localeText(locale,
		fmt.Sprintf("Если освободить %s на %v ч в неделю %s, P50 изменится с %v до %v недель.", empID, delta, weekLabel(week), before, aft),
		fmt.Sprintf("Releasing %s for %v h in week %s would move P50 from %v to %v weeks.", empID, delta, weekLabel(week), before, aft))
	return Counterfactual{
		Text:        text,
		ReasonCodes: []string{"COUNTERFACTUAL_CAPACITY"},
		VerifiedBy:  "rerun",
		RunID:       runID,
		Facts:       facts,
	}, true
}

// teamSizeCounterfactual emits a rerun-verified COUNTERFACTUAL_TEAM_SIZE when removing
// the least-loaded member still leaves a computable schedule. It is skipped when the
// member is the sole coverage of any requirement (the schedule would be undefined).
func teamSizeCounterfactual(snap *runinput.Snapshot, detail VariantDetail, cfg metrics.ScheduleConfig, runID, locale string) (Counterfactual, bool) {
	if len(detail.Members) < 2 {
		return Counterfactual{}, false
	}
	base, err := computeSingle(snap, detail, cfg)
	if err != nil {
		return Counterfactual{}, false
	}
	// The least-loaded member (smallest allocated hours) is the least disruptive to remove.
	least := detail.Members[0]
	for _, m := range detail.Members[1:] {
		if m.AllocatedHours < least.AllocatedHours {
			least = m
		}
	}
	removed := detail
	kept := make([]MemberDetail, 0, len(removed.Members)-1)
	for _, m := range removed.Members {
		if m.EmployeeID == least.EmployeeID {
			continue
		}
		kept = append(kept, m)
	}
	// Remove assignments too, so the schedule does not depend on a dropped member.
	keptAssignments := make([]WeeklyAssignment, 0, len(removed.WeeklyAssignments))
	for _, a := range removed.WeeklyAssignments {
		if a.EmployeeID == least.EmployeeID {
			continue
		}
		keptAssignments = append(keptAssignments, a)
	}
	removed.Members = kept
	removed.WeeklyAssignments = keptAssignments
	after, err := computeSingle(snap, removed, cfg)
	if err != nil {
		return Counterfactual{}, false
	}
	before := round2(base.Duration.P50Weeks)
	aft := round2(after.Duration.P50Weeks)
	facts := map[string]any{
		"employee_id":      least.EmployeeID,
		"team_size_before": len(detail.Members),
		"team_size_after":  len(removed.Members),
		"p50_weeks_before": before,
		"p50_weeks_after":  aft,
		"delta_weeks":      round2(aft - before),
	}
	text := localeText(locale,
		fmt.Sprintf("Если исключить %s, команда уменьшится с %d до %d человек, P50 изменится с %v до %v недель.", least.EmployeeID, len(detail.Members), len(removed.Members), before, aft),
		fmt.Sprintf("Excluding %s would shrink the team from %d to %d, moving P50 from %v to %v weeks.", least.EmployeeID, len(detail.Members), len(removed.Members), before, aft))
	return Counterfactual{
		Text:        text,
		ReasonCodes: []string{"COUNTERFACTUAL_TEAM_SIZE"},
		VerifiedBy:  "rerun",
		RunID:       runID,
		Facts:       facts,
	}, true
}

// deadlineCounterfactual emits a rerun-verified COUNTERFACTUAL_DEADLINE when the run has
// a deadline and the horizon has room to move it by one week.
func deadlineCounterfactual(snap *runinput.Snapshot, detail VariantDetail, cfg metrics.ScheduleConfig, runID, locale string) (Counterfactual, bool) {
	cons := snap.Constraints()
	if cons.DeadlineWeek == "" {
		return Counterfactual{}, false
	}
	horizon := snap.Horizon()
	idx := -1
	for i, week := range horizon {
		if week == cons.DeadlineWeek {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(horizon) {
		return Counterfactual{}, false
	}
	base, err := computeSingle(snap, detail, cfg)
	if err != nil {
		return Counterfactual{}, false
	}
	afterSnap, err := derivedSnapshotDeadline(snap, horizon[idx+1])
	if err != nil {
		return Counterfactual{}, false
	}
	after, err := computeSingle(afterSnap, detail, cfg)
	if err != nil {
		return Counterfactual{}, false
	}
	pBefore, pAfter := ponTime(base), ponTime(after)
	facts := map[string]any{
		"deadline_week_before":   cons.DeadlineWeek,
		"deadline_week_after":    horizon[idx+1],
		"deadline_number_before": weekNum(cons.DeadlineWeek),
		"deadline_number_after":  weekNum(horizon[idx+1]),
		"p_on_time_before":       pBefore,
		"p_on_time_after":        pAfter,
	}
	text := localeText(locale,
		fmt.Sprintf("Если перенести дедлайн с %s на %s, вероятность успеха изменится с %v до %v.", weekLabel(cons.DeadlineWeek), weekLabel(horizon[idx+1]), pBefore, pAfter),
		fmt.Sprintf("Moving the deadline from %s to %s would change the on-time probability from %v to %v.", weekLabel(cons.DeadlineWeek), weekLabel(horizon[idx+1]), pBefore, pAfter))
	return Counterfactual{
		Text:        text,
		ReasonCodes: []string{"COUNTERFACTUAL_DEADLINE"},
		VerifiedBy:  "rerun",
		RunID:       runID,
		Facts:       facts,
	}, true
}

func derivedSnapshotDeadline(snap *runinput.Snapshot, week string) (*runinput.Snapshot, error) {
	data := snap.Data()
	data.Constraints.DeadlineWeek = week
	// A deadline that falls outside the original deadline position is still valid as long
	// as it is within the horizon; runinput.Build validates that.
	return runinput.Build(data)
}

func ponTime(v metrics.VariantResult) float64 {
	if d := v.Duration.POnTime; d != nil {
		return round2(*d)
	}
	return 0
}

func localeText(locale, ru, en string) string {
	if locale == "ru" {
		return ru
	}
	return en
}

// estimatedCounterfactual is a labelled (estimated) fallback so a variant always has at
// least one counterfactual even when no rerun affords a change (e.g. an all-bench pool
// with no load to release and no deadline).
func estimatedCounterfactual(snap *runinput.Snapshot, detail VariantDetail, runID, locale string) Counterfactual {
	deadline := snap.Constraints().DeadlineWeek
	facts := map[string]any{
		"team_size":     len(detail.Members),
		"deadline_week": deadline,
		"estimated":     true,
	}
	text := localeText(locale,
		"Оценочно: сокращение команды на 1 человека увеличит срок примерно на 1 неделю.",
		"Estimated: shrinking the team by 1 person would extend the schedule by about 1 week.")
	return Counterfactual{
		Text:        text,
		ReasonCodes: []string{"COUNTERFACTUAL_TEAM_SIZE"},
		VerifiedBy:  "estimated",
		RunID:       runID,
		Facts:       facts,
	}
}

// sortedMemberIDs returns the team employee ids in stable order (for deterministic text).
func sortedMemberIDs(detail VariantDetail) []string {
	ids := make([]string, 0, len(detail.Members))
	for _, m := range detail.Members {
		ids = append(ids, m.EmployeeID)
	}
	sort.Strings(ids)
	return ids
}

// weekLabel strips the year from an ISO week key so glosses like "неделя 2026-W10" never
// leak a bare year into an explanation (which would be an unsourced number for the
// faithfulness checker).
func weekLabel(week string) string {
	if len(week) >= 6 && week[4] == '-' {
		return week[5:] // "W10" from "2026-W10"
	}
	return week
}

// weekNum returns the ISO week number of the key ("10" for "2026-W10"), so the W-number
// used in a gloss has a numeric fact source.
func weekNum(week string) int {
	if len(week) >= 6 && week[5] <= '9' {
		n := 0
		for _, ch := range week[5:] {
			if ch < '0' || ch > '9' {
				break
			}
			n = n*10 + int(ch-'0')
		}
		return n
	}
	return 0
}
