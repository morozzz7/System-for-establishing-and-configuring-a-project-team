package migrations_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeedPopulatesReference verifies the seed logic loads the expected
// number of roles, competencies, grades, weight presets and level descriptors.
func TestSeedPopulatesReference(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)
	require.NoError(t, goose.Up(db, dir))

	// Locate repo root.
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")
	absRoot, err := filepath.Abs(repoRoot)
	require.NoError(t, err)

	// Run seed inline.
	runSeedInline(t, db, absRoot)

	// Verify counts.
	var n int

	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM level_descriptor`).Scan(&n))
	assert.Equal(t, 5, n, "expected 5 level descriptors")

	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM role`).Scan(&n))
	assert.Equal(t, 12, n, "expected 12 roles")

	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM competency`).Scan(&n))
	assert.GreaterOrEqual(t, n, 101, "expected >= 101 competencies")

	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM grade_band`).Scan(&n))
	assert.Equal(t, 5, n, "expected 5 grades")

	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM weight_preset`).Scan(&n))
	assert.Equal(t, 4, n, "expected 4 weight presets")

	// Verify role_competency.
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM role_competency`).Scan(&n))
	assert.Greater(t, n, 50, "expected > 50 role_competency rows")

	// Verify some core competencies are marked is_core.
	var coreCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM role_competency WHERE is_core = true`).Scan(&coreCount))
	assert.Greater(t, coreCount, 20, "expected > 20 is_core=true rows")

	// Verify synonyms were loaded.
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM competency_synonym`).Scan(&n))
	assert.Greater(t, n, 50, "expected > 50 synonyms")

	// Idempotency: run seed again, counts must not change.
	runSeedInline(t, db, absRoot)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM role`).Scan(&n))
	assert.Equal(t, 12, n, "seed idempotency: roles count changed")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM competency`).Scan(&n))
	assert.GreaterOrEqual(t, n, 101, "seed idempotency: competencies count dropped")
}

// TestSeedNoPersonalData verifies that reference tables contain no
// personal data fields (no names, emails, phones, salaries).
func TestSeedNoPersonalData(t *testing.T) {
	ctx := context.Background()
	db, cleanup := startPostgres(t)
	defer cleanup(ctx) //nolint:errcheck

	dir := migrationsDir(t)
	require.NoError(t, goose.Up(db, dir))

	// Check column names of employee-related tables — none should contain
	// personal data columns.
	tables := []string{"role", "competency", "grade_band", "employee"}
	for _, tbl := range tables {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1
			  AND (column_name ILIKE '%email%' OR column_name ILIKE '%phone%'
			    OR column_name ILIKE '%salary%' OR column_name ILIKE '%first_name%'
			    OR column_name ILIKE '%last_name%' OR column_name ILIKE '%personal%')`,
			tbl).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "table %s contains personal-data column", tbl)
	}
}

// Ensure os import is used (needed in TestSeedNoPersonalData indirectly via filepath).
var _ = os.DevNull
