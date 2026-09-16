package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
}

// TokenIssuer issues and verifies short-lived JWT access tokens (planner
// task G-14), via golang-jwt (a vetted JWT library) — not a hand-rolled
// token format. This is distinct from rendezvous.TokenIssuer, which
// signs a much narrower, non-JWT session-authorization token for device
// pairing (see that package's docs for why it isn't JWT-based).
type TokenIssuer struct {
	signingKey []byte
	accessTTL  time.Duration
}

func NewTokenIssuer(signingKey []byte, accessTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{signingKey: signingKey, accessTTL: accessTTL}
}

func (i *TokenIssuer) IssueAccessToken(userID string) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.accessTTL)),
		},
		UserID: userID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(i.signingKey)
}

// VerifyAccessToken rejects anything not signed with HMAC (guards against
// the classic JWT "alg confusion" attack, where a token crafted with a
// different algorithm — or "none" — tricks a naive verifier into skipping
// or misusing signature checks) in addition to checking the signature
// itself and expiry.
func (i *TokenIssuer) VerifyAccessToken(tokenString string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return i.signingKey, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
