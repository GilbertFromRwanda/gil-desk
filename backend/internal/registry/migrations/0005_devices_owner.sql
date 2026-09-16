-- Ties device identity to a user account (planner task G-17). Nullable at
-- the DB level (no backfill step for any pre-existing rows), but the
-- application layer always sets it going forward — RegisterDevice now
-- requires an authenticated caller.
ALTER TABLE devices ADD COLUMN owner_user_id TEXT REFERENCES users (id);
