ALTER TABLE users ADD COLUMN deleted_at timestamptz;
ALTER TABLE sessions ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX sessions_id_idx ON sessions(id);
