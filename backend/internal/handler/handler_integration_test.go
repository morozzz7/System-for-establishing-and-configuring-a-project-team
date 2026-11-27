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
		"grade":      "L",
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

func TestIntegration_ConcurrentAssignmentsShareWeeklyCapacity(t *testing.T) {
	env := setupIntTest(t)
	employeeID := "emp_asg00002"
	resp := env.doPost("/employees", domain.EmployeeCreate{
		EmployeeID: employeeID, Grade: "S", RoleProfileID: "backend_engineer",
		UnitID: "unt_core", EmploymentType: "internal",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	projects := []string{"prj_asg00002", "prj_asg00003"}
	for _, id := range projects {
		resp = env.doPost("/projects", domain.ProjectCreate{
			ProjectID: id, Code: id, Name: "Synthetic project", Status: "active",
			Priority: 1, Criticality: "normal", StartWeek: "2026-W01", EndWeekPlanned: "2026-W20",
		})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}

	batch := domain.AssignmentCreateBatch{Assignments: []domain.AssignmentCreate{
		{EmployeeID: employeeID, RoleID: "backend_engineer", Week: "2026-W03", Hours: 20},
	}}
	encoded, err := json.Marshal(batch)
	require.NoError(t, err)
	type result struct {
		status int
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, id := range projects {
		go func() {
			<-start
			resp, err := http.Post(env.url("/projects/"+id+"/assignments"), "application/json", bytes.NewReader(encoded))
			if err != nil {
				results <- result{err: err}
				return
			}
			_ = resp.Body.Close()
			results <- result{status: resp.StatusCode}
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, []int{first.status, second.status})

	repo := repository.NewPgAssignmentRepo(env.pool)
	hours, err := repo.TotalAssignedHours(context.Background(), employeeID, "2026-W03")
	require.NoError(t, err)
	require.Equal(t, 20.0, hours)
}

func TestIntegration_SynchronizeApprovedPlan(t *testing.T) {
	env := setupIntTest(t)
	ctx := context.Background()
	projectID := "prj_sync01"
	for _, id := range []string{"emp_sync01", "emp_sync02"} {
		resp := env.doPost("/employees", domain.EmployeeCreate{
			EmployeeID: id, Grade: "S", RoleProfileID: "backend_engineer",
			UnitID: "unt_core", EmploymentType: "internal",
		})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
	resp := env.doPost("/projects", domain.ProjectCreate{
		ProjectID: projectID, Code: "SYNC", Name: "Synthetic project", Status: "planned",
		Priority: 1, Criticality: "normal", StartWeek: "2026-W10", EndWeekPlanned: "2026-W13",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	path := "/projects/" + projectID + "/assignments"
	resp = env.doPost(path, domain.AssignmentCreateBatch{Assignments: []domain.AssignmentCreate{
		{EmployeeID: "emp_sync01", RoleID: "backend_engineer", Week: "2026-W12", Hours: 8},
	}})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var manual []domain.Assignment
	decodeJSON(t, resp, &manual)

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := env.pool.Exec(ctx, query, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO brief (brief_id, title, source_type, language, status, text, text_sha256)
	      VALUES ('brf_sync01','Synthetic','text','ru','confirmed','Synthetic',repeat('0',64))`)
	approve := func(suffix string, at time.Time, hoursByRow []domain.AssignmentCreate) string {
		runID, variantID, publicVariantID := "run_"+suffix, "var_row"+suffix, "var_"+suffix
		exec(`INSERT INTO team_run (run_id, brief_id, status, preset, k, seed, time_limit_ms, start_week, end_week, weights)
		      VALUES ($1,'brf_sync01','succeeded','balanced',1,42,1000,'2026-W10','2026-W13','{}')`, runID)
		exec(`INSERT INTO team_variant (variant_id,run_id,label,gate_pass,score,team_size,cost_units,duration,metric_values,payload_jsonb)
		      VALUES ($1,$2,'Synthetic',true,1,2,1,'{}','{}',jsonb_build_object('variant_id',$3::text))`, variantID, runID, publicVariantID)
		for _, row := range hoursByRow {
			exec(`INSERT INTO variant_member (variant_id,employee_id,role_id,grade,allocated_hours_minutes,assignment_share,competence_fit,reason_codes)
			      VALUES ($1,$2,'backend_engineer','S',1200,1,1,'{}') ON CONFLICT DO NOTHING`, variantID, row.EmployeeID)
			exec(`INSERT INTO variant_weekly_assignment (variant_id,employee_id,requirement_id,week,hours_minutes,share)
			      VALUES ($1,$2,'req_sync01',$3,$4,1)`, variantID, row.EmployeeID, row.Week, int(row.Hours*60))
		}
		exec(`INSERT INTO decision (run_id,variant_id,decided_at,decided_by,audit_event_id)
		      VALUES ($1,$2,$3,'system','aud_sync01')`, runID, publicVariantID, at)
		return runID
	}
	first := approve("sync01", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), []domain.AssignmentCreate{
		{EmployeeID: "emp_sync01", Week: "2026-W10", Hours: 20},
		{EmployeeID: "emp_sync01", Week: "2026-W11", Hours: 14},
	})
	sync := func(runID string, expected int) []byte {
		t.Helper()
		resp := env.doPost(path, domain.AssignmentCreateBatch{ApprovedRunID: runID})
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, expected, resp.StatusCode, "%s", body)
		return body
	}
	sync(first, http.StatusCreated)
	second := approve("sync02", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), []domain.AssignmentCreate{
		{EmployeeID: "emp_sync01", Week: "2026-W10", Hours: 22},
		{EmployeeID: "emp_sync02", Week: "2026-W11", Hours: 12},
	})
	saved := sync(second, http.StatusCreated)
	require.JSONEq(t, string(saved), string(sync(second, http.StatusCreated)))
	sync(first, http.StatusConflict)

	third := approve("sync03", time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC), []domain.AssignmentCreate{
		{EmployeeID: "emp_sync01", Week: "2026-W10", Hours: 18},
		{EmployeeID: "emp_sync01", Week: "2026-W12", Hours: 10},
	})
	sync(third, http.StatusConflict)
	var rows []domain.Assignment
	decodeJSON(t, env.doGet(path), &rows)
	require.Len(t, rows, 3)
	require.Equal(t, 22.0, rows[0].Hours)
	require.Equal(t, "emp_sync02", rows[1].EmployeeID)
	require.Equal(t, manual[0], rows[2])
}
