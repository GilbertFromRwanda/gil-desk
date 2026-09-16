package auth

import (
	"github.com/pquerna/otp/totp"
)

const totpIssuer = "NexDesk"

// GenerateTOTPSecret creates a new TOTP secret for accountEmail via
// pquerna/otp (a vetted RFC 6238 implementation — not hand-rolled), and
// returns both the raw secret and the otpauth:// URL an authenticator
// app can consume (as a QR code, typically — rendering one is a client
// concern, not this backend's).
func GenerateTOTPSecret(accountEmail string) (secret string, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: accountEmail,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTPCode checks a 6-digit code against secret for the current
// time step (with pquerna/otp's default clock-skew tolerance).
func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}
