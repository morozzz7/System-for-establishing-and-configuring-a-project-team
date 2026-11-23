package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/runinput"
	"github.com/teamopt/backend/internal/service"
)

type mockCapacityRepo struct {
	matrix map[string][]domain.WeeklyCapacity
}

func (m *mockCapacityRepo) GetWeeklyCapacity(_ context.Context, employeeID, _, _ string) ([]domain.WeeklyCapacity, error) {
	return m.matrix[employeeID], nil
}

func (m *mockCapacityRepo) GetCapacityMatrix(_ context.Context, employeeIDs []string, _, _ string) (map[string][]domain.WeeklyCapacity, error) {
	out := make(map[string][]domain.WeeklyCapacity, len(employeeIDs))
	for _, id := range employeeIDs {
		out[id] = m.matrix[id]
	}
	return out, nil
}

func (m *mockCapacityRepo) FindCandidatesByCompetency(_ context.Context, _ string, _ int) ([]string, error) {
	return nil, nil
}

func TestPgCandidateSourceBuildsSnapshotCandidatesFromProfiles(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	asgRepo := newMockAssignmentRepo()
	capacity := &mockCapacityRepo{matrix: make(map[string][]domain.WeeklyCapacity)}
	weeks := []string{"2026-W10", "2026-W11"}

	profile := func(id, role, employment string, bench bool) domain.Employee {
		return domain.Employee{
			EmployeeID: id, Grade: "M", RoleProfileID: role, UnitID: "unt_demo01",
			EmploymentType: employment, BenchFlag: bench, CostCoefficient: 1.4, Active: true,
			CapacityProfile: domain.CapacityProfile{
				WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95,
			},
		}
	}
	require.NoError(t, empRepo.Create(context.Background(), profile("emp_qual001", "backend_engineer", "internal", false)))
	require.NoError(t, empRepo.Create(context.Background(), profile("emp_other001", "qa_engineer", "internal", false)))
	require.NoError(t, empRepo.Create(context.Background(), profile("emp_bench001", "backend_engineer", "internal", true)))
	require.NoError(t, empRepo.Create(context.Background(), profile("emp_extern01", "backend_engineer", "contractor", false)))
	inactive := profile("emp_inactiv01", "backend_engineer", "internal", false)
	inactive.Active = false
	require.NoError(t, empRepo.Create(context.Background(), inactive))

	compRepo.data["emp_qual001"] = []domain.EmployeeCompetency{{CompetencyID: "fw_react", Level: 4}}
	compRepo.data["emp_bench001"] = []domain.EmployeeCompetency{{CompetencyID: "fw_react", Level: 3}}
	compRepo.data["emp_extern01"] = []domain.EmployeeCompetency{{CompetencyID: "fw_react", Level: 5}}

	for _, id := range []string{"emp_qual001", "emp_bench001", "emp_extern01"} {
		capacity.matrix[id] = []domain.WeeklyCapacity{
			{EmployeeID: id, Week: "2026-W10", AvailableMinutes: 30 * 60, AssignedMinutes: 8 * 60, AbsentMinutes: 0, NormMinutes: 40 * 60},
			{EmployeeID: id, Week: "2026-W11", AvailableMinutes: 0, AssignedMinutes: 32 * 60, AbsentMinutes: 0, NormMinutes: 40 * 60},
		}
	}
	asgRepo.assignments = []domain.Assignment{
		{EmployeeID: "emp_qual001", ProjectID: "prj_z", Week: "2026-W10", Hours: 8},
		{EmployeeID: "emp_qual001", ProjectID: "prj_a", Week: "2026-W10", Hours: 2},
	}

	source := service.NewPgCandidateSource(empRepo, compRepo, capacity, asgRepo)
	compID := "fw_react"
	requirements := []runinput.Requirement{{RequirementID: "req_1", RoleID: "backend_engineer", CompetencyID: &compID, Level: 3}}

	candidates, err := source.Candidates(context.Background(), requirements, weeks, service.CandidateFilter{IncludeBench: false, MaxCandidates: 500})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	candidate := candidates[0]
	assert.Equal(t, "emp_qual001", candidate.EmployeeID)
	assert.Equal(t, map[string]int{"fw_react": 4}, candidate.CompetencyLevels)
	assert.Equal(t, []string{"prj_a", "prj_z"}, candidate.CurrentProjectIDs)
	require.Len(t, candidate.Capacity, 2)
	assert.Equal(t, 5, candidate.Capacity[0].WorkingDays)
	assert.Equal(t, 30.0, candidate.Capacity[0].FreeHours)
	assert.Equal(t, 8.0, candidate.Capacity[0].AssignedHours)
	assert.False(t, candidate.Capacity[0].Unavailable)
	assert.True(t, candidate.Capacity[1].Unavailable)
	assert.Equal(t, 0.0, candidate.Capacity[1].FreeHours)
}

func TestPgCandidateSourceHonorsCandidateFilter(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	asgRepo := newMockAssignmentRepo()
	capacity := &mockCapacityRepo{matrix: make(map[string][]domain.WeeklyCapacity)}
	weeks := []string{"2026-W10"}

	for _, id := range []string{"emp_qual001", "emp_bench001", "emp_extern01"} {
		employment := "internal"
		bench := false
		if id == "emp_bench001" {
			bench = true
		}
		if id == "emp_extern01" {
			employment = "contractor"
		}
		require.NoError(t, empRepo.Create(context.Background(), domain.Employee{
			EmployeeID: id, Grade: "M", RoleProfileID: "backend_engineer", UnitID: "unt_demo01",
			EmploymentType: employment, BenchFlag: bench, CostCoefficient: 1.4, Active: true,
			CapacityProfile: domain.CapacityProfile{WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95},
		}))
		compRepo.data[id] = []domain.EmployeeCompetency{{CompetencyID: "fw_react", Level: 4}}
		capacity.matrix[id] = []domain.WeeklyCapacity{{EmployeeID: id, Week: weeks[0], AvailableMinutes: 40 * 60, NormMinutes: 40 * 60}}
	}

	source := service.NewPgCandidateSource(empRepo, compRepo, capacity, asgRepo)
	compID := "fw_react"
	requirements := []runinput.Requirement{{RequirementID: "req_1", RoleID: "backend_engineer", CompetencyID: &compID, Level: 3}}

	bench, err := source.Candidates(context.Background(), requirements, weeks, service.CandidateFilter{IncludeBench: false, MaxCandidates: 500})
	require.NoError(t, err)
	require.Len(t, bench, 1)
	assert.Equal(t, "emp_qual001", bench[0].EmployeeID)

	withExternal, err := source.Candidates(context.Background(), requirements, weeks, service.CandidateFilter{IncludeBench: true, IncludeExternal: true, MaxCandidates: 500})
	require.NoError(t, err)
	assert.Len(t, withExternal, 3)

	limited, err := source.Candidates(context.Background(), requirements, weeks, service.CandidateFilter{IncludeBench: true, MaxCandidates: 2})
	require.NoError(t, err)
	assert.Len(t, limited, 2)
}
