---
title: "Deployment & Operations"
date: 2026-03-20
tags: [infrastructure, deployment, docker, monitoring, security, ci-cd]
---

# Deployment & Operations

## Docker Setup

### Multi-Stage Build

```dockerfile
# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/tradingagent ./cmd/server

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/tradingagent /bin/tradingagent
COPY migrations/ /app/migrations/
EXPOSE 8080
ENTRYPOINT ["/bin/tradingagent", "serve"]
```

Final image: ~20MB, no build tools or source code.

### Docker Compose

```yaml
services:
  server:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://trading:trading@postgres:5432/tradingagent?sslmode=disable
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - POLYGON_API_KEY=${POLYGON_API_KEY}
      - ALPACA_API_KEY=${ALPACA_API_KEY}
      - ALPACA_API_SECRET=${ALPACA_API_SECRET}
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: trading
      POSTGRES_PASSWORD: trading
      POSTGRES_DB: tradingagent
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U trading"]
      interval: 5s
      timeout: 3s
      retries: 5

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    profiles:
      - full # only start when explicitly requested

volumes:
  pgdata:
```

## Monitoring & Observability

### Structured Logging

All logs use Go's `slog` with JSON output:

```go
slog.Info("pipeline completed",
    "run_id", run.ID,
    "ticker", run.Ticker,
    "signal", run.Signal,
    "duration_ms", run.Duration.Milliseconds(),
    "llm_calls", run.LLMCallCount,
    "total_tokens", run.TotalTokens,
)
```

Every log line includes a `correlation_id` (the pipeline run ID) for tracing a full request through the system.

### Prometheus Metrics

```go
var (
    pipelineRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tradingagent_pipeline_runs_total",
        Help: "Total number of pipeline runs",
    }, []string{"ticker", "signal", "status"})

    pipelineDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "tradingagent_pipeline_duration_seconds",
        Help:    "Pipeline execution duration",
        Buckets: prometheus.DefBuckets,
    }, []string{"ticker"})

    llmCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tradingagent_llm_calls_total",
        Help: "Total LLM API calls",
    }, []string{"provider", "model", "agent_role"})

    llmTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tradingagent_llm_tokens_total",
        Help: "Total LLM tokens consumed",
    }, []string{"provider", "model", "type"}) // type: prompt, completion

    ordersTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tradingagent_orders_total",
        Help: "Total orders submitted",
    }, []string{"broker", "side", "status"})

    portfolioValue = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "tradingagent_portfolio_value",
        Help: "Current portfolio total value",
    })
)
```

Metrics endpoint: `GET /metrics` (Prometheus-compatible).

### Key Dashboards (Grafana)

| Dashboard           | Panels                                                                  |
| ------------------- | ----------------------------------------------------------------------- |
| **Pipeline Health** | Run count, success rate, latency P50/P95, signal distribution           |
| **LLM Usage**       | Calls by provider, tokens by model, cost estimate, error rate           |
| **Trading**         | Orders by status, fill rate, P&L, position count                        |
| **Risk**            | Circuit breaker state, daily loss, drawdown, kill switch                |
| **System**          | Go runtime (goroutines, heap, GC), PostgreSQL connections, HTTP latency |

### Alerting

| Alert                    | Condition                  | Channel              |
| ------------------------ | -------------------------- | -------------------- |
| Pipeline failure         | 3 consecutive failures     | Telegram + email     |
| Circuit breaker trip     | Any breaker opens          | Telegram (immediate) |
| LLM provider down        | Error rate > 50% for 5 min | Telegram             |
| High latency             | Pipeline > 120s            | Email                |
| Kill switch activated    | Kill switch toggled        | Telegram (immediate) |
| Database connection loss | Connection pool exhausted  | Email + PagerDuty    |

Telegram/Discord webhooks configured per-environment.

## Security

### Secrets Management

- **Development:** `.env` file (gitignored)
- **Production:** Environment variables injected by deployment platform
- Never store API keys in code, config files, or database

### API Security

- JWT tokens with short expiry (1 hour) + refresh tokens
- Rate limiting on all endpoints (100 req/min per client)
- CORS restricted to frontend origin
- Input validation on all endpoints (Zod-like validation in Go)

### Database Security

- Separate database user for the application (no superuser)
- Connection via SSL in production
- Parameterized queries only (sqlc generates safe code)
- Regular backups (pg_dump daily)

### LLM API Keys

- Keys stored only in environment variables
- Different keys for development/production
- Rotate keys quarterly
- Monitor usage dashboards for anomalies

## CI/CD (GitHub Actions)

```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:17
        env:
          POSTGRES_PASSWORD: test
        options: --health-cmd pg_isready
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - run: go test ./... -race -cover
      - run: golangci-lint run

  build:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: docker build -t tradingagent .
```

## Graceful Shutdown

```go
func main() {
    srv := &http.Server{Addr: ":8080", Handler: router}

    go func() {
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("server error", "error", err)
        }
    }()

    // Wait for interrupt signal
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    slog.Info("shutting down gracefully...")

    // Cancel running pipelines gracefully
    orchestrator.CancelAll()

    // Give in-flight requests 30 seconds to complete
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    srv.Shutdown(ctx)
}
```

Running pipelines are given time to finish their current agent node before the process exits.

---

**Related:** [[system-architecture]] · [[technology-stack]] · [[go-project-structure]]
