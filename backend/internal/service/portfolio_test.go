package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/runinput"
)

type portfolioFakeProjectRepo struct {
	projects []domain.Project
	err      error
	filters  []domain.ProjectFilter
}

func (f *portfolioFakeProjectRepo) List(_ context.Context, filter domain.ProjectFilter) ([]domain.Project, int, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]domain.Project, 0, len(f.projects))
	for _, p := range f.projects {
		if filter.Status == "" || p.Status == filter.Status {
			out = append(out, p)
		}
	}
	return out, len(out), nil
}

func (f *portfolioFakeProjectRepo) Get(context.Context, string) (domain.Project, error) {
	return domain.Project{}, nil
}
func (f *portfolioFakeProjectRepo) Create(context.Context, domain.Project) error { return nil }
func (f *portfolioFakeProjectRepo) Update(context.Context, domain.Project) error { return nil }
func (f *portfolioFakeProjectRepo) Delete(context.Context, string) error          { return nil }

type portfolioFakeAssignmentRepo struct {
	rows    []domain.Assignment
	err     error
	filters []domain.AssignmentFilter
}

func (f *portfolioFakeAssignmentRepo) ListByFilter(_ context.Context, filter domain.AssignmentFilter) ([]domain.Assignment, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, f.err
	}
	out := make([]domain.Assignment, 0, len(f.rows))
	for _, a := range f.rows {
		if filter.ProjectID != "" && a.ProjectID != filter.ProjectID {
			continue
		}
		if filter.FromWeek != "" && a.Week < filter.FromWeek {
			continue
		}
		if filter.ToWeek != "" && a.Week > filter.ToWeek {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *portfolioFakeAssignmentRepo) ListByProject(_ context.Context, projectID, fromWeek, toWeek string) ([]domain.Assignment, error) {
	return f.ListByFilter(context.Background(), domain.AssignmentFilter{
		ProjectID: projectID, FromWeek: fromWeek, ToWeek: toWeek,
	})
}
func (f *portfolioFakeAssignmentRepo) Create(context.Context, domain.Assignment) error { return nil }
func (f *portfolioFakeAssignmentRepo) CreateBatch(context.Context, []domain.Assignment) error {
	return nil
}
func (f *portfolioFakeAssignmentRepo) TotalAssignedHours(context.Context, string, string) (float64, error) {
	return 0, nil
}

// Absence queries are unused: the capacity matrix already accounts for approved absences.
type portfolioFakeAbsenceRepo struct{ repository.AbsenceRepo }

type portfolioCapacityCall struct {
	employees  []string
	start, end string
}

type portfolioFakeCapacityRepo struct {
	matrix map[string][]domain.WeeklyCapacity
	err    error
	calls  []portfolioCapacityCall
}

func (f *portfolioFakeCapacityRepo) GetCapacityMatrix(_ context.Context, employeeIDs []string, startWeek, endWeek string) (map[string][]domain.WeeklyCapacity, error) {
	f.calls = append(f.calls, portfolioCapacityCall{
		employees: append([]string(nil), employeeIDs...),
		start:     startWeek,
		end:       endWeek,
	})
	if f.err != nil {
		return nil, f.err
	}
	if f.matrix == nil {
		return map[string][]domain.WeeklyCapacity{}, nil
	}
	return f.matrix, nil
}

func (f *portfolioFakeCapacityRepo) GetWeeklyCapacity(context.Context, string, string, string) ([]domain.WeeklyCapacity, error) {
	return nil, nil
}
func (f *portfolioFakeCapacityRepo) FindCandidatesByCompetency(context.Context, string, int) ([]string, error) {
	return nil, nil
}

func portfolioWeeks() []domain.WeeklyCapacity {
	rows := make([]domain.WeeklyCapacity, 0, 12)
	for _, employee := range []string{"emp_a", "emp_b", "emp_c"} {
		minutes := map[string]int{"emp_a": 600, "emp_b": 300, "emp_c": 120}[employee]
		for week := 1; week <= 4; week++ {
			rows = append(rows, domain.WeeklyCapacity{
				EmployeeID:       employee,
				Week:             domain.FormatWeek(2026, week),
				AvailableMinutes: minutes,
				NormMinutes:      2400,
			})
		}
	}
	return rows
}

func portfolioSourceFixture() (*PgPortfolioSource, *portfolioFakeProjectRepo, *portfolioFakeAssignmentRepo, *portfolioFakeCapacityRepo) {
	projects := &portfolioFakeProjectRepo{projects: []domain.Project{
		{ProjectID: "prj_bravo", Status: "active", Criticality: "high"},
		{ProjectID: "prj_idle", Status: "active", Criticality: "critical"},
		{ProjectID: "prj_paused", Status: "paused", Criticality: "critical"},
		{ProjectID: "prj_alpha", Status: "active", Criticality: "critical"},
		{ProjectID: "prj_low", Status: "active", Criticality: "normal"},
	}}
	assignments := &portfolioFakeAssignmentRepo{rows: []domain.Assignment{
		{ProjectID: "prj_alpha", EmployeeID: "emp_b", Week: "2026-W01", Hours: 6, Source: "plan"},
		{ProjectID: "prj_alpha", EmployeeID: "emp_a", Week: "2026-W01", Hours: 4, Source: "plan"},
		{ProjectID: "prj_alpha", EmployeeID: "emp_a", Week: "2026-W02", Hours: 4, Source: "plan"},
		{ProjectID: "prj_alpha", EmployeeID: "emp_a", Week: "2026-W05", Hours: 10, Source: "plan"},
		{ProjectID: "prj_bravo", EmployeeID: "emp_b", Week: "2026-W03", Hours: 8, Source: "plan"},
		{ProjectID: "prj_low", EmployeeID: "emp_c", Week: "2026-W04", Hours: 2, Source: "plan"},
	}}
	matrix := map[string][]domain.WeeklyCapacity{}
	for _, row := range portfolioWeeks() {
		matrix[row.EmployeeID] = append(matrix[row.EmployeeID], row)
	}
	capacity := &portfolioFakeCapacityRepo{matrix: matrix}
	source := &PgPortfolioSource{
		Projects:    projects,
		Assignments: assignments,
		Absences:    &portfolioFakeAbsenceRepo{},
		Capacity:    capacity,
	}
	return source, projects, assignments, capacity
}

func TestPgPortfolioSourcePortfolioDerivesValuesAndOrder(t *testing.T) {
	source, projects, assignments, capacity := portfolioSourceFixture()

	first, err := source.Portfolio(context.Background(), "2026-W01", "2026-W04")
	require.NoError(t, err)
	otherSource, _, _, _ := portfolioSourceFixture()
	second, err := otherSource.Portfolio(context.Background(), "2026-W01", "2026-W04")
	require.NoError(t, err)
	assert.Equal(t, first, second, "portfolio must be deterministic across calls")

	require.Equal(t, []domain.ProjectFilter{{Status: "active"}}, projects.filters)
	require.Len(t, assignments.filters, 4, "one assignment query per active project")
	require.Len(t, capacity.calls, 3, "no capacity query for a project without assignments")
	for _, call := range capacity.calls {
		assert.Equal(t, "2026-W01", call.start)
		assert.Equal(t, "2026-W04", call.end)
	}
	assert.Equal(t, []string{"emp_a", "emp_b"}, capacity.calls[0].employees, "distinct employee ids in sorted order")
	assert.Equal(t, []string{"emp_b"}, capacity.calls[1].employees)

	require.Equal(t, []runinput.PortfolioProject{
		{ProjectID: "prj_alpha", RemainingHours: 14, WeeklyCapacity: 15, Critical: true, EmployeeIDs: []string{"emp_a", "emp_b"}},
		{ProjectID: "prj_bravo", RemainingHours: 8, WeeklyCapacity: 5, Critical: true, EmployeeIDs: []string{"emp_b"}},
		{ProjectID: "prj_low", RemainingHours: 2, WeeklyCapacity: 2, Critical: false, EmployeeIDs: []string{"emp_c"}},
	}, first)
}

func TestPgPortfolioSourcePortfolioSkipsProjectsWithoutAssignments(t *testing.T) {
	projects := &portfolioFakeProjectRepo{projects: []domain.Project{
		{ProjectID: "prj_idle", Status: "active", Criticality: "critical"},
	}}
	source := &PgPortfolioSource{
		Projects:    projects,
		Assignments: &portfolioFakeAssignmentRepo{},
		Absences:    &portfolioFakeAbsenceRepo{},
		Capacity:    &portfolioFakeCapacityRepo{},
	}

	out, err := source.Portfolio(context.Background(), "2026-W01", "2026-W04")
	require.NoError(t, err)
	require.NotNil(t, out, "callers receive an empty slice, never nil")
	assert.Empty(t, out)
	assert.Equal(t, domain.ProjectFilter{Status: "active"}, projects.filters[0])
}

func TestPgPortfolioSourcePortfolioPropagatesErrors(t *testing.T) {
	sourceWithErrors := func(t *testing.T, kind string) *PgPortfolioSource {
		t.Helper()
		source, projects, assignments, capacity := portfolioSourceFixture()
		sentinel := errors.New("repository failure")
		switch kind {
		case "projects":
			projects.err = sentinel
		case "assignments":
			assignments.err = sentinel
		case "capacity":
			capacity.err = sentinel
		}
		return source
	}

	for _, kind := range []string{"projects", "assignments", "capacity"} {
		t.Run(kind, func(t *testing.T) {
			out, err := sourceWithErrors(t, kind).Portfolio(context.Background(), "2026-W01", "2026-W04")
			require.Error(t, err)
			assert.ErrorContains(t, err, "repository failure")
			assert.Nil(t, out)
		})
	}

	for _, horizon := range [][2]string{
		{"bad", "2026-W04"},
		{"2026-W01", "bad"},
		{"2026-W05", "2026-W01"},
	} {
		t.Run("horizon_"+horizon[0]+"_"+horizon[1], func(t *testing.T) {
			source, _, _, _ := portfolioSourceFixture()
			out, err := source.Portfolio(context.Background(), horizon[0], horizon[1])
			require.Error(t, err)
			assert.Nil(t, out)
		})
	}
}

type portfolioFakeSource struct{}

func (portfolioFakeSource) Portfolio(context.Context, string, string) ([]runinput.PortfolioProject, error) {
	return []runinput.PortfolioProject{}, nil
}

func TestWorkflowServicePortfolioSource(t *testing.T) {
	first := NewWorkflowService(nil)
	second := NewWorkflowService(nil)
	require.Nil(t, first.portfolioSource())
	require.Nil(t, second.portfolioSource())

	source := portfolioFakeSource{}
	first.SetPortfolioSource(source)
	require.Equal(t, source, first.portfolioSource())
	require.Nil(t, second.portfolioSource(), "sources are isolated per service")

	first.SetPortfolioSource(nil)
	require.Nil(t, first.portfolioSource())
}

func TestWorkflowServicePortfolioForRun(t *testing.T) {
	workflow := NewWorkflowService(nil)
	out, err := workflow.portfolioForRun(context.Background(), "2026-W01", "2026-W04")
	require.NoError(t, err)
	assert.Equal(t, []runinput.PortfolioProject{}, out)

	source, _, _, _ := portfolioSourceFixture()
	workflow.SetPortfolioSource(source)
	out, err = workflow.portfolioForRun(context.Background(), "2026-W01", "2026-W04")
	require.NoError(t, err)
	assert.Len(t, out, 3)
}
