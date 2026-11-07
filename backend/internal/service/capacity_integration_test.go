package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

// ---------- Test infrastructure ----------

type capTestEnv struct {
	pool    *pgxpool.Pool
	capSvc  *service.CapacityService
	capRepo *repository.PgCapacityRepo
	t       *testing.T
}

func setupCapIntTest(t *testing.T) *capTestEnv {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "teamopt_cap_test",
			"POSTGRES_USER":     "teamopt",
			"POSTGRES_PASSWORD": "teamopt_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://teamopt:teamopt_test@%s:%s/teamopt_cap_test?sslmode=disable", host, port.Port())
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, runCapMigrations(ctx, t, pool))
	require.NoError(t, seedCapReferenceData(ctx, pool))

	empRepo := repository.NewPgEmployeeRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	return &capTestEnv{pool: pool, capSvc: capSvc, capRepo: capRepo, t: t}
}

func runCapMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	// Use the same helper as handler integration tests.
	// We duplicate the logic here to keep this test file self-contained.
	migrations := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		// Reference tables (simplified).
		`CREATE TABLE IF NOT EXISTS level_descriptor (
			level integer PRIMARY KEY CHECK (level BETWEEN 1 AND 5),
			label_ru text NOT NULL, label_en text NOT NULL,
			description_ru text NOT NULL, description_en text NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS role (
			role_id text PRIMARY KEY, name_en text NOT NULL, name_ru text NOT NULL, family text NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS competency (
			competency_id text PRIMARY KEY, name_en text NOT NULL, name_ru text NOT NULL, category text NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS competency_synonym (
			synonym_id text PRIMARY KEY, competency_id text NOT NULL REFERENCES competency(competency_id),
			term text NOT NULL, lang text NOT NULL DEFAULT 'ru'
		)`,
		`CREATE TABLE IF NOT EXISTS role_competency (
			role_id text NOT NULL REFERENCES role(role_id),
			competency_id text NOT NULL REFERENCES competency(competency_id),
			recommended_level integer NOT NULL CHECK (recommended_level BETWEEN 1 AND 5),
			PRIMARY KEY (role_id, competency_id)
		)`,
		`CREATE TABLE IF NOT EXISTS grade_band (
			grade text PRIMARY KEY, label_ru text NOT NULL, label_en text NOT NULL,
			cost_coefficient numeric NOT NULL CHECK (cost_coefficient BETWEEN 0.5 AND 5.0),
			typical_level_min integer NOT NULL, typical_level_max integer NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS weight_preset (
			preset_id text PRIMARY KEY, name text NOT NULL, weights jsonb NOT NULL
		)`,
		// Employee tables.
		`CREATE TABLE IF NOT EXISTS employee (
			employee_id text PRIMARY KEY CHECK (employee_id ~ '^emp_[a-z0-9]{4,12}$'),
			grade text NOT NULL REFERENCES grade_band(grade),
			role_profile_id text NOT NULL REFERENCES role(role_id),
			unit_id text NOT NULL CHECK (unit_id ~ '^unt_[a-z0-9]{2,12}$'),
			employment_type text NOT NULL CHECK (employment_type IN ('internal','contractor','external_partner')),
			bench_flag boolean NOT NULL DEFAULT false,
			weekly_hours_norm integer NOT NULL CHECK (weekly_hours_norm BETWEEN 1 AND 60),
			reserve_ratio numeric NOT NULL CHECK (reserve_ratio BETWEEN 0 AND 0.5),
			max_utilization_ratio numeric NOT NULL CHECK (max_utilization_ratio BETWEEN 0.5 AND 1.0),
			cost_coefficient numeric NOT NULL CHECK (cost_coefficient BETWEEN 0.5 AND 5.0),
			active boolean NOT NULL DEFAULT true,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS employee_competency (
			employee_id text NOT NULL REFERENCES employee(employee_id) ON DELETE CASCADE,
			competency_id text NOT NULL REFERENCES competency(competency_id),
			level integer NOT NULL CHECK (level BETWEEN 1 AND 5),
			confidence numeric NOT NULL CHECK (confidence BETWEEN 0 AND 1),
			last_used date,
			source text NOT NULL CHECK (source IN ('self_assessment','manager_review','project_history','certification','hr_import')),
			PRIMARY KEY (employee_id, competency_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_employee_competency_lookup ON employee_competency (competency_id, level DESC)`,
		// Project tables.
		`CREATE TABLE IF NOT EXISTS project (
			project_id text PRIMARY KEY CHECK (project_id ~ '^prj_[a-z0-9]{4,12}$'),
			code text NOT NULL CHECK (length(code) BETWEEN 1 AND 40),
			name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
			status text NOT NULL CHECK (status IN ('planned','active','paused','done','cancelled')),
			priority integer NOT NULL CHECK (priority BETWEEN 1 AND 5),
			criticality text NOT NULL CHECK (criticality IN ('critical','high','normal','low')),
			start_week text NOT NULL CHECK (start_week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
			end_week_planned text NOT NULL CHECK (end_week_planned ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
			end_week_forecast text CHECK (end_week_forecast IS NULL OR end_week_forecast ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
			replaces_other_projects boolean NOT NULL DEFAULT false,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS assignment (
			assignment_id text PRIMARY KEY CHECK (assignment_id ~ '^asg_[a-z0-9]{4,12}$'),
			project_id text NOT NULL REFERENCES project(project_id) ON DELETE CASCADE,
			employee_id text NOT NULL REFERENCES employee(employee_id),
			role_id text NOT NULL REFERENCES role(role_id),
			week text NOT NULL CHECK (week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
			hours_minutes integer NOT NULL CHECK (hours_minutes > 0),
			source text NOT NULL CHECK (source IN ('plan','actual','forecast')),
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (employee_id, project_id, week)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_assignment_employee_week ON assignment (employee_id, week)`,
		`CREATE TABLE IF NOT EXISTS absence (
			absence_id text PRIMARY KEY CHECK (absence_id ~ '^abs_[a-z0-9]{4,12}$'),
			employee_id text NOT NULL REFERENCES employee(employee_id),
			type text NOT NULL CHECK (type IN ('vacation','sick_leave','training','business_trip','parental_leave','other')),
			start_date date NOT NULL,
			end_date date NOT NULL CHECK (end_date >= start_date),
			hours_per_day numeric NOT NULL CHECK (hours_per_day BETWEEN 0 AND 12),
			approved boolean NOT NULL DEFAULT true,
			source text NOT NULL CHECK (source IN ('manual','hr_import','calendar_import')),
			deleted_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (employee_id, start_date, end_date, type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_absence_employee ON absence (employee_id) WHERE deleted_at IS NULL`,
		// Audit log (simplified).
		`CREATE TABLE IF NOT EXISTS audit_log (
			event_id text PRIMARY KEY,
			at timestamptz NOT NULL,
			actor text NOT NULL,
			action text NOT NULL,
			entity_type text NOT NULL,
			entity_id text NOT NULL,
			request_id text NOT NULL,
			summary text NOT NULL,
			details jsonb
		)`,
	}

	for _, sql := range migrations {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

func seedCapReferenceData(ctx context.Context, pool *pgxpool.Pool) error {
	// Grade bands.
	grades := []struct{ g, ru, en, cost string; min, max int }{
		{"J", "Junior", "Junior", "1.0", 1, 2},
		{"M", "Middle", "Middle", "1.4", 2, 3},
		{"S", "Senior", "Senior", "1.9", 3, 4},
		{"L", "Lead", "Lead", "2.6", 4, 5},
		{"P", "Principal", "Principal", "3.4", 5, 5},
	}
	for _, g := range grades {
		_, err := pool.Exec(ctx,
			`INSERT INTO grade_band (grade, label_ru, label_en, cost_coefficient, typical_level_min, typical_level_max)
			 VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			g.g, g.ru, g.en, g.cost, g.min, g.max)
		if err != nil {
			return err
		}
	}

	// Roles.
	roles := []struct{ id, en, ru, fam string }{
		{"backend_engineer", "Backend Engineer", "Backend", "engineering"},
		{"frontend_engineer", "Frontend Engineer", "Frontend", "engineering"},
	}
	for _, r := range roles {
		_, err := pool.Exec(ctx,
			`INSERT INTO role (role_id, name_en, name_ru, family) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			r.id, r.en, r.ru, r.fam)
		if err != nil {
			return err
		}
	}

	// Competencies.
	comps := []struct{ id, en, ru, cat string }{
		{"lang_go", "Go", "Go", "language"},
		{"lang_java", "Java", "Java", "language"},
		{"db_postgresql", "PostgreSQL", "PostgreSQL", "database"},
	}
	for _, c := range comps {
		_, err := pool.Exec(ctx,
			`INSERT INTO competency (competency_id, name_en, name_ru, category) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			c.id, c.en, c.ru, c.cat)
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------- Helpers ----------

func (e *capTestEnv) createEmployee(id string, norm int, reserve, maxUtil float64) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO employee (employee_id, grade, role_profile_id, unit_id, employment_type,
		   weekly_hours_norm, reserve_ratio, max_utilization_ratio, cost_coefficient, active)
		 VALUES ($1,'S','backend_engineer','unt_core','internal',$2,$3,$4,1.9,true)`,
		id, norm, reserve, maxUtil)
	require.NoError(e.t, err)
}

func (e *capTestEnv) createProject(id string) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO project (project_id, code, name, status, priority, criticality, start_week, end_week_planned)
		 VALUES ($1,'CODE','Name','active',1,'high','2026-W01','2026-W20')`,
		id)
	require.NoError(e.t, err)
}

func (e *capTestEnv) createAssignment(empID, prjID, week string, hours float64) {
	e.t.Helper()
	asgID := domain.GenerateID("asg_", 8)
	minutes := int(hours * 60)
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO assignment (assignment_id, project_id, employee_id, role_id, week, hours_minutes, source)
		 VALUES ($1,$2,$3,'backend_engineer',$4,$5,'plan')`,
		asgID, prjID, empID, week, minutes)
	require.NoError(e.t, err)
}

