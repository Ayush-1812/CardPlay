CREATE TABLE room_bans (
 room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id),
 actor_id uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(room_id,user_id)
);
