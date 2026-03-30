# Documentation

## Quick Navigation

| Section                                           | Description                                                     |
| ------------------------------------------------- | --------------------------------------------------------------- |
| [Design](design/index.md)                         | Production system architecture, agents, API, database, roadmap  |
| [ADRs](adr/README.md)                             | Architecture Decision Records                                   |
| [Runbooks](runbooks/README.md)                    | Operational procedures for incidents and routine interventions  |
| [Reference](reference/index.md)                   | TradingAgents Python framework (the system we're evolving from) |
| [Research](research/index.md)                     | Trading strategies, LLM patterns, risk management               |
| [Paper Tracker](paper-tracker.md)                 | Index of 52 academic papers in `research/papers/`               |
| [Agent Execution Guide](agent-execution-guide.md) | How autonomous agents pick up and complete work                 |

---

## System Design (`design/`)

The production system spec for our Go/TypeScript/PostgreSQL trading agent.

### Core Architecture

- [Executive Summary](design/executive-summary.md) — Vision, goals, differentiators
- [System Architecture](design/system-architecture.md) — High-level architecture and data flow
- [Technology Stack](design/technology-stack.md) — Stack choices and rationale
- [Database Schema](design/database-schema.md) — PostgreSQL schema design
- [API Design](design/api-design.md) — REST and WebSocket specification
- [Implementation Roadmap](design/implementation-roadmap.md) — Phased build plan

### Agent System

- [Agent System Overview](design/agents/agent-system-overview.md) — Roles, interfaces, lifecycle
- [Analyst Agents](design/agents/analyst-agents.md) — Market, fundamentals, news, social
- [Research Debate](design/agents/research-debate-system.md) — Bull/bear adversarial debate
- [Trader Agent](design/agents/trader-agent.md) — Trading plan generation
- [Risk Management Agents](design/agents/risk-management-agents.md) — Risk debate and final signal

### Backend Systems

- [Agent Orchestration Engine](design/backend/agent-orchestration-engine.md) — DAG pipeline (replaces LangGraph)
- [LLM Provider System](design/backend/llm-provider-system.md) — Multi-provider abstraction
- [Data Ingestion Pipeline](design/backend/data-ingestion-pipeline.md) — Market data fetching and caching
- [Execution Engine](design/backend/execution-engine.md) — Order routing and fills
- [Risk Management Engine](design/backend/risk-management-engine.md) — Circuit breakers, kill switch
- [Memory & Learning](design/backend/memory-and-learning.md) — Agent memory with PostgreSQL FTS
- [WebSocket Server](design/backend/websocket-server.md) — Real-time event streaming
- [CLI Interface](design/backend/cli-interface.md) — TUI dashboard (Bubble Tea + Lipgloss)

### Frontend

- [Frontend Overview](design/frontend/frontend-overview.md) — React 19, Vite, shadcn/ui
- [Dashboard](design/frontend/dashboard-design.md) — Main monitoring dashboard
- [Agent Visualization](design/frontend/agent-visualization.md) — Pipeline and debate views
- [Portfolio & Strategy UI](design/frontend/portfolio-and-strategy-ui.md) — Portfolio and config

### Data & Execution

- [Data Architecture](design/data/data-architecture.md) — Data flow and provider abstraction
- [Market Data Providers](design/data/market-data-providers.md) — Polygon, Alpha Vantage, Yahoo, CCXT
- [Paper Trading](design/execution/paper-trading.md) — Simulated trading validation

### Infrastructure

- [Deployment & Operations](design/infrastructure/deployment-and-operations.md) — Docker, monitoring, CI/CD
- [Runbooks](runbooks/README.md) — Incident response and operational procedures

---

## Architecture Decisions (`adr/`)

| ADR                                     | Status   | Decision                                           |
| --------------------------------------- | -------- | -------------------------------------------------- |
| [001](adr/001-go-backend.md)            | Accepted | Use Go for backend services                        |
| [002](adr/002-two-tier-llm-strategy.md) | Accepted | Two-tier LLM model strategy (DeepThink/QuickThink) |
| [003](adr/003-postgres-fts-memory.md)   | Accepted | PostgreSQL FTS for agent memory (vs vector DB)     |
| [004](adr/004-custom-dag-engine.md)     | Accepted | Custom Go DAG engine (vs LangGraph)                |

---

## Pipeline: How a Trade Happens

```
┌─────────────────────────────────────────────────┐
│ PHASE 1: ANALYSIS (parallel, ~4-7s)             │
│ Market Analyst + Fundamentals + News + Social   │
│ → 4 independent reports                         │
└────────────────────────┬────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────┐
│ PHASE 2: RESEARCH DEBATE (3 rounds, ~9-12s)     │
│ Bull Researcher ◄──► Bear Researcher            │
│ Research Manager (Judge) → Investment Plan       │
└────────────────────────┬────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────┐
│ PHASE 3: TRADING (~2-4s)                        │
│ Trader Agent → Entry, size, stops, take-profit  │
└────────────────────────┬────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────┐
│ PHASE 4: RISK DEBATE (3 rounds, ~6-9s)          │
│ Aggressive ◄──► Conservative ◄──► Neutral       │
│ Risk Manager → FINAL BUY / SELL / HOLD          │
└────────────────────────┬────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────┐
│ PHASE 5: EXECUTION (~1-2s)                      │
│ Risk checks → Order → Fill → Position → Audit   │
└─────────────────────────────────────────────────┘
```

### Agent Roster (10 agents)

| Agent                | Phase               | Tier       | Role                                             |
| -------------------- | ------------------- | ---------- | ------------------------------------------------ |
| Market Analyst       | 1 - Analysis        | QuickThink | Technical analysis from OHLCV + 14 indicators    |
| Fundamentals Analyst | 1 - Analysis        | QuickThink | Financial health, valuation, growth assessment   |
| News Analyst         | 1 - Analysis        | QuickThink | Sentiment, catalysts, macro impact from news     |
| Social Media Analyst | 1 - Analysis        | QuickThink | Retail sentiment from social signals             |
| Bull Researcher      | 2 - Research Debate | DeepThink  | Advocates bullish case, cites evidence           |
| Bear Researcher      | 2 - Research Debate | DeepThink  | Identifies risks, argues bearish case            |
| Research Manager     | 2 - Research Debate | DeepThink  | Synthesizes debate → investment plan             |
| Trader               | 3 - Trading         | DeepThink  | Converts plan → entry, size, stops, targets      |
| Risk Analysts (3)    | 4 - Risk Debate     | DeepThink  | Aggressive / Conservative / Neutral perspectives |
| Risk Manager         | 4 - Risk Debate     | DeepThink  | Final BUY/SELL/HOLD signal with confidence       |

---

## Research Reference (`research/`)

Background material that informed the system design:

- **12 strategy deep-dives** — Momentum, mean-reversion, factor investing, etc.
- **5 LLM trading guides** — Architecture, sentiment, multi-agent systems
- **Execution guides** — Stocks, crypto, prediction markets
- **Risk management** — Position sizing, portfolio controls
- **Backtesting** — Methodology, LLM-specific challenges
- **52 academic papers** — Full PDFs in `research/papers/`

See [research/index.md](research/index.md) for the full index.

---

## Framework Reference (`reference/`)

Documentation of the [TradingAgents v0.2.1](https://github.com/TradingAgents) Python framework (UCLA/MIT, arXiv:2412.20138) that our system evolves from. Useful for understanding the original agent architecture and how our Go implementation differs.

See [reference/index.md](reference/index.md) for the full index.
