-- name: ChatPage :many
SELECT c.id,c.user_id,u.handle,u.display_name,c.body,c.created_at,c.client_id
FROM room_chat c JOIN users u ON u.id=c.user_id
WHERE c.room_id=$1 AND c.id>sqlc.arg(after_id)::bigint AND c.created_at>now()-interval '7 days'
AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE b.user_id=sqlc.arg(viewer_id)::uuid AND b.blocked_id=c.user_id)
ORDER BY c.id LIMIT 100;
-- name: ExistingChat :one
SELECT * FROM room_chat WHERE room_id=$1 AND user_id=$2 AND client_id=$3;
-- name: ChatRecentCount :one
SELECT count(*) FROM room_chat WHERE user_id=$1 AND created_at>now()-interval '10 seconds';
-- name: InsertChat :one
INSERT INTO room_chat(room_id,user_id,client_id,body) VALUES($1,$2,$3,$4) RETURNING *;
-- name: PurgeChat :exec
DELETE FROM room_chat WHERE created_at<=now()-interval '7 days';
-- name: Enqueue :exec
INSERT INTO outbox(room_id,kind,payload) VALUES($1,$2,$3);
-- name: PendingOutbox :many
SELECT * FROM outbox WHERE delivered_at IS NULL ORDER BY id LIMIT 100;
-- name: DeliverOutbox :exec
UPDATE outbox SET delivered_at=now() WHERE id=$1;
-- name: PurgeOutbox :exec
DELETE FROM outbox WHERE delivered_at<now()-interval '1 day';
