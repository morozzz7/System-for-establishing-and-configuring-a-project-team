// Package service implements business logic with validation, G1/G2 enforcement,
// and audit logging for all mutations.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
)

// Services bundles all service interfaces for dependency injection.
type EmployeeService struct {
	EmployeeRepo repository.EmployeeRepo
	CompRepo     repository.EmployeeCompetencyRepo
	AuditRepo    repository.AuditRepo
}

type ProjectService struct {
	ProjectRepo repository.ProjectRepo
	AuditRepo   repository.AuditRepo
}

type AssignmentService struct {
	AssignmentRepo repository.AssignmentRepo
	EmployeeRepo   repository.EmployeeRepo
	AbsenceRepo    repository.AbsenceRepo
	ProjectRepo    repository.ProjectRepo
	AuditRepo      repository.AuditRepo
	CapacitySvc    *CapacityService // optional; when set, G1/G2 checks delegate here
}

type AbsenceService struct {
	AbsenceRepo  repository.AbsenceRepo
	EmployeeRepo repository.EmployeeRepo
	AuditRepo    repository.AuditRepo
}

// ---------- Employee Service ----------

func (s *EmployeeService) List(ctx context.Context, f domain.EmployeeFilter, requestID string) ([]domain.Employee, int, error) {
	employees, total, err := s.EmployeeRepo.List(ctx, f)
	if err != nil {
		slog.ErrorContext(ctx, "list employees", "error", err)
		return nil, 0, domain.NewProblem(500, "INTERNAL_ERROR", "failed to list employees", requestID, nil)
	}
	return employees, total, nil
}

func (s *EmployeeService) Get(ctx context.Context, id string, requestID string) (domain.Employee, error) {
	e, err := s.EmployeeRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Employee{}, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("employee %s not found", id), requestID, nil)
		}
		slog.ErrorContext(ctx, "get employee", "error", err, "id", id)
		return domain.Employee{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get employee", requestID, nil)
	}
	return e, nil
}

func (s *EmployeeService) Create(ctx context.Context, input domain.EmployeeCreate, requestID string) (domain.Employee, error) {
	// Validate input.
	if p := domain.ValidateEmployeeCreate(input, requestID); p != nil {
		return domain.Employee{}, *p
	}

	// Defaults.
	bench := false
	if input.BenchFlag != nil {
		bench = *input.BenchFlag
	}
	cp := domain.CapacityProfile{
		WeeklyHoursNorm:    40,
		ReserveRatio:       0.2,
		MaxUtilizationRatio: 0.95,
	}
	if input.CapacityProfile != nil {
		cp = *input.CapacityProfile
	}
	cost := gradeToCost(input.Grade)
	if input.CostCoefficient != nil {
		cost = *input.CostCoefficient
	}

	emp := domain.Employee{
		EmployeeID:      input.EmployeeID,
		Grade:           input.Grade,
		RoleProfileID:   input.RoleProfileID,
		UnitID:          input.UnitID,
		EmploymentType:  input.EmploymentType,
		BenchFlag:       bench,
		CapacityProfile: cp,
		CostCoefficient: cost,
		Active:          true,
	}

	if err := s.EmployeeRepo.Create(ctx, emp); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			p := domain.NewProblem(409, "CONFLICT_DUPLICATE_ID",
				fmt.Sprintf("employee %s already exists", emp.EmployeeID), requestID,
				[]string{"DUPLICATE_EMPLOYEE_ID"})
			return domain.Employee{}, p
		}
		if repository.IsFKViolation(err) {
			p := domain.NewProblem(422, "VALIDATION_FAILED",
				"referenced role or grade does not exist", requestID, []string{"VALIDATION_FAILED"})
			p.Errors = []domain.FieldError{{Field: "role_profile_id", Code: "FK_VIOLATION", Detail: "referenced entity not found"}}
			return domain.Employee{}, p
		}
		slog.ErrorContext(ctx, "create employee", "error", err, "id", emp.EmployeeID)
		return domain.Employee{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to create employee", requestID, nil)
	}

	// Audit log.
	s.writeAudit(ctx, "employee_updated", "employee", emp.EmployeeID, requestID,
		fmt.Sprintf("Employee %s created (grade=%s, role=%s)", emp.EmployeeID, emp.Grade, emp.RoleProfileID))

	return emp, nil
}

