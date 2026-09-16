package auth

import (
	"testing"
	"time"
)

func TestAccessTokenIssueAndVerify(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-key"), time.Minute)
	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Fatalf("expected user-123, got %s", claims.UserID)
	}
}

func TestAccessTokenRejectsWrongKey(t *testing.T) {
	issuer := NewTokenIssuer([]byte("key-a"), time.Minute)
	token, _ := issuer.IssueAccessToken("user-123")

	other := NewTokenIssuer([]byte("key-b"), time.Minute)
	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("expected token signed with a different key to be rejected")
	}
}

func TestAccessTokenRejectsExpired(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-key"), -time.Minute) // already expired
	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if _, err := issuer.VerifyAccessToken(token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestAccessTokenRejectsMalformedInput(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-key"), time.Minute)
	for _, bad := range []string{"", "not-a-jwt", "a.b.c"} {
		if _, err := issuer.VerifyAccessToken(bad); err == nil {
			t.Fatalf("expected error for malformed token %q", bad)
		}
	}
}
