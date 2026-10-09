# Deploying CardPlay

Three parts have to live somewhere: the Next.js client, the Go API and PostgreSQL. The API holds WebSockets open and runs background sweeps every few seconds, so it needs a host that runs a long-lived process. Vercel cannot be that host, which is what shapes every option here.

The recommended deployment puts everything on **one small server behind one domain**, exactly as it runs locally: the client proxies `/api` and `/ws` to the API, so there is no second origin, no cross-site cookie problem and no socket ticket. It costs nothing on an always-free VM and never sleeps.

Running it day to day (logs, metrics, backups, capacity) is in [10-operations.md](10-operations.md). The release gate is [11-release-checklist.md](11-release-checklist.md).

## One host, one origin

```
browser ──https──> Caddy ──> web (Next.js) ──> api (Go) ──> db (PostgreSQL)
                   TLS        /api, /ws proxy     private network only
```

Everything is in [`compose.prod.yaml`](../compose.prod.yaml). Only Caddy publishes ports; the API and database are reachable only from inside the Docker network.

### What you need

- A machine that runs Docker, with 1 GB of memory. Any of the hosts below; see [Where to run it](#where-to-run-it).
- A free subdomain from DuckDNS pointing at the VM's public IP. Caddy needs a real name to obtain a certificate; a bare IP cannot have one.
- Free SMTP credentials from any transactional provider, for verification and recovery mail.

### Where to run it

The stack is ordinary Docker, so it runs anywhere with a public IP. What differs is price, whether it stays awake, and how far it is from the players.

| Host | Price | Notes |
|---|---|---|
| A VPS near your players (DigitalOcean Bangalore, Vultr or Linode Mumbai, Lightsail Mumbai) | about $4-6/month | Always on, low latency for players in India, no surprises. The best experience for a real-time game. |
| Hetzner (Germany) | about EUR 4/month | Cheapest serious VPS, but 150 ms away from India. Fine for correctness, noticeable in play. |
| AWS, Azure or Google 12-month free tiers | free for a year | A normal VM in a nearby region (AWS and Azure have Mumbai). Check the current terms: AWS changed its free tier for new accounts in 2025. |
| Oracle Cloud Always Free | free forever | Best value when you can get it. The ARM shape is usually "out of capacity": retry in another availability domain or region, or take the always-free AMD shape, which is smaller but enough. |
| Google Cloud Always Free e2-micro | free forever | Only in three US regions, so about 250 ms from India. Workable for testing, poor for playing. |
| Your own PC with a Cloudflare Tunnel | free | No public IP or DNS needed; Cloudflare terminates TLS and forwards to the stack. Good for playing with friends today. The game is up only while the machine is. |

If none of these fit, the client can go on Vercel and the API on Render's free tier instead; see [the alternative below](#alternative-the-client-on-vercel). It sleeps when idle, but it costs nothing and needs no server of your own.

With a Cloudflare Tunnel there is no Caddy, no DuckDNS and no open ports: run the stack without the `caddy` service, point the tunnel at `web:3000`, and set `SITE_ADDRESS` to the hostname Cloudflare gives you.

### 1. Open the VM's ports

Ports 80 and 443 must reach the machine, which on most cloud VMs means two separate places. In the provider's console, add ingress rules for TCP 80 and 443 (on Oracle: the VCN's security list or a network security group). Then on the machine itself, because cloud images ship with their own firewall:

```sh
# Ubuntu/Debian images with iptables rules
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
sudo netfilter-persistent save

# Oracle Linux / RHEL family
sudo firewall-cmd --permanent --add-service=http --add-service=https && sudo firewall-cmd --reload
```

Forgetting the second step is the usual reason certificates never arrive.

### 2. Point the name at the machine

Create a subdomain on DuckDNS and set its IP to the VM's public address. Check it from your own machine before going on:

```sh
nslookup yourname.duckdns.org
```

### 3. Install Docker and get the code

If the repository is private, use a token in the URL or add a deploy key first.

```sh
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER" && newgrp docker
git clone https://github.com/Ayush-1812/CardPlay.git && cd CardPlay
```

### 4. Settings

```sh
cp .env.prod.example .env.prod
nano .env.prod
```

| Variable | What it is |
|---|---|
| `SITE_ADDRESS` | The public name, no scheme and no trailing slash: `yourname.duckdns.org`. Caddy gets a certificate for exactly this, and the API accepts writes and sockets only from it. |
| `ACME_EMAIL` | Where Let's Encrypt sends expiry warnings. |
| `POSTGRES_USER`, `POSTGRES_DB`, `POSTGRES_PASSWORD` | The database. Use letters and digits in the password: it is placed in a URL. |
| `SMTP_ADDR`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `MAIL_FROM` | Mail. Production refuses to start without these. |
| `MATCH_TURN_TIMEOUT` | How long a player may think before the table moves for them. Default `2m`. |

