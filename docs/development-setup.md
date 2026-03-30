# Development Setup Guide

This guide walks through setting up a local development environment for **get-rich-quick**. Most developers will use the Docker Compose workflow; native setup is available for those who prefer running services directly on the host.

---

## Prerequisites

| Tool              | Version  | Purpose                              |
|-------------------|----------|--------------------------------------|
| Docker            | 24+      | Container runtime                    |
| Docker Compose    | v2+      | Multi-service orchestration          |
| Go *(native only)*| 1.25+    | Build & test the backend             |
| Task              | 3+       | Task runner ([taskfile.dev](https://taskfile.dev)) |
| Node.js *(optional)* | 20+  | Frontend development                 |

---

## 1. Docker Compose (Recommended)

This is the fastest way to get a working environment — database, cache, and the Go application all start with a single command, and code changes are hot-reloaded automatically via [Air](https://github.com/air-verse/air).

### 1.1 Clone & Configure

```bash
git clone https://github.com/PatrickFanella/get-rich-quick.git
cd get-rich-quick

# Copy example environment file
cp .env.example .env
```

Edit `.env` to add any API keys you need (LLM providers, data feeds, broker credentials). At minimum you'll need an LLM provider key for agent features:

```dotenv
OPENAI_API_KEY=sk-...          # or ANTHROPIC_API_KEY, etc.
```

### 1.2 Start Services

```bash
docker compose up --build
```

Or in the background:

```bash
docker compose up -d --build
```

This starts three containers:

| Service    | Port | Notes                                       |
|------------|------|---------------------------------------------|
| `app`      | 8080 | Go backend with Air hot-reload              |
| `postgres` | 5432 | PostgreSQL 17 (user: `postgres`, db: `tradingagent`) |
| `redis`    | 6379 | Redis 7                                     |

### 1.3 Verify

```bash
curl http://localhost:8080/healthz
# → ok
```

### 1.4 Run Migrations

Migrations are managed with [golang-migrate](https://github.com/golang-migrate/migrate). With Docker Compose running:

The Taskfile default `DB_URL` uses `tradingagent:tradingagent` credentials, but Docker Compose defaults the PostgreSQL user/password to `postgres`/`postgres`. Override `DB_URL` to match:

```bash
DB_URL="postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable" task migrate:up
```

### 1.5 Useful Docker Compose Commands

```bash
# View live logs
docker compose logs -f
task dev:logs               # shortcut

# Restart the app container (e.g. after changing go.mod)
docker compose restart app
task dev:restart            # shortcut

# Open a psql shell (default Compose user is postgres)
docker compose exec postgres psql -U postgres -d tradingagent

# Stop services
docker compose down

# Stop and wipe all data
docker compose down -v
```

---

## 2. Native Setup (Without Docker)

Use this approach if you want to run the Go binary directly on your host, e.g. for profiling, debugging, or using a local IDE debugger.

### 2.1 Install Go

Follow the official instructions at <https://go.dev/dl/> for Go 1.25+.

### 2.2 Install Task

```bash
# macOS
brew install go-task

# Linux (snap)
sudo snap install task --classic

# Or see https://taskfile.dev/installation/
```

### 2.3 Install Dev Tools

The Taskfile provides a convenience target that installs all required tools:

```bash
task tools
```

This installs:
- **gofumpt** — stricter Go formatting
- **golangci-lint** — linter aggregator
- **govulncheck** — vulnerability scanner
- **golang-migrate** — database migration CLI

### 2.4 Start PostgreSQL & Redis

You can use Docker for just the infrastructure services:

```bash
docker compose up -d postgres redis
```

Or install them natively — ensure PostgreSQL 17 and Redis 7 are running and accessible on their default ports.

### 2.5 Configure Environment

```bash
cp .env.example .env
```

When running natively (outside Docker), update the hostnames from container names to `localhost`:

```dotenv
DATABASE_URL=postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable
REDIS_URL=redis://localhost:6379/0
```

### 2.6 Run Migrations

Ensure `DB_URL` matches your database credentials (it defaults to `tradingagent:tradingagent` in the Taskfile):

```bash
DB_URL="postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable" task migrate:up
```

To create a new migration:

```bash
task migrate:create -- add_new_table
```

To check migration status:

```bash
task migrate:status
```

### 2.7 Build & Run

```bash
task build                # → ./bin/tradingagent
./bin/tradingagent serve  # Start the API server
```

Or, if you've already built the binary, you can start the server directly:

```bash
./bin/tradingagent serve
```

---

## 3. Build, Test & Lint

### Unit Tests

```bash
task test                 # Short mode, fast feedback
task test:race            # With Go race detector
```

### Integration Tests

Integration tests require a running PostgreSQL instance. They create isolated per-test schemas automatically.

```bash
# Ensure DATABASE_URL is set and PostgreSQL is running, then:
task test:integration
```

### Coverage

```bash
task test:cover           # Generates HTML coverage report
```

### Lint & Format

```bash
task lint                 # golangci-lint
task fmt                  # Format with gofumpt
task fmt:check            # Check formatting without modifying files
task vet                  # go vet
task vulncheck            # Vulnerability scan
```

### Full Validation

```bash
task check                # Pre-push: build + test + lint
task ci                   # Replicate the full CI pipeline locally
```

---

## 4. Database Migrations

Migrations live in the `migrations/` directory and use sequential numbering:

```
migrations/
  000001_initial_schema.up.sql
  000001_initial_schema.down.sql
  000002_historical_ohlcv.up.sql
  ...
```

| Command                | Description                         |
|------------------------|-------------------------------------|
| `task migrate:up`      | Apply all pending migrations        |
| `task migrate:down`    | Roll back the last migration        |
| `task migrate:status`  | Show current migration version      |
| `task migrate:create -- <name>` | Create a new migration pair |

---

## 5. Frontend Development

The frontend lives in the `web/` directory and uses Vite + React.

```bash
cd web
npm install
npm run dev               # Starts Vite dev server
```

During development, the backend API is available at `http://localhost:8080`. Frontend code can call this URL directly, or you can configure a dev proxy in `web/vite.config.ts` (for example, under `server.proxy`) if you prefer to use relative API paths.

---

## 6. Pre-commit Hooks

The project uses [pre-commit](https://pre-commit.com/) for automated code quality checks on every commit:

```bash
pip install pre-commit
pre-commit install
```

Hooks run gofumpt and golangci-lint for Go files, and ESLint + Prettier for TypeScript/JavaScript.

---

## 7. Project Layout

```
cmd/tradingagent/       CLI entry point (Cobra bootstrap)
internal/
  agent/                Pipeline phases, debate system, typed node interfaces
  api/                  REST API (chi/v5), WebSocket hub, auth middleware
  backtest/             Backtesting engine
  cli/                  Cobra subcommands, Bubble Tea TUI
  config/               Config loading from env / .env files
  data/                 Market data providers (Alpha Vantage, Polygon, Yahoo, Binance)
  domain/               Core domain models
  execution/            Broker adapters (Alpaca, Binance, Polymarket)
  llm/                  Multi-provider LLM abstraction
  memory/               Agent memory (PostgreSQL full-text search)
  repository/           PostgreSQL data access layer
  risk/                 Risk engine, circuit breakers, kill switch
  scheduler/            Cron-based strategy scheduling
migrations/             SQL migrations (golang-migrate)
web/                    Frontend (TypeScript, React, Vite)
docs/                   Architecture docs, ADRs, research
scripts/                Utility scripts
```

---

## 8. CI Pipeline

GitHub Actions runs the following jobs on every push and pull request:

1. **Lint** — golangci-lint
2. **Unit Tests** — `go test -short -race` with coverage
3. **Integration Tests** — PostgreSQL service container, migrations, `go test -run Integration`
4. **Smoke Tests** — Full Docker Compose environment, health check, `go test -run Smoke`
5. **Build** — Compile binary, build Docker image

See [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) for full details.

---

## 9. Troubleshooting

### Port already in use

If port 8080, 5432, or 6379 is already taken:

```bash
# Find the process using a port
lsof -i :8080

# Or change the port in .env
APP_PORT=9090
```

### Docker Compose fails to start

```bash
# Check container status
docker compose ps

# Check individual service logs
docker compose logs postgres
docker compose logs app

# Rebuild from scratch
docker compose down -v
docker compose up --build
```

### Migration errors

```bash
# Check current migration version
task migrate:status

# Force a specific version (use with caution)
migrate -path migrations -database "$DATABASE_URL" force <version>
```

### Tests fail with "database not found"

Ensure `DATABASE_URL` is set and the PostgreSQL server is running. Integration tests are skipped when the `-short` flag is used or when `DATABASE_URL` is not set.

---

## Further Reading

- [System Architecture](design/system-architecture.md)
- [API Design](design/api-design.md)
- [Architecture Decision Records](adr/)
- [CONTRIBUTING.md](../CONTRIBUTING.md)
