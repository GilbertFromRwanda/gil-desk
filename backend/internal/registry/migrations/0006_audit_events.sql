-- No FK from user_id to users(id): an audit log is meant to be an
-- immutable historical record independent of the current state of
-- whatever it references. A hard FK would either block deleting a user
-- (e.g. for a data-deletion request) or force losing audit history when
-- one is deleted — neither is right for an audit trail.
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    user_id TEXT,
    subject TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_events_user_id ON audit_events (user_id);
CREATE INDEX idx_audit_events_event_type ON audit_events (event_type);
CREATE INDEX idx_audit_events_created_at ON audit_events (created_at);
