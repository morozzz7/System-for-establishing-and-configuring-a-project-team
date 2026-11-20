package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/service"
)

type WorkflowHandler struct{ Svc *service.WorkflowService }

func (h *WorkflowHandler) Register(r chi.Router) {
	r.Post("/briefs", h.CreateBrief)
	r.Get("/briefs", h.ListBriefs)
	r.Get("/briefs/{briefId}", h.GetBrief)
	r.Get("/briefs/{briefId}/text", h.GetBriefText)
	r.Get("/briefs/{briefId}/requirements", h.GetRequirements)
	r.Post("/briefs/{briefId}/requirements:confirm", h.ConfirmBrief)
	r.Post("/runs", h.CreateRun)
	r.Get("/runs/{runId}", h.GetRun)
	r.Get("/runs/{runId}/inputs", h.GetRunInputs)
	r.Get("/runs/{runId}/variants/{variantId}", h.GetVariant)
	r.Get("/runs/{runId}/variants/{variantId}/explanation", h.GetExplanation)
	r.Post("/runs/{runId}/recalculate", h.Recalculate)
}
func (h *WorkflowHandler) CreateBrief(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title              string `json:"title"`
		SourceType         string `json:"source_type"`
		Language           string `json:"language"`
		Text               string `json:"text"`
		RequestedStartWeek string `json:"requested_start_week"`
		DeadlineWeek       string `json:"deadline_week"`
	}
	if err := readJSON(r, &in); err != nil {
		writeProblem(w, problem400(r, err))
		return
	}
	b, err := h.Svc.CreateBrief(r.Context(), service.BriefInput{Title: in.Title, SourceType: in.SourceType, Language: in.Language, Text: in.Text, RequestedStartWeek: in.RequestedStartWeek, DeadlineWeek: in.DeadlineWeek})
	if err != nil {
		writeProblem(w, problem502(r, err))
		return
	}
	writeJSON(w, http.StatusAccepted, briefJSON(b))
}
func (h *WorkflowHandler) ListBriefs(w http.ResponseWriter, _ *http.Request) {
	items := h.Svc.ListBriefs()
	out := make([]any, 0, len(items))
	for _, b := range items {
		out = append(out, briefJSON(b))
	}
	writeJSON(w, 200, map[string]any{"items": out, "meta": map[string]any{"limit": 20, "offset": 0, "total": len(out)}})
}
func (h *WorkflowHandler) GetBrief(w http.ResponseWriter, r *http.Request) {
	b, ok := h.Svc.GetBrief(chi.URLParam(r, "briefId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, briefJSON(b))
}
func (h *WorkflowHandler) GetBriefText(w http.ResponseWriter, r *http.Request) {
	b, ok := h.Svc.GetBrief(chi.URLParam(r, "briefId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	digest := sha256.Sum256([]byte(b.Text))
	writeJSON(w, 200, map[string]any{"brief_id": b.BriefID, "language": b.Language, "deidentified": true, "text": b.Text, "sha256": fmt.Sprintf("%x", digest)})
}
func (h *WorkflowHandler) GetRequirements(w http.ResponseWriter, r *http.Request) {
	b, ok := h.Svc.GetBrief(chi.URLParam(r, "briefId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	items := append([]map[string]any{}, b.Requirements...)
	items = append(items, b.Review...)
	writeJSON(w, 200, map[string]any{"items": items, "meta": map[string]any{"limit": 60, "offset": 0, "total": len(items)}})
}
func (h *WorkflowHandler) ConfirmBrief(w http.ResponseWriter, r *http.Request) {
	b, ok := h.Svc.ConfirmBrief(chi.URLParam(r, "briefId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, briefJSON(b))
}
func (h *WorkflowHandler) CreateRun(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeProblem(w, problem400(r, err))
		return
	}
	briefID, _ := in["brief_id"].(string)
	result, err := h.Svc.CreateRun(r.Context(), briefID, in)
	if err != nil {
		// A request that cannot form a valid snapshot is the caller's error (400);
		// everything else is a solver/transport failure (502).
		if errors.Is(err, service.ErrInvalidRunInput) {
			writeProblem(w, problem400(r, err))
			return
		}
		writeProblem(w, problem502(r, err))
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}
func (h *WorkflowHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	result, ok := h.Svc.GetRun(chi.URLParam(r, "runId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, result)
}
func (h *WorkflowHandler) GetRunInputs(w http.ResponseWriter, r *http.Request) {
	result, ok := h.Svc.GetRunInputs(chi.URLParam(r, "runId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, result)
}
func (h *WorkflowHandler) GetVariant(w http.ResponseWriter, r *http.Request) {
	result, ok := h.Svc.GetVariant(chi.URLParam(r, "runId"), chi.URLParam(r, "variantId"))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, result)
}
func (h *WorkflowHandler) GetExplanation(w http.ResponseWriter, r *http.Request) {
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "ru"
	}
	if locale != "ru" && locale != "en" {
		locale = "ru"
	}
	result, ok := h.Svc.GetExplanation(chi.URLParam(r, "runId"), chi.URLParam(r, "variantId"), locale)
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, result)
}
func (h *WorkflowHandler) Recalculate(w http.ResponseWriter, r *http.Request) {
	var in service.RecalculateRequest
	if err := readJSON(r, &in); err != nil {
		writeProblem(w, problem400(r, err))
		return
	}
	result, err := h.Svc.Recalculate(r.Context(), chi.URLParam(r, "runId"), in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRunNotFound):
			writeProblem(w, problem404(r))
		case errors.Is(err, service.ErrRecalcInfeasible):
			writeProblem(w, domain.NewProblem(422, "SOLVER_INFEASIBLE", err.Error(), requestID(r), []string{"SOLVER_INFEASIBLE"}))
		case errors.Is(err, service.ErrInvalidRunInput), errors.Is(err, service.ErrInvalidOverride):
			writeProblem(w, problem400(r, err))
		default:
			writeProblem(w, problem502(r, err))
		}
		return
	}
	writeJSON(w, 200, result)
}
func briefJSON(b *service.BriefRecord) map[string]any {
	return map[string]any{"brief_id": b.BriefID, "title": b.Title, "source_type": b.SourceType, "language": b.Language, "status": b.Status, "created_at": b.CreatedAt, "requirement_count": len(b.Requirements), "needs_review_count": len(b.Review), "extraction": b.Diagnostics, "reason_codes": []string{}}
}
func problem400(r *http.Request, err error) domain.Problem {
	return domain.NewProblem(400, "BAD_REQUEST", err.Error(), requestID(r), []string{"VALIDATION_FAILED"})
}
func problem404(r *http.Request) domain.Problem {
	return domain.NewProblem(404, "NOT_FOUND", "Resource not found", requestID(r), nil)
}
func problem502(r *http.Request, err error) domain.Problem {
	return domain.NewProblem(502, "ML_UNAVAILABLE", err.Error(), requestID(r), []string{"ML_TIMEOUT"})
}
