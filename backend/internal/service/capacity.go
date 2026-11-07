package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
)

// CapacityService computes weekly capacity and enforces G1/G2 hard constraints.
type CapacityService struct {
	empRepo repository.EmployeeRepo
	asgRepo repository.AssignmentRepo
	absRepo repository.AbsenceRepo
	capRepo repository.CapacityRepository
}

// NewCapacityService constructs a CapacityService.
func NewCapacityService(
	empRepo repository.EmployeeRepo,
	asgRepo repository.AssignmentRepo,
	absRepo repository.AbsenceRepo,
	capRepo repository.CapacityRepository,
) *CapacityService {
	return &CapacityService{
		empRepo: empRepo,
		asgRepo: asgRepo,
		absRepo: absRepo,
		capRepo: capRepo,
	}
}

// CheckAvailability verifies G1 (absence) and G2 (overload) for a proposed assignment.
// requestedMinutes is the number of minutes the new assignment would consume.
// Returns nil if OK, or a *domain.Problem describing the violation.
func (s *CapacityService) CheckAvailability(ctx context.Context, employeeID, week string, requestedMinutes int) error {
	if !domain.ValidateWeekKey(week) {
		return domain.NewProblem(422, "VALIDATION_FAILED",
			fmt.Sprintf("invalid week key: %s", week), "", []string{"VALIDATION_FAILED"})
	}
	if requestedMinutes <= 0 {
		return domain.NewProblem(422, "VALIDATION_FAILED",
			"requested minutes must be > 0", "", []string{"VALIDATION_FAILED"})
	}

	// 1. Employee must exist.
	emp, err := s.empRepo.Get(ctx, employeeID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.NewProblem(404, "NOT_FOUND",
				fmt.Sprintf("employee %s not found", employeeID), "", nil)
		}
		return fmt.Errorf("get employee: %w", err)
	}

	// 2. G1: Check absence.
	absent, err := s.absRepo.IsAbsentInWeek(ctx, employeeID, week)
	if err != nil {
		return fmt.Errorf("check absence: %w", err)
	}
	if absent {
		return domain.NewProblem(409, "CONFLICT_OVERLOAD",
			fmt.Sprintf("%s is absent in %s", employeeID, week), "",
			[]string{"G1_UNAVAILABLE_ASSIGNED"})
	}

	// 3. Compute capacity for this week.
	caps, err := s.capRepo.GetWeeklyCapacity(ctx, employeeID, week, week)
	if err != nil {
		return fmt.Errorf("get weekly capacity: %w", err)
	}
	if len(caps) == 0 {
		// Should not happen since we verified employee exists, but handle defensively.
		return domain.NewProblem(500, "INTERNAL_ERROR",
			"capacity data unavailable", "", nil)
	}
	cap := caps[0]

	// 4. G2: Check overload.
	// Effective limit = min(norm×(1-reserve), norm×max_util).
	normMin := int(emp.CapacityProfile.WeeklyHoursNorm) * 60
	effectiveLimit := effectiveLimitMinutes(normMin, emp.CapacityProfile.ReserveRatio, emp.CapacityProfile.MaxUtilizationRatio)
	totalAfter := cap.AssignedMinutes + cap.AbsentMinutes + requestedMinutes
	if totalAfter > effectiveLimit {
		free := effectiveLimit - cap.AssignedMinutes - cap.AbsentMinutes
		if free < 0 {
			free = 0
		}
		p := domain.NewProblem(409, "CONFLICT_OVERLOAD",
			fmt.Sprintf("%s would reach %d%% utilization in %s",
				employeeID,
				utilizationPercent(cap.AssignedMinutes+cap.AbsentMinutes+requestedMinutes, normMin),
				week),
			"", []string{"G2_OVERLOAD"})
		p.Errors = []domain.FieldError{{
			Field:  "week",
			Code:   "G2_OVERLOAD",
			Detail: fmt.Sprintf("free_minutes=%d, requested_minutes=%d", free, requestedMinutes),
		}}
		return p
	}

	return nil
}

