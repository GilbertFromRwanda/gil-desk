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
)

type User struct {
	ID    string
	Email string
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
	err := s.pool.QueryRow(ctx,
		`SELECT id, password_hash FROM users WHERE email = $1`, email,
	).Scan(&id, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("look up user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return &User{ID: id, Email: email}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
