# Platform API and architecture

All endpoints use JSON unless the response is `204`. Base path: `/api/v1`. A successful mutation commits before its HTTP response. Versions are pinned per room/match as `game_id` and `rules_version`. Public room browsing and spectators are absent by design. **Guests** are name-only accounts (`is_guest`): they may use rooms, matches and chat like a verified account, but friends, blocks and user search answer `403 GUEST_NOT_ALLOWED`.

## HTTP endpoints

| Method / path | Body or query | Access | Response / status |
|---|---|---|---|
| `GET /healthz` | none | Public | `200` while the process serves HTTP (liveness) |
| `GET /readyz` | none | Public | `200 {status, checks}` when the database answers, all migrations are applied with matching checksums and the live-update listener is active; otherwise `503` naming the failing check ([operations](10-operations.md)) |
| `POST /api/v1/client-errors` | `kind`, `message`, `page`, `stack`, `digest` (all optional, 16 KB max) | Public, same-origin, 30/min | `204`; logged as `client_error` with bounded fields, query strings stripped |
| `GET /api/v1/games` | none | Public | Game catalog with `playable`; Monopoly Deal is playable, Cambio is not registered |
| `GET /api/v1/games/{gameID}/cards` | none | Public | Public card manifest (IDs, names, values, colors); no game state |
| `POST /api/v1/auth/register` | `email`, `handle`, `display_name`, `password` | Public | `201 {verification_required}` pending verification; email token sent through configured SMTP. An already-registered email gets the identical response and the owner is emailed a notice instead, so registration never reveals accounts. A taken handle (public) returns `409 CONFLICT`. `503 MAIL_UNAVAILABLE` if delivery unavailable |
| `POST /api/v1/auth/verify` | `token` | Public | `200 verified`; single-use, one-hour token |
| `POST /api/v1/auth/verify/resend` | `email` | Public | `202`; generic response for unknown or already verified email; fresh one-hour token if needed |
| `POST /api/v1/auth/password/forgot` | `email` | Public | `202`; generic response, sends a single-use one-hour reset token when account exists |
| `POST /api/v1/auth/password/reset` | `token`, `password` | Public | `200`; consumes token, replaces hash, revokes every session and marks the email verified |
| `POST /api/v1/auth/login` | `email`, `password` | Public | `200` minimal profile + HttpOnly session cookie and login-device cookie; same error for missing email/wrong password |
| `POST /api/v1/auth/guest` | `display_name` (1–24 characters, plain text) | Public, 60/min | `201` guest profile (`is_guest: true`) + HttpOnly session cookie. No email or password; the session slides with use and the guest is deleted after 7 days unused |
| `POST /api/v1/rooms` | `name`, optional `capacity`, optional `game` (default `monopoly-deal`) | Verified or guest | `201` room. The game must be installed and playable (`400 UNKNOWN_GAME`), and the capacity must be one the game accepts (`400 INVALID_REQUEST`). A game with a fixed seat count, such as Trump's four, fills the capacity in when it is omitted and refuses any other value, in creation and in room settings |
| `POST /api/v1/realtime/ticket` | none | Signed in, 120/min | `201` `{ticket, expires_at}`: a one-shot credential for the WebSocket handshake, valid 30 seconds, deleted the first time it is used, and void once its session ends. Only needed when the client is published on a different site from the API, so the session cookie cannot travel with the handshake |
| `POST /api/v1/auth/logout` | none | Signed in | `204`; current session removed. For a guest this also leaves every room and game and deletes the guest, since the cookie was its only key |
| `GET /api/v1/me` | none | Signed in | Own ID, handle, display name, email (empty for guests), verification state and `is_guest`; email never appears in room views |
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
| `DELETE /api/v1/rooms/{roomID}` | none | Host, waiting room | `204`; members are notified, then the room, its chat, invitations and games are deleted |
| `POST /api/v1/rooms/{roomID}/leave` | none | Member, waiting room | `204`; host passes ownership to the longest-present member; the leaver's invitations in the room are revoked; the last player out deletes the room with its chat and games. Rooms nobody has had open for 1 hour are deleted too |
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
| `POST /api/v1/rooms/{roomID}/chat/{messageID}/report` | `reason` 1–500 chars | Member | `204`; stores one report per reporter per message for moderator review; own or unknown messages rejected |
| `GET /api/v1/mutes`; `PUT` / `DELETE /api/v1/mutes/{userID}` | none | Verified | Muted accounts; mute hides that account's chat for the caller everywhere without touching friendships |
| `POST /api/v1/rooms/{roomID}/matches` | none | Host, waiting room | `201 {match_id}`; needs 2–5 members, all ready (`NOT_READY`, `NOT_ENOUGH_PLAYERS`); deals a fresh game, marks the room playing and revokes its invitations; nonmembers `404`, other members `403` |
| `GET /api/v1/matches/{matchID}` | none | Participant | The caller's projection (resync without taking control of the seat) |
| `POST /api/v1/matches/{matchID}/leave` | none | Participant | `204`; abandons the live match for everyone with no winner |
| `POST /api/v1/matches/{matchID}/abandon` | `vote` boolean | Participant | `204`; allowed while paused once a seat has been absent 5 minutes (`TOO_EARLY`, `NOT_PAUSED`, `NOT_CONNECTED`); a unanimous vote of present players abandons with no winner |

