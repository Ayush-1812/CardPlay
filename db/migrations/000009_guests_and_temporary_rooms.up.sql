-- Guests (owner decision 2026-10-01): a name-only account that lives in one
-- browser and is deleted after 7 days without use.
ALTER TABLE users ADD COLUMN is_guest boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN last_active_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX users_idle_guests_idx ON users(last_active_at) WHERE is_guest AND deleted_at IS NULL;
-- Rooms are temporary (owner decision 2026-10-01): deleting a room removes
-- its games and its queued notifications with it.
ALTER TABLE matches DROP CONSTRAINT matches_room_id_fkey,
  ADD CONSTRAINT matches_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE;
ALTER TABLE outbox DROP CONSTRAINT outbox_room_id_fkey,
  ADD CONSTRAINT outbox_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE;
CREATE INDEX outbox_room_idx ON outbox(room_id);
