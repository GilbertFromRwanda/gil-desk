package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/nexdesk/nexdesk/backend/internal/api"
	"github.com/nexdesk/nexdesk/backend/internal/audit"
	"github.com/nexdesk/nexdesk/backend/internal/auth"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
	"github.com/pquerna/otp/totp"
)

// Real Postgres + Redis via testcontainers, driving the actual HTTP
// handlers (not just the auth package's internal methods) — this proves
// the wiring (JSON decode, status codes, header parsing) works, not just
// the logic underneath it.
func TestTwoFactorLoginFlowAgainstRealPostgresAndRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-based integration test in -short mode")
	}
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("nexdesk"),
		tcpostgres.WithUsername("nexdesk"),
		tcpostgres.WithPassword("nexdesk"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := waitForPing(ctx, pool, 30, time.Second); err != nil {
		t.Fatalf("postgres never became reachable: %v", err)
	}
	if err := registry.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = redisContainer.Terminate(ctx) })
	redisURI, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis connection string: %v", err)
	}
	redisOpts, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("parse redis URI: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	t.Cleanup(func() { _ = redisClient.Close() })

	accounts := auth.NewAccountStore(pool)
	accessTokens := auth.NewTokenIssuer([]byte("test-jwt-key"), time.Hour)
	refreshTokens := auth.NewRefreshStore(pool, time.Hour)
	loginLimits := auth.NewRateLimiter(redisClient, 1000, time.Hour) // not what's under test here
	pendingLogins := auth.NewPendingLoginIssuer([]byte("test-pending-key"), time.Minute)
	auditLogger := audit.NewLogger(pool)
	handlers := api.NewAuthHandlers(accounts, accessTokens, refreshTokens, loginLimits, pendingLogins, auditLogger)

	// 1. Register.
	registerRec := post(t, handlers.Register, "/auth/register", `{"email":"totp-user@example.com","password":"password123"}`, "")
	if registerRec.Code != http.StatusOK {
		t.Fatalf("Register: expected 200, got %d: %s", registerRec.Code, registerRec.Body.String())
	}
	var registerResp struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, registerRec, &registerResp)

	// 2. Login before 2FA is enabled: should succeed immediately, no
	// two_factor_required flag.
	loginRec := post(t, handlers.Login, "/auth/login", `{"email":"totp-user@example.com","password":"password123"}`, "")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("Login (pre-2FA): expected 200, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	var preLoginResp struct {
		AccessToken       string `json:"access_token"`
		TwoFactorRequired bool   `json:"two_factor_required"`
	}
	decodeJSON(t, loginRec, &preLoginResp)
	if preLoginResp.TwoFactorRequired {
		t.Fatal("expected two_factor_required to be false before enrollment")
	}
	if preLoginResp.AccessToken == "" {
		t.Fatal("expected an access token before 2FA is enabled")
	}

	// 3. Enroll TOTP (requires the access token from registration).
	enrollRec := post(t, handlers.EnrollTOTP, "/auth/2fa/enroll", "", "Bearer "+registerResp.AccessToken)
	if enrollRec.Code != http.StatusOK {
		t.Fatalf("EnrollTOTP: expected 200, got %d: %s", enrollRec.Code, enrollRec.Body.String())
	}
	var enrollResp struct {
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	decodeJSON(t, enrollRec, &enrollResp)
	if enrollResp.Secret == "" || enrollResp.OTPAuthURL == "" {
		t.Fatal("expected a non-empty secret and otpauth URL")
	}

	// 4. Verify with a wrong code: must not enable 2FA.
	wrongCode := totpCodeOtherThan(t, enrollResp.Secret)
	verifyWrongRec := post(t, handlers.VerifyTOTP, "/auth/2fa/verify",
		`{"code":"`+wrongCode+`"}`, "Bearer "+registerResp.AccessToken)
	if verifyWrongRec.Code != http.StatusUnauthorized {
		t.Fatalf("VerifyTOTP (wrong code): expected 401, got %d: %s", verifyWrongRec.Code, verifyWrongRec.Body.String())
	}

	// 5. Verify with the real code: enables 2FA.
	code, err := totp.GenerateCode(enrollResp.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	verifyRec := post(t, handlers.VerifyTOTP, "/auth/2fa/verify", `{"code":"`+code+`"}`, "Bearer "+registerResp.AccessToken)
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("VerifyTOTP: expected 200, got %d: %s", verifyRec.Code, verifyRec.Body.String())
	}

	// 6. Login now requires 2FA instead of returning tokens directly.
	login2Rec := post(t, handlers.Login, "/auth/login", `{"email":"totp-user@example.com","password":"password123"}`, "")
	if login2Rec.Code != http.StatusOK {
		t.Fatalf("Login (post-2FA): expected 200, got %d: %s", login2Rec.Code, login2Rec.Body.String())
	}
	var challengeResp struct {
		TwoFactorRequired bool   `json:"two_factor_required"`
		PendingToken      string `json:"pending_token"`
		AccessToken       string `json:"access_token"`
	}
	decodeJSON(t, login2Rec, &challengeResp)
	if !challengeResp.TwoFactorRequired {
		t.Fatal("expected two_factor_required to be true after enrollment")
	}
	if challengeResp.AccessToken != "" {
		t.Fatal("expected no access token before the 2FA challenge is completed")
	}
	if challengeResp.PendingToken == "" {
		t.Fatal("expected a non-empty pending token")
	}

	// 7. Completing the challenge with a wrong code fails.
	wrongCode2 := totpCodeOtherThan(t, enrollResp.Secret)
	badChallengeRec := post(t, handlers.LoginTwoFactor, "/auth/login/2fa",
		`{"pending_token":"`+challengeResp.PendingToken+`","code":"`+wrongCode2+`"}`, "")
	if badChallengeRec.Code != http.StatusUnauthorized {
		t.Fatalf("LoginTwoFactor (wrong code): expected 401, got %d: %s", badChallengeRec.Code, badChallengeRec.Body.String())
	}

	// 8. Completing the challenge with the right code succeeds.
	finalCode, err := totp.GenerateCode(enrollResp.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	finalRec := post(t, handlers.LoginTwoFactor, "/auth/login/2fa",
		`{"pending_token":"`+challengeResp.PendingToken+`","code":"`+finalCode+`"}`, "")
	if finalRec.Code != http.StatusOK {
		t.Fatalf("LoginTwoFactor: expected 200, got %d: %s", finalRec.Code, finalRec.Body.String())
	}
	var finalResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, finalRec, &finalResp)
	if finalResp.AccessToken == "" || finalResp.RefreshToken == "" {
		t.Fatal("expected real tokens after completing the 2FA challenge")
	}
}

func post(t *testing.T, handler http.HandlerFunc, path, body, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
}

// totpCodeOtherThan returns a 6-digit code guaranteed not to be the
// currently-valid one for secret.
func totpCodeOtherThan(t *testing.T, secret string) string {
	t.Helper()
	real, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if real != "000000" {
		return "000000"
	}
	return "111111"
}

func waitForPing(ctx context.Context, pool *pgxpool.Pool, attempts int, delay time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if lastErr = pool.Ping(ctx); lastErr == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return lastErr
}
