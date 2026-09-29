# CardPlay

CardPlay is a private multiplayer card-game site. This Phase 2 foundation includes accounts, friends, invitations, rooms, chat, a responsive Next.js client, and a versioned game boundary. **The Monopoly Deal rules engine is not implemented yet**: rooms can be created, but matches return `GAME_NOT_READY`. Cambio is reserved for a later release.

The workspace lives in `CardPlayy`. The original PDFs and reference repository are research inputs only; no reference art is shipped.

## Run with Docker

Requirements: Docker Engine with the `docker compose` command and Node.js 24 (for one-time local secret generation). From this directory:

```powershell
node scripts/setup.mjs
docker compose up --build -d
docker compose --profile tools run --rm seed
docker compose ps
```

Open http://localhost:3000. The API readiness endpoint is http://localhost:8080/readyz and local verification/reset mail is visible at http://localhost:8025. Seed accounts are `alice@cardplay.test` and `bob@cardplay.test`, both verified. Their password is the local `SEED_PASSWORD` in ignored `.env`; never copy it into a commit or issue. `setup.mjs` is idempotent and does not replace an existing `.env`.

```powershell
docker compose logs -f api web
docker compose down
```

`down` retains the named PostgreSQL volume. The compose stack applies migrations before the API starts. Ports 3000, 8080, 55432, 8025 and 1025 bind only to localhost.

## Run API and web locally

Use this when developing with Go 1.27.1 and Node.js 24.16.0. Start only the PostgreSQL and Mailpit containers, then run the API and web in separate terminals:

```powershell
node scripts/setup.mjs
docker compose up -d db mail
node scripts/run.mjs go run ./cmd/cardplay migrate
node scripts/run.mjs go run ./cmd/cardplay seed
node scripts/run.mjs go run ./cmd/cardplay serve
```

```powershell
cd web
npm ci
npm run dev
```

`scripts/run.mjs` loads the ignored root `.env` without printing its values and locates the standard Windows Go installation. If ports or hosts change, edit `.env` and set `API_INTERNAL_URL` for the Next.js process to the reachable API URL. Browser requests use Next.js same-origin rewrites; the API rejects unsafe requests from an unexpected origin.

## Checks and generated code

```powershell
node scripts/run.mjs go test ./cmd/... ./internal/... ./db/...
node scripts/run.mjs go vet ./cmd/... ./internal/... ./db/...
node scripts/run.mjs go build ./cmd/cardplay
cd web
npm ci
npm run format:check
npm run typecheck
npm run lint
npm run build
```

Go integration tests run when `TEST_DATABASE_URL` points to an **isolated** PostgreSQL database. CI provisions `cardplay_test` and runs them automatically. For local Compose testing, create it once with `docker compose exec -T db psql -U cardplay -d postgres -c 'CREATE DATABASE cardplay_test'`, set `TEST_DATABASE_URL` to the `DATABASE_URL` from `.env` with the database name changed to `cardplay_test`, and run the Go test command above. Tests only add unique test users and migrate the isolated database.

To regenerate sqlc output after editing `db/queries` or migrations:

```powershell
docker compose --profile tools run --rm sqlc
```

The checked-in Go output is in `internal/store`. Frontend versions are locked in `web/package-lock.json`; Go modules are pinned in `go.mod` and `go.sum`. Docker images are tagged to concrete release versions. `docker compose config --quiet` validates Compose interpolation.

## Configuration and architecture

Copy `.env.example` only for custom deployments; use `scripts/setup.mjs` for local development. Required values are validated at startup. Production requires an HTTPS `APP_ORIGIN`, PostgreSQL `sslmode=verify-full`, and authenticated SMTP with STARTTLS (`SMTP_ADDR`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `MAIL_FROM`). Sessions use random HttpOnly cookies; only hashes are stored. Keep credentials in environment/secret storage, never in source control.

The Go monolith separates `accounts`, `social`, `rooms`, `chat`, `matches`, `game`, and `realtime` behind chi routes. pgx/sqlc handles PostgreSQL; embedded, checksummed migrations run transactionally. A durable outbox drives WebSocket invalidations, and clients refetch authorized room views after reconnect. `Game` supplies versioned setup, validation, command application, and player-specific projection; only the Monopoly descriptor is registered today, with `playable: false`.

The endpoint, WebSocket, error, authorization, and projection contract is in [docs/06-api-and-architecture.md](docs/06-api-and-architecture.md). Phase 1 decisions and remaining gameplay questions are in [docs/02-monopoly-deal-rules.md](docs/02-monopoly-deal-rules.md). Product scope and release criteria are in [docs/01-product-requirements.md](docs/01-product-requirements.md) and [docs/05-roadmap-and-acceptance.md](docs/05-roadmap-and-acceptance.md).
