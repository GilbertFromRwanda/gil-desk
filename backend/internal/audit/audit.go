// Package audit records security-relevant events (planner task G-19):
// logins (success/failure/rate-limited), registrations, and device
// authorization changes. Durable (Postgres), not just log lines — a
// production deployment needs to be able to answer "who did what, when"
// after the fact, which grep-through-logs doesn't reliably give you.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event type constants — kept as plain strings (not an enum type) so a
// caller can log an event type this package doesn't know about yet
// without a code change here.
const (
	EventUserRegistered       = "user.registered"
	EventLoginSucceeded       = "login.succeeded"
	EventLoginFailed          = "login.failed"
	EventLoginRateLimited     = "login.rate_limited"
	EventLoginTwoFactorFailed = "login.2fa_failed"
	EventTwoFactorEnabled     = "user.2fa_enabled"
	EventDeviceRegistered     = "device.registered"
	EventDeviceAuthorized     = "device.authorized"
)

type Logger struct {
	pool *pgxpool.Pool
}

func NewLogger(pool *pgxpool.Pool) *Logger {
	return &Logger{pool: pool}
}

// Log records one event. userID may be empty when no authenticated user
// is associated with it (e.g. a failed login against an unknown email) —
// stored as NULL, not a fake sentinel value. metadata may be nil.
func (l *Logger) Log(ctx context.Context, eventType, userID, subject string, metadata map[string]any) error {
	var userIDArg any
	if userID != "" {
		userIDArg = userID
	}

	var metadataJSON []byte
	if metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal audit metadata for %s: %w", eventType, err)
		}
	}

	_, err := l.pool.Exec(ctx, `
		INSERT INTO audit_events (id, event_type, user_id, subject, metadata)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.NewString(), eventType, userIDArg, subject, metadataJSON)
	if err != nil {
		return fmt.Errorf("log audit event %s: %w", eventType, err)
	}
	return nil
}

type Event struct {
	ID        string
	EventType string
	UserID    string // empty if the event had no associated user
	Subject   string
	CreatedAt time.Time
}

// Recent returns the most recent events, newest first — for operators
// and tests; there's no admin UI to view these yet.
func (l *Logger) Recent(ctx context.Context, limit int) ([]Event, error) {
	rows, err := l.pool.Query(ctx, `
		SELECT id, event_type, COALESCE(user_id, ''), COALESCE(subject, ''), created_at
		FROM audit_events
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent audit events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.EventType, &e.UserID, &e.Subject, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}
