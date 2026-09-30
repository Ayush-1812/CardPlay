# Operations: observability, capacity, backup and recovery

This is for whoever deploys and runs CardPlay. The release gate is in [11-release-checklist.md](11-release-checklist.md).

## Configuration added for operations

| Variable | Default | Purpose |
|---|---|---|
| `LOG_FORMAT` | `json` in production, text otherwise | Set `json` for log shippers. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. Health probes log at `debug`. |
| `METRICS_ADDR` | unset (off) | `host:port` for a separate `/metrics` listener. It is **not authenticated**: bind it to a private interface (for example `127.0.0.1:9090` or the pod IP) and never expose it publicly. |
| `DB_MAX_CONNS` | `20` | PostgreSQL pool size per API instance (4–200). Keep `instances × (DB_MAX_CONNS + 1)` below the server's `max_connections`. The `+1` is the dedicated LISTEN connection. |
| `ENABLE_HSTS` (web build) | unset | Set to `1` when building the web image for an HTTPS-only domain. |

## Logs

The API writes one JSON object per line to stderr, with `service: "cardplay-api"`.

- **`http_request`**: one line per request, with `request_id`, `method`, `route`, `status`, `bytes` and `duration_ms`.
  - `route` is the route *pattern* (for example `/api/v1/rooms/{roomID}`), never the raw URL, so query strings and tokens are never logged.
  - No user ID, email, cookie or body is logged. WebSocket connections log once, when they close, with their full duration.
- **`panic`**: a recovered handler panic, with stack trace and `request_id`. The client gets a generic `500 INTERNAL`.
- **`client_error`**: a browser error reported by the web client (see below).
- **`readiness check failed`**, **`outbox listener disconnected; retrying`**, **`match sweep failed`**: warnings worth alerting on when repeated.

The database layer logs only the SQLSTATE of unexpected errors, not their text. Rule rejections are returned to the player and are not logged.

## Health and readiness

| Endpoint | Meaning | Use as |
|---|---|---|
| `GET /healthz` | The process is serving HTTP. | Liveness probe. |
| `GET /readyz` | The database answers within 2 s, every embedded migration is applied with its recorded checksum, and the PostgreSQL LISTEN connection for live updates is active. Returns `503` with `{"checks": {...}}` naming the failing check (`database`, `migrations`, `notifications`). | Readiness probe and load-balancer health check. `cardplay health` (used by Compose) calls it. |

An instance whose notification listener is down cannot deliver live updates, so it reports not ready and stops getting traffic. The listener reconnects with backoff, up to 30 s between attempts.

## Metrics

Prometheus text format on `METRICS_ADDR` at any path (for example `/metrics`). Labels never carry user content, IDs or card data.

| Metric | Type | Notes |
|---|---|---|
| `cardplay_http_requests_total{method,route,code}` | counter | `code` is the status class (`2xx`…`5xx`). |
| `cardplay_http_request_duration_seconds{route}` | histogram | WebSocket upgrades are excluded. |
| `cardplay_ws_connections` | gauge | Open sockets on this instance. |
| `cardplay_ws_messages_total{type}` | counter | Inbound protocol messages. |
| `cardplay_game_commands_total{result}` | counter | `applied`, `stale`, `rule_rejected`, `duplicate_conflict`, `refused`, `error`. |
| `cardplay_game_command_duration_seconds` | histogram | Validate, apply, persist and commit one command. |
| `cardplay_match_state_pushes_total` | counter | Per-player projections sent. |
| `cardplay_notifications_total{kind}`, `cardplay_notifications_listening` | counter, gauge | Outbox NOTIFY deliveries; `listening` is 0 while the listener is reconnecting. |
| `cardplay_logins_total{result}` | counter | `success` or `rejected`. |
| `cardplay_rate_limited_total{scope}` | counter | `api`, `auth`, `auth_global`, `chat`, `websocket`, `websocket_connections`. |
| `cardplay_chat_messages_total` | counter | |
| `cardplay_panics_total`, `cardplay_client_errors_total` | counter | |
| `cardplay_db_pool_{acquired,idle,max}_connections`, `cardplay_db_pool_empty_acquire_total`, `cardplay_db_pool_acquire_wait_seconds_total` | gauge | Pool saturation. A steadily rising wait total means the pool is too small. |
| `cardplay_goroutines`, `cardplay_uptime_seconds` | gauge | |