Client IDs and room IDs are identifiers, never credentials. Room membership is checked in the database for HTTP and WebSocket subscriptions. Session cookie is random 256-bit data; only its SHA-256 hash is stored. It is `HttpOnly`, `SameSite=Lax`, `Secure` for HTTPS, scoped to `/`. Sessions expire after seven days. Unsafe HTTP methods require an exact `Origin` match with `APP_ORIGIN`; WebSocket upgrades always require that exact origin. Cross-origin browser access is not enabled. Authentication endpoints have per-email or per-token limits (three mail requests or ten other attempts per minute) plus a higher source-address ceiling. The Next.js proxy forwards no client address, so limits never trust `X-Forwarded-For`. A successful login sets an HttpOnly `cardplay_device` cookie (path `/api/v1/auth/login`, 180 days; only its hash is stored, and it grants no access). Logins from a browser holding a known device cookie for that email get their own ten-per-minute bucket, so failed attempts sent by strangers cannot lock the owner out of a browser they have used before; unknown or invented device cookies share the email's bucket; social search, friend changes, room creation, joins and invites have account limits. The in-memory limiter applies per API instance; deployments with multiple API replicas need a shared edge or distributed limiter.

## WebSocket `/ws`

Use the authenticated cookie. A client hosted on another site, which cannot send that cookie with an upgrade, may instead present a single-use ticket as `?ticket=` (see `POST /api/v1/realtime/ticket`); a cookie that does arrive always wins and leaves the ticket unspent. No long-lived credential ever appears in a URL. The server closes with code `4001` when the session has ended (the client signs out) `4004` when the subscribed room is no longer available (the client returns to the lobby) and `4009` when another tab or device took control of the seat (the client offers to take it back and never reconnects on its own); other closes are transient and the client reconnects. The opening frame is `{"v":1,"type":"hello","payload":{"heartbeat_seconds":20}}`. Every client command has version `v:1`, a UUID `id` for correlation, `type`, optional `room_id` and optional `payload`. Frame limit: 16 KiB; up to 30 frames in 10 seconds. Server rechecks the session on every command and heartbeat.

| Client type | Payload | Server response |
|---|---|---|
| `ping` | none | `pong` with same `id` |
| `room.subscribe` | `room_id` | Authorized `room.snapshot` with same `id`, or `error` |
| `chat.send` | `room_id`, `{client_id,body}` | `ack` with `message_id`, or `error` |
| `match.subscribe` | `match_id` | Makes this socket the seat's controller (older ones close with `4009`) and replies `match.state`; nonparticipants get `NOT_FOUND` |
| `match.resync` | none | Fresh `match.state` for the subscribed match |
| `game.command` | `match_id`, `{command_id (UUID), expected_revision, kind, payload}` | `ack {revision, duplicate}` after the durable commit, or `error {code, message, revision}` |

