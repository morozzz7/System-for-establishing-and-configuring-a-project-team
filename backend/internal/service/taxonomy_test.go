package service

import (
	"context"
	"errors"
	"testing"

	"github.com/teamopt/backend/internal/domain"
)

type stubTaxonomyRepo struct {
	roles        []domain.Role
	competencies []domain.Competency
	total        int
	roleFilter   domain.RoleFilter
	compFilter   domain.CompetencyFilter
}

func (s *stubTaxonomyRepo) ListRoles(_ context.Context, f domain.RoleFilter) ([]domain.Role, int, error) {
	s.roleFilter = f
	return s.roles, s.total, nil
}

func (s *stubTaxonomyRepo) ListCompetencies(_ context.Context, f domain.CompetencyFilter) ([]domain.Competency, int, error) {
	s.compFilter = f
	return s.competencies, s.total, nil
}

func TestTaxonomyServiceListRolesPaginatesAndFillsEmptyArrays(t *testing.T) {
	repo := &stubTaxonomyRepo{
		roles: []domain.Role{{
			RoleID: "backend_engineer", NameEN: "Backend Engineer", NameRU: "Бэкенд-разработчик",
			Family: "engineering",
		}},
		total: 12,
	}
	svc := NewTaxonomyService(repo)
	page, err := svc.ListRoles(context.Background(), domain.RoleFilter{
		Q: "backend", Family: "engineering", Limit: 500, Offset: -3,
	})
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if page.Meta != (domain.PageMeta{Limit: 200, Offset: 0, Total: 12}) {
		t.Fatalf("meta = %+v, want clamped 200/0/12", page.Meta)
	}
	if repo.roleFilter.Q != "backend" || repo.roleFilter.Family != "engineering" {
		t.Fatalf("filters not forwarded: %+v", repo.roleFilter)
	}
	item := page.Items[0]
	if item.Synonyms == nil || item.CoreCompetencies == nil || item.TypicalGrades == nil {
		t.Fatalf("empty arrays must be serialized as []: %+v", item)
	}
}

func TestTaxonomyServiceListCompetenciesPaginatesAndFillsEmptyArrays(t *testing.T) {
	repo := &stubTaxonomyRepo{
		competencies: []domain.Competency{{
			CompetencyID: "lang_go", NameEN: "Go", NameRU: "Go", Category: "language", LevelScale: 5,
		}},
		total: 101,
	}
	svc := NewTaxonomyService(repo)
	page, err := svc.ListCompetencies(context.Background(), domain.CompetencyFilter{
		RoleID: "backend_engineer", Category: "language", Q: "golang", Limit: 0, Offset: 5,
	})
	if err != nil {
		t.Fatalf("list competencies: %v", err)
	}
	if page.Meta != (domain.PageMeta{Limit: 20, Offset: 5, Total: 101}) {
		t.Fatalf("meta = %+v, want default 20/5/101", page.Meta)
	}
	if repo.compFilter.RoleID != "backend_engineer" || repo.compFilter.Category != "language" || repo.compFilter.Q != "golang" {
		t.Fatalf("filters not forwarded: %+v", repo.compFilter)
	}
	if page.Items[0].Synonyms == nil || page.Items[0].Roles == nil {
		t.Fatalf("empty arrays must be serialized as []: %+v", page.Items[0])
	}
}

func TestTaxonomyServiceRequiresRepository(t *testing.T) {
	svc := NewTaxonomyService(nil)
	if _, err := svc.ListRoles(context.Background(), domain.RoleFilter{}); !errors.Is(err, ErrTaxonomyUnavailable) {
		t.Fatalf("list roles error = %v, want ErrTaxonomyUnavailable", err)
	}
	if _, err := svc.ListCompetencies(context.Background(), domain.CompetencyFilter{}); !errors.Is(err, ErrTaxonomyUnavailable) {
		t.Fatalf("list competencies error = %v, want ErrTaxonomyUnavailable", err)
	}
}
