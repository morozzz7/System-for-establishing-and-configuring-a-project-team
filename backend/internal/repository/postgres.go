// Package repository defines interfaces and PostgreSQL implementations
// for all CRUD operations. All methods accept context.Context.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/domain"
)

// Sentinel errors for the service layer.
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrG1Unavailable = errors.New("G1: employee unavailable in week")
	ErrG2Overload    = errors.New("G2: assignment would exceed capacity")
)

// IsDuplicate checks if a pgx error is a unique violation (SQLSTATE 23505).
func IsDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// IsFKViolation checks if a pgx error is a foreign-key violation (SQLSTATE 23503).
func IsFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// ---------- Employee Repository ----------

// EmployeeRepo defines employee CRUD operations.
type EmployeeRepo interface {
	List(ctx context.Context, f domain.EmployeeFilter) ([]domain.Employee, int, error)
	Get(ctx context.Context, id string) (domain.Employee, error)
	Create(ctx context.Context, e domain.Employee) error
	Update(ctx context.Context, e domain.Employee) error
	Exists(ctx context.Context, id string) (bool, error)
}

// EmployeeCompetencyRepo defines competency CRUD operations.
type EmployeeCompetencyRepo interface {
	ListByEmployee(ctx context.Context, employeeID string) ([]domain.EmployeeCompetency, error)
	UpsertSet(ctx context.Context, employeeID string, comps []domain.EmployeeCompetency) error
}

// ---------- Project Repository ----------

// ProjectRepo defines project CRUD operations.
type ProjectRepo interface {
	List(ctx context.Context, f domain.ProjectFilter) ([]domain.Project, int, error)
	Get(ctx context.Context, id string) (domain.Project, error)
	Create(ctx context.Context, p domain.Project) error
	Update(ctx context.Context, p domain.Project) error
}

// ---------- Assignment Repository ----------

// AssignmentRepo defines assignment CRUD operations.
type AssignmentRepo interface {
	ListByProject(ctx context.Context, projectID string, fromWeek, toWeek string) ([]domain.Assignment, error)
	ListByFilter(ctx context.Context, f domain.AssignmentFilter) ([]domain.Assignment, error)
	Create(ctx context.Context, a domain.Assignment) error
	// TotalAssignedHours returns the total assigned hours for an employee in a given week
	// across all projects (for G2 check).
	TotalAssignedHours(ctx context.Context, employeeID, week string) (float64, error)
}

// ---------- Absence Repository ----------

// AbsenceRepo defines absence CRUD operations.
type AbsenceRepo interface {
	List(ctx context.Context, f domain.AbsenceFilter) ([]domain.Absence, int, error)
	Get(ctx context.Context, id string) (domain.Absence, error)
	Create(ctx context.Context, a domain.Absence) error
	SoftDelete(ctx context.Context, id string) error
	// HasOverlap checks whether the employee has any active absence that overlaps [start, end].
	HasOverlap(ctx context.Context, employeeID string, start, end time.Time) (bool, error)
	// IsAbsentInWeek checks whether the employee has any approved absence covering the given ISO week.
	IsAbsentInWeek(ctx context.Context, employeeID, week string) (bool, error)
}

// ---------- Audit Repository ----------

// AuditRepo defines audit log operations.
type AuditRepo interface {
	Create(ctx context.Context, e domain.AuditEntry) error
}

// ---------- PgEmployeeRepo ----------

type PgEmployeeRepo struct{ Pool *pgxpool.Pool }

func NewPgEmployeeRepo(pool *pgxpool.Pool) *PgEmployeeRepo {
	return &PgEmployeeRepo{Pool: pool}
}

