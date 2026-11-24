package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

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
	r.Get("/runs/{runId}/variants", h.ListVariants)
	r.Get("/runs/{runId}/audit", h.ListAudit)
	r.Get("/runs/{runId}/variants/{variantId}", h.GetVariant)
	r.Get("/runs/{runId}/variants/{variantId}/explanation", h.GetExplanation)
	r.Post("/runs/{runId}/recalculate", h.Recalculate)
	r.Post("/runs/{runId}/decision", h.RecordDecision)
	r.Get("/runs/{runId}/decision", h.GetDecision)
}
func (h *WorkflowHandler) CreateBrief(w http.ResponseWriter, r *http.Request) {
	var input service.BriefInput
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Limit to 10 MB per openapi.yaml
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeProblem(w, problem400(r, fmt.Errorf("file too large or invalid multipart form: %w", err)))
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeProblem(w, problem400(r, fmt.Errorf("missing file field in multipart request: %w", err)))
			return
		}
		defer file.Close()

		fileBytes, err := io.ReadAll(file)
		if err != nil {
			writeProblem(w, problem400(r, fmt.Errorf("failed to read uploaded file: %w", err)))
			return
		}

		title := r.FormValue("title")
		if title == "" {
			title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
			if title == "" {
				title = "Без названия"
			}
		}

		text := ExtractTextFromFile(header.Filename, fileBytes)
		if len(strings.TrimSpace(text)) == 0 {
			writeProblem(w, problem400(r, fmt.Errorf("uploaded file contains no extractable text")))
			return
		}

		lang := r.FormValue("language")
		if lang == "" {
			lang = "ru"
		}

		input = service.BriefInput{
			Title:              title,
			SourceType:         "document",
			Language:           lang,
			Text:               text,
			RequestedStartWeek: r.FormValue("requested_start_week"),
			DeadlineWeek:       r.FormValue("deadline_week"),
		}
	} else {
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
		if in.SourceType == "" {
			in.SourceType = "text"
		}
		if in.Language == "" {
			in.Language = "ru"
		}
		input = service.BriefInput{
			Title:              in.Title,
			SourceType:         in.SourceType,
			Language:           in.Language,
			Text:               in.Text,
			RequestedStartWeek: in.RequestedStartWeek,
			DeadlineWeek:       in.DeadlineWeek,
		}
	}

	b, err := h.Svc.CreateBrief(r.Context(), input)
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
func (h *WorkflowHandler) ListVariants(w http.ResponseWriter, r *http.Request) {
	result, ok := h.Svc.ListVariants(r.Context(), chi.URLParam(r, "runId"),
		parseIntQuery(r, "limit", 20), parseIntQuery(r, "offset", 0))
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, 200, result)
}
func (h *WorkflowHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	result, ok := h.Svc.ListAudit(r.Context(), chi.URLParam(r, "runId"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
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
func (h *WorkflowHandler) RecordDecision(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	var in service.DecisionInput
	if err := readJSON(r, &in); err != nil {
		writeProblem(w, problem400(r, err))
		return
	}
	resp, err := h.Svc.RecordDecision(r.Context(), runID, in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRunNotFound):
			writeProblem(w, problem404(r))
		case errors.Is(err, service.ErrVariantNotFound):
			writeProblem(w, domain.NewProblem(400, "VARIANT_NOT_FOUND", err.Error(), requestID(r), []string{"VARIANT_NOT_FOUND"}))
		case errors.Is(err, service.ErrDecisionExists):
			writeProblem(w, domain.NewProblem(409, "CONFLICT_DECISION_EXISTS", "Use a recalculation run to change the decision.", requestID(r), []string{"DECISION_IMMUTABLE"}))
		default:
			writeProblem(w, domain.NewProblem(500, "DECISION_PERSISTENCE_FAILED", "Decision could not be recorded.", requestID(r), []string{"AUDIT_WRITE_FAILED"}))
		}
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}
func (h *WorkflowHandler) GetDecision(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	d, ok := h.Svc.GetDecision(runID)
	if !ok {
		writeProblem(w, problem404(r))
		return
	}
	writeJSON(w, http.StatusOK, d)
}
func briefJSON(b *service.BriefRecord) map[string]any {
	out := map[string]any{"brief_id": b.BriefID, "title": b.Title, "source_type": b.SourceType, "language": b.Language, "status": b.Status, "created_at": b.CreatedAt, "requirement_count": len(b.Requirements), "needs_review_count": len(b.Review), "extraction": b.Diagnostics, "reason_codes": []string{}}
	if b.RequestedStartWeek != "" {
		out["requested_start_week"] = b.RequestedStartWeek
	}
	if b.DeadlineWeek != "" {
		out["deadline_week"] = b.DeadlineWeek
	}
	return out
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