func (s *EmployeeService) Update(ctx context.Context, id string, input domain.EmployeeUpdate, requestID string) (domain.Employee, error) {
	if p := domain.ValidateEmployeeUpdate(input, requestID); p != nil {
		return domain.Employee{}, *p
	}

	// Fetch existing.
	existing, err := s.EmployeeRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Employee{}, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("employee %s not found", id), requestID, nil)
		}
		return domain.Employee{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get employee", requestID, nil)
	}

	// Build before state for audit.
	before, _ := json.Marshal(existing)

	// Apply updates.
	if input.Grade != nil {
		existing.Grade = *input.Grade
		existing.CostCoefficient = gradeToCost(existing.Grade)
	}
	if input.RoleProfileID != nil {
		existing.RoleProfileID = *input.RoleProfileID
	}
	if input.UnitID != nil {
		existing.UnitID = *input.UnitID
	}
	if input.EmploymentType != nil {
		existing.EmploymentType = *input.EmploymentType
	}
	if input.BenchFlag != nil {
		existing.BenchFlag = *input.BenchFlag
	}
	if input.CapacityProfile != nil {
		existing.CapacityProfile = *input.CapacityProfile
	}
	if input.Active != nil {
		existing.Active = *input.Active
	}

	if err := s.EmployeeRepo.Update(ctx, existing); err != nil {
		slog.ErrorContext(ctx, "update employee", "error", err, "id", id)
		return domain.Employee{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to update employee", requestID, nil)
	}

	// Audit log.
	after, _ := json.Marshal(existing)
	details, _ := json.Marshal(map[string]json.RawMessage{"before": before, "after": after})
	s.writeAuditDetailed(ctx, "employee_updated", "employee", id, requestID,
		fmt.Sprintf("Employee %s updated", id), details)

	return existing, nil
}

// ---------- Competency Service ----------

func (s *EmployeeService) ListCompetencies(ctx context.Context, employeeID string, requestID string) ([]domain.EmployeeCompetency, error) {
	// Check employee exists.
	if _, err := s.EmployeeRepo.Get(ctx, employeeID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("employee %s not found", employeeID), requestID, nil)
		}
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get employee", requestID, nil)
	}
	comps, err := s.CompRepo.ListByEmployee(ctx, employeeID)
	if err != nil {
		slog.ErrorContext(ctx, "list competencies", "error", err, "employee", employeeID)
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to list competencies", requestID, nil)
	}
	return comps, nil
}

func (s *EmployeeService) ReplaceCompetencies(ctx context.Context, employeeID string, input []domain.EmployeeCompetencyUpsert, requestID string) ([]domain.EmployeeCompetency, error) {
	// Check employee exists.
	if _, err := s.EmployeeRepo.Get(ctx, employeeID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("employee %s not found", employeeID), requestID, nil)
		}
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get employee", requestID, nil)
	}

	if len(input) > 200 {
		p := domain.NewProblem(422, "VALIDATION_FAILED", "maximum 200 competencies", requestID, []string{"VALIDATION_FAILED"})
		p.Errors = []domain.FieldError{{Field: "body", Code: "TOO_MANY_ITEMS", Detail: "max 200 competencies allowed"}}
		return nil, p
	}

	// Validate each.
	var errs []domain.FieldError
	for i, c := range input {
		if fe := domain.ValidateCompetencyUpsert(c, i); fe.Code != "" {
			errs = append(errs, fe)
		}
	}
	if len(errs) > 0 {
		p := domain.NewValidationProblem(fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
		return nil, p
	}

	// Convert to domain.
	comps := make([]domain.EmployeeCompetency, len(input))
	for i, u := range input {
		comps[i] = domain.EmployeeCompetency{
			CompetencyID: u.CompetencyID,
			Level:        u.Level,
			Confidence:   u.Confidence,
			LastUsed:     u.LastUsed,
			Source:       u.Source,
		}
	}

	if err := s.CompRepo.UpsertSet(ctx, employeeID, comps); err != nil {
		if repository.IsFKViolation(err) {
			p := domain.NewProblem(422, "VALIDATION_FAILED",
				"one or more competency IDs not found in taxonomy", requestID, []string{"VALIDATION_FAILED"})
			return nil, p
		}
		slog.ErrorContext(ctx, "upsert competencies", "error", err, "employee", employeeID)
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to update competencies", requestID, nil)
	}

	// Audit log.
	s.writeAudit(ctx, "competency_updated", "employee", employeeID, requestID,
		fmt.Sprintf("Competencies of %s replaced (%d entries)", employeeID, len(comps)))

	// Return the stored set.
	result, err := s.CompRepo.ListByEmployee(ctx, employeeID)
	if err != nil {
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to list competencies after upsert", requestID, nil)
	}
	return result, nil
}