func (r *PgEmployeeRepo) List(ctx context.Context, f domain.EmployeeFilter) ([]domain.Employee, int, error) {
	var conditions []string
	var args []interface{}
	argN := 0

	if f.RoleID != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("role_profile_id = $%d", argN))
		args = append(args, f.RoleID)
	}
	if f.CompetencyID != "" {
		argN++
		sub := fmt.Sprintf(
			"employee_id IN (SELECT employee_id FROM employee_competency WHERE competency_id = $%d",
			argN,
		)
		args = append(args, f.CompetencyID)
		if f.MinLevel > 0 {
			argN++
			sub += fmt.Sprintf(" AND level >= $%d", argN)
			args = append(args, f.MinLevel)
		}
		sub += ")"
		conditions = append(conditions, sub)
	} else if f.MinLevel > 0 {
		argN++
		conditions = append(conditions,
			fmt.Sprintf("employee_id IN (SELECT employee_id FROM employee_competency WHERE level >= $%d)", argN))
		args = append(args, f.MinLevel)
	}
	if f.BenchOnly {
		conditions = append(conditions, "bench_flag = true")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count
	countSQL := fmt.Sprintf("SELECT count(*) FROM employee %s", where)
	var total int
	if err := r.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count employees: %w", err)
	}

	// Select with pagination
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	argN++
	limitIdx := argN
	args = append(args, limit)
	argN++
	offsetIdx := argN
	args = append(args, offset)

	selectSQL := fmt.Sprintf(
		`SELECT employee_id, grade, role_profile_id, unit_id, employment_type, bench_flag,
		        weekly_hours_norm, reserve_ratio, max_utilization_ratio, cost_coefficient, active
		 FROM employee %s ORDER BY employee_id LIMIT $%d OFFSET $%d`,
		where, limitIdx, offsetIdx,
	)

	rows, err := r.Pool.Query(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list employees: %w", err)
	}
	defer rows.Close()

	var employees []domain.Employee
	for rows.Next() {
		var e domain.Employee
		if err := rows.Scan(
			&e.EmployeeID, &e.Grade, &e.RoleProfileID, &e.UnitID, &e.EmploymentType, &e.BenchFlag,
			&e.CapacityProfile.WeeklyHoursNorm, &e.CapacityProfile.ReserveRatio,
			&e.CapacityProfile.MaxUtilizationRatio, &e.CostCoefficient, &e.Active,
		); err != nil {
			return nil, 0, fmt.Errorf("scan employee: %w", err)
		}
		employees = append(employees, e)
	}
	if employees == nil {
		employees = []domain.Employee{}
	}
	return employees, total, nil
}

func (r *PgEmployeeRepo) Get(ctx context.Context, id string) (domain.Employee, error) {
	var e domain.Employee
	err := r.Pool.QueryRow(ctx,
		`SELECT employee_id, grade, role_profile_id, unit_id, employment_type, bench_flag,
		        weekly_hours_norm, reserve_ratio, max_utilization_ratio, cost_coefficient, active
		 FROM employee WHERE employee_id = $1`, id,
	).Scan(
		&e.EmployeeID, &e.Grade, &e.RoleProfileID, &e.UnitID, &e.EmploymentType, &e.BenchFlag,
		&e.CapacityProfile.WeeklyHoursNorm, &e.CapacityProfile.ReserveRatio,
		&e.CapacityProfile.MaxUtilizationRatio, &e.CostCoefficient, &e.Active,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Employee{}, fmt.Errorf("%w: employee %s", ErrNotFound, id)
		}
		return domain.Employee{}, fmt.Errorf("get employee: %w", err)
	}
	return e, nil
}

func (r *PgEmployeeRepo) Create(ctx context.Context, e domain.Employee) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO employee (employee_id, grade, role_profile_id, unit_id, employment_type,
		   bench_flag, weekly_hours_norm, reserve_ratio, max_utilization_ratio, cost_coefficient, active)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.EmployeeID, e.Grade, e.RoleProfileID, e.UnitID, e.EmploymentType,
		e.BenchFlag, int(e.CapacityProfile.WeeklyHoursNorm), e.CapacityProfile.ReserveRatio,
		e.CapacityProfile.MaxUtilizationRatio, e.CostCoefficient, e.Active,
	)
	if err != nil {
		if IsDuplicate(err) {
			return fmt.Errorf("%w: employee %s", ErrConflict, e.EmployeeID)
		}
		if IsFKViolation(err) {
			return fmt.Errorf("foreign key violation: %w", err)
		}
		return fmt.Errorf("create employee: %w", err)
	}
	return nil
}

