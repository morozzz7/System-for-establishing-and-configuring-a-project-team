package handler

import (
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

type taxonomyFile struct {
	Competencies []struct {
		ID       string   `yaml:"competency_id"`
		NameRU   string   `yaml:"name_ru"`
		Synonyms []string `yaml:"synonyms"`
	} `yaml:"competencies"`
}
type TaxonomyHandler struct{ path string }

func NewTaxonomyHandler() *TaxonomyHandler {
	path := os.Getenv("TAXONOMY_PATH")
	if path == "" {
		path = "contracts/taxonomy.yaml"
	}
	return &TaxonomyHandler{path: path}
}
func (h *TaxonomyHandler) Register(r chi.Router) { r.Post("/taxonomy/normalize", h.Normalize) }
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
