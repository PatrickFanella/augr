# Architecture Reference

This document describes the architecture of the Go codebase for the
multi-agent AI trading system.

## Project Structure

The repository follows standard Go project layout conventions. Application
entry points live under `cmd/` and all internal packages live under
`internal/`.

### Entry Point

| Path | Purpose |
|------|---------|
| `cmd/tradingagent/` | Main binary. Wires CLI, API server, and TUI dependencies. |

### Core Packages

| Package | Import Path | Purpose |
|---------|-------------|---------|
| agent | `internal/agent` | Pipeline orchestration engine; defines `Node` interface and executes the four pipeline phases. |
| agent/analysts | `internal/agent/analysts` | Analyst agent implementations (market, fundamentals, news, social). |
| agent/debate | `internal/agent/debate` | Debate round execution logic shared by research and risk debate phases. |
| agent/risk | `internal/agent/risk` | Risk debate agents (aggressive, conservative, neutral). |
| agent/trader | `internal/agent/trader` | Trader agent for position sizing and order generation. |
| api | `internal/api` | HTTP handlers, middleware, and WebSocket hub. |
| backtest | `internal/backtest` | Historical backtesting engine. |
| cli | `internal/cli` | Cobra CLI commands. |
| cli/tui | `internal/cli/tui` | Bubble Tea terminal UI. |
| config | `internal/config` | Application configuration loading and validation. |
| data | `internal/data` | `DataProvider` interface and provider chain. |
| data/alphavantage | `internal/data/alphavantage` | Alpha Vantage market data provider. |
| data/binance | `internal/data/binance` | Binance crypto data provider. |
| data/newsapi | `internal/data/newsapi` | News API provider. |
| data/polygon | `internal/data/polygon` | Polygon.io stock data provider. |
| data/yahoo | `internal/data/yahoo` | Yahoo Finance data provider. |
| domain | `internal/domain` | Core domain types (Order, Position, Strategy, Pipeline, AgentRole, Phase). |
| execution | `internal/execution` | `Broker` interface for order execution. |
| execution/alpaca | `internal/execution/alpaca` | Alpaca securities broker. |
| execution/binance | `internal/execution/binance` | Binance exchange execution. |
| execution/paper | `internal/execution/paper` | Paper trading simulator. |
| execution/polymarket | `internal/execution/polymarket` | Polymarket prediction market execution. |
| llm | `internal/llm` | LLM provider abstraction, registry, and routing. |
| llm/anthropic | `internal/llm/anthropic` | Anthropic Claude adapter. |
| llm/google | `internal/llm/google` | Google Gemini adapter. |
| llm/ollama | `internal/llm/ollama` | Ollama local LLM adapter. |
| llm/openai | `internal/llm/openai` | OpenAI adapter. |
| llm/parse | `internal/llm/parse` | LLM response parsing utilities. |
| memory | `internal/memory` | Agent memory and learning backed by PostgreSQL full-text search. |
| notification | `internal/notification` | Alert notifications (email, Telegram, webhook). |
| papervalidation | `internal/papervalidation` | Paper trading validation. |
| registry | `internal/registry` | Service registry and dependency injection. |
| repository | `internal/repository` | Data access interfaces. |
| repository/postgres | `internal/repository/postgres` | PostgreSQL repository implementations. |
| risk | `internal/risk` | `RiskEngine` interface and hard risk controls. |
| scheduler | `internal/scheduler` | Cron-based pipeline triggering. |

## Pipeline Execution Flow

The agent pipeline runs in four sequential phases. Each phase is defined by a
`Phase` constant in `internal/domain`:

```text
analysis → research_debate → trading → risk_debate
```

### Phase 1 — Analysis

Four analyst nodes run concurrently using an `errgroup`:

- **MarketAnalyst** — analyses price action and technical indicators.
- **FundamentalsAnalyst** — evaluates company fundamentals.
- **NewsAnalyst** — summarises relevant news articles.
- **SocialMediaAnalyst** — assesses social-media sentiment.

Each analyst receives market data from `PipelineState` and writes its report
back into `PipelineState.AnalystReports`. Partial failures are tolerated; the
pipeline continues if at least one analyst succeeds.

### Phase 2 — Research Debate

A multi-round debate between a **BullResearcher** and a **BearResearcher**.
After the configured number of rounds (default 3) an **InvestJudge** evaluates
the arguments and produces an investment plan stored in
`PipelineState.ResearchDebate`.

### Phase 3 — Trading

A single **Trader** agent receives the investment plan from Phase 2 and
produces a `TradingPlan` containing position size, entry price, exit price, and
stop-loss level. The plan is validated against the `RiskEngine` before being
written to `PipelineState.TradingPlan`.

