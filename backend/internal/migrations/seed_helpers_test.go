package migrations_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Inline seed logic mirroring backend/cmd/seed/main.go.
// This avoids cross-package main invocation in tests.

type taxFile struct {
	Levels []struct {
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

type refFile struct {
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

func runSeedInline(t *testing.T, db *sql.DB, repoRoot string) {
	t.Helper()

	taxBytes, err := os.ReadFile(filepath.Join(repoRoot, "contracts", "taxonomy.yaml"))
	require.NoError(t, err)
	refBytes, err := os.ReadFile(filepath.Join(repoRoot, "contracts", "reference.yaml"))
	require.NoError(t, err)

	var tax taxFile
	require.NoError(t, yaml.Unmarshal(taxBytes, &tax))
	var ref refFile
	require.NoError(t, yaml.Unmarshal(refBytes, &ref))

	// Levels.
	for _, l := range tax.Levels {
		_, err := db.Exec(`
			INSERT INTO level_descriptor (level, label_ru, label_en, description_ru, description_en)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (level) DO NOTHING`,
			l.Level, l.LabelRU, l.LabelEN, l.DescriptionRU, l.DescriptionEN)
		require.NoError(t, err)
	}

	// Roles.
	for _, r := range tax.Roles {
		_, err := db.Exec(`
			INSERT INTO role (role_id, name_en, name_ru, family, synonyms, core_competencies, typical_grades)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (role_id) DO UPDATE SET
				name_en=EXCLUDED.name_en, name_ru=EXCLUDED.name_ru, family=EXCLUDED.family,
				synonyms=EXCLUDED.synonyms, core_competencies=EXCLUDED.core_competencies,
				typical_grades=EXCLUDED.typical_grades, updated_at=now()`,
			r.RoleID, r.NameEN, r.NameRU, r.Family, r.Synonyms, r.CoreCompetencies, r.TypicalGrades)
		require.NoError(t, err)
	}

	// Competencies + synonyms.
	for _, c := range tax.Competencies {
		_, err := db.Exec(`
			INSERT INTO competency (competency_id, name_en, name_ru, category)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (competency_id) DO UPDATE SET
				name_en=EXCLUDED.name_en, name_ru=EXCLUDED.name_ru,
				category=EXCLUDED.category, updated_at=now()`,
			c.CompetencyID, c.NameEN, c.NameRU, c.Category)
		require.NoError(t, err)
		for _, s := range c.Synonyms {
			_, err := db.Exec(`
				INSERT INTO competency_synonym (competency_id, synonym)
				VALUES ($1, $2) ON CONFLICT (competency_id, synonym) DO NOTHING`,
				c.CompetencyID, s)
			require.NoError(t, err)
		}
	}

	// Role-competency links.
	for _, r := range tax.Roles {
		coreSet := map[string]bool{}
		for _, c := range r.CoreCompetencies {
			coreSet[c] = true
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
			_, err := db.Exec(`
				INSERT INTO role_competency (role_id, competency_id, is_core)
				VALUES ($1, $2, $3)
				ON CONFLICT (role_id, competency_id) DO UPDATE SET is_core=EXCLUDED.is_core`,
				r.RoleID, c.CompetencyID, coreSet[c.CompetencyID])
			require.NoError(t, err)
		}
	}

	// Grades.
	for _, g := range ref.Grades {
		minL, maxL := 1, 5
		if len(g.TypicalLevelRange) == 2 {
			minL = g.TypicalLevelRange[0]
			maxL = g.TypicalLevelRange[1]
		}
		_, err := db.Exec(`
			INSERT INTO grade_band (grade, label_ru, label_en, cost_coefficient, typical_level_min, typical_level_max)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (grade) DO UPDATE SET
				label_ru=EXCLUDED.label_ru, label_en=EXCLUDED.label_en,
				cost_coefficient=EXCLUDED.cost_coefficient,
				typical_level_min=EXCLUDED.typical_level_min,
				typical_level_max=EXCLUDED.typical_level_max`,
			g.Grade, g.LabelRU, g.LabelEN, g.CostCoefficient, minL, maxL)
		require.NoError(t, err)
	}

	// Weight presets.
	for _, p := range ref.WeightsPresets {
		wJSON, err := marshalWeightsJSON(p.Weights)
		require.NoError(t, err)
		_, err = db.Exec(`
			INSERT INTO weight_preset (preset, label_ru, weights, sum_check)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (preset) DO UPDATE SET
				label_ru=EXCLUDED.label_ru, weights=EXCLUDED.weights,
				sum_check=EXCLUDED.sum_check`,
			p.Preset, p.LabelRU, wJSON, p.Sum)
		require.NoError(t, err)
	}
}

func marshalWeightsJSON(w map[string]float64) (string, error) {
	b, err := json.Marshal(w)
	return string(b), err
}
