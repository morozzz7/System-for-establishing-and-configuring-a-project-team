package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/domain"
)

// TaxonomyRepository serves the seeded reference tables loaded by cmd/seed from
// contracts/taxonomy.yaml. Family and category are validated by DB CHECK constraints.
type TaxonomyRepository interface {
	ListRoles(ctx context.Context, f domain.RoleFilter) ([]domain.Role, int, error)
	ListCompetencies(ctx context.Context, f domain.CompetencyFilter) ([]domain.Competency, int, error)
}

type PgTaxonomyRepo struct{ Pool *pgxpool.Pool }

func NewPgTaxonomyRepo(pool *pgxpool.Pool) *PgTaxonomyRepo {
	return &PgTaxonomyRepo{Pool: pool}
}

// likePattern builds a case-folded LIKE pattern and escapes the query's own wildcards.
func likePattern(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return ""
	}
	q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	return "%" + q + "%"
}

func roleFilterClause(f domain.RoleFilter) (string, []any) {
	pattern := likePattern(f.Q)
	conditions := []string{"TRUE"}
	var args []any
	if f.Family != "" {
		args = append(args, f.Family)
		conditions = append(conditions, fmt.Sprintf("family = $%d", len(args)))
	}
	if pattern != "" {
		args = append(args, pattern)
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(
			`(lower(role_id) LIKE $%d OR lower(name_en) LIKE $%d OR lower(name_ru) LIKE $%d
			  OR EXISTS (SELECT 1 FROM unnest(COALESCE(synonyms, '{}')) AS syn WHERE lower(syn) LIKE $%d))`,
			n, n, n, n))
	}
	return strings.Join(conditions, " AND "), args
}

func (r *PgTaxonomyRepo) ListRoles(ctx context.Context, f domain.RoleFilter) ([]domain.Role, int, error) {
	where, args := roleFilterClause(f)
	var total int
	if err := r.Pool.QueryRow(ctx, "SELECT count(*) FROM role WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count roles: %w", err)
	}

	limitIdx, offsetIdx := len(args)+1, len(args)+2
	query := fmt.Sprintf(`SELECT role_id, name_en, name_ru, family,
			COALESCE(synonyms, '{}'), COALESCE(core_competencies, '{}'), COALESCE(typical_grades, '{}')
		 FROM role WHERE %s ORDER BY role_id LIMIT $%d OFFSET $%d`, where, limitIdx, offsetIdx)
	rows, err := r.Pool.Query(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Role, 0)
	for rows.Next() {
		var item domain.Role
		if err := rows.Scan(&item.RoleID, &item.NameEN, &item.NameRU, &item.Family,
			&item.Synonyms, &item.CoreCompetencies, &item.TypicalGrades); err != nil {
			return nil, 0, fmt.Errorf("scan role: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate roles: %w", err)
	}
	return items, total, nil
}

func competencyFilterClause(f domain.CompetencyFilter) (string, []any) {
	pattern := likePattern(f.Q)
	conditions := []string{"TRUE"}
	var args []any
	if f.Category != "" {
		args = append(args, f.Category)
		conditions = append(conditions, fmt.Sprintf("c.category = $%d", len(args)))
	}
	if f.RoleID != "" {
		args = append(args, f.RoleID)
		conditions = append(conditions, fmt.Sprintf(
			`EXISTS (SELECT 1 FROM role_competency rc WHERE rc.competency_id = c.competency_id AND rc.role_id = $%d)`,
			len(args)))
	}
	if pattern != "" {
		args = append(args, pattern)
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(
			`(lower(c.competency_id) LIKE $%d OR lower(c.name_en) LIKE $%d OR lower(c.name_ru) LIKE $%d
			  OR EXISTS (SELECT 1 FROM competency_synonym syn
			             WHERE syn.competency_id = c.competency_id AND lower(syn.synonym) LIKE $%d))`,
			n, n, n, n))
	}
	return strings.Join(conditions, " AND "), args
}

func (r *PgTaxonomyRepo) ListCompetencies(ctx context.Context, f domain.CompetencyFilter) ([]domain.Competency, int, error) {
	where, args := competencyFilterClause(f)
	var total int
	if err := r.Pool.QueryRow(ctx, "SELECT count(*) FROM competency c WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count competencies: %w", err)
	}

	limitIdx, offsetIdx := len(args)+1, len(args)+2
	query := fmt.Sprintf(`SELECT c.competency_id, c.name_en, c.name_ru, c.category, c.level_scale,
			COALESCE(s.synonyms, '{}'), COALESCE(rc.roles, '{}')
		 FROM competency c
		 LEFT JOIN LATERAL (
			SELECT array_agg(synonym ORDER BY synonym) AS synonyms
			FROM competency_synonym WHERE competency_id = c.competency_id
		 ) s ON true
		 LEFT JOIN LATERAL (
			SELECT array_agg(role_id ORDER BY role_id) AS roles
			FROM role_competency WHERE competency_id = c.competency_id
		 ) rc ON true
		 WHERE %s ORDER BY c.competency_id LIMIT $%d OFFSET $%d`, where, limitIdx, offsetIdx)
	rows, err := r.Pool.Query(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list competencies: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Competency, 0)
	for rows.Next() {
		var item domain.Competency
		if err := rows.Scan(&item.CompetencyID, &item.NameEN, &item.NameRU, &item.Category,
			&item.LevelScale, &item.Synonyms, &item.Roles); err != nil {
			return nil, 0, fmt.Errorf("scan competency: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate competencies: %w", err)
	}
	return items, total, nil
}
