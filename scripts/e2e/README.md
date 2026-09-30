# Browser end-to-end tests (Playwright)

The suite drives real browser sessions through the production web build. Each test uses several players at once.

| Spec | Covers |
|---|---|
| `social.spec.ts` | Registration with the emailed verification token. Server-side validation, and an unverified account being refused. Friend request and acceptance. Room creation, a friend invitation and an invite link. Live lobby chat (plain text only, direction overrides refused). Report and mute. |
| `match.spec.ts` | Invite, ready and start. Chat during a match with the unread count. Refresh restoring the same hand. A dropped connection pausing and resuming the match. A second tab taking the seat and giving it back. An API kill and restart mid-match. A **complete two-player match played through the UI until someone wins**. Hidden-card leak checks on every WebSocket frame. |
| `mobile.spec.ts` | A phone (Pixel 7 emulation, touch) against a desktop player. No sideways scroll in the lobby or at the table. Cards opened by tap. The chat drawer. Landscape. Leaving a match. |

Every spec also fails on uncaught page errors.

## Run

The tests start their own Mailpit (ports 1026/8026), the API (127.0.0.1:18080) and the web build (127.0.0.1:3100). Use a **throwaway** database and make sure nothing else uses those ports.

1. Create an empty database and migrate it: `DATABASE_URL=… APP_ORIGIN=http://127.0.0.1:3100 cardplay migrate`.
2. Build the API: `go build -o /path/to/cardplay ./cmd/cardplay`.
3. Build the web client against the test API. Use a copy of `web/` if a dev server is running from it:
   ```sh
   API_INTERNAL_URL=http://127.0.0.1:18080 npm run build
   ```
4. Install and run. Only the test runner is downloaded; the tests use the installed Microsoft Edge (`E2E_CHANNEL=chrome` uses Chrome instead):
   ```sh
   cd scripts/e2e
   PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm ci
   E2E_API_BIN=/path/to/cardplay E2E_MAILPIT_BIN=/path/to/mailpit E2E_WEB_DIR=/path/to/web \
   DATABASE_URL=postgres://…/cardplay_e2e npx playwright test
   ```

Optional settings: `E2E_WEB_PORT` (3100), `E2E_API_ADDR` (127.0.0.1:18080), `E2E_SMTP_ADDR` (127.0.0.1:1026), `E2E_MAIL_HTTP` (127.0.0.1:8026), `E2E_CHANNEL` (msedge).

Failures keep a trace, screenshots and the last 40 socket events of the stuck player. Open the report with `npx playwright show-report`.

Limits worth knowing:
- The "dropped connection" test closes the socket from the page. Playwright's offline mode does not reliably block WebSockets, so this simulates an outage rather than cutting the network.
- The full-match bots never play action cards, so the match ends through property sets. The action, payment and Just Say No rules are covered by the Go engine tests and the load test.
- The hidden-card leak check compares each player's current hand with every frame the others received. A reshuffled discard pile could in principle make it report a false leak.
