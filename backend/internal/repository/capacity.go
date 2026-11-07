package repository

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/domain"
)

// CapacityRepository provides batch-optimized capacity queries.
type CapacityRepository interface {
	// GetWeeklyCapacity returns capacity for a single employee across a week range.
	GetWeeklyCapacity(ctx context.Context, employeeID string, startWeek, endWeek string) ([]domain.WeeklyCapacity, error)

	// GetCapacityMatrix returns capacity for multiple employees across a week range.
	// Avoids N+1 queries by batching profiles, assignments, and absences.
	GetCapacityMatrix(ctx context.Context, employeeIDs []string, startWeek, endWeek string) (map[string][]domain.WeeklyCapacity, error)

	// FindCandidatesByCompetency returns active employee IDs with the given competency at or above minLevel.
	FindCandidatesByCompetency(ctx context.Context, competencyID string, minLevel int) ([]string, error)
}

// PgCapacityRepo implements CapacityRepository on PostgreSQL.
type PgCapacityRepo struct{ Pool *pgxpool.Pool }

// NewPgCapacityRepo creates a new PgCapacityRepo.
func NewPgCapacityRepo(pool *pgxpool.Pool) *PgCapacityRepo {
	return &PgCapacityRepo{Pool: pool}
}

// ---------- internal types for batch queries ----------

type empProfile struct {
	EmployeeID        string
	WeeklyHoursNorm   int // hours (integer in DB)
	ReserveRatio      float64
	MaxUtilization    float64
}

// ---------- single-employee ----------

func (r *PgCapacityRepo) GetWeeklyCapacity(ctx context.Context, employeeID, startWeek, endWeek string) ([]domain.WeeklyCapacity, error) {
	// 1. Validate week range.
	sy, sw, err := domain.ParseWeek(startWeek)
	if err != nil {
		return nil, fmt.Errorf("start week: %w", err)
	}
	ey, ew, err := domain.ParseWeek(endWeek)
	if err != nil {
		return nil, fmt.Errorf("end week: %w", err)
	}

	// 2. Get employee profile.
	var p empProfile
	err = r.Pool.QueryRow(ctx,
		`SELECT employee_id, weekly_hours_norm, reserve_ratio, max_utilization_ratio
		 FROM employee WHERE employee_id = $1 AND active = true`, employeeID,
	).Scan(&p.EmployeeID, &p.WeeklyHoursNorm, &p.ReserveRatio, &p.MaxUtilization)
	if err != nil {
		return nil, fmt.Errorf("get employee profile: %w", err)
	}

	// 3. Get assignment sums per week.
	assignedMap, err := r.getAssignedMinutes(ctx, []string{employeeID}, startWeek, endWeek)
	if err != nil {
		return nil, err
	}

	// 4. Get absences in date range.
	monday := domain.WeekMonday(sy, sw)
	sunday := domain.WeekMonday(ey, ew).AddDate(0, 0, 6)
	absences, err := r.getAbsences(ctx, []string{employeeID}, monday, sunday)
	if err != nil {
		return nil, err
	}

	// 5. Compute weekly capacity.
	weeks, err := domain.WeekRange(startWeek, endWeek)
	if err != nil {
		return nil, err
	}

	return buildWeeklyCapacities(p, weeks, assignedMap[employeeID], absences[employeeID]), nil
}

// ---------- batch ----------

func (r *PgCapacityRepo) GetCapacityMatrix(ctx context.Context, employeeIDs []string, startWeek, endWeek string) (map[string][]domain.WeeklyCapacity, error) {
	if len(employeeIDs) == 0 {
		return map[string][]domain.WeeklyCapacity{}, nil
	}

	sy, sw, err := domain.ParseWeek(startWeek)
	if err != nil {
		return nil, fmt.Errorf("start week: %w", err)
	}
	ey, ew, err := domain.ParseWeek(endWeek)
	if err != nil {
		return nil, fmt.Errorf("end week: %w", err)
	}

	weeks, err := domain.WeekRange(startWeek, endWeek)
	if err != nil {
		return nil, err
	}

	// 1. Batch profiles.
	profiles, err := r.getProfiles(ctx, employeeIDs)
	if err != nil {
		return nil, err
	}

	// 2. Batch assignment sums.
	assignedMap, err := r.getAssignedMinutes(ctx, employeeIDs, startWeek, endWeek)
	if err != nil {
		return nil, err
	}

	// 3. Batch absences.
	monday := domain.WeekMonday(sy, sw)
	sunday := domain.WeekMonday(ey, ew).AddDate(0, 0, 6)
	absencesMap, err := r.getAbsences(ctx, employeeIDs, monday, sunday)
	if err != nil {
		return nil, err
	}

	// 4. Compute per-employee.
	result := make(map[string][]domain.WeeklyCapacity, len(profiles))
	for _, p := range profiles {
		result[p.EmployeeID] = buildWeeklyCapacities(p, weeks, assignedMap[p.EmployeeID], absencesMap[p.EmployeeID])
	}
	return result, nil
}

// ---------- candidate search ----------

