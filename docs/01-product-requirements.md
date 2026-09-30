# Product requirements

## Objective and scope

CardPlay lets friends play multiplayer card games in a browser with clear rules, private hands and reliable recovery. Release one supports Monopoly Deal, US 01723 rules, one 106-card playing deck, 2–5 players. Cambio is a future game, not part of this release. No game code is authorized in this planning phase.

Everything below is a **product decision**, unless explicitly attributed to the rule specification. These choices do not claim to be printed game rules.

## Release requirements

| ID | Requirement and chosen behavior |
|---|---|
| P01 Accounts | Verified email/password accounts; sign up, sign in/out, verification resend, password reset, session revocation and account deletion. Unique public handle plus editable display name; never expose email in rooms. Invitations preserve their destination through authentication. Guest play and social sign-in deferred. |
| P02 Friends | Search by exact handle; request, accept, decline, cancel, remove and block. No email lookup or public directory. Duplicate/crossed requests resolve atomically. Presence shown to accepted friends; users can hide presence. Blocking stops future requests/invitations and hides that person's chat locally. |
| P03 Invitations | In-app invitations to accepted friends plus revocable room links. Invitations expire after 30 minutes or when the room starts/closes; acceptance does not reserve a seat. Show full/expired/started/blocked states accurately. No email or push invitations in v1. Room membership is authorized by the server; a room code never authenticates a player. |
| P04 Rooms | Private rooms by default, capacity 2–5; host creates, invites, removes waiting players and starts when everyone is ready. Pin edition/rule version; no house-rule toggles in v1. Changing seats resets readiness. Random first seat, fixed clockwise seat order thereafter. No new seats, host kicks or spectators during a match. Lobby host transfers to the longest-present member after a 60-second absence. |
| P05 Play | Private hand; visible banks, property sets, hand counts, active player, plays remaining and public action history. Readable card inspection; legal-action choices; explicit target, set and payment selection; response chains explained in plain language. Server validates every command and computes rent. End-turn excess selection is distinct from ordinary played actions. |
| P06 Chat | Room text chat in lobby, match and results; 500-character limit and 5 messages per 10 seconds per account. Plain text only; unread indicator; mute/block/report. Chat remains usable while waiting for a response. Store for 7 days, restrict to room members and authorized moderation; no hand contents in automatic messages. Friend direct messaging deferred. |
| P07 Responsive access | Support portrait phone, tablet and desktop starting at 360 CSS px width. Desktop table overview; mobile opponent selector with persistent all-player status, own hand and pending-action banner. Every drag action has tap/click and keyboard alternatives. Color names/icons supplement color bands; visible focus, labeled dialogs, focus restoration, reduced motion, optional sound and independent volume. |
| P08 Reconnection | Seat ownership belongs to authenticated account, never name or public player ID. One active controller per seat; new authenticated controller invalidates the old one. Refresh/reconnect restores hand, selected server-confirmed state, turn and exact pending obligation without replaying a command. Server persists each accepted game transition before acknowledgement. |
| P09 Absence and abandonment | Turn timeout (owner decision 2026-09-30, replacing the original "no turn timeout" rule): when a connected player the game is waiting on makes no move for 2 minutes (`MATCH_TURN_TIMEOUT`), the server makes the most passive legal move for them (end the turn, accept, pay bank-first lowest value, place received cards). No other automatic card choices. Pause when a required actor disconnects; also pause at detection of any seat disconnect to avoid unequal targeting. Reserve seat and state for at least 5 minutes. Thereafter remaining players may unanimously abandon without a winner, or keep waiting. All-offline matches expire after 24 hours with no winner. Explicit leave during play is abandonment, not card redistribution. These are service lifecycle choices. |
| P10 Results | Show verified winner, final public sets, edition and rematch option; rematch requires readiness, new shuffle and first-seat selection. Abandoned games are clearly labeled and never recorded as rule wins. Retain minimal match results for 90 days; do not expose hidden hands in results. |
| P11 Future games | Shared accounts, friends, invitations, rooms, chat and sessions. Each game provides versioned rules, state validation, commands, per-seat views and its own UI. Store `gameId` and `rulesVersion` on every match. Cambio may have different information and response rules; do not embed Monopoly assumptions into room services. |

## Reliability and delivery targets

- Server is authoritative; clients request actions and cannot assign ownership, debt, deck order or winners. Use runtime schemas, authorization, unique command IDs and state revisions. Reject stale/duplicate commands safely.
- Keep hands and draw order out of other players' network payloads, analytics and chat. Use explicit public/private serializers, secure sessions, TLS, input limits and rate limits.
- Target p95 accepted-action acknowledgement below 500 ms at 100 concurrent five-player rooms in a same-region load test; measure from connected client send to acknowledged durable commit. Rejoin target: within 5 seconds of restored connectivity under that test profile. This is a release target, not a measured result.
- Crash/restart recovery must preserve every acknowledged command, pending counter and payment. Backups, restore exercise, health/readiness checks, structured logs, alerting and rollback procedures are release requirements.
- Retention periods above are provisional product defaults to be reflected in the privacy interface. Deleting an account removes credentials and personal profile, anonymizes retained match results and invalidates sessions.

## Exclusions

No Cambio implementation, ranked play, wagering, tournaments, matchmaking, bots, spectator mode, public rooms, user uploads, voice chat, two-deck mode or configurable rule variants in v1. English UI first; keep text externalizable. Create original CardPlay branding and card UI; asset clearance is a separate release gate.
