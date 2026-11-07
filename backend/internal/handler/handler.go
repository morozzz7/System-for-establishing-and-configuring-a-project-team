// Package handler implements HTTP handlers for all CRUD endpoints.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/service"
)

// ---------- Response helpers ----------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode", "error", err)
	}
}

func writeProblem(w http.ResponseWriter, p domain.Problem) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func requestID(r *http.Request) string {
	rid := r.Header.Get("X-Request-Id")
	if rid == "" {
		rid = domain.GenerateID("req_", 12)
	}
	return rid
}

func echoRequestID(w http.ResponseWriter, rid string) {
	w.Header().Set("X-Request-Id", rid)
}

func readJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func parseIntQuery(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// ---------- Employee Handler ----------

type EmployeeHandler struct {
	Svc *service.EmployeeService
}

func (h *EmployeeHandler) Register(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{employeeId}", h.Get)
	r.Patch("/{employeeId}", h.Update)
	r.Get("/{employeeId}/competencies", h.ListCompetencies)
	r.Put("/{employeeId}/competencies", h.ReplaceCompetencies)
}

func (h *EmployeeHandler) List(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	f := domain.EmployeeFilter{
		RoleID:       r.URL.Query().Get("role_id"),
		CompetencyID: r.URL.Query().Get("competency_id"),
		BenchOnly:    r.URL.Query().Get("bench_only") == "true",
		Limit:        parseIntQuery(r, "limit", 20),
		Offset:       parseIntQuery(r, "offset", 0),
	}
	if s := r.URL.Query().Get("min_level"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			f.MinLevel = n
		}
	}

	employees, total, err := h.Svc.List(r.Context(), f, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"items": employees,
		"meta":  domain.PageMeta{Limit: f.Limit, Offset: f.Offset, Total: total},
	})
}

func (h *EmployeeHandler) Get(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	id := chi.URLParam(r, "employeeId")
	emp, err := h.Svc.Get(r.Context(), id, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 200, emp)
}

func (h *EmployeeHandler) Create(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	var input domain.EmployeeCreate
	if err := readJSON(r, &input); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	emp, err := h.Svc.Create(r.Context(), input, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 201, emp)
}

func (h *EmployeeHandler) Update(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	id := chi.URLParam(r, "employeeId")
	var input domain.EmployeeUpdate
	if err := readJSON(r, &input); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	emp, err := h.Svc.Update(r.Context(), id, input, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 200, emp)
}

func (h *EmployeeHandler) ListCompetencies(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	id := chi.URLParam(r, "employeeId")
	comps, err := h.Svc.ListCompetencies(r.Context(), id, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 200, comps)
}

func (h *EmployeeHandler) ReplaceCompetencies(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	id := chi.URLParam(r, "employeeId")
	var input []domain.EmployeeCompetencyUpsert
	if err := readJSON(r, &input); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	comps, err := h.Svc.ReplaceCompetencies(r.Context(), id, input, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 200, comps)
}

// ---------- Project Handler ----------

type ProjectHandler struct {
	Svc *service.ProjectService
}

func (h *ProjectHandler) Register(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{projectId}", h.Get)
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	f := domain.ProjectFilter{
		Status: r.URL.Query().Get("status"),
		Limit:  parseIntQuery(r, "limit", 20),
		Offset: parseIntQuery(r, "offset", 0),
	}

	projects, total, err := h.Svc.List(r.Context(), f, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"items": projects,
		"meta":  domain.PageMeta{Limit: f.Limit, Offset: f.Offset, Total: total},
	})
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	id := chi.URLParam(r, "projectId")
	prj, err := h.Svc.Get(r.Context(), id, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 200, prj)
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	var input domain.ProjectCreate
	if err := readJSON(r, &input); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	prj, err := h.Svc.Create(r.Context(), input, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 201, prj)
}

// ---------- Assignment Handler (project-scoped) ----------

type AssignmentHandler struct {
	Svc *service.AssignmentService
}

func (h *AssignmentHandler) Register(r chi.Router) {
	r.Get("/{projectId}/assignments", h.ListByProject)
	r.Post("/{projectId}/assignments", h.CreateBatch)
}

func (h *AssignmentHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	projectID := chi.URLParam(r, "projectId")
	fromWeek := r.URL.Query().Get("from_week")
	toWeek := r.URL.Query().Get("to_week")

	assignments, err := h.Svc.ListByProject(r.Context(), projectID, fromWeek, toWeek, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 200, assignments)
}

func (h *AssignmentHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	projectID := chi.URLParam(r, "projectId")
	var batch domain.AssignmentCreateBatch
	if err := readJSON(r, &batch); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	assignments, err := h.Svc.CreateBatch(r.Context(), projectID, batch, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 201, assignments)
}

