package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/handler"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

type intTestEnv struct {
	ts   *httptest.Server
	pool *pgxpool.Pool
	t    *testing.T
}

func setupIntTest(t *testing.T) *intTestEnv {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "teamopt_test",
			"POSTGRES_USER":     "teamopt",
			"POSTGRES_PASSWORD": "teamopt_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://teamopt:teamopt_test@%s:%s/teamopt_test?sslmode=disable", host, port.Port())

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Run migrations using the migration files embedded in the repo.
	err = runMigrations(ctx, t, pool)
	require.NoError(t, err)

	// Seed reference data (minimal for CRUD tests).
	err = seedReferenceData(ctx, pool)
	require.NoError(t, err)

	// Wire up services.
	empRepo := repository.NewPgEmployeeRepo(pool)
	compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
	prjRepo := repository.NewPgProjectRepo(pool)
	asgRepo := repository.NewPgAssignmentRepo(pool)
	absRepo := repository.NewPgAbsenceRepo(pool)
	audRepo := repository.NewPgAuditRepo(pool)
	capRepo := repository.NewPgCapacityRepo(pool)

	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)

	empSvc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}
	prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
	asgSvc := &service.AssignmentService{
		AssignmentRepo: asgRepo,
		EmployeeRepo:   empRepo,
		AbsenceRepo:    absRepo,
		ProjectRepo:    prjRepo,
		AuditRepo:      audRepo,
		CapacitySvc:    capSvc,
	}
	absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	crudRouter := handler.NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)
	r.Mount("/api/v1", crudRouter)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	return &intTestEnv{ts: ts, pool: pool, t: t}
}

func (e *intTestEnv) url(path string) string {
	return e.ts.URL + "/api/v1" + path
}

func (e *intTestEnv) doPost(path string, body interface{}) *http.Response {
	e.t.Helper()
	b, err := json.Marshal(body)
	require.NoError(e.t, err)
	resp, err := http.Post(e.url(path), "application/json", bytes.NewReader(b))
	require.NoError(e.t, err)
	return resp
}

func (e *intTestEnv) doGet(path string) *http.Response {
	e.t.Helper()
	resp, err := http.Get(e.url(path))
	require.NoError(e.t, err)
	return resp
}

func (e *intTestEnv) doPatch(path string, body interface{}) *http.Response {
	e.t.Helper()
	b, err := json.Marshal(body)
	require.NoError(e.t, err)
	req, err := http.NewRequest(http.MethodPatch, e.url(path), bytes.NewReader(b))
	require.NoError(e.t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	return resp
}

func (e *intTestEnv) doDelete(path string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.url(path), nil)
	require.NoError(e.t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, v))
}

// ---------- Integration Tests ----------

func TestIntegration_EmployeeCRUD(t *testing.T) {
	env := setupIntTest(t)

	// 1. Create employee.
	emp := domain.EmployeeCreate{
		EmployeeID:     "emp_test0001",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	}
	resp := env.doPost("/employees", emp)
	assert.Equal(t, 201, resp.StatusCode)

	var created domain.Employee
	decodeJSON(t, resp, &created)
	assert.Equal(t, "emp_test0001", created.EmployeeID)
	assert.Equal(t, "S", created.Grade)
	assert.True(t, created.Active)

	// 2. Get employee.
	resp = env.doGet("/employees/emp_test0001")
	assert.Equal(t, 200, resp.StatusCode)
	var got domain.Employee
	decodeJSON(t, resp, &got)
	assert.Equal(t, "emp_test0001", got.EmployeeID)

	// 3. List employees.
	resp = env.doGet("/employees?limit=10&offset=0")
	assert.Equal(t, 200, resp.StatusCode)
	var page map[string]interface{}
	decodeJSON(t, resp, &page)
	assert.NotNil(t, page["items"])
	assert.NotNil(t, page["meta"])

	// 4. Update employee (PATCH).
	resp = env.doPatch("/employees/emp_test0001", map[string]interface{}{
		"grade":     "L",
		"bench_flag": true,
	})
	assert.Equal(t, 200, resp.StatusCode)
	var updated domain.Employee
	decodeJSON(t, resp, &updated)
	assert.Equal(t, "L", updated.Grade)
	assert.True(t, updated.BenchFlag)

	// 5. Duplicate create -> 409.
	resp = env.doPost("/employees", emp)
	assert.Equal(t, 409, resp.StatusCode)
	var problem domain.Problem
	decodeJSON(t, resp, &problem)
	assert.Equal(t, 409, problem.Status)
	assert.Contains(t, problem.ReasonCodes, "DUPLICATE_EMPLOYEE_ID")

	// 6. Get non-existent -> 404.
	resp = env.doGet("/employees/emp_nonexist")
	assert.Equal(t, 404, resp.StatusCode)
}