// ---------- Project Service ----------

func (s *ProjectService) List(ctx context.Context, f domain.ProjectFilter, requestID string) ([]domain.Project, int, error) {
	projects, total, err := s.ProjectRepo.List(ctx, f)
	if err != nil {
		slog.ErrorContext(ctx, "list projects", "error", err)
		return nil, 0, domain.NewProblem(500, "INTERNAL_ERROR", "failed to list projects", requestID, nil)
	}
	return projects, total, nil
}

func (s *ProjectService) Get(ctx context.Context, id string, requestID string) (domain.Project, error) {
	p, err := s.ProjectRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Project{}, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("project %s not found", id), requestID, nil)
		}
		return domain.Project{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get project", requestID, nil)
	}
	return p, nil
}

func (s *ProjectService) Create(ctx context.Context, input domain.ProjectCreate, requestID string) (domain.Project, error) {
	if p := domain.ValidateProjectCreate(input, requestID); p != nil {
		return domain.Project{}, *p
	}

	projectID := input.ProjectID
	if projectID == "" {
		projectID = domain.GenerateID("prj_", 6)
	}

	replaces := false
	if input.ReplacesOtherProjects != nil {
		replaces = *input.ReplacesOtherProjects
	}

	prj := domain.Project{
		ProjectID:             projectID,
		Code:                  input.Code,
		Name:                  input.Name,
		Status:                input.Status,
		Priority:              input.Priority,
		Criticality:           input.Criticality,
		StartWeek:             input.StartWeek,
		EndWeekPlanned:        input.EndWeekPlanned,
		ReplacesOtherProjects: replaces,
	}

	if err := s.ProjectRepo.Create(ctx, prj); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return domain.Project{}, domain.NewProblem(409, "CONFLICT_DUPLICATE_ID",
				fmt.Sprintf("project %s already exists", prj.ProjectID), requestID, []string{"DUPLICATE_PROJECT_ID"})
		}
		slog.ErrorContext(ctx, "create project", "error", err, "id", prj.ProjectID)
		return domain.Project{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to create project", requestID, nil)
	}

	s.writeAudit(ctx, "employee_updated", "project", prj.ProjectID, requestID,
		fmt.Sprintf("Project %s (%s) created", prj.ProjectID, prj.Code))

	return prj, nil
}

func (s *ProjectService) Update(ctx context.Context, id string, input domain.ProjectCreate, requestID string) (domain.Project, error) {
	if p := domain.ValidateProjectCreate(input, requestID); p != nil {
		return domain.Project{}, *p
	}

	existing, err := s.ProjectRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Project{}, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("project %s not found", id), requestID, nil)
		}
		return domain.Project{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get project", requestID, nil)
	}

	replaces := existing.ReplacesOtherProjects
	if input.ReplacesOtherProjects != nil {
		replaces = *input.ReplacesOtherProjects
	}

	updated := domain.Project{
		ProjectID:             id,
		Code:                  input.Code,
		Name:                  input.Name,
		Status:                input.Status,
		Priority:              input.Priority,
		Criticality:           input.Criticality,
		StartWeek:             input.StartWeek,
		EndWeekPlanned:        input.EndWeekPlanned,
		EndWeekForecast:       existing.EndWeekForecast,
		ReplacesOtherProjects: replaces,
	}

	if err := s.ProjectRepo.Update(ctx, updated); err != nil {
		slog.ErrorContext(ctx, "update project", "error", err, "id", id)
		return domain.Project{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to update project", requestID, nil)
	}

	s.writeAudit(ctx, "employee_updated", "project", id, requestID,
		fmt.Sprintf("Project %s updated", id))

	return updated, nil
}

// ---------- Assignment Service ----------

