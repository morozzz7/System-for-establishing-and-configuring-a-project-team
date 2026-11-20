package explain

import (
	"fmt"
	"math"
	"sort"

	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// Generate produces the deterministic Explanation for a variant (ADR-007 §2). It is pure
// with respect to the inputs: the same Input always yields a byte-identical Explanation.
func Generate(in Input) (Explanation, error) {
	if in.Snapshot == nil {
		return Explanation{}, fmt.Errorf("explain: snapshot is required")
	}
	locale := in.Locale
	if locale == "" {
		locale = "ru"
	}
	explanations := Explanation{
		VariantID: in.Variant.VariantID,
		RunID:     in.RunID,
		Locale:    locale,
	}

	// Included members.
	for _, m := range in.Detail.Members {
		explanations.Included = append(explanations.Included, memberFactor(in, m, locale))
	}
	sort.Slice(explanations.Included, func(i, j int) bool {
		return idOf(explanations.Included[i]) < idOf(explanations.Included[j])
	})

	// Relevant excluded candidates, in precedence order.
	for _, ex := range CandidateExclusions(in.Snapshot, in.Detail) {
		explanations.Excluded = append(explanations.Excluded, exclusionFactor(in, ex, locale))
	}

	// Deficits.
	for _, d := range in.Variant.Deficits {
		explanations.Deficits = append(explanations.Deficits, deficitFactor(in, d, locale))
	}

	// Headline + tradeoff.
	explanations.Headline, explanations.HeadlineReasonCodes = headline(in, locale)
	explanations.Tradeoff = tradeoff(in, locale)

	// Counterfactuals: prefer rerun-verified ones, fall back to a labelled estimate so
	// there is always at least one.
	explanations.Counterfactuals = counterfactuals(in, locale)

	return explanations, nil
}

func idOf(f Factor) string {
	if f.EmployeeID != nil {
		return *f.EmployeeID
	}
	return ""
}

// ---------- included member ----------

func memberFactor(in Input, m MemberDetail, locale string) Factor {
	candidate, _ := in.Snapshot.CandidateByID(m.EmployeeID)
	cons := in.Snapshot.Constraints()
	facts := map[string]any{
		"employee_id":      m.EmployeeID,
		"role_id":          m.RoleID,
		"grade":            m.Grade,
		"allocated_hours":  round2PctH(m.AllocatedHours),
		"assignment_share": round2(m.AssignmentShare),
		"competence_fit":   round2(m.CompetenceFit),
	}
	reasons := []string{"INCLUDE_REQUIRED_ROLE"}

	// High competence: the deepest level the member meets across their assignments.
	if comp := topCompetence(in, m.EmployeeID); comp != nil {
		reasons = append(reasons, "INCLUDE_HIGH_COMPETENCE")
		facts["competency"] = comp.id
		facts["level"] = comp.level
		facts["required_level"] = comp.required
	}
	if hasFreeCapacity(candidate) {
		reasons = append(reasons, "INCLUDE_AVAILABLE_CAPACITY")
		facts["free_hours"] = round2PctH(sumFree(candidate))
	}
	if candidate.BenchFlag {
		reasons = append(reasons, "INCLUDE_BENCH")
	}
	if isBusFactor(in.Snapshot, m.EmployeeID) {
		reasons = append(reasons, "INCLUDE_BUS_FACTOR")
	}
	if contains(cons.ForceInclude, m.EmployeeID) {
		reasons = append(reasons, "INCLUDE_USER_FORCE")
	}
	if candidate.ExperienceScore != nil && *candidate.ExperienceScore >= 0.6 {
		reasons = append(reasons, "INCLUDE_EXPERIENCE_RELEVANT")
		facts["experience_score"] = round2(*candidate.ExperienceScore)
	}
	if candidate.CostCoefficient > 0 && candidate.CostCoefficient <= 1.4 {
		reasons = append(reasons, "INCLUDE_COST_EFFICIENT")
		facts["cost_coefficient"] = candidate.CostCoefficient
	}

	hours := fmt.Sprintf("%v", round2PctH(m.AllocatedHours))
	fit := fmt.Sprintf("%v", round2(m.CompetenceFit))
	text := localeText(locale,
		fmt.Sprintf("Включён: %s (гр. %s), нагрузка %s ч, соответствие %s.", m.RoleID, m.Grade, hours, fit),
		fmt.Sprintf("Included: %s (grade %s), allocation %s h, fit %s.", m.RoleID, m.Grade, hours, fit))
	if cid, ok := facts["competency"]; ok {
		lvl := fmt.Sprintf("%v", facts["level"])
		req := fmt.Sprintf("%v", facts["required_level"])
		text += " " + localeText(locale,
			fmt.Sprintf("Уровень %s %s при требуемом %s.", cid, lvl, req),
			fmt.Sprintf("Level %s %s (required %s).", cid, lvl, req))
	}
	return Factor{EmployeeID: strPtr(m.EmployeeID), Text: text[:minInt(len(text), 600)], ReasonCodes: reasons, Facts: facts}
}

// topCompetence returns the requirement competency where the member most exceeds the
// required level among the requirements they are assigned to.
func topCompetence(in Input, employeeID string) *compFact {
	cand, ok := in.Snapshot.CandidateByID(employeeID)
	if !ok {
		return nil
	}
	best := (*compFact)(nil)
	for _, a := range in.Detail.WeeklyAssignments {
		if a.EmployeeID != employeeID || a.Hours <= 0 {
			continue
		}
		req, ok := in.Snapshot.RequirementByID(a.RequirementID)
		if !ok || req.CompetencyID == nil {
			continue
		}
		lvl := cand.CompetencyLevels[*req.CompetencyID]
		if lvl > 0 && lvl >= req.Level {
			cf := compFact{id: *req.CompetencyID, level: lvl, required: req.Level}
			if best == nil || lvl-req.Level > best.level-best.required {
				best = &cf
			}
		}
	}
	return best
}

type compFact struct {
	id              string
	level, required int
}

func isBusFactor(snap *runinput.Snapshot, employeeID string) bool {
	cand, ok := snap.CandidateByID(employeeID)
	if !ok {
		return false
	}
	// The member contributes to the bus factor of a must requirement they back up with at
	// least one other qualified member.
	for _, req := range snap.Requirements() {
		if !req.IsMust() {
			continue
		}
		if !qualified(cand, req) {
			continue
		}
		// Count qualified candidates for this must requirement.
		count := 0
		for _, other := range snap.Candidates() {
			if qualified(other, req) {
				count++
			}
		}
		if count >= 2 {
			return true
		}
	}
	return false
}

func qualified(c runinput.Candidate, req runinput.Requirement) bool {
	if req.CompetencyID == nil {
		return c.HasRole(req.RoleID)
	}
	return c.CompetencyLevels[*req.CompetencyID] >= req.Level
}

func hasFreeCapacity(candidate runinput.Candidate) bool {
	for _, row := range candidate.Capacity {
		if !row.Unavailable && row.FreeHours > 0 {
			return true
		}
	}
	return false
}

func sumFree(candidate runinput.Candidate) float64 {
	total := 0.0
	for _, row := range candidate.Capacity {
		if !row.Unavailable {
			total += row.FreeHours
		}
	}
	return total
}

// ---------- excluded candidate ----------

func exclusionFactor(in Input, ex Exclusion, locale string) Factor {
	facts := map[string]any{"employee_id": ex.EmployeeID}
	for k, v := range ex.Facts {
		facts[k] = v
	}
	text := exclusionText(locale, ex)
	return Factor{EmployeeID: strPtr(ex.EmployeeID), Text: text[:minInt(len(text), 600)], ReasonCodes: ex.ReasonCodes, Facts: facts}
}

func exclusionText(locale string, ex Exclusion) string {
	switch ex.ReasonCodes[0] {
	case "EXCLUDE_USER_FORCE":
		return localeText(locale,
			fmt.Sprintf("Исключён пользователем."),
			"Excluded by the manager.")
	case "EXCLUDE_ABSENT":
		w := fmt.Sprintf("%v", ex.Facts["absent_weeks"])
		return localeText(locale,
			fmt.Sprintf("Недоступен: %s недель отсутствия/отпуска.", w),
			fmt.Sprintf("Unavailable: %s weeks of absence/leave.", w))
	case "EXCLUDE_OVERLOADED":
		pct := fmt.Sprintf("%v", ex.Facts["overload_pct"])
		return localeText(locale,
			fmt.Sprintf("Перегружен: перегрузка %s%%.", pct),
			fmt.Sprintf("Overloaded: %s%% overload.", pct))
	case "EXCLUDE_BELOW_REQUIRED_LEVEL":
		return localeText(locale,
			"Уровень компетенции ниже требуемого.",
			"Competency level is below the required level.")
	case "EXCLUDE_TEAM_SIZE_LIMIT":
		n := fmt.Sprintf("%v", ex.Facts["max_team_size"])
		return localeText(locale,
			fmt.Sprintf("Лимит команды (%s) достигнут.", n),
			fmt.Sprintf("Team size limit (%s) reached.", n))
	case "EXCLUDE_NO_FREE_CAPACITY":
		return localeText(locale,
			"Нет свободной ёмкости в горизонте.",
			"No free capacity in the horizon.")
	case "EXCLUDE_PORTFOLIO_PROTECTION":
		return localeText(locale,
			"Занят в защищаемом проекте портфеля.",
			"Committed to a protected portfolio project.")
	case "EXCLUDE_EXPENSIVE":
		coef := fmt.Sprintf("%v", ex.Facts["cost_coefficient"])
		return localeText(locale,
			fmt.Sprintf("Дорогой: коэффициент стоимости %s.", coef),
			fmt.Sprintf("Expensive: cost coefficient %s.", coef))
	case "EXCLUDE_DATA_INCOMPLETE":
		return localeText(locale,
			"Данные о профиле неполные.",
			"Profile data is incomplete.")
	default:
		return localeText(locale,
			"Релевантен, но не выбран в этом варианте.",
			"Relevant, but not selected in this variant.")
	}
}

// ---------- deficit ----------

func deficitFactor(in Input, d metrics.Deficit, locale string) Factor {
	req, _ := in.Snapshot.RequirementByID(d.RequirementID)
	personDays := round2(d.PersonDays)
	facts := map[string]any{
		"requirement_id": d.RequirementID,
		"role_id":        d.RoleID,
		"person_days":    personDays,
		"external":       d.ExternalCooperationFlag,
	}
	if d.CompetencyID != nil {
		facts["competency_id"] = *d.CompetencyID
	}
	codes := []string{"DEFICIT_MUST_ROLE"}
	reqName := d.RoleID
	if cid, ok := facts["competency_id"]; ok {
		reqName = cid.(string)
		codes = []string{"DEFICIT_MUST_ROLE"}
	}
	_ = req
	pd := fmt.Sprintf("%v", personDays)
	text := localeText(locale,
		fmt.Sprintf("Дефицит по %s: %s человеко-дней.", reqName, pd),
		fmt.Sprintf("Deficit of %s: %s person-days.", reqName, pd))
	if d.ExternalCooperationFlag {
		codes = append(codes, "DEFICIT_EXTERNAL_CONTRACTOR")
		text += " " + localeText(locale, "Требуется внешний подрядчик.", "External cooperation required.")
	}
	return Factor{CompetencyID: d.CompetencyID, Text: text[:minInt(len(text), 600)], ReasonCodes: codes, Facts: facts}
}

// ---------- headline / tradeoff ----------

func headline(in Input, locale string) (string, []string) {
	v := in.Variant
	score := round2(v.Metrics.Score)
	p50 := round2(v.Duration.P50Weeks)
	deficit := round2(sumDeficitPD(v.Deficits))
	codes := []string{"VARIANT_BALANCED"}
	if deficit > 0 {
		codes = append(codes, "DEFICIT_MUST_ROLE")
	}
	title := v.Label
	if title == "" {
		title = v.VariantID
	}
	text := localeText(locale,
		fmt.Sprintf("Вариант %s: P50 %v нед., дефицит %v чел.-дн., балл %v.", title, p50, deficit, score),
		fmt.Sprintf("Variant %s: P50 %v weeks, deficit %v person-days, score %v.", title, p50, deficit, score))
	return text, codes
}

func tradeoff(in Input, locale string) Tradeoff {
	v := in.Variant
	p50 := round2(v.Duration.P50Weeks)
	deficit := round2(sumDeficitPD(v.Deficits))
	facts := map[string]any{
		"p50_weeks":           p50,
		"deficit_person_days": deficit,
		"score":               round2(v.Metrics.Score),
	}
	codes := []string{"TRADEOFF_LOAD_VS_SPEED"}
	p50s := fmt.Sprintf("%v", p50)
	switch {
	case deficit > 0:
		codes = []string{"TRADEOFF_SPEED_VS_DEFICIT"}
		dd := fmt.Sprintf("%v", deficit)
		return Tradeoff{
			Text: localeText(locale,
				fmt.Sprintf("Скорость (P50 %s нед.) достигается ценой дефицита %s чел.-дн.", p50s, dd),
				fmt.Sprintf("Speed (P50 %s weeks) at the cost of a %s person-day deficit.", p50s, dd)),
			ReasonCodes: codes, Facts: facts,
		}
	default:
		codes = []string{"TRADEOFF_LOAD_VS_SPEED"}
		return Tradeoff{
			Text: localeText(locale,
				fmt.Sprintf("Сбалансировано: P50 %s нед., дефицит отсутствует.", p50s),
				fmt.Sprintf("Balanced: P50 %s weeks, no deficit.", p50s)),
			ReasonCodes: codes, Facts: facts,
		}
	}
}

// ---------- counterfactuals ----------

func counterfactuals(in Input, locale string) []Counterfactual {
	out := make([]Counterfactual, 0, 3)
	if cf, ok := capacityReliefCounterfactual(in.Snapshot, in.Detail, in.Schedule, in.RunID, locale); ok {
		out = append(out, cf)
	}
	if cf, ok := teamSizeCounterfactual(in.Snapshot, in.Detail, in.Schedule, in.RunID, locale); ok {
		out = append(out, cf)
	}
	if cf, ok := deadlineCounterfactual(in.Snapshot, in.Detail, in.Schedule, in.RunID, locale); ok {
		out = append(out, cf)
	}
	if len(out) == 0 {
		out = append(out, estimatedCounterfactual(in.Snapshot, in.Detail, in.RunID, locale))
	}
	return out
}

// ---------- helpers ----------

func sumDeficitPD(deficits []metrics.Deficit) float64 {
	total := 0.0
	for _, d := range deficits {
		total += d.PersonDays
	}
	return total
}

func round2PctH(v float64) float64 { return math.Round(v*100) / 100 }

func strPtr(s string) *string { return &s }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
