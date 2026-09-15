// Package rendezvous implements device registration, endpoint registry,
// peer lookup, and session authorization as a gRPC service (planner tasks
// G-08..G-12).
package rendezvous

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
)

type Service struct {
	nexdeskv1.UnimplementedRendezvousServiceServer
	store    *registry.Store
	presence *registry.Presence
	tokens   *TokenIssuer
}

func NewService(store *registry.Store, presence *registry.Presence, tokens *TokenIssuer) *Service {
	return &Service{store: store, presence: presence, tokens: tokens}
}

func (s *Service) RegisterDevice(
	ctx context.Context, req *nexdeskv1.RegisterDeviceRequest,
) (*nexdeskv1.RegisterDeviceResponse, error) {
	if req.GetDeviceId() == "" || req.GetPublicKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id and public_key are required")
	}
	if err := s.store.UpsertDevice(ctx, req.GetDeviceId(), req.GetPublicKey()); err != nil {
		return nil, status.Errorf(codes.Internal, "register device: %v", err)
	}
	return &nexdeskv1.RegisterDeviceResponse{Accepted: true}, nil
}

func (s *Service) Heartbeat(
	ctx context.Context, req *nexdeskv1.HeartbeatRequest,
) (*nexdeskv1.HeartbeatResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
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
	if req.GetOwnerDeviceId() == "" || req.GetAllowedDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_device_id and allowed_device_id are required")
	}
	if err := s.store.AuthorizeDevice(ctx, req.GetOwnerDeviceId(), req.GetAllowedDeviceId()); err != nil {
		return nil, status.Errorf(codes.Internal, "authorize device: %v", err)
	}
	return &nexdeskv1.AuthorizeDeviceResponse{Accepted: true}, nil
}

func (s *Service) RequestSession(
	ctx context.Context, req *nexdeskv1.RequestSessionRequest,
) (*nexdeskv1.RequestSessionResponse, error) {
	if req.GetRequesterDeviceId() == "" || req.GetTargetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "requester_device_id and target_device_id are required")
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
