CREATE TABLE device_authorizations (
    owner_device_id TEXT NOT NULL,
    allowed_device_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_device_id, allowed_device_id)
);
