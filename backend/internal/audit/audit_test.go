package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/nexdesk/nexdesk/backend/internal/audit"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
)

// Real Postgres via testcontainers, not mocks — same reasoning as the
// other packages' integration tests.
func TestAuditLogAgainstRealPostgres(t *testing.T) {
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
	// budget is generous.
	if err := waitForPing(ctx, pool, 30, time.Second); err != nil {
		t.Fatalf("postgres never became reachable: %v", err)
	}

	if err := registry.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	logger := audit.NewLogger(pool)

	t.Run("logs an event with a user and metadata", func(t *testing.T) {
		if err := logger.Log(ctx, audit.EventLoginSucceeded, "user-1", "alice@example.com", map[string]any{
			"ip": "203.0.113.5",
		}); err != nil {
			t.Fatalf("Log: %v", err)
		}

		events, err := logger.Recent(ctx, 1)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].EventType != audit.EventLoginSucceeded {
			t.Fatalf("expected event type %q, got %q", audit.EventLoginSucceeded, events[0].EventType)
		}
		if events[0].UserID != "user-1" {
			t.Fatalf("expected user_id %q, got %q", "user-1", events[0].UserID)
		}
		if events[0].Subject != "alice@example.com" {
			t.Fatalf("expected subject %q, got %q", "alice@example.com", events[0].Subject)
		}
	})

	t.Run("logs an event with no user as NULL, not a fake value", func(t *testing.T) {
		if err := logger.Log(ctx, audit.EventLoginFailed, "", "unknown@example.com", nil); err != nil {
			t.Fatalf("Log: %v", err)
		}

		events, err := logger.Recent(ctx, 1)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].UserID != "" {
			t.Fatalf("expected empty user_id for an event with no associated user, got %q", events[0].UserID)
		}
	})

	t.Run("recent returns newest first and respects the limit", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			if err := logger.Log(ctx, audit.EventDeviceRegistered, "user-2", "device-x", nil); err != nil {
				t.Fatalf("Log: %v", err)
			}
		}

		events, err := logger.Recent(ctx, 3)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if len(events) != 3 {
			t.Fatalf("expected 3 events (limit), got %d", len(events))
		}
		for i := 0; i+1 < len(events); i++ {
			if events[i].CreatedAt.Before(events[i+1].CreatedAt) {
				t.Fatalf("expected events in newest-first order")
			}
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