### 5. Database certificates

Production verifies the database's certificate in full, so even a database on the same host needs TLS. One command issues a private CA and a certificate for the name `db`:

```sh
sudo sh deploy/make-db-certs.sh
```

It writes `deploy/certs/`, which is ignored by git. The API trusts `ca.crt`; the database uses `server.crt` and `server.key`. Keep `ca.key` private; it exists only to issue a replacement in ten years.

### 6. Start

```sh
docker compose -f compose.prod.yaml --env-file .env.prod up -d --build
```

The client bakes the API address in at build time, which is why `compose.prod.yaml` passes it as a build argument; changing it means rebuilding, not restarting. The first build takes a few minutes. Migrations run automatically before the API starts, and Caddy requests the certificate as soon as it has traffic on port 80. Watch it come up:

```sh
docker compose -f compose.prod.yaml --env-file .env.prod ps
docker compose -f compose.prod.yaml --env-file .env.prod logs -f caddy api
```

### 7. Check it

```sh
curl -si https://yourname.duckdns.org/api/v1/healthz | head -3
```

Then in a browser at `https://yourname.duckdns.org`:

1. **Play as guest** with a name.
2. Create a room — the badge must say **Connected**, not reconnect in a loop. That is the WebSocket working through Caddy and the client.
3. Open the invite link in a second browser or phone, join, start a match.
4. Play a card in one and watch the other update. Send a chat message both ways.
5. Reload mid-match: the same seat and hand come back.

### Updating

```sh
git pull
docker compose -f compose.prod.yaml --env-file .env.prod up -d --build
```

Migrations run before the new API starts. Acknowledged moves survive the restart; clients reconnect on their own and matches resume.

### Backups

The database holds everything, including matches in progress. Take a dump regularly and keep it off the machine:

```sh
docker compose -f compose.prod.yaml --env-file .env.prod exec -T db \
  pg_dump -U cardplay -d cardplay --format=custom > cardplay-$(date +%F).dump
```

Restore and verification drills are in [10-operations.md](10-operations.md).

### Why the database stays on the box

A managed database (Supabase, Neon) is tempting, but this application is a poor fit for one:

- **Round trips.** Every move is one locked transaction that reads the match, writes the new snapshot, its events, the command record and an outbox row. On the same host that is a fraction of a millisecond each; across the internet it is tens of milliseconds each, and the player feels it on every card.
- **Live updates depend on `LISTEN/NOTIFY`.** A trigger on `outbox` calls `pg_notify`, and each API instance holds a dedicated connection on `LISTEN cardplay_outbox`. A pooled connection string in transaction mode silently drops notifications, and nothing moves until a client reloads. If a managed database is used, it must be a direct or session-mode connection.
- **Free tiers go to sleep.** Supabase pauses an inactive project after about a week and waits for a human to restore it; Neon suspends compute but resumes by itself. Either way the first move after a quiet spell pays for it.

A database on the same host costs nothing, answers instantly, and needs only the backup below. Its one weakness is that the data lives on one machine, which a scheduled dump copied off the box answers.

## Alternative: the client on Vercel

