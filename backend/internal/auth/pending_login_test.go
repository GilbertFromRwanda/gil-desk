package auth

import (
	"testing"
	"time"
)

func TestPendingLoginIssueAndVerify(t *testing.T) {
	issuer := NewPendingLoginIssuer([]byte("test-key"), time.Minute)
	token, _ := issuer.Issue("user-123")

	userID, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("expected user-123, got %s", userID)
	}
}

func TestPendingLoginRejectsTampering(t *testing.T) {
	issuer := NewPendingLoginIssuer([]byte("test-key"), time.Minute)
	token, _ := issuer.Issue("user-123")

	// Flip the last character to something guaranteed different — a
	// fixed replacement is flaky when the original already matches it
	// (see rendezvous/token_test.go for the bug this pattern avoids).
	last := token[len(token)-1]
	replacement := byte('a')
	if last == replacement {
		replacement = 'b'
	}
	tampered := token[:len(token)-1] + string(replacement)

	if _, err := issuer.Verify(tampered); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestPendingLoginRejectsWrongKey(t *testing.T) {
	issuer := NewPendingLoginIssuer([]byte("key-a"), time.Minute)
	token, _ := issuer.Issue("user-123")

	other := NewPendingLoginIssuer([]byte("key-b"), time.Minute)
	if _, err := other.Verify(token); err == nil {
		t.Fatal("expected token signed with a different key to be rejected")
	}
}

func TestPendingLoginRejectsExpired(t *testing.T) {
	issuer := NewPendingLoginIssuer([]byte("test-key"), time.Minute)
	start := time.Now()
	issuer.now = func() time.Time { return start }

	token, _ := issuer.Issue("user-123")

	issuer.now = func() time.Time { return start.Add(2 * time.Minute) }
	if _, err := issuer.Verify(token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestPendingLoginRejectsMalformedInput(t *testing.T) {
	issuer := NewPendingLoginIssuer([]byte("test-key"), time.Minute)
	for _, bad := range []string{"", "no-dot-here", "payload.not-base64!!!", "a|b.AAAA"} {
		if _, err := issuer.Verify(bad); err == nil {
			t.Fatalf("expected error for malformed token %q", bad)
		}
	}
}
