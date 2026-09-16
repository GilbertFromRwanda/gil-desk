package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

// RefreshStore backs refresh-token rotation (planner task G-15). Only the
// SHA-256 hash of each token is stored — a database leak alone shouldn't
// yield usable tokens, only a leak plus a working preimage attack on
// SHA-256, which is not a realistic threat.
type RefreshStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewRefreshStore(pool *pgxpool.Pool, ttl time.Duration) *RefreshStore {
	return &RefreshStore{pool: pool, ttl: ttl}
}

// Issue creates a new refresh token for userID and returns the raw token
// (given to the client) — only its hash is ever persisted.
func (s *RefreshStore) Issue(ctx context.Context, userID string) (string, error) {
	raw, err := randomToken()
	if err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), userID, hashToken(raw), time.Now().Add(s.ttl),
	)
	if err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	return raw, nil
}

// Rotate validates rawToken, revokes it, and issues a replacement in one
// transaction. Refresh tokens are single-use: presenting the same one a
// second time (e.g. a stolen, already-used token, or a replay) fails,
// because it's already revoked by the first use.
func (s *RefreshStore) Rotate(ctx context.Context, rawToken string) (userID, newToken string, err error) {
	hash := hashToken(rawToken)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op if already committed

	var id string
	err = tx.QueryRow(ctx, `
		SELECT id, user_id FROM refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
	`, hash).Scan(&id, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrInvalidRefreshToken
	}
	if err != nil {
		return "", "", fmt.Errorf("look up refresh token: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1`, id); err != nil {
		return "", "", fmt.Errorf("revoke refresh token: %w", err)
	}

	newRaw, err := randomToken()
	if err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), userID, hashToken(newRaw), time.Now().Add(s.ttl),
	); err != nil {
		return "", "", fmt.Errorf("store rotated refresh token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("commit rotation: %w", err)
	}
	return userID, newRaw, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
