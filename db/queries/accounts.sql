-- name: CreateUser :one
INSERT INTO users(email,handle,display_name,password_hash,email_verified) VALUES ($1,$2,$3,$4,$5) RETURNING *;
-- name: UserByEmail :one
SELECT * FROM users WHERE email=$1 AND deleted_at IS NULL;
-- name: UserByIDForUpdate :one
SELECT * FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE;
-- name: CreateSession :exec
INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3);
-- name: CreateGuest :one
INSERT INTO users(email,handle,display_name,password_hash,email_verified,is_guest) VALUES ($1,$2,$3,'guest-no-password',false,true) RETURNING *;
-- name: SessionUser :one
SELECT u.id,u.handle,u.display_name,u.email,u.email_verified,u.is_guest,u.last_active_at,s.expires_at
FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.deleted_at IS NULL;
-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash=$1;
-- name: ListSessions :many
SELECT id,created_at,expires_at FROM sessions WHERE user_id=$1 AND expires_at>now() ORDER BY created_at DESC LIMIT 50;
-- name: RevokeSession :execrows
DELETE FROM sessions WHERE id=$1 AND user_id=$2;
-- name: RevokeAllSessions :exec
DELETE FROM sessions WHERE user_id=$1;
-- name: CreateAccountToken :exec
INSERT INTO account_tokens(token_hash,user_id,purpose,expires_at) VALUES($1,$2,$3,$4);
-- name: ConsumeAccountToken :one
DELETE FROM account_tokens WHERE token_hash=$1 AND purpose=$2 AND expires_at>now() RETURNING user_id;
-- name: VerifyUser :exec
UPDATE users SET email_verified=true WHERE id=$1;
-- name: ChangePassword :exec
UPDATE users SET password_hash=$2 WHERE id=$1;
-- name: ChangeDisplayName :exec
UPDATE users SET display_name=$2 WHERE id=$1 AND deleted_at IS NULL;
-- name: DeleteAccountTokens :exec
DELETE FROM account_tokens WHERE user_id=$1;
-- name: DeletePurposeTokens :exec
DELETE FROM account_tokens WHERE user_id=$1 AND purpose=$2;
-- name: DeleteFriendships :exec
DELETE FROM friendships WHERE requester_id=$1 OR recipient_id=$1;
-- name: DeleteBlocks :exec
DELETE FROM user_blocks WHERE user_id=$1 OR blocked_id=$1;
-- name: RevokeUserInvitations :exec
UPDATE invitations SET revoked_at=now() WHERE (inviter_id=$1 OR target_id=$1) AND revoked_at IS NULL;
-- name: CloseHostedRooms :many
UPDATE rooms SET status='closed',revision=revision+1 WHERE host_id=$1 AND status='waiting' RETURNING id;
-- name: AnonymizeUser :exec
UPDATE users SET email='deleted+'||id::text||'@cardplay.invalid',handle='deleted_'||substr(replace(id::text,'-',''),1,16),display_name='Deleted player',password_hash=$2,email_verified=false,deleted_at=now() WHERE id=$1;
-- name: RemoveFromWaitingRooms :many
DELETE FROM room_members WHERE user_id=$1 AND room_id IN (SELECT id FROM rooms WHERE status='waiting') RETURNING room_id;
-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at<=now();
-- name: CreateLoginDevice :exec
INSERT INTO login_devices(token_hash,user_id,expires_at) VALUES($1,$2,$3);
-- name: LoginDeviceKnown :one
SELECT EXISTS(SELECT 1 FROM login_devices d JOIN users u ON u.id=d.user_id
WHERE d.token_hash=sqlc.arg(token_hash) AND d.expires_at>now() AND u.email=sqlc.arg(email) AND u.deleted_at IS NULL);
-- name: DeleteExpiredLoginDevices :exec
DELETE FROM login_devices WHERE expires_at<=now();
-- name: TouchUser :exec
-- Records activity at most hourly. A guest's sessions slide forward, so a
-- guest who keeps playing is never signed out.
WITH touched AS (
  UPDATE users u SET last_active_at=now() WHERE u.id=$1 AND u.last_active_at < now()-interval '1 hour' RETURNING u.id AS user_id,u.is_guest
)
UPDATE sessions s SET expires_at=greatest(s.expires_at, now()+interval '7 days')
FROM touched t WHERE s.user_id=t.user_id AND t.is_guest;
-- name: IdleGuests :many
SELECT id FROM users WHERE is_guest AND deleted_at IS NULL AND last_active_at < now()-interval '7 days'
ORDER BY last_active_at LIMIT 200;
-- name: CreateSocketTicket :exec
INSERT INTO socket_tickets(token_hash,user_id,session_hash,expires_at) VALUES($1,$2,$3,$4);
-- name: ConsumeSocketTicket :one
-- Single use: the handshake that presents the ticket deletes it and receives
-- the player it belongs to, provided the underlying session is still valid.
WITH used AS (
  DELETE FROM socket_tickets t WHERE t.token_hash=$1 AND t.expires_at>now() RETURNING t.user_id,t.session_hash
)
SELECT u.id,u.handle,u.display_name,u.email,u.email_verified,u.is_guest,t.session_hash
FROM used t JOIN users u ON u.id=t.user_id
JOIN sessions s ON s.token_hash=t.session_hash AND s.expires_at>now()
WHERE u.deleted_at IS NULL;
-- name: DeleteExpiredSocketTickets :exec
DELETE FROM socket_tickets WHERE expires_at<=now();