func (r *PgEmployeeRepo) Update(ctx context.Context, e domain.Employee) error {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE employee SET grade=$2, role_profile_id=$3, unit_id=$4, employment_type=$5,
		   bench_flag=$6, weekly_hours_norm=$7, reserve_ratio=$8, max_utilization_ratio=$9,
		   cost_coefficient=$10, active=$11, updated_at=now()
		 WHERE employee_id=$1`,
		e.EmployeeID, e.Grade, e.RoleProfileID, e.UnitID, e.EmploymentType,
		e.BenchFlag, int(e.CapacityProfile.WeeklyHoursNorm), e.CapacityProfile.ReserveRatio,
		e.CapacityProfile.MaxUtilizationRatio, e.CostCoefficient, e.Active,
	)
	if err != nil {
		return fmt.Errorf("update employee: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: employee %s", ErrNotFound, e.EmployeeID)
	}
	return nil
}

func (r *PgEmployeeRepo) Exists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employee WHERE employee_id=$1)", id).Scan(&exists)
	return exists, err
}

// ---------- PgEmployeeCompetencyRepo ----------

type PgEmployeeCompetencyRepo struct{ Pool *pgxpool.Pool }

func NewPgEmployeeCompetencyRepo(pool *pgxpool.Pool) *PgEmployeeCompetencyRepo {
	return &PgEmployeeCompetencyRepo{Pool: pool}
}

func (r *PgEmployeeCompetencyRepo) ListByEmployee(ctx context.Context, employeeID string) ([]domain.EmployeeCompetency, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT competency_id, level, confidence, last_used, source
		 FROM employee_competency WHERE employee_id = $1 ORDER BY competency_id`, employeeID)
	if err != nil {
		return nil, fmt.Errorf("list competencies: %w", err)
	}
	defer rows.Close()

	var comps []domain.EmployeeCompetency
	for rows.Next() {
		var c domain.EmployeeCompetency
		if err := rows.Scan(&c.CompetencyID, &c.Level, &c.Confidence, &c.LastUsed, &c.Source); err != nil {
			return nil, fmt.Errorf("scan competency: %w", err)
		}
		comps = append(comps, c)
	}
	if comps == nil {
		comps = []domain.EmployeeCompetency{}
	}
	return comps, nil
}

