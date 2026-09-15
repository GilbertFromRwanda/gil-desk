package registry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Presence tracks which devices are currently reachable and where, backed
// by Redis TTL keys (planner task G-11: heartbeats/TTL) — deliberately not
// PostgreSQL, since this is short-lived, high-churn data, not the durable
// device identity Store handles.
type Presence struct {
	client *redis.Client
	ttl    time.Duration
}

func NewPresence(client *redis.Client, ttl time.Duration) *Presence {
	return &Presence{client: client, ttl: ttl}
}

func (p *Presence) TTL() time.Duration {
	return p.ttl
}

// Heartbeat records (or refreshes) a device's endpoint, resetting its TTL.
func (p *Presence) Heartbeat(ctx context.Context, deviceID, endpoint string) error {
	if err := p.client.Set(ctx, presenceKey(deviceID), endpoint, p.ttl).Err(); err != nil {
		return fmt.Errorf("heartbeat %s: %w", deviceID, err)
	}
	return nil
}

// Endpoint returns the device's last-known endpoint and whether its
// presence has not yet expired. A device with no recent heartbeat is not
// an error — it's just offline.
func (p *Presence) Endpoint(ctx context.Context, deviceID string) (endpoint string, online bool, err error) {
	val, err := p.client.Get(ctx, presenceKey(deviceID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup presence for %s: %w", deviceID, err)
	}
	return val, true, nil
}

func presenceKey(deviceID string) string {
	return "presence:" + deviceID
}
