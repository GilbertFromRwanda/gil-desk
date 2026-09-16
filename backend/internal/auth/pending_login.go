package auth

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

// PendingLoginIssuer issues short-lived tokens proving "this caller
// already provided a correct password for this user and now just needs
// to complete a TOTP challenge" (planner task G-16's login flow). Built
// on crypto/hmac+crypto/sha256, the same vetted-primitive approach as
// rendezvous.TokenIssuer — deliberately its own small type rather than a
// shared one: the two tokens mean different things, and keeping them
// structurally distinct means a pending-login token can never be mistaken
// for a device session token (or, more importantly, for a real JWT
// access token — TokenIssuer.VerifyAccessToken only ever accepts a JWT).
type PendingLoginIssuer struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func NewPendingLoginIssuer(key []byte, ttl time.Duration) *PendingLoginIssuer {
	return &PendingLoginIssuer{key: key, ttl: ttl, now: time.Now}
}

func (i *PendingLoginIssuer) Issue(userID string) (token string, expiresAt time.Time) {
	expiresAt = i.now().Add(i.ttl)
	payload := base64.RawURLEncoding.EncodeToString([]byte(userID)) + "|" + strconv.FormatInt(expiresAt.Unix(), 10)
	sig := i.sign(payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(sig), expiresAt
}

func (i *PendingLoginIssuer) Verify(token string) (userID string, err error) {
	payload, sigB64, ok := strings.Cut(token, ".")
	if !ok {
		return "", errors.New("malformed pending login token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return "", fmt.Errorf("malformed pending login token signature: %w", err)
	}
	if !hmac.Equal(sig, i.sign(payload)) {
		return "", errors.New("pending login token signature does not match")
	}

	parts := strings.Split(payload, "|")
	if len(parts) != 2 {
		return "", errors.New("malformed pending login token payload")
	}
	userIDBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("malformed pending login token user id: %w", err)
	}
	expiresAtUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", fmt.Errorf("malformed pending login token expiry: %w", err)
	}
	if i.now().After(time.Unix(expiresAtUnix, 0)) {
		return "", errors.New("pending login token has expired")
	}
	return string(userIDBytes), nil
}

func (i *PendingLoginIssuer) sign(payload string) []byte {
	mac := hmac.New(sha256.New, i.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