// GetAvailableHours returns the capacity breakdown for the given employee and weeks.
// Weeks must be valid ISO week keys. The result is ordered to match the input.
func (s *CapacityService) GetAvailableHours(ctx context.Context, employeeID string, weeks []string) ([]domain.WeeklyCapacity, error) {
	if len(weeks) == 0 {
		return []domain.WeeklyCapacity{}, nil
	}

	// Determine the contiguous range covering all requested weeks.
	startWeek := weeks[0]
	endWeek := weeks[len(weeks)-1]
	for _, w := range weeks[1:] {
		if w < startWeek {
			startWeek = w
		}
		if w > endWeek {
			endWeek = w
		}
	}

	all, err := s.capRepo.GetWeeklyCapacity(ctx, employeeID, startWeek, endWeek)
	if err != nil {
		return nil, fmt.Errorf("get weekly capacity: %w", err)
	}

	// Build a set of requested weeks and filter.
	requested := make(map[string]bool, len(weeks))
	for _, w := range weeks {
		requested[w] = true
	}

	var result []domain.WeeklyCapacity
	for _, c := range all {
		if requested[c.Week] {
			result = append(result, c)
		}
	}
	return result, nil
}

// FindAvailableEmployees returns employee IDs that:
//  1. Have competency ≥ minLevel for the given competencyID.
//  2. Have at least minMinutes available in every specified week.
func (s *CapacityService) FindAvailableEmployees(ctx context.Context, weeks []string, minMinutes int, competencyID string, minLevel int) ([]string, error) {
	if len(weeks) == 0 {
		return []string{}, nil
	}

	// 1. Find candidates by competency.
	candidates, err := s.capRepo.FindCandidatesByCompetency(ctx, competencyID, minLevel)
	if err != nil {
		return nil, fmt.Errorf("find candidates: %w", err)
	}
	if len(candidates) == 0 {
		return []string{}, nil
	}

	// 2. Determine week range.
	startWeek := weeks[0]
	endWeek := weeks[0]
	for _, w := range weeks[1:] {
		if w < startWeek {
			startWeek = w
		}
		if w > endWeek {
			endWeek = w
		}
	}

	// 3. Get capacity matrix for all candidates.
	matrix, err := s.capRepo.GetCapacityMatrix(ctx, candidates, startWeek, endWeek)
	if err != nil {
		return nil, fmt.Errorf("get capacity matrix: %w", err)
	}

	// 4. Build week set for filtering.
	weekSet := make(map[string]bool, len(weeks))
	for _, w := range weeks {
		weekSet[w] = true
	}

	// 5. Filter: employee must have >= minMinutes in all requested weeks.
	var result []string
	for _, empID := range candidates {
		caps, ok := matrix[empID]
		if !ok {
			continue // no capacity data (shouldn't happen for active employees)
		}
		available := true
		for _, c := range caps {
			if weekSet[c.Week] && c.AvailableMinutes < minMinutes {
				available = false
				break
			}
		}
		if available {
			result = append(result, empID)
		}
	}
	if result == nil {
		result = []string{}
	}

	slog.DebugContext(ctx, "find available employees",
		"competency", competencyID, "min_level", minLevel,
		"candidates", len(candidates), "available", len(result),
		"weeks", len(weeks), "min_minutes", minMinutes)

	return result, nil
}

// GetCapacityMatrix returns capacity for multiple employees (batch query).
func (s *CapacityService) GetCapacityMatrix(ctx context.Context, employeeIDs []string, startWeek, endWeek string) (map[string][]domain.WeeklyCapacity, error) {
	return s.capRepo.GetCapacityMatrix(ctx, employeeIDs, startWeek, endWeek)
}

// ---------- helpers ----------

// effectiveLimitMinutes computes the effective assignable limit in minutes.
func effectiveLimitMinutes(normMinutes int, reserveRatio, maxUtilization float64) int {
	reserveLimit := float64(normMinutes) * (1.0 - reserveRatio)
	maxLimit := float64(normMinutes) * maxUtilization
	limit := reserveLimit
	if maxLimit < limit {
		limit = maxLimit
	}
	return int(limit + 0.5) // round
}

func utilizationPercent(totalMinutes, normMinutes int) int {
	if normMinutes <= 0 {
		return 0
	}
	return int(float64(totalMinutes) / float64(normMinutes) * 100)
}
