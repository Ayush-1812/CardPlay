-- name: ListFriendships :many
SELECT f.requester_id,f.recipient_id,f.status,u.id,u.handle,u.display_name
FROM friendships f JOIN users u ON u.id=CASE WHEN f.requester_id=$1 THEN f.recipient_id ELSE f.requester_id END
WHERE f.requester_id=$1 OR f.recipient_id=$1 ORDER BY f.created_at DESC LIMIT 100;
-- name: HasBlock :one
SELECT EXISTS(SELECT 1 FROM user_blocks WHERE (user_id=$1 AND blocked_id=$2) OR (user_id=$2 AND blocked_id=$1));
-- name: AreFriends :one
SELECT EXISTS(SELECT 1 FROM friendships WHERE status='accepted' AND ((requester_id=$1 AND recipient_id=$2) OR (requester_id=$2 AND recipient_id=$1)));
-- name: RequestFriend :exec
INSERT INTO friendships(requester_id,recipient_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
-- name: AcceptFriend :execrows
UPDATE friendships SET status='accepted' WHERE requester_id=$1 AND recipient_id=$2 AND status='pending';
-- name: RemoveFriend :exec
DELETE FROM friendships WHERE (requester_id=$1 AND recipient_id=$2) OR (requester_id=$2 AND recipient_id=$1);
-- name: BlockUser :exec
INSERT INTO user_blocks(user_id,blocked_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
-- name: UnblockUser :exec
DELETE FROM user_blocks WHERE user_id=$1 AND blocked_id=$2;
-- name: LockSocialPair :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(pair)::text,0));
