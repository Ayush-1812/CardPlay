# Platform API and architecture

All endpoints use JSON unless the response is `204`. Base path: `/api/v1`. A successful mutation commits before its HTTP response. Versions are pinned per room/match as `game_id` and `rules_version`. Public room browsing, spectators and guest sessions are absent by design.

## HTTP endpoints

| Method / path | Body or query | Access | Response / status |
|---|---|---|---|
| `GET /healthz`, `GET /readyz` | none | Public | `200`; readiness queries PostgreSQL; `503 NOT_READY` on failure |
| `GET /api/v1/games` | none | Public | Game catalog with `playable`; Monopoly module currently reports false, Cambio is not registered |
| `POST /api/v1/auth/register` | `email`, `handle`, `display_name`, `password` | Public | `201 {verification_required}` pending verification; email token sent through configured SMTP. An already-registered email gets the identical response and the owner is emailed a notice instead, so registration never reveals accounts. A taken handle (public) returns `409 CONFLICT`. `503 MAIL_UNAVAILABLE` if delivery unavailable |
| `POST /api/v1/auth/verify` | `token` | Public | `200 verified`; single-use, one-hour token |
| `POST /api/v1/auth/verify/resend` | `email` | Public | `202`; generic response for unknown or already verified email; fresh one-hour token if needed |
| `POST /api/v1/auth/password/forgot` | `email` | Public | `202`; generic response, sends a single-use one-hour reset token when account exists |
| `POST /api/v1/auth/password/reset` | `token`, `password` | Public | `200`; consumes token, replaces hash, revokes every session and marks the email verified |
| `POST /api/v1/auth/login` | `email`, `password` | Public | `200` minimal profile + HttpOnly session cookie and login-device cookie; same error for missing email/wrong password |
| `POST /api/v1/auth/logout` | none | Signed in | `204`; current session removed |
| `GET /api/v1/me` | none | Signed in | Own ID, handle, display name, email and verification state; email never appears in room views |
| `PATCH /api/v1/me` | `display_name` | Signed in | `200`; updates own public display name (1–60 characters) |
| `PUT /api/v1/me/password` | `current_password`, `new_password` | Signed in | `204`; verifies current password, changes it and revokes all sessions |
| `DELETE /api/v1/me` | `password` | Signed in | `204`; revokes tokens/sessions, passes each hosted waiting room to its longest-present other member (closing rooms nobody else is in), removes waiting memberships, friendships and blocks, anonymizes retained profile |
| `GET /api/v1/sessions` | none | Signed in | Up to 50 unexpired own session IDs with creation and expiry times |
| `DELETE /api/v1/sessions/{sessionID}` | none | Session owner | `204`; revokes exactly that session, `404` for foreign/unknown IDs |
| `GET /api/v1/users?handle=x` | exact handle | Verified | ID, handle and display name only; unverified, deleted or mutually blocked users return `404` |
| `GET /api/v1/friendships` | none | Verified | Accepted, incoming and outgoing requests |
| `GET /api/v1/blocks` | none | Verified | Public profiles of accounts blocked by the caller; no email |
| `POST /api/v1/friendships/{userID}/{action}` | action: `request`, `accept`, `decline`, `remove`, `block`, `unblock` | Verified | `204`; `request` to someone with a pending request to you accepts it and returns `200 {"status":"accepted"}`; requests to unknown or unverified users also return `204`; recipient alone may accept or decline; remove cancels an outgoing request or removes a friend; blocks revoke pair invitations and prevent new requests/invites |
| `GET /api/v1/rooms` | none | Verified | Own open rooms only |
| `POST /api/v1/rooms` | `name`, `capacity` 2–5 | Verified | `201`; private room, creator seat zero |
| `GET /api/v1/rooms/{roomID}` | none | Member | Room metadata, member handles/seat/ready state; nonmembers receive `404` |
| `PATCH /api/v1/rooms/{roomID}` | `name`, `capacity` 2–5 | Host, waiting room | `200`; rejects capacity below occupied seats |
| `DELETE /api/v1/rooms/{roomID}` | none | Host, waiting room | `204`; closes room, revokes invitations and removes access to room/chat views |
| `POST /api/v1/rooms/{roomID}/leave` | none | Member, waiting room | `204`; host passes ownership to longest-present member or closes empty room; the leaver's invitations in the room are revoked |
| `PUT /api/v1/rooms/{roomID}/host` | `user_id` of member | Host, waiting room | `204`; explicit host transfer |
| `DELETE /api/v1/rooms/{roomID}/members/{userID}` | none | Host, waiting room | `204`; removes and bans member from rejoining through old links, revokes invitations to and created by them; resets readiness |
| `PUT /api/v1/rooms/{roomID}/ready` | `ready` boolean | Member, waiting room | `204`; serialized with room lock |
| `POST /api/v1/rooms/{roomID}/invitations` | `target_id` of friend, or empty for link | Member, waiting room | `201` ID/expiry and token for link only. Only the creation response exposes a link token. |
| `GET /api/v1/rooms/{roomID}/invitations` | none | Member | Own created invitation IDs, targets and expiry/status only; never a link token |
| `GET /api/v1/invitations` | none | Verified | Pending personal friend invitations |
| `DELETE /api/v1/invitations/{invitationID}` | none | Inviter or room host | `204` |
| `POST /api/v1/rooms/join` | exactly one of `invitation_id` or `token` | Verified | `200` joined room. Room lock guards capacity, status and invitation reread. Joining resets everyone’s ready state. |
| `GET /api/v1/rooms/{roomID}/chat?after=ID` | numeric cursor | Member | Up to 100 messages, oldest first, last seven days; viewer-blocked senders filtered. Without a cursor (or `after=0`) returns the newest 100; with a cursor, the next 100 after it |
| `POST /api/v1/rooms/{roomID}/chat` | UUID `client_id`, 1–500 char `body` | Member | `200` message; retries with same ID/body return prior message; five per ten seconds per account |
| `POST /api/v1/rooms/{roomID}/matches` | none | Host | `501 GAME_NOT_READY` in foundation; nonmembers receive `404`, other members `403` |
| `GET /api/v1/matches/{matchID}` | none | Participant | Player projection once engine exists; otherwise `501 GAME_NOT_READY` |

