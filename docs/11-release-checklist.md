# Release checklist: CardPlay Monopoly Deal

Work through this list for every release. **Status** records the 2026-09-30 release-readiness pass. Items marked ☐ must be done by the person deploying, because they depend on the production environment. Operational detail is in [10-operations.md](10-operations.md).

## 1. Code and tests (all must pass)

| Check | Command | Status 2026-09-30 |
|---|---|---|
| Go formatting | `gofmt -l cmd internal db scripts` (empty) | ✅ |
| Go static checks | `go vet ./cmd/... ./internal/... ./db/... ./scripts/...` | ✅ |
| Go unit + DB integration tests | `TEST_DATABASE_URL=… go test ./cmd/... ./internal/... ./db/...` | ✅ 6 packages with tests, all passing. The server suite covers accounts, rooms and chat authorization, the live match (15 subtests, including the turn timeout), multi-instance realtime, and operations/validation (5 subtests). The engine suite includes 200 random games checking that every turn-timeout default move is legal. |
| Race detector | `go test -race …` (CI, Linux) | ⚠️ Not run locally: the Windows gcc here cannot build 64-bit cgo. It runs in CI. |
| Migration up → down → up, schema equality, checksum tamper | `go test ./db` (`TestMigrationsUpDownUp`) | ✅ |
| sqlc generated code drift | `sqlc diff` (v1.31.1) | ✅ clean |
| Web formatting, types, lint | `npm run format:check && npm run typecheck && npm run lint` | ✅ |
| Web production build | `npm run build` | ✅ (built from an identical copy of `web/`) |
| Browser end-to-end (Playwright, Edge) | `cd scripts/e2e && npx playwright test` | ✅ 31/31 on 2026-10-01 after the new flow (guests, games list, temporary rooms, reference-style sheets, auto end turn, only-relevant choices). Includes the action-card spec (Birthday, Debt Collector with Just Say No, Sly Deal, Forced Deal, Deal Breaker, Rent paid in properties, placement, Reorganize) and a real server-side turn timeout |
| Load test | `go run ./scripts/loadtest …` | ✅ 400 sockets / 197 commands/s with 0 errors; see capacity in the ops doc |
| Backup/restore drill | `scripts/ops/backup.sh`, `restore.sh` | ✅ Backup under live load, restore, exact comparison, refusals, API ready on the restored database |

## 2. Security review

| Area | Result |
|---|---|
| Authentication | Argon2id (64 MiB, t=2); constant-work login for unknown emails; generic errors; email verification required for accounts; name-only guests (7-day sliding sessions, no friends, deleted on sign-out or after 7 days unused) for play; per-email/token and per-known-device rate limits; hashing concurrency capped (new). |
| Sessions | Random 256-bit tokens, only hashes stored; HttpOnly, SameSite=Lax, Secure on HTTPS; 7-day TTL; list and revoke sessions; password change and reset revoke every session; WebSockets recheck the session on every message and every 20 s. |
| CSRF | Every non-GET API request must carry the exact `APP_ORIGIN` `Origin`; SameSite=Lax cookies; JSON-only bodies with unknown fields rejected. |
| WebSocket | Exact Origin match, cookie auth + verified account or guest, 16 KB frames, 30 messages / 10 s, 5 sockets per user, per-seat controller generation (a second tab takes over), close codes 4001/4004/4009. |
| Authorization | Room, chat, invitation and match endpoints check membership or participation in the same transaction; non-members get 404, not 403, so nothing is revealed; host-only controls. Covered by integration tests. |
| Input validation | Size-limited JSON bodies. Display names (60), room names (80), chat (500) and report reasons (500) must be valid UTF-8 with no control characters or bidi overrides (**new**; this was a spoofing gap). Handles `[a-z0-9_]{3,24}`. |
| Chat abuse | 5 messages per account-wide window; idempotent client IDs; report (one per reporter per message); per-user mute; blocks; plain-text rendering; chat is deleted with its room. |
| Hidden game data | Each player receives only their own projection. Rule-rejection messages don't reveal hidden cards. Engine invariant failures now return a generic error, where the text could previously include card IDs (**fixed**). The Playwright suite checks every match state a player received against the stored snapshot of that revision, and fails if it contains a card that was hidden in another player's hand (exact, no false alarms from reshuffled cards). |
| Secrets | None tracked in git or history. Production config refuses HTTP origins, non-`verify-full` DB TLS and missing SMTP auth. `seed` and `migrate-down` are disabled in production. Logs never contain tokens, cookies or raw URLs. |
| Headers | API: `CSP default-src 'none'`, `frame-ancestors 'none'`, `X-Frame-Options DENY`, `nosniff`, `no-referrer`, `no-store`. Web: CSP (self only; `unsafe-inline` scripts required by Next.js hydration), DENY framing, `nosniff`, `no-referrer`, Permissions-Policy, COOP; HSTS opt-in with `ENABLE_HSTS=1`; no `X-Powered-By` (**new**). |
| Dependencies | `govulncheck`: 0 reachable. GO-2026-5932 (x/crypto openpgp) is in a required module but not called and has no fix yet. `npm audit`: 0 in web (prod + dev) and 0 in the e2e runner. The old puppeteer-based e2e script pulled a vulnerable `extract-zip` and has been removed. |

