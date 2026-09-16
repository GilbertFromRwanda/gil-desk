package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/nexdesk/nexdesk/backend/internal/auth"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
)

// Real Postgres via testcontainers, not mocks — same reasoning as
// internal/rendezvous's integration test.
func TestAccountsAndRefreshTokensAgainstRealPostgres(t *testing.T) {
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

	// See internal/rendezvous/rendezvous_test.go for why this retry
	// budget is generous: Postgres's one-time restart during first-run
	// init can take longer than expected under concurrent test load.
	if err := waitForPing(ctx, pool, 30, time.Second); err != nil {
		t.Fatalf("postgres never became reachable: %v", err)
	}

	if err := registry.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	accounts := auth.NewAccountStore(pool)
	refresh := auth.NewRefreshStore(pool, time.Minute)

	t.Run("create then verify password succeeds", func(t *testing.T) {
		user, err := accounts.CreateUser(ctx, "alice@example.com", "correct horse battery staple")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		verified, err := accounts.VerifyPassword(ctx, "alice@example.com", "correct horse battery staple")
		if err != nil {
			t.Fatalf("VerifyPassword: %v", err)
		}
		if verified.ID != user.ID {
			t.Fatalf("expected user ID %s, got %s", user.ID, verified.ID)
		}
	})

	t.Run("duplicate email is rejected", func(t *testing.T) {
		_, err := accounts.CreateUser(ctx, "bob@example.com", "password123")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		_, err = accounts.CreateUser(ctx, "bob@example.com", "different-password")
		if !errors.Is(err, auth.ErrEmailTaken) {
			t.Fatalf("expected ErrEmailTaken, got %v", err)
		}
	})

	t.Run("wrong password and unknown email are indistinguishable", func(t *testing.T) {
		_, err := accounts.CreateUser(ctx, "carol@example.com", "the-real-password")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if _, err := accounts.VerifyPassword(ctx, "carol@example.com", "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials, got %v", err)
		}
		if _, err := accounts.VerifyPassword(ctx, "no-such-user@example.com", "whatever"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials for unknown email too, got %v", err)
		}
	})

	t.Run("refresh token rotation is single-use", func(t *testing.T) {
		user, err := accounts.CreateUser(ctx, "dave@example.com", "password123")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		raw, err := refresh.Issue(ctx, user.ID)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}

		userID, next, err := refresh.Rotate(ctx, raw)
		if err != nil {
			t.Fatalf("Rotate: %v", err)
		}
		if userID != user.ID {
			t.Fatalf("expected user ID %s, got %s", user.ID, userID)
		}
		if next == raw {
			t.Fatal("expected a new token, got the same one back")
		}

		if _, _, err := refresh.Rotate(ctx, raw); !errors.Is(err, auth.ErrInvalidRefreshToken) {
			t.Fatalf("expected ErrInvalidRefreshToken on reuse of an already-rotated token, got %v", err)
		}
		if _, _, err := refresh.Rotate(ctx, next); err != nil {
			t.Fatalf("expected the rotated token to still be valid: %v", err)
		}
	})

	t.Run("unknown refresh token is rejected", func(t *testing.T) {
		if _, _, err := refresh.Rotate(ctx, "not-a-real-token"); !errors.Is(err, auth.ErrInvalidRefreshToken) {
			t.Fatalf("expected ErrInvalidRefreshToken, got %v", err)
		}
	})
}

// Real Redis via testcontainers, not mocks.
func TestRateLimiterAgainstRealRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-based integration test in -short mode")
	}
	ctx := context.Background()

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

	t.Run("allows up to the limit then blocks", func(t *testing.T) {
		limiter := auth.NewRateLimiter(redisClient, 3, time.Minute)
		for i := 0; i < 3; i++ {
			allowed, err := limiter.Allow(ctx, "user-x@example.com")
			if err != nil {
				t.Fatalf("Allow (attempt %d): %v", i+1, err)
			}
			if !allowed {
				t.Fatalf("expected attempt %d to be allowed (limit is 3)", i+1)
			}
		}
		allowed, err := limiter.Allow(ctx, "user-x@example.com")
		if err != nil {
			t.Fatalf("Allow (4th attempt): %v", err)
		}
		if allowed {
			t.Fatal("expected the 4th attempt to be blocked")
		}
	})

	t.Run("reset restores access", func(t *testing.T) {
		limiter := auth.NewRateLimiter(redisClient, 1, time.Minute)
		key := "user-y@example.com"

		if allowed, err := limiter.Allow(ctx, key); err != nil || !allowed {
			t.Fatalf("expected first attempt allowed, got allowed=%v err=%v", allowed, err)
		}
		if allowed, err := limiter.Allow(ctx, key); err != nil || allowed {
			t.Fatalf("expected second attempt blocked, got allowed=%v err=%v", allowed, err)
		}

		if err := limiter.Reset(ctx, key); err != nil {
			t.Fatalf("Reset: %v", err)
		}

		if allowed, err := limiter.Allow(ctx, key); err != nil || !allowed {
			t.Fatalf("expected attempt allowed after reset, got allowed=%v err=%v", allowed, err)
		}
	})

	t.Run("different keys are independent", func(t *testing.T) {
		limiter := auth.NewRateLimiter(redisClient, 1, time.Minute)

		if allowed, err := limiter.Allow(ctx, "user-z1@example.com"); err != nil || !allowed {
			t.Fatalf("expected user-z1's first attempt allowed, got allowed=%v err=%v", allowed, err)
		}
		if allowed, err := limiter.Allow(ctx, "user-z1@example.com"); err != nil || allowed {
			t.Fatalf("expected user-z1's second attempt blocked, got allowed=%v err=%v", allowed, err)
		}
		// A different key must not be affected by user-z1 being blocked.
		if allowed, err := limiter.Allow(ctx, "user-z2@example.com"); err != nil || !allowed {
			t.Fatalf("expected user-z2's first attempt allowed, got allowed=%v err=%v", allowed, err)
		}
	})
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
