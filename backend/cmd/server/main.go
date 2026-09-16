// Command server is the NexDesk backend entrypoint (planner tasks G-01,
// G-02, G-06, G-07).
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/api"
	"github.com/nexdesk/nexdesk/backend/internal/audit"
	"github.com/nexdesk/nexdesk/backend/internal/auth"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
	"github.com/nexdesk/nexdesk/backend/internal/rendezvous"
)

const (
	presenceTTL            = 30 * time.Second
	sessionTokenTTL        = 60 * time.Second
	accessTokenTTL         = 15 * time.Minute
	refreshTokenTTL        = 30 * 24 * time.Hour
	loginRateLimitAttempts = 5
	loginRateLimitWindow   = 15 * time.Minute
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	postgresURL := getenv("NEXDESK_POSTGRES_URL", "postgres://nexdesk:nexdesk@localhost:5432/nexdesk")
	redisAddr := getenv("NEXDESK_REDIS_ADDR", "localhost:6379")
	httpAddr := getenv("NEXDESK_ADDR", ":8080")
	grpcAddr := getenv("NEXDESK_GRPC_ADDR", ":9090")

	pool, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		slog.Error("connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := registry.RunMigrations(ctx, pool); err != nil {
		slog.Error("run migrations", "error", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()

	sessionSigningKey, err := signingKey("NEXDESK_SESSION_SIGNING_KEY")
	if err != nil {
		slog.Error("session signing key", "error", err)
		os.Exit(1)
	}
	jwtSigningKey, err := signingKey("NEXDESK_JWT_SIGNING_KEY")
	if err != nil {
		slog.Error("jwt signing key", "error", err)
		os.Exit(1)
	}

	auditLogger := audit.NewLogger(pool)

	accounts := auth.NewAccountStore(pool)
	accessTokens := auth.NewTokenIssuer(jwtSigningKey, accessTokenTTL)
	refreshTokens := auth.NewRefreshStore(pool, refreshTokenTTL)
	loginLimits := auth.NewRateLimiter(redisClient, loginRateLimitAttempts, loginRateLimitWindow)
	authHandlers := api.NewAuthHandlers(accounts, accessTokens, refreshTokens, loginLimits, auditLogger)

	store := registry.NewStore(pool)
	presence := registry.NewPresence(redisClient, presenceTTL)
	sessionTokens := rendezvous.NewTokenIssuer(sessionSigningKey, sessionTokenTTL)
	rendezvousService := rendezvous.NewService(store, presence, sessionTokens, accessTokens, auditLogger)

	grpcServer := grpc.NewServer()
	nexdeskv1.RegisterRendezvousServiceServer(grpcServer, rendezvousService)

	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		slog.Error("listen grpc", "addr", grpcAddr, "error", err)
		os.Exit(1)
	}
	go func() {
		slog.Info("grpc listening", "addr", grpcAddr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			slog.Error("grpc serve", "error", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", readyzHandler(pool, redisClient))
	mux.HandleFunc("POST /auth/register", authHandlers.Register)
	mux.HandleFunc("POST /auth/login", authHandlers.Login)
	mux.HandleFunc("POST /auth/refresh", authHandlers.Refresh)
	httpServer := &http.Server{Addr: httpAddr, Handler: mux}

	go func() {
		slog.Info("http listening", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http serve", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	grpcServer.GracefulStop()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyzHandler reports ready only once Postgres and Redis are actually
// reachable — closes the TODO that used to be here.
func readyzHandler(pool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not ready", "reason": "postgres: " + err.Error(),
			})
			return
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not ready", "reason": "redis: " + err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// signingKey reads envVar, or generates a random key if unset. A generated
// key is ephemeral — it changes on every restart, invalidating any
// outstanding tokens signed with it — which is fine for the short-lived
// session-authorization token (60s TTL), but a production deployment
// should set NEXDESK_JWT_SIGNING_KEY explicitly: a generated one would
// invalidate every user's session on every restart.
func signingKey(envVar string) ([]byte, error) {
	if v := os.Getenv(envVar); v != "" {
		return []byte(v), nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	slog.Warn(envVar+" not set; generated an ephemeral key for this process only", "env_var", envVar)
	return key, nil
}
