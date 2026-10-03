# Trump: rules specification and implementation plan

Trump is a four-player, two-team, trick-taking game with a chosen trump suit. This document is the specification agreed before implementation: the rules, the state machine, the server actions, the screens, the rule-to-test map and the plan. Monopoly Deal is unaffected; Trump lives in its own engine package behind the same game boundary.

**Status: rules confirmed by the owner on 2026-10-03** (§9). Phase 2, the engine, is implemented and tested; phases 3-5 remain.

## 1. How it fits the existing platform

The platform already provides everything that is not game rules: accounts and guests, rooms, invitations, presence, WebSockets, durable exactly-once commands, per-player projections, pause and resume, turn timeouts and chat. Trump reuses all of it.

| Layer | Reused as is | Needs work |
|---|---|---|
| `internal/game` boundary (`Game`, `Setup`, `Transition`, `View`, `TimeoutPolicy`, `CardCatalog`) | ✅ | — |
| `internal/matches` runtime: locking, idempotency, revisions, snapshots, events, outbox | ✅ | — |
| `internal/rooms` | Membership, seats, ready, host, invitations | **A room cannot choose its game today.** `rooms.game_id` defaults to `monopoly-deal` and `POST /api/v1/rooms` accepts only name and capacity ([rooms.go:94](../internal/rooms/rooms.go#L94)). Trump needs the game chosen at creation and the capacity pinned to 4. |
| `internal/chat` | Room chat, mute, report | **Chat must be closed during trump selection** (rule 4.6). Needs a gate the game module controls. |
| Web client | Entry, games screen, lobby, room, invite links, chat drawer, reconnection | A Trump table, a trump-selection sheet and a round-result sheet. The existing table is Monopoly-specific. |
| Database | `matches`, `game_snapshots`, `game_events`, `game_commands`, `outbox` | No new tables. Trump state is one JSON snapshot like Monopoly's. |

**Seats and teams.** `room_members.seat` is `0..4` and the runtime seats participants in member order ([runtime.go:619](../internal/matches/runtime.go#L619)), so seats 0–3 work unchanged.

**A Trump match never "finishes".** The platform's `Outcome.Finished` marks a match winner; Trump has rounds but no session victory condition, so `Finished` stays false and a match ends only when players leave or abandon it. Round results live in the game state, not in `matches.winner_id`.

## 2. Rules

### 2.1 Players, teams, seating
1. Exactly four players. The match cannot start with any other number.
2. Seats 0 and 2 are Team A; seats 1 and 3 are Team B. Teammates sit opposite.
3. Play order is by ascending seat, wrapping 3 → 0 (confirmed 2026-10-03).
4. Each client renders its own seat at the bottom, its partner at the top, opponents left and right, without changing the real order.

### 2.2 Cards
5. A standard 52-card deck, no jokers: four suits (spades, hearts, diamonds, clubs) × thirteen ranks.
6. Ranks high to low: A K Q J 10 9 8 7 6 5 4 3 2.
7. Every physical card has a unique ID, `<suit>-<rank>`, for example `spades-a`, `hearts-10`.
8. Shuffling is server-side and unpredictable: a 32-byte `crypto/rand` seed per match, then ChaCha8 keyed by `SHA-256(seed ‖ counter)`, the same construction Monopoly uses.

### 2.3 The toss
9. One toss per **match**, performed when the match is created. It selects the team entitled to choose trump in round 1.
10. Reconnecting, refreshing, a server restart or a later round must never re-toss. The result is in the stored state.
11. Later rounds: the team that won the previous round is entitled to choose.
12. A new match (a fresh session with the same four people) tosses again.

### 2.4 Dealing
13. Each player is dealt **five** cards before trump selection. A player sees only their own cards.
14. After trump is chosen, the remaining **eight** cards are dealt to each player, giving everyone 13 (confirmed 2026-10-03).
15. The deal is a single shuffle of the 52 cards; the first five per player and then the next eight come off the same shuffled deck.

### 2.5 Choosing trump
16. The toss names a **team**, not a player. **Either** member of that team may take the decision, seeing only their own five cards; the first of the two to act becomes the decider (owner decision 2026-10-03). Whether they confer beforehand is their own business, and chat is closed, so nothing can be signalled in the app.
17. That player may either **choose a suit** or **delegate**, through an action labelled "My teammate will choose trump".
18. After delegation the teammate must choose, using only their own five cards. Delegation cannot be returned or repeated.
19. The player who actually chooses leads the first trick.
20. During this phase no chat, quick replies or reactions are accepted from anyone in the room; only the two selection actions exist. Chat reopens the moment the trump is named and stays open for the rest of the round (owner decision 2026-10-03).
21. A teammate's hand is never sent to the other player, before or after delegation.

### 2.6 Playing a trick
22. A trick is one card from each of the four players, in seat order starting from the leader.
23. The first card sets the lead suit.
24. A player holding any card of the lead suit must play one of that suit.
25. A player holding none may play any card, including trump. Trumping is optional and there is no obligation to beat the card currently winning.

### 2.7 Winning a trick
26. If any trump was played, the highest trump wins. Otherwise the highest card of the lead suit wins. A card that is neither trump nor the lead suit cannot win (confirmed 2026-10-03).
27. The winner's team gains one trick; the winning player leads the next trick.

### 2.8 Winning a round
28. The first team to **seven** tricks wins the round, which ends immediately; the remaining cards are not played (confirmed 2026-10-03).
29. The result records the winning team, both trick counts, the trump suit and who chose it, appended to the round history.
30. Rounds won and tricks won are tracked separately.
31. All four players ready up for the next round; the round number increases, a fresh 52-card shuffle is dealt, and the previous round's winners choose trump by the same five-card process.
32. There is no series, no points formula and no overall session winner.

### 2.9 Leaving and timeouts
33. **No automatic move, forfeit or timeout applies to a Trump match** (owner instruction 2026-10-03): the module does not implement `game.TimeoutPolicy`, so a connected but idle player is waited for indefinitely. The engine can still compute the most passive legal move (`State.TimeoutAction`) if a policy is approved later. A player who permanently leaves mid-round ends the match with no winner and no round credited, exactly as Monopoly does today (`end_reason` `left`). The other three return to the room and may start again (owner decision 2026-10-03). The platform already behaves this way; no new code is needed.
34. Turn timeout (the platform's 2 minutes) proposes a default move, as Monopoly does: during selection, choose the suit the player holds most of (ties by suit order); during play, the lowest legal card. Recommended, not an owner decision — tell me if you want something else.

## 3. State machine

```
            match created
                 │
                 ▼
          ┌────────────┐   one toss per match, never repeated
          │   TOSS     │   picks entitled team (+ designated chooser)
          └─────┬──────┘
                │ deal 5 each
                ▼
       ┌──────────────────┐   choose_trump ──────────────┐
       │ TRUMP_SELECTION  │                              │
       │ chat closed      │   delegate_trump             │
       └─────┬────────────┘        │                     │
             │                     ▼                     │
             │            ┌──────────────────┐           │
             │            │ TRUMP_DELEGATED  │ choose_trump
             │            │ teammate decides │───────────┤
             │            └──────────────────┘           │
             │                                           │
             └───────────────────┬───────────────────────┘
                                 │ deal remaining 8 each (13 total)
                                 ▼
                         ┌───────────────┐
                 ┌──────▶│     PLAY      │  play_card ×4 = one trick
                 │       └───────┬───────┘
     trick won,  │               │ a team reaches 7 tricks
     winner      │               ▼
     leads next  │       ┌───────────────┐
                 └───────│  ROUND_OVER   │  result + history
                         └───────┬───────┘
                                 │ all four ready_round
                                 ▼
                      next round: entitled team = winners
                      (no new toss) → TRUMP_SELECTION
```

Phases: `toss` is resolved inside match creation, so the first stored state is already `trump_selection`.

### Stages

`Stage()` names every state the rules distinguish, derived purely from stored fields so a reconnecting client and the server always agree ([stage.go](../internal/game/trump/stage.go)).

| Stage | Meaning |
|---|---|
| `waiting_for_players` | Fewer than four seats. The platform starts a match only with four, and `NewGame` refuses anything else, so no stored state rests here |
| `initial_deal` | **Transition**: five cards to each player. Reported on the `dealt_five` events |
| `trump_decision` | Either member of the entitled team may choose or delegate |
| `delegated_trump_decision` | The teammate must choose and cannot pass back |
| `remaining_deal` | **Transition**: eight more cards each. Reported on the `dealt_rest` events |
| `active_trick` | Waiting for the next card |
| `completed_trick` | The finished trick stays on the table until its winner leads, so clients can show who took it |
| `completed_round` | Seven tricks reached, nobody ready yet |
| `waiting_for_readiness` | Some players ready for the next round |

Dealing is automatic, so the two deal stages are transitions rather than resting states; naming them on the events that perform them keeps all nine explicit without inventing states a player would have to act out of.

**Randomness and time.** Randomness is injected: `NewGame` takes an `io.Reader` for the 32-byte seed (`crypto/rand` in production, fixed bytes in tests), and every later deal derives from it, so a match replays exactly. The engine needs no clock: it has no deadline, timestamp or expiry of its own, and the turn timeout belongs to the platform, which already records `awaiting_since` per match. If round history should carry timestamps, that is a clock worth injecting; say so and I will add it.

## 4. Server actions

Commands arrive over the existing WebSocket `game.command` envelope and are applied through the same durable path.

| Command | Payload | Who | Effect |
|---|---|---|---|
| `choose_trump` | `{"suit":"spades"}` | The designated chooser, or the delegate after delegation | Sets trump, deals the remaining eight to each player, the chooser leads |
| `delegate_trump` | `{}` | The designated chooser only, once | Passes the decision to the teammate |
| `play_card` | `{"card":"hearts-q"}` | The player to act | Plays one legal card; completes the trick on the fourth |
| `ready_round` | `{}` | Any seat, in `round_over` | Marks that player ready; the fourth starts the next round |

Rejections reuse the platform's codes: `NOT_YOUR_TURN`, `WRONG_PHASE`, `INVALID_CARD`, `ILLEGAL` (for example not following suit), `STALE_REVISION`.

Events (public unless noted): `toss_resolved`, `dealt_five` (private per seat), `trump_delegated`, `trump_chosen`, `dealt_rest` (private per seat), `card_played`, `trick_won`, `round_won`, `round_started`.

### Projection

`public`: phase, trump suit (null before selection), round number, rounds won per team, tricks won per team this round, seat → team map, whose turn, the current trick's cards in play order, the lead suit, each player's card count, who chooses trump and whether it was delegated, round history.

`self`: your hand, and which of your cards are legal right now (so the client can dim the rest), plus your role in selection.

Never projected: another player's cards, the undealt remainder, the seed. The existing Playwright leak check applies unchanged.

## 5. Frontend screens

1. **Games screen** — Trump becomes playable; choosing it leads to the lobby. Room creation must send the chosen game and pin capacity to 4.
2. **Room lobby** — as today, plus the team split (You and your partner opposite) and "needs exactly 4 players".
3. **Trump table** — own seat bottom, partner top, opponents left and right; the current trick in the middle; a trump badge; per-team trick counters; round and rounds-won; the turn clock; your hand as a rail with illegal cards dimmed and unplayable.
4. **Trump selection sheet** — your five cards, four large suit buttons, and "My teammate will choose trump"; other players see a waiting state naming who is deciding; the chat button is disabled with a short explanation.
5. **Round result sheet** — winning team, trick counts, trump that was played, round history, and "Ready for next round" with who is ready.
6. **Responsive** — phone first: the table fits 412 px with no sideways scroll, sheets are bottom sheets under 768 px and modals above.

## 6. Rule-to-test checklist

| Rule | Test |
|---|---|
| 5–7 deck shape, unique IDs | `TestDeck` (52 cards, 4×13, IDs unique and parseable) |
| 6 rank order | `TestRankOrder` |
| 8 shuffle | `TestShuffleIsSeeded` (same seed ⇒ same deal; different seed ⇒ different) |
| 1–2 four players, team map | `TestSeating` (rejects 3 or 5 players; seats 0,2 vs 1,3) |
| 9–12 one toss per match | `TestTossHappensOnce` (reconnect, round 2, restart) |
| 13–15 deal sizes | `TestDealFiveThenEight` |
| 16–19 selection and delegation | `TestTrumpSelection` (chooser only; delegate once; no return; chooser leads) |
| 20 chat closed | `TestChatClosedDuringSelection` (server rejects, integration) |
| 21 hand privacy | `TestNoTeammateLeak` + the Playwright frame check |
| 24 follow suit | `TestMustFollowSuit` |
| 25 trumping optional | `TestTrumpingIsOptional` |
| 26 trick winner | `TestTrickWinner` (all trump, no trump, off-suit never wins, low trump beats high plain) |
| 27 winner leads | `TestWinnerLeadsNext` |
| 28 seven tricks | `TestRoundEndsAtSeven` (stops immediately) |
| 29–31 history, next round | `TestNextRoundEntitlement` |
| 30 rounds vs tricks | `TestCountersAreSeparate` |
| 33 leaving | `TestLeaveAbandonsMatch` (platform integration; the engine needs no change) |
| 34 timeouts | `TestTimeoutMoves` + `TestTimeoutIsAlwaysLegal` (random games) |
| Invariants | `TestRandomGames`: 52 cards conserved every transition, no duplicate card, hand sizes equal, ≤13 tricks |

End-to-end (Playwright, four browser contexts): create a Trump room, four guests join, toss shown, selection with delegation, a full round to seven tricks, round result, ready up, second round entitlement, refresh mid-trick, and the hidden-card frame check.

## 7. Implementation plan

| Phase | Work | Verification |
|---|---|---|
| **1. Specify** (this document) | Inspect, specify, confirm assumptions | Owner sign-off |
| **2. Engine** | `internal/game/trump`: cards, deal, toss, selection, trick play, round scoring, invariants, timeout policy. Pure Go, no I/O | Unit tests from §6, plus randomised games |
| **3. Platform** | `Module` adapter; room game selection (API + capacity 4); chat gate; register in the server registry | Go integration tests against real PostgreSQL and WebSockets |
| **4. Client** | Trump table, selection sheet, round result, games screen and lobby changes, responsive CSS | Typecheck, lint, screenshots at 412 px and 1280 px |
| **5. Harden** | Playwright four-player spec, leak check, docs, release checklist | Full suite green |

Each phase ends with a short report. Monopoly Deal is not touched in any phase; the only shared files are the games list, the room creation path and the chat gate, each additive.

## 9. Owner decisions, 3 October 2026

| Question | Decision |
|---|---|
| Play order | Ascending seat, 0 → 1 → 2 → 3 → 0 |
| Dealing | Five each, trump chosen, then eight each for thirteen |
| Who chooses trump | The toss names a **team**; either member may take the decision, and the first to act decides. That player may name the suit or pass it once to their partner, who must then choose |
| Chat | Closed during trump selection, open again from the moment trump is named |
| Trick winner | Highest trump, otherwise highest card of the lead suit |
| Round end | Stops the instant a team reaches seven tricks |
| A player leaves mid-round | The match is abandoned with no winner, as Monopoly does |

## 10. Build status

| Phase | State |
|---|---|
| 1. Specify | ✅ this document |
| 2. Engine (`internal/game/trump`) | ✅ nine explicit stages, cards, seating, toss, deals, selection with delegation, legal-card calculation, trick play and resolution, team trick counts, seven-trick victory, next-round initialisation, per-player views, invariants, timeout policy, module adapter. 52 tests, 89.6% coverage |
| 3. Platform (room game choice, chat gate, registry) | ✅ registered in the server registry; rooms choose their game and have the seat count pinned by its descriptor; chat gated by `game.ChatPolicy`; `TestTrumpLiveMatch` covers authorization, serialization, out-of-turn, stale, malformed and duplicate commands, reconnect, restart and projection privacy |
| 4. Client (table, selection sheet, round result) | ✅ `TrumpTable` with four seats, teams, counters, trick area, selection and round sheets; Trump playable from the games screen; `trump.spec.ts` (10 browser tests) |
| 5. Verification | ✅ 52 engine tests (89.7% coverage), 14 live integration subtests, 12 browser tests across four sessions. See §11 |
| 6. Release preparation | ✅ socket hardening verified (6 subtests); a complete four-player match on staging using the production binary and standalone client; recovery from a database copy; release, rollback and backup procedures documented. See §12 |

## 11. Verification, 3 October 2026

Every behaviour below was run, not reasoned about. Engine tests are pure Go; integration tests drive real WebSockets against PostgreSQL; browser tests drive four isolated sessions through the built client.

| Behaviour | Where it is proven |
|---|---|
| Four players join with the right seats and teams | `TestSeating`; `TestTrumpLiveMatch/seats_and_teams_are_as_the_rules_say`; browser "the table shows seats, teams, counters" |
| The first deal exposes only five cards per player | `TestNewGame`, `TestDealingCounts`; integration checks all four hands; browser asserts five per player |
| Trump chosen directly | Browser "the second round's trump can be chosen directly"; `TestTrumpSelection/either_teammate_may_act_first` |
| Trump delegated once, no passing back | `TestTrumpSelection/delegation_locks_the_choice`; integration refuses the delegating player; browser "the choice can be passed to the partner" |
| The rest of the deal: eight more, thirteen each | `TestDealingCounts`; integration; browser polls every hand to 13 |
| Following suit is mandatory | `TestPlayingATrick`, `TestLegalListMatchesEnforcement`; browser dims illegal cards with the reason |
| Trumping is optional when void | `TestTrumpsAndDiscards` finds a real void and accepts both the trump and the discard |
| Highest trump, else highest lead suit | `TestCardBeats` (8 cases), `TestTrickWinnerAndLead`, `TestTrumpsAndDiscards` (4 cases) |
| The trick winner leads next | `TestLeaders` across several tricks; browser shows "Leads next" |
| Seven tricks ends the round | `TestRoundEndsAtSeven`; browser plays a full round and asserts exactly 7 with cards unplayed |
| The winners choose next round's trump | `TestTossHappensOnce`; browser checks `entitled_team` after readying up |
| One toss per session | `TestTossHappensOnce` (round 2 inherits); integration confirms it across reconnect and restart |
| Reconnection restores the private hand and public state | `TestTrumpLiveMatch/a_refresh_re-reads_the_committed_state`: same revision, same 12 cards, trick intact, old socket closed 4009 |
| Duplicate actions cannot corrupt state | Integration: a repeated command ID returns its recorded ack and plays no second card |
| Concurrent actions cannot corrupt state | Integration: eight simultaneous plays at one revision, exactly one applies, revision advances by one |
| Unauthorized clients cannot observe or act | Integration: an outsider gets 404 on the match, the room and chat, and is refused both `match.subscribe` and `game.command` |
| No hidden information in projections | Integration compares every player's projection with the server's snapshot: no other hand, no undealt rest, no seed |
| The selection-phase chat restriction cannot be bypassed | Integration refuses it over HTTP **and** over the WebSocket, then confirms nothing was stored |
| Server restart recovery | Integration: a second instance serves the committed revision, trump and trick |
| Mobile layout | Browser at 412px: the page cannot be scrolled sideways, the body fits, and the hand still scrolls in its own rail |

**Not covered, deliberately:** the race detector does not run on this machine (no 64-bit C toolchain), so it runs in CI. No automatic timeout policy exists for Trump, so there is nothing to test for it.

## 12. Release preparation, 3 October 2026

**Staging.** The production artifacts were run against their own database: the release binary (`cardplay serve`, metrics on a private port) and the standalone Next.js server assembled exactly as [web/Dockerfile](../web/Dockerfile) does. A complete four-player match was played on it — room of four, toss, delegation, a full round ending 7–3 after 40 cards, round history, ready-up, the winners choosing next, and a mid-game refresh that restored the same private hand. 14/14 checks, no page errors. The script is [scripts/e2e/staging-trump.mjs](../scripts/e2e/staging-trump.mjs).

**Operational endpoints.** `/healthz` returns ok; `/readyz` reports database, migrations and notifications; metrics serve on `METRICS_ADDR` only and return 404 on the public port; logs are structured JSON lines with a request ID per request.

**Socket hardening.** Verified by `TestSocketHardening`: a foreign or missing origin is refused with 403, an unauthenticated socket with 401, a room the player is not in cannot be subscribed, an oversized frame and a flood of frames both close the connection, and a shutdown closes sockets while committed state survives.

**Recovery.** A copy of the staging database was compared table by table with content digests — all 21 identical — and a server started against the copy reported ready. `pg_dump`/`pg_restore` could not be exercised on this machine because the scratch PostgreSQL install has no client tools; the archive drill in [10-operations.md](10-operations.md) remains the record for that path.

**Not verified here:** TLS termination, a real SIGTERM (Windows has no equivalent; shutdown was exercised through context cancellation), the race detector (no 64-bit C toolchain locally; it runs in CI), and behaviour under real network loss or sustained load.
