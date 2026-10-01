-- Socket tickets let a client open the WebSocket when its session cookie
-- cannot travel with the handshake, which happens when the client and the API
-- are on different sites (for example a Vercel client and a Fly API). The
-- ticket is requested over the ordinary authenticated HTTP path, is valid for
-- seconds, and is consumed by the first handshake that presents it.
CREATE TABLE socket_tickets (
 token_hash text PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 session_hash text NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX socket_tickets_expiry_idx ON socket_tickets(expires_at);
CREATE INDEX socket_tickets_user_idx ON socket_tickets(user_id);
