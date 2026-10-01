DROP INDEX outbox_room_idx;
ALTER TABLE outbox DROP CONSTRAINT outbox_room_id_fkey,
  ADD CONSTRAINT outbox_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms(id);
ALTER TABLE matches DROP CONSTRAINT matches_room_id_fkey,
  ADD CONSTRAINT matches_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms(id);
DROP INDEX users_idle_guests_idx;
ALTER TABLE users DROP COLUMN last_active_at;
ALTER TABLE users DROP COLUMN is_guest;
