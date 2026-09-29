-- name: Match :one
SELECT * FROM matches WHERE id=$1;
-- name: LatestSnapshot :one
SELECT * FROM game_snapshots WHERE match_id=$1 ORDER BY revision DESC LIMIT 1;
-- name: IsParticipant :one
SELECT EXISTS(SELECT 1 FROM match_participants WHERE match_id=$1 AND user_id=$2);
-- name: VisibleEvents :many
SELECT revision,event_index,kind,payload FROM game_events WHERE match_id=$1 AND revision>sqlc.arg(after_revision)::bigint
AND (public OR audience_user_id=sqlc.arg(viewer_id)::uuid) ORDER BY revision,event_index LIMIT 100;
