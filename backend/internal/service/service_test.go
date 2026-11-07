package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

// ---------- Mock repositories ----------

type mockEmployeeRepo struct {
	employees map[string]domain.Employee
	createErr error
}

func newMockEmployeeRepo() *mockEmployeeRepo {
	return &mockEmployeeRepo{employees: make(map[string]domain.Employee)}
}

func (m *mockEmployeeRepo) List(ctx context.Context, f domain.EmployeeFilter) ([]domain.Employee, int, error) {
	var result []domain.Employee
	for _, e := range m.employees {
		result = append(result, e)
	}
	return result, len(result), nil
}

func (m *mockEmployeeRepo) Get(ctx context.Context, id string) (domain.Employee, error) {
	e, ok := m.employees[id]
	if !ok {
		return domain.Employee{}, fmt.Errorf("%w: %s", repository.ErrNotFound, id)
	}
	return e, nil
}

func (m *mockEmployeeRepo) Create(ctx context.Context, e domain.Employee) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, ok := m.employees[e.EmployeeID]; ok {
		return fmt.Errorf("%w: %s", repository.ErrConflict, e.EmployeeID)
	}
	m.employees[e.EmployeeID] = e
	return nil
}

func (m *mockEmployeeRepo) Update(ctx context.Context, e domain.Employee) error {
	if _, ok := m.employees[e.EmployeeID]; !ok {
		return fmt.Errorf("%w: %s", repository.ErrNotFound, e.EmployeeID)
	}
	m.employees[e.EmployeeID] = e
	return nil
}

func (m *mockEmployeeRepo) Exists(ctx context.Context, id string) (bool, error) {
	_, ok := m.employees[id]
	return ok, nil
}

type mockCompRepo struct {
	data map[string][]domain.EmployeeCompetency
}

func newMockCompRepo() *mockCompRepo {
	return &mockCompRepo{data: make(map[string][]domain.EmployeeCompetency)}
}

func (m *mockCompRepo) ListByEmployee(ctx context.Context, employeeID string) ([]domain.EmployeeCompetency, error) {
	comps, ok := m.data[employeeID]
	if !ok {
		return []domain.EmployeeCompetency{}, nil
	}
	return comps, nil
}

func (m *mockCompRepo) UpsertSet(ctx context.Context, employeeID string, comps []domain.EmployeeCompetency) error {
	m.data[employeeID] = comps
	return nil
}

type mockAuditRepo struct {
	entries []domain.AuditEntry
}

func newMockAuditRepo() *mockAuditRepo {
	return &mockAuditRepo{}
}

func (m *mockAuditRepo) Create(ctx context.Context, e domain.AuditEntry) error {
	m.entries = append(m.entries, e)
	return nil
}

type mockProjectRepo struct {
	projects map[string]domain.Project
}

func newMockProjectRepo() *mockProjectRepo {
	return &mockProjectRepo{projects: make(map[string]domain.Project)}
}

func (m *mockProjectRepo) List(ctx context.Context, f domain.ProjectFilter) ([]domain.Project, int, error) {
	var result []domain.Project
	for _, p := range m.projects {
		result = append(result, p)
	}
	return result, len(result), nil
}

func (m *mockProjectRepo) Get(ctx context.Context, id string) (domain.Project, error) {
	p, ok := m.projects[id]
	if !ok {
		return domain.Project{}, fmt.Errorf("%w: %s", repository.ErrNotFound, id)
	}
	return p, nil
}

func (m *mockProjectRepo) Create(ctx context.Context, p domain.Project) error {
	if _, ok := m.projects[p.ProjectID]; ok {
		return fmt.Errorf("%w: %s", repository.ErrConflict, p.ProjectID)
	}
	m.projects[p.ProjectID] = p
	return nil
}

func (m *mockProjectRepo) Update(ctx context.Context, p domain.Project) error {
	if _, ok := m.projects[p.ProjectID]; !ok {
		return fmt.Errorf("%w: %s", repository.ErrNotFound, p.ProjectID)
	}
	m.projects[p.ProjectID] = p
	return nil
}

type mockAssignmentRepo struct {
	assignments []domain.Assignment
}

func newMockAssignmentRepo() *mockAssignmentRepo {
	return &mockAssignmentRepo{}
}

