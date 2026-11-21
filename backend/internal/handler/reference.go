package handler

import (
	"embed"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/teamopt/backend/internal/domain"

	"gopkg.in/yaml.v3"
)

//go:embed contracts/reference.yaml
var referenceYAML embed.FS

//go:embed contracts/taxonomy.yaml
var taxonomyYAML embed.FS

type ReferenceHandler struct{}

func NewReferenceHandler() *ReferenceHandler {
	return &ReferenceHandler{}
}

func (h *ReferenceHandler) Register(r chi.Router) {
	r.Get("/reference/grades", h.Grades)
	r.Get("/reference/weight-presets", h.WeightPresets)
	r.Get("/reference/metrics", h.Metrics)
	r.Get("/reference/planning-params", h.PlanningParams)
}

type GradeBand struct {
	Grade             string   `yaml:"grade" json:"grade"`
	LabelRU           string   `yaml:"label_ru" json:"label_ru"`
	CostCoefficient   float64  `yaml:"cost_coefficient" json:"cost_coefficient"`
	TypicalLevelRange []int    `yaml:"typical_level_range" json:"typical_level_range"`
}

type WeightPreset struct {
	Preset      string             `yaml:"preset" json:"preset"`
	LabelRU     string             `yaml:"label_ru" json:"label_ru"`
	Description string             `yaml:"description_ru,omitempty" json:"description,omitempty"`
	Weights     map[string]float64 `yaml:"weights" json:"weights"`
	SumCheck    float64            `yaml:"sum" json:"sum"`
}

type referenceFile struct {
	Grades        []GradeBand   `yaml:"grades"`
	WeightPresets []WeightPreset `yaml:"weights_presets"`
}

type MetricDefinition struct {
	ID                 string    `yaml:"id" json:"id"`
	NameRU             string    `yaml:"name_ru" json:"name_ru"`
	NameEN             string    `yaml:"name_en" json:"name_en"`
	FormulaRU          string    `yaml:"formula_ru" json:"formula_ru"`
	InterpretationRU   string    `yaml:"interpretation_ru" json:"interpretation_ru"`
	IsCore             bool      `yaml:"is_core" json:"is_core"`
	TypicalWeightRange []float64 `yaml:"typical_weight_range" json:"typical_weight_range"`
}

type metricsFile struct {
	Metrics []MetricDefinition `yaml:"metrics"`
}

type PlanningParams struct {
	TimeModel struct {
		WeekStart          string  `yaml:"week_start" json:"week_start"`
		WorkingDaysPerWeek int     `yaml:"working_days_per_week" json:"working_days_per_week"`
		HoursPerDay        int     `yaml:"hours_per_day" json:"hours_per_day"`
		WeeklyHoursNorm    int     `yaml:"weekly_hours_norm" json:"weekly_hours_norm"`
		ReserveRatio       float64 `yaml:"reserve_ratio" json:"reserve_ratio"`
		Calendar           string  `yaml:"calendar" json:"calendar"`
	} `yaml:"time_model" json:"time_model"`
	Schedule struct {
		MonteCarloRuns          int     `yaml:"monte_carlo_runs" json:"monte_carlo_runs"`
		MonteCarloSeed          int     `yaml:"monte_carlo_seed" json:"monte_carlo_seed"`
		PortfolioTauWorkingDays float64 `yaml:"portfolio_tau_working_days" json:"portfolio_tau_working_days"`
	} `yaml:"schedule" json:"schedule"`
	Solver struct {
		DefaultTimeLimitMS  int     `yaml:"default_time_limit_ms" json:"default_time_limit_ms"`
		RecalcTimeLimitMS   int     `yaml:"recalc_time_limit_ms" json:"recalc_time_limit_ms"`
		KMin                int     `yaml:"k_min" json:"k_min"`
		KMax                int     `yaml:"k_max" json:"k_max"`
		MinJaccardDiversity float64 `yaml:"min_jaccard_diversity" json:"min_jaccard_diversity"`
	} `yaml:"solver" json:"solver"`
	Limits struct {
		MaxEmployees    int `yaml:"max_employees" json:"max_employees"`
		MaxRequirements int `yaml:"max_requirements" json:"max_requirements"`
	} `yaml:"limits" json:"limits"`
	MetricsVersion string `yaml:"metrics_version" json:"metrics_version"`
}

