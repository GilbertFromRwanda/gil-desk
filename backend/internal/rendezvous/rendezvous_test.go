package rendezvous_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/nexdesk/nexdesk/backend/internal/registry"
	"github.com/nexdesk/nexdesk/backend/internal/rendezvous"
	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
)

// Real Postgres + Redis via testcontainers — not mocked. This is exactly
// the kind of boundary (Go <-> PostgreSQL, Go <-> Redis) the planner's
// testing strategy (Section 16) calls out as a required integration test.
func TestRendezvousServiceAgainstRealPostgresAndRedis(t *testing.T) {
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
	t.Logf("postgres connection string: %s", connStr)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	// The postgres image restarts its server process once during first-run
	// initialization; the container can report "ready" a moment before the
	// post-restart listener is actually accepting connections. Retry
	// rather than fail on that race.
	if err := waitForPing(ctx, pool, 10, 500*time.Millisecond); err != nil {
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

	store := registry.NewStore(pool)
	presence := registry.NewPresence(redisClient, 30*time.Second)
	service := rendezvous.NewService(store, presence)

	t.Run("register then lookup finds the device but reports offline", func(t *testing.T) {
		_, err := service.RegisterDevice(ctx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId:  "device-a",
			PublicKey: "pubkey-a",
		})
		if err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}

		resp, err := service.LookupPeer(ctx, &nexdeskv1.LookupPeerRequest{DeviceId: "device-a"})
		if err != nil {
			t.Fatalf("LookupPeer: %v", err)
		}
		if !resp.GetFound() {
			t.Fatal("expected device to be found")
		}
		if resp.GetPublicKey() != "pubkey-a" {
			t.Fatalf("expected public key %q, got %q", "pubkey-a", resp.GetPublicKey())
		}
		if resp.GetOnline() {
			t.Fatal("expected device to be offline (no heartbeat sent yet)")
		}
	})

	t.Run("heartbeat makes the device look up as online with its endpoint", func(t *testing.T) {
		_, err := service.RegisterDevice(ctx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId:  "device-b",
			PublicKey: "pubkey-b",
		})
		if err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}

		hbResp, err := service.Heartbeat(ctx, &nexdeskv1.HeartbeatRequest{
			DeviceId: "device-b",
			Endpoint: "203.0.113.5:9000",
		})
		if err != nil {
			t.Fatalf("Heartbeat: %v", err)
		}
		if !hbResp.GetAccepted() {
			t.Fatal("expected heartbeat to be accepted")
		}
		if hbResp.GetTtlSeconds() == 0 {
			t.Fatal("expected a non-zero TTL")
		}

		resp, err := service.LookupPeer(ctx, &nexdeskv1.LookupPeerRequest{DeviceId: "device-b"})
		if err != nil {
			t.Fatalf("LookupPeer: %v", err)
		}
		if !resp.GetOnline() {
			t.Fatal("expected device to be online after heartbeat")
		}
		if resp.GetEndpoint() != "203.0.113.5:9000" {
			t.Fatalf("expected endpoint %q, got %q", "203.0.113.5:9000", resp.GetEndpoint())
		}
	})

	t.Run("lookup of an unregistered device is not an error", func(t *testing.T) {
		resp, err := service.LookupPeer(ctx, &nexdeskv1.LookupPeerRequest{DeviceId: "never-registered"})
		if err != nil {
			t.Fatalf("LookupPeer: %v", err)
		}
		if resp.GetFound() {
			t.Fatal("expected device not to be found")
		}
	})

	t.Run("re-registering a device updates its public key", func(t *testing.T) {
		_, err := service.RegisterDevice(ctx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId:  "device-c",
			PublicKey: "old-key",
		})
		if err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}
		_, err = service.RegisterDevice(ctx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId:  "device-c",
			PublicKey: "new-key",
		})
		if err != nil {
			t.Fatalf("RegisterDevice (re-register): %v", err)
		}

		resp, err := service.LookupPeer(ctx, &nexdeskv1.LookupPeerRequest{DeviceId: "device-c"})
		if err != nil {
			t.Fatalf("LookupPeer: %v", err)
		}
		if resp.GetPublicKey() != "new-key" {
			t.Fatalf("expected updated public key %q, got %q", "new-key", resp.GetPublicKey())
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
