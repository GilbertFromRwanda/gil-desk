// Package relay implements the byte relay used when two peers can't
// establish a direct connection (planner Gate G3's "if direct fails ->
// relay" step, tasks G-20/G-21). It has no database and no state beyond
// the pairing map below — every trust decision it needs was already made
// by rendezvous.Service.RequestSession (G-12), and is captured entirely
// in the session token both sides present.
package relay

import (
	"errors"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/rendezvous"
)

// TokenVerifier is the subset of rendezvous.TokenIssuer this package
// needs — a named interface (rather than depending on *rendezvous.TokenIssuer
// directly) so relay doesn't otherwise couple to the rendezvous package.
type TokenVerifier interface {
	Verify(token string) (*rendezvous.SessionClaims, error)
}

type Service struct {
	nexdeskv1.UnimplementedRelayServiceServer
	tokens TokenVerifier

	mu      sync.Mutex
	waiting map[string]*waitingPeer
}

type waitingPeer struct {
	stream  nexdeskv1.RelayService_StreamServer
	matched chan nexdeskv1.RelayService_StreamServer
}

func NewService(tokens TokenVerifier) *Service {
	return &Service{tokens: tokens, waiting: make(map[string]*waitingPeer)}
}

// Stream pairs this call with whichever other call (if any) presents the
// same session token, then forwards frames between them until either
// side disconnects. The first frame from each side must carry the token;
// every frame after that is treated as opaque data to forward.
func (s *Service) Stream(stream nexdeskv1.RelayService_StreamServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	token := first.GetSessionToken()
	if token == "" {
		return status.Error(codes.InvalidArgument, "first frame must carry a session_token")
	}
	if _, err := s.tokens.Verify(token); err != nil {
		return status.Error(codes.Unauthenticated, "invalid or expired session token")
	}

	s.mu.Lock()
	if peer, ok := s.waiting[token]; ok {
		delete(s.waiting, token)
		s.mu.Unlock()

		// We're the second side to arrive: hand our stream to the side
		// that's already waiting, then drive our half of the pipe (read
		// our own stream, write to theirs — the mirror image of what
		// their own goroutine, unblocked below, now does).
		peer.matched <- stream
		slog.Info("relay paired two streams")
		return pipeOneDirection(stream, peer.stream)
	}

	w := &waitingPeer{stream: stream, matched: make(chan nexdeskv1.RelayService_StreamServer, 1)}
	s.waiting[token] = w
	s.mu.Unlock()

	select {
	case peerStream := <-w.matched:
		return pipeOneDirection(stream, peerStream)
	case <-stream.Context().Done():
		s.mu.Lock()
		// Only remove ourselves if we're still the ones waiting — a
		// concurrent pairing could have already deleted (and replaced
		// our slot's meaning) between the peer send above and this lock.
		if s.waiting[token] == w {
			delete(s.waiting, token)
		}
		s.mu.Unlock()
		return stream.Context().Err()
	}
}

// pipeOneDirection reads only from src (this call's own stream — every
// gRPC stream must only ever be Recv'd from a single goroutine) and
// writes only to dst (the peer's stream, whose own handler goroutine is
// the one Recv-ing it) — so each of the two streams ends up with exactly
// one goroutine calling Recv and a different single goroutine calling
// Send, which is the concurrency pattern grpc-go requires.
func pipeOneDirection(src, dst nexdeskv1.RelayService_StreamServer) error {
	for {
		frame, err := src.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if frame.GetSessionToken() != "" {
			return status.Error(codes.InvalidArgument, "session_token frame received after the first frame")
		}
		if err := dst.Send(frame); err != nil {
			return err
		}
	}
}
