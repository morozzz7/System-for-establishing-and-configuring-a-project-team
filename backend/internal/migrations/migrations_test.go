// Package migrations_test runs goose migrations against a real PostgreSQL
// (testcontainers-go) and verifies CHECK / FK / up-down round-trip invariants.
//
// Run with:  cd backend && go test ./internal/migrations/ -v -count=1
//
// Requires Docker on the host (testcontainers-go uses it).
package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

// migrationsDir returns the absolute path to db/migrations.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// internal/migrations -> backend -> repo root.
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "db", "migrations")
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("migrations directory not found at %s", abs)
	}
	return abs
}

// startPostgres spins up a pgvector:pg16 container and returns a *sql.DB plus a cleanup.
func startPostgres(t *testing.T) (*sql.DB, func(context.Context) error) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "teamopt",
			"POSTGRES_PASSWORD": "teamopt",
			"POSTGRES_DB":       "teamopt_test",
		},
		WaitingFor: tcwait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "failed to start postgres container")

	mappedPort, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	host, err := container.Host(ctx)
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://teamopt:teamopt@%s:%s/teamopt_test?sslmode=disable",
		host, mappedPort.Port())

	// Wait until the DB accepts connections.
	var db *sql.DB
	require.Eventually(t, func() bool {
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return false
		}
		return db.Ping() == nil
	}, 20*time.Second, 500*time.Millisecond, "postgres did not become ready")

	cleanup := func(c context.Context) error {
		_ = db.Close()
		return container.Terminate(c)
	}
	return db, cleanup
}

// ----------------------------------------------------------
// Test: up -> down -> up round-trip.
// ----------------------------------------------------------

