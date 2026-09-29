# Monopoly Deal rule specification

Version: `monopoly-deal-us-01723-draft-1`. Status: executable rules contract. Q1–Q3 were decided on 29 September 2026 (see [Decisions](#decisions-29-september-2026)); implemented in `internal/game/monopoly` and traced in [08](08-monopoly-engine-traceability.md).

## Sources, edition and notation

- **B1** = [Monopoly Deal Card Game Instructions.pdf, PDF page 1](../../Monopoly%20Deal%20Card%20Game%20Instructions.pdf). One large foldout page; section names disambiguate citations. Hasbro/Parker Brothers, US property names, 01723 / 01723-I, ©1935, 2008. Metadata creation: 11 March 2009. Ages 8+, 2–5 on the front; a separate paragraph allows two packs for six or more.
- **G1–G5** = [Instructions.pdf, PDF pages 1–5](../../Instructions.pdf). An unattributed detailed guide summarizing a fan site, with source-access date 28 September 2026. It explicitly defers to edition-specific booklet/card text. It is not another official edition.
- **R** = reference repository: observations only, never authority. **D** = labeled digital/product convention. **Q** = unresolved gameplay question. **W** = user-requested monopolydealrules.com pages/photos; **U** = card-face evidence gap (now closed for numeric metadata by W photos).

Authority order: B1 printed card faces and booklet together; user-requested W booklet/card photographs corroborate and extend that edition evidence; W FAQ and G fill compatible gaps. Conflicts with B1 and interpretation gaps remain explicitly recorded. The user added W as an instruction reference during this review; this did not explicitly override B1 when W FAQ contradicts its own booklet photographs. Familiar rules and reference code do not silently fill gaps.

All six PDF pages were read. Text extracts and full-page renders are in [evidence](evidence/); B1 visual reading was necessary because sorted extraction interleaves foldout columns.

## Resolved contradictions

| Topic | Sources disagree | Adopted baseline |
|---|---|---|
| Winning colors | G3 allows repeated colors; reference default does too | Three **different** complete colors. B1, How to Win / The Winner / turn 2B. Duplicate complete sets may exist but count once per color. |
| Off-turn victory | G1 says win as soon as three sets exist | An off-turn qualifying collection must wait until that player's turn. B1, turn 2B explicitly says so. It must still qualify then. |
| Excess hand cards | G1 sends excess to discard | Put extras at the **bottom of draw pile**, leaving seven. B1, End Your Turn. Used action cards still go to the center play pile. |
| Buildings' bonus | G4 reports a disputed +4 versus +7 total | B1 pictured House adds 3M and Hotel adds 4M; both can remain on the same set. Interpret additive +7M with both, consistent with these printed effects. Do not adopt the guide's unverified email report. |
| Player count | Reference advertises 2–6 with one deck | Release uses one deck and 2–5. B1's six-plus mode needs two packs and is out of scope. |
| Inventory | G4–G5 warn about inconsistent fan-site counts | B1's own contents list controls all quantities below. |
| General action discard statement | G3 broadly says actions go to center | House/Hotel used as buildings stay attached on the table, as B1 describes; banked actions stay banked. |
| Double eligibility | B1 prose says standard Rent, pictured card just says rent; G4 is broad | Q2, not silently generalized to multicolor Rent. |

## Deck inventory

One pack contains 110 physical cards: **106 playable + 4 reference cards**. Remove references before shuffling. Counts below are B1, “6 or More Players” contents list, corroborated by G5. `M` means printed million-unit value, not a separate token balance.

| Money denomination | Count |
|---|---:|
| 1M | 6 |
| 2M | 5 |
| 3M | 3 |
| 4M | 3 |
| 5M | 2 |
| 10M | 1 |
| Total | 20 |

Money total is 57M, derived from these counts.

| Ordinary property group | Count / full-set size | Rent ladder, M | Printed value, M |
|---|---:|---|---:|
| Brown | 2 / 2 | 1, 2 | 1 |
| Light blue | 3 / 3 | 1, 2, 3 | 1 |
| Pink | 3 / 3 | 1, 2, 4 | 2 |
| Orange | 3 / 3 | 1, 3, 5 | 2 |
| Red | 3 / 3 | 2, 3, 6 | 3 |
| Yellow | 3 / 3 | 2, 4, 6 | 3 |
| Green | 3 / 3 | 2, 4, 7 | 4 |
| Dark blue (contents calls it Blue) | 2 / 2 | 3, 8 | 4 |
| Railroad | 4 / 4 | 1, 2, 3, 4 | 2 |
| Utility | 2 / 2 | 1, 2 | 2 |
| Total | 28 | | |

**U1 closed with additional evidence:** B1 establishes counts; G2 corroborates set sizes. On the user's instruction, all ten property-group photographs on the [website Cards page](https://monopolydealrules.com/index.php?page=cards) were visually inspected. They verify the complete ladders, set sizes and monetary values above, including values absent from the PDF. These are now sourced to printed card photographs, not the reference implementation. Local contact sheet: [property faces](evidence/site-contact-properties.png). Photographs show the matching US property/card design; they do not prove the user's physical printing.

| Property wildcard pair | Count | Printed value, M |
|---|---:|---:|
| Light blue / brown | 1 | 1 |
| Light blue / railroad | 1 | 4 |
| Pink / orange | 2 | 2 |
| Red / yellow | 2 | 3 |
| Dark blue / green | 1 | 4 |
| Green / railroad | 1 | 4 |
| Railroad / utility | 1 | 2 |
| Multicolor property wild | 2 | 0; cannot pay debt |
| Total | 11 | |

Counts and multicolor restriction: B1 contents / Property Wildcards. Dark-blue/green 4M and pink/orange 2M are pictured in B1. All remaining values were verified against the website wildcard photographs, including the light-blue/railroad 4M card; see [wildcard faces](evidence/site-contact-wildcards.png). Wildcards are properties, not bankable action cards.

| Rent pair/type | Count | Bank value, M | Payers |
|---|---:|---:|---|
| Light blue / brown | 2 | 1 | Every opponent |
| Pink / orange | 2 | 1 | Every opponent |
| Red / yellow | 2 | 1 | Every opponent |
| Dark blue / green | 2 | 1 | Every opponent |
| Railroad / utility | 2 | 1 | Every opponent |
| Multicolor Rent | 3 | 3 | One chosen opponent |
| Total | 13 | | |

Counts: B1 contents. Generic 1M two-color and 3M multicolor card faces and target scopes: B1, Rent cards. All five two-color pairs and multicolor Rent values were additionally checked in [website Rent photographs](evidence/site-contact-rents.png).

| Action | Count | Bank value, M |
|---|---:|---:|
| Deal Breaker | 2 | 5 |
| Forced Deal | 3 | 3 |
| Sly Deal | 3 | 3 |
| Just Say No | 3 | 4 |
| Debt Collector | 3 | 3 |
| It's My Birthday | 3 | 2 |
| Double the Rent | 2 | 1 |
| House | 3 | 3 |
| Hotel | 2 | 4 |
| Pass Go | 10 | 1 |
| Total | 34 | |

Action values are visible on B1's pictured card faces. Category sum: 20 + 28 + 11 + 13 + 34 = 106. Buildings and doublers are included in the 34; do not count them again.

**U2 closed:** Website photographs verify all 28 US property names. Inventory: Mediterranean Avenue, Baltic Avenue; Oriental Avenue, Vermont Avenue, Connecticut Avenue; St. Charles Place, States Avenue, Virginia Avenue; St. James Place, Tennessee Avenue, New York Avenue; Kentucky Avenue, Indiana Avenue, Illinois Avenue; Atlantic Avenue, Ventnor Avenue, Marvin Gardens; Pacific Avenue, North Carolina Avenue, Pennsylvania Avenue; Park Place, Boardwalk; Reading Railroad, Pennsylvania Railroad, B. & O. Railroad, Short Line; Electric Company, Water Works. Use stable IDs independent of display spelling. See the property contact sheet above.

## Setup, zones and invariants

Shuffle the 106 playing cards; deal five face down to each player; keep hands secret; rest form a face-down draw pile; choose first player; move clockwise. B1, Set Up; G1. **D:** choose first seat randomly and show fixed seat order; no age collection.

Zones: private hand; public bank; public property sets with attachments; public center used-action pile; private draw pile; temporary resolving-action area (**D**, prevents unresolved cards being recycled). Every physical card has one immutable ID and exactly one owner/zone. Banked actions permanently function as money and transfer bank-to-bank; no table card returns to hand. Recycling a used action through the draw pile is separately permitted by G1 and is not withdrawal from a player's bank. B1, turn 2A / payment / table diagram.

Properties and property wildcards cannot be banked. Money cannot be played as property. An ordinary property has fixed color; a two-color wildcard has exactly one active allowed color; multicolor wild has one chosen property color or a visibly unassigned placement (**D**, no rent or completion credit until assigned). A card cannot count in two sets. Excess same-color cards form separate sets, G2. All values and set sizes come from the verified manifest, not client input.

## Turn sequence

1. **Start / win check (D timing):** on becoming active, check existing three different full colors before drawing, implementing B1's wait-until-your-turn requirement. No pending previous action may remain.
2. **Draw:** two cards, or five instead if the hand was empty before this draw. Emptying a hand later in a turn does not cause an immediate five-card refill. B1, turn 1 / End Your Turn; G1.
3. **Play:** zero to three cards from hand, in any combination and order. Banking, property placement, each action, each building and each doubler each costs one normal play. Drawn actions can be used this turn. B1, turn 2; G1. Response JSN costs zero plays even on the active turn, explicitly answered by W Just Say No FAQ questions 1 and 5.
4. **Reorganize:** own property collection only during own turn; reposition/flip existing property wilds without consuming a hand-card play (G2; B1 turn 2B / Property Wildcards). **D:** atomic rearrangement commands validate all resulting sets; no rearrangement during a pending response/payment. A player may reorganize after the third play before ending. Building movement remains partly Q3; W House FAQ explicitly supports detached buildings and reattachment to a later complete set.
5. **End:** resolve all pending effects first. If more than seven cards remain, player chooses exactly the excess and places them on the draw-pile bottom (B1, End Your Turn). **D:** player orders those cards, face down; the first listed is the next of that group to be drawn. Publish count only. This cannot be used as voluntary discarding below seven. Advance clockwise.

**Draw depletion:** G1 explicitly supplies the missing B1 rule: shuffle resolved center discards into a new draw pile when draw pile empties, continuing a partial draw. Preserve any existing bottom-of-draw cards before recycling. **D:** no unresolved action/counter enters that shuffle; if both piles are empty, draw only what exists, continue play, never fabricate cards. Do not silently reshuffle table cards or hands.

## Permitted actions and selection

| Action | Preconditions and exact selections | Resolution / response | Source |
|---|---|---|---|
| Bank | Active player chooses one money or action/Rent/building card from hand | Move to own bank, spend one play; no opponent response | B1 turn 2A |
| Place property | Choose property from hand, legal color for wild, existing nonfull set or new set | Move to property area, one play; check active player's victory | B1 turn 2B; G2 |
| Pass Go | One card from hand; play remaining | Draw two, one play; may play another Pass Go if budget remains; no opponent JSN because not directed at them | B1 Pass Go / turn 2C |
| Sly Deal | Actor picks opponent and one ordinary/wild property outside a complete set | Defender response; if unblocked transfer selected property, recipient chooses legal placement; cannot steal from bank/hand; W Action FAQ additionally permits detached building targets | B1 Sly Deal; G2–G3 |
| Forced Deal | Actor picks own offered property and opponent's property; target cannot be in a full set; own full-set restriction Q1 | Defender response, atomic swap; each recipient places received card; no value-matching requirement; W Action FAQ permits detached building targets | B1 pictured card vs prose; G3 |
| Deal Breaker | Actor chooses opponent and specific full set, including attachments | Defender response; transfer whole selected set intact if unblocked | B1 pictured card / Deal Breaker; G3–G4 |
| Debt Collector | Actor selects one opponent | Response then 5M debt; payer chooses cards | B1 Debt Collector |
| Birthday | Every other seated player, regardless of connectivity | Separate response/payment for each, 2M each | B1 Birthday; G3 |
| Two-color Rent | Actor chooses one matching displayed set, Q3 for duplicate sets | Each opponent responds and pays that set's frozen rent | B1 Rent; G4 |
| Multicolor Rent | Actor chooses one displayed set of any property color and one opponent | That opponent responds/pays | B1 multicolor Rent; G4 |
| Double the Rent | Select Rent plus one/two doublers in same declaration; require 2/3 remaining plays; eligibility Q2 | Double once or twice (4×, G4); individual counter behavior Q2 | B1 Double the Rent; G1/G4 |
| House | Choose full non-railroad/non-utility set with no House | Attach, +3M rent; one play; no opponent response | B1 House/Hotel and pictured House |
| Hotel | Choose full eligible set already containing House and no Hotel | Attach, +4M in addition to House; one play | B1 House/Hotel and pictured Hotel |
| Just Say No | From hand, in response to an action against that player, including opposing JSN | Discard and toggle targeted effect; Free response; Q1/Q2 refine scope and counter component | B1 Just Say No; G3 |
| Rearrange | Own turn, no unresolved action; choose specific cards and legal destination(s) | Move existing property cards; not a new hand-card play; building cases Q3 | B1 turn 2B / Wildcards; G2 |
| End turn / excess selection | Own turn, no response, payment or placement pending | Return excess to draw bottom and advance | B1 End Your Turn |

**D invalid input:** incomplete selection is a local preview. On submit, invalid target, insufficient plays, wrong actor, duplicate ID, stale revision or illegal zone causes atomic rejection with no card/play consumed. This intentionally avoids G4's optional mistaken-Deal-Breaker penalty; G4 explicitly asks online implementations to define it. Successful submissions are final. No free trade, gifting, selling, buying from bank, voluntary debt or property-to-money conversion exists.

**Rent calculation:** printed ladder for selected set's current property count, plus valid attached building bonuses, then doublers. Freeze amount when declared; payments cannot retroactively increase later opponents' debts. G4; Q3 for set selection. A multicolor wild alone earns no rent (G2/G4). Color assignment affects rent and completion, not printed payment value.

## Response protocol and card transfers

**D protocol**, constrained by B1's right to use JSN:

1. Validate the entire declaration; reserve the play budget and card(s); publish source, targets, chosen public card/set IDs, rent basis and amount. Do not move opponents' assets yet.
2. Ask a target to accept or play JSN. For group charges process opponents clockwise after source, one at a time, with independent obligations. Never infer “no JSN” from an opponent's hand or expose whether they hold one.
3. Open a fresh counter opportunity after every JSN. Q1 proposes alternating only source and affected target; parity determines whether the chosen effect survives. Accepting a block closes that chain. Q2 specifies component selection when doubling.
4. If an obligation survives, payer submits card selection. A property steal/swap transfers only after its response window closes. Any received wildcard/set-placement choice is completed before ordinary play resumes; placement is allowed off-turn for incoming cards only (G2), not wholesale rearrangement.
5. Resolve each transfer atomically. Commit spent action and JSN cards to center once their effect is finished. Return to play only after all surviving obligations and placements are complete, then check active player's victory.

No response clock, auto-accept or automatic payment in v1 (**D**). A disconnected target remains a target; pause and reconnect per PRD. A paid/accepted obligation cannot be reopened. A duplicate network submission is not a new counter. Response messages must name the pending action and current response revision.

## Payment behavior

Payer chooses cards from their own tabled bank and/or properties, never from hand; creditor cannot choose, demand bank-first, or force an exact subset. No change; received bank cards stay money, properties go to recipient property area. B1, Important! How to Pay Other Players; G2.

- Payment value uses printed monetary values, never rent. A property may be taken out of a complete set as payment (G2); Sly/Forced protection does not protect against debts.
- If eligible value is at least the debt, selection must total at least that amount. Overpayment is allowed even if another exact combination exists (G2).
- If total eligible value is below debt, give all eligible positive-value tabled cards; no residual debt. With no eligible value, pay nothing (G2, B1 for no cards).
- Multicolor property wilds have no monetary value and **cannot be selected for payment**, even alongside paid cards (B1 Property Wildcards). Two-color wilds may pay at their verified printed value.
- House/Hotel may pay (G2). Destination and finer attachment transitions remain Q3; W House FAQ confirms buildings detached by property payment remain on the table for later placement; banked buildings are always bank money and can never be reactivated (B1 turn 2A).
- UI shows amount due, selected amount, overpayment, cards that break a set and remaining assets. Allow editable selection until submit. Do not auto-submit or optimize on the player's behalf.
- Server rejects duplicate IDs, nonexistent cards, wrong owners, hand cards and zero-value wilds before any mutation. Apply removal and receipt together; no partial transfer on error (**D**).

## Victory and edge conditions

Three full sets of **distinct property colors**, on the table, while it is your turn. Rails and utilities are property sets too. B1 How to Win / Property cards / turn 2B. Extra duplicate-color sets do not increase distinct-color count. Wild validity is Q3.

**D:** automatically recognize a win after a fully resolved action or committed rearrangement; do not require a UI declaration race. Off-turn completion is not a reserved win: an opponent may break that collection before its owner's next turn. A Forced Deal that completes both players' third distinct sets gives the active player priority; the defender is still off-turn. Finish the current action's responses and mandatory placements before checking. Stop subsequent normal plays immediately once victory is committed.

Edge coverage includes: five-card refill only at turn start; partial draws; draw and discard both empty; three plays exhausted while a response is pending; receiving property off-turn; duplicate-color sets; invalid/partial payments; exhausted table value; cannot pay rainbow wild; stolen set with buildings; banked JSN cannot respond; banked Pass Go cannot draw; counter chains of all three JSNs; no mid-response rearrangement; no counter to an ordinary bank/property placement; third-play Pass Go still requires hand reduction; full hand does not limit mid-turn draws; no extra plays after drawing; reconnect cannot skip obligations.

## Decisions (29 September 2026)

The product owner adopted every proposal below and decided two follow-ups raised during implementation:

- **Q1a** Forced Deal: neither the offered nor the taken card may come from a complete set. **Q1a-F2** a detached building may be taken but not offered.
- **Q1b** On group charges a Just Say No protects only its defender, and only the source and that defender take part in a chain.
- **Q2** Double the Rent works only with two-color Rent. A defender's Just Say No targets the whole charge or one doubler; counters restore that part. **Q2-F1** the defender may keep starting chains against other still-active parts until they accept.
- **Q3** adopted in full (items 1–5 of the proposal below).

## Material questions (historical record; now decided)

The user supplied the website as an instruction reference rather than selecting the earlier proposed bundles. The findings below supersede those bundles: JSN cost and detached-building theft are now resolved. Other suggestions remain **unapproved**; no game implementation depends on them in this phase.

| Question | Residual gap and proposed answer | Why it changes play |
|---|---|---|
| Q1 - Swap and response scope | B1 Forced Deal illustration does not distinguish offered versus taken property; W Action FAQ does not settle it. Propose neither card from a full set. W JSN FAQ explicitly permits either group-block interpretation, favoring defender-only: propose defender-only. Propose only attacker and affected defender can counter, consistent with B1's example. | Changes legal swaps, intervention and who pays. JSN being free on all turns is already resolved by W. |
| Q2 - Doublers | B1 standard-Rent prose remains narrower than its generic card face; W Rent FAQ does not settle multicolor eligibility. Propose two-color Rent only. Two doublers for 4? is resolved by G4/W Rent. W JSN FAQ specifies removal of doubling but not all choices with two doublers. Propose selecting either base Rent (cancel whole personal charge) or one doubler (remove that factor); JSN counters restore the targeted component. | Changes legal combinations and component debt. |
| Q3 - Set composition and building transitions | Neither source clearly settles completion with only multicolor wilds, rent across duplicate same-color sets, paid-building destination, Hotel losing House, or moving still-attached buildings. Propose at least one ordinary/two-color property per complete or rent-bearing set; one selected set for rent; paid buildings stay buildings; Hotel detaches if House removed; moving attachments is free only on own turn to a legal full set. | Changes completion, rent and property-area assets. |

**Resolved building baseline from W:** detached buildings can be taken by Sly/Forced Deal (unlike the earlier proposed prohibition). Buildings orphaned by property payment remain next to the property area and can be attached when another full set becomes available, or used to pay. Attached buildings travel with Deal Breaker. Preserve the difference between a detached building and a banked building; banked buildings cannot reactivate. [House FAQ, questions 7-8](https://monopolydealrules.com/index.php?page=house); [Action FAQ, question 3](https://monopolydealrules.com/index.php?page=action).

Q3 proposal consequences: breaking a set detaches both attachments; detached cards grant no rent bonus; a received building stays detached if no legal destination exists; reattachment requires a full eligible set, no existing building of the same type, and a House before Hotel. Reattaching a tabled building uses no hand-card play. These finer transitions remain proposed, not claims from the booklet.

## Website reconciliation (29 September 2026)

- [Instruction book](https://monopolydealrules.com/index.php?page=instructionbook): both photographed sides were read. They repeat distinct-color victory, own-turn declaration, bottom-of-draw excess and standard-Rent wording, consistent with B1. The photographed footer carries a different print-line code; use the supplied PDF's 01723-I identifier rather than claiming byte-identical printing.
- [JSN FAQ](https://monopolydealrules.com/index.php?page=justsayno): resolves free active-turn counters; explicitly leaves group protection variable and describes cancellation of doubling with base rent still owed. It does not resolve every stacked-component sequence.
- [Rent FAQ](https://monopolydealrules.com/index.php?page=rent): confirms two doublers consume two of the three plays. Its one-color answer does not distinguish two sets of the same color.
- [Property FAQ](https://monopolydealrules.com/index.php?page=property): duplicate-color victory conflicts with both B1 and the site's booklet pictures. Keep B1. Its statement that a multicolor wild needs another property for rent does not clearly define an all-multicolor full set.
- [Cards page](https://monopolydealrules.com/index.php?page=cards): photographs fill U1/U2. Prose counts are internally inconsistent: listed actions total 36 despite saying 34; it says eight dual property wilds where the listed counts total nine. Use B1 counts, not that prose.
- [Payment FAQ](https://monopolydealrules.com/index.php?page=payment): corroborates payer choice, table-only payment, no change and shortfall forgiveness. It does not explicitly locate a paid, previously attached building in the recipient's zones.

The website identifies its authors as game enthusiasts and acknowledges customary answers where official rules are unknown. Using it as requested resolves factual gaps without treating its openly disputed answers as definitive edition rules. All card metadata is now evidenced; the remaining Q entries are interpretation choices only.
