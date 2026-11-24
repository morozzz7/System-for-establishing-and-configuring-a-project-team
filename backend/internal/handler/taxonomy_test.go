package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/service"
)

type fakeTaxonomyRepo struct {
	roleFilter domain.RoleFilter
	compFilter domain.CompetencyFilter
}

func (f *fakeTaxonomyRepo) ListRoles(_ context.Context, filter domain.RoleFilter) ([]domain.Role, int, error) {
	f.roleFilter = filter
	return []domain.Role{{
		RoleID: "backend_engineer", NameEN: "Backend Engineer", NameRU: "Бэкенд-разработчик",
		Family: "engineering", Synonyms: []string{"backend"},
		CoreCompetencies: []string{"lang_go"}, TypicalGrades: []string{"J", "M", "S"},
	}}, 12, nil
}

func (f *fakeTaxonomyRepo) ListCompetencies(_ context.Context, filter domain.CompetencyFilter) ([]domain.Competency, int, error) {
	f.compFilter = filter
	return []domain.Competency{{
		CompetencyID: "lang_go", NameEN: "Go", NameRU: "Go", Category: "language",
		Synonyms: []string{"golang"}, Roles: []string{"backend_engineer"}, LevelScale: 5,
	}}, 101, nil
}

func taxonomyTestRouter(svc *service.TaxonomyService) chi.Router {
	r := chi.NewRouter()
	(&TaxonomyHandler{Svc: svc}).Register(r)
	return r
}

func decodeStrict(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

func TestTaxonomyListRolesContractShape(t *testing.T) {
	repo := &fakeTaxonomyRepo{}
	rec := httptest.NewRecorder()
	taxonomyTestRouter(service.NewTaxonomyService(repo)).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/taxonomy/roles?q=backend&family=engineering&limit=1&offset=2", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw) != 2 || raw["items"] == nil || raw["meta"] == nil {
		t.Fatalf("unexpected role page keys: %v", raw)
	}
	var page domain.RolePage
	decodeStrict(t, rec, &page)
	if page.Meta != (domain.PageMeta{Limit: 1, Offset: 2, Total: 12}) {
		t.Fatalf("meta = %+v, want 1/2/12", page.Meta)
	}
	if len(page.Items) != 1 || page.Items[0].RoleID != "backend_engineer" {
		t.Fatalf("items = %+v", page.Items)
	}
	if !reflect.DeepEqual(repo.roleFilter, domain.RoleFilter{
		Q: "backend", Family: "engineering", Limit: 1, Offset: 2,
	}) {
		t.Fatalf("role filter = %+v", repo.roleFilter)
	}
}

func TestTaxonomyListCompetenciesContractShape(t *testing.T) {
	repo := &fakeTaxonomyRepo{}
	rec := httptest.NewRecorder()
	taxonomyTestRouter(service.NewTaxonomyService(repo)).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet,
			"/taxonomy/competencies?role_id=backend_engineer&category=language&q=golang&limit=5", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var page domain.CompetencyPage
	decodeStrict(t, rec, &page)
	if page.Meta != (domain.PageMeta{Limit: 5, Offset: 0, Total: 101}) {
		t.Fatalf("meta = %+v, want 5/0/101", page.Meta)
	}
	if len(page.Items) != 1 || page.Items[0].CompetencyID != "lang_go" || page.Items[0].LevelScale != 5 {
		t.Fatalf("items = %+v", page.Items)
	}
	if !reflect.DeepEqual(repo.compFilter, domain.CompetencyFilter{
		RoleID: "backend_engineer", Category: "language", Q: "golang", Limit: 5, Offset: 0,
	}) {
		t.Fatalf("competency filter = %+v", repo.compFilter)
	}
}

func TestTaxonomyListsUnavailableWithoutDatabase(t *testing.T) {
	for _, path := range []string{"/taxonomy/roles", "/taxonomy/competencies"} {
		rec := httptest.NewRecorder()
		taxonomyTestRouter(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503: %s", path, rec.Code, rec.Body.String())
		}
	}
}
