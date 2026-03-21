---
title: "API Design"
date: 2026-03-20
tags: [api, rest, websocket, endpoints]
---

# API Design

The Go server exposes a REST API for CRUD operations and a WebSocket endpoint for real-time streaming.

## Base URL

```
REST:      http://localhost:8080/api/v1
WebSocket: ws://localhost:8080/ws
```

## Authentication

- JWT bearer tokens for browser sessions
- API key header (`X-API-Key`) for programmatic access
- All endpoints require authentication except `/api/v1/auth/login`

## REST Endpoints

### Strategies

| Method   | Path                  | Description                   |
| -------- | --------------------- | ----------------------------- |
| `GET`    | `/strategies`         | List all strategies           |
| `POST`   | `/strategies`         | Create a new strategy         |
| `GET`    | `/strategies/:id`     | Get strategy details          |
| `PUT`    | `/strategies/:id`     | Update strategy configuration |
| `DELETE` | `/strategies/:id`     | Deactivate strategy           |
| `POST`   | `/strategies/:id/run` | Trigger a manual pipeline run |

### Pipeline Runs

| Method | Path                  | Description                                               |
| ------ | --------------------- | --------------------------------------------------------- |
| `GET`  | `/runs`               | List pipeline runs (filterable by strategy, status, date) |
| `GET`  | `/runs/:id`           | Get run details with all agent decisions                  |
| `GET`  | `/runs/:id/decisions` | Get agent decisions for a specific run                    |
| `POST` | `/runs/:id/cancel`    | Cancel a running pipeline                                 |

### Portfolio

| Method | Path                       | Description                                    |
| ------ | -------------------------- | ---------------------------------------------- |
| `GET`  | `/portfolio/positions`     | List open positions                            |
| `GET`  | `/portfolio/positions/:id` | Position detail with P&L                       |
| `GET`  | `/portfolio/summary`       | Portfolio summary (total value, P&L, exposure) |
| `GET`  | `/portfolio/history`       | Historical portfolio value time series         |

### Orders & Trades

| Method | Path          | Description              |
| ------ | ------------- | ------------------------ |
| `GET`  | `/orders`     | List orders (filterable) |
| `GET`  | `/orders/:id` | Order detail with fills  |
| `GET`  | `/trades`     | List executed trades     |

### Agent Memory

| Method   | Path                              | Description                 |
| -------- | --------------------------------- | --------------------------- |
| `GET`    | `/memories`                       | List memories by agent role |
| `GET`    | `/memories/search?q=...&role=...` | Search memories (full-text) |
| `DELETE` | `/memories/:id`                   | Remove a memory entry       |

### Configuration

| Method | Path                    | Description                             |
| ------ | ----------------------- | --------------------------------------- |
| `GET`  | `/config`               | Get system configuration                |
| `PUT`  | `/config`               | Update system configuration             |
| `GET`  | `/config/llm-providers` | List available LLM providers and models |

### Risk Controls

| Method   | Path                | Description                                    |
| -------- | ------------------- | ---------------------------------------------- |
| `GET`    | `/risk/status`      | Current risk status (circuit breakers, limits) |
| `POST`   | `/risk/kill-switch` | Activate kill switch (halt all trading)        |
| `DELETE` | `/risk/kill-switch` | Deactivate kill switch                         |
| `GET`    | `/risk/limits`      | Get current risk limits                        |
| `PUT`    | `/risk/limits`      | Update risk limits                             |

## Request/Response Examples

### Create Strategy

```json
// POST /api/v1/strategies
{
  "name": "AAPL Multi-Agent",
  "ticker": "AAPL",
  "market_type": "stock",
  "schedule_cron": "0 30 9 * * 1-5",
  "is_paper": true,
  "config": {
    "analysts": ["market", "fundamentals", "news"],
    "max_debate_rounds": 3,
    "max_risk_debate_rounds": 3,
    "deep_think_llm": { "provider": "anthropic", "model": "claude-sonnet-4-6" },
    "quick_think_llm": {
      "provider": "anthropic",
      "model": "claude-haiku-4-5-20251001"
    },
    "position_size_pct": 5.0,
    "stop_loss_atr_mult": 1.5,
    "take_profit_atr_mult": 3.0
  }
}
```

### Pipeline Run Response

```json
// GET /api/v1/runs/:id
{
  "id": "uuid",
  "strategy_id": "uuid",
  "ticker": "AAPL",
  "trade_date": "2026-03-20",
  "status": "completed",
  "signal": "buy",
  "started_at": "2026-03-20T09:30:00Z",
  "completed_at": "2026-03-20T09:30:28Z",
  "decisions": [
    {
      "agent_role": "market_analyst",
      "phase": "analysis",
      "output_structured": {
        "trend": "bullish",
        "key_levels": { "support": 178.5, "resistance": 185.0 }
      },
      "latency_ms": 3200
    }
  ]
}
```

## WebSocket Protocol

### Connection

```
ws://localhost:8080/ws?token=<jwt>
```

### Message Format

All messages are JSON with a `type` field:

```json
{
  "type": "agent_decision",
  "run_id": "uuid",
  "data": {
    "agent_role": "bull_researcher",
    "phase": "research_debate",
    "round": 2,
    "output_preview": "Strong momentum confirmed by...",
    "timestamp": "2026-03-20T09:30:12Z"
  }
}
```

### Event Types

| Type              | Description                  | When                 |
| ----------------- | ---------------------------- | -------------------- |
| `pipeline_start`  | Pipeline initiated           | Run begins           |
| `agent_decision`  | Agent produced output        | Each agent completes |
| `debate_round`    | Debate round completed       | Each debate round    |
| `signal`          | Final BUY/SELL/HOLD          | Risk manager decides |
| `order_submitted` | Order sent to broker         | Execution phase      |
| `order_filled`    | Order fully/partially filled | Fill received        |
| `position_update` | Position changed             | After fill           |
| `circuit_breaker` | Risk limit triggered         | Automatic halt       |
| `error`           | Pipeline error               | Any failure          |

### Client Subscriptions

Clients can subscribe to specific strategies or run IDs:

```json
// Send after connecting
{ "action": "subscribe", "strategy_ids": ["uuid1", "uuid2"] }
{ "action": "subscribe", "run_ids": ["uuid"] }
{ "action": "unsubscribe", "strategy_ids": ["uuid1"] }
```

## Error Response Format

```json
{
  "error": {
    "code": "STRATEGY_NOT_FOUND",
    "message": "Strategy with ID abc-123 not found",
    "details": {}
  }
}
```

Standard HTTP status codes: 400 (validation), 401 (auth), 404 (not found), 409 (conflict), 500 (server error).

---

**Related:** [[system-architecture]] · [[websocket-server]] · [[frontend-overview]] · [[go-project-structure]]
