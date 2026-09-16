package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

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
