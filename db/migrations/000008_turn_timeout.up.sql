-- When the game started waiting for its current move. Reset by every
-- applied command and whenever a paused match resumes; drives the turn
-- timeout (docs/09, decision 2026-09-30).
ALTER TABLE matches ADD COLUMN awaiting_since timestamptz NOT NULL DEFAULT now();
CREATE INDEX matches_awaiting_idx ON matches(awaiting_since) WHERE status = 'playing';
