-- name: ChatPage :many
SELECT c.id,c.user_id,u.handle,u.display_name,c.body,c.created_at,c.client_id
FROM room_chat c JOIN users u ON u.id=c.user_id
WHERE c.room_id=$1 AND c.id>sqlc.arg(after_id)::bigint AND c.created_at>now()-interval '7 days'
AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE b.user_id=sqlc.arg(viewer_id)::uuid AND b.blocked_id=c.user_id)
AND NOT EXISTS(SELECT 1 FROM user_mutes mu WHERE mu.user_id=sqlc.arg(viewer_id)::uuid AND mu.muted_id=c.user_id)
ORDER BY CASE WHEN sqlc.arg(after_id)::bigint=0 THEN -c.id ELSE c.id END LIMIT 100;
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
-- name: PurgeOutbox :exec
DELETE FROM outbox WHERE created_at<now()-interval '1 day';
-- name: ChatAuthor :one
SELECT user_id FROM room_chat WHERE id=$1 AND room_id=$2 AND created_at>now()-interval '7 days';
-- name: ReportChat :execrows
INSERT INTO chat_reports(message_id,reporter_id,reason) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: MuteUser :execrows
INSERT INTO user_mutes(user_id,muted_id)
SELECT sqlc.arg(user_id)::uuid,u.id FROM users u WHERE u.id=sqlc.arg(muted_id)::uuid AND u.deleted_at IS NULL
ON CONFLICT DO NOTHING;
-- name: UnmuteUser :exec
DELETE FROM user_mutes WHERE user_id=$1 AND muted_id=$2;
-- name: ListMutes :many
SELECT u.id,u.handle,u.display_name FROM user_mutes m JOIN users u ON u.id=m.muted_id
WHERE m.user_id=$1 AND u.deleted_at IS NULL ORDER BY m.created_at DESC LIMIT 100;
-- name: DeleteMutes :exec
DELETE FROM user_mutes WHERE user_id=$1 OR muted_id=$1;