func (h *ReferenceHandler) Grades(w http.ResponseWriter, r *http.Request) {
	data, err := referenceYAML.ReadFile("contracts/reference.yaml")
	if err != nil {
		writeProblem(w, problem500(r, err))
		return
	}
	var ref referenceFile
	if err := yaml.Unmarshal(data, &ref); err != nil {
		writeProblem(w, problem500(r, err))
		return
	}
	writeJSON(w, 200, ref.Grades)
}

func (h *ReferenceHandler) WeightPresets(w http.ResponseWriter, r *http.Request) {
	data, err := referenceYAML.ReadFile("contracts/reference.yaml")
	if err != nil {
		writeProblem(w, problem500(r, err))
		return
	}
	var ref referenceFile
	if err := yaml.Unmarshal(data, &ref); err != nil {
		writeProblem(w, problem500(r, err))
		return
	}

	// Build metrics map from taxonomy.yaml level descriptors (or hardcode for now)
	metricsMap := map[string]map[string]string{
		"M1": {"name_ru": "Покрытие компетенций", "name_en": "Competency Coverage", "description_ru": "Доля требуемых компетенций, покрытых командой на нужном уровне"},
		"M2": {"name_ru": "Покрытие трудозатрат", "name_en": "Effort Coverage", "description_ru": "Доля плановых человеко-дней, которые команда может закрыть"},
		"M3": {"name_ru": "Соответствие уровней", "name_en": "Level Fit", "description_ru": "Насколько уровни сотрудников близки к требуемым (штраф за пере/недоквалификацию)"},
		"M4": {"name_ru": "Релевантность опыта", "name_en": "Experience Relevance", "description_ru": "Семантическая близость прошлых проектов сотрудников к текущему ТЗ"},
		"M5": {"name_ru": "График", "name_en": "Schedule", "description_ru": "Вероятность уложиться в дедлайн или относительная скорость (P50)"},
		"M6": {"name_ru": "Стоимость", "name_en": "Cost", "description_ru": "Относительная стоимость команды (чем ниже — тем лучше)"},
		"M7": {"name_ru": "Здоровье нагрузки", "name_en": "Load Health", "description_ru": "Отсутствие перегрузов (>90% утилизации) и простоев"},
		"M8": {"name_ru": "Влияние на портфель", "name_en": "Portfolio Impact", "description_ru": "Задержки других проектов из-за вытягивания людей"},
		"M9": {"name_ru": "Bus-factor", "name_en": "Bus Factor", "description_ru": "Доля must-компетенций, закрытых минимум 2 людьми"},
		"M10": {"name_ru": "Фокус", "name_en": "Focus", "description_ru": "Низкая фрагментация, мало параллельных проектов, оптимальный размер команды"},
		"M11": {"name_ru": "Использование бенча", "name_en": "Bench Utilization", "description_ru": "Доля часов сотрудников с bench_flag, вовлеченных в проект"},
		"M12": {"name_ru": "Устойчивость", "name_en": "Resilience", "description_ru": "Способность команды сохранить выполнимость при отвале любого участника на 2 недели"},
	}

	response := map[string]any{
		"presets": ref.WeightPresets,
		"metrics": metricsMap,
	}
	writeJSON(w, 200, response)
}

