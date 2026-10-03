-- Team names a room can set, and seat swapping for team games.
-- An empty name means the client shows its default label.
ALTER TABLE rooms
 ADD COLUMN team_a text NOT NULL DEFAULT '' CHECK (char_length(team_a) <= 24),
 ADD COLUMN team_b text NOT NULL DEFAULT '' CHECK (char_length(team_b) <= 24);

-- Swapping two players exchanges their seats in one statement, which trips a
-- unique constraint checked row by row. Deferring it to the end of the
-- transaction makes the swap legal while still forbidding two players on one
-- seat once the transaction commits.
ALTER TABLE room_members DROP CONSTRAINT room_members_room_id_seat_key;
ALTER TABLE room_members ADD CONSTRAINT room_members_room_id_seat_key
 UNIQUE(room_id, seat) DEFERRABLE INITIALLY IMMEDIATE;
