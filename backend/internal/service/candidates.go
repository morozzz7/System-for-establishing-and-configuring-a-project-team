package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/runinput"
)

// CandidateFilter mirrors contracts/openapi.yaml CandidateFilter (POST /runs).
type CandidateFilter struct {
	IncludeBench    bool
	IncludeExternal bool
	RoleIDs         []string
	MaxCandidates   int
}

// candidateFilterFromInput applies the contract defaults when a field is absent.
func candidateFilterFromInput(input map[string]any) CandidateFilter {
	filter := CandidateFilter{IncludeBench: true, MaxCandidates: 500}
	raw, ok := input["candidate_filter"].(map[string]any)
	if !ok {
		return filter
	}
	if value, ok := raw["include_bench"].(bool); ok {
		filter.IncludeBench = value
	}
	if value, ok := raw["include_external"].(bool); ok {
		filter.IncludeExternal = value
	}
	for _, entry := range stringList(raw["role_ids"]) {
		if domain.RoleIDRe.MatchString(entry) {
			filter.RoleIDs = append(filter.RoleIDs, entry)
		}
	}
	if limit := intOption(raw, "max_candidates", filter.MaxCandidates); limit >= 1 && limit <= 2000 {
		filter.MaxCandidates = limit
	}
	return filter
}

// CandidateSource materializes the run's candidate pool. The in-memory workflow slice
// stays usable without a database; production uses PgCandidateSource.
type CandidateSource interface {
	Candidates(ctx context.Context, requirements []runinput.Requirement, weeks []string, filter CandidateFilter) ([]runinput.Candidate, error)
}

// PgCandidateSource builds candidates from the active employee profiles, their competency
// levels, the authoritative capacity matrix and current assignments. It never reads
// names, rates or other personal data: only the pseudonymous id, grade and cost coefficient
// travel into the run snapshot.
type PgCandidateSource struct {
	empRepo  repository.EmployeeRepo
	compRepo repository.EmployeeCompetencyRepo
	capRepo  repository.CapacityRepository
	asgRepo  repository.AssignmentRepo
}

func NewPgCandidateSource(
	empRepo repository.EmployeeRepo,
	compRepo repository.EmployeeCompetencyRepo,
	capRepo repository.CapacityRepository,
	asgRepo repository.AssignmentRepo,
) *PgCandidateSource {
	return &PgCandidateSource{empRepo: empRepo, compRepo: compRepo, capRepo: capRepo, asgRepo: asgRepo}
}