func (h *ReferenceHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	// Parse from reference.yaml or hardcode full definitions
	metrics := []MetricDefinition{
		{ID: "M1", NameRU: "Покрытие компетенций", NameEN: "Competency Coverage", FormulaRU: "Σ(w_r × min(1, lvl_ir / L_r)) / Σ(w_r)", InterpretationRU: "1.0 = все компетенции закрыты на требуемом уровне", IsCore: true, TypicalWeightRange: []float64{0.10, 0.20}},
		{ID: "M2", NameRU: "Покрытие трудозатрат", NameEN: "Effort Coverage", FormulaRU: "Σ(min(E_r, Σ(a_irw × q_ir × d_w))) / Σ(E_r)", InterpretationRU: "Доля закрытых человеко-дней; дефициты видны в деталях варианта", IsCore: true, TypicalWeightRange: []float64{0.10, 0.15}},
		{ID: "M3", NameRU: "Соответствие уровней", NameEN: "Level Fit", FormulaRU: "1 - |lvl - L| / 4 (недоуровень ×1.5, переуровень ≥2 ×1.0)", InterpretationRU: "Штраф за недоквалификацию выше, чем за переквалификацию", IsCore: true, TypicalWeightRange: []float64{0.05, 0.12}},
		{ID: "M4", NameRU: "Релевантность опыта", NameEN: "Experience Relevance", FormulaRU: "cosine(brief_embedding, project_descriptions) взвешено по часам", InterpretationRU: "Требует истории проектов; при отсутствии — вес переносится на остальные", IsCore: false, TypicalWeightRange: []float64{0.05, 0.10}},
		{ID: "M5", NameRU: "График", NameEN: "Schedule", FormulaRU: "P(T ≤ D) при дедлайне; иначе T_best / T50", InterpretationRU: "Монте-Карло P50/P80 по трёхточечной оценке усилий", IsCore: true, TypicalWeightRange: []float64{0.08, 0.30}},
		{ID: "M6", NameRU: "Стоимость", NameEN: "Cost", FormulaRU: "1 - (C - C_min) / (C_ref - C_min)", InterpretationRU: "Относительная дешевизна; C_min — самый дешевый feasible вариант", IsCore: true, TypicalWeightRange: []float64{0.04, 0.27}},
		{ID: "M7", NameRU: "Здоровье нагрузки", NameEN: "Load Health", FormulaRU: "1 - mean(min(1, max(0, u - 0.9) / 0.1))", InterpretationRU: "Штраф за утилизацию >90%; идеально 60-90%", IsCore: true, TypicalWeightRange: []float64{0.05, 0.12}},
		{ID: "M8", NameRU: "Влияние на портфель", NameEN: "Portfolio Impact", FormulaRU: "exp(-Σ(slip_p) / τ), τ=10 рабочих дней", InterpretationRU: "Экспоненциальный штраф за суммарные задержки других проектов", IsCore: true, TypicalWeightRange: []float64{0.05, 0.10}},
		{ID: "M9", NameRU: "Bus-factor", NameEN: "Bus Factor", FormulaRU: "|{r ∈ must: N_r ≥ 2}| / |must|", InterpretationRU: "Доля must-компетенций с резервом ≥2 человек", IsCore: true, TypicalWeightRange: []float64{0.03, 0.10}},
		{ID: "M10", NameRU: "Фокус", NameEN: "Focus", FormulaRU: "1 - (0.4×frag + 0.3×ctx + 0.3×size_pen)", InterpretationRU: "Штрафы за фрагментацию (<20%), контекст-свитчинг, избыточный размер", IsCore: true, TypicalWeightRange: []float64{0.03, 0.05}},
		{ID: "M11", NameRU: "Использование бенча", NameEN: "Bench Utilization", FormulaRU: "bench_hours_assigned / bench_hours_available", InterpretationRU: "Прямо связано с проблемой «скамейки» из паспорта проекта", IsCore: true, TypicalWeightRange: []float64{0.01, 0.03}},
		{ID: "M12", NameRU: "Устойчивость", NameEN: "Resilience", FormulaRU: "Доля сценариев drop-out 2 нед., где Gate=1 и ΔT80 ≤ 20%", InterpretationRU: "Симуляция потери каждого участника", IsCore: true, TypicalWeightRange: []float64{0.01, 0.10}},
	}
	writeJSON(w, 200, map[string]any{"metrics": metrics})
}

func (h *ReferenceHandler) PlanningParams(w http.ResponseWriter, r *http.Request) {
	data, err := referenceYAML.ReadFile("contracts/reference.yaml")
	if err != nil {
		writeProblem(w, problem500(r, err))
		return
	}
	var params PlanningParams
	if err := yaml.Unmarshal(data, &params); err != nil {
		writeProblem(w, problem500(r, err))
		return
	}
	writeJSON(w, 200, params)
}

func problem500(r *http.Request, err error) domain.Problem {
	rid, ok := r.Context().Value("request_id").(string)
	if !ok || rid == "" {
		rid = "unknown"
	}
	return domain.Problem{
		Type:        "https://teamopt.internal/problems/internal",
		Title:       "Internal Server Error",
		Status:      500,
		Code:        "INTERNAL_ERROR",
		Detail:      err.Error(),
		RequestID:   rid,
		ReasonCodes: []string{"INTERNAL_ERROR"},
	}
}
