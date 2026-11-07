//go:build property

package service_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"testing/quick"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

// ---------- Mock repositories for property tests ----------

type propEmployeeRepo struct {
	employees map[string]domain.Employee
}

func newPropEmployeeRepo() *propEmployeeRepo {
	return &propEmployeeRepo{employees: make(map[string]domain.Employee)}
}

func (m *propEmployeeRepo) List(_ context.Context, _ domain.EmployeeFilter) ([]domain.Employee, int, error) {
	var r []domain.Employee
	for _, e := range m.employees {
		r = append(r, e)
	}
	return r, len(r), nil
}
func (m *propEmployeeRepo) Get(_ context.Context, id string) (domain.Employee, error) {
	e, ok := m.employees[id]
	if !ok {
		return domain.Employee{}, fmt.Errorf("%w: %s", repository.ErrNotFound, id)
	}
	return e, nil
}
func (m *propEmployeeRepo) Create(_ context.Context, e domain.Employee) error {
	m.employees[e.EmployeeID] = e
	return nil
}
func (m *propEmployeeRepo) Update(_ context.Context, e domain.Employee) error {
	m.employees[e.EmployeeID] = e
	return nil
}
func (m *propEmployeeRepo) Exists(_ context.Context, id string) (bool, error) {
	_, ok := m.employees[id]
	return ok, nil
}

type propAssignmentRepo struct {
	assignments []domain.Assignment
}

func (m *propAssignmentRepo) ListByProject(_ context.Context, _ string, _, _ string) ([]domain.Assignment, error) {
	return m.assignments, nil
}
func (m *propAssignmentRepo) ListByFilter(_ context.Context, _ domain.AssignmentFilter) ([]domain.Assignment, error) {
	return m.assignments, nil
}
func (m *propAssignmentRepo) Create(_ context.Context, a domain.Assignment) error {
	m.assignments = append(m.assignments, a)
	return nil
}
func (m *propAssignmentRepo) TotalAssignedHours(_ context.Context, employeeID, week string) (float64, error) {
	var total float64
	for _, a := range m.assignments {
		if a.EmployeeID == employeeID && a.Week == week {
			total += a.Hours
		}
	}
	return total, nil
}

type propAbsenceRepo struct {
	absentIn map[string]bool
}

func (m *propAbsenceRepo) List(_ context.Context, _ domain.AbsenceFilter) ([]domain.Absence, int, error) {
	return nil, 0, nil
}
func (m *propAbsenceRepo) Get(_ context.Context, _ string) (domain.Absence, error) {
	return domain.Absence{}, repository.ErrNotFound
}
func (m *propAbsenceRepo) Create(_ context.Context, _ domain.Absence) error { return nil }
func (m *propAbsenceRepo) SoftDelete(_ context.Context, _ string) error     { return nil }
func (m *propAbsenceRepo) HasOverlap(_ context.Context, _ string, _, _ time.Time) (bool, error) {
	return false, nil
}
func (m *propAbsenceRepo) IsAbsentInWeek(_ context.Context, employeeID, week string) (bool, error) {
	return m.absentIn[employeeID+"|"+week], nil
}

type propCapacityRepo struct {
	empRepo *propEmployeeRepo
	asgRepo *propAssignmentRepo
	absRepo *propAbsenceRepo
}

func (r *propCapacityRepo) GetWeeklyCapacity(_ context.Context, employeeID, startWeek, endWeek string) ([]domain.WeeklyCapacity, error) {
	emp, ok := r.empRepo.employees[employeeID]
	if !ok {
		return nil, fmt.Errorf("employee not found: %s", employeeID)
	}

	weeks, err := domain.WeekRange(startWeek, endWeek)
	if err != nil {
		return nil, err
	}

	normMin := int(emp.CapacityProfile.WeeklyHoursNorm) * 60
	effectiveLimit := int(math.Round(float64(normMin) * math.Min(1-emp.CapacityProfile.ReserveRatio, emp.CapacityProfile.MaxUtilizationRatio)))

	var result []domain.WeeklyCapacity
	for _, w := range weeks {
		assignedMin := 0
		for _, a := range r.asgRepo.assignments {
			if a.EmployeeID == employeeID && a.Week == w {
				assignedMin += int(math.Round(a.Hours * 60))
			}
		}
		absentMin := 0
		if r.absRepo.absentIn[employeeID+"|"+w] {
			absentMin = 8 * 5 * 60
		}

		available := effectiveLimit - assignedMin - absentMin
		if available < 0 {
			available = 0
		}
		utilRatio := 0.0
		if normMin > 0 {
			utilRatio = float64(assignedMin+absentMin) / float64(normMin)
		}
		result = append(result, domain.WeeklyCapacity{
			EmployeeID:       employeeID,
			Week:             w,
			AvailableMinutes: available,
			AssignedMinutes:  assignedMin,
			AbsentMinutes:    absentMin,
			NormMinutes:      normMin,
			UtilizationRatio: utilRatio,
		})
	}
	return result, nil
}

func (r *propCapacityRepo) GetCapacityMatrix(ctx context.Context, ids []string, start, end string) (map[string][]domain.WeeklyCapacity, error) {
	result := make(map[string][]domain.WeeklyCapacity)
	for _, id := range ids {
		caps, err := r.GetWeeklyCapacity(ctx, id, start, end)
		if err != nil {
			continue
		}
		result[id] = caps
	}
	return result, nil
}

func (r *propCapacityRepo) FindCandidatesByCompetency(_ context.Context, _ string, _ int) ([]string, error) {
	var ids []string
	for id := range r.empRepo.employees {
		ids = append(ids, id)
	}
	return ids, nil
}