func (s *PgCandidateSource) Candidates(ctx context.Context, requirements []runinput.Requirement, weeks []string, filter CandidateFilter) ([]runinput.Candidate, error) {
	if len(weeks) == 0 {
		return nil, fmt.Errorf("%w: cannot build candidates without a planning horizon", ErrInvalidRunInput)
	}

	roleIDs, competencyIDs := requirementKeys(requirements)
	profiles := make([]domain.Employee, 0)
	levels := make(map[string]map[string]int)
	const pageSize = 500
	for offset := 0; ; offset += pageSize {
		employees, total, err := s.empRepo.List(ctx, domain.EmployeeFilter{
			Limit: pageSize, Offset: offset, ActiveOnly: true,
			InternalOnly: !filter.IncludeExternal, ExcludeBench: !filter.IncludeBench, RoleIDs: filter.RoleIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("list candidate employees: %w", err)
		}
		eligible := make([]domain.Employee, 0, len(employees))
		employeeIDs := make([]string, 0, len(employees))
		for _, emp := range employees {
			if !emp.Active || (!filter.IncludeBench && emp.BenchFlag) || (!filter.IncludeExternal && emp.EmploymentType != "internal") {
				continue
			}
			if len(filter.RoleIDs) > 0 && !containsString(filter.RoleIDs, emp.RoleProfileID) {
				continue
			}
			eligible = append(eligible, emp)
			employeeIDs = append(employeeIDs, emp.EmployeeID)
		}
		competencies, err := s.candidateCompetencies(ctx, employeeIDs)
		if err != nil {
			return nil, fmt.Errorf("list candidate competencies: %w", err)
		}
		for _, emp := range eligible {
			comps := competencies[emp.EmployeeID]
			employeeLevels := make(map[string]int, len(comps))
			for _, comp := range comps {
				if comp.Level < 1 || comp.Level > 5 {
					return nil, fmt.Errorf("%w: %s has competency %s at invalid level %d",
						ErrInvalidRunInput, emp.EmployeeID, comp.CompetencyID, comp.Level)
				}
				employeeLevels[comp.CompetencyID] = comp.Level
			}
			if !matchesRequirement(emp.RoleProfileID, employeeLevels, roleIDs, competencyIDs) {
				continue
			}
			profiles = append(profiles, emp)
			levels[emp.EmployeeID] = employeeLevels
			if len(profiles) == filter.MaxCandidates {
				break
			}
		}
		if len(profiles) == filter.MaxCandidates || len(employees) < pageSize || offset+len(employees) >= total {
			break
		}
	}
	if len(profiles) == 0 {
		return []runinput.Candidate{}, nil
	}

	ids := make([]string, 0, len(profiles))
	for _, emp := range profiles {
		ids = append(ids, emp.EmployeeID)
	}
	capacity, err := s.capRepo.GetCapacityMatrix(ctx, ids, weeks[0], weeks[len(weeks)-1])
	if err != nil {
		return nil, fmt.Errorf("build candidate capacity matrix: %w", err)
	}
	projects, err := s.asgRepo.ListByFilter(ctx, domain.AssignmentFilter{
		EmployeeIDs: ids, FromWeek: weeks[0], ToWeek: weeks[len(weeks)-1],
	})
	if err != nil {
		return nil, fmt.Errorf("list current assignments: %w", err)
	}
	currentProjects := make(map[string]map[string]bool, len(ids))
	for _, a := range projects {
		if currentProjects[a.EmployeeID] == nil {
			currentProjects[a.EmployeeID] = make(map[string]bool)
		}
		currentProjects[a.EmployeeID][a.ProjectID] = true
	}

	out := make([]runinput.Candidate, 0, len(profiles))
	for _, emp := range profiles {
		weeksCapacity, ok := capacity[emp.EmployeeID]
		if !ok || len(weeksCapacity) == 0 {
			return nil, fmt.Errorf("%w: capacity is missing for candidate %s", ErrInvalidRunInput, emp.EmployeeID)
		}
		rows := make([]runinput.CapacityRow, 0, len(weeksCapacity))
		for _, week := range weeksCapacity {
			availableMinutes := week.AvailableMinutes
			if availableMinutes < 0 {
				availableMinutes = 0
			}
			year, number, err := domain.ParseWeek(week.Week)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid week %s", ErrInvalidRunInput, week.Week)
			}
			workingDays, err := domain.WorkingDaysInWeekChecked(year, number)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", ErrInvalidRunInput, err.Error())
			}
			rows = append(rows, runinput.CapacityRow{
				Week:          week.Week,
				WorkingDays:   workingDays,
				NormHours:     minutesToHours(week.NormMinutes),
				AssignedHours: minutesToHours(week.AssignedMinutes),
				AbsentHours:   minutesToHours(week.AbsentMinutes),
				FreeHours:     minutesToHours(availableMinutes),
				Unavailable:   availableMinutes == 0,
			})
		}
		projectIDs := make([]string, 0, len(currentProjects[emp.EmployeeID]))
		for projectID := range currentProjects[emp.EmployeeID] {
			projectIDs = append(projectIDs, projectID)
		}
		sort.Strings(projectIDs)
		out = append(out, runinput.Candidate{
			EmployeeID:        emp.EmployeeID,
			Grade:             emp.Grade,
			CostCoefficient:   emp.CostCoefficient,
			RoleProfileID:     emp.RoleProfileID,
			EmploymentType:    emp.EmploymentType,
			BenchFlag:         emp.BenchFlag,
			ExperienceScore:   nil,
			CurrentProjectIDs: projectIDs,
			CompetencyLevels:  levels[emp.EmployeeID],
			Capacity:          rows,
		})
	}
	return out, nil
}

func requirementKeys(requirements []runinput.Requirement) (map[string]bool, map[string]bool) {
	roles := make(map[string]bool, len(requirements))
	competencies := make(map[string]bool, len(requirements))
	for _, r := range requirements {
		roles[r.RoleID] = true
		if r.CompetencyID != nil {
			competencies[*r.CompetencyID] = true
		}
	}
	return roles, competencies
}

func matchesRequirement(roleID string, levels map[string]int, roles, competencies map[string]bool) bool {
	if roles[roleID] {
		return true
	}
	for competencyID := range levels {
		if competencies[competencyID] {
			return true
		}
	}
	return false
}

func minutesToHours(minutes int) float64 { return float64(minutes) / 60.0 }

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		if id, ok := entry.(string); ok {
			out = append(out, id)
		}
	}
	return out
}

func (s *PgCandidateSource) candidateCompetencies(ctx context.Context, ids []string) (map[string][]domain.EmployeeCompetency, error) {
	result := make(map[string][]domain.EmployeeCompetency, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if batch, ok := s.compRepo.(interface {
		ListByEmployees(context.Context, []string) (map[string][]domain.EmployeeCompetency, error)
	}); ok {
		return batch.ListByEmployees(ctx, ids)
	}
	for _, id := range ids {
		comps, err := s.compRepo.ListByEmployee(ctx, id)
		if err != nil {
			return nil, err
		}
		result[id] = comps
	}
	return result, nil
}
