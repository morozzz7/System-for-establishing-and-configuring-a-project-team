package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/platform/auth"
)

// AuthHandler issues tokens from the local provider. The response echoes the principal
// from the token claims; it never accepts a caller-asserted identity.
type AuthHandler struct{ Provider *auth.Provider }

func (h *AuthHandler) Register(r chi.Router) {
	r.Post("/auth/login", h.Login)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	rid := requestID(r)
	echoRequestID(w, rid)
	if h.Provider == nil {
		writeProblem(w, domain.NewProblem(http.StatusServiceUnavailable, "AUTH_UNAVAILABLE",
			"Auth provider is not configured", rid, []string{"AUTH_UNAVAILABLE"}))
		return
	}
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeProblem(w, domain.NewProblem(http.StatusBadRequest, "BAD_REQUEST",
			err.Error(), rid, []string{"VALIDATION_FAILED"}))
		return
	}
	principal, ok := h.Provider.Authenticate(in.Login, in.Password)
	if !ok {
		writeProblem(w, domain.NewProblem(http.StatusUnauthorized, "UNAUTHORIZED",
			"Invalid login or password", rid, []string{"INVALID_CREDENTIALS"}))
		return
	}
	token, expiresIn, err := h.Provider.Issue(principal)
	if err != nil {
		writeProblem(w, domain.NewProblem(http.StatusInternalServerError, "TOKEN_ISSUE_FAILED",
			err.Error(), rid, nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
		"actor":        principal,
	})
}
