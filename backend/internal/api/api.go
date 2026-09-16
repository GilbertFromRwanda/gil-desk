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
	"strings"
	"time"

	"github.com/nexdesk/nexdesk/backend/internal/audit"
	"github.com/nexdesk/nexdesk/backend/internal/auth"
	"github.com/nexdesk/nexdesk/backend/internal/metrics"
)

const minPasswordLength = 8

type AuthHandlers struct {
	accounts      *auth.AccountStore
	tokens        *auth.TokenIssuer
	refresh       *auth.RefreshStore
	loginLimits   *auth.RateLimiter
	pendingLogins *auth.PendingLoginIssuer
	audit         *audit.Logger
}

func NewAuthHandlers(
	accounts *auth.AccountStore,
	tokens *auth.TokenIssuer,
	refresh *auth.RefreshStore,
	loginLimits *auth.RateLimiter,
	pendingLogins *auth.PendingLoginIssuer,
	auditLogger *audit.Logger,
) *AuthHandlers {
	return &AuthHandlers{
		accounts:      accounts,
		tokens:        tokens,
		refresh:       refresh,
		loginLimits:   loginLimits,
		pendingLogins: pendingLogins,
		audit:         auditLogger,
	}
}

// logAudit logs best-effort: a failure here must never block a
// legitimate request, so it's a warning, not an error response.
func (h *AuthHandlers) logAudit(ctx context.Context, eventType, userID, subject string) {
	if err := h.audit.Log(ctx, eventType, userID, subject, nil); err != nil {
		slog.Warn("audit log failed", "event_type", eventType, "error", err)
	}
}

// authenticate extracts and verifies the caller's JWT access token from
// the Authorization header — the HTTP-side equivalent of
// rendezvous.Service.authenticate for gRPC.
func (h *AuthHandlers) authenticate(r *http.Request) (string, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", errors.New("missing or malformed Authorization header")
	}
	claims, err := h.tokens.VerifyAccessToken(token)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
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

type twoFactorRequiredResponse struct {
	TwoFactorRequired bool   `json:"two_factor_required"`
	PendingToken      string `json:"pending_token"`
	ExpiresInSeconds  uint32 `json:"expires_in_seconds"`
}

type enrollTOTPResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

type verifyTOTPRequest struct {
	Code string `json:"code"`
}

type loginTwoFactorRequest struct {
	PendingToken string `json:"pending_token"`
	Code         string `json:"code"`
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
		metrics.LoginAttemptsTotal.WithLabelValues("rate_limited").Inc()
		h.logAudit(r.Context(), audit.EventLoginRateLimited, "", req.Email)
		writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	user, err := h.accounts.VerifyPassword(r.Context(), req.Email, req.Password)
	if err != nil {
		metrics.LoginAttemptsTotal.WithLabelValues("failed").Inc()
		h.logAudit(r.Context(), audit.EventLoginFailed, "", req.Email)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := h.loginLimits.Reset(r.Context(), req.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if user.TOTPEnabled {
		metrics.LoginAttemptsTotal.WithLabelValues("two_factor_required").Inc()
		pendingToken, expiresAt := h.pendingLogins.Issue(user.ID)
		writeJSON(w, http.StatusOK, twoFactorRequiredResponse{
			TwoFactorRequired: true,
			PendingToken:      pendingToken,
			ExpiresInSeconds:  uint32(time.Until(expiresAt).Seconds()),
		})
		return
	}

	metrics.LoginAttemptsTotal.WithLabelValues("succeeded").Inc()
	h.logAudit(r.Context(), audit.EventLoginSucceeded, user.ID, req.Email)
	h.issueTokens(w, r.Context(), user.ID)
}

// LoginTwoFactor completes a login that Login reported as needing 2FA.
func (h *AuthHandlers) LoginTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req loginTwoFactorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, err := h.pendingLogins.Verify(req.PendingToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired pending login token")
		return
	}

	secret, err := h.accounts.GetTOTPSecret(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}
	if !auth.ValidateTOTPCode(secret, req.Code) {
		metrics.LoginAttemptsTotal.WithLabelValues("two_factor_failed").Inc()
		h.logAudit(r.Context(), audit.EventLoginTwoFactorFailed, userID, "")
		writeError(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}

	metrics.LoginAttemptsTotal.WithLabelValues("succeeded").Inc()
	h.logAudit(r.Context(), audit.EventLoginSucceeded, userID, "")
	h.issueTokens(w, r.Context(), userID)
}

// EnrollTOTP generates a new secret for the authenticated caller and
// stores it as pending — 2FA isn't enabled until VerifyTOTP confirms the
// user's authenticator app actually has it right, so a broken enrollment
// can't lock anyone out.
func (h *AuthHandlers) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing or invalid access token")
		return
	}

	user, err := h.accounts.GetUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	secret, otpauthURL, err := auth.GenerateTOTPSecret(user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.accounts.SetPendingTOTPSecret(r.Context(), userID, secret); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, enrollTOTPResponse{Secret: secret, OTPAuthURL: otpauthURL})
}

// VerifyTOTP confirms enrollment by checking a code against the pending
// secret, and only then enables 2FA on the account.
func (h *AuthHandlers) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing or invalid access token")
		return
	}

	var req verifyTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	secret, err := h.accounts.GetTOTPSecret(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "no pending TOTP enrollment for this account")
		return
	}
	if !auth.ValidateTOTPCode(secret, req.Code) {
		writeError(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}

	if err := h.accounts.EnableTOTP(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logAudit(r.Context(), audit.EventTwoFactorEnabled, userID, "")
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
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
