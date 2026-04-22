# get-rich-quick

An autonomous, multi-agent trading system built in Go. The system uses LLM-powered agents organized in a pipeline to analyze markets, debate investment theses, generate trade plans, evaluate risk, and execute orders — all with configurable risk controls and paper-trading support.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                         Trading Agent Pipeline                          │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │ Phase 1: Analysis (parallel)                                     │    │
│  │  Market Analyst · Fundamentals · News · Social Media             │    │
│  └─────────────────────────┬────────────────────────────────────────┘    │
│                            ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │ Phase 2: Research Debate (3 rounds)                              │    │
│  │  Bull Researcher ◄──► Bear Researcher → Research Manager         │    │
│  └─────────────────────────┬────────────────────────────────────────┘    │
│                            ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │ Phase 3: Trading                                                 │    │
│  │  Trader Agent → Entry, size, stops, take-profit                  │    │
│  └─────────────────────────┬────────────────────────────────────────┘    │
│                            ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │ Phase 4: Risk Debate (3 rounds)                                  │    │
│  │  Aggressive ◄──► Conservative ◄──► Neutral → Risk Manager        │    │
│  └─────────────────────────┬────────────────────────────────────────┘    │
│                            ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │ Phase 5: Execution                                               │    │
│  │  Risk checks → Order → Fill → Position → Audit                   │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                          │
├──────────────────────────────────────────────────────────────────────────┤
│  REST API (chi/v5)  │  WebSocket  │  Cobra CLI / TUI  │  Scheduler      │
├──────────────────────────────────────────────────────────────────────────┤
│  PostgreSQL 17      │  Redis 7    │  LLM Providers    │  Broker Adapters │
└──────────────────────────────────────────────────────────────────────────┘
```

### Technology Stack

| Layer           | Technology                                                 |
|-----------------|------------------------------------------------------------|
| Language        | Go 1.25                                                    |
| HTTP Router     | chi/v5                                                     |
| Database        | PostgreSQL 17 (pgx/v5)                                    |
| Cache           | Redis 7                                                    |
| CLI             | Cobra + Bubble Tea TUI                                     |
| LLM Providers   | OpenAI, Anthropic, Google, OpenRouter, XAI, Ollama         |
| Data Providers  | Alpha Vantage, Polygon, Yahoo Finance, Binance             |
| Brokers         | Alpaca, Binance (with paper-trading modes)                 |
| Frontend        | TypeScript, React, Vite                                    |
| Task Runner     | Taskfile                                                   |
| Containerization| Docker & Docker Compose                                    |

## Quick Start

> **Prerequisites:** [Docker](https://docs.docker.com/get-docker/), [Docker Compose v2+](https://docs.docker.com/compose/install/), and either one supported cloud LLM API key or a local [Ollama](https://ollama.com/download) install.

```bash
# 1. Clone the repository
git clone https://github.com/PatrickFanella/get-rich-quick.git
cd get-rich-quick

# 2. Copy the example environment file and configure an LLM provider
cp .env.example .env

# 3. Start the backend Compose stack (app + PostgreSQL + Redis)
docker compose up -d --build

# 4. Apply database migrations explicitly
task migrate:up

# 5. Restart the app if it started before migrations or reported a schema mismatch
docker compose restart app
```


For cloud LLMs, set one provider key in `.env` (for example `OPENAI_API_KEY`). For local Ollama, install Ollama, run `ollama pull llama3.2`, then set `LLM_DEFAULT_PROVIDER=ollama` and keep `OLLAMA_MODEL=llama3.2`. See the [Development Setup Guide](docs/development-setup.md) for the full prerequisites list and Docker-vs-native Ollama notes.

The Compose stack in this repo serves the backend only. `http://localhost:8080` is the API and ops surface, not the frontend SPA root. Run the Vite frontend separately from `web/` when you need the browser UI.

If the app logs a schema version mismatch on startup, that failure is intentional and happens before the rest of the runtime boots. Apply migrations, then restart the process or container; migrations applied after process start require a fresh restart.

## Development Setup (Docker Compose)

Docker Compose brings up three services with hot-reload enabled for the Go backend:

| Service    | Port | Description                          |
|------------|------|--------------------------------------|
| `app`      | 8080 | Go application with Air hot-reload   |
| `postgres` | 5432 | PostgreSQL 17 database               |
| `redis`    | 6379 | Redis 7 cache                        |

### Common Commands

```bash
# Start services in the background
docker compose up -d --build

# Or use the task runner
task dev

# View logs
docker compose logs -f        # all services
task dev:logs                  # shortcut

# Run database migrations explicitly
task migrate:up

# Restart app after migrations if startup failed on schema mismatch
docker compose restart app

# Open a PostgreSQL shell (default Compose user is postgres)
docker compose exec postgres psql -U postgres -d tradingagent

# Stop services
docker compose down

# Stop services and wipe database volumes
docker compose down -v
```

### Agent Workspace

If you use the shared `~/.agents` hub, this repo has a local launcher for the standard tmux workspace:

```bash
task workspace
```

That opens the standard window layout:

- `edit`
- `deck`
- `claude`
- `opencode`
- `db`
- `ops`

Alternate Agent Deck profiles:

```bash
task workspace:research
task workspace:review
task workspace:ops
```

You can also override the profile directly or pick a custom tmux session name:

```bash
AGENT_DECK_PROFILE=opencode-research ./scripts/workspace.sh research
```

### Production Compose Verification

To verify the production image and `docker-compose.prod.yml` end-to-end, run:

```bash
./scripts/verify-prod-build.sh
```

The script builds the production image, starts `docker-compose.prod.yml`, waits for PostgreSQL, applies migrations, asserts the expected schema version, verifies `GET /healthz` returns `{"status":"all-ok"}`, and checks an authenticated `GET /api/v1/strategies` request against the running stack.

### Build, Test & Lint

