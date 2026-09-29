ALTER TABLE room_members ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX room_members_presence_idx ON room_members(room_id,last_seen_at DESC);
