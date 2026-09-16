package rendezvous_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/audit"
	"github.com/nexdesk/nexdesk/backend/internal/auth"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
	"github.com/nexdesk/nexdesk/backend/internal/rendezvous"
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

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	// The postgres image restarts its server process once during first-run
	// initialization; the container can report "ready" a moment before the
	// post-restart listener is actually accepting connections again. That
	// gap can stretch well past a second when multiple packages' tests
	// are starting containers concurrently (go test ./... runs packages
	// in parallel by default) and contending for host resources — retry
	// generously rather than assume best-case timing.
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
	jwt := auth.NewTokenIssuer([]byte("test-jwt-key"), time.Minute)

	store := registry.NewStore(pool)
	presence := registry.NewPresence(redisClient, 30*time.Second)
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	auditLogger := audit.NewLogger(pool)
	service := rendezvous.NewService(store, presence, tokens, jwt, auditLogger)

	// newActor creates a fresh, uniquely-emailed user and returns an
	// authenticated context for it (JWT attached as gRPC metadata, the
	// same way a real client would send it).
	newActor := func(t *testing.T, emailLocalPart string) context.Context {
		t.Helper()
		user, err := accounts.CreateUser(ctx, emailLocalPart+"@example.com", "password123")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		token, err := jwt.IssueAccessToken(user.ID)
		if err != nil {
			t.Fatalf("IssueAccessToken: %v", err)
		}
		md := metadata.Pairs("authorization", "Bearer "+token)
		return metadata.NewIncomingContext(ctx, md)
	}

	t.Run("register then lookup finds the device but reports offline", func(t *testing.T) {
		authed := newActor(t, "user-a")
		_, err := service.RegisterDevice(authed, &nexdeskv1.RegisterDeviceRequest{
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
		authed := newActor(t, "user-b")
		_, err := service.RegisterDevice(authed, &nexdeskv1.RegisterDeviceRequest{
			DeviceId:  "device-b",
			PublicKey: "pubkey-b",
		})
		if err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}

		hbResp, err := service.Heartbeat(authed, &nexdeskv1.HeartbeatRequest{
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
		authed := newActor(t, "user-c")
		_, err := service.RegisterDevice(authed, &nexdeskv1.RegisterDeviceRequest{
			DeviceId:  "device-c",
			PublicKey: "old-key",
		})
		if err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}
		_, err = service.RegisterDevice(authed, &nexdeskv1.RegisterDeviceRequest{
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

	t.Run("session request is denied until the requester is authorized, then issues a valid token", func(t *testing.T) {
		ownerCtx := newActor(t, "target-owner")
		requesterCtx := newActor(t, "requester-owner")

		if _, err := service.RegisterDevice(ownerCtx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId: "target-device", PublicKey: "pubkey-target",
		}); err != nil {
			t.Fatalf("RegisterDevice(target-device): %v", err)
		}
		if _, err := service.RegisterDevice(requesterCtx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId: "requester-device", PublicKey: "pubkey-requester",
		}); err != nil {
			t.Fatalf("RegisterDevice(requester-device): %v", err)
		}

		denied, err := service.RequestSession(requesterCtx, &nexdeskv1.RequestSessionRequest{
			RequesterDeviceId: "requester-device",
			TargetDeviceId:    "target-device",
		})
		if err != nil {
			t.Fatalf("RequestSession (before authorization): %v", err)
		}
		if denied.GetAuthorized() {
			t.Fatal("expected session request to be denied before authorization")
		}
		if denied.GetSessionToken() != "" {
			t.Fatal("expected no session token when unauthorized")
		}

		authResp, err := service.AuthorizeDevice(ownerCtx, &nexdeskv1.AuthorizeDeviceRequest{
			OwnerDeviceId:   "target-device",
			AllowedDeviceId: "requester-device",
		})
		if err != nil {
			t.Fatalf("AuthorizeDevice: %v", err)
		}
		if !authResp.GetAccepted() {
			t.Fatal("expected AuthorizeDevice to be accepted")
		}

		granted, err := service.RequestSession(requesterCtx, &nexdeskv1.RequestSessionRequest{
			RequesterDeviceId: "requester-device",
			TargetDeviceId:    "target-device",
		})
		if err != nil {
			t.Fatalf("RequestSession (after authorization): %v", err)
		}
		if !granted.GetAuthorized() {
			t.Fatal("expected session request to be authorized")
		}
		if granted.GetSessionToken() == "" {
			t.Fatal("expected a non-empty session token")
		}
		if granted.GetExpiresInSeconds() == 0 {
			t.Fatal("expected a non-zero expiry")
		}

		claims, err := tokens.Verify(granted.GetSessionToken())
		if err != nil {
			t.Fatalf("issued token did not verify: %v", err)
		}
		if claims.RequesterDeviceID != "requester-device" || claims.TargetDeviceID != "target-device" {
			t.Fatalf("unexpected claims: %+v", claims)
		}
	})

	// This is the G-17 fix under direct test: before it, any caller could
	// claim to be any device_id in a request's fields. Now the caller's
	// identity comes from a verified JWT, not a request field.
	t.Run("a device cannot be acted on by anyone other than its owner", func(t *testing.T) {
		ownerCtx := newActor(t, "real-owner")
		attackerCtx := newActor(t, "attacker")

		if _, err := service.RegisterDevice(ownerCtx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId: "victim-device", PublicKey: "pubkey-victim",
		}); err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}

		// Attacker tries to re-register (hijack) the owner's device_id.
		_, err := service.RegisterDevice(attackerCtx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId: "victim-device", PublicKey: "attacker-key",
		})
		requirePermissionDenied(t, err, "re-registering someone else's device_id")

		// Attacker tries to send a heartbeat (spoof presence/endpoint) for
		// the owner's device.
		_, err = service.Heartbeat(attackerCtx, &nexdeskv1.HeartbeatRequest{
			DeviceId: "victim-device", Endpoint: "10.0.0.1:1",
		})
		requirePermissionDenied(t, err, "heartbeating someone else's device")

		// Attacker tries to authorize their own device to reach the
		// victim's, pretending to own the victim's device.
		_, err = service.AuthorizeDevice(attackerCtx, &nexdeskv1.AuthorizeDeviceRequest{
			OwnerDeviceId: "victim-device", AllowedDeviceId: "victim-device",
		})
		requirePermissionDenied(t, err, "authorizing access to someone else's device")

		// Attacker registers their own device, then tries to request a
		// session claiming to be the victim's device as the requester.
		if _, err := service.RegisterDevice(attackerCtx, &nexdeskv1.RegisterDeviceRequest{
			DeviceId: "attacker-device", PublicKey: "pubkey-attacker",
		}); err != nil {
			t.Fatalf("RegisterDevice(attacker-device): %v", err)
		}
		_, err = service.RequestSession(attackerCtx, &nexdeskv1.RequestSessionRequest{
			RequesterDeviceId: "victim-device", TargetDeviceId: "attacker-device",
		})
		requirePermissionDenied(t, err, "impersonating someone else's device as the requester")
	})

	t.Run("authenticated RPCs reject a missing or malformed access token", func(t *testing.T) {
		req := &nexdeskv1.RegisterDeviceRequest{DeviceId: "no-auth-device", PublicKey: "key"}

		if _, err := service.RegisterDevice(ctx, req); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected Unauthenticated with no metadata at all, got %v", err)
		}

		badMD := metadata.Pairs("authorization", "not-a-bearer-token")
		if _, err := service.RegisterDevice(metadata.NewIncomingContext(ctx, badMD), req); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected Unauthenticated for a non-Bearer authorization value, got %v", err)
		}

		garbageMD := metadata.Pairs("authorization", "Bearer not-a-real-jwt")
		if _, err := service.RegisterDevice(metadata.NewIncomingContext(ctx, garbageMD), req); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected Unauthenticated for a garbage token, got %v", err)
		}
	})
}

func requirePermissionDenied(t *testing.T, err error, action string) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied when %s, got %v", action, err)
	}
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