The server pushes `match.state` (the caller's own projection with `revision` and `version`) after every committed change. While a match is playing it also carries `turn_seconds_left`: seconds until the server makes the default move for the awaited player. Clients count it down from when the state arrived. A default move adds a public `timed_out` event (`{user_id}`) before the game's events. Clients keep the highest `version`. Command pipeline, idempotency, pauses and recovery are in [09-multiplayer-and-recovery.md](09-multiplayer-and-recovery.md).

`room.updated` and `chat.updated` are durable outbox invalidations without state or message contents. Clients refetch the authorized HTTP view. After reconnect, send a fresh `room.subscribe` and reconcile against its room revision; no prior socket connection is trusted to have delivered all notifications. The frontend uses exponential backoff with jitter and cancels retries on component cleanup. An insert trigger on the outbox issues a PostgreSQL `NOTIFY` on commit, and every API instance `LISTEN`s, so clients connected to any replica receive every invalidation. When the listener reconnects, each subscribed client is told to refetch because notifications may have been missed. Invalidations may repeat; clients must treat them as hints. Outbox rows are kept for one day. A subscribed room heartbeat records authenticated presence every 20 seconds. In a waiting room, if the host has not been seen for 60 seconds, a worker checks every five seconds and transfers ownership to the longest-present member seen within 40 seconds. Row locks serialize this with a returning host, joins and explicit host changes.

## Error envelope

`{"error":{"code":"CODE","message":"Safe explanation","request_id":"..."}}`. Main codes: `INVALID_REQUEST` (400), `UNAUTHENTICATED` / `INVALID_CREDENTIALS` (401), `FORBIDDEN`, `EMAIL_UNVERIFIED`, `ORIGIN_REJECTED` (403), `NOT_FOUND` / `INVITATION_INVALID` (404), `METHOD_NOT_ALLOWED` (405), `CONFLICT`, `ROOM_STARTED`, `ROOM_FULL`, `IDEMPOTENCY_CONFLICT`, `STALE_REVISION`, `MATCH_PAUSED`, `MATCH_OVER`, `CONTROLLER_REPLACED`, `NOT_READY`, `NOT_ENOUGH_PLAYERS`, `TOO_EARLY`, `NOT_PAUSED`, `NOT_CONNECTED` (409), rule rejections such as `NOT_YOUR_TURN` or `NO_PLAYS_LEFT` (422), `RATE_LIMITED` (429), `INTERNAL`, `STATE_INVALID` (500), `GAME_NOT_READY` (501), `MAIL_UNAVAILABLE`, `NOT_READY` (503). Unexpected database errors are logged with SQLSTATE only and returned as `INTERNAL`, without query text or private values. WebSocket `error.payload` contains `code` and `message`, correlated by `id`.

## Database and future game boundary

Migration `000001` creates users, sessions, one-use account tokens, friendships, blocks, rooms, members, invitations, matches, participants, snapshots, events, deduplicated commands, outbox, chat and reports. Migration `000002` adds stable session IDs for revocation and a deletion marker for anonymized accounts. `000003` records room removals so old links cannot restore a kicked seat; `000004` records room member presence for host succession; `000005` adds the outbox notification trigger; `000006` stores hashed login-device tokens for rate limiting; `000007` adds match presence, abandon votes, a change version, end reasons and chat mutes. Room/chat paths use sqlc-generated pgx queries; migrations are embedded, checksummed and transactional under a PostgreSQL advisory lock. A version mismatch is a startup error. `migrate-down` is a local repair command, disabled in production. Account deletion retains referenced match/chat records under an anonymized handle and abandons any live match the account plays in.

`Game` has `Descriptor`, `New`, `Validate`, `Apply`, and `View`. `State` is raw server data. `View` has explicit `public`, `self` and legal choices; transports never marshal raw state. The match runner passes the authenticated actor ID and expected revision, and commits its transition, snapshot, typed public/private events, command ID/hash/result and outbox record in one transaction under a match row lock. Retrying the same command ID with the same hash returns the recorded result; a changed hash is a conflict. Running matches retain their registered rules version. The Monopoly 01723 engine (`internal/game/monopoly`) implements every rule with typed actions, explicit phases, seeded replay and per-seat projections ([traceability](08-monopoly-engine-traceability.md)); `internal/matches` is the transaction runner described here. Cambio has no module.


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