func TestUpDownRoundtrip(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)

	// Up.
	require.NoError(t, goose.Up(db, dir), "goose up failed")

	// Verify key tables exist.
	tables := []string{
		"level_descriptor", "role", "competency", "competency_synonym",
		"role_competency", "grade_band", "weight_preset",
		"employee", "employee_competency",
		"project", "assignment", "absence",
		"brief", "requirement", "requirement_assumption", "requirement_evidence",
		"team_run", "run_snapshot", "run_snapshot_candidate",
		"team_variant", "variant_member", "variant_weekly_assignment",
		"variant_deficit", "variant_recommended_action", "variant_active_constraint",
		"variant_excluded_candidate",
		"audit_log", "constraint_override", "decision", "explanation",
	}
	for _, tbl := range tables {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema='public' AND table_name=$1
			)`, tbl).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "table %s missing after migration up", tbl)
	}

	// Down to 0.
	require.NoError(t, goose.DownTo(db, dir, 0), "goose down to 0 failed")

	// After full rollback, only the goose_db_version table should remain.
	var gooseTables int
	err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_type='BASE TABLE'
		  AND table_name LIKE 'goose_%'`).Scan(&gooseTables)
	require.NoError(t, err)
	assert.Equal(t, 1, gooseTables, "expected only goose_db_version table after full down")

	var appTables int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_type='BASE TABLE'
		  AND table_name NOT LIKE 'goose_%'`).Scan(&appTables)
	require.NoError(t, err)
	assert.Equal(t, 0, appTables, "expected 0 application tables after full down migration, got %d", appTables)

	// Up again.
	require.NoError(t, goose.Up(db, dir), "goose up after down failed")
}

// ----------------------------------------------------------
// Test: CHECK constraints reject bad data.
// ----------------------------------------------------------

func TestCheckConstraints(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)
	require.NoError(t, goose.Up(db, dir))

	t.Run("employee_id_pattern", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO employee (employee_id, grade, role_profile_id, unit_id,
				employment_type, weekly_hours_norm, reserve_ratio,
				max_utilization_ratio, cost_coefficient)
			VALUES ('BAD_ID', 'J', 'backend_engineer', 'unt_a1',
				'internal', 40, 0.2, 0.9, 1.0)`)
		assertCheckViolation(t, err, "employee.employee_id")
	})

	t.Run("week_pattern_project", func(t *testing.T) {
		err := execDB(db, `
			INSERT INTO project (project_id, code, name, status, priority, criticality,
				start_week, end_week_planned)
			VALUES ('prj_abcd', 'X', 'Test', 'planned', 1, 'normal',
				'2026-W99', '2026-W52')`)
		assertCheckViolation(t, err, "project.start_week")
	})

	t.Run("level_1_to_5", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO employee_competency
				(employee_id, competency_id, level, confidence, source)
			VALUES ('emp_abcd', 'lang_go', 0, 1.0, 'self_assessment')`)
		assertCheckViolation(t, err, "employee_competency.level=0")
		err = execDB(db, `
			INSERT INTO employee_competency
				(employee_id, competency_id, level, confidence, source)
			VALUES ('emp_abcd', 'lang_go', 6, 1.0, 'self_assessment')`)
		assertCheckViolation(t, err, "employee_competency.level=6")
	})

	t.Run("effort_ordering", func(t *testing.T) {
		insertPrereqs(t, db)
		insertBrief(t, db)
		err := execDB(db, `
			INSERT INTO requirement (requirement_id, brief_id, role_id, role_confidence,
				competency_id, level, criticality, headcount,
				effort_min, effort_likely, effort_max, confidence)
			VALUES ('req_A001', 'brf_A001', 'backend_engineer', 0.9,
				'lang_go', 3, 'must', 1,
				10, 5, 20, 0.8)`)
		assertCheckViolation(t, err, "requirement.effort_min<=effort_likely")
	})

	t.Run("hours_minutes_positive", func(t *testing.T) {
		insertPrereqs(t, db)
		insertProject(t, db)
		err := execDB(db, `
			INSERT INTO assignment (assignment_id, project_id, employee_id, role_id,
				week, hours_minutes, source)
			VALUES ('asg_abcd', 'prj_abcd', 'emp_abcd', 'backend_engineer',
				'2026-W01', 0, 'plan')`)
		assertCheckViolation(t, err, "assignment.hours_minutes>0")
	})

	t.Run("grade_invalid", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO employee (employee_id, grade, role_profile_id, unit_id,
				employment_type, weekly_hours_norm, reserve_ratio,
				max_utilization_ratio, cost_coefficient)
			VALUES ('emp_badc', 'X', 'backend_engineer', 'unt_a1',
				'internal', 40, 0.2, 0.9, 1.0)`)
		// Grade 'X' is rejected by either the CHECK enum or the FK to grade_band.
		assert.True(t, err != nil, "expected error for invalid grade 'X'")
		msg := strings.ToLower(err.Error())
		assert.True(t, strings.Contains(msg, "violates check constraint") ||
			strings.Contains(msg, "violates foreign key") ||
			strings.Contains(msg, "check") ||
			strings.Contains(msg, "foreign key"),
			"expected CHECK or FK violation for invalid grade, got: %v", err)
	})

	t.Run("absence_end_after_start", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO absence (absence_id, employee_id, type, start_date, end_date,
				hours_per_day, source)
			VALUES ('abs_abcd', 'emp_abcd', 'vacation',
				'2026-06-15', '2026-06-10', 8, 'manual')`)
		assertCheckViolation(t, err, "absence.end_date>=start_date")
	})

	t.Run("cost_coefficient_range", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO employee (employee_id, grade, role_profile_id, unit_id,
				employment_type, weekly_hours_norm, reserve_ratio,
				max_utilization_ratio, cost_coefficient)
			VALUES ('emp_cost1', 'J', 'backend_engineer', 'unt_a1',
				'internal', 40, 0.2, 0.9, 99.0)`)
		assertCheckViolation(t, err, "employee.cost_coefficient")
	})

	t.Run("score_between_0_and_1", func(t *testing.T) {
		insertPrereqs(t, db)
		insertBrief(t, db)
		execDB(db, `INSERT INTO team_run (run_id, brief_id, status, preset, k, seed,
			time_limit_ms, start_week, end_week, weights)
			VALUES ('run_A001','brf_A001','queued','fast',3,42,10000,'2026-W01','2026-W10','{}'::jsonb)`)
		err := execDB(db, `INSERT INTO team_variant (variant_id, run_id, label, gate_pass,
			score, team_size, cost_units, duration, metric_values, reason_codes)
			VALUES ('var_A1','run_A001','Fast',true,1.5,3,100,'{}','{}','{}')`)
		assertCheckViolation(t, err, "team_variant.score<=1")
	})
}

// ----------------------------------------------------------
// Test: FK constraints reject dangling references.
// ----------------------------------------------------------