### Phase 4 — Risk Debate

Three risk agents — **AggressiveRisk**, **ConservativeRisk**, and
**NeutralRisk** — debate the proposed trading plan. A **RiskManager** judge
evaluates the debate and produces the `FinalSignal` (buy, sell, or hold with a
confidence score). Hard risk controls such as circuit breakers and the kill
switch are enforced at this stage.

### Pipeline Executor

The `Pipeline.Execute` method in `internal/agent` drives the flow:

1. Creates a `domain.PipelineRun` and calls `RecordRunStart`.
2. Initialises `PipelineState` with a mutex for thread-safe access.
3. Runs each phase sequentially; a phase failure stops the pipeline.
4. Records completion or failure via `RecordRunComplete`.
5. Emits `PipelineCompleted` or `PipelineFailed` events over the event channel.

Phase and pipeline timeouts are configured through `PipelineConfig`.

## Data Flow

### Market Data Ingestion

External market data APIs are accessed through the `DataProvider` interface
defined in `internal/data`. The `ProviderChain` (also in `internal/data`)
wraps multiple providers and falls back to the next provider when one returns
an error.

```text
External APIs → DataProvider implementations → ProviderChain → PipelineState
```

### Through the Pipeline

```text
PipelineState (market data, news, fundamentals, social sentiment)
  │
  ├─ Phase 1: Analyst nodes read state, write AnalystReports
  │
  ├─ Phase 2: Debaters read AnalystReports, judge writes ResearchDebate
  │
  ├─ Phase 3: Trader reads ResearchDebate, RiskEngine validates, writes TradingPlan
  │
  └─ Phase 4: Risk agents read TradingPlan, judge writes FinalSignal
```

### Order Execution

The `FinalSignal` drives order creation. Orders are submitted through the
`Broker` interface in `internal/execution`, which routes to the configured
broker implementation (Alpaca, Binance, paper, or Polymarket).

### Persistence

Pipeline runs, agent decisions, and events are persisted through the
`DecisionPersister` interface in `internal/agent`. The default implementation
writes to PostgreSQL via `internal/repository/postgres`.

### Real-Time Events

The pipeline emits events (agent started, decision made, debate round
completed, pipeline completed/failed) over an event channel. The WebSocket hub
in `internal/api` broadcasts these events to connected clients such as the TUI
and web dashboard.

## Key Interfaces

### Node

Defined in `internal/agent/node.go`. Every agent in the pipeline implements
this interface.

```go
type Node interface {
    Name() string
    Role() AgentRole
    Phase() Phase
    Execute(ctx context.Context, state *PipelineState) error
}
```

Specialised optional interfaces extend `Node` for typed input/output:

- `AnalystNode` — adds `Analyze(ctx, AnalysisInput) (AnalysisOutput, error)`
- `DebaterNode` — adds `Debate(ctx, DebateInput) (DebateOutput, error)`
- `TraderNode` — adds `Trade(ctx, TradingInput) (TradingOutput, error)`
- `RiskJudgeNode` — adds `JudgeRisk(ctx, RiskJudgeInput) (RiskJudgeOutput, error)`

### DataProvider

Defined in `internal/data/provider.go`. Abstracts market data retrieval.

```go
type DataProvider interface {
    GetOHLCV(ctx context.Context, ticker string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error)
    GetFundamentals(ctx context.Context, ticker string) (Fundamentals, error)
    GetNews(ctx context.Context, ticker string, from, to time.Time) ([]NewsArticle, error)
    GetSocialSentiment(ctx context.Context, ticker string, from, to time.Time) ([]SocialSentiment, error)
}
```

Implementations: `internal/data/polygon`, `internal/data/alphavantage`,
`internal/data/yahoo`, `internal/data/binance`. The `ProviderChain` in
`internal/data/chain.go` provides ordered fallback across providers.

### Broker

Defined in `internal/execution/broker.go`. Market-agnostic order execution
contract.

```go
type Broker interface {
    SubmitOrder(ctx context.Context, order *domain.Order) (externalID string, err error)
    CancelOrder(ctx context.Context, externalID string) error
    GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error)
    GetPositions(ctx context.Context) ([]domain.Position, error)
    GetAccountBalance(ctx context.Context) (Balance, error)
}
```

Implementations: `internal/execution/alpaca`, `internal/execution/binance`,
`internal/execution/paper`, `internal/execution/polymarket`.

### RiskEngine

Defined in `internal/risk/engine.go`. Hard risk controls enforced
independently of model-driven analysis.

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

Implementation: `internal/risk/engine_impl.go`.