The project uses [Task](https://taskfile.dev) as its task runner. Install Task, then:

```bash
task build                   # Compile binary to ./bin/tradingagent
task test                    # Unit tests (short mode)
task test:race               # Unit tests with race detector
task test:integration        # Integration tests (requires PostgreSQL)
task lint                    # golangci-lint
task fmt                     # Format with gofumpt
task check                   # Pre-push: build + test + lint
task ci                      # Full CI pipeline locally
```

Run `task --list` for the complete list of available tasks.

> For a detailed walkthrough of native (non-Docker) development, database migrations, tool installation, and troubleshooting, see **[docs/development-setup.md](docs/development-setup.md)**.

## Configuration Reference

All configuration is managed via environment variables. Copy `.env.example` to `.env` and edit as needed. Key groups:

| Variable                          | Default                              | Description                                   |
|-----------------------------------|--------------------------------------|-----------------------------------------------|
| `APP_ENV`                         | `development`                        | Runtime environment (`development`/`production`) |
| `APP_PORT`                        | `8080`                               | HTTP listen port                              |
| `DATABASE_URL`                    | `postgres://…/tradingagent`          | PostgreSQL connection string                  |
| `REDIS_URL`                       | `redis://redis:6379/0`               | Redis connection string                       |
| `JWT_SECRET`                      | *(required)*                         | Secret for JWT token signing                  |
| **LLM**                          |                                      |                                               |
| `LLM_DEFAULT_PROVIDER`           | `openai`                             | Default LLM provider                          |
| `LLM_DEEP_THINK_MODEL`           | `gpt-5.2`                            | Model for research & risk debates             |
| `LLM_QUICK_THINK_MODEL`          | `gpt-5-mini`                         | Model for analyst phases                      |
| `OPENAI_API_KEY`                  | —                                    | OpenAI API key                                |
| **Brokers**                      |                                      |                                               |
| `ALPACA_API_KEY` / `_API_SECRET`  | —                                    | Alpaca credentials                            |
| `ALPACA_PAPER_MODE`              | `true`                               | Use Alpaca paper trading                      |
| `BINANCE_API_KEY` / `_API_SECRET` | —                                    | Binance credentials                           |
| `BINANCE_PAPER_MODE`            | `true`                               | Use Binance testnet                           |
| **Risk**                         |                                      |                                               |
| `RISK_MAX_POSITION_SIZE_PCT`     | `0.10`                               | Max single-position size (% of portfolio)     |
| `RISK_MAX_DAILY_LOSS_PCT`        | `0.02`                               | Max daily loss before circuit breaker          |
| `RISK_MAX_DRAWDOWN_PCT`          | `0.10`                               | Max drawdown before circuit breaker            |
| **Feature Flags**                |                                      |                                               |
| `ENABLE_LIVE_TRADING`            | `false`                              | Enable live order execution                   |
| `ENABLE_SCHEDULER`               | `false`                              | Enable cron-based strategy scheduler          |
| `ENABLE_AGENT_MEMORY`            | `true`                               | Enable agent memory system                    |

See [`.env.example`](.env.example) for the full list of variables including all supported LLM providers and data-source API keys.

## API Overview

The REST API is served under `/api/v1`. Public HTTP endpoints are `GET /healthz`, `GET /health`, `GET /metrics`, `POST /api/v1/auth/login`, and `POST /api/v1/auth/refresh`. The WebSocket endpoint is `GET /ws`; it authenticates the upgrade request before switching protocols and accepts `Authorization: Bearer`, `X-API-Key`, `?token=<jwt>`, or `?api_key=<key>` credentials. Backend root `/` is not the frontend SPA in the current Compose or production stack.

All other `/api/v1/*` routes require either `Authorization: Bearer <jwt>` or `X-API-Key: <api_key>`. Implemented route groups include strategies, runs, portfolio, orders, trades, memories, risk, settings, events, conversations, audit log, and automation health/status.

For the canonical route list, request/response examples, and WebSocket command format, see [`docs/reference/api.md`](docs/reference/api.md).

## CLI

The `tradingagent` binary provides a Cobra CLI with the following subcommands:

```
tradingagent serve        # Start the API server
tradingagent run          # Trigger a one-off strategy run
tradingagent strategies   # Manage strategies
tradingagent portfolio    # View portfolio & positions
tradingagent risk         # Inspect risk engine status
tradingagent memories     # Browse agent memories
tradingagent dashboard    # Interactive terminal dashboard (Bubble Tea TUI)
```

Run `tradingagent --help` for full usage details.

## Project Structure

```
cmd/tradingagent/       Entry point — CLI bootstrap
internal/
  agent/                Trading agent pipeline, phase executors, debate system
  api/                  REST API server, WebSocket hub, middleware
  backtest/             Backtesting engine
  cli/                  Cobra commands and Bubble Tea TUI
  config/               Configuration loading
  data/                 Market data providers (Alpha Vantage, Polygon, Yahoo, Binance)
  domain/               Domain models (Strategy, Order, Position, etc.)
  execution/            Broker adapters (Alpaca, Binance, Polymarket)
  llm/                  LLM provider abstraction
  memory/               Agent memory with PostgreSQL full-text search
  repository/           Data access layer (PostgreSQL repositories)
  risk/                 Risk management engine, circuit breakers, kill switch
  scheduler/            Cron-based strategy scheduler
migrations/             SQL migration files (golang-migrate)
web/                    Frontend application (TypeScript/Vite/React)
docs/                   Architecture docs, ADRs, research
```

## Documentation

- **[Documentation Hub](docs/README.md)** — Canonical entry point for all app documentation
- **[Getting Started](docs/getting-started.md)** — Fastest path from clone to first login, first strategy, and first run
- **[Development Setup](docs/development-setup.md)** — Full contributor workflow, migrations, testing, and smoke mode
- **[Reference](docs/reference/README.md)** — Source-of-truth API, CLI, architecture, runtime, config, and UI docs
- **[Runbooks](docs/runbooks/README.md)** — Incident and operator procedures
- **[Known Issues](docs/known-issues.md)** — Current gaps and repo-health caveats
- **[Roadmap](docs/roadmap.md)** — Proposed future work and product direction
- **[ADRs](docs/adr/README.md)** — Architecture Decision Records
- **[Research Archive](docs/research/index.md)** — Background research that informed the system
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Branch strategy, commit conventions, and definition of done

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for branch strategy, commit conventions, and the definition of done.
