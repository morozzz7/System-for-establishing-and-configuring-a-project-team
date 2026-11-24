package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/handler"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

func TestTaxonomyEndpointsAgainstPostgres(t *testing.T) {
	env := setupIntTest(t)
	ctx := context.Background()

	_, err := env.pool.Exec(ctx, `
		INSERT INTO role (role_id, name_en, name_ru, family, synonyms, core_competencies, typical_grades)
		VALUES ('backend_engineer', 'Backend Engineer', 'Бэкенд-разработчик', 'engineering',
		        '{backend,бэкенд}', '{lang_go}', '{J,M,S}')
		ON CONFLICT (role_id) DO UPDATE SET name_ru = EXCLUDED.name_ru,
			synonyms = EXCLUDED.synonyms, core_competencies = EXCLUDED.core_competencies,
			typical_grades = EXCLUDED.typical_grades`)
	require.NoError(t, err)
	_, err = env.pool.Exec(ctx, `
		INSERT INTO competency (competency_id, name_en, name_ru, category)
		VALUES ('api_rest', 'REST API', 'REST API', 'architecture')
		ON CONFLICT (competency_id) DO NOTHING`)
	require.NoError(t, err)
	_, err = env.pool.Exec(ctx, `
		INSERT INTO competency_synonym (competency_id, synonym) VALUES ('lang_go', 'golang')
		ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = env.pool.Exec(ctx, `
		INSERT INTO role_competency (role_id, competency_id, is_core)
		VALUES ('backend_engineer', 'lang_go', true), ('backend_engineer', 'api_rest', false)
		ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	r := chi.NewRouter()
	handler.NewTaxonomyHandler(service.NewTaxonomyService(repository.NewPgTaxonomyRepo(env.pool))).Register(r)
	server := httptest.NewServer(r)
	defer server.Close()

	get := func(path string, out any) int {
		t.Helper()
		resp, err := http.Get(server.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
		return resp.StatusCode
	}

	var roles domain.RolePage
	status := get("/taxonomy/roles?family=engineering&limit=1", &roles)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, domain.PageMeta{Limit: 1, Offset: 0, Total: 2}, roles.Meta)
	require.Len(t, roles.Items, 1)
	assert.Equal(t, "backend_engineer", roles.Items[0].RoleID)
	assert.Equal(t, []string{"backend", "бэкенд"}, roles.Items[0].Synonyms)
	assert.Equal(t, []string{"lang_go"}, roles.Items[0].CoreCompetencies)
	assert.Equal(t, []string{"J", "M", "S"}, roles.Items[0].TypicalGrades)

	var searched domain.RolePage
	status = get("/taxonomy/roles?q="+url.QueryEscape("бэкенд"), &searched)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, searched.Items, 1)
	assert.Equal(t, "backend_engineer", searched.Items[0].RoleID)

	var competencies domain.CompetencyPage
	status = get("/taxonomy/competencies?role_id=backend_engineer&category=language", &competencies)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, domain.PageMeta{Limit: 20, Offset: 0, Total: 1}, competencies.Meta)
	require.Len(t, competencies.Items, 1)
	assert.Equal(t, "lang_go", competencies.Items[0].CompetencyID)
	assert.Equal(t, []string{"golang"}, competencies.Items[0].Synonyms)
	assert.Equal(t, []string{"backend_engineer"}, competencies.Items[0].Roles)
	assert.Equal(t, 5, competencies.Items[0].LevelScale)

	var bySynonym domain.CompetencyPage
	status = get("/taxonomy/competencies?q=golang&limit=1&offset=1", &bySynonym)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, domain.PageMeta{Limit: 1, Offset: 1, Total: 1}, bySynonym.Meta)
	assert.Empty(t, bySynonym.Items)
}