Keep the client on Vercel and put the API elsewhere (Render's free tier, Fly, a VPS). The trade is more moving parts, a proxy hop and a few constraints. Vercel cannot proxy a WebSocket, so the client opens it directly against the API with a one-shot ticket: it calls `POST /api/v1/realtime/ticket` over the proxied, cookie-authenticated path, then connects to `wss://<api>/ws?ticket=…`. Tickets last 30 seconds, are deleted on first use and die with their session. Setting `NEXT_PUBLIC_API_ORIGIN` turns this on; leaving it unset keeps the plain same-origin socket.

1. **Database:** Neon, using the **direct** connection string with `sslmode=verify-full`. Never a transaction pooler: `LISTEN/NOTIFY` carries every live update and a pooler drops it.
2. **API:** Render reads [`render.yaml`](../render.yaml) from the repository (New → Blueprint) and builds the root `Dockerfile`. Fill in `APP_ORIGIN` (the **client's** origin, exactly), `DATABASE_URL` and the SMTP values. The Blueprint sets `MIGRATE_ON_START=1`, so the new API instance applies embedded migrations before it begins serving and `/readyz` can return 200. If the service was created outside the Blueprint, add that environment variable in Render's dashboard before redeploying.
3. **Check the API:** `https://<api>/readyz` must return 200. The game catalog at `https://<api>/api/v1/games` must include both Monopoly Deal and Trump before deploying the client.
4. **Client:** Vercel project with Root Directory `web` — without it Vercel finds `go.mod` at the repository root and tries to build a Go project. Set `API_INTERNAL_URL` and `NEXT_PUBLIC_API_ORIGIN` to the API's URL, and `ENABLE_HSTS=1`. Both URLs are baked in at build time, so changing them needs a redeploy.

Things to know before choosing this: a free Render service **sleeps** after about 15 minutes idle, so the first visitor waits roughly a minute and the background sweeps stop meanwhile; and **preview deployments do not work**, because each gets a new URL while the API accepts exactly one origin.

## Releasing and rolling back

A release is one migration step and one roll of the API. The client and the API are versioned together but deploy separately, so order matters.

### Release

1. **Check the build.** `go test ./...`, the browser suite, and `sqlc diff` must be clean. The release checklist is [11-release-checklist.md](11-release-checklist.md).
2. **Apply migrations.** `cardplay migrate` takes an advisory lock, so running it twice or from two machines is safe. Migrations are embedded in the binary and checksummed: a server refuses to report ready if the database is behind or if an applied migration's checksum differs. The free Render Blueprint runs this step on startup with `MIGRATE_ON_START=1`; other deployments run `cardplay migrate` before rolling the API.
3. **Roll the API.** A restarted instance loses no acknowledged command. Clients reconnect with backoff and resynchronise; matches pause while a seat is away and resume when it returns.
4. **Deploy the client.** The API address is compiled into it, so a change there needs a rebuild, not a restart.
5. **Verify.** `/readyz` must return 200 on every instance, then play one hand of each game.

Migrations are written to be safe with the previous version running: new tables and nullable columns only, so a roll is never all-or-nothing.

### Rollback

- **Prefer forward.** `cardplay migrate-down` is disabled when `APP_ENV=production`. The project tests up/down/up in CI, but in production a forward fix or a restore is safer than reversing a migration under traffic.
- **Reverting code only** (no new migration): redeploy the previous image. Nothing else is needed, because migrations are additive.
- **Reverting across a migration:** restore the most recent dump into a new database, point the API at it, and roll. Expect to lose anything committed since the dump, including matches in progress.
- **A bad client build:** redeploy the previous client; it keeps working against the newer API as long as no command kind was removed.

### Backups

The database holds everything, including matches in progress. Take a dump on a schedule and copy it off the machine:

```sh
pg_dump "$DATABASE_URL" --format=custom > cardplay-$(date +%F).dump
pg_restore --dbname="$RESTORE_URL" --exit-on-error cardplay-2026-10-03.dump
```

A backup is only real once a restore has been proven: restore into an empty database, start the API against it, and check `/readyz` and a match's state. The full drill, including a per-table comparison, is in [10-operations.md](10-operations.md).

## A host that sleeps

A free Render service stops after about 15 minutes without traffic. The next
visitor then waits up to a minute while it starts, and until it answers the
gateway in front of it returns 502 with `X-Render-Routing: no-deploy`. This is
the usual reason a site that worked last week looks broken today.

Two defences, both already in the repository:

1. **The client waits it out.** A read that fails with a dropped connection or
   502, 503 or 504 is retried with backoff for about 40 seconds, and the
   screen says the server is starting instead of showing an error. Writes are
   never retried, because one may have been applied even when the answer was
   lost.
2. **A scheduled ping keeps it running.** [`.github/workflows/keepalive.yml`](../.github/workflows/keepalive.yml)
   calls the health endpoint every ten minutes once the repository variable
   `API_HEALTH_URL` is set, for example `https://cardplay.onrender.com/healthz`.

**Keep exactly one free service in the account.** An always-awake service uses
about 730 of the 750 free instance-hours a month; a second one exhausts the
allowance and both are suspended until the month turns over, which looks
exactly like the sleeping problem but does not fix itself when pinged.

## When something is wrong

| Symptom | Cause |
|---|---|
| No certificate; Caddy retries | Port 80 is not reaching the machine (cloud firewall or the VM's own rules), or the name does not point at this IP yet. |
| "CardPlay returned an unexpected response" | The client cannot reach the API. On one host, check `docker compose ps` and the `api` logs; on Vercel, `API_INTERNAL_URL` is wrong or the API is asleep. |
| API will not start, log mentions `sslmode` | `DATABASE_URL` lacks `sslmode=verify-full`, or `deploy/certs` was not created before the first start. |
| API will not start, log mentions SMTP or origin | Production requires authenticated SMTP and an HTTPS `APP_ORIGIN`. |
| Database refuses to start after the certificates were made | `server.key` must be owned by uid 999 and mode 600: `sudo chown 999:999 deploy/certs/server.key && sudo chmod 600 deploy/certs/server.key`. |
| `403 ORIGIN_REJECTED` on sign-in | `APP_ORIGIN` does not exactly match the address in the browser — a trailing slash, `www.`, or the wrong host. |
| Table stuck on "Connecting" | On one host: Caddy or the client is not passing the upgrade; check `logs caddy web`. On Vercel: `NEXT_PUBLIC_API_ORIGIN` was missing at build time. |
| Moves appear only after a reload | The database connection is pooled. Use a direct one. |
| Build fails with "No Go entrypoint found" | A Vercel project whose Root Directory is not `web`. |
