package rendezvous

import (
	"testing"
	"time"
)

func TestTokenIssueAndVerify(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	token, expiresAt := issuer.Issue("device-a", "device-b")
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.RequesterDeviceID != "device-a" || claims.TargetDeviceID != "device-b" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	// The token payload only carries second precision (Unix timestamp) —
	// compare truncated to that, not exact nanosecond equality.
	if !claims.ExpiresAt.Equal(expiresAt.Truncate(time.Second)) {
		t.Fatalf("expected expiry %v, got %v", expiresAt.Truncate(time.Second), claims.ExpiresAt)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	token, _ := issuer.Issue("device-a", "device-b")

	tampered := token[:len(token)-1] + "x"
	if _, err := issuer.Verify(tampered); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestTokenRejectsWrongKey(t *testing.T) {
	issuer := NewTokenIssuer([]byte("key-a"), time.Minute)
	token, _ := issuer.Issue("device-a", "device-b")

	otherIssuer := NewTokenIssuer([]byte("key-b"), time.Minute)
	if _, err := otherIssuer.Verify(token); err == nil {
		t.Fatal("expected token signed with a different key to be rejected")
	}
}

func TestTokenRejectsExpired(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	start := time.Now()
	issuer.now = func() time.Time { return start }

	token, _ := issuer.Issue("device-a", "device-b")

	issuer.now = func() time.Time { return start.Add(2 * time.Minute) }
	if _, err := issuer.Verify(token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestTokenRejectsMalformedInput(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-signing-key"), time.Minute)
	for _, bad := range []string{"", "no-dot-here", "payload.not-base64!!!", "a|b|c.AAAA"} {
		if _, err := issuer.Verify(bad); err == nil {
			t.Fatalf("expected error for malformed token %q", bad)
		}
	}
}
