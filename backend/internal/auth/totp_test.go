package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateAndValidateTOTP(t *testing.T) {
	secret, url, err := GenerateTOTPSecret("alice@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if secret == "" {
		t.Fatal("expected a non-empty secret")
	}
	if url == "" {
		t.Fatal("expected a non-empty otpauth URL")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if !ValidateTOTPCode(secret, code) {
		t.Fatal("expected the freshly generated code to validate")
	}
}

func TestValidateTOTPRejectsWrongCode(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("bob@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	real, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	wrong := "000000"
	if wrong == real {
		wrong = "111111"
	}
	if ValidateTOTPCode(secret, wrong) {
		t.Fatal("expected an incorrect code to be rejected")
	}
}

func TestValidateTOTPRejectsCodeForADifferentSecret(t *testing.T) {
	secretA, _, err := GenerateTOTPSecret("carol@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	secretB, _, err := GenerateTOTPSecret("dave@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	codeForA, err := totp.GenerateCode(secretA, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if ValidateTOTPCode(secretB, codeForA) {
		t.Fatal("expected a code generated for one secret to be rejected against a different secret")
	}
}