func TestIntegration_EmployeeValidation(t *testing.T) {
	env := setupIntTest(t)

	// Invalid grade.
	resp := env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID:     "emp_test0002",
		Grade:          "X",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	})
	assert.Equal(t, 422, resp.StatusCode)

	// Invalid employee_id pattern.
	resp = env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID:     "bad",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	})
	assert.Equal(t, 422, resp.StatusCode)
}

func TestIntegration_CompetencyCRUD(t *testing.T) {
	env := setupIntTest(t)

	// Create employee first.
	env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID:     "emp_comp0001",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	})

	// Add competencies (PUT replaces all).
	comps := []domain.EmployeeCompetencyUpsert{
		{CompetencyID: "lang_go", Level: 4, Confidence: 0.9, Source: "manager_review"},
		{CompetencyID: "db_postgresql", Level: 3, Confidence: 0.8, Source: "project_history"},
	}
	b, _ := json.Marshal(comps)
	req, _ := http.NewRequest(http.MethodPut, env.url("/employees/emp_comp0001/competencies"), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var stored []domain.EmployeeCompetency
	decodeJSON(t, resp, &stored)
	assert.Len(t, stored, 2)

	// List competencies.
	resp = env.doGet("/employees/emp_comp0001/competencies")
	assert.Equal(t, 200, resp.StatusCode)
	decodeJSON(t, resp, &stored)
	assert.Len(t, stored, 2)
}

func TestIntegration_ProjectCRUD(t *testing.T) {
	env := setupIntTest(t)

	// Create project.
	prj := domain.ProjectCreate{
		ProjectID:      "prj_test0001",
		Code:           "CRM-2026",
		Name:           "CRM rework",
		Status:         "active",
		Priority:       2,
		Criticality:    "high",
		StartWeek:      "2025-W40",
		EndWeekPlanned: "2026-W20",
	}
	resp := env.doPost("/projects", prj)
	assert.Equal(t, 201, resp.StatusCode)
	var created domain.Project
	decodeJSON(t, resp, &created)
	assert.Equal(t, "prj_test0001", created.ProjectID)

	// Get project.
	resp = env.doGet("/projects/prj_test0001")
	assert.Equal(t, 200, resp.StatusCode)

	// List projects.
	resp = env.doGet("/projects")
	assert.Equal(t, 200, resp.StatusCode)
}

