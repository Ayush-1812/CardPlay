# Implementation roadmap and acceptance criteria

This is a future implementation plan, not authorization to write the game in this phase. Work stays inside `CardPlayy`. Scope and product decisions are in the PRD; exact rules and open items are in the rule specification. No delivery dates are asserted before staffing and decisions are known.

## Milestones and dependencies

| Milestone | Deliverable | Exit gate |
|---|---|---|
| M0 — Freeze rules and assets | Resolve remaining Q entries after website reconciliation; verify card manifest against photographed faces and booklet counts; record edition/version; choose original visual identity and licensed upstream resources | No unresolved gameplay branch hidden in code; all 106 card instances accounted for; all shipped assets have permission/license evidence |
| M1 — Platform foundation | Repository structure, CI, configuration, database migrations, auth/sessions, accounts, friends, invitations and room membership | P01–P04 scenarios pass; unauthorized membership/seat takeover impossible in tested flows |
| M2 — Rule engine | Pure authoritative transitions, manifest, shuffle abstraction, setup, zones, plays, rent, stealing, payments, JSN, buildings and victory | Rule scenario matrix below passes, including approved Q variants; deterministic replay and conservation properties |
| M3 — Durable multiplayer | Per-room serialization, runtime command schemas, command IDs/revisions, durable state/events, WebSocket per-seat projections, reconnect and abandonment | Crash, duplicate, reordered and concurrent command tests pass without hand leakage or lost acknowledged state |
| M4 — Play UI and chat | Original card UI, responsive table, accessible action/payment/response workflows, room chat, results and rematch | 2-, 3- and 5-player end-to-end games on target devices; keyboard-only full match; all decision prompts recover after refresh |
| M5 — Release hardening | Cross-browser validation, load/fault tests, observability, backups/restore, runbooks, dependency notices and deployment rollback | All release criteria pass; no critical/high unresolved correctness or access-control defects; license gate complete |

M1 can proceed independently of the open game rules once implementation is requested. M2 depends on M0 for disputed behavior. M3 consumes M2 transitions; M4 can prototype interfaces against agreed contracts but must not duplicate authoritative logic. Public release requires M5.

## Proposed architecture decisions

Use a modular server and web client, not a microservice fleet for v1. Persist accounts, friendship edges, invites, room membership and durable match records in a transactional database. Serialize transitions per match and commit state revision, command result and public/private event data atomically before acknowledgement. Use an outbox or equivalent delivery mechanism so commit and notification cannot lose each other. A reconnect snapshot is authoritative; clients reconcile by revision instead of guessing missed moves.

Game modules expose setup, validation/transition, legal choices, private/public projection and outcome detection. Store a stable rules version with each match; do not upgrade running matches to new rules. Shared metadata contains no hidden deck or hand state. Use server-side secure randomness for shuffling; tests inject deterministic randomness. Stable card IDs, set IDs and pending-action IDs are mandatory. Accounts and chat do not depend on Monopoly set colors. Avoid direct reference-code imports.

## Rule acceptance matrix

Each scenario specifies observable behavior; these are future tests, not tests already run.

| ID | Scenario / expected result | Source or decision |
|---|---|---|
| A01 | Manifest has 106 unique playable IDs, 28 ordinary properties, 11 property wilds, 34 actions, 13 rents and 20 money; rule aids never shuffled | B1 contents |
| A02 | For 2 and 5 players, each starts with five private cards; first seat is chosen once; turn order remains fixed clockwise | B1 Set Up; D |
| A03 | Nonempty hand draws two; empty-at-start draws five; emptying hand mid-turn gives no extra refill | B1 turn 1/3 |
| A04 | Zero to three hand-card plays allowed; fourth rejected atomically; Pass Go draws do not restore plays | B1 turn 2 |
| A05 | Eight-card end hand returns exactly one selected card to draw bottom; invalid/excess/duplicate selection changes nothing; other players learn count only | B1 turn 3; D |
| A06 | Partial draw uses existing deck then reshuffled resolved center pile; cards parked on draw bottom are drawn before recycling; empty piles never create cards | G1; D |
| A07 | Banked actions remain money after transfer and cannot activate; property/wild banking and hand payments rejected | B1 bank/payment |
| A08 | Debt 4M: payer can select 2M+3M and overpay, or 3M+1M exactly; creditor cannot choose; no change | G2 |
| A09 | Debt 5M with only 3M eligible value transfers all 3M; with only rainbow wild transfers nothing; no remaining debt | B1/G2 |
| A10 | Payment may break a full set; ordinary property goes to recipient property area, bank card to bank; approved building-state rule applied | B1/G2; Q3 |
| A11 | Repeated card ID, another player's card, hand card, zero-value wild or underpayment with eligible assets remaining is rejected without any transfer | B1; D; R02 regression |
| A12 | Sly/Forced cannot target property in a full set; eligible incomplete wild can be taken; complete-set offer and detached-building cases follow finalized Q1/Q3 | B1; website; Q |
| A13 | Forced swap is atomic even if either placement needs input; response cancellation leaves both original properties untouched | B1; D |
| A14 | Deal Breaker takes selected full set and its House/Hotel; two full same-color sets remain individually selectable | B1 pictured card; R09 regression |
| A15 | Dual Rent and Birthday include disconnected seated opponents; absence pauses, never cancels debt; wild Rent and Debt Collector select one opponent | B1; D; R05 regression |
| A16 | Every property group's rent ladder matches verified faces; ordinary, wildcard and building configurations yield expected values; only declared set/color charged | B1 face rule; website photos; Q3 |
| A17 | House on incomplete/rail/utility rejected; second House/Hotel rejected; Hotel without House rejected; valid House+Hotel adds 7M | B1 |
| A18 | Rent plus one/two doublers costs 2/3 plays and yields 2×/4×; no standalone doubling, no delayed doubling after payment; eligibility follows Q2 | B1/G4; Q2 |
| A19 | JSN on theft/debt/rent/Birthday opens counter; one/two/three JSNs block/restore/block; free response cost; banked JSN unusable | B1; website JSN FAQ |
| A20 | Uninvolved/repeated/stale responder cannot consume JSN or affect another defender; component and group-block scopes follow finalized questions | D; Q1/Q2; R03 regression |
| A21 | Rearranging wilds is free on own turn outside pending actions; off-turn only incoming-card placement allowed; no card appears in two sets | B1/G2; D |
| A22 | Three different complete colors win on own turn; duplicate colors do not; off-turn completion waits and can be lost before next turn | B1 |
| A23 | Third-play action completes all response/placement obligations before victory/end turn; no premature winner during reversible counter chain | D |
| A24 | Every accepted transition preserves 106-card conservation, unique ownership, legal zones, nonnegative play budget and set-size bounds | D |