func (r *PgEmployeeCompetencyRepo) UpsertSet(ctx context.Context, employeeID string, comps []domain.EmployeeCompetency) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Delete existing competencies for this employee.
	if _, err := tx.Exec(ctx, "DELETE FROM employee_competency WHERE employee_id = $1", employeeID); err != nil {
		return fmt.Errorf("delete old competencies: %w", err)
	}

	// Insert new ones.
	for _, c := range comps {
		_, err := tx.Exec(ctx,
			`INSERT INTO employee_competency (employee_id, competency_id, level, confidence, last_used, source)
			 VALUES ($1,$2,$3,$4,$5,$6)`,
			employeeID, c.CompetencyID, c.Level, c.Confidence, c.LastUsed, c.Source,
		)
		if err != nil {
			if IsFKViolation(err) {
				return fmt.Errorf("competency %s not found in taxonomy: %w", c.CompetencyID, err)
			}
			return fmt.Errorf("insert competency: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// ---------- PgProjectRepo ----------

type PgProjectRepo struct{ Pool *pgxpool.Pool }

func NewPgProjectRepo(pool *pgxpool.Pool) *PgProjectRepo {
	return &PgProjectRepo{Pool: pool}
}

func (r *PgProjectRepo) List(ctx context.Context, f domain.ProjectFilter) ([]domain.Project, int, error) {
	var where string
	var args []interface{}
	if f.Status != "" {
		where = "WHERE status = $1"
		args = append(args, f.Status)
	}

	var total int
	countSQL := "SELECT count(*) FROM project " + where
	if err := r.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count projects: %w", err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	argN := len(args)
	argN++
	limitIdx := argN
	args = append(args, limit)
	argN++
	offsetIdx := argN
	args = append(args, offset)

	selectSQL := fmt.Sprintf(
		`SELECT project_id, code, name, status, priority, criticality, start_week,
		        end_week_planned, COALESCE(end_week_forecast,''), replaces_other_projects
		 FROM project %s ORDER BY project_id LIMIT $%d OFFSET $%d`,
		where, limitIdx, offsetIdx,
	)
	rows, err := r.Pool.Query(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ProjectID, &p.Code, &p.Name, &p.Status, &p.Priority,
			&p.Criticality, &p.StartWeek, &p.EndWeekPlanned, &p.EndWeekForecast,
			&p.ReplacesOtherProjects,
		); err != nil {
			return nil, 0, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []domain.Project{}
	}
	return projects, total, nil
}

func (r *PgProjectRepo) Get(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	err := r.Pool.QueryRow(ctx,
		`SELECT project_id, code, name, status, priority, criticality, start_week,
		        end_week_planned, COALESCE(end_week_forecast,''), replaces_other_projects
		 FROM project WHERE project_id = $1`, id,
	).Scan(&p.ProjectID, &p.Code, &p.Name, &p.Status, &p.Priority,
		&p.Criticality, &p.StartWeek, &p.EndWeekPlanned, &p.EndWeekForecast,
		&p.ReplacesOtherProjects,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, fmt.Errorf("%w: project %s", ErrNotFound, id)
		}
		return domain.Project{}, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

func (r *PgProjectRepo) Create(ctx context.Context, p domain.Project) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO project (project_id, code, name, status, priority, criticality,
		   start_week, end_week_planned, end_week_forecast, replaces_other_projects)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10)`,
		p.ProjectID, p.Code, p.Name, p.Status, p.Priority, p.Criticality,
		p.StartWeek, p.EndWeekPlanned, p.EndWeekForecast, p.ReplacesOtherProjects,
	)
	if err != nil {
		if IsDuplicate(err) {
			return fmt.Errorf("%w: project %s", ErrConflict, p.ProjectID)
		}
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func (r *PgProjectRepo) Update(ctx context.Context, p domain.Project) error {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE project SET code=$2, name=$3, status=$4, priority=$5, criticality=$6,
		   start_week=$7, end_week_planned=$8, end_week_forecast=NULLIF($9,''),
		   replaces_other_projects=$10, updated_at=now()
		 WHERE project_id=$1`,
		p.ProjectID, p.Code, p.Name, p.Status, p.Priority, p.Criticality,
		p.StartWeek, p.EndWeekPlanned, p.EndWeekForecast, p.ReplacesOtherProjects,
	)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: project %s", ErrNotFound, p.ProjectID)
	}
	return nil
}

// ---------- PgAssignmentRepo ----------

type PgAssignmentRepo struct{ Pool *pgxpool.Pool }

func NewPgAssignmentRepo(pool *pgxpool.Pool) *PgAssignmentRepo {
	return &PgAssignmentRepo{Pool: pool}
}

func (r *PgAssignmentRepo) ListByProject(ctx context.Context, projectID string, fromWeek, toWeek string) ([]domain.Assignment, error) {
	var conditions = []string{"project_id = $1"}
	args := []interface{}{projectID}
	argN := 1

	if fromWeek != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("week >= $%d", argN))
		args = append(args, fromWeek)
	}
	if toWeek != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("week <= $%d", argN))
		args = append(args, toWeek)
	}

	sql := fmt.Sprintf(
		`SELECT assignment_id, project_id, employee_id, role_id, week, hours_minutes, source
		 FROM assignment WHERE %s ORDER BY week, employee_id`,
		strings.Join(conditions, " AND "),
	)
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	return scanAssignments(rows)
}

func (r *PgAssignmentRepo) ListByFilter(ctx context.Context, f domain.AssignmentFilter) ([]domain.Assignment, error) {
	var conditions []string
	var args []interface{}
	argN := 0

	if f.ProjectID != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argN))
		args = append(args, f.ProjectID)
	}
	if f.EmployeeID != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argN))
		args = append(args, f.EmployeeID)
	}
	if f.FromWeek != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("week >= $%d", argN))
		args = append(args, f.FromWeek)
	}
	if f.ToWeek != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("week <= $%d", argN))
		args = append(args, f.ToWeek)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	sql := fmt.Sprintf(
		`SELECT assignment_id, project_id, employee_id, role_id, week, hours_minutes, source
		 FROM assignment %s ORDER BY week, employee_id`, where,
	)
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	return scanAssignments(rows)
}

