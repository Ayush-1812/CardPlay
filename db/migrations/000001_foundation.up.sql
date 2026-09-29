CREATE TABLE users (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 email text NOT NULL UNIQUE CHECK (email = lower(email)),
 handle text NOT NULL UNIQUE CHECK (handle ~ '^[a-z0-9_]{3,24}$'),
 display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 60),
 password_hash text NOT NULL,
 email_verified boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);
CREATE TABLE account_tokens (
 token_hash text PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 purpose text NOT NULL CHECK (purpose IN ('verify_email','reset_password')),
 expires_at timestamptz NOT NULL
);
CREATE TABLE friendships (
 requester_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 recipient_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted')),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(requester_id,recipient_id),
 CHECK(requester_id <> recipient_id)
);
CREATE UNIQUE INDEX friendships_pair_idx ON friendships(least(requester_id,recipient_id),greatest(requester_id,recipient_id));
CREATE TABLE user_blocks (
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 blocked_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,blocked_id), CHECK(user_id <> blocked_id)
);
CREATE TABLE rooms (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 host_id uuid NOT NULL REFERENCES users(id),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 80),
 game_id text NOT NULL DEFAULT 'monopoly-deal',
 rules_version text NOT NULL DEFAULT 'us-01723-v1',
 capacity integer NOT NULL DEFAULT 5 CHECK(capacity BETWEEN 2 AND 5),
 status text NOT NULL DEFAULT 'waiting' CHECK(status IN ('waiting','playing','finished','closed')),
 revision bigint NOT NULL DEFAULT 0 CHECK(revision >= 0),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE room_members (
 room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id),
 seat integer NOT NULL CHECK(seat BETWEEN 0 AND 4),
 ready boolean NOT NULL DEFAULT false,
 joined_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(room_id,user_id), UNIQUE(room_id,seat)
);
CREATE INDEX room_members_user_idx ON room_members(user_id);
CREATE TABLE invitations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
 inviter_id uuid NOT NULL REFERENCES users(id),
 target_id uuid REFERENCES users(id),
 token_hash text UNIQUE,
 expires_at timestamptz NOT NULL DEFAULT now() + interval '30 minutes',
 revoked_at timestamptz,
 accepted_at timestamptz,
 CHECK(target_id IS NOT NULL OR token_hash IS NOT NULL)
);
CREATE INDEX invitations_target_idx ON invitations(target_id,expires_at);
CREATE TABLE matches (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 room_id uuid NOT NULL REFERENCES rooms(id),
 game_id text NOT NULL,
 rules_version text NOT NULL,
 status text NOT NULL CHECK(status IN ('playing','paused','finished','abandoned')),
 revision bigint NOT NULL DEFAULT 0,
 winner_id uuid REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz
);
CREATE UNIQUE INDEX matches_one_active_room_idx ON matches(room_id) WHERE status IN ('playing','paused');
CREATE TABLE match_participants (
 match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id),
 seat integer NOT NULL CHECK(seat BETWEEN 0 AND 4),
 controller_generation bigint NOT NULL DEFAULT 0,
 disconnected_at timestamptz,
 PRIMARY KEY(match_id,user_id), UNIQUE(match_id,seat)
);
CREATE TABLE game_snapshots (
 match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision >= 0),
 schema_version integer NOT NULL CHECK(schema_version > 0),
 state jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(match_id,revision)
);
CREATE TABLE game_events (
 match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
 revision bigint NOT NULL,
 event_index integer NOT NULL CHECK(event_index >= 0),
 kind text NOT NULL,
 audience_user_id uuid REFERENCES users(id),
 public boolean NOT NULL DEFAULT false,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(match_id,revision,event_index),
 CHECK(NOT public OR audience_user_id IS NULL)
);
CREATE TABLE game_commands (
 match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
 actor_id uuid NOT NULL REFERENCES users(id),
 command_id uuid NOT NULL,
 request_hash text NOT NULL,
 resulting_revision bigint NOT NULL,
 result jsonb NOT NULL,
 PRIMARY KEY(match_id,actor_id,command_id)
);
CREATE TABLE outbox (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 room_id uuid NOT NULL REFERENCES rooms(id),
 kind text NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 delivered_at timestamptz
);
CREATE INDEX outbox_pending_idx ON outbox(id) WHERE delivered_at IS NULL;
CREATE TABLE room_chat (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id),
 client_id uuid NOT NULL,
 body text NOT NULL CHECK(char_length(body) BETWEEN 1 AND 500),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(room_id,user_id,client_id)
);
CREATE INDEX room_chat_page_idx ON room_chat(room_id,id);
CREATE INDEX room_chat_retention_idx ON room_chat(created_at);
CREATE TABLE chat_reports (
 message_id bigint NOT NULL REFERENCES room_chat(id) ON DELETE CASCADE,
 reporter_id uuid NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 500),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(message_id,reporter_id)
);
