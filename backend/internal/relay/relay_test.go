package relay_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/relay"
	"github.com/nexdesk/nexdesk/backend/internal/rendezvous"
)

// Mirrors relay.go's own unexported constant — a duplicate rather than
// an export purely for this package's tests, since it's part of the
// wire contract (relay.proto's own docs), not an implementation detail
// worth exporting just to avoid repeating a string in tests.
const sessionTokenMetadataKey = "x-nexdesk-session-token"

// A real gRPC server (bufconn — an in-memory listener, not mocked RPC
// calls) is required here, not direct method calls like other packages'
// tests use: relay.Service.Stream needs two genuinely concurrent
// streaming calls to pair with each other, which only a real client/server
// round trip exercises.
func newTestServer(t *testing.T, tokens *rendezvous.TokenIssuer) nexdeskv1.RelayServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	nexdeskv1.RegisterRelayServiceServer(server, relay.NewService(tokens))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return nexdeskv1.NewRelayServiceClient(conn)
}

func openStream(t *testing.T, client nexdeskv1.RelayServiceClient, token string) nexdeskv1.RelayService_StreamClient {
	t.Helper()
	ctx := metadata.AppendToOutgoingContext(context.Background(), sessionTokenMetadataKey, token)
	stream, err := client.Stream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	return stream
}

func TestRelayPairsTwoStreamsAndForwardsBothDirections(t *testing.T) {
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	token, _ := tokens.Issue("device-requester", "device-target")
	client := newTestServer(t, tokens)

	a := openStream(t, client, token)
	b := openStream(t, client, token)

	// requester (a) -> target (b)
	if err := a.Send(&nexdeskv1.RelayFrame{Data: []byte("hello from a")}); err != nil {
		t.Fatalf("a.Send: %v", err)
	}
	got, err := b.Recv()
	if err != nil {
		t.Fatalf("b.Recv: %v", err)
	}
	if string(got.GetData()) != "hello from a" {
		t.Fatalf("b got %q, want %q", got.GetData(), "hello from a")
	}

	// target (b) -> requester (a)
	if err := b.Send(&nexdeskv1.RelayFrame{Data: []byte("hello from b")}); err != nil {
		t.Fatalf("b.Send: %v", err)
	}
	got, err = a.Recv()
	if err != nil {
		t.Fatalf("a.Recv: %v", err)
	}
	if string(got.GetData()) != "hello from b" {
		t.Fatalf("a got %q, want %q", got.GetData(), "hello from b")
	}

	// Several frames each direction, in order, proving this isn't a
	// one-shot pairing fluke.
	for i := 0; i < 5; i++ {
		payload := []byte{byte(i)}
		if err := a.Send(&nexdeskv1.RelayFrame{Data: payload}); err != nil {
			t.Fatalf("a.Send #%d: %v", i, err)
		}
		got, err := b.Recv()
		if err != nil {
			t.Fatalf("b.Recv #%d: %v", i, err)
		}
		if len(got.GetData()) != 1 || got.GetData()[0] != byte(i) {
			t.Fatalf("b.Recv #%d = %v, want [%d]", i, got.GetData(), i)
		}
	}
}

func TestRelayRejectsAMissingSessionToken(t *testing.T) {
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	client := newTestServer(t, tokens)

	// No sessionTokenMetadataKey attached — the server checks for it
	// before even trying to Recv() a frame.
	stream, err := client.Stream(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

func TestRelayRejectsAnInvalidSessionToken(t *testing.T) {
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	client := newTestServer(t, tokens)

	stream := openStream(t, client, "not-a-real-token")
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("got %v, want Unauthenticated", err)
	}
}

func TestRelayRejectsAnExpiredSessionToken(t *testing.T) {
	// A real, very short TTL plus an actual sleep past it — rendezvous.TokenIssuer
	// doesn't expose its clock for injection outside its own package, so
	// this proves expiry the direct way rather than skipping it.
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), 20*time.Millisecond)
	token, _ := tokens.Issue("device-requester", "device-target")
	time.Sleep(50 * time.Millisecond)
	client := newTestServer(t, tokens)

	stream := openStream(t, client, token)
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("got %v, want Unauthenticated", err)
	}
}

func TestRelayDoesNotCrossWireDifferentTokenPairs(t *testing.T) {
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	tokenAB, _ := tokens.Issue("device-a", "device-b")
	tokenCD, _ := tokens.Issue("device-c", "device-d")
	client := newTestServer(t, tokens)

	a := openStream(t, client, tokenAB)
	c := openStream(t, client, tokenCD)

	if err := a.Send(&nexdeskv1.RelayFrame{Data: []byte("for b only")}); err != nil {
		t.Fatalf("a.Send: %v", err)
	}

	// Neither a nor c has a partner yet (b and d never connected), so
	// both should simply be waiting, not have received each other's
	// frame. Confirm via a short-lived context: Recv must not return
	// c's own unrelated data within a short window.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	done := make(chan struct{})
	go func() {
		_, _ = c.Recv()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("c.Recv returned, but c was never paired with a's token — it must not receive anything")
	case <-waitCtx.Done():
		// expected: c is still blocked waiting for its own pair (device-d)
	}
}

// TestRelayDoesNotPairAcrossInstances documents a real, deliberately
// unfixed limitation found running the planner's G-30 horizontal
// scaling test against two real backend processes sharing the same
// Postgres/Redis and signing keys: everything backed by shared storage
// (accounts, JWTs, device registry, AuthorizeDevice's ACL, presence)
// correctly worked across instances — only relay pairing didn't, because
// Service.waiting is an in-memory, per-process map. Two peers whose
// relay connections happen to land on different replicas behind a load
// balancer would each wait forever, never knowing the other exists.
//
// This is captured here as a permanent regression-relevant test (two
// separate Service instances sharing only a token issuer, standing in
// for two real processes sharing only Postgres/Redis) so the boundary
// stays *known and tested*, not just discovered once and forgotten.
// Fixing it for real needs either infrastructure-level sticky routing
// (keyed on the session token — now gRPC metadata a load balancer can
// actually see and route on, rather than being buried inside the first
// stream message the way it was when this test was first written; that
// move was the real prerequisite fix, done, but the routing itself
// isn't) or a shared coordination layer (e.g. Redis pub/sub relaying
// frames between instances) — real, separate design work the planner
// already scopes as G-31 ("multi-region design"), not implemented here.
func TestRelayDoesNotPairAcrossInstances(t *testing.T) {
	tokens := rendezvous.NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	token, _ := tokens.Issue("device-requester", "device-target")

	instanceA := newTestServer(t, tokens)
	instanceB := newTestServer(t, tokens)

	onA := openStream(t, instanceA, token)
	onB := openStream(t, instanceB, token)

	if err := onA.Send(&nexdeskv1.RelayFrame{Data: []byte("hello from instance A")}); err != nil {
		t.Fatalf("onA.Send: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = onB.Recv()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("onB.Recv returned — if this starts passing, relay pairing has become cross-instance-aware; " +
			"update this test's docs (and TestRelayDoesNotPairAcrossInstances's name) to match, don't just delete it")
	case <-ctx.Done():
		// expected today: onB never sees onA's frame, because they're on
		// two Service instances with independent in-memory waiting maps
	}
}
