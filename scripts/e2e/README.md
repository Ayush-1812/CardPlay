# Live-match browser test

`match-e2e.mjs` drives three real browser sessions (Alice, Bob and Carol) through a Monopoly Deal match in the web client:
- Starting the match and six live turns.
- Chat and the unread badge.
- Refresh, a second tab taking and returning a seat, and network loss and recovery.
- A server kill and restart mid-match.
- Report, mute and leave.

It checks every WebSocket frame for hidden-card leaks.

The script starts and restarts the API itself, so use a throwaway database and ports that nothing else uses. It uses an installed Chromium-based browser (Edge by default).

1. Create an isolated database and migrate it, then seed the three accounts (`cardplay migrate`, `cardplay seed` with `SEED_PASSWORD` set).
2. Build the API binary: `go build -o /path/to/cardplay ./cmd/cardplay`.
3. Build and start the web client with its API pointed at the test API:
   ```sh
   cd web
   API_INTERNAL_URL=http://127.0.0.1:18080 npm run build
   npx next start -p 3100 -H 127.0.0.1
   ```
4. Run the test:
   ```sh
   cd scripts/e2e
   npm install
   DATABASE_URL=postgres://… E2E_PASSWORD=<SEED_PASSWORD> E2E_API_BIN=/path/to/cardplay npm run match
   ```

Optional settings:

| Variable | Default |
|---|---|
| `E2E_BASE` | `http://127.0.0.1:3100` |
| `E2E_API_ADDR` | `127.0.0.1:18080` |
| `E2E_BROWSER` | Edge's default Windows path |

The network-loss step pauses the match until the server notices the drop, so a full run takes a few minutes.
