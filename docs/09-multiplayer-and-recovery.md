# Live multiplayer and match recovery

This document covers how CardPlay runs Monopoly Deal matches over WebSockets and how a match survives disconnects, extra tabs and server restarts. Code: `internal/matches` (lifecycle and commands), `internal/realtime` (sockets), `internal/game/monopoly` (rules). Product rules cited as P04–P10 are in [01-product-requirements.md](01-product-requirements.md).

## Where state lives

PostgreSQL holds the only copy of every match:

| Table | Contents |
|---|---|
| `matches` | Status (`paused`, `playing`, `finished`, `abandoned`), `revision` (game state number), `version` (bumps on any visible change), winner, end reason |
| `match_participants` | Seat, controller generation, last heartbeat, absence time, abandon vote |
| `game_snapshots` | Full engine state per revision, including hands, deck order and seed. Server-only |
| `game_events` | Public events and private events tagged with one viewer |
| `game_commands` | Every command ID with its request hash and recorded result |
| `outbox` | Change notifications, committed with the change they describe |

Nothing important lives only in process memory. Notifications travel by PostgreSQL `LISTEN/NOTIFY`, fired by a trigger on commit. They are hints: a client that misses one still converges, because every reconnect and every push reads the durable state. There is no Redis and no in-memory copy of a match.

## Command pipeline

A client sends `game.command` with a UUID `command_id`, the `expected_revision` it was looking at, a `kind` and a `payload`. The server handles it in one transaction:

1. Lock the match row (`SELECT … FOR UPDATE`). This serializes commands per match across every API instance.
2. Reject if this socket no longer controls the seat (`CONTROLLER_REPLACED`).
3. If this player already sent this command ID, return the recorded answer: the same acknowledgement, or the same rule rejection. The same ID with a different request is `IDEMPOTENCY_CONFLICT`.
4. Reject if the match ended (`MATCH_OVER`) or is paused (`MATCH_PAUSED`).
5. Reject if `expected_revision` is not the current revision (`STALE_REVISION`, carrying the current revision). The client refreshes and the player decides again.
6. Apply the command with the rules engine. A rule rejection is recorded against the command ID and changes nothing.
7. Otherwise write the new snapshot, its events, the command record and an outbox notification, then commit.

Only after the commit does the client get `ack` with the new revision. An acknowledged command is therefore durable and applied exactly once, and a retry after a lost acknowledgement returns the same result.

After each commit every connected participant's socket reloads the latest snapshot and sends that player **their own projection** (`match.state`). Projections come only from the engine's per-seat view: your hand, everyone's hand counts, public tables, the pending action, and events whose audience is public or you. Draw order, other hands, the seed and whether anyone holds a Just Say No are never sent. `version` lets clients drop a push that arrives out of order.

## Seats, tabs and presence

- **One controller per seat (P08).** `match.subscribe` increments the seat's controller generation. The previous tab or device is closed with WebSocket code `4009`, and its commands and heartbeats are refused. The web client shows "open in another tab" with a **Play here instead** button, and never reconnects on its own after `4009`, so two tabs cannot keep taking the seat from each other.
- **Heartbeats.** Every 20 seconds each controller socket records presence. A socket that closes marks its seat absent immediately. A socket that vanishes silently is marked absent by a sweep, which runs every 5 seconds, once 45 seconds pass without a heartbeat. If such a controller was only slow, its next heartbeat restores the seat.
- **Refresh.** The browser remembers the open room per tab. On reload it resubscribes, takes back its own seat and receives the exact committed state, including any pending response, payment or placement it still owes.

## Pauses, abandonment and endings (P09, P10)

**Turn timeout** (owner decision 2026-09-30, P09). The match records when it started waiting for the current move (`awaiting_since`). Every applied command restarts that clock, and so does resuming from a pause. A paused match never times out.

When a *connected* player the game is waiting on makes no move for `MATCH_TURN_TIMEOUT` (default 2 minutes; `0` turns it off), the sweep (every 5 seconds) asks the rules engine for the most passive legal move. It applies that move through the same durable path as a player's command:

- **Play phase:** end the turn, returning the lowest-value cards over seven.
- **An action against you, or a Just Say No exchange:** accept, so the effect or the last Just Say No stands.
- **Payment:** bank first, then detached buildings, then property cards and attached buildings, lowest value first, stopping once the debt is covered. If the table is worth less than the debt, everything goes.
- **Received properties:** into an incomplete set of a legal color, otherwise a new set; a multicolor wild stays unassigned.

The server keeps moving for the same player until the game waits on someone else, so a whole decision completes (for example accept, then pay). It never plays a card from hand onto the table and never targets anyone.

- Each such move is announced with a public `timed_out` event before the game's own events, and recorded in `game_commands` with `"timeout": true`.
- Clients get `turn_seconds_left` in every `match.state` and count down locally.
- The match lock, plus a recheck of the clock under it, means two API instances never both move.

Covered by `TestTimeoutActionIsAlwaysLegal` (200 random games), `TestTimeoutActionChoices`, the "an idle player's turn times out" live-match subtest, and the Playwright action spec.

| Situation | Behavior |
|---|---|
| Awaited player connected but idle for 2 minutes | The server makes the default move for them (see above); play continues |
| Any seat absent | Match `paused`; every command is refused and the turn clock stops. It resumes automatically when all seats are present again, with a fresh clock |
| New match | Starts `paused` and begins once every player has opened it |
| Seat absent 5+ minutes | Players still present may vote to abandon. A unanimous vote ends the match with no winner (`voted`). Votes clear if the absent player returns |
| Everyone absent 24 hours | The sweep abandons the match with no winner (`expired`) |
| A player chooses **Leave match**, or deletes their account | The match is abandoned with no winner (`left`), and no cards are redistributed |
| Three different complete sets on your turn | The match is `finished` with that winner (`won`) |

When a match ends, the room returns to the lobby with readiness cleared, so a rematch needs everyone ready again and deals a fresh shuffle. The last result stays visible in the room. Intermediate snapshots of ended matches are pruned hourly, and ended matches are deleted after 90 days (P10). While a match runs, the room accepts no joins, kicks or setting changes, and invitations are revoked when it starts (P03, P04).

## Recovery after a server restart

1. Every match's state, pending actions and payments are already committed. Nothing is replayed or rebuilt from memory.
2. The restarted server starts `LISTEN` and asks any already-connected clients to refetch, closing the window between startup and listening.
3. Browsers reconnect automatically with backoff. Each resubscribes to its seat and receives its projection of the latest committed revision.
4. Until everyone is back, the match shows as paused. Seats whose sockets died without closing are marked absent by the sweep after 45 seconds. When every player has reconnected, play resumes from exactly where it stopped.
5. A command that was in flight when the server stopped either committed (and a retry returns its recorded result) or did not (and the retry applies it once).

With several API instances, the database lock and the controller generation keep one serial order per match. Each instance delivers notifications to its own sockets.

## Chat (P06)

Room chat works in the lobby, during a match and after it. It is members-only plain text: at most 500 characters and 5 messages per 10 seconds per account, kept for 7 days. Moderation tools:
- **Report** a message; reports are stored for moderator review in `chat_reports`, one per reporter per message.
- **Mute** an account to hide its chat for you everywhere, without affecting friendships.
- **Block** also hides chat.
- An unread badge appears when new messages arrive while the chat panel is out of view.

Game events never put hand contents in chat.

## Verification

- `internal/server/match_integration_test.go` (real PostgreSQL and WebSockets). Covers:
  - Start rules, unauthorized subscriptions and hidden-hand checks on raw frames.
  - Stale revisions, duplicate and conflicting command IDs, and recorded rejections.
  - Eight concurrent commands at one revision, of which exactly one applies.
  - Tab takeover, pause and resume on disconnect, and silent-drop detection.
  - A full server restart, abandon voting, leave, a winning finish, 24-hour expiry, chat report and mute, and account deletion mid-match.
- `internal/server/realtime_integration_test.go`: notifications reach sockets on a second API instance.
- The Playwright suite in `scripts/e2e` drives real browser sessions through start, chat with the unread badge, refresh, a second tab taking and returning a seat, a dropped connection and recovery, an API kill and restart mid-match, a complete match to a winner, report, mute and leave. It checks every WebSocket frame for hidden-card leaks.
