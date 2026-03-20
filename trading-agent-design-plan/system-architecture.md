---
title: "System Architecture"
date: 2026-03-20
tags: [architecture, design, system-design]
---

# System Architecture

## High-Level Overview

```
┌─────────────────────────────────────────────────────────┐
│                    React Frontend                        │
│  Dashboard · Agent Viz · Portfolio · Strategy Config     │
└────────────────────┬────────────────────────────────────┘
                     │ REST + WebSocket
┌────────────────────▼────────────────────────────────────┐
│                   Go API Server                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐  │
│  │ REST API │ │ WS Server│ │ Auth     │ │ Scheduler │  │
│  └────┬─────┘ └────┬─────┘ └──────────┘ └─────┬─────┘  │
│       │             │                          │         │
│  ┌────▼─────────────▼──────────────────────────▼──────┐ │
│  │           Agent Orchestration Engine                │ │
│  │  ┌─────────────────────────────────────────────┐   │ │
│  │  │              Pipeline DAG                    │   │ │
│  │  │                                              │   │ │
│  │  │  Analysts ──► Research ──► Trader ──► Risk   │   │ │
│  │  │  (parallel)   Debate       Agent     Debate  │   │ │
│  │  └─────────────────────────────────────────────┘   │ │
│  └────────────────────────────────────────────────────┘ │
│       │             │              │            │        │
│  ┌────▼───┐  ┌──────▼───┐  ┌──────▼──┐  ┌─────▼─────┐ │
│  │  LLM   │  │  Data    │  │Execution│  │   Risk    │ │
│  │Provider│  │ Ingestion│  │ Engine  │  │  Engine   │ │
│  │ System │  │ Pipeline │  │         │  │           │ │
│  └────────┘  └──────────┘  └─────────┘  └───────────┘ │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                   PostgreSQL                             │
│  trades · positions · agent_decisions · memories ·       │
│  market_data · strategies · configurations · audit_log   │
└─────────────────────────────────────────────────────────┘
```

## Component Responsibilities

### API Layer

| Component        | Responsibility                                                        |
| ---------------- | --------------------------------------------------------------------- |
| REST API         | CRUD for strategies, configurations, portfolio; query agent decisions |
| WebSocket Server | Push real-time agent events, trade updates, market data to frontend   |
| Auth             | JWT-based authentication; API key management for programmatic access  |
| Scheduler        | Cron-like scheduling for strategy runs (e.g., daily at market open)   |

### Core Engine

| Component                      | Responsibility                                                                    |
| ------------------------------ | --------------------------------------------------------------------------------- |
| [[agent-orchestration-engine]] | DAG-based pipeline execution; manages agent lifecycle and state transitions       |
| [[llm-provider-system]]        | Abstraction over OpenAI, Anthropic, Google, Ollama; handles retries and fallbacks |
| [[data-ingestion-pipeline]]    | Fetches market data from multiple providers; caches in PostgreSQL and Redis       |
| [[execution-engine]]           | Routes orders to brokers/exchanges; manages fills and position tracking           |
| [[risk-management-engine]]     | Enforces portfolio-level limits, circuit breakers, and kill switches              |

### Persistence

| Store            | Purpose                                                  |
| ---------------- | -------------------------------------------------------- |
| PostgreSQL       | Primary data store for all persistent data               |
| Redis (optional) | Hot cache for market data, pub/sub for WebSocket fan-out |

## Data Flow — Single Trading Decision

```
1. Scheduler triggers pipeline for (ticker, date)
       │
2. ┌───▼───────────────────────────────────────────┐
   │  PHASE 1: ANALYSIS (parallel)                  │
   │  Market Analyst ──┐                            │
   │  Fundamentals ────┤──► Analyst Reports         │
   │  News Analyst ────┤                            │
   │  Social Analyst ──┘                            │
   └───────────────────────┬───────────────────────┘
                           │
3. ┌───────────────────────▼───────────────────────┐
   │  PHASE 2: RESEARCH DEBATE                      │
   │  Bull Researcher ◄──► Bear Researcher          │
   │          (N rounds, configurable)              │
   │  Research Manager ──► Investment Plan           │
   └───────────────────────┬───────────────────────┘
                           │
4. ┌───────────────────────▼───────────────────────┐
   │  PHASE 3: TRADING PLAN                         │
   │  Trader Agent ──► Concrete Trade Plan          │
   │  (entry, size, stop-loss, take-profit)         │
   └───────────────────────┬───────────────────────┘
                           │
5. ┌───────────────────────▼───────────────────────┐
   │  PHASE 4: RISK DEBATE                          │
   │  Aggressive ◄──► Conservative ◄──► Neutral     │
   │  Risk Manager ──► BUY / SELL / HOLD            │
   └───────────────────────┬───────────────────────┘
                           │
6. ┌───────────────────────▼───────────────────────┐
   │  PHASE 5: EXECUTION                            │
   │  Signal Extraction ──► Order Routing ──► Fill  │
   │  Position Update ──► Portfolio Rebalance       │
   └───────────────────────────────────────────────┘
```

All intermediate states (analyst reports, debate transcripts, trade plans, risk assessments) are persisted to PostgreSQL and streamed via WebSocket.

## Design Patterns

| Pattern                 | Usage                                                                   |
| ----------------------- | ----------------------------------------------------------------------- |
| **DAG / State Machine** | Pipeline orchestration — nodes are agents, edges are data dependencies  |
| **Factory**             | LLM provider instantiation, data provider instantiation                 |
| **Strategy**            | Pluggable trading strategies, indicator calculators, execution adapters |
| **Observer**            | Event bus for WebSocket streaming and audit logging                     |
| **Repository**          | Database access abstraction for each domain entity                      |
| **Circuit Breaker**     | Automatic disengagement on excessive losses or API failures             |

## Concurrency Model

Go's goroutines and channels are central to the design:

- **Phase 1 analysts** run concurrently via `errgroup.Group`
- **Debate rounds** are sequential within a debate but debates could overlap across tickers
- **Data ingestion** uses worker pools for parallel API calls
- **WebSocket fan-out** uses a hub pattern with per-client goroutines
- **Execution** is serial per order but parallel across independent orders

## Error Handling Strategy

1. **LLM failures** — Retry with exponential backoff; fall back to secondary provider
2. **Data provider failures** — Fall back to next provider in priority chain
3. **Execution failures** — Log, alert, and halt pipeline; never retry orders automatically
4. **Database failures** — Critical path; pipeline halts and alerts
5. **All errors** — Logged with structured fields, correlated by pipeline run ID

---

**Related:** [[executive-summary]] · [[technology-stack]] · [[agent-orchestration-engine]] · [[database-schema]]