func (s *AssignmentService) ListByProject(ctx context.Context, projectID string, fromWeek, toWeek, requestID string) ([]domain.Assignment, error) {
	// Check project exists.
	if _, err := s.ProjectRepo.Get(ctx, projectID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("project %s not found", projectID), requestID, nil)
		}
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get project", requestID, nil)
	}
	assignments, err := s.AssignmentRepo.ListByProject(ctx, projectID, fromWeek, toWeek)
	if err != nil {
		slog.ErrorContext(ctx, "list assignments", "error", err, "project", projectID)
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to list assignments", requestID, nil)
	}
	return assignments, nil
}

func (s *AssignmentService) CreateBatch(ctx context.Context, projectID string, batch domain.AssignmentCreateBatch, requestID string) ([]domain.Assignment, error) {
	// Check project exists.
	if _, err := s.ProjectRepo.Get(ctx, projectID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("project %s not found", projectID), requestID, nil)
		}
		return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get project", requestID, nil)
	}

	if len(batch.Assignments) == 0 {
		p := domain.NewProblem(422, "VALIDATION_FAILED", "assignments array must not be empty",
			requestID, []string{"VALIDATION_FAILED"})
		p.Errors = []domain.FieldError{{Field: "assignments", Code: "EMPTY_ARRAY", Detail: "at least 1 assignment required"}}
		return nil, p
	}
	if len(batch.Assignments) > 500 {
		p := domain.NewProblem(422, "VALIDATION_FAILED", "maximum 500 assignments per batch",
			requestID, []string{"VALIDATION_FAILED"})
		p.Errors = []domain.FieldError{{Field: "assignments", Code: "TOO_MANY_ITEMS", Detail: "max 500"}}
		return nil, p
	}

	// Validate each assignment.
	var errs []domain.FieldError
	for i, a := range batch.Assignments {
		if fe := domain.ValidateAssignmentCreate(a, i); fe.Code != "" {
			errs = append(errs, fe)
		}
	}
	if len(errs) > 0 {
		return nil, domain.NewValidationProblem(
			fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
	}

	// Create each assignment with G1/G2 checks.
	var results []domain.Assignment
	for i, ac := range batch.Assignments {
		// Check employee exists and get capacity profile.
		emp, err := s.EmployeeRepo.Get(ctx, ac.EmployeeID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				p := domain.NewProblem(422, "VALIDATION_FAILED",
					fmt.Sprintf("employee %s not found", ac.EmployeeID), requestID, []string{"VALIDATION_FAILED"})
				p.Errors = []domain.FieldError{{
					Field: fmt.Sprintf("assignments[%d].employee_id", i),
					Code:  "NOT_FOUND",
					Detail: fmt.Sprintf("employee %s does not exist", ac.EmployeeID),
				}}
				return nil, p
			}
			return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to get employee", requestID, nil)
		}

		// G1 + G2 enforcement via CapacityService (preferred) or legacy inline checks.
		requestedMinutes := int(ac.Hours*60 + 0.5)
		if s.CapacitySvc != nil {
			if err := s.CapacitySvc.CheckAvailability(ctx, ac.EmployeeID, ac.Week, requestedMinutes); err != nil {
				var p domain.Problem
				if errors.As(err, &p) {
					// G1 is never overridable. G2 can be overridden.
					if p.Code == "G2_OVERLOAD" && batch.OverloadOverride != nil {
						// Override accepted – proceed.
					} else {
						// Tag the error to the specific assignment index.
						if len(p.Errors) > 0 {
							for j := range p.Errors {
								p.Errors[j].Field = fmt.Sprintf("assignments[%d].%s", i, p.Errors[j].Field)
							}
						}
						p.RequestID = requestID
						return nil, p
					}
				} else {
					return nil, domain.NewProblem(500, "INTERNAL_ERROR", "capacity check failed", requestID, nil)
				}
			}
		} else {
			// Legacy inline G1/G2 checks (kept for backward compatibility with tests that
			// do not inject a CapacityService).
			absent, err := s.AbsenceRepo.IsAbsentInWeek(ctx, ac.EmployeeID, ac.Week)
			if err != nil {
				return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to check absence", requestID, nil)
			}
			if absent && batch.OverloadOverride == nil {
				p := domain.NewProblem(409, "CONFLICT_OVERLOAD",
					fmt.Sprintf("%s is absent in %s", ac.EmployeeID, ac.Week), requestID,
					[]string{"G1_UNAVAILABLE_ASSIGNED"})
				p.Errors = []domain.FieldError{{
					Field:  fmt.Sprintf("assignments[%d].week", i),
					Code:   "G1_UNAVAILABLE_ASSIGNED",
					Detail: fmt.Sprintf("employee is unavailable in %s", ac.Week),
				}}
				return nil, p
			}

			existingHours, err := s.AssignmentRepo.TotalAssignedHours(ctx, ac.EmployeeID, ac.Week)
			if err != nil {
				return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to check capacity", requestID, nil)
			}
			maxHours := emp.CapacityProfile.WeeklyHoursNorm * (1 - emp.CapacityProfile.ReserveRatio)
			newTotal := existingHours + ac.Hours
			if newTotal > maxHours && batch.OverloadOverride == nil {
				p := domain.NewProblem(409, "CONFLICT_OVERLOAD",
					fmt.Sprintf("%s would reach %.0f%% in %s",
						ac.EmployeeID,
						(newTotal/emp.CapacityProfile.WeeklyHoursNorm)*100,
						ac.Week),
					requestID, []string{"G2_OVERLOAD"})
				p.Errors = []domain.FieldError{{
					Field:  fmt.Sprintf("assignments[%d].hours", i),
					Code:   "G2_OVERLOAD",
					Detail: fmt.Sprintf("free_hours=%.0f, requested=%.0f", maxHours-existingHours, ac.Hours),
				}}
				return nil, p
			}
		}

		a := domain.Assignment{
			AssignmentID: domain.GenerateID("asg_", 6),
			ProjectID:    projectID,
			EmployeeID:   ac.EmployeeID,
			RoleID:       ac.RoleID,
			Week:         ac.Week,
			Hours:        ac.Hours,
			Source:       "plan",
		}

		if err := s.AssignmentRepo.Create(ctx, a); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				p := domain.NewProblem(409, "CONFLICT_DUPLICATE",
					fmt.Sprintf("duplicate assignment for %s in %s on project %s",
						ac.EmployeeID, ac.Week, projectID), requestID, nil)
				return nil, p
			}
			return nil, domain.NewProblem(500, "INTERNAL_ERROR", "failed to create assignment", requestID, nil)
		}

		results = append(results, a)
	}

	// Audit.
	s.writeAudit(ctx, "employee_updated", "assignment", projectID, requestID,
		fmt.Sprintf("%d assignments created on project %s", len(results), projectID))

	return results, nil
}

