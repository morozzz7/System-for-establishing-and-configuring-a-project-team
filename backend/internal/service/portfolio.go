package service

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/runinput"
)

// PortfolioSource supplies the existing projects that M8 checks for slip.
type PortfolioSource interface {
	Portfolio(ctx context.Context, startWeek, endWeek string) ([]runinput.PortfolioProject, error)
}

// PgPortfolioSource derives portfolio impact inputs from active projects, their planned
// assignments and the authoritative capacity matrix. Absences are not queried directly:
// the matrix already subtracts approved absences from each employee-week.
type PgPortfolioSource struct {
	Projects    repository.ProjectRepo
	Assignments repository.AssignmentRepo
	Absences    repository.AbsenceRepo
	Capacity    repository.CapacityRepository
}

func (s *PgPortfolioSource) Portfolio(ctx context.Context, startWeek, endWeek string) ([]runinput.PortfolioProject, error) {
	weeks, err := portfolioHorizonWeeks(startWeek, endWeek)
	if err != nil {
		return nil, err
	}
	weekSet := make(map[string]bool, len(weeks))
	for _, week := range weeks {
		weekSet[week] = true
	}

	projects, _, err := s.Projects.List(ctx, domain.ProjectFilter{Status: "active"})
	if err != nil {
		return nil, fmt.Errorf("list active projects: %w", err)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ProjectID < projects[j].ProjectID })

	out := make([]runinput.PortfolioProject, 0, len(projects))
	for _, project := range projects {
		assignments, err := s.Assignments.ListByFilter(ctx, domain.AssignmentFilter{
			ProjectID: project.ProjectID,
			FromWeek:  startWeek,
			ToWeek:    endWeek,
		})
		if err != nil {
			return nil, fmt.Errorf("list assignments for project %s: %w", project.ProjectID, err)
		}
		if len(assignments) == 0 {
			continue
		}

		// No DB column stores remaining work: approximate it by the hours planned on
		// this project inside the horizon, ignoring assignments outside the window.
		remaining := 0.0
		employeeIDs := make([]string, 0, len(assignments))
		for _, a := range assignments {
			if !weekSet[a.Week] {
				continue
			}
			remaining += a.Hours
			employeeIDs = append(employeeIDs, a.EmployeeID)
		}
		sort.Strings(employeeIDs)
		employeeIDs = uniqueSorted(employeeIDs)

		matrix, err := s.Capacity.GetCapacityMatrix(ctx, employeeIDs, startWeek, endWeek)
		if err != nil {
			return nil, fmt.Errorf("capacity matrix for project %s: %w", project.ProjectID, err)
		}

		// The matrix already applies min(norm*(1-reserve), norm*maxUtil) and subtracts
		// assigned/absent minutes. Average its free minutes over the horizon weeks.
		totalMinutes := 0.0
		for _, employeeID := range employeeIDs {
			minutesByWeek := make(map[string]int, len(matrix[employeeID]))
			for _, c := range matrix[employeeID] {
				if weekSet[c.Week] {
					minutesByWeek[c.Week] += c.AvailableMinutes
				}
			}
			for _, week := range weeks {
				totalMinutes += float64(minutesByWeek[week])
			}
		}
		weeklyCapacity := totalMinutes / float64(len(weeks)) / 60.0

		out = append(out, runinput.PortfolioProject{
			ProjectID:      project.ProjectID,
			RemainingHours: remaining,
			WeeklyCapacity: weeklyCapacity,
			Critical:       criticalProject(project.Criticality),
			EmployeeIDs:    employeeIDs,
		})
	}
	return out, nil
}

func portfolioHorizonWeeks(startWeek, endWeek string) ([]string, error) {
	sy, sw, err := domain.ParseWeek(startWeek)
	if err != nil {
		return nil, fmt.Errorf("start week: %w", err)
	}
	ey, ew, err := domain.ParseWeek(endWeek)
	if err != nil {
		return nil, fmt.Errorf("end week: %w", err)
	}
	if sy > ey || (sy == ey && sw > ew) {
		return nil, fmt.Errorf("invalid horizon: start week %s is after end week %s", startWeek, endWeek)
	}
	weeks, err := domain.WeekRange(startWeek, endWeek)
	if err != nil {
		return nil, fmt.Errorf("horizon weeks: %w", err)
	}
	return weeks, nil
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

// criticalProject treats the two highest levels of domain.ProjectCriticalities
// ("critical", "high") as critical for M8; "normal" and "low" do not count.
func criticalProject(criticality string) bool {
	return criticality == "critical" || criticality == "high"
}

// portfolioSources is keyed by *WorkflowService because this helper lives in a
// separate file and cannot add a field to the WorkflowService struct.
var portfolioSources sync.Map

// SetPortfolioSource wires a persistent portfolio reader into runs. A nil source
// clears it; without one the workflow keeps its empty portfolio slice.
func (s *WorkflowService) SetPortfolioSource(source PortfolioSource) {
	if source == nil {
		portfolioSources.Delete(s)
		return
	}
	portfolioSources.Store(s, source)
}

func (s *WorkflowService) portfolioSource() PortfolioSource {
	if stored, ok := portfolioSources.Load(s); ok {
		source, _ := stored.(PortfolioSource)
		return source
	}
	return nil
}

// portfolioForRun loads M8 input for the run horizon; without a configured source
// the in-memory workflow keeps its empty portfolio.
func (s *WorkflowService) portfolioForRun(ctx context.Context, startWeek, endWeek string) ([]runinput.PortfolioProject, error) {
	source := s.portfolioSource()
	if source == nil || startWeek == "" || endWeek == "" {
		return []runinput.PortfolioProject{}, nil
	}
	return source.Portfolio(ctx, startWeek, endWeek)
}