func TestForeignKeyConstraints(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)
	require.NoError(t, goose.Up(db, dir))

	t.Run("employee_competency_missing_employee", func(t *testing.T) {
		insertCompetencyOnly(t, db)
		err := execDB(db, `
			INSERT INTO employee_competency
				(employee_id, competency_id, level, confidence, source)
			VALUES ('emp_nope', 'lang_go', 3, 1.0, 'self_assessment')`)
		assertFKViolation(t, err, "employee_competency.employee_id")
	})

	t.Run("assignment_missing_project", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO assignment (assignment_id, project_id, employee_id, role_id,
				week, hours_minutes, source)
			VALUES ('asg_zzzz', 'prj_nope', 'emp_abcd', 'backend_engineer',
				'2026-W01', 60, 'plan')`)
		assertFKViolation(t, err, "assignment.project_id")
	})

	t.Run("requirement_missing_brief", func(t *testing.T) {
		insertPrereqs(t, db)
		err := execDB(db, `
			INSERT INTO requirement (requirement_id, brief_id, role_id, role_confidence,
				competency_id, level, criticality, headcount,
				effort_min, effort_likely, effort_max, confidence)
			VALUES ('req_ZZZZ', 'brf_nope', 'backend_engineer', 0.9,
				'lang_go', 3, 'must', 1, 5, 10, 20, 0.8)`)
		assertFKViolation(t, err, "requirement.brief_id")
	})

	t.Run("team_run_missing_brief", func(t *testing.T) {
		err := execDB(db, `
			INSERT INTO team_run (run_id, brief_id, status, preset, k, seed,
				time_limit_ms, start_week, end_week, weights)
			VALUES ('run_A001', 'brf_nope', 'queued', 'fast', 3, 42,
				10000, '2026-W01', '2026-W10', '{}'::jsonb)`)
		assertFKViolation(t, err, "team_run.brief_id")
	})

	t.Run("variant_member_missing_variant", func(t *testing.T) {
		err := execDB(db, `
			INSERT INTO variant_member (variant_id, employee_id, role_id, grade,
				allocated_hours_minutes, assignment_share, competence_fit, reason_codes)
			VALUES ('var_ZZ', 'emp_abcd', 'backend_engineer', 'S',
				480, 0.5, 0.9, '{"INCLUDE_HIGH_COMPETENCE"}')`)
		assertFKViolation(t, err, "variant_member.variant_id")
	})

	t.Run("constraint_override_missing_run", func(t *testing.T) {
		err := execDB(db, `
			INSERT INTO constraint_override (run_id, type)
			VALUES ('run_nope', 'force_include')`)
		assertFKViolation(t, err, "constraint_override.run_id")
	})

	t.Run("explanation_missing_variant", func(t *testing.T) {
		err := execDB(db, `
			INSERT INTO explanation (explanation_id, variant_id, locale, headline,
				tradeoff, included, excluded, deficits, counterfactuals)
			VALUES ('exp_abcd', 'var_nope', 'ru', 'Test',
				'{}','[]','[]','[]','[]')`)
		assertFKViolation(t, err, "explanation.variant_id")
	})
}

// ----------------------------------------------------------
// Test: ON DELETE CASCADE works correctly.
// ----------------------------------------------------------

func TestCascadeDelete(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)
	require.NoError(t, goose.Up(db, dir))

	insertFullChain(t, db)

	// Delete brief -> requirement + assumption + evidence must vanish.
	_, err := db.ExecContext(ctx, `DELETE FROM brief WHERE brief_id = 'brf_A001'`)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM requirement WHERE brief_id = 'brf_A001'`).Scan(&count))
	assert.Equal(t, 0, count, "requirement must be cascade-deleted with brief")

	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM requirement_assumption WHERE requirement_id = 'req_A001'`).Scan(&count))
	assert.Equal(t, 0, count, "requirement_assumption must be cascade-deleted with requirement")

	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM requirement_evidence WHERE requirement_id = 'req_A001'`).Scan(&count))
	assert.Equal(t, 0, count, "requirement_evidence must be cascade-deleted with requirement")
}

// ----------------------------------------------------------
// Test: pgvector extension is installed.
// ----------------------------------------------------------

func TestPgvectorExtension(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)
	require.NoError(t, goose.Up(db, dir))

	var extVersion string
	err := db.QueryRowContext(ctx,
		`SELECT extversion FROM pg_extension WHERE extname = 'vector'`).Scan(&extVersion)
	require.NoError(t, err, "pgvector extension must be installed")
	assert.NotEmpty(t, extVersion)
}

// ----------------------------------------------------------
// Test: migration files are named 00001..00007.
// ----------------------------------------------------------