// ---------- Absence Service ----------

func (s *AbsenceService) List(ctx context.Context, f domain.AbsenceFilter, requestID string) ([]domain.Absence, int, error) {
	absences, total, err := s.AbsenceRepo.List(ctx, f)
	if err != nil {
		slog.ErrorContext(ctx, "list absences", "error", err)
		return nil, 0, domain.NewProblem(500, "INTERNAL_ERROR", "failed to list absences", requestID, nil)
	}
	return absences, total, nil
}

func (s *AbsenceService) Create(ctx context.Context, input domain.AbsenceCreate, requestID string) (domain.Absence, error) {
	if p := domain.ValidateAbsenceCreate(input, requestID); p != nil {
		return domain.Absence{}, *p
	}

	// Check employee exists.
	if _, err := s.EmployeeRepo.Get(ctx, input.EmployeeID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Absence{}, domain.NewProblem(422, "VALIDATION_FAILED",
				fmt.Sprintf("employee %s not found", input.EmployeeID), requestID, []string{"VALIDATION_FAILED"})
		}
		return domain.Absence{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to check employee", requestID, nil)
	}

	// Check overlap.
	overlaps, err := s.AbsenceRepo.HasOverlap(ctx, input.EmployeeID, input.StartDate, input.EndDate)
	if err != nil {
		return domain.Absence{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to check overlap", requestID, nil)
	}
	if overlaps {
		p := domain.NewProblem(409, "CONFLICT_ABSENCE_OVERLAP",
			fmt.Sprintf("%s already has an approved absence overlapping %s to %s",
				input.EmployeeID,
				domain.FormatDate(input.StartDate),
				domain.FormatDate(input.EndDate)),
			requestID, []string{"ABSENCE_OVERLAP"})
		return domain.Absence{}, p
	}

	approved := true
	if input.Approved != nil {
		approved = *input.Approved
	}
	source := input.Source
	if source == "" {
		source = "manual"
	}

	absence := domain.Absence{
		AbsenceID:   domain.GenerateID("abs_", 6),
		EmployeeID:  input.EmployeeID,
		Type:        input.Type,
		StartDate:   input.StartDate,
		EndDate:     input.EndDate,
		HoursPerDay: input.HoursPerDay,
		Approved:    approved,
		Source:      source,
	}

	if err := s.AbsenceRepo.Create(ctx, absence); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			p := domain.NewProblem(409, "CONFLICT_ABSENCE_OVERLAP",
				"duplicate absence", requestID, []string{"ABSENCE_OVERLAP"})
			return domain.Absence{}, p
		}
		return domain.Absence{}, domain.NewProblem(500, "INTERNAL_ERROR", "failed to create absence", requestID, nil)
	}

	s.writeAudit(ctx, "absence_created", "absence", absence.AbsenceID, requestID,
		fmt.Sprintf("Absence %s created for %s (%s, %s to %s)",
			absence.AbsenceID, absence.EmployeeID, absence.Type,
			domain.FormatDate(absence.StartDate), domain.FormatDate(absence.EndDate)))

	return absence, nil
}