func (m *mockAssignmentRepo) ListByProject(ctx context.Context, projectID string, fromWeek, toWeek string) ([]domain.Assignment, error) {
	var result []domain.Assignment
	for _, a := range m.assignments {
		if a.ProjectID == projectID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *mockAssignmentRepo) ListByFilter(ctx context.Context, f domain.AssignmentFilter) ([]domain.Assignment, error) {
	return m.assignments, nil
}

func (m *mockAssignmentRepo) Create(ctx context.Context, a domain.Assignment) error {
	m.assignments = append(m.assignments, a)
	return nil
}

func (m *mockAssignmentRepo) TotalAssignedHours(ctx context.Context, employeeID, week string) (float64, error) {
	var total float64
	for _, a := range m.assignments {
		if a.EmployeeID == employeeID && a.Week == week {
			total += a.Hours
		}
	}
	return total, nil
}

type mockAbsenceRepo struct {
	absences  []domain.Absence
	overlaps  map[string]bool // employee_id -> has overlap
	absentIn  map[string]bool // "employeeID|week" -> absent
}

func newMockAbsenceRepo() *mockAbsenceRepo {
	return &mockAbsenceRepo{
		overlaps: make(map[string]bool),
		absentIn: make(map[string]bool),
	}
}

func (m *mockAbsenceRepo) List(ctx context.Context, f domain.AbsenceFilter) ([]domain.Absence, int, error) {
	return m.absences, len(m.absences), nil
}

func (m *mockAbsenceRepo) Get(ctx context.Context, id string) (domain.Absence, error) {
	for _, a := range m.absences {
		if a.AbsenceID == id {
			return a, nil
		}
	}
	return domain.Absence{}, fmt.Errorf("%w: %s", repository.ErrNotFound, id)
}

func (m *mockAbsenceRepo) Create(ctx context.Context, a domain.Absence) error {
	m.absences = append(m.absences, a)
	return nil
}

func (m *mockAbsenceRepo) SoftDelete(ctx context.Context, id string) error {
	for i, a := range m.absences {
		if a.AbsenceID == id {
			m.absences = append(m.absences[:i], m.absences[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", repository.ErrNotFound, id)
}

func (m *mockAbsenceRepo) HasOverlap(ctx context.Context, employeeID string, start, end time.Time) (bool, error) {
	return m.overlaps[employeeID], nil
}

func (m *mockAbsenceRepo) IsAbsentInWeek(ctx context.Context, employeeID, week string) (bool, error) {
	return m.absentIn[employeeID+"|"+week], nil
}

// ---------- Employee Service Tests ----------

func TestEmployeeService_Create_Valid(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	input := domain.EmployeeCreate{
		EmployeeID:     "emp_abcd1234",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	}

	emp, err := svc.Create(context.Background(), input, "req_test123456")
	require.NoError(t, err)
	assert.Equal(t, "emp_abcd1234", emp.EmployeeID)
	assert.Equal(t, "S", emp.Grade)
	assert.Equal(t, 1.9, emp.CostCoefficient) // S grade default cost
	assert.True(t, emp.Active)
	assert.Equal(t, 40.0, emp.CapacityProfile.WeeklyHoursNorm)
	assert.Equal(t, 0.2, emp.CapacityProfile.ReserveRatio)
	assert.Len(t, audRepo.entries, 1)
	assert.Equal(t, "employee_updated", audRepo.entries[0].Action)
}

func TestEmployeeService_Create_ValidationError(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	input := domain.EmployeeCreate{
		EmployeeID:     "bad_id",
		Grade:          "X",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	}

	_, err := svc.Create(context.Background(), input, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 422, p.Status)
	assert.GreaterOrEqual(t, len(p.Errors), 2) // employee_id + grade
}

func TestEmployeeService_Create_Duplicate(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	input := domain.EmployeeCreate{
		EmployeeID:     "emp_abcd1234",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	}

	// First create.
	_, err := svc.Create(context.Background(), input, "req_test123456")
	require.NoError(t, err)

	// Duplicate.
	_, err = svc.Create(context.Background(), input, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
	assert.Contains(t, p.ReasonCodes, "DUPLICATE_EMPLOYEE_ID")
}

func TestEmployeeService_Get_NotFound(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	_, err := svc.Get(context.Background(), "emp_nonexist", "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 404, p.Status)
}

func TestEmployeeService_Update(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	// Create first.
	createInput := domain.EmployeeCreate{
		EmployeeID:     "emp_abcd1234",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	}
	_, err := svc.Create(context.Background(), createInput, "req_test123456")
	require.NoError(t, err)

	// Update grade.
	newGrade := "L"
	benchFlag := true
	updateInput := domain.EmployeeUpdate{
		Grade:     &newGrade,
		BenchFlag: &benchFlag,
	}
	emp, err := svc.Update(context.Background(), "emp_abcd1234", updateInput, "req_test123456")
	require.NoError(t, err)
	assert.Equal(t, "L", emp.Grade)
	assert.Equal(t, 2.6, emp.CostCoefficient) // L grade cost
	assert.True(t, emp.BenchFlag)
	assert.GreaterOrEqual(t, len(audRepo.entries), 2)
}

// ---------- Competency Service Tests ----------

func TestEmployeeService_ReplaceCompetencies(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	// Create employee first.
	empRepo.employees["emp_abcd1234"] = domain.Employee{
		EmployeeID:     "emp_abcd1234",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm:    40,
			ReserveRatio:       0.2,
			MaxUtilizationRatio: 0.95,
		},
		CostCoefficient: 1.9,
		Active:          true,
	}

	input := []domain.EmployeeCompetencyUpsert{
		{CompetencyID: "lang_go", Level: 4, Confidence: 0.9, Source: "manager_review"},
		{CompetencyID: "db_postgresql", Level: 3, Confidence: 0.8, Source: "project_history"},
	}

	comps, err := svc.ReplaceCompetencies(context.Background(), "emp_abcd1234", input, "req_test123456")
	require.NoError(t, err)
	assert.Len(t, comps, 2)
	assert.Len(t, audRepo.entries, 1)
	assert.Equal(t, "competency_updated", audRepo.entries[0].Action)
}

func TestEmployeeService_ReplaceCompetencies_InvalidLevel(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	empRepo.employees["emp_abcd1234"] = domain.Employee{EmployeeID: "emp_abcd1234"}

	input := []domain.EmployeeCompetencyUpsert{
		{CompetencyID: "lang_go", Level: 6, Confidence: 0.9, Source: "manager_review"}, // invalid level
	}

	_, err := svc.ReplaceCompetencies(context.Background(), "emp_abcd1234", input, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 422, p.Status)
}

// ---------- Project Service Tests ----------

func TestProjectService_Create(t *testing.T) {
	prjRepo := newMockProjectRepo()
	audRepo := newMockAuditRepo()
	svc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}

	input := domain.ProjectCreate{
		Code:           "PORTAL-2026",
		Name:           "Portal migration",
		Status:         "planned",
		Priority:       1,
		Criticality:    "critical",
		StartWeek:      "2026-W02",
		EndWeekPlanned: "2026-W14",
	}

	prj, err := svc.Create(context.Background(), input, "req_test123456")
	require.NoError(t, err)
	assert.NotEmpty(t, prj.ProjectID)
	assert.Regexp(t, `^prj_[a-z0-9]{4,12}$`, prj.ProjectID)
	assert.Equal(t, "PORTAL-2026", prj.Code)
	assert.Len(t, audRepo.entries, 1)
}

func TestProjectService_Create_ValidationError(t *testing.T) {
	prjRepo := newMockProjectRepo()
	audRepo := newMockAuditRepo()
	svc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}

	input := domain.ProjectCreate{
		Code:           "",
		Name:           "Portal",
		Status:         "invalid",
		Priority:       0,
		Criticality:    "low",
		StartWeek:      "bad",
		EndWeekPlanned: "2026-W14",
	}

	_, err := svc.Create(context.Background(), input, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 422, p.Status)
}

// ---------- Assignment Service Tests (G1/G2 enforcement) ----------

func TestAssignmentService_CreateBatch_G1_Absent(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	asgRepo := newMockAssignmentRepo()
	absRepo := newMockAbsenceRepo()
	prjRepo := newMockProjectRepo()
	audRepo := newMockAuditRepo()

	empRepo.employees["emp_abcd1234"] = domain.Employee{
		EmployeeID: "emp_abcd1234",
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm:    40,
			ReserveRatio:       0.2,
			MaxUtilizationRatio: 0.95,
		},
	}
	prjRepo.projects["prj_test1234"] = domain.Project{ProjectID: "prj_test1234"}
	absRepo.absentIn["emp_abcd1234|2026-W05"] = true // Employee is absent in W05

	svc := &service.AssignmentService{
		AssignmentRepo: asgRepo,
		EmployeeRepo:   empRepo,
		AbsenceRepo:    absRepo,
		ProjectRepo:    prjRepo,
		AuditRepo:      audRepo,
	}

	batch := domain.AssignmentCreateBatch{
		Assignments: []domain.AssignmentCreate{
			{EmployeeID: "emp_abcd1234", RoleID: "backend_engineer", Week: "2026-W05", Hours: 20},
		},
	}

	_, err := svc.CreateBatch(context.Background(), "prj_test1234", batch, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
	assert.Contains(t, p.ReasonCodes, "G1_UNAVAILABLE_ASSIGNED")
}

func TestAssignmentService_CreateBatch_G2_Overload(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	asgRepo := newMockAssignmentRepo()
	absRepo := newMockAbsenceRepo()
	prjRepo := newMockProjectRepo()
	audRepo := newMockAuditRepo()

	empRepo.employees["emp_abcd1234"] = domain.Employee{
		EmployeeID: "emp_abcd1234",
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm:    40,
			ReserveRatio:       0.2,
			MaxUtilizationRatio: 0.95,
		},
	}
	prjRepo.projects["prj_test1234"] = domain.Project{ProjectID: "prj_test1234"}
	// Already 30 hours assigned -> max available = 32 - 30 = 2 hours
	asgRepo.assignments = []domain.Assignment{
		{AssignmentID: "asg_existing", EmployeeID: "emp_abcd1234", Week: "2026-W03", Hours: 30, ProjectID: "prj_other"},
	}

	svc := &service.AssignmentService{
		AssignmentRepo: asgRepo,
		EmployeeRepo:   empRepo,
		AbsenceRepo:    absRepo,
		ProjectRepo:    prjRepo,
		AuditRepo:      audRepo,
	}

	batch := domain.AssignmentCreateBatch{
		Assignments: []domain.AssignmentCreate{
			{EmployeeID: "emp_abcd1234", RoleID: "backend_engineer", Week: "2026-W03", Hours: 10},
		},
	}

	_, err := svc.CreateBatch(context.Background(), "prj_test1234", batch, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
	assert.Contains(t, p.ReasonCodes, "G2_OVERLOAD")
}

func TestAssignmentService_CreateBatch_Success(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	asgRepo := newMockAssignmentRepo()
	absRepo := newMockAbsenceRepo()
	prjRepo := newMockProjectRepo()
	audRepo := newMockAuditRepo()

	empRepo.employees["emp_abcd1234"] = domain.Employee{
		EmployeeID: "emp_abcd1234",
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm:    40,
			ReserveRatio:       0.2,
			MaxUtilizationRatio: 0.95,
		},
	}
	prjRepo.projects["prj_test1234"] = domain.Project{ProjectID: "prj_test1234"}

	svc := &service.AssignmentService{
		AssignmentRepo: asgRepo,
		EmployeeRepo:   empRepo,
		AbsenceRepo:    absRepo,
		ProjectRepo:    prjRepo,
		AuditRepo:      audRepo,
	}

	batch := domain.AssignmentCreateBatch{
		Assignments: []domain.AssignmentCreate{
			{EmployeeID: "emp_abcd1234", RoleID: "backend_engineer", Week: "2026-W03", Hours: 20},
			{EmployeeID: "emp_abcd1234", RoleID: "backend_engineer", Week: "2026-W04", Hours: 10},
		},
	}

	results, err := svc.CreateBatch(context.Background(), "prj_test1234", batch, "req_test123456")
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "prj_test1234", results[0].ProjectID)
	assert.Equal(t, "emp_abcd1234", results[0].EmployeeID)
	assert.Equal(t, 20.0, results[0].Hours)
	assert.Regexp(t, `^asg_[a-z0-9]{4,12}$`, results[0].AssignmentID)
	assert.Len(t, audRepo.entries, 1)
}

// ---------- Absence Service Tests ----------

func TestAbsenceService_Create_Success(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	absRepo := newMockAbsenceRepo()
	audRepo := newMockAuditRepo()

	empRepo.employees["emp_abcd1234"] = domain.Employee{EmployeeID: "emp_abcd1234"}

	svc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	start, _ := domain.ParseDate("2026-02-16")
	end, _ := domain.ParseDate("2026-02-18")
	input := domain.AbsenceCreate{
		EmployeeID:  "emp_abcd1234",
		Type:        "vacation",
		StartDate:   start,
		EndDate:     end,
		HoursPerDay: 8,
	}

	absence, err := svc.Create(context.Background(), input, "req_test123456")
	require.NoError(t, err)
	assert.Regexp(t, `^abs_[a-z0-9]{4,12}$`, absence.AbsenceID)
	assert.Equal(t, "emp_abcd1234", absence.EmployeeID)
	assert.True(t, absence.Approved)
	assert.Equal(t, "manual", absence.Source)
	assert.Len(t, audRepo.entries, 1)
	assert.Equal(t, "absence_created", audRepo.entries[0].Action)
}

func TestAbsenceService_Create_Overlap(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	absRepo := newMockAbsenceRepo()
	audRepo := newMockAuditRepo()

	empRepo.employees["emp_abcd1234"] = domain.Employee{EmployeeID: "emp_abcd1234"}
	absRepo.overlaps["emp_abcd1234"] = true // Simulate overlap

	svc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	start, _ := domain.ParseDate("2026-02-16")
	end, _ := domain.ParseDate("2026-02-18")
	input := domain.AbsenceCreate{
		EmployeeID:  "emp_abcd1234",
		Type:        "vacation",
		StartDate:   start,
		EndDate:     end,
		HoursPerDay: 8,
	}

	_, err := svc.Create(context.Background(), input, "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 409, p.Status)
	assert.Contains(t, p.ReasonCodes, "ABSENCE_OVERLAP")
}

func TestAbsenceService_Delete(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	absRepo := newMockAbsenceRepo()
	audRepo := newMockAuditRepo()

	absRepo.absences = []domain.Absence{
		{AbsenceID: "abs_test1234", EmployeeID: "emp_abcd1234"},
	}

	svc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	err := svc.Delete(context.Background(), "abs_test1234", "req_test123456")
	require.NoError(t, err)
	assert.Len(t, absRepo.absences, 0) // Soft-deleted (removed from mock)
	assert.Len(t, audRepo.entries, 1)
	assert.Equal(t, "absence_cancelled", audRepo.entries[0].Action)
}

func TestAbsenceService_Delete_NotFound(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	absRepo := newMockAbsenceRepo()
	audRepo := newMockAuditRepo()

	svc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

	err := svc.Delete(context.Background(), "abs_nonexist", "req_test123456")
	require.Error(t, err)
	var p domain.Problem
	require.ErrorAs(t, err, &p)
	assert.Equal(t, 404, p.Status)
}

// ---------- Audit log format test ----------

func TestAuditLog_DetailsFormat(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	compRepo := newMockCompRepo()
	audRepo := newMockAuditRepo()
	svc := &service.EmployeeService{EmployeeRepo: empRepo, CompRepo: compRepo, AuditRepo: audRepo}

	// Create employee.
	_, err := svc.Create(context.Background(), domain.EmployeeCreate{
		EmployeeID:     "emp_abcd1234",
		Grade:          "S",
		RoleProfileID:  "backend_engineer",
		UnitID:         "unt_core",
		EmploymentType: "internal",
	}, "req_test123456")
	require.NoError(t, err)

	// Update employee.
	newGrade := "L"
	_, err = svc.Update(context.Background(), "emp_abcd1234", domain.EmployeeUpdate{Grade: &newGrade}, "req_test123456")
	require.NoError(t, err)

	// Check the update audit entry has before/after details.
	updateAudit := audRepo.entries[1]
	assert.Equal(t, "employee_updated", updateAudit.Action)
	require.NotNil(t, updateAudit.Details)

	var details map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(updateAudit.Details, &details))
	assert.Contains(t, details, "before")
	assert.Contains(t, details, "after")
}
