// Package main is the seed binary that:
//  1. Applies all goose migrations from db/migrations/.
//  2. Loads reference / taxonomy data from contracts/taxonomy.yaml and
//     contracts/reference.yaml into the database.
//
// It is idempotent (ON CONFLICT DO NOTHING / goose idempotency) and safe
// to run repeatedly.
//
// Sub-commands:
//
//	seed          — apply migrations + load reference data (default)
//	migrate-only  — apply migrations only, skip seed
//	healthcheck   — exit 0 if binary is functional
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"gopkg.in/yaml.v3"
)

// taxonomy.yaml shape.
type taxonomyFile struct {
	SchemaVersion string `yaml:"schema_version"`
	LevelScale    int    `yaml:"level_scale"`
	Levels        []struct {
		Level         int    `yaml:"level"`
		LabelRU       string `yaml:"label_ru"`
		LabelEN       string `yaml:"label_en"`
		DescriptionRU string `yaml:"description_ru"`
		DescriptionEN string `yaml:"description_en"`
	} `yaml:"levels"`
	Roles []struct {
		RoleID           string   `yaml:"role_id"`
		NameEN           string   `yaml:"name_en"`
		NameRU           string   `yaml:"name_ru"`
		Family           string   `yaml:"family"`
		Synonyms         []string `yaml:"synonyms"`
		CoreCompetencies []string `yaml:"core_competencies"`
		TypicalGrades    []string `yaml:"typical_grades"`
	} `yaml:"roles"`
	Competencies []struct {
		CompetencyID string   `yaml:"competency_id"`
		NameEN       string   `yaml:"name_en"`
		NameRU       string   `yaml:"name_ru"`
		Category     string   `yaml:"category"`
		Roles        []string `yaml:"roles"`
		Synonyms     []string `yaml:"synonyms"`
	} `yaml:"competencies"`
}