func (e *capTestEnv) createAbsence(empID string, start, end time.Time, hoursPerDay float64) {
	e.t.Helper()
	absID := domain.GenerateID("abs_", 8)
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO absence (absence_id, employee_id, type, start_date, end_date, hours_per_day, approved, source)
		 VALUES ($1,$2,'vacation',$3,$4,$5,true,'manual')`,
		absID, empID, start, end, hoursPerDay)
	require.NoError(e.t, err)
}

func (e *capTestEnv) addCompetency(empID, compID string, level int) {
	e.t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO employee_competency (employee_id, competency_id, level, confidence, source)
		 VALUES ($1,$2,$3,0.9,'manager_review')`,
		empID, compID, level)
	require.NoError(e.t, err)
}

// ---------- Integration tests ----------

func TestCapInt_BasicCapacityCalculation(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	// Employee with 40h norm, 20% reserve → effective limit = min(32h, 38h) = 32h = 1920 min.
	env.createEmployee("emp_cap00001", 40, 0.2, 0.95)
	env.createProject("prj_cap00001")

	// Assign 10 hours in W03.
	env.createAssignment("emp_cap00001", "prj_cap00001", "2026-W03", 10)

	caps, err := env.capSvc.GetAvailableHours(ctx, "emp_cap00001", []string{"2026-W03"})
	require.NoError(t, err)
	require.Len(t, caps, 1)

	c := caps[0]
	assert.Equal(t, "2026-W03", c.Week)
	assert.Equal(t, 2400, c.NormMinutes)     // 40h × 60
	assert.Equal(t, 600, c.AssignedMinutes)  // 10h × 60
	assert.Equal(t, 0, c.AbsentMinutes)
	assert.Equal(t, 1320, c.AvailableMinutes) // 1920 - 600 = 1320
	assert.InDelta(t, 0.25, c.UtilizationRatio, 0.01) // 600/2400
}

