ALTER TABLE match_participants ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE match_participants ADD COLUMN abandon_vote boolean NOT NULL DEFAULT false;
ALTER TABLE matches ADD COLUMN version bigint NOT NULL DEFAULT 0;
ALTER TABLE matches ADD COLUMN end_reason text CHECK (end_reason IN ('won','left','voted','expired'));
ALTER TABLE matches ADD COLUMN ended_by uuid REFERENCES users(id);
CREATE INDEX matches_room_idx ON matches(room_id, created_at DESC);
CREATE INDEX matches_live_idx ON matches(status) WHERE status IN ('playing','paused');
CREATE INDEX match_participants_user_idx ON match_participants(user_id);
CREATE TABLE user_mutes (
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 muted_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id, muted_id), CHECK(user_id <> muted_id)
);