func (s *AbsenceService) Delete(ctx context.Context, id string, requestID string) error {
	if err := s.AbsenceRepo.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("absence %s not found", id), requestID, nil)
		}
		return domain.NewProblem(500, "INTERNAL_ERROR", "failed to delete absence", requestID, nil)
	}

	s.writeAudit(ctx, "absence_cancelled", "absence", id, requestID,
		fmt.Sprintf("Absence %s cancelled", id))

	return nil
}

// ---------- Helpers ----------

func (s *EmployeeService) writeAudit(ctx context.Context, action, entityType, entityID, requestID, summary string) {
	entry := domain.AuditEntry{
		EventID:    domain.GenerateID("aud_", 6),
		At:         time.Now().UTC(),
		Actor:      "system",
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		RequestID:  requestID,
		Summary:    summary,
	}
	if err := s.AuditRepo.Create(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit log write failed", "error", err, "event", action)
	}
}

func (s *EmployeeService) writeAuditDetailed(ctx context.Context, action, entityType, entityID, requestID, summary string, details json.RawMessage) {
	entry := domain.AuditEntry{
		EventID:    domain.GenerateID("aud_", 6),
		At:         time.Now().UTC(),
		Actor:      "system",
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		RequestID:  requestID,
		Summary:    summary,
		Details:    details,
	}
	if err := s.AuditRepo.Create(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit log write failed", "error", err, "event", action)
	}
}

func (s *ProjectService) writeAudit(ctx context.Context, action, entityType, entityID, requestID, summary string) {
	entry := domain.AuditEntry{
		EventID:    domain.GenerateID("aud_", 6),
		At:         time.Now().UTC(),
		Actor:      "system",
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		RequestID:  requestID,
		Summary:    summary,
	}
	if err := s.AuditRepo.Create(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit log write failed", "error", err, "event", action)
	}
}

func (s *AssignmentService) writeAudit(ctx context.Context, action, entityType, entityID, requestID, summary string) {
	entry := domain.AuditEntry{
		EventID:    domain.GenerateID("aud_", 6),
		At:         time.Now().UTC(),
		Actor:      "system",
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		RequestID:  requestID,
		Summary:    summary,
	}
	if err := s.AuditRepo.Create(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit log write failed", "error", err, "event", action)
	}
}

func (s *AbsenceService) writeAudit(ctx context.Context, action, entityType, entityID, requestID, summary string) {
	entry := domain.AuditEntry{
		EventID:    domain.GenerateID("aud_", 6),
		At:         time.Now().UTC(),
		Actor:      "system",
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		RequestID:  requestID,
		Summary:    summary,
	}
	if err := s.AuditRepo.Create(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit log write failed", "error", err, "event", action)
	}
}

// gradeToCost returns the default cost coefficient for a grade.
func gradeToCost(grade string) float64 {
	switch grade {
	case "J":
		return 1.0
	case "M":
		return 1.4
	case "S":
		return 1.9
	case "L":
		return 2.6
	case "P":
		return 3.4
	default:
		return 1.0
	}
}

