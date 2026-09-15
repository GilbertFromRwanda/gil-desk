// Package registry backs the device/session registry on top of PostgreSQL
// (durable device identity) and Redis (short-lived presence — planner
// tasks G-03..G-05).
package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Device struct {
	DeviceID  string
	PublicKey string
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpsertDevice registers a device, or updates its public key if it's
// already registered — registration is idempotent by design, since a
// client may legitimately re-register (new key, or just a retry).
func (s *Store) UpsertDevice(ctx context.Context, deviceID, publicKey string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO devices (device_id, public_key, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (device_id) DO UPDATE
			SET public_key = EXCLUDED.public_key, updated_at = now()
	`, deviceID, publicKey)
	if err != nil {
		return fmt.Errorf("upsert device %s: %w", deviceID, err)
	}
	return nil
}

// GetDevice returns (nil, nil) if the device isn't registered — that's a
// valid outcome for a lookup, not an error.
func (s *Store) GetDevice(ctx context.Context, deviceID string) (*Device, error) {
	var d Device
	err := s.pool.QueryRow(ctx,
		`SELECT device_id, public_key FROM devices WHERE device_id = $1`, deviceID,
	).Scan(&d.DeviceID, &d.PublicKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", deviceID, err)
	}
	return &d, nil
}

// AuthorizeDevice grants allowedDeviceID permission to request a session
// with ownerDeviceID (planner task G-12). Idempotent — authorizing an
// already-authorized pair is not an error.
func (s *Store) AuthorizeDevice(ctx context.Context, ownerDeviceID, allowedDeviceID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO device_authorizations (owner_device_id, allowed_device_id)
		VALUES ($1, $2)
		ON CONFLICT (owner_device_id, allowed_device_id) DO NOTHING
	`, ownerDeviceID, allowedDeviceID)
	if err != nil {
		return fmt.Errorf("authorize device %s -> %s: %w", allowedDeviceID, ownerDeviceID, err)
	}
	return nil
}

// IsAuthorized reports whether requesterDeviceID may request a session
// with targetDeviceID.
func (s *Store) IsAuthorized(ctx context.Context, targetDeviceID, requesterDeviceID string) (bool, error) {
	var authorized bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM device_authorizations
			WHERE owner_device_id = $1 AND allowed_device_id = $2
		)
	`, targetDeviceID, requesterDeviceID).Scan(&authorized)
	if err != nil {
		return false, fmt.Errorf("check authorization %s -> %s: %w", requesterDeviceID, targetDeviceID, err)
	}
	return authorized, nil
}
