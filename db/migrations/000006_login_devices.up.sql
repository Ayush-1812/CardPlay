CREATE TABLE login_devices (
 token_hash text PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_devices_user_idx ON login_devices(user_id);
CREATE INDEX login_devices_expiry_idx ON login_devices(expires_at);