Suggested alerts:
- `/readyz` failing for more than 1 minute.
- `rate(cardplay_panics_total[5m]) > 0`.
- 5xx ratio above 1% for 5 minutes.
- `cardplay_notifications_listening == 0` for 1 minute.
- `game_command_duration` p99 above 250 ms for 10 minutes.
- `rate(cardplay_db_pool_acquire_wait_seconds_total[5m]) > 0.5`.
- A spike in `cardplay_client_errors_total`.

## Browser error reporting

The web client reports uncaught errors, unhandled promise rejections and React render errors (`app/error.tsx`, `app/global-error.tsx`) to `POST /api/v1/client-errors`.

- Reports are sent same-origin only; the Origin check applies.
- The endpoint takes a bounded JSON body and is rate limited (30 per minute per user or per proxy address).
- Each page load sends at most 10 reports, with no duplicates.
- The server logs `kind`, `message` (500 chars), `page` (path only; query and fragment stripped, so invite tokens never leave the browser), `stack` (2,000 chars) and the React `digest`.
- Nothing is sent to third parties. To forward reports to an error tracker, ship the `client_error` log lines.

## Capacity measured before release

Measured with `go run ./scripts/loadtest` on one Windows laptop (8 logical CPUs) running the API, PostgreSQL 18 and the load generator together. It is a lower bound, not a production benchmark.

Each bot logs in through the API, creates or joins a room through an invitation, readies, and plays Monopoly Deal over WebSockets. Bots bank, lay properties, charge Birthday and Debt Collector, pay and accept, with 200–600 ms think time, chat roughly every 20 s and poll the room list.

| Load | Pool | Commands/s | Command ack p50 / p95 / p99 | Command → new state p99 | Errors |
|---|---|---|---|---|---|
| 25 matches × 4 (100 sockets), 3 min | 10 | 36 | 5.8 / 12.1 / 24.6 ms | 39 ms | 0 |
| 50 × 4 (200 sockets), 3 min | 10 | 66 | 6.0 / 11.7 / 21.6 ms | 31 ms | 0 |
| 50 × 4 (200 sockets), 3 min | 20 | 73 | 5.8 / 12.6 / 28.1 ms | 41 ms | 0 |
| 100 × 4 (400 sockets), 2 min, logins over 10 s | 20 | 197 | 7.3 / 22.0 / 42.7 ms | 57 ms | 0 |

Findings and fixes:
- **Database pool saturation.** With the old hard-coded pool of 10, 200 sockets caused 5,124 waits for a connection (62 s of waiting in total). The pool is now `DB_MAX_CONNS`, default 20, which halved the waits at the same load (2,240 / 31 s). The metrics above expose saturation.
- **Password hashing is CPU-bound by design.** Argon2id (64 MiB, 2 passes) costs about 59 ms per hash, so this machine peaks near 30 logins/s. Concurrent hashes are now capped at the CPU count so a login burst cannot exhaust memory (200 × 64 MiB). 200 simultaneous logins took up to 8 s; spread over 10 s, 400 logins had p95 2.1 s.
- **Site-wide auth ceiling.** Behind the Next.js proxy all auth requests share one limiter key, capped at 600 per minute. That is ample for a small launch but must be revisited before a large one.
- **Windows accept backlog.** 400 simultaneous TCP connects exceeded the Windows accept backlog (~200) and were refused before reaching Go. Linux defaults to 4096 (`net.core.somaxconn`); keep that on production hosts.
- **Next optimization.** Each state push reads the match with 7 round trips per viewer, so each command costs about 4 × 7 queries in a 4-player match. Consolidating that read is the next step if `db_pool_acquire_wait` grows in production.

## Backup and recovery

**What must be backed up:** the PostgreSQL database only. The API and web are stateless. Sessions, matches, command history, chat and the outbox are all in PostgreSQL.

