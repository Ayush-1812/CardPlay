ALTER TABLE room_members DROP CONSTRAINT room_members_room_id_seat_key;
ALTER TABLE room_members ADD CONSTRAINT room_members_room_id_seat_key UNIQUE(room_id, seat);
ALTER TABLE rooms DROP COLUMN team_a, DROP COLUMN team_b;