func TestCapInt_G1_AbsenceRejection(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	env.createEmployee("emp_cap00002", 40, 0.2, 0.95)

	// Vacation in W05 (2026-01-26 is Monday of W05).
	start := time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	env.createAbsence("emp_cap00002", start, end, 8)

	// Try to assign in W05 → should fail G1.
	err := env.capSvc.CheckAvailability(ctx, "emp_cap00002", "2026-W05", 60)
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
	assert.Contains(t, p.ReasonCodes, "G1_UNAVAILABLE_ASSIGNED")
}

func TestCapInt_G2_OverloadRejection(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	env.createEmployee("emp_cap00003", 40, 0.2, 0.95)
	env.createProject("prj_cap00003")

	// Assign 30h in W03 (1800 min). Effective limit = 1920 min. Free = 120 min.
	env.createAssignment("emp_cap00003", "prj_cap00003", "2026-W03", 30)

	// Request 3h (180 min) → 1800 + 180 = 1980 > 1920 → G2 violation.
	err := env.capSvc.CheckAvailability(ctx, "emp_cap00003", "2026-W03", 180)
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
	assert.Contains(t, p.ReasonCodes, "G2_OVERLOAD")
}

func TestCapInt_AbsenceReducesCapacity(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	env.createEmployee("emp_cap00004", 40, 0.2, 0.95)

	// Partial week absence: Mon–Wed (3 working days) in W10.
	// W10 of 2026: Monday = March 2.
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	env.createAbsence("emp_cap00004", start, end, 8)

	caps, err := env.capSvc.GetAvailableHours(ctx, "emp_cap00004", []string{"2026-W10"})
	require.NoError(t, err)
	require.Len(t, caps, 1)

	c := caps[0]
	// Absent minutes: 8h × 3 days × 60 = 1440 min.
	assert.Equal(t, 1440, c.AbsentMinutes)
	// Effective limit = 1920 min. Available = 1920 - 0 - 1440 = 480 min.
	assert.Equal(t, 480, c.AvailableMinutes)
}