func TestMigrationOrder(t *testing.T) {
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	expected := []string{
		"00001_extensions.sql",
		"00002_reference.sql",
		"00003_employee.sql",
		"00004_project.sql",
		"00005_brief.sql",
		"00006_run.sql",
		"00007_audit.sql",
	}
	assert.Equal(t, expected, names)
}

// ----------------------------------------------------------
// helpers
// ----------------------------------------------------------

func execDB(db *sql.DB, query string, args ...any) error {
	_, err := db.Exec(query, args...)
	return err
}

func execIgnore(db *sql.DB, query string, args ...any) {
	_, _ = db.Exec(query, args...)
}

func insertPrereqs(t *testing.T, db *sql.DB) {
	t.Helper()
	execIgnore(db, `INSERT INTO level_descriptor (level, label_ru, label_en, description_ru, description_en)
		VALUES (1,'Озн','Awareness','rue','ene') ON CONFLICT DO NOTHING`)
	execIgnore(db, `INSERT INTO grade_band (grade, label_ru, label_en, cost_coefficient, typical_level_min, typical_level_max)
		VALUES ('J','Дж','Jr',1.0,1,2) ON CONFLICT DO NOTHING`)
	execIgnore(db, `INSERT INTO role (role_id, name_en, name_ru, family)
		VALUES ('backend_engineer','Backend Engineer','BE','engineering') ON CONFLICT DO NOTHING`)
	insertCompetencyOnly(t, db)
	execIgnore(db, `INSERT INTO employee (employee_id, grade, role_profile_id, unit_id,
		employment_type, weekly_hours_norm, reserve_ratio, max_utilization_ratio, cost_coefficient)
		VALUES ('emp_abcd','J','backend_engineer','unt_a1','internal',40,0.2,0.9,1.0)
		ON CONFLICT DO NOTHING`)
}

func insertCompetencyOnly(t *testing.T, db *sql.DB) {
	t.Helper()
	execIgnore(db, `INSERT INTO competency (competency_id, name_en, name_ru, category)
		VALUES ('lang_go','Go','Go','language') ON CONFLICT DO NOTHING`)
}

func insertProject(t *testing.T, db *sql.DB) {
	t.Helper()
	execIgnore(db, `INSERT INTO project (project_id, code, name, status, priority, criticality,
		start_week, end_week_planned)
		VALUES ('prj_abcd','X','Test','planned',1,'normal','2026-W01','2026-W10')
		ON CONFLICT DO NOTHING`)
}

func insertBrief(t *testing.T, db *sql.DB) {
	t.Helper()
	execIgnore(db, `INSERT INTO brief (brief_id, title, source_type, language, status, text, text_sha256)
		VALUES ('brf_A001','Test Brief','text','ru','uploaded',
			'Some brief text for testing purposes.',
			'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
		ON CONFLICT DO NOTHING`)
}

func insertFullChain(t *testing.T, db *sql.DB) {
	t.Helper()
	insertPrereqs(t, db)
	insertBrief(t, db)
	execIgnore(db, `INSERT INTO requirement (requirement_id, brief_id, role_id, role_confidence,
		competency_id, level, criticality, headcount,
		effort_min, effort_likely, effort_max, confidence)
		VALUES ('req_A001','brf_A001','backend_engineer',0.9,'lang_go',3,'must',1,5,10,20,0.8)
		ON CONFLICT DO NOTHING`)
	execIgnore(db, `INSERT INTO requirement_assumption (requirement_id, position, text)
		VALUES ('req_A001', 0, 'Assumes standard Go environment.')
		ON CONFLICT DO NOTHING`)
	execIgnore(db, `INSERT INTO requirement_evidence (requirement_id, kind, quote, position)
		VALUES ('req_A001','brief_span','testing purposes',0)
		ON CONFLICT DO NOTHING`)
}

func assertCheckViolation(t *testing.T, err error, label string) {
	t.Helper()
	require.Error(t, err, "expected CHECK violation on %s", label)
	msg := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(msg, "violates check constraint") ||
		strings.Contains(msg, "check"),
		"expected CHECK violation for %s, got: %v", label, err)
}

func assertFKViolation(t *testing.T, err error, label string) {
	t.Helper()
	require.Error(t, err, "expected FK violation on %s", label)
	msg := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(msg, "foreign key constraint") ||
		strings.Contains(msg, "violates foreign key"),
		"expected FK violation for %s, got: %v", label, err)
}
