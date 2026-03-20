---
title: "Go Project Structure"
date: 2026-03-20
tags: [backend, go, project-structure, organization]
---

# Go Project Structure

Standard Go project layout following `golang-standards/project-layout` conventions.

## Directory Layout

```
trading-agent/
├── cmd/
│   └── tradingagent/
│       └── main.go                    # Entry point — cobra root + serve/TUI
│
├── internal/
│   ├── api/
│   │   ├── handler/                   # HTTP handlers (strategies, runs, portfolio, risk)
│   │   ├── middleware/                # Auth, logging, CORS, rate limiting
│   │   ├── router.go                  # chi router setup
│   │   └── websocket/                # WebSocket hub and client management
│   │
│   ├── agent/
│   │   ├── orchestrator.go           # DAG pipeline engine
│   │   ├── node.go                    # Agent node interface and base implementation
│   │   ├── state.go                   # Pipeline state (AgentState, DebateState)
│   │   ├── analyst/
│   │   │   ├── market.go             # Market analyst agent
│   │   │   ├── fundamentals.go       # Fundamentals analyst agent
│   │   │   ├── news.go               # News analyst agent
│   │   │   └── social.go            # Social media analyst agent
│   │   ├── research/
│   │   │   ├── bull.go               # Bull researcher
│   │   │   ├── bear.go               # Bear researcher
│   │   │   └── manager.go           # Research manager (judge)
│   │   ├── trader/
│   │   │   └── trader.go            # Trader agent
│   │   ├── risk/
│   │   │   ├── aggressive.go        # Aggressive risk analyst
│   │   │   ├── conservative.go      # Conservative risk analyst
│   │   │   ├── neutral.go           # Neutral risk analyst
│   │   │   └── manager.go          # Risk manager (final judge)
│   │   └── signal/
│   │       └── extractor.go         # Signal extraction from risk manager output
│   │
│   ├── llm/
│   │   ├── provider.go               # Provider interface
│   │   ├── factory.go                # Factory: create provider by name
│   │   ├── openai.go                 # OpenAI adapter
│   │   ├── anthropic.go              # Anthropic adapter
│   │   ├── google.go                 # Google/Gemini adapter
│   │   ├── ollama.go                 # Ollama (local) adapter
│   │   └── message.go               # Shared message types
│   │
│   ├── data/
│   │   ├── provider.go               # DataProvider interface
│   │   ├── polygon.go                # Polygon.io implementation
│   │   ├── alphavantage.go          # Alpha Vantage implementation
│   │   ├── yahoo.go                  # Yahoo Finance implementation
│   │   ├── news.go                   # News aggregation (NewsAPI, etc.)
│   │   ├── indicators.go            # Technical indicator calculations
│   │   └── cache.go                 # Market data caching layer
│   │
│   ├── execution/
│   │   ├── broker.go                 # Broker interface
│   │   ├── alpaca.go                 # Alpaca adapter
│   │   ├── binance.go               # Binance adapter
│   │   ├── polymarket.go            # Polymarket adapter
│   │   ├── paper.go                  # Paper trading simulator
│   │   └── position.go             # Position tracking
│   │
│   ├── risk/
│   │   ├── engine.go                 # Risk engine — evaluates limits
│   │   ├── circuit_breaker.go       # Circuit breaker implementations
│   │   ├── kill_switch.go           # Kill switch (API + file + env)
│   │   └── sizing.go               # Position sizing (ATR, Kelly, fixed fractional)
│   │
│   ├── memory/
│   │   ├── store.go                  # Memory store interface
│   │   ├── postgres.go              # PostgreSQL FTS-based memory
│   │   └── reflection.go           # Outcome reflection and memory generation
│   │
│   ├── domain/
│   │   ├── strategy.go              # Strategy entity
│   │   ├── order.go                 # Order entity
│   │   ├── position.go             # Position entity
│   │   ├── trade.go                # Trade entity
│   │   ├── pipeline_run.go         # PipelineRun entity
│   │   └── agent_decision.go       # AgentDecision entity
│   │
│   ├── repository/
│   │   ├── strategy.go              # Strategy CRUD
│   │   ├── order.go                 # Order CRUD
│   │   ├── position.go             # Position CRUD
│   │   ├── pipeline_run.go         # PipelineRun CRUD
│   │   ├── agent_decision.go       # AgentDecision CRUD
│   │   ├── market_data.go          # Market data cache
│   │   └── audit.go               # Audit log
│   │
│   ├── scheduler/
│   │   └── scheduler.go            # Cron-based pipeline trigger
│   │
│   ├── cli/                          # Bubble Tea TUI (see [[cli-interface]])
│   │   ├── cmd/                     # cobra commands (root, run, serve, risk, etc.)
│   │   ├── dashboard/               # TUI views (portfolio, strategies, pipeline, risk)
│   │   ├── theme/                   # Lipgloss styles and colors
│   │   └── api/                     # HTTP/WS client for backend
│   │
│   └── config/
│       └── config.go                # Application configuration
│
├── migrations/
│   ├── 000001_create_strategies.up.sql
│   ├── 000001_create_strategies.down.sql
│   └── ...
│
├── web/                               # React frontend (embedded or separate)
│   ├── src/
│   ├── package.json
│   └── vite.config.ts
│
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
└── .env.example
```

## Key Design Decisions

### `internal/` Over `pkg/`

All application code lives under `internal/` — this is not a library. If we later need shared packages, we extract to `pkg/`.

### Domain-Driven Boundaries

```
internal/
├── agent/       ← Agent orchestration domain
├── llm/         ← LLM provider abstraction
├── data/        ← Market data domain
├── execution/   ← Order execution domain
├── risk/        ← Risk management domain
├── memory/      ← Agent memory domain
├── domain/      ← Shared entity definitions
├── repository/  ← Database access
└── api/         ← HTTP presentation layer
```

Each domain depends on `domain/` for entity types and `repository/` for persistence, but domains do not import each other directly. Cross-domain communication flows through the orchestrator.

### Interface-Driven Design

Every external dependency is behind an interface:

```go
// internal/llm/provider.go
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// internal/data/provider.go
type MarketDataProvider interface {
    GetOHLCV(ctx context.Context, ticker string, from, to time.Time) ([]OHLCV, error)
    GetFundamentals(ctx context.Context, ticker string) (*Fundamentals, error)
    GetNews(ctx context.Context, ticker string, from, to time.Time) ([]NewsItem, error)
}

// internal/execution/broker.go
type Broker interface {
    SubmitOrder(ctx context.Context, order Order) (string, error)
    GetOrderStatus(ctx context.Context, orderID string) (OrderStatus, error)
    CancelOrder(ctx context.Context, orderID string) error
    GetPositions(ctx context.Context) ([]Position, error)
}
```

This allows:

- Unit testing with mocks
- Swapping providers without changing business logic
- Paper trading as a `Broker` implementation

### Makefile Targets

```makefile
.PHONY: build run test migrate-up migrate-down generate lint

build:
    go build -o bin/server ./cmd/server

run:
    air  # live reload

test:
    go test ./... -race -cover

migrate-up:
    migrate -database $(DATABASE_URL) -path migrations up

migrate-down:
    migrate -database $(DATABASE_URL) -path migrations down 1

generate:
    sqlc generate

lint:
    golangci-lint run ./...

docker-up:
    docker compose up -d

docker-down:
    docker compose down
```

---

**Related:** [[technology-stack]] · [[agent-orchestration-engine]] · [[database-schema]]
