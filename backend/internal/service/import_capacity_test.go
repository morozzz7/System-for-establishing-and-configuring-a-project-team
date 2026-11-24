package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/service"
)

const (
	importEmployeeID = "emp_ab01"
	importProjectID  = "prj_test01"
	importWeek       = "2026-W10"
)

// Effective weekly limit: 40 h norm * min(1-0.2, 0.95) = 32 h.
func importCapacityServices() (*mockEmployeeRepo, *mockAssignmentRepo, *mockAbsenceRepo, *service.EmployeeService) {
	empRepo := newMockEmployeeRepo()
	asgRepo := newMockAssignmentRepo()
	absRepo := newMockAbsenceRepo()
	prjRepo := newMockProjectRepo()
	prjRepo.projects[importProjectID] = domain.Project{ProjectID: importProjectID}
	capRepo := &mockCapacityRepo{matrix: map[string][]domain.WeeklyCapacity{
		importEmployeeID: {{
			EmployeeID: importEmployeeID, Week: importWeek,
			AvailableMinutes: 32 * 60, NormMinutes: 40 * 60,
		}},
	}}
	capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)
	svc := &service.EmployeeService{
		EmployeeRepo: empRepo, CompRepo: newMockCompRepo(), AuditRepo: newMockAuditRepo(),
		AsgRepo: asgRepo, AbsRepo: absRepo, ProjectRepo: prjRepo, CapacitySvc: capSvc,
	}
	return empRepo, asgRepo, absRepo, svc
}

func importItem(assignments ...domain.AssignmentCreate) domain.EmployeeImportItem {
	return domain.EmployeeImportItem{
		EmployeeID: importEmployeeID, Grade: "M", RoleProfileID: "backend_engineer",
		UnitID: "unt_core", EmploymentType: "internal",
		CapacityProfile: &domain.CapacityProfile{
			WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95,
		},
		Assignments: assignments,
	}
}

func importAssignment(hours float64) domain.AssignmentCreate {
	return domain.AssignmentCreate{
		EmployeeID: importEmployeeID, ProjectID: importProjectID,
		RoleID: "backend_engineer", Week: importWeek, Hours: hours,
	}
}

func importPayload(item domain.EmployeeImportItem) domain.EmployeeImportRequest {
	return domain.EmployeeImportRequest{
		Mode: "upsert", Employees: []domain.EmployeeImportItem{item},
	}
}

func TestImportEmployeesRejectsAggregatedOverload(t *testing.T) {
	_, asgRepo, _, svc := importCapacityServices()
	item := importItem(importAssignment(20), importAssignment(20))

	resp, err := svc.ImportEmployees(context.Background(), importPayload(item), "req_test123")
	require.NoError(t, err)
	require.Equal(t, 1, resp.Totals.Errors)
	require.Equal(t, "error", resp.Employees[0].Status)
	require.Len(t, resp.ValidationErrors, 1)
	assert.Equal(t, "G2_OVERLOAD", resp.ValidationErrors[0].Code)
	assert.Empty(t, asgRepo.assignments, "no row of an overloaded employee-week may be written")
}

func TestImportEmployeesAcceptsWithinCapacityBatch(t *testing.T) {
	_, asgRepo, _, svc := importCapacityServices()
	item := importItem(importAssignment(15), importAssignment(15))

	resp, err := svc.ImportEmployees(context.Background(), importPayload(item), "req_test123")
	require.NoError(t, err)
	require.Equal(t, 0, resp.Totals.Errors)
	require.Equal(t, 2, resp.Employees[0].AssignmentsCreated)
	assert.Len(t, asgRepo.assignments, 2)
}

func TestImportEmployeesRejectsAssignmentDuringPayloadAbsence(t *testing.T) {
	_, asgRepo, _, svc := importCapacityServices()
	friday := domain.WeekMonday(2026, 10).AddDate(0, 0, 4)
	item := importItem(importAssignment(8))
	item.Absences = []domain.AbsenceCreate{{
		EmployeeID: importEmployeeID, Type: "vacation", StartDate: friday, EndDate: friday,
		HoursPerDay: 8, Source: "hr_import",
	}}

	resp, err := svc.ImportEmployees(context.Background(), importPayload(item), "req_test123")
	require.NoError(t, err)
	require.Len(t, resp.ValidationErrors, 1)
	assert.Equal(t, "G1_UNAVAILABLE_ASSIGNED", resp.ValidationErrors[0].Code)
	assert.Empty(t, asgRepo.assignments)
}

func TestImportEmployeesRejectsOverlappingPayloadAbsences(t *testing.T) {
	_, _, absRepo, svc := importCapacityServices()
	monday := domain.WeekMonday(2026, 10)
	item := importItem()
	approved := true
	item.Absences = []domain.AbsenceCreate{
		{EmployeeID: importEmployeeID, Type: "vacation", StartDate: monday, EndDate: monday.AddDate(0, 0, 4), HoursPerDay: 8, Approved: &approved, Source: "hr_import"},
		{EmployeeID: importEmployeeID, Type: "sick_leave", StartDate: monday.AddDate(0, 0, 2), EndDate: monday.AddDate(0, 0, 6), HoursPerDay: 8, Approved: &approved, Source: "hr_import"},
	}

	resp, err := svc.ImportEmployees(context.Background(), importPayload(item), "req_test123")
	require.NoError(t, err)
	require.Len(t, resp.ValidationErrors, 1)
	assert.Equal(t, "ABSENCE_OVERLAP", resp.ValidationErrors[0].Code)
	assert.Empty(t, absRepo.absences)
}

func TestAssignmentServiceCreateBatchAccumulatesBeforeWriting(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	asgRepo := newMockAssignmentRepo()
	absRepo := newMockAbsenceRepo()
	prjRepo := newMockProjectRepo()
	prjRepo.projects[importProjectID] = domain.Project{ProjectID: importProjectID}
	empRepo.employees[importEmployeeID] = domain.Employee{
		EmployeeID: importEmployeeID,
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95,
		},
	}
	svc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: newMockAuditRepo(),
	}
	batch := domain.AssignmentCreateBatch{Assignments: []domain.AssignmentCreate{
		importAssignment(20), importAssignment(20),
	}}

	_, err := svc.CreateBatch(context.Background(), importProjectID, batch, "req_test123")
	require.Error(t, err)
	var problem domain.Problem
	require.ErrorAs(t, err, &problem)
	assert.Contains(t, problem.ReasonCodes, "G2_OVERLOAD")
	assert.Empty(t, asgRepo.assignments, "a rejected batch must not write its first row")
}

func TestAssignmentServiceCreateBatchAcceptsAggregateWithinCapacity(t *testing.T) {
	empRepo := newMockEmployeeRepo()
	asgRepo := newMockAssignmentRepo()
	absRepo := newMockAbsenceRepo()
	prjRepo := newMockProjectRepo()
	prjRepo.projects[importProjectID] = domain.Project{ProjectID: importProjectID}
	empRepo.employees[importEmployeeID] = domain.Employee{
		EmployeeID: importEmployeeID,
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95,
		},
	}
	svc := &service.AssignmentService{
		AssignmentRepo: asgRepo, EmployeeRepo: empRepo, AbsenceRepo: absRepo,
		ProjectRepo: prjRepo, AuditRepo: newMockAuditRepo(),
	}
	batch := domain.AssignmentCreateBatch{Assignments: []domain.AssignmentCreate{
		importAssignment(15), importAssignment(15),
	}}

	results, err := svc.CreateBatch(context.Background(), importProjectID, batch, "req_test123")
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Len(t, asgRepo.assignments, 2)
}
