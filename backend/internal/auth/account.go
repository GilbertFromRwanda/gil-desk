package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrTOTPNotEnrolled    = errors.New("no TOTP secret enrolled for this user")
)

type User struct {
	ID          string
	Email       string
	TOTPEnabled bool
}

// AccountStore backs the account model (planner task G-13) — deliberately
// only email/password today; anything beyond that (profile fields, OAuth,
// etc.) is out of scope until a real product requirement needs it.
type AccountStore struct {
	pool *pgxpool.Pool
}

func NewAccountStore(pool *pgxpool.Pool) *AccountStore {
	return &AccountStore{pool: pool}
}

// CreateUser hashes the password with bcrypt (golang.org/x/crypto, not a
// hand-rolled hash) — plaintext passwords are never stored or compared.
func (s *AccountStore) CreateUser(ctx context.Context, email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	id := uuid.NewString()
	_, err = s.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		id, email, string(hash),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &User{ID: id, Email: email}, nil
}

// VerifyPassword returns ErrInvalidCredentials for both "no such email"
// and "wrong password" — the same error either way, deliberately, so a
// caller can't use response differences to enumerate registered emails.
func (s *AccountStore) VerifyPassword(ctx context.Context, email, password string) (*User, error) {
	var id, passwordHash string
	var totpEnabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT id, password_hash, totp_enabled FROM users WHERE email = $1`, email,
	).Scan(&id, &passwordHash, &totpEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("look up user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return &User{ID: id, Email: email, TOTPEnabled: totpEnabled}, nil
}

// GetUser returns ErrUserNotFound if userID doesn't exist — used by the
// 2FA enrollment endpoints, which authenticate via an access token
// (already-verified user ID) rather than email+password.
func (s *AccountStore) GetUser(ctx context.Context, userID string) (*User, error) {
	var u User
	u.ID = userID
	err := s.pool.QueryRow(ctx,
		`SELECT email, totp_enabled FROM users WHERE id = $1`, userID,
	).Scan(&u.Email, &u.TOTPEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", userID, err)
	}
	return &u, nil
}

// SetPendingTOTPSecret stores a newly generated secret without enabling
// 2FA — enrollment isn't complete until VerifyAndEnableTOTP confirms the
// user's authenticator app actually has it right, so a broken enrollment
// can't lock someone out.
func (s *AccountStore) SetPendingTOTPSecret(ctx context.Context, userID, secret string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_secret = $1, totp_enabled = false WHERE id = $2`, secret, userID)
	if err != nil {
		return fmt.Errorf("set pending TOTP secret for %s: %w", userID, err)
	}
	return nil
}

// GetTOTPSecret returns ErrTOTPNotEnrolled if no secret has been set yet
// (pending or enabled — the caller decides what "enrolled" requires).
func (s *AccountStore) GetTOTPSecret(ctx context.Context, userID string) (string, error) {
	var secret *string
	err := s.pool.QueryRow(ctx,
		`SELECT totp_secret FROM users WHERE id = $1`, userID,
	).Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) || secret == nil {
		return "", ErrTOTPNotEnrolled
	}
	if err != nil {
		return "", fmt.Errorf("get TOTP secret for %s: %w", userID, err)
	}
	return *secret, nil
}

func (s *AccountStore) EnableTOTP(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET totp_enabled = true WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("enable TOTP for %s: %w", userID, err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
