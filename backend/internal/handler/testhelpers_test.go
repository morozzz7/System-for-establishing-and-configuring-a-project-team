package handler_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationDir is the path to the SQL migration files relative to the project root.
// In tests, we need to resolve this relative to the module root.
func findMigrationsDir() string {
	// Walk up from the test binary to find the db/migrations directory.
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, "db", "migrations")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: try relative to backend/
	return filepath.Join("..", "..", "db", "migrations")
}

func runMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	migrationsDir := findMigrationsDir()

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		// Extract the "Up" section (between -- +goose Up and -- +goose Down).
		sql := extractGooseUp(string(content))
		if sql == "" {
			continue
		}

		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("exec migration %s: %w", entry.Name(), err)
		}
		t.Logf("migration applied: %s", entry.Name())
	}
	return nil
}

// extractGooseUp extracts the SQL between "-- +goose Up" and "-- +goose Down".
func extractGooseUp(content string) string {
	upMarker := "-- +goose Up"
	downMarker := "-- +goose Down"

	upIdx := strings.Index(content, upMarker)
	if upIdx < 0 {
		return ""
	}
	afterUp := content[upIdx+len(upMarker):]

	downIdx := strings.Index(afterUp, downMarker)
	if downIdx < 0 {
		return strings.TrimSpace(afterUp)
	}
	return strings.TrimSpace(afterUp[:downIdx])
}

// seedReferenceData inserts the minimal reference data needed for CRUD tests.
func seedReferenceData(ctx context.Context, pool *pgxpool.Pool) error {
	// Insert grade bands.
	grades := []struct {
		grade string
		ru    string
		en    string
		cost  string
		min   int
		max   int
	}{
		{"J", "Джуниор", "Junior", "1.0", 1, 2},
		{"M", "Мидл", "Middle", "1.4", 2, 3},
		{"S", "Сеньор", "Senior", "1.9", 3, 4},
		{"L", "Лид", "Lead", "2.6", 4, 5},
		{"P", "Принципал", "Principal", "3.4", 5, 5},
	}
	for _, g := range grades {
		_, err := pool.Exec(ctx,
			`INSERT INTO grade_band (grade, label_ru, label_en, cost_coefficient, typical_level_min, typical_level_max)
			 VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			g.grade, g.ru, g.en, g.cost, g.min, g.max)
		if err != nil {
			return fmt.Errorf("seed grade %s: %w", g.grade, err)
		}
	}

	// Insert a minimal set of roles.
	roles := []struct {
		id, en, ru, family string
	}{
		{"backend_engineer", "Backend Engineer", "Бэкенд-разработчик", "engineering"},
		{"frontend_engineer", "Frontend Engineer", "Фронтенд-разработчик", "engineering"},
		{"qa_engineer", "QA Engineer", "Инженер по тестированию", "quality"},
		{"business_analyst", "Business Analyst", "Бизнес-аналитик", "analysis"},
		{"devops_engineer", "DevOps Engineer", "Инженер инфраструктуры", "infrastructure"},
	}
	for _, r := range roles {
		_, err := pool.Exec(ctx,
			`INSERT INTO role (role_id, name_en, name_ru, family) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			r.id, r.en, r.ru, r.family)
		if err != nil {
			return fmt.Errorf("seed role %s: %w", r.id, err)
		}
	}

	// Insert a minimal set of competencies.
	compets := []struct {
		id, en, ru, cat string
	}{
		{"lang_go", "Go", "Go", "language"},
		{"lang_java", "Java", "Java", "language"},
		{"db_postgresql", "PostgreSQL", "PostgreSQL", "database"},
		{"rest_api", "REST API", "REST API", "architecture"},
		{"testing", "Testing", "Тестирование", "testing"},
	}
	for _, c := range compets {
		_, err := pool.Exec(ctx,
			`INSERT INTO competency (competency_id, name_en, name_ru, category) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			c.id, c.en, c.ru, c.cat)
		if err != nil {
			return fmt.Errorf("seed competency %s: %w", c.id, err)
		}
	}

	return nil
}