// ---------- Absence Handler ----------

type AbsenceHandler struct {
	Svc *service.AbsenceService
}

func (h *AbsenceHandler) Register(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{absenceId}", h.Delete)
}

func (h *AbsenceHandler) List(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	f := domain.AbsenceFilter{
		EmployeeID: r.URL.Query().Get("employee_id"),
		Limit:      parseIntQuery(r, "limit", 20),
		Offset:     parseIntQuery(r, "offset", 0),
	}
	if s := r.URL.Query().Get("from"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			f.From = &t
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			f.To = &t
		}
	}

	absences, total, err := h.Svc.List(r.Context(), f, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"items": absences,
		"meta":  domain.PageMeta{Limit: f.Limit, Offset: f.Offset, Total: total},
	})
}

func (h *AbsenceHandler) Create(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	var input domain.AbsenceCreate
	if err := readJSON(r, &input); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	absence, err := h.Svc.Create(r.Context(), input, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	writeJSON(w, 201, absence)
}

func (h *AbsenceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	id := chi.URLParam(r, "absenceId")
	if err := h.Svc.Delete(r.Context(), id, rid); err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}
	w.WriteHeader(204)
}

// ---------- Capacity Handler ----------

// CapacityHandler provides read-only endpoints for weekly capacity queries.
type CapacityHandler struct {
	Svc *service.CapacityService
}

func (h *CapacityHandler) Register(r chi.Router) {
	// Nested under /employees via router; this handles the sub-route.
	r.Get("/{employeeId}/capacity", h.GetEmployeeCapacity)
}

func (h *CapacityHandler) GetEmployeeCapacity(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	employeeID := chi.URLParam(r, "employeeId")
	startWeek := r.URL.Query().Get("start_week")
	endWeek := r.URL.Query().Get("end_week")

	if startWeek == "" || endWeek == "" {
		writeProblem(w, domain.NewProblem(400, "VALIDATION_FAILED",
			"start_week and end_week query parameters are required", rid, []string{"VALIDATION_FAILED"}))
		return
	}
	if !domain.ValidateWeekKey(startWeek) || !domain.ValidateWeekKey(endWeek) {
		writeProblem(w, domain.NewProblem(422, "VALIDATION_FAILED",
			"start_week and end_week must be valid ISO week keys (YYYY-WNN)", rid, []string{"VALIDATION_FAILED"}))
		return
	}

	caps, err := h.Svc.GetAvailableHours(r.Context(), employeeID, mustWeekRange(startWeek, endWeek))
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}

	writeJSON(w, 200, domain.CapacityMatrix{
		EmployeeID: employeeID,
		Weeks:      caps,
	})
}

// CapacityMatrixHandler serves the batch capacity matrix endpoint.
type CapacityMatrixHandler struct {
	Svc *service.CapacityService
}

func (h *CapacityMatrixHandler) GetMatrix(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	idsParam := r.URL.Query().Get("employee_ids")
	startWeek := r.URL.Query().Get("start_week")
	endWeek := r.URL.Query().Get("end_week")

	if idsParam == "" || startWeek == "" || endWeek == "" {
		writeProblem(w, domain.NewProblem(400, "VALIDATION_FAILED",
			"employee_ids, start_week, and end_week are required", rid, []string{"VALIDATION_FAILED"}))
		return
	}
	if !domain.ValidateWeekKey(startWeek) || !domain.ValidateWeekKey(endWeek) {
		writeProblem(w, domain.NewProblem(422, "VALIDATION_FAILED",
			"start_week and end_week must be valid ISO week keys (YYYY-WNN)", rid, []string{"VALIDATION_FAILED"}))
		return
	}

	ids := strings.Split(idsParam, ",")
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	if len(ids) > 200 {
		writeProblem(w, domain.NewProblem(422, "VALIDATION_FAILED",
			"maximum 200 employee_ids per request", rid, []string{"VALIDATION_FAILED"}))
		return
	}

	matrix, err := h.Svc.GetCapacityMatrix(r.Context(), ids, startWeek, endWeek)
	if err != nil {
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}

	// Convert to slice for JSON response.
	result := make([]domain.CapacityMatrix, 0, len(matrix))
	for _, id := range ids {
		if caps, ok := matrix[id]; ok {
			result = append(result, domain.CapacityMatrix{EmployeeID: id, Weeks: caps})
		}
	}

	writeJSON(w, 200, result)
}

// mustWeekRange returns all weeks between start and end inclusive.
func mustWeekRange(start, end string) []string {
	weeks, err := domain.WeekRange(start, end)
	if err != nil {
		return []string{start} // fallback
	}
	return weeks
}
