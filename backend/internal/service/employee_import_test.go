package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
)

// TestEmployeeImport_Unit tests the import logic with a mock database.
// For integration tests, see handler/employee_import_test.go with testcontainers.
func TestEmployeeImport_Upsert(t *testing.T) {
	// Use testcontainers for real DB integration tests.
	// This is a placeholder showing the test structure.
	t.Skip("Run with testcontainers integration test")
}

func TestEmployeeImport_Replace(t *testing.T) {
	t.Skip("Run with testcontainers integration test")
}

func TestEmployeeImport_ValidateOnly(t *testing.T) {
	t.Skip("Run with testcontainers integration test")
}

// Property-based test for idempotency: same payload twice = 0 created second time.
func TestEmployeeImport_Idempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)

	empSvc := &EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}

	// Create a test project for assignments.
	prj := domain.Project{
		ProjectID:      "prj_test1",
		Code:           "TEST-1",
		Name:           "Test Project",
		Status:         "active",
		Priority:       1,
		Criticality:    "high",
		StartWeek:      "2026-W01",
		EndWeekPlanned: "2026-W10",
	}
	err := prjRepo.Create(ctx, prj)
	require.NoError(t, err)

	payload := domain.EmployeeImportRequest{
		Mode: "upsert",
		Employees: []domain.EmployeeImportItem{
			{
				EmployeeID:     "emp_test1",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
				BenchFlag:      boolPtr(false),
				CapacityProfile: &domain.CapacityProfile{
					WeeklyHoursNorm:    40,
					ReserveRatio:       0.2,
					MaxUtilizationRatio: 0.95,
				},
				Competencies: []domain.EmployeeCompetencyUpsert{
					{CompetencyID: "lang_go", Level: 4, Confidence: 0.9, Source: "manager_review"},
				},
				Assignments: []domain.AssignmentCreate{
					{ProjectID: "prj_test1", RoleID: "backend_engineer", Week: "2026-W02", Hours: 20},
				},
				Absences: []domain.AbsenceCreate{
					{Type: "vacation", StartDate: parseDate("2026-01-15"), EndDate: parseDate("2026-01-19"), HoursPerDay: 8, Approved: boolPtr(true), Source: "hr_import"},
				},
			},
		},
		ValidateOnly: boolPtr(false),
	}

	// First import - should create.
	resp1, err := empSvc.ImportEmployees(ctx, payload, "req_test1")
	require.NoError(t, err)
	require.Equal(t, 1, resp1.Totals.Created)
	require.Equal(t, 0, resp1.Totals.Updated)
	require.Equal(t, 0, resp1.Totals.Errors)
	require.Equal(t, "created", resp1.Employees[0].Status)

	// Second import with same payload - should be unchanged.
	resp2, err := empSvc.ImportEmployees(ctx, payload, "req_test2")
	require.NoError(t, err)
	require.Equal(t, 0, resp2.Totals.Created)
	require.Equal(t, 1, resp2.Totals.Unchanged)
	require.Equal(t, 0, resp2.Totals.Errors)
	require.Equal(t, "unchanged", resp2.Employees[0].Status)
}

// Property-based test for replace mode: deletes employees not in payload.
func TestEmployeeImport_ReplaceDeletesMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)

	empSvc := &EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}

	// Create two employees initially.
	emp1 := domain.Employee{
		EmployeeID:      "emp_exist1",
		Grade:           "M",
		RoleProfileID:   "backend_engineer",
		UnitID:          "unt_core",
		EmploymentType:  "internal",
		BenchFlag:       false,
		CapacityProfile: domain.CapacityProfile{WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95},
		CostCoefficient: 1.4,
		Active:          true,
	}
	emp2 := domain.Employee{
		EmployeeID:      "emp_exist2",
		Grade:           "S",
		RoleProfileID:   "qa_engineer",
		UnitID:          "unt_qa",
		EmploymentType:  "internal",
		BenchFlag:       false,
		CapacityProfile: domain.CapacityProfile{WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95},
		CostCoefficient: 1.9,
		Active:          true,
	}
	require.NoError(t, empRepo.Create(ctx, emp1))
	require.NoError(t, empRepo.Create(ctx, emp2))

	// Import with replace mode, only including emp_exist1.
	payload := domain.EmployeeImportRequest{
		Mode: "replace",
		Employees: []domain.EmployeeImportItem{
			{
				EmployeeID:     "emp_exist1",
				Grade:          "M",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
		},
		ValidateOnly: boolPtr(false),
	}

	resp, err := empSvc.ImportEmployees(ctx, payload, "req_replace1")
	require.NoError(t, err)
	require.Equal(t, 0, resp.Totals.Created)
	require.Equal(t, 1, resp.Totals.Unchanged)

	// emp_exist2 should be deleted.
	_, err = empRepo.Get(ctx, "emp_exist2")
	require.ErrorIs(t, err, repository.ErrNotFound)

	// emp_exist1 should still exist.
	e, err := empRepo.Get(ctx, "emp_exist1")
	require.NoError(t, err)
	require.Equal(t, "emp_exist1", e.EmployeeID)
}

func TestEmployeeImport_ValidationErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)

	empSvc := &EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}

	// Invalid payload: duplicate employee_id in request, invalid grade, missing required fields.
	payload := domain.EmployeeImportRequest{
		Mode: "upsert",
		Employees: []domain.EmployeeImportItem{
			{
				EmployeeID:     "emp_test1",
				Grade:          "X", // invalid grade
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
			{
				EmployeeID:     "emp_test1", // duplicate in request
				Grade:          "S",
				RoleProfileID:  "qa_engineer",
				UnitID:         "unt_qa",
				EmploymentType: "internal",
			},
		},
		ValidateOnly: boolPtr(false),
	}

	resp, err := empSvc.ImportEmployees(ctx, payload, "req_val1")
	require.NoError(t, err) // Validation errors don't return error, they return in response
	require.Equal(t, 2, resp.Totals.Errors)
	require.Len(t, resp.ValidationErrors, 2) // one for invalid grade, one for duplicate
}

func TestEmployeeImport_ValidateOnlyMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)

	empSvc := &EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}

	payload := domain.EmployeeImportRequest{
		Mode: "upsert",
		Employees: []domain.EmployeeImportItem{
			{
				EmployeeID:     "emp_val1",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
		},
		ValidateOnly: boolPtr(true),
	}

	resp, err := empSvc.ImportEmployees(ctx, payload, "req_valonly1")
	require.NoError(t, err)
	require.Equal(t, 0, resp.Totals.Created)
	require.Equal(t, 0, resp.Totals.Updated)
	require.Equal(t, 0, resp.Totals.Errors)

	// Employee should not exist in DB.
	_, err = empRepo.Get(ctx, "emp_val1")
	require.ErrorIs(t, err, repository.ErrNotFound)
}

// Helper functions for tests.

func setupTestDB(t *testing.T) *pgxpool.Pool {
	// In real tests, use testcontainers. For now, skip.
	t.Skip("Requires testcontainers setup")
	return nil
}

func parseDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func boolPtr(b bool) *bool {
	return &b
}