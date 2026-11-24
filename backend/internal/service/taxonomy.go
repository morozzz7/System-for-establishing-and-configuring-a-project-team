package service

import (
	"context"
	"errors"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
)

// ErrTaxonomyUnavailable marks a taxonomy query made before the reference tables are
// wired (e.g. the API running without DATABASE_URL). The HTTP layer maps it to 503.
var ErrTaxonomyUnavailable = errors.New("taxonomy repository unavailable")

// TaxonomyService reads the seeded role/competency tables and applies contract pagination.
type TaxonomyService struct {
	Repo repository.TaxonomyRepository
}

func NewTaxonomyService(repo repository.TaxonomyRepository) *TaxonomyService {
	return &TaxonomyService{Repo: repo}
}

func normalizeLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func (s *TaxonomyService) ListRoles(ctx context.Context, f domain.RoleFilter) (domain.RolePage, error) {
	if s == nil || s.Repo == nil {
		return domain.RolePage{}, ErrTaxonomyUnavailable
	}
	f.Limit = normalizeLimit(f.Limit)
	f.Offset = normalizeOffset(f.Offset)
	items, total, err := s.Repo.ListRoles(ctx, f)
	if err != nil {
		return domain.RolePage{}, err
	}
	for i := range items {
		if items[i].Synonyms == nil {
			items[i].Synonyms = []string{}
		}
		if items[i].CoreCompetencies == nil {
			items[i].CoreCompetencies = []string{}
		}
		if items[i].TypicalGrades == nil {
			items[i].TypicalGrades = []string{}
		}
	}
	return domain.RolePage{
		Items: items,
		Meta:  domain.PageMeta{Limit: f.Limit, Offset: f.Offset, Total: total},
	}, nil
}

func (s *TaxonomyService) ListCompetencies(ctx context.Context, f domain.CompetencyFilter) (domain.CompetencyPage, error) {
	if s == nil || s.Repo == nil {
		return domain.CompetencyPage{}, ErrTaxonomyUnavailable
	}
	f.Limit = normalizeLimit(f.Limit)
	f.Offset = normalizeOffset(f.Offset)
	items, total, err := s.Repo.ListCompetencies(ctx, f)
	if err != nil {
		return domain.CompetencyPage{}, err
	}
	for i := range items {
		if items[i].Synonyms == nil {
			items[i].Synonyms = []string{}
		}
		if items[i].Roles == nil {
			items[i].Roles = []string{}
		}
	}
	return domain.CompetencyPage{
		Items: items,
		Meta:  domain.PageMeta{Limit: f.Limit, Offset: f.Offset, Total: total},
	}, nil
}
