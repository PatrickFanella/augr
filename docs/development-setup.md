---
title: "Development Setup"
description: "Complete local development workflow for backend, frontend, database, testing, and smoke-mode execution."
status: "canonical"
updated: "2026-08-15"
tags: [development, setup, local-dev]
---

# Development Setup

This guide is for contributors who need the full day-to-day workflow rather than the shortest first-run path.

## Toolchain

Required:

- Go 1.25.8 or newer (the minimum is declared in `go.mod`)
- Node.js 22 (pinned in `.nvmrc` and `mise.toml`, and used by frontend CI)
- npm
- Docker and Docker Compose v2+
- PostgreSQL client tools if you want to inspect the database outside Compose

Recommended:

- [Task](https://taskfile.dev) for the project command runner
- `jq` for API and login scripting
- the Go quality/migration tools installed by `task tools`: `gofumpt`,
  `golangci-lint`, `govulncheck`, and `migrate`

## Repository layout

These are the directories you will touch most often:

| Path | Purpose |
| --- | --- |
| `cmd/tradingagent` | app bootstrap, runtime wiring, strategy runner, docs tests |
| `internal/api` | REST API, middleware, auth, settings, WebSocket hub |
| `internal/agent` | agent runtime, config resolution, prompts, runner orchestration |
| `internal/data` | provider chains, caching, historical downloads |
| `internal/execution` | brokers, paper trading, order management |
| `internal/risk` | hard risk engine, kill switch, exposure limits |
| `internal/repository/postgres` | persistence layer |
| `web/` | React/Vite frontend |
| `migrations/` | SQL migrations |
| `docs/` | canonical docs plus archive material |

## Configuration model

The server loads configuration from environment variables through `internal/config`.

Important behavior:

- `.env` is auto-loaded only when `APP_ENV=development`.
- `JWT_SECRET` is required for the API server to start.
- most provider integrations are opt-in by key presence
- non-secret settings edited through the API/UI persist to the `app_settings` table when the DB-backed persister is wired; secrets are not written back to `.env` or stored in the database
- startup fails fast on database schema mismatch before the rest of the runtime boots; fix by running migrations, then restarting the process

Start from:

```bash
cp .env.example .env
```

Then set the minimum viable local config:

```dotenv
APP_ENV=development
JWT_SECRET=replace-this-with-a-real-secret
OPENAI_API_KEY=...
```

## Running the stack with Docker Compose

The default contributor path is:

```bash
docker compose up --build
```

That Compose stack is backend-only in current local and production wiring. Run the frontend separately from `web/`.

Or with Task:

```bash
task dev
```

Useful Compose/Task commands:

```bash
task dev
task dev:down
task dev:logs
task dev:restart
task dev:psql
```

### Isolated Phase 1 and Phase 2 services

When another Augr Compose project is already using this checkout, keep Phase 1
and Phase 2 schema work on separate loopback-only ports and named volumes. The
built-in Docker bridge avoids allocating another custom subnet. The existing
`augr-phase1-*` container names are retained while Phase 2 uses the same
isolated development database; they do not refer to a shared or deployed
environment.

First-time creation:

```bash
docker volume create augr_phase1_postgres_data
docker volume create augr_phase1_redis_data

docker run -d \
  --name augr-phase1-postgres \
  --network bridge \
  --label com.subcult.augr.environment=phase1-local \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=tradingagent \
  -p 127.0.0.1:55464:5432 \
  -v augr_phase1_postgres_data:/var/lib/postgresql/data \
  timescale/timescaledb-ha:pg17

docker run -d \
  --name augr-phase1-redis \
  --network bridge \
  --label com.subcult.augr.environment=phase1-local \
  -p 127.0.0.1:56380:6379 \
  -v augr_phase1_redis_data:/data \
  redis:7-alpine
```

Start or stop the existing environment without touching the default Augr
stack:

```bash
docker start augr-phase1-postgres augr-phase1-redis
docker stop augr-phase1-postgres augr-phase1-redis
```

Apply or inspect migrations explicitly:

```bash
export AUGR_PHASE1_DB_URL='postgres://postgres:postgres@127.0.0.1:55464/tradingagent?sslmode=disable'
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" version
docker exec augr-phase1-redis redis-cli ping
```

These credentials and ports are intentionally local-development-only. Do not
reuse them for a shared, staging, or production database.

## Running the backend natively

If you want the API server outside Docker:

1. Start PostgreSQL and Redis yourself, or run only those services via Compose.
2. Set `DATABASE_URL`, `REDIS_URL`, and `JWT_SECRET`.
3. Run migrations.
4. Start the server:

```bash
go run ./cmd/tradingagent serve
```

Or build first:

```bash
task build
./bin/tradingagent serve
```

## Running the frontend

```bash
# Use the version manager already available on this host. `mise exec` makes
# the selected version explicit even in shells without the activation hook:
mise install
mise exec -- npm --prefix web install
mise exec -- npm --prefix web run dev

# Or, with nvm:
nvm use
cd web
npm install
npm run dev
```

The frontend default API base URL is `http://localhost:8080`.

The frontend is a separate Vite app. Backend root `/` is not the SPA in the current Compose or production stack.

## Database migrations

The project uses SQL migrations under `migrations/`.

Run them explicitly before expecting a new build to boot cleanly against an updated database. If the server already started and failed with a schema mismatch, apply migrations and then restart it; the mismatch is fail-fast and does not self-heal inside the running process.

Common commands:

```bash
task migrate:up
task migrate:down
task migrate:status
task migrate:create -- add_feature_name
```

The schema includes persistence for:

- strategies
- pipeline runs and phase timings
- pipeline run snapshots
- agent decisions and events
- conversations and messages
- orders, positions, trades
- memories
- market-data cache and historical OHLCV
- audit log
- users
- API keys
- backtest configs and backtest runs
- explicit accounts and append-only capital flows
- immutable ledger transactions and balanced postings
- mark observations and projection checkpoints
- canonical instruments and immutable dated alias events
- venue contracts and corporate-action facts
- explicit instrument-identity quarantine findings

Schema 66 deliberately leaves existing ticker-based application reads in
place. It backfills legacy symbols as deterministic quarantined identities and
does not infer currency, tick size, lot size, multiplier, settlement, or
tradability. Inspect the local quarantine before using any canonical identity
in new work:

```sql
SELECT
    instrument.identity_key,
    instrument.asset_class,
    instrument.primary_venue,
    finding.finding_code,
    finding.source,
    finding.details
FROM instruments AS instrument
JOIN instrument_identity_quarantine AS finding
  ON finding.instrument_id = instrument.id
WHERE instrument.status = 'quarantined'
ORDER BY instrument.identity_key, finding.observed_at, finding.id;
```

## Creating a local user

There is no self-service registration flow yet. For local dev:

```bash
docker compose exec postgres psql -U postgres -d tradingagent <<'SQL'
INSERT INTO users (username, password_hash)
VALUES ('demo', crypt('demo-pass', gen_salt('bf')))
ON CONFLICT (username) DO NOTHING;
SQL
```

## Smoke mode for deterministic runs

`APP_ENV=smoke` activates a deterministic manual-run path that is useful for end-to-end testing without depending on real LLMs and live upstream providers.

Because `.env` auto-loading only happens in `development`, export your env file before starting smoke mode:

```bash
set -a
source .env
set +a
export APP_ENV=smoke
./bin/tradingagent serve
```

Smoke mode is especially useful when you want to verify:

- strategy creation
- login/auth
- manual run dispatch
- run detail pages
- event plumbing
- persistence wiring

## Testing and quality checks

Primary Task targets:

```bash
task build
task tools
task test
task test:race
task test:integration
task test:cover
task lint
task fmt
task fmt:check
task vet
task vulncheck
task audit
task check
task ci
```

The Go task targets are scoped to `cmd/`, `internal/`, and `migrations/` so a
completed frontend install cannot make Go discover language ports bundled by
packages under `web/node_modules/`. Frontend tests also force relative mock API
and WebSocket paths; an ignored `web/.env.local` remains available for manual
development without redirecting the test suite to a running backend.

Notes:

- integration tests require PostgreSQL
- database integration tests create and remove isolated schemas; point `DB_URL` or
  `DATABASE_URL` at a disposable development database, never production
- docs-only validation currently relies mostly on file/link checks and the dedicated docs tests in `cmd/tradingagent`

## CLI workflow

The CLI talks to the local API server. Typical env setup:

```bash
export TRADINGAGENT_API_URL=http://127.0.0.1:8080
export TRADINGAGENT_TOKEN=...
```

Examples:

```bash
./bin/tradingagent strategies list
./bin/tradingagent run AAPL
./bin/tradingagent portfolio
./bin/tradingagent risk status
./bin/tradingagent dashboard
./bin/tradingagent memories search earnings
```

For CLI entry points, see the command summary in the repository [README](../README.md#cli) and run `./bin/tradingagent --help` after building.

## Frontend workflow

The web app lives in `web/` and exposes these routes:

- `/login`
- `/`
- `/strategies`
- `/strategies/:id`
- `/runs`
- `/runs/:id`
- `/portfolio`
- `/memories`
- `/settings`
- `/risk`
- `/realtime`

For the mounted route list, see `web/src/App.tsx`.

## Operational development notes

Useful health endpoints:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/health
curl http://localhost:8080/metrics
```

Useful database access:

```bash
docker compose exec postgres psql -U postgres -d tradingagent
```

Useful log inspection:

```bash
docker compose logs -f app
docker compose logs -f postgres
docker compose logs -f redis
```

## Current contributor hazards

Before doing anything expensive, read [Known Issues](known-issues.md).

The big ones today:

- unresolved merge conflicts exist in several runtime, risk, API-test, and frontend files
- some documented integrations are partially wired rather than fully productionized
- WebSocket auth is not enforced by the current handler
- secret values entered through the settings UI do not persist across restarts; non-secret settings persist through `app_settings`

## Suggested contributor reading order

1. [Getting Started](getting-started.md)
2. [Architecture Audit](AUGR_ARCHITECTURE_AUDIT.md)
3. [Roadmap](roadmap.md)
4. [ADRs](adr/README.md)
5. [Known Issues](known-issues.md)
