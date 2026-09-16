// Package api implements the HTTP/gRPC surface exposed to clients (planner
// task G-01, G-02). Login/register/refresh are HTTP+JSON (this is a
// user-facing flow, not the Rust<->Go device rendezvous boundary, which
// is gRPC per the planner's architecture decision).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nexdesk/nexdesk/backend/internal/audit"
	"github.com/nexdesk/nexdesk/backend/internal/auth"
)

const minPasswordLength = 8

type AuthHandlers struct {
	accounts    *auth.AccountStore
	tokens      *auth.TokenIssuer
	refresh     *auth.RefreshStore
	loginLimits *auth.RateLimiter
	audit       *audit.Logger
}

func NewAuthHandlers(accounts *auth.AccountStore, tokens *auth.TokenIssuer, refresh *auth.RefreshStore, loginLimits *auth.RateLimiter, auditLogger *audit.Logger) *AuthHandlers {
	return &AuthHandlers{accounts: accounts, tokens: tokens, refresh: refresh, loginLimits: loginLimits, audit: auditLogger}
}

// logAudit logs best-effort: a failure here must never block a
// legitimate request, so it's a warning, not an error response.
func (h *AuthHandlers) logAudit(ctx context.Context, eventType, userID, subject string) {
	if err := h.audit.Log(ctx, eventType, userID, subject, nil); err != nil {
		slog.Warn("audit log failed", "event_type", eventType, "error", err)
	}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || len(req.Password) < minPasswordLength {
		writeError(w, http.StatusBadRequest, "email is required and password must be at least 8 characters")
		return
	}

	user, err := h.accounts.CreateUser(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			writeError(w, http.StatusConflict, "email is already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logAudit(r.Context(), audit.EventUserRegistered, user.ID, req.Email)
	h.issueTokens(w, r.Context(), user.ID)
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	allowed, err := h.loginLimits.Allow(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !allowed {
		h.logAudit(r.Context(), audit.EventLoginRateLimited, "", req.Email)
		writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	user, err := h.accounts.VerifyPassword(r.Context(), req.Email, req.Password)
	if err != nil {
		h.logAudit(r.Context(), audit.EventLoginFailed, "", req.Email)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := h.loginLimits.Reset(r.Context(), req.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.logAudit(r.Context(), audit.EventLoginSucceeded, user.ID, req.Email)
	h.issueTokens(w, r.Context(), user.ID)
}

func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, newRefreshToken, err := h.refresh.Rotate(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	accessToken, err := h.tokens.IssueAccessToken(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: accessToken, RefreshToken: newRefreshToken})
}

func (h *AuthHandlers) issueTokens(w http.ResponseWriter, ctx context.Context, userID string) {
	accessToken, err := h.tokens.IssueAccessToken(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	refreshToken, err := h.refresh.Issue(ctx, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: accessToken, RefreshToken: refreshToken})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
