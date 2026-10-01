# Browser end-to-end tests (Playwright)

The suite drives real browser sessions through the production web build. Each test uses several players at once.

| Spec | Covers |
|---|---|
| `social.spec.ts` | Account registration with the emailed token, then sign-in. Server-side validation; an unverified account is refused rooms. Friends (request, accept, unknown handle) in the Friends sheet. A room from the Monopoly Deal lobby, renamed and resized in Room settings. A friend's invitation joined from the games screen; the room's ready-made invite link joined directly. Live lobby chat (plain text, direction overrides refused), quick replies, report and mute. |
| `match.spec.ts` | Invite, ready and start. Chat with the unread count; the Chat button closes the drawer. Refresh keeps the hand. A dropped connection pauses and resumes. A second tab (via the games screen's Rejoin) takes the seat and gives it back. An API kill and restart mid-match, with controls locked while reconnecting. A **complete two-player match played through the UI to a winner**: money and one-color properties go down with a tap, a card with one sensible home goes down with a tap, wild cards with a choice pick the best tile, turns end by themselves after three plays. Exact hidden-card checks (below). |
| `actions.spec.ts` | Action cards through the sheets, from a staged deal:<br>• A multicolor wild offered only the sets it can join.<br>• It's My Birthday paid from one Action! sheet.<br>• Debt Collector blocked by Just Say No.<br>• Sly Deal with one stealable card named on the button.<br>• Rent on the only matching set in one tap, paid in properties that land by themselves.<br>• Moving a wild on your own table.<br>• Forced Deal as a single button when each side has one card.<br>• Deal Breaker with the only complete set named on the button.<br>• Banking money with one tap.<br>• Turns ending after the third play.<br>• The **server's turn timeout**.<br>The deal is staged in the database at revision 0 (before any move); every move after that goes through the UI. |
| `mobile.spec.ts` | **Guests**: a desktop guest creates a room. A phone guest (Pixel 7, touch) opens the invite link in a new tab, types a name and lands in the room. No sideways scroll. Sheets fit the screen. Chat as a full sheet with quick replies. Landscape. Leaving the match. The room is deleted when the last player leaves. A guest's sign-out ends the guest. |

Every spec also fails on uncaught page errors.

**Hidden-card check.** For every `match.state` a player received, the test loads the stored snapshot of that exact revision from the database. It fails if the frame contains a card that was then in another player's hand and had never been public. There are no false alarms from reshuffled cards.

## Run

The tests start their own Mailpit (ports 1026/8026), the API (127.0.0.1:18080) and the web build (127.0.0.1:3100). Use a **throwaway** database and make sure nothing else uses those ports.

1. Create an empty database and migrate it: `DATABASE_URL=… APP_ORIGIN=http://127.0.0.1:3100 cardplay migrate`.
2. Build the API: `go build -o /path/to/cardplay ./cmd/cardplay`.
3. Build the web client against the test API. Use a copy of `web/` if a dev server is running from it:
   ```sh
   API_INTERNAL_URL=http://127.0.0.1:18080 npm run build
   ```
4. The tests read the database directly (staging and privacy checks), so `DATABASE_URL` must be the same throwaway database the API uses.
5. Install and run. Only the test runner is downloaded; the tests use the installed Microsoft Edge (`E2E_CHANNEL=chrome` uses Chrome instead):
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
- The full-match test ends through property sets; action cards are exercised in `actions.spec.ts`. Pass Go, House/Hotel and Double the Rent are covered only by the Go engine tests.