// reference.yaml shape.
type referenceFile struct {
	Grades []struct {
		Grade             string  `yaml:"grade"`
		LabelRU           string  `yaml:"label_ru"`
		LabelEN           string  `yaml:"label_en"`
		CostCoefficient   float64 `yaml:"cost_coefficient"`
		TypicalLevelRange []int   `yaml:"typical_level_range"`
	} `yaml:"grades"`
	WeightsPresets []struct {
		Preset  string             `yaml:"preset"`
		LabelRU string             `yaml:"label_ru"`
		Weights map[string]float64 `yaml:"weights"`
		Sum     float64            `yaml:"sum"`
	} `yaml:"weights_presets"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		fmt.Println("seed ok")
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL is empty")
		os.Exit(1)
	}

	// 1. Apply migrations via goose (uses database/sql with pgx driver).
	if err := runMigrations(dsn); err != nil {
		logger.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	if len(os.Args) > 1 && os.Args[1] == "migrate-only" {
		logger.Info("migrate-only: done")
		return
	}

	// 2. Seed reference data via pgxpool.
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("db ping failed", "error", err)
		os.Exit(1)
	}

	taxBytes, refBytes, err := loadContracts()
	if err != nil {
		logger.Error("failed to load contracts", "error", err)
		os.Exit(1)
	}

	var tax taxonomyFile
	if err := yaml.Unmarshal(taxBytes, &tax); err != nil {
		logger.Error("failed to parse taxonomy.yaml", "error", err)
		os.Exit(1)
	}
	var ref referenceFile
	if err := yaml.Unmarshal(refBytes, &ref); err != nil {
		logger.Error("failed to parse reference.yaml", "error", err)
		os.Exit(1)
	}

	if err := seedLevels(ctx, pool, tax); err != nil {
		logger.Error("seed levels failed", "error", err)
		os.Exit(1)
	}
	if err := seedRoles(ctx, pool, tax); err != nil {
		logger.Error("seed roles failed", "error", err)
		os.Exit(1)
	}
	if err := seedCompetencies(ctx, pool, tax); err != nil {
		logger.Error("seed competencies failed", "error", err)
		os.Exit(1)
	}
	if err := seedRoleCompetencies(ctx, pool, tax); err != nil {
		logger.Error("seed role_competency failed", "error", err)
		os.Exit(1)
	}
	if err := seedGrades(ctx, pool, ref); err != nil {
		logger.Error("seed grades failed", "error", err)
		os.Exit(1)
	}
	if err := seedWeightPresets(ctx, pool, ref); err != nil {
		logger.Error("seed weight presets failed", "error", err)
		os.Exit(1)
	}

	logger.Info("seed completed",
		"levels", len(tax.Levels),
		"roles", len(tax.Roles),
		"competencies", len(tax.Competencies),
		"grades", len(ref.Grades),
		"weight_presets", len(ref.WeightsPresets),
	)
}

// runMigrations applies all goose migrations up.
func runMigrations(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	dir := migrationsDir()
	return goose.Up(db, dir)
}

// migrationsDir resolves the path to db/migrations relative to the binary source
// or the CWD (for container builds where migrations are copied to /app/db/migrations).
func migrationsDir() string {
	// 1. Relative to source file.
	if _, file, _, ok := runtime.Caller(0); ok {
		dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "db", "migrations")
		if abs, err := filepath.Abs(dir); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	// 2. CWD-relative (container).
	return filepath.Join("db", "migrations")
}

// loadContracts resolves contracts files from the filesystem.
// Search order:
//  1. Relative to this source file (works with `go run` from any CWD).
//  2. Relative to CWD (works in containers with CWD=/app and contracts at ./contracts).
func loadContracts() (tax, ref []byte, err error) {
	candidates := []string{}

	// 1. Relative to source file (go run from any dir).
	if _, file, _, ok := runtime.Caller(0); ok {
		dir := filepath.Dir(file)
		// backend/cmd/seed -> 3 levels up to repo root.
		repoRoot := filepath.Join(dir, "..", "..", "..")
		candidates = append(candidates, filepath.Join(repoRoot, "contracts"))
	}

	// 2. CWD/contracts (container: WORKDIR=/app, contracts at /app/contracts).
	candidates = append(candidates, "contracts")

	for _, dir := range candidates {
		taxPath := filepath.Join(dir, "taxonomy.yaml")
		refPath := filepath.Join(dir, "reference.yaml")
		tax, err1 := os.ReadFile(taxPath)
		if err1 != nil {
			continue
		}
		ref, err2 := os.ReadFile(refPath)
		if err2 != nil {
			continue
		}
		return tax, ref, nil
	}
	return nil, nil, fmt.Errorf("contracts not found (searched %v)", candidates)
}

func seedLevels(ctx context.Context, pool *pgxpool.Pool, tax taxonomyFile) error {
	for _, l := range tax.Levels {
		_, err := pool.Exec(ctx, `
			INSERT INTO level_descriptor (level, label_ru, label_en, description_ru, description_en)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (level) DO NOTHING`,
			l.Level, l.LabelRU, l.LabelEN, l.DescriptionRU, l.DescriptionEN,
		)
		if err != nil {
			return fmt.Errorf("level %d: %w", l.Level, err)
		}
	}
	return nil
}

func seedRoles(ctx context.Context, pool *pgxpool.Pool, tax taxonomyFile) error {
	for _, r := range tax.Roles {
		_, err := pool.Exec(ctx, `
			INSERT INTO role (role_id, name_en, name_ru, family, synonyms, core_competencies, typical_grades)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (role_id) DO UPDATE SET
				name_en = EXCLUDED.name_en,
				name_ru = EXCLUDED.name_ru,
				family  = EXCLUDED.family,
				synonyms = EXCLUDED.synonyms,
				core_competencies = EXCLUDED.core_competencies,
				typical_grades = EXCLUDED.typical_grades,
				updated_at = now()`,
			r.RoleID, r.NameEN, r.NameRU, r.Family, r.Synonyms, r.CoreCompetencies, r.TypicalGrades,
		)
		if err != nil {
			return fmt.Errorf("role %s: %w", r.RoleID, err)
		}
	}
	return nil
}

func seedCompetencies(ctx context.Context, pool *pgxpool.Pool, tax taxonomyFile) error {
	for _, c := range tax.Competencies {
		_, err := pool.Exec(ctx, `
			INSERT INTO competency (competency_id, name_en, name_ru, category)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (competency_id) DO UPDATE SET
				name_en  = EXCLUDED.name_en,
				name_ru  = EXCLUDED.name_ru,
				category = EXCLUDED.category,
				updated_at = now()`,
			c.CompetencyID, c.NameEN, c.NameRU, c.Category,
		)
		if err != nil {
			return fmt.Errorf("competency %s: %w", c.CompetencyID, err)
		}
		for _, s := range c.Synonyms {
			_, err := pool.Exec(ctx, `
				INSERT INTO competency_synonym (competency_id, synonym)
				VALUES ($1, $2)
				ON CONFLICT (competency_id, synonym) DO NOTHING`,
				c.CompetencyID, s,
			)
			if err != nil {
				return fmt.Errorf("synonym %s/%s: %w", c.CompetencyID, s, err)
			}
		}
	}
	return nil
}

func seedRoleCompetencies(ctx context.Context, pool *pgxpool.Pool, tax taxonomyFile) error {
	for _, r := range tax.Roles {
		core := map[string]bool{}
		for _, c := range r.CoreCompetencies {
			core[c] = true
		}
		for _, c := range tax.Competencies {
			listed := false
			for _, rid := range c.Roles {
				if rid == r.RoleID {
					listed = true
					break
				}
			}
			if !listed {
				continue
			}
			isCore := core[c.CompetencyID]
			_, err := pool.Exec(ctx, `
				INSERT INTO role_competency (role_id, competency_id, is_core)
				VALUES ($1, $2, $3)
				ON CONFLICT (role_id, competency_id) DO UPDATE SET is_core = EXCLUDED.is_core`,
				r.RoleID, c.CompetencyID, isCore,
			)
			if err != nil {
				return fmt.Errorf("role_competency %s/%s: %w", r.RoleID, c.CompetencyID, err)
			}
		}
	}
	return nil
}

func seedGrades(ctx context.Context, pool *pgxpool.Pool, ref referenceFile) error {
	for _, g := range ref.Grades {
		minL, maxL := 1, 5
		if len(g.TypicalLevelRange) == 2 {
			minL = g.TypicalLevelRange[0]
			maxL = g.TypicalLevelRange[1]
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO grade_band (grade, label_ru, label_en, cost_coefficient, typical_level_min, typical_level_max)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (grade) DO UPDATE SET
				label_ru = EXCLUDED.label_ru,
				label_en = EXCLUDED.label_en,
				cost_coefficient = EXCLUDED.cost_coefficient,
				typical_level_min = EXCLUDED.typical_level_min,
				typical_level_max = EXCLUDED.typical_level_max`,
			g.Grade, g.LabelRU, g.LabelEN, g.CostCoefficient, minL, maxL,
		)
		if err != nil {
			return fmt.Errorf("grade %s: %w", g.Grade, err)
		}
	}
	return nil
}

