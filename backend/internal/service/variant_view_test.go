package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

func TestContractDeficitsUseTaxonomyNames(t *testing.T) {
	competencyID := "lang_go"
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_t0002", BriefID: "brf_t0002", Preset: "balanced", K: 1, Seed: 42,
		Horizon: []string{"2026-W10"}, Weights: map[string]float64{"M1": 1},
		Requirements: []runinput.Requirement{{
			RequirementID: "req_n1", RoleID: "backend_engineer", CompetencyID: &competencyID,
			RoleNameRU: "Бэкенд-инженер", CompetencyNameRU: "Go", Level: 3,
			Criticality: "must", Headcount: 1,
			Effort:         runinput.Effort{MinPersonDays: 1, LikelyPersonDays: 1, MaxPersonDays: 1},
			AllowsExternal: true,
		}},
		Candidates: []runinput.Candidate{{
			EmployeeID: "emp_ab01", Grade: "M", CostCoefficient: 1.4,
			RoleProfileID: "backend_engineer", EmploymentType: "internal",
			Capacity: []runinput.CapacityRow{{
				Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: 0,
			}},
		}},
		CreatedAt: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	result := metrics.VariantResult{VariantID: "var_t1", Deficits: []metrics.Deficit{{
		RequirementID: "req_n1", RoleID: "backend_engineer", CompetencyID: &competencyID,
		PersonDays: 2, ExternalCooperationFlag: true,
	}}}
	out := contractDeficits(snap, metrics.Proposal{}, result, scheduleConfig(snap))
	require.Len(t, out, 1)
	payload := out[0].(map[string]any)
	assert.Equal(t, "Go", payload["competency_name_ru"], "the taxonomy name must replace the raw id")
	assert.NotContains(t, payload["reason_codes"], "DEFICIT_NAME_UNAVAILABLE")

	result.Deficits[0].CompetencyID = nil
	out = contractDeficits(snap, metrics.Proposal{}, result, scheduleConfig(snap))
	payload = out[0].(map[string]any)
	assert.Equal(t, "Бэкенд-инженер", payload["competency_name_ru"], "role-only deficits use the role name")
}

func TestContractDeficitsFallsBackWithReasonCode(t *testing.T) {
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_t0003", BriefID: "brf_t0003", Preset: "balanced", K: 1, Seed: 42,
		Horizon: []string{"2026-W10"}, Weights: map[string]float64{"M1": 1},
		Requirements: []runinput.Requirement{{
			RequirementID: "req_n2", RoleID: "backend_engineer", Level: 3,
			Criticality: "must", Headcount: 1,
			Effort:         runinput.Effort{MinPersonDays: 1, LikelyPersonDays: 1, MaxPersonDays: 1},
			AllowsExternal: true,
		}},
		Candidates: []runinput.Candidate{{
			EmployeeID: "emp_ab01", Grade: "M", CostCoefficient: 1.4,
			RoleProfileID: "backend_engineer", EmploymentType: "internal",
			Capacity: []runinput.CapacityRow{{
				Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: 0,
			}},
		}},
		CreatedAt: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	result := metrics.VariantResult{VariantID: "var_t2", Deficits: []metrics.Deficit{{
		RequirementID: "req_n2", RoleID: "backend_engineer", PersonDays: 2,
	}}}
	out := contractDeficits(snap, metrics.Proposal{}, result, scheduleConfig(snap))
	payload := out[0].(map[string]any)
	assert.Equal(t, "backend_engineer", payload["competency_name_ru"])
	assert.Contains(t, payload["reason_codes"], "DEFICIT_NAME_UNAVAILABLE")
}
