---
title: "Architecture Reference"
description: "Current architecture of the get-rich-quick codebase, including runtime flow, packages, and major interfaces."
status: "canonical"
updated: "2026-04-03"
tags: [architecture, reference]
---

# Architecture Reference

This document describes the architecture that is actually present in the repository today. Where older design material and current code disagree, this file should win.

## Project structure

The repository uses a conventional Go layout:

- `cmd/tradingagent` contains the main binary entry point and runtime wiring.
- `internal/...` contains the application packages.
- `web/` contains the React frontend.
- `migrations/` contains SQL migrations.

### Entry point

| Path | Purpose |
| --- | --- |
| `cmd/tradingagent` | main binary, Cobra CLI bootstrap, runtime assembly, strategy runner selection, docs tests |

### Core packages

| Package | Purpose |
| --- | --- |
| `internal/agent` | runtime orchestration, prompt selection, config resolution, runner logic |
| `internal/agent/analysts` | analyst agent implementations |
| `internal/agent/debate` | reusable debate mechanics |
| `internal/agent/risk` | risk-debate role implementations |
| `internal/agent/trader` | trade-plan generation |
| `internal/api` | REST server, auth, handlers, WebSocket hub, settings surface |
| `internal/backtest` | backtest engine and analytics |
| `internal/cli` | Cobra commands and API-backed terminal workflows |
| `internal/cli/tui` | Bubble Tea dashboard |
| `internal/config` | environment loading and validation |
| `internal/data` | provider abstraction, chains, caching, historical downloads |
| `internal/data/alphavantage` | Alpha Vantage integration |
| `internal/data/binance` | Binance market data integration |
| `internal/data/newsapi` | News API integration package |
| `internal/data/polygon` | Polygon integration |
| `internal/data/yahoo` | Yahoo Finance integration |
| `internal/domain` | core domain types for strategies, runs, signals, orders, positions, trades, users |
| `internal/execution` | broker abstraction and order-management helpers |
| `internal/execution/alpaca` | Alpaca adapter |
| `internal/execution/binance` | Binance adapter |
| `internal/execution/paper` | local paper broker |
| `internal/execution/polymarket` | Polymarket adapter package |
| `internal/llm` | provider abstraction and common LLM primitives |
| `internal/llm/anthropic` | Anthropic adapter |
| `internal/llm/google` | Google adapter |
| `internal/llm/ollama` | Ollama adapter |
| `internal/llm/openai` | OpenAI-compatible adapter used directly and as the basis for some compatible providers |
| `internal/llm/parse` | LLM parsing helpers |
| `internal/memory` | agent memory logic |
| `internal/notification` | outbound notifications and alert fan-out |
| `internal/papervalidation` | paper-trading validation helpers |
| `internal/registry` | service assembly helpers |
| `internal/repository` | repository interfaces |
| `internal/repository/postgres` | PostgreSQL repository implementations |
| `internal/risk` | hard risk engine and status model |
| `internal/scheduler` | scheduled strategy and backtest execution |

## Runtime flow

At a high level, the app is a control plane around a strategy execution runtime.

### Surfaces

- REST API for CRUD, runs, risk, settings, memories, and conversations
- WebSocket feed for real-time events
- CLI for local control
- React web UI for operators

### Execution pipeline

The trading runtime moves through these named phases:

```text
analysis -> research_debate -> trading -> risk_debate
```

The practical flow is:

1. Resolve strategy config against system defaults.
2. Load initial state from data providers.
3. Run analyst roles in parallel.
4. Run the research debate and synthesize an investment plan.
5. Generate a trading plan.
6. Run the risk debate and produce the final signal.
7. Apply hard risk checks and execution routing.
8. Persist run artifacts and broadcast events.

## Agent/runtime architecture

There are two important concepts in the repo:

- the legacy `Pipeline` abstraction in `internal/agent`
- the newer runtime path centered on `agent.Runner` and the production strategy runner under `cmd/tradingagent`

For real strategy execution, the current code path is the runtime runner wired in `cmd/tradingagent/runtime.go` and `cmd/tradingagent/prod_strategy_runner.go`.

### Agent roster

Implemented runtime roles include:

- market analyst
- fundamentals analyst
- news analyst
- social media analyst
- bull researcher
- bear researcher
- research manager / judge
- trader
- aggressive analyst
- conservative analyst
- neutral analyst
- risk manager

### Config resolution

The runtime merges:

1. strategy-level overrides
2. global settings surface
3. hardcoded defaults