**Targets for a small release:**
- RPO 24 h with nightly logical backups, or about 5 minutes with provider point-in-time recovery (WAL archiving).
- RTO 30 minutes for a database under 1 GB (the drill restored 35 MB in 12 s).

Prefer the managed provider's PITR where available. Keep the logical backups below as well: they are portable and let you restore a single table.

### Nightly logical backup

`scripts/ops/backup.sh` runs `pg_dump` in custom format from one consistent snapshot, so matches, commands and outbox always agree. It then:
- checks the archive can be listed;
- writes a `.sha256` next to it;
- deletes archives older than `BACKUP_KEEP_DAYS` (14).

It never prints the connection URL.

```sh
BACKUP_DATABASE_URL='postgres://backup_role@db/cardplay?sslmode=verify-full' \
BACKUP_DIR=/var/backups/cardplay scripts/ops/backup.sh
```

- Run it daily from cron or a scheduled job with `pg_dump` 18 or newer.
- Copy the directory to storage in another region or account, encrypted at rest and write-once where possible.
- The role needs `SELECT` on all tables.
- Alert if no new archive appears for 26 hours.

With Docker Compose locally:

```sh
docker compose exec -T db pg_dump -U cardplay -d cardplay --format=custom > cardplay.dump
```

### Restore

1. **Stop writes.** Scale the API to zero or stop the `api` service. Leave the web up; it shows reconnecting.
2. **Create an empty database** (never restore over live data): `CREATE DATABASE cardplay_restore;`
3. **Restore.** `scripts/ops/restore.sh` checks the checksum, refuses a non-empty target, restores in one transaction and prints row counts:
   ```sh
   RESTORE_DATABASE_URL='postgres://owner@db/cardplay_restore?sslmode=verify-full' \
   scripts/ops/restore.sh /var/backups/cardplay/cardplay-<stamp>.dump
   ```
4. **Migrate.** Run `cardplay migrate` against the restored database. It verifies every applied migration's checksum and applies newer ones.
5. **Point the API at it.** Update `DATABASE_URL` (or swap database names) and start one instance. Wait for `/readyz` to return 200, then scale up.
6. **Tell players.** Matches resume at the backup point. Commands after it are lost. Clients resynchronize automatically on reconnect because state is always served from the database.
7. **Afterwards.** Revoke sessions if the incident involved credentials (`DELETE FROM sessions;` signs everyone out). Keep the old database until the restore is verified.

### Drill performed on 2026-09-30

Run against a copy of the load-test database: 1,925 users, 267 matches, 57,711 commands, 4,712 chat messages.

| Step | Result |
|---|---|
| Backup taken *while* 100 bots were playing (58 commands/s) | 34 MB, 9 s |
| Restore into an empty database | 12 s. All 267 matches consistent: each match's revision equals its newest snapshot, and no command is ahead of its match. |
| Restore over a non-empty database | Refused |
| Restore of a corrupted archive | Refused (checksum mismatch) |
| Quiescent backup, restore, then per-table md5 of every row across all 20 tables | Identical |
| `cardplay migrate` and `serve` on the restored database | `/readyz` 200 |

Repeat the drill every quarter and after any schema change.

## Routine operations

- **Deploy.** Run `cardplay migrate` once (it takes an advisory lock, so concurrent runs are safe), then roll the API instances. A restarted instance loses no acknowledged command. Clients reconnect with backoff and resynchronize. Matches pause while a seat is disconnected and resume on reconnect.
- **Rollback.** Migrations roll back one step at a time with `cardplay migrate-down`, which is disabled in production. Test down migrations in staging (CI checks up/down/up on every change). In production, prefer a forward fix or a restore.
- **Background jobs.** These run inside every API instance:
  - every 5 s: host transfer and match presence sweeps;
  - hourly: purge of expired chat, outbox, sessions and login devices, and match retention (90 days).
  
  No separate worker is needed.
- **Secrets.** `DATABASE_URL`, `SMTP_PASSWORD` and `SEED_PASSWORD` come from the environment or secret store only. `seed` and `migrate-down` refuse to run in production.