Client IDs and room IDs are identifiers, never credentials. Room membership is checked in the database for HTTP and WebSocket subscriptions. Session cookie is random 256-bit data; only its SHA-256 hash is stored. It is `HttpOnly`, `SameSite=Lax`, `Secure` for HTTPS, scoped to `/`. Sessions expire after seven days. Unsafe HTTP methods require an exact `Origin` match with `APP_ORIGIN`; WebSocket upgrades always require that exact origin. Cross-origin browser access is not enabled. Authentication endpoints have per-email or per-token limits (three mail requests or ten other attempts per minute) plus a higher source-address ceiling. The Next.js proxy forwards no client address, so limits never trust `X-Forwarded-For`. A successful login sets an HttpOnly `cardplay_device` cookie (path `/api/v1/auth/login`, 180 days; only its hash is stored, and it grants no access). Logins from a browser holding a known device cookie for that email get their own ten-per-minute bucket, so failed attempts sent by strangers cannot lock the owner out of a browser they have used before; unknown or invented device cookies share the email's bucket; social search, friend changes, room creation, joins and invites have account limits. The in-memory limiter applies per API instance; deployments with multiple API replicas need a shared edge or distributed limiter.

## WebSocket `/ws`

Use the authenticated cookie; no token in URL. The server closes with code `4001` when the session has ended (the client signs out) and `4004` when the subscribed room is no longer available (the client returns to the lobby); other closes are transient and the client reconnects. The opening frame is `{"v":1,"type":"hello","payload":{"heartbeat_seconds":20}}`. Every client command has version `v:1`, a UUID `id` for correlation, `type`, optional `room_id` and optional `payload`. Frame limit: 16 KiB; up to 30 frames in 10 seconds. Server rechecks the session on every command and heartbeat.

| Client type | Payload | Server response |
|---|---|---|
| `ping` | none | `pong` with same `id` |
| `room.subscribe` | `room_id` | Authorized `room.snapshot` with same `id`, or `error` |
| `chat.send` | `room_id`, `{client_id,body}` | `ack` with `message_id`, or `error` |
| `game.command` | reserved for future game adapter | `error` with `GAME_NOT_READY` |

