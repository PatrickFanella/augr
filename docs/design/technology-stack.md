---
title: "Technology Stack"
date: 2026-03-20
tags: [stack, go, typescript, react, postgresql]
---

# Technology Stack

## Backend — Go

| Component      | Library / Tool             | Purpose                                         |
| -------------- | -------------------------- | ----------------------------------------------- |
| HTTP Server    | `net/http` + `chi` router  | REST API, middleware                            |
| WebSocket      | `nhooyr.io/websocket`      | Real-time streaming                             |
| Database       | `pgx` (v5)                 | PostgreSQL driver (pure Go, connection pooling) |
| Migrations     | `golang-migrate/migrate`   | Schema versioning                               |
| ORM (optional) | `sqlc`                     | Type-safe SQL → Go code generation              |
| Config         | `viper`                    | Environment/file-based configuration            |
| Logging        | `slog` (stdlib)            | Structured logging                              |
| Scheduling     | `robfig/cron`              | Periodic pipeline triggers                      |
| HTTP Client    | `net/http`                 | LLM API calls, market data fetching             |
| Testing        | `testing` + `testify`      | Unit and integration tests                      |
| Concurrency    | `errgroup`, channels       | Parallel agent execution                        |
| JSON           | `encoding/json`            | API serialization                               |
| Metrics        | `prometheus/client_golang` | Prometheus metrics export                       |

### Why Go

- **Concurrency** — Goroutines allow parallel analyst execution, concurrent data fetching, and WebSocket fan-out with minimal overhead
- **Performance** — Compiled binary with low memory footprint; suitable for 24/7 crypto operation
- **Single binary deployment** — No runtime dependencies; simplifies Docker images
- **Strong typing** — Interfaces enforce agent contracts at compile time
- **Mature HTTP ecosystem** — First-class HTTP client/server for LLM API integration

### Where Python May Be Used

Python is avoided except where Go alternatives are significantly inferior:

| Use Case                   | Justification                                   | Go Alternative Considered                  |
| -------------------------- | ----------------------------------------------- | ------------------------------------------ |
| FinBERT inference          | Pre-trained HuggingFace model; no Go equivalent | Would require CGo + ONNX runtime — fragile |
| Prototyping new indicators | Rapid iteration with pandas/numpy               | Final indicators implemented in Go         |

Python components communicate with the Go backend via HTTP microservice (not embedded).

## Frontend — TypeScript / React

| Component     | Library                        | Purpose                                  |
| ------------- | ------------------------------ | ---------------------------------------- |
| Framework     | React 19 + Vite                | SPA with fast HMR                        |
| Routing       | React Router v7                | Client-side navigation                   |
| State         | TanStack Query                 | Server state + caching                   |
| Charts        | Recharts or Lightweight Charts | Price charts, portfolio performance      |
| UI Components | shadcn/ui + Tailwind CSS 4     | Consistent, accessible component library |
| WebSocket     | Native `WebSocket` API         | Real-time agent events                   |
| Forms         | React Hook Form + Zod          | Strategy configuration forms             |
| Tables        | TanStack Table                 | Trade history, agent decisions           |

### Why React + TypeScript

- Largest ecosystem for financial dashboards and charting
- Type safety catches API contract mismatches at build time
- shadcn/ui provides production-quality components without heavy runtime

## Database — PostgreSQL

| Feature                       | Usage                                                           |
| ----------------------------- | --------------------------------------------------------------- |
| JSONB columns                 | Store flexible agent reports and LLM responses                  |
| Full-text search (`tsvector`) | Agent memory retrieval (replaces BM25)                          |
| Partitioning                  | Time-range partitioning for `market_data` and `agent_decisions` |
| `LISTEN/NOTIFY`               | Real-time change notifications to Go server                     |
| Materialized views            | Pre-aggregated portfolio metrics                                |
| Row-level security            | Multi-user isolation (future)                                   |

### Why PostgreSQL Over Alternatives

- **vs. MongoDB** — ACID transactions critical for trade recording; JSONB covers flexible schema needs
- **vs. TimescaleDB** — Standard PostgreSQL partitioning is sufficient for our data volume; avoids extension dependency
- **vs. SQLite** — Need concurrent writes from multiple goroutines; network-accessible for separate services

## Optional Components

| Component     | Tool           | When to Add                                                                          |
| ------------- | -------------- | ------------------------------------------------------------------------------------ |
| Cache         | Redis          | When market data fetch latency becomes a bottleneck                                  |
| Message queue | NATS           | When scaling to multiple Go workers                                                  |
| Search        | PostgreSQL FTS | Built-in; upgrade to Elasticsearch only if memory corpus exceeds millions of records |

## Development Tools

| Tool              | Purpose                                                 |
| ----------------- | ------------------------------------------------------- |
| Docker Compose    | Local development environment (Go + PostgreSQL + Redis) |
| `air`             | Go live reload during development                       |
| `sqlc`            | Generate type-safe Go from SQL queries                  |
| `oapi-codegen`    | Generate Go server stubs from OpenAPI spec              |
| GitHub Actions    | CI/CD pipeline                                          |
| `golangci-lint`   | Go linting                                              |
| ESLint + Prettier | Frontend linting and formatting                         |

---

**Related:** [[system-architecture]] · [[go-project-structure]] · [[database-schema]]
