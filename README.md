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

> **Prerequisites:** [Docker](https://docs.docker.com/get-docker/) and [Docker Compose v2+](https://docs.docker.com/compose/install/)

```bash
# 1. Clone the repository
git clone https://github.com/PatrickFanella/get-rich-quick.git
cd get-rich-quick

# 2. Copy the example environment file and add your API keys
cp .env.example .env

# 3. Start all services (app + PostgreSQL + Redis)
docker compose up --build
```

The application will be available at [http://localhost:8080](http://localhost:8080). See the [Development Setup Guide](docs/development-setup.md) for native (non-Docker) setup and advanced configuration.

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

# Run database migrations (set DB_URL to match Docker Compose credentials)
DB_URL="postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable" task migrate:up

# Open a PostgreSQL shell (default Compose user is postgres)
docker compose exec postgres psql -U postgres -d tradingagent

# Stop services
docker compose down

# Stop services and wipe database volumes
docker compose down -v
```

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

The REST API is served under `/api/v1` and requires JWT authentication. A WebSocket endpoint provides real-time event streaming. The table below lists the primary endpoints; additional monitoring routes (`GET /health`, `GET /metrics`) are also available.

| Method   | Endpoint                            | Description                     |
|----------|-------------------------------------|---------------------------------|
| `GET`    | `/healthz`                          | Health check                    |
| `GET`    | `/health`                           | Liveness probe (monitoring)     |
| `GET`    | `/metrics`                          | Metrics endpoint (monitoring)   |
| `GET`    | `/ws`                               | WebSocket event stream          |
| **Strategies** |                               |                                 |
| `GET`    | `/api/v1/strategies`                | List strategies                 |
| `POST`   | `/api/v1/strategies`                | Create a strategy               |
| `GET`    | `/api/v1/strategies/{id}`           | Get strategy details            |
| `PUT`    | `/api/v1/strategies/{id}`           | Update a strategy               |
| `DELETE` | `/api/v1/strategies/{id}`           | Delete a strategy               |
| `POST`   | `/api/v1/strategies/{id}/run`       | Trigger a pipeline run          |
| **Pipeline Runs** |                            |                                 |
| `GET`    | `/api/v1/runs`                      | List pipeline runs              |
| `GET`    | `/api/v1/runs/{id}`                 | Get run details                 |
| `GET`    | `/api/v1/runs/{id}/decisions`       | Get agent decisions for a run   |
| `POST`   | `/api/v1/runs/{id}/cancel`          | Cancel a running pipeline       |
| **Portfolio** |                                 |                                 |
| `GET`    | `/api/v1/portfolio/positions`       | List all positions              |
| `GET`    | `/api/v1/portfolio/positions/open`  | List open positions             |
| `GET`    | `/api/v1/portfolio/summary`         | Portfolio summary               |
| **Orders & Trades** |                          |                                 |
| `GET`    | `/api/v1/orders`                    | List orders                     |
| `GET`    | `/api/v1/orders/{id}`               | Get order details               |
| `GET`    | `/api/v1/trades`                    | List trades                     |
| **Memories** |                                  |                                 |
| `GET`    | `/api/v1/memories`                  | List agent memories             |
| `POST`   | `/api/v1/memories/search`           | Search memories (full-text)     |
| `DELETE` | `/api/v1/memories/{id}`             | Delete a memory                 |
| **Risk & Settings** |                          |                                 |
| `GET`    | `/api/v1/risk/status`               | Risk engine status              |
| `POST`   | `/api/v1/risk/killswitch`           | Toggle kill switch              |
| `GET`    | `/api/v1/settings`                  | Get runtime settings            |
| `PUT`    | `/api/v1/settings`                  | Update runtime settings         |

### WebSocket Protocol

Connect to `/ws` and send JSON commands:

```jsonc
{ "action": "subscribe",   "strategy_ids": ["strategy-uuid-1"], "run_ids": ["run-uuid-1"] }
{ "action": "unsubscribe", "strategy_ids": ["strategy-uuid-1"], "run_ids": ["run-uuid-1"] }
{ "action": "subscribe_all" }
{ "action": "unsubscribe_all" }
```

The server streams `WSMessage` envelopes with the following fields: `type`, `strategy_id`, `run_id`, `data`, and `timestamp`.

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

- **[Development Setup Guide](docs/development-setup.md)** — Detailed native setup, database migrations, and troubleshooting
- **[Architecture & Design](docs/design/system-architecture.md)** — System architecture and data flow
- **[ADRs](docs/adr/)** — Architecture Decision Records
- **[Agent Execution Guide](docs/agent-execution-guide.md)** — How agents pick up and complete work
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Branch strategy, commit conventions, and definition of done

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for branch strategy, commit conventions, and the definition of done.