For each Q decision, add explicit positive and negative fixtures before M2 exits: own-full-set swap, active-player counter at zero remaining plays, doubled wild Rent, blocking one/two multipliers, group JSN scope, all-rainbow completion, detached-building theft/payment/reattachment, Hotel losing House and duplicate-set rent. Do not mark A12/A16/A18/A20 complete while their rules are unresolved.

## Product, network and release acceptance

| ID | Acceptance criterion |
|---|---|
| P-A01 | Two verified accounts can request/accept/remove friendship; blocking prevents new invitations; crossed/duplicate requests create one relation. Auth/reset/verification and session revocation tested; no account email appears in room payloads. |
| P-A02 | Friend/link invitations survive sign-in, expire/revoke correctly and handle racing acceptance of last seat. Host cannot start with fewer than two, more than five or unready players; non-host cannot start/kick. |
| P-A03 | Chat is member-only plain text; length/rate limits enforced on server; mute/block/report and unread behavior work; chat cannot inject markup or leak hidden cards. |
| P-A04 | Each of 2, 3 and 5 real clients can finish a game and rematch; play through banking, rent, theft, payments, three-card counters and wildcard placement. |
| N01 | Public payload capture contains no other hand, draw order, hidden seed, secret seat credential or private payment preview. Public IDs/names cannot reclaim a seat. |
| N02 | Retry an accepted command after losing acknowledgement: same result, no second play/payment. Race two commands at one revision: one serial result, loser resyncs without partial mutation. |
| N03 | Refresh/reconnect during draw, play, JSN, payment, received-wild placement and excess selection restores exact committed state and remaining obligation. Second controller invalidates first. |
| N04 | Kill server immediately after acknowledgement; restart recovers that transition, pending action and room membership. Crash before commit yields no acknowledged result and safe retry. Test durable restore from backup. |
| N05 | Disconnect never changes group-charge targets, deals a fresh hand or skips a debt. Five-minute grace / unanimous abandonment / all-offline expiry have no false winner. |
| U01 | At 360×800, 390×844, 768×1024 and 1440×900 CSS px, plus phone landscape and 200% zoom, every mandatory decision remains reachable; own hand and all-player pending status remain discoverable. |
| U02 | Keyboard-only completion of every action; dialog focus trapped/restored, labels announced, no color-only meaning; readable card inspection; reduced motion and mute respected. |
| U03 | Current stable Chrome, Firefox, Edge and Safari desktop plus iOS Safari/Android Chrome pass main flows; record actual tested versions at release. |
| O01 | At 100 simultaneous five-player rooms, p95 durable action acknowledgement <500ms in test region and reconnect <5s after network restoration; run a 60-minute soak without state divergence. Record hardware/network and error rate. |
| O02 | Health/readiness, alerts for failed commits/room stalls, privacy-safe logs, migration/backups, restore drill and rollback are documented and exercised. No secrets or hands in ordinary logs. |
| L01 | Every shipped asset/dependency has recorded license/permission and required notices; no reference MP3, logo, source copy or evidence directory accidentally bundled. Monopoly branding/publication clearance recorded. |

Release evidence consists of automated scenario results, browser/device checklist, network privacy capture, load report, restart/restore report and completed asset register. This planning phase performs only source review and document consistency checks; it claims none of these future acceptance tests have passed.