## 3. Production environment (☐ deployer)

- ☐ HTTPS origin; `APP_ORIGIN` is the exact public origin; the web image is built with `ENABLE_HSTS=1`.
- ☐ `APP_ENV=production`; `DATABASE_URL` with `sslmode=verify-full`; SMTP credentials; secrets from the secret store.
- ☐ `LOG_FORMAT=json` shipped to the log store; alerts from the ops doc configured.
- ☐ `METRICS_ADDR` bound to a private interface and scraped.
- ☐ `DB_MAX_CONNS × instances + instances` below PostgreSQL `max_connections`.
- ☐ Liveness `/healthz`, readiness `/readyz` wired into the orchestrator and load balancer.
- ☐ Load balancer / proxy allows WebSocket upgrades on `/ws` with an idle timeout above 60 s.
- ☐ Linux `net.core.somaxconn` at 4096 or more on API hosts.
- ☐ Nightly `backup.sh` scheduled, copied off-site, and alerting on a missing backup; or provider PITR enabled. One restore drill done on the production-like environment.
- ☐ `cardplay migrate` run before rolling out the API.
- ☐ Smoke test after deploy: register → verify → sign in → create a room → invite → start a 2-player match → play a turn → chat.

## 4. Known defects and features that are not ready

These do not block a small, invited launch, but they must be understood before calling it production-ready.

| Item | Impact | Status |
|---|---|---|
| Cambio | Listed as "coming later"; not registered as a game. | Not ready (by design) |
| Race detector not run on this machine | Data races in the Go server would go unnoticed until CI. | Runs in CI on Linux; **must be green before release** |
| CI changes not yet executed | The new e2e job, `-race` and govulncheck steps were written but not run on GitHub Actions from here. | ☐ Push and confirm green |
| Auth rate limits are per proxy | Behind Next.js, all visitors share one source address; auth has a site-wide ceiling of 600 requests/min. | Acceptable for a small launch; revisit with a trusted client-IP header before growth |
| Login throughput is CPU-bound | About 30 logins/s per 8-CPU instance (Argon2id). A burst of 200 simultaneous logins waited up to 8 s. | Scale API instances for launch events |
| State push cost | Was 7 round trips per viewer per update; now one statement. About 2.7× faster with a 1 ms database round trip (benchmark in the ops doc). | **Fixed** 2026-09-30 |
| Web CSP allows inline scripts | Required by Next.js without nonces. XSS risk is reduced by React escaping and plain-text chat. | Consider nonce-based CSP later |
| Metrics endpoint is unauthenticated | Must be bound privately (see §3). | By design |
| No admin/moderator UI | Chat reports are stored for review but need SQL access to read. | Not ready: define a moderation process before public (non-invite) launch |
| Action cards in browser tests | `actions.spec.ts` plays Birthday, Debt Collector, Just Say No, Sly Deal, Forced Deal, Deal Breaker, Rent, payment, placement and Reorganize through the real screens from a staged deal. Pass Go, House/Hotel and Double the Rent are covered by engine tests only. | **Fixed** 2026-09-30 |
| Idle connected player could stall a match | The server now makes the default move after 2 minutes (owner decision; `MATCH_TURN_TIMEOUT`). | **Fixed** 2026-09-30 |
| Moves attempted while reconnecting failed with an error | Game controls are now locked while the table is disconnected, with a notice. | **Fixed** 2026-09-30 |
| Multicolor wild defaulted to "New Brown set" | The default is now the own set closest to completion, otherwise Unassigned. | **Fixed** 2026-09-30 |
| Chat drawer hid the Chat button | On wide screens the drawer sits below the header; on phones it is a full-screen sheet with a labeled "Back to table" button. | **Fixed** 2026-09-30 |
| The dropped-connection e2e test closes sockets rather than cutting the network | Playwright offline mode does not block WebSockets. | Test limitation |

**Verdict for this pass:** every local check listed in section 1 passed, and the security review found and fixed four issues with none left open. The service is **not yet declared production-ready**. That needs CI green (including `-race` and the new e2e job) and the section 3 environment items completed and verified on the real deployment.