`room.updated` and `chat.updated` are durable outbox invalidations without state or message contents. Clients refetch the authorized HTTP view. After reconnect, send a fresh `room.subscribe` and reconcile against its room revision; no prior socket connection is trusted to have delivered all notifications. The frontend uses exponential backoff with jitter and cancels retries on component cleanup. An insert trigger on the outbox issues a PostgreSQL `NOTIFY` on commit, and every API instance `LISTEN`s, so clients connected to any replica receive every invalidation. When the listener reconnects, each subscribed client is told to refetch because notifications may have been missed. Invalidations may repeat; clients must treat them as hints. Outbox rows are kept for one day. A subscribed room heartbeat records authenticated presence every 20 seconds. In a waiting room, if the host has not been seen for 60 seconds, a worker checks every five seconds and transfers ownership to the longest-present member seen within 40 seconds. Row locks serialize this with a returning host, joins and explicit host changes.

## Error envelope

`{"error":{"code":"CODE","message":"Safe explanation","request_id":"..."}}`. Main codes: `INVALID_REQUEST` (400), `UNAUTHENTICATED` / `INVALID_CREDENTIALS` (401), `FORBIDDEN`, `EMAIL_UNVERIFIED`, `ORIGIN_REJECTED` (403), `NOT_FOUND` / `INVITATION_INVALID` (404), `METHOD_NOT_ALLOWED` (405), `CONFLICT`, `ROOM_STARTED`, `ROOM_FULL`, `IDEMPOTENCY_CONFLICT` (409), `RATE_LIMITED` (429), `INTERNAL`, `STATE_INVALID` (500), `GAME_NOT_READY` (501), `MAIL_UNAVAILABLE`, `NOT_READY` (503). Unexpected database errors are logged with SQLSTATE only and returned as `INTERNAL`, without query text or private values. WebSocket `error.payload` contains `code` and `message`, correlated by `id`.

## Database and future game boundary

Migration `000001` creates users, sessions, one-use account tokens, friendships, blocks, rooms, members, invitations, matches, participants, snapshots, events, deduplicated commands, outbox, chat and reports. Migration `000002` adds stable session IDs for revocation and a deletion marker for anonymized accounts. `000003` records room removals so old links cannot restore a kicked seat; `000004` records room member presence for host succession; `000005` adds the outbox notification trigger; `000006` stores hashed login-device tokens for rate limiting. Room/chat paths use sqlc-generated pgx queries; migrations are embedded, checksummed and transactional under a PostgreSQL advisory lock. A version mismatch is a startup error. `migrate-down` is a local repair command, disabled in production. Account deletion retains referenced match/chat records under an anonymized handle; active-match deletion behavior belongs to the game milestone because matches cannot start yet.

`Game` has `Descriptor`, `New`, `Validate`, `Apply`, and `View`. `State` is raw server data. `View` has explicit `public`, `self` and legal choices; transports never marshal raw state. A future game adapter receives authenticated actor ID and expected revision, and should commit its transition, snapshot, typed public/private events, command ID/hash/result and outbox record in one transaction under a match row lock. Retrying the same command ID with the same hash returns the recorded result; a changed hash is a conflict. Running matches retain their registered rules version. The Monopoly 01723 engine (`internal/game/monopoly`) implements every rule with typed actions, explicit phases, seeded replay and per-seat projections ([traceability](08-monopoly-engine-traceability.md)); it stays `playable: false` until the transaction runner above is built. Cambio has no module.

The snapshot/event/command schema is present for this contract, but the generic transaction runner and game actions belong to the playable Monopoly milestone. No match can start with the current adapter.

## Access matrix

| Data or operation | Anonymous | Verified user | Room member | Match participant | Host |
|---|---|---|---|---|---|
| Game catalog and health | Read | Read | Read | Read | Read |
| Own profile and friends | — | Own only | Own only | Own only | Own only |
| Room list/details/chat | — | Own rooms only | Own room | If still member | Own room |
| Invite/create link | — | — | Allowed while waiting | — | Allowed while waiting |
| Change ready state | — | — | Self while waiting | — | Self while waiting |
| Start match | — | — | — | — | Host only, once playable |
| Match state | — | — | — | Own projected state | Own projected state |
| Hidden hand/draw order | — | — | — | Own hand via game projection; draw order never | Same limitation |

The database stores match state privately; a future game `View` must be reviewed and tested to ensure no opponent hand, deck order, secret seed or private counter choice leaks. Room and social services never query snapshot JSON.