This merge happens in `internal/agent/resolve_config.go`.

## Data Flow

The system’s data flow is:

```text
External APIs / brokers
  -> internal/data provider chains
  -> initial runtime state
  -> agent decisions and run artifacts
  -> repository layer / Postgres
  -> API / WebSocket
  -> web UI and CLI
```

More concretely:

```text
OHLCV, fundamentals, news, social
  -> analyst phase
  -> research_debate
  -> trading
  -> risk_debate
  -> signal / orders / positions / audit records
```

## Persistence model

The repository layer supports durable storage for:

- strategies
- backtest configs and runs
- pipeline runs
- pipeline run snapshots
- agent decisions
- agent events
- conversations and messages
- orders
- positions
- trades
- memories
- market-data cache
- historical OHLCV
- audit log
- API keys
- users

Most of the concrete production implementations live under `internal/repository/postgres`.

## API/server architecture

`internal/api/server.go` assembles:

- dependency repositories
- auth manager
- risk engine
- settings service
- manual strategy runner
- WebSocket hub
- middleware stack

Public endpoints:

- `/healthz`
- `/health`
- `/metrics`
- `/api/v1/auth/login`
- `/api/v1/auth/refresh`
- `/ws`

Protected endpoints are mounted under `/api/v1` with auth middleware.

## Scheduler architecture

`internal/scheduler` provides cron-style orchestration for:

- scheduled strategy runs
- scheduled backtests
- in-flight deduplication and stop behavior during shutdown

The scheduler is optional and controlled by feature flags and runtime wiring.

## Risk architecture

There are two separate layers of “risk” in the system:

1. model-driven risk debate inside the agent runtime
2. hard controls in `internal/risk`

The hard risk engine is authoritative for safety controls such as:

- kill switch
- circuit breaker
- max position limits
- market exposure limits
- concurrent position caps

## Key interfaces

### Node

The conceptual building block for agent execution lives in `internal/agent/node.go`.

```go
type Node interface {
    Name() string
    Role() AgentRole
    Phase() Phase
    Execute(ctx context.Context, state *PipelineState) error
}
```

### DataProvider

`internal/data/provider.go` defines the shared market-data contract:

```go
type DataProvider interface {
    GetOHLCV(ctx context.Context, ticker string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error)
    GetFundamentals(ctx context.Context, ticker string) (Fundamentals, error)
    GetNews(ctx context.Context, ticker string, from, to time.Time) ([]NewsArticle, error)
    GetSocialSentiment(ctx context.Context, ticker string, from, to time.Time) ([]SocialSentiment, error)
}
```

### Broker

`internal/execution/broker.go` defines the execution contract:

```go
type Broker interface {
    SubmitOrder(ctx context.Context, order *domain.Order) (externalID string, err error)
    CancelOrder(ctx context.Context, externalID string) error
    GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error)
    GetPositions(ctx context.Context) ([]domain.Position, error)
    GetAccountBalance(ctx context.Context) (Balance, error)
}
```

### RiskEngine

`internal/risk/engine.go` defines the hard safety-control contract:

```go
type RiskEngine interface {
    CheckPreTrade(ctx context.Context, order *domain.Order, portfolio Portfolio) (approved bool, reason string, err error)
    CheckPositionLimits(ctx context.Context, ticker string, quantity float64, portfolio Portfolio) (approved bool, reason string, err error)
    GetStatus(ctx context.Context) (EngineStatus, error)
    TripCircuitBreaker(ctx context.Context, reason string) error
    ResetCircuitBreaker(ctx context.Context) error
    IsKillSwitchActive(ctx context.Context) (bool, error)
    ActivateKillSwitch(ctx context.Context, reason string) error
    DeactivateKillSwitch(ctx context.Context) error
    UpdateMetrics(ctx context.Context, dailyPnL, totalDrawdown float64, consecutiveLosses int) error
}
```

## Important implementation notes

- The production runner supports OpenAI, Anthropic, Google, OpenRouter, xAI, and Ollama.
- `openrouter` and `xai` are handled through OpenAI-compatible transport wiring in the runtime.
- The settings service used by the API/UI persists through the `app_settings` table when the settings persister is wired; fallback in-memory behavior is only for degraded/local scenarios.
- The repository includes backtest support, but the main API server does not yet expose a full public backtest surface.
- Some files in the repo currently contain merge-conflict markers; treat repo-health as a real architecture constraint until they are resolved.
