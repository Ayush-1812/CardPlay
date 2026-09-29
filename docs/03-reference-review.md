# Reference UI and code review

Reviewed local `Reference/coopoly-deal`, commit `a5b6dc50741f4e656b0025819c3131080b22e843`, clean working tree on 29 September 2026. Version in root manifest: 1.6.3. This is a static source and asset inspection, not a browser usability test or executed exploit. The reference was not modified, built or deployed.

## UI ideas worth independently implementing

Paths below are relative to the reference root.

| Pattern and evidence | Value for CardPlay | Adaptation / limitation |
|---|---|---|
| Separate `GameTableDesktop.tsx` / `GameTableCompact.tsx`, shared `layout/useGameTableState.ts`, `hooks/useLayout.ts` | Desktop overview and mobile opponent carousel without shrinking an entire table | Retain all-player obligation/status strip on mobile; don't hide who still owes payment. Test short landscape screens as well as the 1024px width threshold. |
| `cards/GameCard.tsx`, `FannedCards.tsx`, `PropertySetDisplay.tsx` | Data-driven card faces, set counts, rent ladders and expanded inspection | Create original styling and art. Small faces use 8–9px text: provide readable inspection, text labels and keyboard access. Keep distinct same-color sets identifiable. |
| `game/ActionPrompt.tsx:89` | Player-selected bank/property payment with amount totals and insufficient-funds handling | Display overpayment and which sets break; server must enforce what UI filters. Clear selections per pending-action ID; no automatic strategy. |
| `CardActionDialog.tsx`, wildcard dialogs | Explicit target selection and deliberate color assignment | Use stable set IDs, not color as set identity. Show complete-set protection and why choices are unavailable. |
| `common/BottomSheet.tsx` | Shared mobile sheet / desktop modal for decisions | Add accessible name, focus trap, focus restoration and background inertness. Existing shell has dialog roles but does not implement those focus behaviors. |
| `hooks/usePointerCardDrag.ts:16`, `utils/drop-zone.ts` | One pointer interaction for mouse/touch, drag threshold and visible drop targets | Treat drag as optional; tap and keyboard paths must reach every action. Retain selection previews until server confirms. |
| `lobby/CopyRoomLinkButton.tsx`, `RoomQrCode.tsx`, `utils/room-link.ts` | Room link / QR entry and truthful clipboard success feedback | Adopt interaction idea; add authenticated invitation expiry, revocation and full-room errors. QR is optional for release. |
| `hooks/useWebSocket.ts`, server `toClientState` | Rejoin flow and per-player hidden-hand filtering | Rebuild authenticated reconnect and whitelist serialization; public player IDs must never become secrets. |
| `hooks/useSoundManager.ts`, `useBackgroundMusic.ts`, `useHaptics.ts` | Optional feedback and separate music/effects | Original short effects; music off unless deliberately enabled. Do not copy sound patches or music without rights. |
| `i18n/`, `theme/colors.ts`, CSS variables | Central text and theme data | Start English; keep extensibility. Do not copy translations or political theme/branding. |

## Findings and concrete failure scenarios

Severity here is CardPlay reuse risk. “Static finding” means the source establishes the relevant path; runtime impact has not been demonstrated by a running test.

