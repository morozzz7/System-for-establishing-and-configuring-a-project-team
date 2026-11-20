package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

// TestEmployeeImportHandler_Upsert tests the PUT /employees endpoint with upsert mode.
func TestEmployeeImportHandler_Upsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	empSvc := &service.EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}
	prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
	asgSvc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: audRepo, CapacitySvc: capSvc,
	}
	absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	router := NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)

	// Create a test project for assignments.
	ctx := context.Background()
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
	require.NoError(t, prjRepo.Create(ctx, prj))

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

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_test1")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	require.Equal(t, "req_test1", resp["request_id"])
	require.Equal(t, "upsert", resp["mode"])
	require.Equal(t, float64(1), resp["totals"].(map[string]interface{})["created"])
	require.Equal(t, "created", resp["employees"].([]interface{})[0].(map[string]interface{})["status"])
}

// TestEmployeeImportHandler_Replace tests the PUT /employees endpoint with replace mode.
func TestEmployeeImportHandler_Replace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	empSvc := &service.EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}
	prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
	asgSvc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: audRepo, CapacitySvc: capSvc,
	}
	absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	router := NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)

	ctx := context.Background()

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

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_replace1")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	require.Equal(t, "replace", resp["mode"])
	require.Equal(t, float64(0), resp["totals"].(map[string]interface{})["created"])
	require.Equal(t, float64(1), resp["totals"].(map[string]interface{})["unchanged"])

	// emp_exist2 should be deleted.
	_, err := empRepo.Get(ctx, "emp_exist2")
	require.ErrorIs(t, err, repository.ErrNotFound)
}

// TestEmployeeImportHandler_ValidateOnly tests the validate_only mode.
func TestEmployeeImportHandler_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	empSvc := &service.EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}
	prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
	asgSvc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: audRepo, CapacitySvc: capSvc,
	}
	absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	router := NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)

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

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_valonly1")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	require.Equal(t, float64(0), resp["totals"].(map[string]interface{})["created"])
	require.Equal(t, float64(0), resp["totals"].(map[string]interface{})["updated"])
	require.Equal(t, float64(0), resp["totals"].(map[string]interface{})["errors"])

	// Employee should not exist in DB.
	ctx := context.Background()
	_, err := empRepo.Get(ctx, "emp_val1")
	require.ErrorIs(t, err, repository.ErrNotFound)
}

// TestEmployeeImportHandler_ValidationErrors tests validation error responses.
func TestEmployeeImportHandler_ValidationErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	empSvc := &service.EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}
	prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
	asgSvc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: audRepo, CapacitySvc: capSvc,
	}
	absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	router := NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)

	// Invalid payload: duplicate employee_id in request, invalid grade.
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

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_val1")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code) // Validation errors are in response body, not HTTP error

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	require.Equal(t, float64(2), resp["totals"].(map[string]interface{})["errors"])
	require.Len(t, resp["validation_errors"].([]interface{}), 2)
}

// TestEmployeeImportHandler_Idempotency tests that importing the same payload twice results in 0 created on second call.
func TestEmployeeImportHandler_Idempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	empSvc := &service.EmployeeService{
		EmployeeRepo: empRepo,
		CompRepo:     compRepo,
		AuditRepo:    audRepo,
		AsgRepo:      asgRepo,
		AbsRepo:      absRepo,
		ProjectRepo:  prjRepo,
	}
	prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
	asgSvc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: audRepo, CapacitySvc: capSvc,
	}
	absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	router := NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)

	ctx := context.Background()

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
	require.NoError(t, prjRepo.Create(ctx, prj))

	payload := domain.EmployeeImportRequest{
		Mode: "upsert",
		Employees: []domain.EmployeeImportItem{
			{
				EmployeeID:     "emp_idem1",
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

	// First request.
	body, _ := json.Marshal(payload)
	req1 := httptest.NewRequest(http.MethodPut, "/api/v1/employees", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Request-Id", "req_idem1")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	require.Equal(t, http.StatusOK, rr1.Code)

	var resp1 map[string]interface{}
	json.Unmarshal(rr1.Body.Bytes(), &resp1)
	require.Equal(t, float64(1), resp1["totals"].(map[string]interface{})["created"])

	// Second request with same payload.
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/employees", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Request-Id", "req_idem2")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	require.Equal(t, http.StatusOK, rr2.Code)

	var resp2 map[string]interface{}
	json.Unmarshal(rr2.Body.Bytes(), &resp2)
	require.Equal(t, float64(0), resp2["totals"].(map[string]interface{})["created"])
	require.Equal(t, float64(1), resp2["totals"].(map[string]interface{})["unchanged"])
	require.Equal(t, "unchanged", resp2["employees"].([]interface{})[0].(map[string]interface{})["status"])
}

// Helper functions.

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