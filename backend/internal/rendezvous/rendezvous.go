// Package rendezvous implements device registration, endpoint registry, and
// peer lookup as a gRPC service (planner tasks G-08..G-11). Session
// authorization (G-12) is not implemented yet.
package rendezvous

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
	"github.com/nexdesk/nexdesk/backend/internal/registry"
)

type Service struct {
	nexdeskv1.UnimplementedRendezvousServiceServer
	store    *registry.Store
	presence *registry.Presence
}

func NewService(store *registry.Store, presence *registry.Presence) *Service {
	return &Service{store: store, presence: presence}
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
