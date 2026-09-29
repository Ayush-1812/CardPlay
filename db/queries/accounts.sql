-- name: CreateUser :one
INSERT INTO users(email,handle,display_name,password_hash,email_verified) VALUES ($1,$2,$3,$4,$5) RETURNING *;
-- name: UserByEmail :one
SELECT * FROM users WHERE email=$1;
-- name: UserByHandle :one
SELECT id,handle,display_name FROM users WHERE handle=$1;
-- name: CreateSession :exec
INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3);
-- name: SessionUser :one
SELECT u.id,u.handle,u.display_name,u.email,u.email_verified,s.expires_at
FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now();
-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash=$1;
-- name: CreateAccountToken :exec
INSERT INTO account_tokens(token_hash,user_id,purpose,expires_at) VALUES($1,$2,$3,$4);
-- name: ConsumeAccountToken :one
DELETE FROM account_tokens WHERE token_hash=$1 AND purpose=$2 AND expires_at>now() RETURNING user_id;
-- name: VerifyUser :exec
UPDATE users SET email_verified=true WHERE id=$1;
-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at<=now();