// ---------- helpers ----------

func makeCapacityService(empID string, norm int, reserve, maxUtil float64) (*service.CapacityService, *propEmployeeRepo, *propAssignmentRepo, *propAbsenceRepo) {
	empRepo := newPropEmployeeRepo()
	empRepo.employees[empID] = domain.Employee{
		EmployeeID: empID,
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm:    float64(norm),
			ReserveRatio:       reserve,
			MaxUtilizationRatio: maxUtil,
		},
	}
	asgRepo := &propAssignmentRepo{}
	absRepo := &propAbsenceRepo{absentIn: make(map[string]bool)}
	capRepo := &propCapacityRepo{empRepo: empRepo, asgRepo: asgRepo, absRepo: absRepo}
	svc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)
	return svc, empRepo, asgRepo, absRepo
}

// ---------- Property 1: G1 – if employee is absent, CheckAvailability must fail ----------

func TestPropertyG1NoAbsenceConflict(t *testing.T) {
	err := quick.Check(func(absent uint8, requestMin uint16) bool {
		svc, _, _, absRepo := makeCapacityService("emp_prop0001", 40, 0.2, 0.95)
		week := "2026-W05"
		if absent%2 == 1 {
			absRepo.absentIn["emp_prop0001|"+week] = true
		}

		reqMin := int(requestMin%1200) + 1 // 1..1200
		err := svc.CheckAvailability(context.Background(), "emp_prop0001", week, reqMin)
		if absent%2 == 1 {
			return err != nil // must fail
		}
		return err == nil // not absent, small request, should succeed
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------- Property 2: G2 – if total load > effective limit, must fail ----------

func TestPropertyG2NoOverload(t *testing.T) {
	err := quick.Check(func(assignedMin uint16, requestMin uint16, normH uint8, reservePct uint8, maxUtilPct uint8) bool {
		norm := int(normH%51) + 10            // 10..60
		reserve := float64(reservePct%51) / 100.0 // 0.00..0.50
		maxUtil := float64(maxUtilPct%51+50) / 100.0 // 0.50..1.00

		svc, _, asgRepo, _ := makeCapacityService("emp_prop0002", norm, reserve, maxUtil)

		normMin := norm * 60
		effectiveLimit := int(math.Round(float64(normMin) * math.Min(1-reserve, maxUtil)))

		aMin := int(assignedMin % 3000)
		rMin := int(requestMin%1200) + 1

		if aMin > 0 {
			asgRepo.assignments = []domain.Assignment{
				{EmployeeID: "emp_prop0002", Week: "2026-W10", Hours: float64(aMin) / 60.0, ProjectID: "prj_test"},
			}
		}

		err := svc.CheckAvailability(context.Background(), "emp_prop0002", "2026-W10", rMin)
		totalAfter := aMin + rMin
		if totalAfter > effectiveLimit {
			return err != nil // must reject
		}
		return err == nil // must accept
	}, &quick.Config{MaxCount: 500})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------- Property 3: Available minutes are non-negative ----------

func TestPropertyAvailableNonNegative(t *testing.T) {
	err := quick.Check(func(assignedMin uint16, absentFlag uint8, normH uint8, reservePct uint8, maxUtilPct uint8) bool {
		norm := int(normH%51) + 10
		reserve := float64(reservePct%51) / 100.0
		maxUtil := float64(maxUtilPct%51+50) / 100.0

		svc, _, asgRepo, absRepo := makeCapacityService("emp_prop0003", norm, reserve, maxUtil)

		aMin := int(assignedMin % 6000)
		if aMin > 0 {
			asgRepo.assignments = []domain.Assignment{
				{EmployeeID: "emp_prop0003", Week: "2026-W10", Hours: float64(aMin) / 60.0, ProjectID: "prj_test"},
			}
		}
		if absentFlag%2 == 1 {
			absRepo.absentIn["emp_prop0003|2026-W10"] = true
		}

		caps, err := svc.GetAvailableHours(context.Background(), "emp_prop0003", []string{"2026-W10"})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range caps {
			if c.AvailableMinutes < 0 {
				return false
			}
		}
		return true
	}, &quick.Config{MaxCount: 500})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------- Property 4: Capacity is monotonic – adding assignment reduces available ----------

func TestPropertyCapacityMonotonic(t *testing.T) {
	err := quick.Check(func(initialMin uint16, addedMin uint16, normH uint8) bool {
		norm := int(normH%41) + 20 // 20..60
		svc, _, asgRepo, _ := makeCapacityService("emp_prop0004", norm, 0.2, 0.95)

		week := "2026-W10"
		iMin := int(initialMin % 1800)
		aMin := int(addedMin%540) + 60

		if iMin > 0 {
			asgRepo.assignments = []domain.Assignment{
				{EmployeeID: "emp_prop0004", Week: week, Hours: float64(iMin) / 60.0, ProjectID: "prj_t1"},
			}
		}

		capsBefore, err := svc.GetAvailableHours(context.Background(), "emp_prop0004", []string{week})
		if err != nil {
			t.Fatal(err)
		}
		before := capsBefore[0].AvailableMinutes

		asgRepo.assignments = append(asgRepo.assignments, domain.Assignment{
			EmployeeID: "emp_prop0004", Week: week, Hours: float64(aMin) / 60.0, ProjectID: "prj_t2",
		})

		capsAfter, err := svc.GetAvailableHours(context.Background(), "emp_prop0004", []string{week})
		if err != nil {
			t.Fatal(err)
		}
		after := capsAfter[0].AvailableMinutes

		return after <= before
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatal(err)
	}
}
