// Package handler implements HTTP handlers for employee import.
package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/service"
)

// EmployeeImportHandler handles bulk employee import.
type EmployeeImportHandler struct {
	Svc *service.EmployeeService
}

// Register registers the import endpoint on /employees (PUT).
func (h *EmployeeImportHandler) Register(r chi.Router) {
	r.Put("/", h.Import)
}

// Import handles PUT /api/v1/employees - bulk import employees.
func (h *EmployeeImportHandler) Import(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)

	var input domain.EmployeeImportRequest
	if err := readJSON(r, &input); err != nil {
		writeProblem(w, domain.NewProblem(400, "BAD_REQUEST", err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}

	resp, err := h.Svc.ImportEmployees(r.Context(), input, rid)
	if err != nil {
		var p domain.Problem
		if errors.As(err, &p) {
			writeProblem(w, p)
			return
		}
		slog.ErrorContext(r.Context(), "import employees", "error", err)
		writeProblem(w, domain.NewProblem(500, "INTERNAL_ERROR", err.Error(), rid, nil))
		return
	}

	// The service returns a response with domain types already, just add request_id.
	apiResp := map[string]interface{}{
		"request_id":        rid,
		"mode":              resp.Mode,
		"totals":            resp.Totals,
		"employees":         resp.Employees,
		"validation_errors": resp.ValidationErrors,
	}

	writeJSON(w, 200, apiResp)
}