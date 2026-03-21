---
title: "Implementation Roadmap"
date: 2026-03-20
tags: [roadmap, planning, milestones, phases]
---

# Implementation Roadmap

Six phases, each building on the previous. Every phase ends with a working, testable system.

## Phase 1 — Foundation (Weeks 1–3)

**Goal:** Working Go server with PostgreSQL, basic REST API, and a single analyst agent.

### Tasks

- [ ] Initialize Go module with project structure per [[go-project-structure]]
- [ ] Set up PostgreSQL with Docker Compose
- [ ] Create initial database migrations per [[database-schema]]
- [ ] Implement `chi` router with middleware (logging, CORS, auth stub)
- [ ] Build repository layer for `strategies` and `pipeline_runs` tables
- [ ] Implement [[llm-provider-system]] with OpenAI and Anthropic adapters
- [ ] Build minimal [[agent-orchestration-engine]] — single-node pipeline
- [ ] Implement Market Analyst agent (see [[analyst-agents]])
- [ ] Wire `/strategies` CRUD endpoints
- [ ] Wire `/runs` endpoint that triggers a single-analyst pipeline
- [ ] Write integration tests for full request → LLM → database flow

### Deliverable

`POST /strategies/:id/run` triggers a pipeline that runs the market analyst, stores the report in `agent_decisions`, and returns the result.

---

## Phase 2 — Full Agent Pipeline (Weeks 4–6)

**Goal:** Complete multi-agent pipeline with all analysts, debates, and signal extraction.

### Tasks

- [ ] Implement remaining analysts: Fundamentals, News, Social Media (see [[analyst-agents]])
- [ ] Build [[data-ingestion-pipeline]] with provider abstraction
- [ ] Integrate Polygon.io and Alpha Vantage providers (see [[market-data-providers]])
- [ ] Implement [[technical-indicators]] computation in Go
- [ ] Build parallel analyst execution using `errgroup`
- [ ] Implement Research Debate system — Bull, Bear, Research Manager (see [[research-debate-system]])
- [ ] Implement [[trader-agent]] — trading plan generation
- [ ] Implement Risk Debate — Aggressive, Conservative, Neutral, Risk Manager (see [[risk-management-agents]])
- [ ] Build signal extraction from Risk Manager output
- [ ] Implement conditional routing (debate round control, analyst sequencing)
- [ ] Store all intermediate outputs in `agent_decisions`
- [ ] End-to-end test: full pipeline for a single stock

### Deliverable

Full pipeline: 4 analysts (parallel) → research debate (N rounds) → trader → risk debate (N rounds) → BUY/SELL/HOLD signal. All decisions persisted and queryable.

---

## Phase 3 — Memory, Learning & Risk Controls (Weeks 7–9)

**Goal:** Agents learn from past outcomes; risk controls prevent catastrophic losses.

### Tasks

- [ ] Implement [[memory-and-learning]] with PostgreSQL full-text search
- [ ] Build `reflect_and_remember` flow — outcomes update agent memories
- [ ] Inject relevant memories into agent prompts during pipeline execution
- [ ] Implement [[risk-management-engine]]:
  - Daily loss limit circuit breaker
  - Max drawdown circuit breaker
  - Per-position size limits
  - Kill switch (API-triggered and file-based)
- [ ] Build position sizing calculator (ATR-based, Kelly criterion)
- [ ] Add risk status to REST API
- [ ] Write memory retrieval benchmarks (ensure < 50ms for 10K memories)
- [ ] Integration test: verify circuit breaker halts pipeline correctly

### Deliverable

Agents recall relevant past situations during analysis and debate. Risk controls halt trading when limits are breached. Memory retrieval performs well at scale.

---

## Phase 4 — Execution Layer (Weeks 10–12)

**Goal:** System can execute trades on paper and live accounts.

### Tasks