| ID / severity | Evidence | Finding / scenario | CardPlay requirement |
|---|---|---|---|
| R01 Critical | `server/src/rooms/room-manager.ts:321`, `:334`; `models/types.ts:305`; `ws/websocket-handler.ts:374` | Reclaim accepts `playerId`; IDs are sent to all clients. Fallback reclaims disconnected seats by display name. An observer can submit a public ID or disconnected name to obtain that seat's view/control. Static authentication flaw; UUID randomness does not help once published. | Account-bound seat capability, authenticated session, controller generation and no name/ID-only reclaim. |
| R02 High | `engine/game-engine.ts:1286` through payment transfer; `ws/websocket-handler.ts:549` | Payment prevalidation sums an ID on every occurrence without uniqueness check. Repeating a single 3M bank card twice can pass a 5M debt check and attempts two transfers of the same object. | Unique IDs, prevalidated immutable transfer plan, transactional application, conservation tests. |
| R03 High | `engine/game-engine.ts:1154`; `ws/websocket-handler.ts:562` | JSN verifies pending action and possession, but not eligibility/alternating responder before consuming card. An uninvolved player can submit JSN via socket even though UI hides it. | Validate action ID, participant, response stage and revision before consuming a reaction. |
| R04 High | `engine/game-engine.ts:624` | Discard removes cards sequentially with no prevalidation of entire batch, phase or exact excess count. A valid ID followed by invalid ID can mutate then throw; arbitrary discarding can shed cards below seven. Also sends excess to center, contrary to B1. | Atomic end-turn-only excess transaction to draw bottom. |
| R05 High | `engine/game-engine.ts:990`, `:1018` | Birthday/dual Rent select only connected opponents. Disconnecting can avoid a debt. | Freeze all seated targets, pause required responses; network state does not change rules. |
| R06 High, edition mismatch | `models/types.ts:56`; `engine/game-engine.ts:2042`, `:1406` | Duplicate-color wins default true; win check lacks active-turn restriction and Forced Deal checks defender. Wrong for B1 distinct colors and own-turn claim. | Distinct-color count and active-turn victory gate. |
| R07 Medium/high | `engine/game-engine.ts:2004`; `client/src/utils/rent-calculator.ts` | Rent combines card counts across every set of a color, then caps ladder and aggregates building flags. Under proposed one-set rent, two separate partial same-color sets can be charged as a larger set. | Pending Q3; then selected-set rent computed by server. |
| R08 Medium/high | `engine/game-engine.ts:1939`, `:1953`, `:1911` | Removing property moves orphan buildings into bank; rebuild paths can reattach Hotel without House. Conflicts with supplementary/site orphan rule and can deactivate/reassign value unexpectedly. | Explicit detached-building state and tested attachment invariants. |
| R09 Medium | `engine/game-engine.ts:789`, `:960`; `models/types.ts:146` | Sets lack IDs; Deal Breaker chooses first full set matching color. Cannot precisely target one of two same-color full sets with different buildings. | Stable set IDs for rent, buildings, rearrangement and theft. |
| R10 Medium | `client/src/hooks/useWebSocket.ts:40` onward | `onclose` always schedules reconnect; disconnect/unmount does not cancel retry or mark intentional close. CONNECTING sockets do not prevent another connect. | Lifecycle cancellation, single connection owner, backoff/jitter and authenticated rejoin. |
| R11 High reliability limitation | README Graceful Updates; `rooms/room-store.ts` | In-memory rooms, shutdown snapshots by default; unexpected crash can lose acknowledged progress. Snapshot writes do use temporary file/rename; they are not a transactional event log. | Durable accepted transitions and tested crash recovery. |
| R12 Medium documentation drift | `docs/CARDS.md`, `docs/GAME_RULES.md`, engine | CARDS says wildcards can be banked; GAME_RULES describes orphan buildings staying on table while code banks them; mistaken Deal Breaker spending differs from code rejection. PENDING_REFACTORS lists wildcard assignment as pending although code already queues it. | Source-traced rules and tests, not copied reference documentation. |

The shared serializer hides other hands, and the engine has useful prevalidation patterns, tests for rejoin/drop/wildcards and a separate deck factory. Those are useful architectural ideas. Broad spreading of state in `toClientState` is fragile when new private fields are added; prefer explicit projections.

## Product and architecture gaps

The inspected message/room model centers on anonymous names and rooms. It does not supply the requested durable accounts, friendship lifecycle, authenticated invitations or a room-chat subsystem. These need new services. The single-process room owner and JSON snapshot design need a deliberate production persistence strategy. Client/server duplicate rule tables can drift; CardPlay should share verified public card metadata while keeping authoritative state private. Settings permit alternate rules inappropriate for the locked edition baseline.

No defect was “fixed” in this phase and no runtime/test pass is claimed. Findings become acceptance scenarios in the roadmap. Direct code/art copying is blocked by the license inventory; recreate the useful patterns independently.
