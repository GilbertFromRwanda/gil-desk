-- Stores the raw TOTP secret, not encrypted at rest — a real gap, not an
-- oversight to gloss over. This system has no key-management
-- infrastructure yet (session/JWT signing keys are also just plain env
-- vars), so there's nothing to meaningfully encrypt the secret *with*
-- yet; a production deployment needs envelope encryption (KMS-backed)
-- before this is acceptable for real user accounts.
ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_enabled BOOLEAN NOT NULL DEFAULT false;
