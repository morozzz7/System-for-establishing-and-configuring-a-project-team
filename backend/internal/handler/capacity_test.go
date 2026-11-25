package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
)

type capacityEmployeeRepoStub struct {
	employee domain.Employee
	err      error
}

func (s *capacityEmployeeRepoStub) List(context.Context, domain.EmployeeFilter) ([]domain.Employee, int, error) {
	return nil, 0, nil
}
func (s *capacityEmployeeRepoStub) Get(_ context.Context, id string) (domain.Employee, error) {
	if s.err != nil {
		return domain.Employee{}, s.err
	}
	if id != s.employee.EmployeeID {
		return domain.Employee{}, fmt.Errorf("%w: %s", repository.ErrNotFound, id)
	}
	return s.employee, nil
}
func (s *capacityEmployeeRepoStub) Create(context.Context, domain.Employee) error { return nil }
func (s *capacityEmployeeRepoStub) Update(context.Context, domain.Employee) error { return nil }
func (s *capacityEmployeeRepoStub) Exists(context.Context, string) (bool, error)  { return true, nil }
func (s *capacityEmployeeRepoStub) Delete(context.Context, string) error          { return nil }
func (s *capacityEmployeeRepoStub) ListIDs(context.Context) ([]string, error)     { return nil, nil }

type capacityRepoStub struct {
	rows map[string][]domain.WeeklyCapacity
}

func (s *capacityRepoStub) GetWeeklyCapacity(_ context.Context, employeeID, _, _ string) ([]domain.WeeklyCapacity, error) {
	return s.rows[employeeID], nil
}
func (s *capacityRepoStub) GetCapacityMatrix(_ context.Context, ids []string, _, _ string) (map[string][]domain.WeeklyCapacity, error) {
	out := make(map[string][]domain.WeeklyCapacity, len(ids))
	for _, id := range ids {
		out[id] = s.rows[id]
	}
	return out, nil
}
func (s *capacityRepoStub) FindCandidatesByCompetency(context.Context, string, int) ([]string, error) {
	return nil, nil
}

func capacityTestRouter(svc *service.CapacityService) chi.Router {
	r := chi.NewRouter()
	r.Route("/employees", func(r chi.Router) {
		(&CapacityHandler{Svc: svc}).Register(r)
	})
	return r
}

func capacityTestService() *service.CapacityService {
	empRepo := &capacityEmployeeRepoStub{employee: domain.Employee{
		EmployeeID: "emp_7fa2",
		CapacityProfile: domain.CapacityProfile{
			WeeklyHoursNorm: 40, ReserveRatio: 0.2, MaxUtilizationRatio: 0.95,
		},
	}}
	capRepo := &capacityRepoStub{rows: map[string][]domain.WeeklyCapacity{
		"emp_7fa2": {
			{EmployeeID: "emp_7fa2", Week: "2026-W02", NormMinutes: 2400, AssignedMinutes: 720, AbsentMinutes: 0, AvailableMinutes: 1200},
			{EmployeeID: "emp_7fa2", Week: "2026-W03", NormMinutes: 2400, AssignedMinutes: 0, AbsentMinutes: 0, AvailableMinutes: 1920},
			{EmployeeID: "emp_7fa2", Week: "2026-W04", NormMinutes: 2400, AssignedMinutes: 0, AbsentMinutes: 0, AvailableMinutes: 1920},
			{EmployeeID: "emp_7fa2", Week: "2026-W05", NormMinutes: 2400, AssignedMinutes: 0, AbsentMinutes: 2400, AvailableMinutes: 0},
		},
	}}
	return service.NewCapacityService(empRepo, nil, nil, capRepo)
}

// The rows mirror the contract example: 2026-W02 is a New Year holiday week (zero working days).
func TestEmployeeCapacityReturnsContractArray(t *testing.T) {
	rec := httptest.NewRecorder()
	capacityTestRouter(capacityTestService()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/employees/emp_7fa2/capacity?from_week=2026-W02&to_week=2026-W05", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var weeks []domain.CapacityWeek
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&weeks); err != nil {
		t.Fatalf("decode capacity array: %v", err)
	}
	if len(weeks) != 4 {
		t.Fatalf("len = %d, want 4 weeks: %+v", len(weeks), weeks)
	}
	wantFirst := domain.CapacityWeek{
		EmployeeID: "emp_7fa2", Week: "2026-W02", WorkingDays: 0,
		NormHours: 40, ReservedHours: 8, AssignedHours: 12, AbsenceHours: 0,
		FreeHours: 20, LoadRatio: 0.3, Unavailable: true,
	}
	if !reflect.DeepEqual(weeks[0], wantFirst) {
		t.Fatalf("first week = %+v, want %+v", weeks[0], wantFirst)
	}
	wantLast := domain.CapacityWeek{
		EmployeeID: "emp_7fa2", Week: "2026-W05", WorkingDays: 5,
		NormHours: 40, ReservedHours: 8, AssignedHours: 0, AbsenceHours: 40,
		FreeHours: 0, LoadRatio: 0, Unavailable: true,
	}
	if !reflect.DeepEqual(weeks[3], wantLast) {
		t.Fatalf("last week = %+v, want %+v", weeks[3], wantLast)
	}

	var raw []map[string]any
	rec2 := httptest.NewRecorder()
	capacityTestRouter(capacityTestService()).ServeHTTP(rec2,
		httptest.NewRequest(http.MethodGet, "/employees/emp_7fa2/capacity?from_week=2026-W02&to_week=2026-W05", nil))
	if err := json.Unmarshal(rec2.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw array: %v", err)
	}
	wantKeys := []string{"employee_id", "week", "working_days", "norm_hours", "reserved_hours",
		"assigned_hours", "absence_hours", "free_hours", "load_ratio", "unavailable"}
	if len(raw[0]) != len(wantKeys) {
		t.Fatalf("capacity week keys = %v, want exactly %v", raw[0], wantKeys)
	}
	for _, key := range wantKeys {
		if _, ok := raw[0][key]; !ok {
			t.Fatalf("capacity week is missing %q", key)
		}
	}
}

func TestEmployeeCapacityRequiresFromAndToWeek(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"missing both", "/employees/emp_7fa2/capacity", http.StatusBadRequest},
		{"missing to", "/employees/emp_7fa2/capacity?from_week=2026-W02", http.StatusBadRequest},
		{"invalid week", "/employees/emp_7fa2/capacity?from_week=bad&to_week=2026-W05", http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			capacityTestRouter(capacityTestService()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestEmployeeCapacityUnknownEmployeeIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	capacityTestRouter(capacityTestService()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/employees/emp_nobody/capacity?from_week=2026-W02&to_week=2026-W05", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