func (r *PgCapacityRepo) FindCandidatesByCompetency(ctx context.Context, competencyID string, minLevel int) ([]string, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT DISTINCT e.employee_id
		 FROM employee e
		 JOIN employee_competency ec ON ec.employee_id = e.employee_id
		 WHERE e.active = true AND ec.competency_id = $1 AND ec.level >= $2
		 ORDER BY e.employee_id`, competencyID, minLevel)
	if err != nil {
		return nil, fmt.Errorf("find candidates: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ---------- internal helpers ----------

func (r *PgCapacityRepo) getProfiles(ctx context.Context, employeeIDs []string) ([]empProfile, error) {
	query := `SELECT employee_id, weekly_hours_norm, reserve_ratio, max_utilization_ratio
	          FROM employee WHERE active = true AND employee_id = ANY($1)`
	rows, err := r.Pool.Query(ctx, query, employeeIDs)
	if err != nil {
		return nil, fmt.Errorf("batch profiles: %w", err)
	}
	defer rows.Close()

	var profiles []empProfile
	for rows.Next() {
		var p empProfile
		if err := rows.Scan(&p.EmployeeID, &p.WeeklyHoursNorm, &p.ReserveRatio, &p.MaxUtilization); err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// getAssignedMinutes returns map[employeeID]map[week]totalMinutes.
func (r *PgCapacityRepo) getAssignedMinutes(ctx context.Context, employeeIDs []string, startWeek, endWeek string) (map[string]map[string]int, error) {
	query := `SELECT employee_id, week, COALESCE(SUM(hours_minutes), 0)
	          FROM assignment
	          WHERE employee_id = ANY($1) AND week >= $2 AND week <= $3
	          GROUP BY employee_id, week`
	rows, err := r.Pool.Query(ctx, query, employeeIDs, startWeek, endWeek)
	if err != nil {
		return nil, fmt.Errorf("batch assigned minutes: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]int)
	for rows.Next() {
		var empID, week string
		var minutes int
		if err := rows.Scan(&empID, &week, &minutes); err != nil {
			return nil, fmt.Errorf("scan assigned: %w", err)
		}
		if result[empID] == nil {
			result[empID] = make(map[string]int)
		}
		result[empID][week] = minutes
	}
	return result, nil
}

// getAbsences returns map[employeeID][]domain.Absence for approved, non-deleted absences
// overlapping [rangeStart, rangeEnd].
func (r *PgCapacityRepo) getAbsences(ctx context.Context, employeeIDs []string, rangeStart, rangeEnd time.Time) (map[string][]domain.Absence, error) {
	query := `SELECT employee_id, start_date, end_date, hours_per_day
	          FROM absence
	          WHERE employee_id = ANY($1) AND deleted_at IS NULL AND approved = true
	            AND end_date >= $2 AND start_date <= $3`
	rows, err := r.Pool.Query(ctx, query, employeeIDs, rangeStart, rangeEnd)
	if err != nil {
		return nil, fmt.Errorf("batch absences: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]domain.Absence)
	for rows.Next() {
		var a domain.Absence
		if err := rows.Scan(&a.EmployeeID, &a.StartDate, &a.EndDate, &a.HoursPerDay); err != nil {
			return nil, fmt.Errorf("scan absence: %w", err)
		}
		result[a.EmployeeID] = append(result[a.EmployeeID], a)
	}
	return result, nil
}

// ---------- capacity computation ----------

// buildWeeklyCapacities computes WeeklyCapacity for each week given profile, assignment data, and absences.
func buildWeeklyCapacities(p empProfile, weeks []string, assigned map[string]int, absences []domain.Absence) []domain.WeeklyCapacity {
	normMin := p.WeeklyHoursNorm * 60
	// Effective limit = min(norm × (1 − reserve), norm × max_util).
	effectiveFraction := math.Min(1.0-p.ReserveRatio, p.MaxUtilization)
	effectiveLimit := int(math.Round(float64(normMin) * effectiveFraction))

	result := make([]domain.WeeklyCapacity, len(weeks))
	for i, w := range weeks {
		y, wn, _ := domain.ParseWeek(w)
		assignedMin := assigned[w] // 0 if missing
		absentMin := computeAbsentMinutes(absences, y, wn)

		available := effectiveLimit - assignedMin - absentMin
		if available < 0 {
			available = 0
		}

		utilRatio := 0.0
		if normMin > 0 {
			utilRatio = float64(assignedMin+absentMin) / float64(normMin)
		}

		result[i] = domain.WeeklyCapacity{
			EmployeeID:       p.EmployeeID,
			Week:             w,
			AvailableMinutes: available,
			AssignedMinutes:  assignedMin,
			AbsentMinutes:    absentMin,
			NormMinutes:      normMin,
			UtilizationRatio: utilRatio,
		}
	}
	return result
}

// computeAbsentMinutes sums absent minutes for a given ISO week from a list of absences.
// For each absence overlapping the week, counts Mon–Fri working days in the overlap
// and multiplies by hours_per_day.
func computeAbsentMinutes(absences []domain.Absence, year, week int) int {
	monday := domain.WeekMonday(year, week)
	sunday := monday.AddDate(0, 0, 6)

	total := 0
	for _, abs := range absences {
		overlapStart := abs.StartDate
		if overlapStart.Before(monday) {
			overlapStart = monday
		}
		overlapEnd := abs.EndDate
		if overlapEnd.After(sunday) {
			overlapEnd = sunday
		}
		if overlapStart.After(overlapEnd) {
			continue
		}
		workDays := domain.WorkingDaysInOverlap(overlapStart, overlapEnd)
		total += int(math.Round(abs.HoursPerDay * float64(workDays) * 60))
	}
	return total
}

// ComputeAbsentMinutesInWeek is the exported version for use by other packages.
func ComputeAbsentMinutesInWeek(absences []domain.Absence, year, week int) int {
	return computeAbsentMinutes(absences, year, week)
}

// ---------- query helpers for string building ----------

func buildAnyClause(paramIdx, count int) string {
	if count == 0 {
		return ""
	}
	parts := make([]string, count)
	for i := 0; i < count; i++ {
		parts[i] = fmt.Sprintf("$%d", paramIdx+i)
	}
	return "employee_id IN (" + strings.Join(parts, ",") + ")"
}
