// Package rendezvous implements device registration, endpoint registry,
// peer lookup, and session authorization as a gRPC service (planner tasks
// G-08..G-12). RegisterDevice, Heartbeat, AuthorizeDevice, and
// RequestSession all require an authenticated caller (planner task G-17):
// each verifies the caller's JWT access token and checks it owns the
// device_id(s) it's acting on. LookupPeer is deliberately left open —
// it's read-only, doesn't reveal ownership, and requiring auth for it
// would block discovering a peer before pairing has happened.
package rendezvous

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/auth"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
)

type Service struct {
	nexdeskv1.UnimplementedRendezvousServiceServer
	store    *registry.Store
	presence *registry.Presence
	tokens   *TokenIssuer
	jwt      *auth.TokenIssuer
}

func NewService(store *registry.Store, presence *registry.Presence, tokens *TokenIssuer, jwt *auth.TokenIssuer) *Service {
	return &Service{store: store, presence: presence, tokens: tokens, jwt: jwt}
}

// authenticate extracts and verifies the caller's JWT access token from
// the "authorization: Bearer <token>" gRPC metadata, returning the
// authenticated user ID.
func (s *Service) authenticate(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing request metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	token, ok := strings.CutPrefix(values[0], "Bearer ")
	if !ok {
		return "", status.Error(codes.Unauthenticated, "authorization metadata must be a Bearer token")
	}
	claims, err := s.jwt.VerifyAccessToken(token)
	if err != nil {
		return "", status.Errorf(codes.Unauthenticated, "invalid access token: %v", err)
	}
	return claims.UserID, nil
}

func (s *Service) RegisterDevice(
	ctx context.Context, req *nexdeskv1.RegisterDeviceRequest,
) (*nexdeskv1.RegisterDeviceResponse, error) {
	userID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetDeviceId() == "" || req.GetPublicKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id and public_key are required")
	}
	if err := s.store.UpsertDevice(ctx, req.GetDeviceId(), req.GetPublicKey(), userID); err != nil {
		if errors.Is(err, registry.ErrOwnershipMismatch) {
			return nil, status.Error(codes.PermissionDenied, "device_id is registered to a different account")
		}
		return nil, status.Errorf(codes.Internal, "register device: %v", err)
	}
	return &nexdeskv1.RegisterDeviceResponse{Accepted: true}, nil
}

func (s *Service) Heartbeat(
	ctx context.Context, req *nexdeskv1.HeartbeatRequest,
) (*nexdeskv1.HeartbeatResponse, error) {
	userID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if err := s.requireOwner(ctx, req.GetDeviceId(), userID); err != nil {
		return nil, err
	}

	if err := s.presence.Heartbeat(ctx, req.GetDeviceId(), req.GetEndpoint()); err != nil {
		return nil, status.Errorf(codes.Internal, "heartbeat: %v", err)
	}
	return &nexdeskv1.HeartbeatResponse{
		Accepted:   true,
		TtlSeconds: uint32(s.presence.TTL().Seconds()),
	}, nil
}

func (s *Service) LookupPeer(
	ctx context.Context, req *nexdeskv1.LookupPeerRequest,
) (*nexdeskv1.LookupPeerResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}

	device, err := s.store.GetDevice(ctx, req.GetDeviceId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "lookup device: %v", err)
	}
	if device == nil {
		return &nexdeskv1.LookupPeerResponse{Found: false}, nil
	}

	endpoint, online, err := s.presence.Endpoint(ctx, req.GetDeviceId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "lookup presence: %v", err)
	}

	return &nexdeskv1.LookupPeerResponse{
		Found:     true,
		PublicKey: device.PublicKey,
		Online:    online,
		Endpoint:  endpoint,
	}, nil
}

func (s *Service) AuthorizeDevice(
	ctx context.Context, req *nexdeskv1.AuthorizeDeviceRequest,
) (*nexdeskv1.AuthorizeDeviceResponse, error) {
	userID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetOwnerDeviceId() == "" || req.GetAllowedDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_device_id and allowed_device_id are required")
	}
	if err := s.requireOwner(ctx, req.GetOwnerDeviceId(), userID); err != nil {
		return nil, err
	}

	if err := s.store.AuthorizeDevice(ctx, req.GetOwnerDeviceId(), req.GetAllowedDeviceId()); err != nil {
		return nil, status.Errorf(codes.Internal, "authorize device: %v", err)
	}
	return &nexdeskv1.AuthorizeDeviceResponse{Accepted: true}, nil
}

func (s *Service) RequestSession(
	ctx context.Context, req *nexdeskv1.RequestSessionRequest,
) (*nexdeskv1.RequestSessionResponse, error) {
	userID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetRequesterDeviceId() == "" || req.GetTargetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "requester_device_id and target_device_id are required")
	}
	if err := s.requireOwner(ctx, req.GetRequesterDeviceId(), userID); err != nil {
		return nil, err
	}

	authorized, err := s.store.IsAuthorized(ctx, req.GetTargetDeviceId(), req.GetRequesterDeviceId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check authorization: %v", err)
	}
	if !authorized {
		return &nexdeskv1.RequestSessionResponse{Authorized: false}, nil
	}

	token, expiresAt := s.tokens.Issue(req.GetRequesterDeviceId(), req.GetTargetDeviceId())
	return &nexdeskv1.RequestSessionResponse{
		Authorized:       true,
		SessionToken:     token,
		ExpiresInSeconds: uint32(time.Until(expiresAt).Seconds()),
	}, nil
}

// requireOwner returns a PermissionDenied status unless userID owns
// deviceID — the shared check behind every RPC that acts on a specific
// device on the caller's behalf.
func (s *Service) requireOwner(ctx context.Context, deviceID, userID string) error {
	isOwner, err := s.store.IsDeviceOwner(ctx, deviceID, userID)
	if err != nil {
		return status.Errorf(codes.Internal, "check device ownership: %v", err)
	}
	if !isOwner {
		return status.Errorf(codes.PermissionDenied, "you do not own device %s", deviceID)
	}
	return nil
}