func TestIntegration_AssignmentWithG1G2(t *testing.T) {
	env := setupIntTest(t)

	// Create employee.
	env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID:     "emp_asg00001",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	})

	// Create project.
	env.doPost("/projects", domain.ProjectCreate{
		ProjectID:      "prj_asg00001",
		Code:           "PROJ-1",
		Name:           "Test project",
		Status:         "active",
		Priority:       1,
		Criticality:    "high",
		StartWeek:      "2026-W01",
		EndWeekPlanned: "2026-W20",
	})

	// Create absence (vacation in W05).
	env.doPost("/absences", domain.AbsenceCreate{
		EmployeeID:  "emp_asg00001",
		Type:        "vacation",
		StartDate:   mustParseDate("2026-01-26"),
		EndDate:     mustParseDate("2026-02-06"),
		HoursPerDay: 8,
		Approved:    boolPtr(true),
	})

	// G1 violation: try to assign during absence (W05 = Jan 26 - Feb 1).
	batch := domain.AssignmentCreateBatch{
		Assignments: []domain.AssignmentCreate{
			{EmployeeID: "emp_asg00001", RoleID: "backend_engineer", Week: "2026-W05", Hours: 20},
		},
	}
	resp := env.doPost("/projects/prj_asg00001/assignments", batch)
	assert.Equal(t, 409, resp.StatusCode)
	var problem domain.Problem
	decodeJSON(t, resp, &problem)
	assert.Contains(t, problem.ReasonCodes, "G1_UNAVAILABLE_ASSIGNED")

	// G2 check: assign within capacity (norm=40, reserve=0.2, max=32h).
	batch2 := domain.AssignmentCreateBatch{
		Assignments: []domain.AssignmentCreate{
			{EmployeeID: "emp_asg00001", RoleID: "backend_engineer", Week: "2026-W03", Hours: 30},
		},
	}
	resp = env.doPost("/projects/prj_asg00001/assignments", batch2)
	assert.Equal(t, 201, resp.StatusCode)

	// G2 violation: assign again, pushing over 32h.
	batch3 := domain.AssignmentCreateBatch{
		Assignments: []domain.AssignmentCreate{
			{EmployeeID: "emp_asg00001", RoleID: "backend_engineer", Week: "2026-W03", Hours: 10},
		},
	}
	resp = env.doPost("/projects/prj_asg00001/assignments", batch3)
	// This would go to a different project so it won't conflict on UNIQUE, but should fail G2.
	// Actually, 30 + 10 = 40 > 32 = 40*(1-0.2), so it should be rejected.
	assert.Equal(t, 409, resp.StatusCode)
	decodeJSON(t, resp, &problem)
	assert.Contains(t, problem.ReasonCodes, "G2_OVERLOAD")
}

func TestIntegration_AbsenceCRUD(t *testing.T) {
	env := setupIntTest(t)

	// Create employee.
	env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID:     "emp_abs00001",
		Grade:          "M",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	})

	// Create absence.
	resp := env.doPost("/absences", domain.AbsenceCreate{
		EmployeeID:  "emp_abs00001",
		Type:        "training",
		StartDate:   mustParseDate("2026-03-01"),
		EndDate:     mustParseDate("2026-03-03"),
		HoursPerDay: 8,
		Approved:    boolPtr(true),
	})
	assert.Equal(t, 201, resp.StatusCode)
	var absence domain.Absence
	decodeJSON(t, resp, &absence)
	assert.Regexp(t, `^abs_[a-z0-9]{4,12}$`, absence.AbsenceID)

	// List absences.
	resp = env.doGet("/absences?employee_id=emp_abs00001")
	assert.Equal(t, 200, resp.StatusCode)
	var page map[string]interface{}
	decodeJSON(t, resp, &page)
	assert.NotNil(t, page["items"])

	// Delete absence.
	resp = env.doDelete("/absences/" + absence.AbsenceID)
	assert.Equal(t, 204, resp.StatusCode)

	// Delete non-existent -> 404.
	resp = env.doDelete("/absences/abs_nonexist")
	assert.Equal(t, 404, resp.StatusCode)
}

func TestIntegration_AbsenceOverlap(t *testing.T) {
	env := setupIntTest(t)

	// Create employee.
	env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID:     "emp_ovlp0001",
		Grade:          "M",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	})

	// Create first absence.
	env.doPost("/absences", domain.AbsenceCreate{
		EmployeeID:  "emp_ovlp0001",
		Type:        "vacation",
		StartDate:   mustParseDate("2026-03-01"),
		EndDate:     mustParseDate("2026-03-10"),
		HoursPerDay: 8,
		Approved:    boolPtr(true),
	})

	// Overlapping absence -> 409.
	resp := env.doPost("/absences", domain.AbsenceCreate{
		EmployeeID:  "emp_ovlp0001",
		Type:        "training",
		StartDate:   mustParseDate("2026-03-05"),
		EndDate:     mustParseDate("2026-03-07"),
		HoursPerDay: 8,
		Approved:    boolPtr(true),
	})
	assert.Equal(t, 409, resp.StatusCode)
	var problem domain.Problem
	decodeJSON(t, resp, &problem)
	assert.Contains(t, problem.ReasonCodes, "ABSENCE_OVERLAP")
}

// ---------- Test Helpers ----------

func mustParseDate(s string) time.Time {
	t, err := domain.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return t
}

func boolPtr(b bool) *bool { return &b }
