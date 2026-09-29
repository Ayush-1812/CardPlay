# Monopoly Deal engine traceability

Engine: `internal/game/monopoly` (rules version `us-01723-v1`, state schema 1). It is pure Go with no HTTP, WebSocket, database or UI code. The server is authoritative: clients submit typed intents (`actions.go`), and `Apply` validates them against a clone, so an illegal or out-of-turn action changes nothing. After the seeded shuffle, the same state and action always produce the same result.

**Sources.** B1 = `Monopoly Deal Card Game Instructions.pdf`, PDF page 1 (a single foldout; the section is named). G1–G5 = `Instructions.pdf`, PDF pages 1–5. W = monopolydealrules.com, as reconciled in the [rule specification](02-monopoly-deal-rules.md). D = a digital convention recorded in that specification. Q = a product decision taken 29 September 2026 (see [decisions](#decisions-applied)). The A-numbers refer to the [acceptance matrix](05-roadmap-and-acceptance.md).

| Rule | Source | Code | Tests |
|---|---|---|---|
| 106 playing cards, B1 counts, printed values, set sizes and rent ladders; 4 reference cards excluded (A01) | B1 p1 "6 or More Players" contents, card faces; G5 p5; W photos | `cards.go` | `TestManifest`, `TestRentLadders` |
| Shuffle, deal 5 face down, secret hands, random first seat, clockwise order (A02) | B1 p1 Set Up 1–5; G1 p1; D first seat | `deck.go` `NewGame` | `TestNewGame`, `TestSeededDealIsDeterministic`, `TestViewPrivacy` |
| Cryptographically strong shuffle; deterministic replay from seed and action log | Roadmap architecture; D | `deck.go` `shuffle` (ChaCha8 keyed by SHA-256 of the seed and shuffle number) | `TestSeededDealIsDeterministic`, `TestReplayIsDeterministic` |
| Draw 2, or 5 from an empty hand; no mid-turn refill (A03) | B1 p1 On Your Turn 1, End Your Turn; G1 p1 | `startTurn` | `TestTurnStartDraw` |
| Empty draw pile: shuffle the resolved center pile and continue; returned bottom cards first; resolving cards never recycled; both piles empty draws what exists (A06) | G1 p1 Finish; D | `draw` | `TestDrawDepletion` |
| Up to 3 plays; each bank, property, action, building and doubler costs one; responses are free (A04) | B1 p1 On Your Turn 2; G1 p1; W Just Say No FAQ 1, 5 | `requirePlay` | `TestPlayBudget`, `TestJustSayNoParity` |
| Bank money, action, Rent and building cards; banked actions are money forever; properties never banked (A07) | B1 p1 2A; G2 p2 | `bank` | `TestBanking`, `TestPaymentSelection` |
| Place properties; wild color choice; excess same-color cards start a new set; multicolor wild may be unassigned | B1 p1 2B, Property Wildcards; G2 p2; D unassigned | `place` | `TestPlayProperty` |
| Reorganize only on your own turn, free, atomic, never while an action is pending; buildings move to eligible full sets (A21) | B1 p1 2B, Property Wildcards; G2 p2; D; Q3.5 | `rearrange` | `TestRearrange`, `TestOutOfTurnAndPhase` |
| End turn: return exactly the excess over 7 to the draw-pile bottom in chosen order; others learn only the count (A05) | B1 p1 End Your Turn; D order | `endTurn` | `TestEndTurnExcess` |
| Pass Go draws 2; several per turn; no response; third-play Pass Go still needs hand reduction | B1 p1 Pass Go; G3 p3 | `passGo` | `TestPlayBudget`, `TestDrawDepletion`, `TestJustSayNoResponders` |
| Sly Deal: one property outside a complete set, or a detached building; never bank or hand (A12) | B1 p1 Sly Deal card and detail; G2–G3; W Action FAQ Q3 | `slyDeal`, `stealable` | `TestSlyDeal` |
| Forced Deal: atomic swap; neither card from a complete set; the offer must be a property (A12, A13) | B1 p1 Forced Deal; G3 p3; Q1a; Q1a-F2 | `forcedDeal` | `TestForcedDeal`, `TestOffTurnVictory` |
| Deal Breaker: one chosen complete set with its House and Hotel; duplicate sets individually selectable (A14) | B1 p1 Deal Breaker card and detail; G4 p4 | `dealBreaker` | `TestDealBreaker` |
| Debt Collector 5M from one player; Birthday 2M from each other player, clockwise, one at a time (A15) | B1 p1 Debt Collector, It's My Birthday; G3 p3; D order | `debtCollector`, `birthday`, `advance` | `TestChargesAndOrder` |
| Two-color Rent charges every opponent; multicolor Rent charges one chosen opponent; rent is charged on one chosen set and frozen when declared (A16) | B1 p1 Rent cards; G4 p4; Q3.2 | `rent` | `TestRentDeclaration` |
| Rent = printed ladder + House 3M + Hotel 4M (7M with both); a set of only multicolor wilds is never complete and earns nothing (A16) | B1 p1 House/Hotel faces; G2 p2, G4 p4; Q3.1 | `PropertySet.Rent`, `Complete` | `TestRentConfigurations`, `TestBuildings`, `TestVictory` |
| Double the Rent only with two-color Rent; one play each; ×2 or ×4 (A18) | B1 p1 Double the Rent; G1 p1, G4 p4; Q2 | `rent` | `TestRentDeclaration`, `TestPlayBudget` |
| House on a complete non-railroad, non-utility set; Hotel only after a House; one of each (A17) | B1 p1 House/Hotel | `canAttach` | `TestBuildings` |
| Just Say No: from hand in response only; free; each counter flips the result; all three can chain; banked JSN unusable (A19) | B1 p1 Just Say No; G3 p3; W Just Say No FAQ | `justSayNo`, `closeChain` | `TestJustSayNoParity`, `TestThreeCardChain`, `TestJustSayNoResponders` |
| JSN protects only its defender on group charges; chains alternate only between source and that defender; stale or duplicate responses rejected (A20) | G3 p3 (variation noted); Q1b; D revision | `justSayNo`, `currentTarget` | `TestGroupJustSayNoIsPerDefender`, `TestJustSayNoResponders` |
| Against doubled Rent a JSN targets the whole charge or one doubler; counters restore it; the defender may start further chains on other active parts until accepting (A18, A20) | G3 p3; Q2; Q2-F1 | `Component`, `closeChain`, `proceed` | `TestJustSayNoAgainstDoubledRent` |
| Payment: payer chooses bank or property cards, never hand; no change; overpay allowed; shortfall gives everything; multicolor wild unusable; no creditor choice (A08, A09, A11) | B1 p1 How to Pay, Property Wildcards; G2 p2 | `payDebt` | `TestPaymentSelection` |
| Paying from a set breaks it and detaches its buildings; a paid building stays a detached building; losing the House detaches the Hotel (A10) | G2 p2, G4 p4; W House FAQ 7–8; Q3.3; Q3.4 | `payDebt`, `normalize` | `TestPaymentBreaksSets` |
| Received properties are placed by their recipient, including off-turn, before play resumes | G2 p2; D | `placeReceived`, `finish` | `TestSlyDeal`, `TestForcedDeal`, `TestPaymentBreaksSets` |
| Win with 3 complete sets of different colors on your own turn; duplicates count once; off-turn sets wait and can be broken; checked at turn start before drawing; the active player has priority; no win until the action and placements finish (A22, A23) | B1 p1 How to Win, 2B, The Winner; D timing | `checkVictory`, `startTurn` | `TestVictory`, `TestOffTurnVictory`, `TestNoPrematureWin` |
| Illegal, out-of-turn, stale or malformed actions are rejected with no state change | D invalid input | `Apply`, `DecodeAction` | every `rejected(...)` assertion; `TestOutOfTurnAndPhase`, `TestDecodeEveryAction` |
| 106-card conservation, unique ownership, legal zones, set bounds and play budget after every transition (A24) | Roadmap A24 | `CheckInvariants` | `TestInvariantViolations`, `TestRandomGamesPreserveInvariants` (300 games; every action kind exercised) |
| Player-specific view: own hand; opponents see hand counts and tabled cards only; no draw order, seed or held JSN leaked | B1 p1 Set Up 3; roadmap N01 | `ViewFor`, `Module.View` | `TestViewPrivacy`, `TestLegalActionsHideHoldings`, `TestModuleAdapter` |

## Decisions applied

The product owner decided these on 29 September 2026. Each adopts the rule specification's proposal, and the two follow-ups were raised while implementing.

| ID | Decision |
|---|---|
| Q1a | Forced Deal: neither the offered nor the taken card may come from a complete set. |
| Q1a-F2 | Forced Deal may take a detached building but must offer a property. |
| Q1b | On group charges a Just Say No protects only its defender; only the source and that defender take part in the chain. |
| Q2 | Double the Rent works only with two-color Rent. A defender's Just Say No targets the whole charge or one doubler; counters restore that part. |
| Q2-F1 | The defender may keep starting chains against other still-active parts until they accept; a blocked whole charge ends their obligation. |
| Q3.1 | A set is complete, or earns rent, only with at least one card that is not a multicolor wild. |
| Q3.2 | Rent is charged on one chosen set, even with two sets of that color. |
| Q3.3 | A building given as payment stays a detached building for the receiver. |
| Q3.4 | A set that becomes incomplete detaches its buildings; losing the House also detaches the Hotel. |
| Q3.5 | On their own turn, a player may move attached or detached buildings free to another eligible complete set. |

## Not in the engine

- Six or more players (B1 p1: needs two packs). The release supports 2–5 players with one deck.
- G4 p4's optional penalty for a mistaken Deal Breaker. Per the specification (D), invalid targets are rejected atomically instead.
- Pauses, disconnects, abandonment, command IDs, revisions and persistence. These are platform concerns (roadmap M3). `Module` exposes the engine through `game.Game` but stays `playable: false` until matches are wired.