func TestCapInt_FindAvailableEmployees(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	// Create 3 employees with different competency levels and availability.
	env.createEmployee("emp_cap00010", 40, 0.2, 0.95) // go:4
	env.createEmployee("emp_cap00011", 40, 0.2, 0.95) // go:3
	env.createEmployee("emp_cap00012", 40, 0.2, 0.95) // go:2

	env.addCompetency("emp_cap00010", "lang_go", 4)
	env.addCompetency("emp_cap00011", "lang_go", 3)
	env.addCompetency("emp_cap00012", "lang_go", 2)

	env.createProject("prj_cap00010")

	// Overload emp_cap00010 in W05 (assign 32h = full effective limit).
	env.createAssignment("emp_cap00010", "prj_cap00010", "2026-W05", 32)

	// Find employees with go >= 3 and at least 600 min available in W05.
	result, err := env.capSvc.FindAvailableEmployees(ctx, []string{"2026-W05"}, 600, "lang_go", 3)
	require.NoError(t, err)

	// emp_cap00010: overloaded (0 available) → excluded.
	// emp_cap00011: full capacity → included.
	// emp_cap00012: level 2 < minLevel 3 → excluded.
	assert.Contains(t, result, "emp_cap00011")
	assert.NotContains(t, result, "emp_cap00010")
	assert.NotContains(t, result, "emp_cap00012")
}

func TestCapInt_CapacityMatrix(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	env.createEmployee("emp_cap00020", 40, 0.2, 0.95)
	env.createEmployee("emp_cap00021", 40, 0.1, 0.95)
	env.createProject("prj_cap00020")

	env.createAssignment("emp_cap00020", "prj_cap00020", "2026-W05", 10)
	env.createAssignment("emp_cap00021", "prj_cap00020", "2026-W05", 20)

	matrix, err := env.capSvc.GetCapacityMatrix(ctx, []string{"emp_cap00020", "emp_cap00021"}, "2026-W05", "2026-W05")
	require.NoError(t, err)

	assert.Len(t, matrix, 2)

	// emp_cap00020: norm=2400, effective_limit=min(1920,2280)=1920, assigned=600, available=1320.
	c20 := matrix["emp_cap00020"]
	require.Len(t, c20, 1)
	assert.Equal(t, 1320, c20[0].AvailableMinutes)

	// emp_cap00021: norm=2400, effective_limit=min(2160,2280)=2160, assigned=1200, available=960.
	c21 := matrix["emp_cap00021"]
	require.Len(t, c21, 1)
	assert.Equal(t, 960, c21[0].AvailableMinutes)
}

func TestCapInt_EdgeCase_ZeroAvailableAfterFullAssignment(t *testing.T) {
	env := setupCapIntTest(t)
	ctx := context.Background()

	// Employee with 20h norm, 0% reserve, max_util=0.95.
	env.createEmployee("emp_cap00030", 20, 0.0, 0.95)
	env.createProject("prj_cap00030")

	// Assign exactly the effective limit: 20h × 0.95 = 19h = 1140 min.
	env.createAssignment("emp_cap00030", "prj_cap00030", "2026-W10", 19)

	caps, err := env.capSvc.GetAvailableHours(ctx, "emp_cap00030", []string{"2026-W10"})
	require.NoError(t, err)
	require.Len(t, caps, 1)
	assert.Equal(t, 0, caps[0].AvailableMinutes) // No capacity left.

	// Any additional assignment should fail G2.
	err = env.capSvc.CheckAvailability(ctx, "emp_cap00030", "2026-W10", 60)
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
}
