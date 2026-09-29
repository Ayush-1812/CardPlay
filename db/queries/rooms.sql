-- name: CreateRoom :one
INSERT INTO rooms(host_id,name,capacity) VALUES($1,$2,$3) RETURNING *;
-- name: Room :one
SELECT * FROM rooms WHERE id=$1;
-- name: LockRoom :one
SELECT * FROM rooms WHERE id=$1 FOR UPDATE;
-- name: MyRooms :many
SELECT r.* FROM rooms r JOIN room_members m ON m.room_id=r.id WHERE m.user_id=$1 AND r.status<>'closed' ORDER BY r.created_at DESC LIMIT 100;
-- name: Members :many
SELECT u.id,u.handle,u.display_name,m.seat,m.ready,m.joined_at FROM room_members m JOIN users u ON u.id=m.user_id WHERE m.room_id=$1 ORDER BY m.seat;
-- name: IsMember :one
SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=$1 AND user_id=$2);
-- name: AddMember :exec
INSERT INTO room_members(room_id,user_id,seat) VALUES($1,$2,$3);
-- name: SetReady :exec
UPDATE room_members SET ready=$3 WHERE room_id=$1 AND user_id=$2;
-- name: ResetReady :exec
UPDATE room_members SET ready=false WHERE room_id=$1;
-- name: BumpRoom :exec
UPDATE rooms SET revision=revision+1 WHERE id=$1;
-- name: CreateInvitation :one
INSERT INTO invitations(room_id,inviter_id,target_id,token_hash) VALUES($1,$2,NULLIF(sqlc.arg(target_id)::text,'')::uuid,NULLIF(sqlc.arg(token_hash)::text,'')) RETURNING id,expires_at;
-- name: MyInvitations :many
SELECT i.id,i.room_id,i.inviter_id,i.expires_at,r.name FROM invitations i JOIN rooms r ON r.id=i.room_id
WHERE i.target_id=sqlc.arg(user_id)::uuid AND i.expires_at>now() AND i.revoked_at IS NULL AND i.accepted_at IS NULL AND r.status='waiting' ORDER BY i.expires_at LIMIT 100;
-- name: Invitation :one
SELECT * FROM invitations WHERE id=$1;
-- name: InvitationByToken :one
SELECT * FROM invitations WHERE token_hash=$1;
-- name: AcceptInvitation :exec
UPDATE invitations SET accepted_at=now() WHERE id=$1 AND target_id IS NOT NULL;
-- name: RevokeInvitation :execrows
UPDATE invitations SET revoked_at=now() WHERE id=$1 AND inviter_id=$2;
