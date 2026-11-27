package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/domain"
)

type assignmentTransactionKey struct{}

type assignmentDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assignmentConnection(ctx context.Context, pool *pgxpool.Pool) assignmentDB {
	if tx, ok := ctx.Value(assignmentTransactionKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

func (r *PgAssignmentRepo) WithTransaction(ctx context.Context, work func(context.Context) error) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := work(context.WithValue(ctx, assignmentTransactionKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgAssignmentRepo) LockWeeks(ctx context.Context, rows []domain.AssignmentCreate) error {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.EmployeeID+"/"+row.Week)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if i > 0 && keys[i-1] == key {
			continue
		}
		if _, err := assignmentConnection(ctx, r.Pool).Exec(ctx,
			"SELECT pg_advisory_xact_lock(hashtextextended($1, 47))", key); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgAssignmentRepo) PrepareApprovedPlan(ctx context.Context, projectID, runID string) ([]domain.AssignmentCreate, error) {
	conn := assignmentConnection(ctx, r.Pool)
	var lockedProject string
	if err := conn.QueryRow(ctx, "SELECT project_id FROM project WHERE project_id=$1 FOR NO KEY UPDATE", projectID).Scan(&lockedProject); err != nil {
		return nil, err
	}
	var latestRun, publicVariantID string
	err := conn.QueryRow(ctx, `
  WITH brief_key AS (
   SELECT right(regexp_replace(regexp_replace(lower(brief_id), '^brf_?', ''), '[^a-z0-9]', '', 'g'), 12) AS suffix,
          brief_id
   FROM team_run WHERE run_id=$1
  )
  SELECT d.run_id, d.variant_id
  FROM brief_key b JOIN team_run r USING (brief_id) JOIN decision d ON d.run_id=r.run_id
  WHERE $2 = 'prj_' || lpad(b.suffix, greatest(4, length(b.suffix)), '0')
  ORDER BY d.decided_at DESC, d.run_id DESC LIMIT 1`, runID, projectID).Scan(&latestRun, &publicVariantID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && latestRun != runID) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	var variantID string
	err = conn.QueryRow(ctx, `
  SELECT variant_id FROM team_variant
  WHERE run_id = $1 AND (payload_jsonb->>'variant_id' = $2 OR variant_id = $2)
  ORDER BY (payload_jsonb->>'variant_id' = $2) DESC NULLS LAST LIMIT 1`, runID, publicVariantID).Scan(&variantID)
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(ctx, `
  SELECT a.employee_id, m.role_id, a.week, sum(a.hours_minutes)::double precision / 60
  FROM variant_weekly_assignment a JOIN variant_member m USING (variant_id, employee_id)
  WHERE a.variant_id=$1 AND a.hours_minutes > 0
  GROUP BY a.employee_id, m.role_id, a.week
  ORDER BY a.employee_id, a.week`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assignments []domain.AssignmentCreate
	for rows.Next() {
		var assignment domain.AssignmentCreate
		if err := rows.Scan(&assignment.EmployeeID, &assignment.RoleID, &assignment.Week, &assignment.Hours); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

func (r *PgAssignmentRepo) ReplaceApprovedPlanStart(ctx context.Context, projectID string, next []domain.AssignmentCreate) ([]domain.Assignment, error) {
	conn := assignmentConnection(ctx, r.Pool)
	rows, err := conn.Query(ctx, `
  SELECT assignment_id, project_id, employee_id, role_id, week, hours_minutes, source
  FROM assignment WHERE project_id=$1 AND approval_managed ORDER BY employee_id, week`, projectID)
	if err != nil {
		return nil, err
	}
	existing, err := scanAssignments(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if matchesApprovedPlan(existing, next) {
		return existing, nil
	}

	keys := append([]domain.AssignmentCreate(nil), next...)
	for _, row := range existing {
		keys = append(keys, domain.AssignmentCreate{EmployeeID: row.EmployeeID, Week: row.Week})
	}
	if err := r.LockWeeks(ctx, keys); err != nil {
		return nil, err
	}
	_, err = conn.Exec(ctx, "DELETE FROM assignment WHERE project_id=$1 AND approval_managed", projectID)
	return nil, err
}

func matchesApprovedPlan(existing []domain.Assignment, next []domain.AssignmentCreate) bool {
	if len(existing) != len(next) {
		return false
	}
	for i, previous := range existing {
		row := next[i]
		if previous.EmployeeID != row.EmployeeID || previous.Week != row.Week ||
			previous.RoleID != row.RoleID || assignmentMinutes(previous.Hours) != assignmentMinutes(row.Hours) {
			return false
		}
	}
	return true
}

func (r *PgAssignmentRepo) CreateApproved(ctx context.Context, a domain.Assignment) error {
	_, err := assignmentConnection(ctx, r.Pool).Exec(ctx, `
  INSERT INTO assignment (assignment_id, project_id, employee_id, role_id, week, hours_minutes, source, approval_managed)
  VALUES ($1,$2,$3,$4,$5,$6,'plan',true)`, a.AssignmentID, a.ProjectID, a.EmployeeID, a.RoleID, a.Week, assignmentMinutes(a.Hours))
	if IsDuplicate(err) {
		return ErrConflict
	}
	return err
}