func scanAssignments(rows pgx.Rows) ([]domain.Assignment, error) {
	var assignments []domain.Assignment
	for rows.Next() {
		var a domain.Assignment
		var hoursMin int
		if err := rows.Scan(&a.AssignmentID, &a.ProjectID, &a.EmployeeID, &a.RoleID,
			&a.Week, &hoursMin, &a.Source); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		a.Hours = float64(hoursMin) / 60.0
		assignments = append(assignments, a)
	}
	if assignments == nil {
		assignments = []domain.Assignment{}
	}
	return assignments, nil
}

func (r *PgAssignmentRepo) Create(ctx context.Context, a domain.Assignment) error {
	hoursMin := int(a.Hours * 60)
	if hoursMin < 1 {
		hoursMin = 1
	}
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO assignment (assignment_id, project_id, employee_id, role_id, week, hours_minutes, source)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		a.AssignmentID, a.ProjectID, a.EmployeeID, a.RoleID, a.Week, hoursMin, a.Source,
	)
	if err != nil {
		if IsDuplicate(err) {
			return fmt.Errorf("%w: duplicate assignment for employee %s, project %s, week %s",
				ErrConflict, a.EmployeeID, a.ProjectID, a.Week)
		}
		if IsFKViolation(err) {
			return fmt.Errorf("foreign key violation: %w", err)
		}
		return fmt.Errorf("create assignment: %w", err)
	}
	return nil
}

func (r *PgAssignmentRepo) TotalAssignedHours(ctx context.Context, employeeID, week string) (float64, error) {
	var totalMin int
	err := r.Pool.QueryRow(ctx,
		`SELECT COALESCE(sum(hours_minutes), 0) FROM assignment
		 WHERE employee_id = $1 AND week = $2`, employeeID, week,
	).Scan(&totalMin)
	if err != nil {
		return 0, fmt.Errorf("total assigned hours: %w", err)
	}
	return float64(totalMin) / 60.0, nil
}

// ---------- PgAbsenceRepo ----------

type PgAbsenceRepo struct{ Pool *pgxpool.Pool }

func NewPgAbsenceRepo(pool *pgxpool.Pool) *PgAbsenceRepo {
	return &PgAbsenceRepo{Pool: pool}
}

func (r *PgAbsenceRepo) List(ctx context.Context, f domain.AbsenceFilter) ([]domain.Absence, int, error) {
	var conditions []string
	var args []interface{}
	argN := 0

	conditions = append(conditions, "deleted_at IS NULL")

	if f.EmployeeID != "" {
		argN++
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argN))
		args = append(args, f.EmployeeID)
	}
	if f.From != nil {
		argN++
		conditions = append(conditions, fmt.Sprintf("end_date >= $%d", argN))
		args = append(args, *f.From)
	}
	if f.To != nil {
		argN++
		conditions = append(conditions, fmt.Sprintf("start_date <= $%d", argN))
		args = append(args, *f.To)
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	countSQL := "SELECT count(*) FROM absence " + where
	if err := r.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count absences: %w", err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	argN++
	limitIdx := argN
	args = append(args, limit)
	argN++
	offsetIdx := argN
	args = append(args, offset)

	selectSQL := fmt.Sprintf(
		`SELECT absence_id, employee_id, type, start_date, end_date, hours_per_day, approved, source
		 FROM absence %s ORDER BY start_date LIMIT $%d OFFSET $%d`,
		where, limitIdx, offsetIdx,
	)
	rows, err := r.Pool.Query(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list absences: %w", err)
	}
	defer rows.Close()

	var absences []domain.Absence
	for rows.Next() {
		var a domain.Absence
		if err := rows.Scan(&a.AbsenceID, &a.EmployeeID, &a.Type, &a.StartDate, &a.EndDate,
			&a.HoursPerDay, &a.Approved, &a.Source); err != nil {
			return nil, 0, fmt.Errorf("scan absence: %w", err)
		}
		absences = append(absences, a)
	}
	if absences == nil {
		absences = []domain.Absence{}
	}
	return absences, total, nil
}

