# Bluff

Three to six players, one 52-card deck, no teams. Play to the called rank
whether or not you hold it; anyone may call the claim, and the whole pile
goes to whoever was wrong. First player with no cards left, once their last
play survives, wins.

This document records what was built and why, so the next person does not
have to read the engine to find out.

## The rules as implemented

| # | Rule | Where |
|---|---|---|
| 1 | Everyone plays for themselves; 3–6 seats; one deck, no jokers; suits mean nothing | [cards.go](../internal/game/bluff/cards.go), `MinPlayers`/`MaxPlayers` |
| 2 | A fresh deck is shuffled per game and dealt one card at a time, clockwise, from a seat drawn at random; the first player is drawn separately | `NewGame` in [engine.go](../internal/game/bluff/engine.go) |
| 3 | The call advances Ace → 2 → … → King → Ace after **every** play that ends, challenged or not | `advance`, `NextRank` |
| 4 | A play is one to four cards the player holds; they may match the call, miss it entirely, or mix | `play` |
| 5 | Everything played goes face down on one pile; the latest batch is identifiable and private | `State.Pile`, `State.Last` |
| 6 | A challenge reveals **only** the latest batch; the whole pile goes to the challenger if the claim held and to the player if it did not | `challenge` |
| 7 | Play always continues clockwise from the player who was challenged; winning a challenge is not a turn | `advance(last.Seat)` |
| 8 | A last card makes a pending winner, confirmed when the play survives; a bluff nobody calls still wins | `play`, `closeWindow`, `challenge` |

## More than one deck

A room may be played with **one to four decks** (a room setting beside
capacity; the other games have fixed decks by rule). Everything about the
game is unchanged -- there are simply more cards.

- Every copy is its own card. The first deck keeps the plain ids, so a
  one-deck game reads exactly as before; later decks carry a copy marker,
  so `q-hearts#2` is the second deck's queen of hearts. Rank and suit read
  through the marker, which is why no rule had to learn about copies.
- A play may hold four of a rank **per deck**: up to eight with two decks,
  sixteen with four. The table takes that number from the server, so the
  controls and the refusals follow on their own.
- The whole lot is still dealt out: 104 cards across three players is
  35/35/34.
- The count reaches the engine through `game.Setup.Options`, a room's
  settings as JSON, rather than a field on the boundary that only one game
  would use. A room that never set it, or a client that has never heard of
  it, plays with one deck, and a settings update that omits it leaves it
  alone rather than resetting it.

Hands are dealt as evenly as the deck allows: 3 players get 18/17/17, four
get 13 each, five get 11/11/10/10/10, six get 9/9/9/9/8/8. Because the
dealing seat is drawn at random, the spare cards do not always fall on the
same seats, and the lead is drawn separately again.

## Timing: the challenge window

These are CardPlay policies, not rules of Bluff.

- Every accepted play opens a **five-second** window (`ChallengeWindow`).
- Nobody may play while it is open; the phase is `challenge`.
- Each eligible player may **Accept** (binding — they cannot then call it)
  or **Call bluff**. The player who made the play may do neither.
- If every eligible player accepts, the window closes early.
- Silence until the deadline counts as accepting.
- The first valid challenge the server commits is the one that counts.
  Commands for a match are serialised, so two challenges arriving together
  are applied one after the other and the second is refused as a stale play:
  one challenge, one penalty, ever.
- Late is decided by the server's receipt time. A challenge or acceptance
  arriving after the deadline is refused.
- There is no automatic card-play timer in this release. A player who never
  plays holds the table; the room's existing abandon vote is the way out.

### How a deadline is enforced

The platform's turn timeout is two minutes, which is the wrong clock for a
five-second window, so two small things were added to the platform rather
than worked around inside the game:

1. **A stamped clock.** `game.WithNow` / `game.Now` carry the server's time
   into `Apply`. The engine reads it instead of calling `time.Now`, so the
   rules stay testable and a client's clock never decides anything.
2. **`game.Paced`.** A game may declare a deadline held in its own state.
   The sweep reads that deadline rather than measuring idleness, which
   matters because an acceptance arriving at the last moment must not push
   the window out. Expiry then runs through the existing timeout machinery:
   `TimeoutMoves` returns one move — close the window — attributed to the
   player whose play it was. **No card is ever played for anybody.**

The sweep runs once a second against a query narrowed to paced games, so a
window closes within about a second of its deadline.

## What is private

Public: seats, names, connection, hand counts, the current player, the
call, the last declaration (seat, count, rank), the pile size, the
deadline, who has accepted, the cards and outcome of a **resolved**
challenge, the pending winner and the winner.

Private: every hand, every card in the pile, and the identity of the cards
behind an unresolved claim. The projection in [view.go](../internal/game/bluff/view.go)
is built from counts and the claim; it has no field that could carry a
hidden card, so there is nowhere for one to leak through a reconnect
snapshot, an event, or an error message.

Whether an unchallenged claim was true is never revealed — not when the
window closes, not when the pile is later collected, not when the game ends
on it.

## Chat and voice

Room chat and voice stay open during play, because talking is how bluffing
works. Neither pauses the window, and neither counts as a challenge: only
the explicit action does. Bluff therefore implements no `ChatPolicy` and no
`VoicePolicy`, which is what leaves both open.

## Disconnects, reconnects and rematches

- A reconnect restores the same hand, turn, pile and deadline from the
  stored state. Nothing is reshuffled or redealt, and no deadline is
  extended.
- A window resolves on time whether or not everyone is connected: silence
  is acceptance, including the silence of someone whose connection dropped.
- When an offline player's turn arrives the table waits and shows them as
  offline. Their cards are never played for them, shown, or redistributed.
- Leaving abandons the match with no winner, through the existing room
  controls. Seats are not replaced mid-match.
- A rematch is the room's existing ready-up: a new match means a new
  shuffle, a new deal and a newly drawn first player.

## Verification

Engine ([engine_test.go](../internal/game/bluff/engine_test.go)): deal shapes
for 3–6 players with all 52 cards accounted for; the dealing seat and the
lead both move around; truthful, false and mixed plays all accepted;
ownership, duplicates, zero, five, wrong turn and wrong phase all refused;
the call advancing and wrapping King → Ace; whole-pile collection with
latest-batch-only revelation; the turn after every challenge outcome;
accepting binding; stale and repeated answers refused; the window closing
early only when every eligible player has answered; expiry; all three
last-card endings; and a full game to a winner with the deck intact.

Server ([bluff_integration_test.go](../internal/server/bluff_integration_test.go)):
the deal over the real socket, no projection carrying another hand, a claim
carrying no cards, three simultaneous challenges yielding exactly one
acknowledgement and one penalty, a late challenge refused, a window closing
itself through the real sweep, a reconnect restoring the same hand, and an
outsider refused.

Browser ([bluff.spec.ts](../scripts/e2e/tests/bluff.spec.ts)): four
sessions through the real screens, including the leak check — every card id
a client received is one it held or saw a challenge reveal.

## Known limitations

- **No play timer.** A player who never plays stalls the table; only the
  abandon vote resolves it. Deliberate, per the brief.
- **Countdown drift.** The countdown is drawn from the server's deadline
  against the client's own clock, so a badly skewed client sees a slightly
  wrong number. It is display only: lateness is decided server-side.
- **The paced sweep is per-second and unsharded.** It reads up to fifty
  paced matches per tick. That is ample here and would need an index or a
  queue at a scale CardPlay is nowhere near.
- **A table that challenges every single play cannot end**, because the
  pile returns to a hand every time. That is a property of the rules, not a
  bug, and the engine test says so out loud.
