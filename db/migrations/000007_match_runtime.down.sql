DROP TABLE user_mutes;
DROP INDEX match_participants_user_idx;
DROP INDEX matches_live_idx;
DROP INDEX matches_room_idx;
ALTER TABLE matches DROP COLUMN ended_by;
ALTER TABLE matches DROP COLUMN end_reason;
ALTER TABLE matches DROP COLUMN version;
ALTER TABLE match_participants DROP COLUMN abandon_vote;
ALTER TABLE match_participants DROP COLUMN last_seen_at;