func (r *PgAbsenceRepo) Get(ctx context.Context, id string) (domain.Absence, error) {
	var a domain.Absence
	err := r.Pool.QueryRow(ctx,
		`SELECT absence_id, employee_id, type, start_date, end_date, hours_per_day, approved, source
		 FROM absence WHERE absence_id = $1 AND deleted_at IS NULL`, id,
	).Scan(&a.AbsenceID, &a.EmployeeID, &a.Type, &a.StartDate, &a.EndDate,
		&a.HoursPerDay, &a.Approved, &a.Source)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Absence{}, fmt.Errorf("%w: absence %s", ErrNotFound, id)
		}
		return domain.Absence{}, fmt.Errorf("get absence: %w", err)
	}
	return a, nil
}

func (r *PgAbsenceRepo) Create(ctx context.Context, a domain.Absence) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO absence (absence_id, employee_id, type, start_date, end_date, hours_per_day, approved, source)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		a.AbsenceID, a.EmployeeID, a.Type, a.StartDate, a.EndDate, a.HoursPerDay, a.Approved, a.Source,
	)
	if err != nil {
		if IsDuplicate(err) {
			return fmt.Errorf("%w: duplicate absence", ErrConflict)
		}
		if IsFKViolation(err) {
			return fmt.Errorf("employee not found: %w", err)
		}
		return fmt.Errorf("create absence: %w", err)
	}
	return nil
}

func (r *PgAbsenceRepo) SoftDelete(ctx context.Context, id string) error {
	tag, err := r.Pool.Exec(ctx,
		"UPDATE absence SET deleted_at = now() WHERE absence_id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		return fmt.Errorf("soft delete absence: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: absence %s", ErrNotFound, id)
	}
	return nil
}

func (r *PgAbsenceRepo) HasOverlap(ctx context.Context, employeeID string, start, end time.Time) (bool, error) {
	var exists bool
	err := r.Pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM absence
			WHERE employee_id = $1 AND deleted_at IS NULL AND approved = true
			  AND daterange(start_date, end_date, '[]') && daterange($2, $3, '[]')
		)`, employeeID, start, end,
	).Scan(&exists)
	return exists, err
}

func (r *PgAbsenceRepo) IsAbsentInWeek(ctx context.Context, employeeID, week string) (bool, error) {
	// Convert ISO week to a Monday date, then check if any absence covers it.
	// Week format: YYYY-WNN
	if len(week) != 8 {
		return false, fmt.Errorf("invalid week key: %s", week)
	}
	var year, wn int
	if _, err := fmt.Sscanf(week, "%d-W%d", &year, &wn); err != nil {
		return false, fmt.Errorf("parse week key: %w", err)
	}
	// ISO week date: find Monday of that week.
	// Use time.Date with Jan 4 (always in week 1) and adjust.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	// Monday of week 1:
	weekday := jan4.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	mondayW1 := jan4.AddDate(0, 0, -int(weekday-1))
	monday := mondayW1.AddDate(0, 0, (wn-1)*7)
	tuesday := monday.AddDate(0, 0, 4) // Friday of that week

	var exists bool
	err := r.Pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM absence
			WHERE employee_id = $1 AND deleted_at IS NULL AND approved = true
			  AND start_date <= $3 AND end_date >= $2
		)`, employeeID, monday, tuesday,
	).Scan(&exists)
	return exists, err
}

// ---------- PgAuditRepo ----------

type PgAuditRepo struct{ Pool *pgxpool.Pool }

func NewPgAuditRepo(pool *pgxpool.Pool) *PgAuditRepo {
	return &PgAuditRepo{Pool: pool}
}

func (r *PgAuditRepo) Create(ctx context.Context, e domain.AuditEntry) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO audit_log (event_id, at, actor, action, entity_type, entity_id, request_id, summary, details)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.EventID, e.At, e.Actor, e.Action, e.EntityType, e.EntityID,
		e.RequestID, e.Summary, e.Details,
	)
	if err != nil {
		return fmt.Errorf("create audit entry: %w", err)
	}
	return nil
}
