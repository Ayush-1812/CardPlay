-- name: ListFriendships :many
SELECT f.requester_id,f.recipient_id,f.status,u.id,u.handle,u.display_name
FROM friendships f JOIN users u ON u.id=CASE WHEN f.requester_id=$1 THEN f.recipient_id ELSE f.requester_id END
WHERE (f.requester_id=$1 OR f.recipient_id=$1) AND u.deleted_at IS NULL AND u.email_verified ORDER BY f.created_at DESC LIMIT 100;
-- name: SearchPublicUser :one
SELECT u.id,u.handle,u.display_name FROM users u WHERE u.handle=sqlc.arg(handle)::text AND u.email_verified AND u.deleted_at IS NULL
AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.user_id=sqlc.arg(actor_id)::uuid AND b.blocked_id=u.id) OR (b.user_id=u.id AND b.blocked_id=sqlc.arg(actor_id)::uuid));
-- name: ListBlocks :many
SELECT u.id,u.handle,u.display_name FROM user_blocks b JOIN users u ON u.id=b.blocked_id WHERE b.user_id=$1 AND u.deleted_at IS NULL AND u.email_verified ORDER BY b.created_at DESC LIMIT 100;
-- name: HasBlock :one
SELECT EXISTS(SELECT 1 FROM user_blocks WHERE (user_id=$1 AND blocked_id=$2) OR (user_id=$2 AND blocked_id=$1));
-- name: AreFriends :one
SELECT EXISTS(SELECT 1 FROM friendships WHERE status='accepted' AND ((requester_id=$1 AND recipient_id=$2) OR (requester_id=$2 AND recipient_id=$1)));
-- name: RequestFriend :execrows
INSERT INTO friendships(requester_id,recipient_id)
SELECT sqlc.arg(requester_id)::uuid,u.id FROM users u WHERE u.id=sqlc.arg(recipient_id)::uuid AND u.deleted_at IS NULL AND u.email_verified
ON CONFLICT DO NOTHING;
-- name: AcceptFriend :execrows
UPDATE friendships SET status='accepted' WHERE requester_id=$1 AND recipient_id=$2 AND status='pending';
-- name: DeclineFriend :execrows
DELETE FROM friendships WHERE requester_id=$1 AND recipient_id=$2 AND status='pending';
-- name: RemoveFriend :exec
DELETE FROM friendships WHERE (requester_id=$1 AND recipient_id=$2) OR (requester_id=$2 AND recipient_id=$1);
-- name: BlockUser :execrows
INSERT INTO user_blocks(user_id,blocked_id)
SELECT sqlc.arg(user_id)::uuid,u.id FROM users u WHERE u.id=sqlc.arg(blocked_id)::uuid AND u.deleted_at IS NULL AND u.email_verified
ON CONFLICT DO NOTHING;
-- name: UnblockUser :exec
DELETE FROM user_blocks WHERE user_id=$1 AND blocked_id=$2;
-- name: RevokePairInvitations :exec
UPDATE invitations SET revoked_at=now() WHERE ((inviter_id=sqlc.arg(actor_id)::uuid AND target_id=sqlc.arg(other_id)::uuid) OR (inviter_id=sqlc.arg(other_id)::uuid AND target_id=sqlc.arg(actor_id)::uuid)) AND revoked_at IS NULL;
-- name: LockSocialPair :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(pair)::text,0));