- [ ] Build [[execution-engine]] with broker adapter interface
- [ ] Implement Alpaca adapter (paper + live) — see [[execution-overview]]
- [ ] Implement crypto exchange adapter (Binance) via REST API
- [ ] Implement Polymarket adapter — CLOB order placement
- [ ] Build [[paper-trading]] engine (simulated fills with realistic slippage)
- [ ] Implement order lifecycle management (submit → partial → filled/cancelled)
- [ ] Build position tracker — update `positions` table on fills
- [ ] Implement P&L calculation (unrealized from market data, realized from fills)
- [ ] Wire execution into pipeline: signal → order → fill → position update
- [ ] Add order and trade endpoints to REST API
- [ ] Implement trade cooldown (configurable delay between trades)

### Deliverable

Pipeline signal triggers order submission via configured broker. Paper trading mode provides realistic simulation. Position and P&L tracking work end-to-end.

---

## Phase 5 — Frontend Dashboard (Weeks 13–16)

**Goal:** React dashboard provides full visibility into agent activity and portfolio.

### Tasks

- [ ] Initialize React + Vite + TypeScript project per [[frontend-overview]]
- [ ] Set up TanStack Query for API integration
- [ ] Build WebSocket connection manager
- [ ] Implement [[websocket-server]] in Go backend
- [ ] Build [[dashboard-design]] — portfolio summary, recent signals, active strategies
- [ ] Build [[agent-visualization]] — real-time pipeline progress, debate flow
- [ ] Build [[portfolio-and-strategy-ui]]:
  - Strategy list with create/edit forms
  - Position table with P&L
  - Trade history with filters
  - Agent decision inspector (full reasoning chain)
- [ ] Implement risk controls panel (circuit breaker status, kill switch toggle)
- [ ] Build strategy configuration wizard
- [ ] Responsive layout for desktop and tablet

### Deliverable

Fully functional dashboard. Users can create strategies, watch agents deliberate in real-time, monitor portfolio, and control risk settings.

---

## Phase 6 — Hardening & Production (Weeks 17–20)

**Goal:** System is reliable enough for real capital.

### Tasks

- [ ] Implement JWT authentication and API key management
- [ ] Build scheduling system — cron-triggered pipeline runs
- [ ] Implement [[deployment-and-operations]]:
  - Multi-stage Docker build
  - Docker Compose for production
  - Prometheus metrics + Grafana dashboards
  - Structured logging with correlation IDs
- [ ] 60-day paper trading validation (target Sharpe > 1.0)
- [ ] Implement graceful shutdown (finish current pipeline before exit)
- [ ] Build alerting — Telegram/Discord notifications for signals and circuit breakers
- [ ] Load testing — concurrent pipelines, WebSocket fan-out
- [ ] Security audit — secrets management, SQL injection review, input validation
- [ ] Write operational runbook (start, stop, recover, debug)
- [ ] Backfill historical pipeline runs for strategy evaluation

### Deliverable

Production-ready system. Paper trading validates strategy performance. Monitoring and alerting in place. Security hardened.

---

## Milestone Summary

| Phase             | Weeks | Key Outcome                                |
| ----------------- | ----- | ------------------------------------------ |
| 1 — Foundation    | 1–3   | Go server + DB + single analyst pipeline   |
| 2 — Full Pipeline | 4–6   | Complete multi-agent pipeline with debates |
| 3 — Memory & Risk | 7–9   | Learning from outcomes + circuit breakers  |
| 4 — Execution     | 10–12 | Paper and live trade execution             |
| 5 — Frontend      | 13–16 | React dashboard with real-time agent viz   |
| 6 — Hardening     | 17–20 | Production-ready with monitoring           |

## Dependencies Between Phases

```
Phase 1 ──► Phase 2 ──► Phase 3 ──► Phase 4
                                       │
             Phase 5 ◄─────────────────┘
                │
             Phase 6
```

Phase 5 (frontend) can start in parallel with Phase 3–4 for API-independent components (layout, charts, mocks) but needs the WebSocket server and execution endpoints from Phase 4.

---

**Related:** [[executive-summary]] · [[system-architecture]] · [[go-project-structure]]
