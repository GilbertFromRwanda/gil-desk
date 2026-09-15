package rendezvous

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TokenIssuer issues and verifies short-lived session authorization
// tokens (planner task G-12) — proof, for a relay or the target device,
// that the rendezvous server already checked the requester is on the
// target's authorization list, without either of them needing their own
// round trip back to the database. This is not the G-13..G-19 user-
// account JWT system (see internal/auth) — it's scoped to one rendezvous
// session between two already-registered devices.
//
// Built on the standard library's crypto/hmac + crypto/sha256 (a vetted
// MAC construction), not a hand-rolled signature scheme — the same
// "don't invent your own crypto" rule this project applies everywhere
// else (see crypto/tls.rs in core/).
type TokenIssuer struct {
	key []byte
	ttl time.Duration
	now func() time.Time // overridable for tests
}

func NewTokenIssuer(key []byte, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{key: key, ttl: ttl, now: time.Now}
}

type SessionClaims struct {
	RequesterDeviceID string
	TargetDeviceID    string
	ExpiresAt         time.Time
}

// Issue returns an opaque token string and its expiry.
func (i *TokenIssuer) Issue(requesterDeviceID, targetDeviceID string) (token string, expiresAt time.Time) {
	expiresAt = i.now().Add(i.ttl)
	payload := encodePayload(requesterDeviceID, targetDeviceID, expiresAt)
	sig := i.sign(payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(sig), expiresAt
}

// Verify checks the signature and expiry, returning the claims if valid.
func (i *TokenIssuer) Verify(token string) (*SessionClaims, error) {
	payload, sigB64, ok := strings.Cut(token, ".")
	if !ok {
		return nil, errors.New("malformed session token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("malformed session token signature: %w", err)
	}
	if !hmac.Equal(sig, i.sign(payload)) {
		return nil, errors.New("session token signature does not match")
	}

	claims, err := decodePayload(payload)
	if err != nil {
		return nil, fmt.Errorf("malformed session token payload: %w", err)
	}
	if i.now().After(claims.ExpiresAt) {
		return nil, errors.New("session token has expired")
	}
	return claims, nil
}

func (i *TokenIssuer) sign(payload string) []byte {
	mac := hmac.New(sha256.New, i.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func encodePayload(requesterDeviceID, targetDeviceID string, expiresAt time.Time) string {
	fields := []string{
		base64.RawURLEncoding.EncodeToString([]byte(requesterDeviceID)),
		base64.RawURLEncoding.EncodeToString([]byte(targetDeviceID)),
		strconv.FormatInt(expiresAt.Unix(), 10),
	}
	return strings.Join(fields, "|")
}

func decodePayload(payload string) (*SessionClaims, error) {
	parts := strings.Split(payload, "|")
	if len(parts) != 3 {
		return nil, errors.New("expected 3 fields")
	}
	requester, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("requester_device_id: %w", err)
	}
	target, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("target_device_id: %w", err)
	}
	expiresAtUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("expires_at: %w", err)
	}
	return &SessionClaims{
		RequesterDeviceID: string(requester),
		TargetDeviceID:    string(target),
		ExpiresAt:         time.Unix(expiresAtUnix, 0),
	}, nil
}
