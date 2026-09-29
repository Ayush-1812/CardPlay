-- name: CreateMatch :one
INSERT INTO matches(room_id,game_id,rules_version,status) VALUES($1,$2,$3,'paused') RETURNING *;
-- name: AddParticipant :exec
INSERT INTO match_participants(match_id,user_id,seat,disconnected_at) VALUES($1,$2,$3,now());
-- name: LockMatch :one
SELECT * FROM matches WHERE id=$1 FOR UPDATE;
-- name: MatchParticipants :many
SELECT p.user_id,p.seat,p.controller_generation,p.disconnected_at,p.last_seen_at,p.abandon_vote,u.handle,u.display_name
FROM match_participants p JOIN users u ON u.id=p.user_id WHERE p.match_id=$1 ORDER BY p.seat;
-- name: MatchParticipant :one
SELECT * FROM match_participants WHERE match_id=$1 AND user_id=$2;
-- name: TakeControl :one
UPDATE match_participants SET controller_generation=controller_generation+1,last_seen_at=now(),disconnected_at=NULL
WHERE match_id=$1 AND user_id=$2 RETURNING controller_generation;
-- name: ControllerHeartbeat :execrows
UPDATE match_participants SET last_seen_at=now()
WHERE match_id=$1 AND user_id=$2 AND controller_generation=$3 AND disconnected_at IS NULL;
-- name: ControllerReturned :execrows
UPDATE match_participants SET last_seen_at=now(),disconnected_at=NULL
WHERE match_id=$1 AND user_id=$2 AND controller_generation=$3 AND disconnected_at IS NOT NULL;
-- name: ControllerDisconnected :execrows
UPDATE match_participants SET disconnected_at=now()
WHERE match_id=$1 AND user_id=$2 AND controller_generation=$3 AND disconnected_at IS NULL;
-- name: StaleControllers :many
UPDATE match_participants p SET disconnected_at=p.last_seen_at FROM matches m
WHERE m.id=p.match_id AND m.status IN ('playing','paused') AND p.disconnected_at IS NULL AND p.last_seen_at < now()-interval '45 seconds'
RETURNING p.match_id;
-- name: SetMatchStatus :exec
UPDATE matches SET status=$2,version=version+1 WHERE id=$1;
-- name: BumpMatchVersion :exec
UPDATE matches SET version=version+1 WHERE id=$1;
-- name: AdvanceMatch :exec
UPDATE matches SET revision=$2,version=version+1 WHERE id=$1;
-- name: EndMatch :exec
UPDATE matches SET status=sqlc.arg(status),winner_id=sqlc.narg(winner_id),end_reason=sqlc.arg(end_reason),ended_by=sqlc.narg(ended_by),finished_at=now(),version=version+1
WHERE id=sqlc.arg(id);
-- name: InsertSnapshot :exec
INSERT INTO game_snapshots(match_id,revision,schema_version,state) VALUES($1,$2,$3,$4);
-- name: InsertGameEvent :exec
INSERT INTO game_events(match_id,revision,event_index,kind,audience_user_id,public,payload) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: FindCommand :one
SELECT * FROM game_commands WHERE match_id=$1 AND actor_id=$2 AND command_id=$3;
-- name: InsertCommand :exec
INSERT INTO game_commands(match_id,actor_id,command_id,request_hash,resulting_revision,result) VALUES($1,$2,$3,$4,$5,$6);
-- name: RecentEvents :many
SELECT revision,event_index,kind,payload,created_at FROM game_events
WHERE match_id=sqlc.arg(match_id) AND (public OR audience_user_id=sqlc.arg(viewer_id)::uuid)
ORDER BY revision DESC,event_index DESC LIMIT 60;
-- name: LatestRoomMatch :one
SELECT * FROM matches WHERE room_id=$1 ORDER BY created_at DESC LIMIT 1;
-- name: ActiveMatchesForUser :many
SELECT m.id FROM matches m JOIN match_participants p ON p.match_id=m.id
WHERE p.user_id=$1 AND m.status IN ('playing','paused');
-- name: SetAbandonVote :exec
UPDATE match_participants SET abandon_vote=$3 WHERE match_id=$1 AND user_id=$2;
-- name: ClearAbandonVotes :exec
UPDATE match_participants SET abandon_vote=false WHERE match_id=$1;
-- name: ExpiredMatches :many
SELECT m.id FROM matches m WHERE m.status IN ('playing','paused')
AND NOT EXISTS(SELECT 1 FROM match_participants p WHERE p.match_id=m.id AND (p.disconnected_at IS NULL OR p.disconnected_at > now()-interval '24 hours'));
-- name: PruneEndedSnapshots :exec
DELETE FROM game_snapshots s USING matches m
WHERE m.id=s.match_id AND m.status IN ('finished','abandoned') AND s.revision < m.revision;
-- name: PurgeOldMatches :exec
DELETE FROM matches WHERE status IN ('finished','abandoned') AND finished_at < now()-interval '90 days';
-- name: SetRoomStatus :exec
UPDATE rooms SET status=$2,revision=revision+1 WHERE id=$1;