func seedWeightPresets(ctx context.Context, pool *pgxpool.Pool, ref referenceFile) error {
	for _, p := range ref.WeightsPresets {
		wJSON, err := marshalWeights(p.Weights)
		if err != nil {
			return fmt.Errorf("preset %s: %w", p.Preset, err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO weight_preset (preset, label_ru, weights, sum_check)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (preset) DO UPDATE SET
				label_ru  = EXCLUDED.label_ru,
				weights   = EXCLUDED.weights,
				sum_check = EXCLUDED.sum_check`,
			p.Preset, p.LabelRU, wJSON, p.Sum,
		)
		if err != nil {
			return fmt.Errorf("preset %s: %w", p.Preset, err)
		}
	}
	return nil
}

// marshalWeights converts map to jsonb text.
func marshalWeights(w map[string]float64) (string, error) {
	b := []byte{'{'}
	first := true
	order := []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10", "M11", "M12"}
	for _, k := range order {
		v, ok := w[k]
		if !ok {
			continue
		}
		if !first {
			b = append(b, ',')
		}
		first = false
		b = append(b, '"')
		b = append(b, k...)
		b = append(b, '"', ':')
		b = append(b, []byte(fmt.Sprintf("%g", v))...)
	}
	b = append(b, '}')
	return string(b), nil
}
