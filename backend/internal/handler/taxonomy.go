package handler

import (
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/service"
)

type taxonomyFile struct {
	Competencies []struct {
		ID       string   `yaml:"competency_id"`
		NameRU   string   `yaml:"name_ru"`
		Synonyms []string `yaml:"synonyms"`
	} `yaml:"competencies"`
}
type TaxonomyHandler struct {
	path string
	Svc  *service.TaxonomyService
}

func NewTaxonomyHandler(svc *service.TaxonomyService) *TaxonomyHandler {
	path := os.Getenv("TAXONOMY_PATH")
	if path == "" {
		path = "contracts/taxonomy.yaml"
	}
	return &TaxonomyHandler{path: path, Svc: svc}
}

func (h *TaxonomyHandler) Register(r chi.Router) {
	r.Get("/taxonomy/roles", h.ListRoles)
	r.Get("/taxonomy/competencies", h.ListCompetencies)
	r.Post("/taxonomy/normalize", h.Normalize)
}

func (h *TaxonomyHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)
	if h.Svc == nil {
		writeProblem(w, domain.NewProblem(503, "SERVICE_UNAVAILABLE", "taxonomy reference data is unavailable", rid, nil))
		return
	}
	page, err := h.Svc.ListRoles(r.Context(), domain.RoleFilter{
		Q:      r.URL.Query().Get("q"),
		Family: r.URL.Query().Get("family"),
		Limit:  parseIntQuery(r, "limit", 20),
		Offset: parseIntQuery(r, "offset", 0),
	})
	if err != nil {
		h.writeTaxonomyError(w, r, rid, err)
		return
	}
	writeJSON(w, 200, page)
}

func (h *TaxonomyHandler) ListCompetencies(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)
	if h.Svc == nil {
		writeProblem(w, domain.NewProblem(503, "SERVICE_UNAVAILABLE", "taxonomy reference data is unavailable", rid, nil))
		return
	}
	page, err := h.Svc.ListCompetencies(r.Context(), domain.CompetencyFilter{
		RoleID:   r.URL.Query().Get("role_id"),
		Category: r.URL.Query().Get("category"),
		Q:        r.URL.Query().Get("q"),
		Limit:    parseIntQuery(r, "limit", 20),
		Offset:   parseIntQuery(r, "offset", 0),
	})
	if err != nil {
		h.writeTaxonomyError(w, r, rid, err)
		return
	}
	writeJSON(w, 200, page)
}

func (h *TaxonomyHandler) writeTaxonomyError(w http.ResponseWriter, r *http.Request, rid string, err error) {
	if errors.Is(err, service.ErrTaxonomyUnavailable) {
		writeProblem(w, domain.NewProblem(503, "SERVICE_UNAVAILABLE", err.Error(), rid, nil))
		return
	}
	writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
}
func (h *TaxonomyHandler) Normalize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string `json:"text"`
		RoleHint string `json:"role_hint"`
		TopK     int    `json:"top_k"`
	}
	if err := readJSON(r, &in); err != nil {
		writeProblem(w, problem400(r, err))
		return
	}
	if in.TopK < 1 || in.TopK > 20 {
		in.TopK = 5
	}
	data, err := os.ReadFile(h.path)
	if err != nil {
		writeProblem(w, problem502(r, err))
		return
	}
	var tax taxonomyFile
	if err := yaml.Unmarshal(data, &tax); err != nil {
		writeProblem(w, problem502(r, err))
		return
	}
	query := strings.ToLower(strings.TrimSpace(in.Text))
	type scored struct {
		id, name string
		score    float64
	}
	items := make([]scored, 0)
	for _, c := range tax.Competencies {
		score := 0.0
		for _, term := range append([]string{c.ID, c.NameRU}, c.Synonyms...) {
			if strings.Contains(query, strings.ToLower(term)) {
				score = 1
				break
			}
		}
		if score > 0 {
			items = append(items, scored{c.ID, c.NameRU, score})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	if len(items) > in.TopK {
		items = items[:in.TopK]
	}
	candidates := make([]map[string]any, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, map[string]any{"competency_id": item.id, "name_ru": item.name, "score": item.score, "method": "exact_synonym"})
	}
	needs := len(candidates) == 0 || candidates[0]["score"].(float64) < 0.8
	code := "NORMALIZE_TOP1_STRONG"
	if needs {
		code = "NORMALIZE_TOP1_WEAK"
	}
	writeJSON(w, 200, map[string]any{"query": in.Text, "candidates": candidates, "needs_review": needs, "reason_codes": []string{code}})
}
