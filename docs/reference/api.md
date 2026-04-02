---
title: "API Reference"
date: 2026-04-02
tags: [api, rest, websocket, reference]
---

# API Reference

Canonical route source: `internal/api/server.go`, `internal/api/handlers.go`, `internal/api/auth.go`, and `internal/api/responses.go`.

## Base URLs

```text
REST:      http://localhost:8080/api/v1
WebSocket: ws://localhost:8080/ws
Ops:       http://localhost:8080/healthz | /health | /metrics
```

## Authentication

### Public endpoints

- `GET /healthz`
- `GET /health`
- `GET /metrics`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `GET /ws` (WebSocket upgrade; auth is not enforced by the current handler)

### Protected endpoints

All other `/api/v1/*` routes require one of:

```http
Authorization: Bearer <access_token>
```

or

```http
X-API-Key: <api_key>
```

## Shared response shapes

### Error envelope

```json
{
  "error": "authentication required",
  "code": "ERR_UNAUTHORIZED"
}
```

### Paginated list envelope

```json
{
  "data": [],
  "limit": 50,
  "offset": 0
}
```

Notes:

- `limit` defaults to `50` and is capped at `100`.
- `offset` defaults to `0`.
- The `ListResponse` type has a `total` field, but handlers currently omit it.

## Route summary

| Group | Endpoints |
| --- | --- |
| Auth | `POST /auth/login`, `POST /auth/refresh` |
| Strategies | `GET/POST /strategies`, `GET/PUT/DELETE /strategies/{id}`, `POST /strategies/{id}/run`, `POST /strategies/{id}/pause`, `POST /strategies/{id}/resume`, `POST /strategies/{id}/skip-next` |
| Runs | `GET /runs`, `GET /runs/{id}`, `GET /runs/{id}/decisions`, `POST /runs/{id}/cancel`, `GET /runs/{id}/snapshot` |
| Portfolio | `GET /portfolio/positions`, `GET /portfolio/positions/open`, `GET /portfolio/summary` |
| Orders | `GET /orders`, `GET /orders/{id}` |
| Trades | `GET /trades` |
| Memories | `GET /memories`, `POST /memories/search`, `DELETE /memories/{id}` |
| Risk | `GET /risk/status`, `POST /risk/killswitch` |
| Settings | `GET /settings`, `PUT /settings` |
| Events | `GET /events` |
| Conversations | `GET/POST /conversations`, `GET/POST /conversations/{id}/messages` |
| Audit log | `GET /audit-log` |

## REST endpoints

### Auth

#### POST `/api/v1/auth/login`
- Auth: public
- Purpose: validate username/password and mint access + refresh tokens
- Request example:

```json
{
  "username": "alice",
  "password": "correct-horse-battery-staple"
}
```

- Response example:

```json
{
  "access_token": "eyJhbGciOiJI...",
  "refresh_token": "eyJhbGciOiJI...",
  "expires_at": "2026-04-02T16:40:00Z"
}
```

#### POST `/api/v1/auth/refresh`
- Auth: public
- Purpose: exchange a refresh token for a new token pair
- Request example:

```json
{
  "refresh_token": "eyJhbGciOiJI..."
}
```

- Response example:

```json
{
  "access_token": "eyJhbGciOiJI...",
  "refresh_token": "eyJhbGciOiJI...",
  "expires_at": "2026-04-02T17:40:00Z"
}
```

### Strategies

#### GET `/api/v1/strategies`
- Auth: required
- Query params: `ticker`, `market_type`, `status`, `is_paper`, `limit`, `offset`
- Request example:

```text
GET /api/v1/strategies?ticker=AAPL&market_type=stock&status=active&is_paper=true&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "11111111-1111-1111-1111-111111111111",
      "name": "AAPL Daily",
      "ticker": "AAPL",
      "market_type": "stock",
      "schedule_cron": "0 30 9 * * 1-5",
      "config": {
        "llm_config": {
          "provider": "openai",
          "deep_think_model": "gpt-5.2",
          "quick_think_model": "gpt-5-mini"
        }
      },
      "status": "active",
      "skip_next_run": false,
      "is_paper": true,
      "created_at": "2026-04-02T09:00:00Z",
      "updated_at": "2026-04-02T09:00:00Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### POST `/api/v1/strategies`
- Auth: required
- Purpose: create a strategy after `domain.Strategy.Validate()` and typed config validation
- Request example:

```json
{
  "name": "AAPL Daily",
  "description": "US equities momentum",
  "ticker": "AAPL",
  "market_type": "stock",
  "schedule_cron": "0 30 9 * * 1-5",
  "config": {
    "llm_config": {
      "provider": "openai",
      "deep_think_model": "gpt-5.2",
      "quick_think_model": "gpt-5-mini"
    },
    "pipeline_config": {
      "debate_rounds": 2,
      "analysis_timeout_seconds": 90,
      "debate_timeout_seconds": 45
    },
    "risk_config": {
      "position_size_pct": 5,
      "stop_loss_multiplier": 1.5,
      "take_profit_multiplier": 3,
      "min_confidence": 0.65
    },
    "analyst_selection": ["market_analyst", "news_analyst"]
  },
  "status": "active",
  "skip_next_run": false,
  "is_paper": true
}
```

- Response example:

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "name": "AAPL Daily",
  "description": "US equities momentum",
  "ticker": "AAPL",
  "market_type": "stock",
  "schedule_cron": "0 30 9 * * 1-5",
  "config": {
    "llm_config": {
      "provider": "openai",
      "deep_think_model": "gpt-5.2",
      "quick_think_model": "gpt-5-mini"
    }
  },
  "status": "active",
  "skip_next_run": false,
  "is_paper": true,
  "created_at": "2026-04-02T09:00:00Z",
  "updated_at": "2026-04-02T09:00:00Z"
}
```

#### GET `/api/v1/strategies/{id}`
- Auth: required
- Request example:

```text
GET /api/v1/strategies/11111111-1111-1111-1111-111111111111
```

- Response example:

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "name": "AAPL Daily",
  "ticker": "AAPL",
  "market_type": "stock",
  "config": {},
  "status": "active",
  "skip_next_run": false,
  "is_paper": true,
  "created_at": "2026-04-02T09:00:00Z",
  "updated_at": "2026-04-02T09:00:00Z"
}
```

#### PUT `/api/v1/strategies/{id}`
- Auth: required
- Purpose: replace the strategy payload for the target id
- Request example:

```json
{
  "name": "AAPL Daily v2",
  "description": "Updated thesis",
  "ticker": "AAPL",
  "market_type": "stock",
  "schedule_cron": "0 45 9 * * 1-5",
  "config": {
    "pipeline_config": {
      "debate_rounds": 3
    }
  },
  "status": "active",
  "skip_next_run": false,
  "is_paper": true
}
```

- Response example:

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "name": "AAPL Daily v2",
  "description": "Updated thesis",
  "ticker": "AAPL",
  "market_type": "stock",
  "schedule_cron": "0 45 9 * * 1-5",
  "config": {
    "pipeline_config": {
      "debate_rounds": 3
    }
  },
  "status": "active",
  "skip_next_run": false,
  "is_paper": true,
  "created_at": "2026-04-02T09:00:00Z",
  "updated_at": "2026-04-02T10:00:00Z"
}
```

#### DELETE `/api/v1/strategies/{id}`
- Auth: required
- Purpose: delete the strategy row
- Request example:

```text
DELETE /api/v1/strategies/11111111-1111-1111-1111-111111111111
```

- Response example:

```text
204 No Content
```

#### POST `/api/v1/strategies/{id}/run`
- Auth: required
- Purpose: trigger an on-demand strategy run
- Notes: returns `501` when no `StrategyRunner` is configured
- Request example:

```text
POST /api/v1/strategies/11111111-1111-1111-1111-111111111111/run
```

- Response example:

```json
{
  "run": {
    "id": "22222222-2222-2222-2222-222222222222",
    "strategy_id": "11111111-1111-1111-1111-111111111111",
    "ticker": "AAPL",
    "trade_date": "2026-04-02T00:00:00Z",
    "status": "completed",
    "signal": "buy",
    "started_at": "2026-04-02T09:30:00Z",
    "completed_at": "2026-04-02T09:30:12Z"
  },
  "signal": "buy",
  "orders": [
    {
      "id": "33333333-3333-3333-3333-333333333333",
      "ticker": "AAPL",
      "side": "buy",
      "order_type": "market",
      "quantity": 10,
      "filled_quantity": 10,
      "status": "filled",
      "broker": "alpaca",
      "created_at": "2026-04-02T09:30:03Z"
    }
  ],
  "positions": [
    {
      "id": "44444444-4444-4444-4444-444444444444",
      "ticker": "AAPL",
      "side": "long",
      "quantity": 10,
      "avg_entry": 182.5,
      "realized_pnl": 0,
      "opened_at": "2026-04-02T09:30:04Z"
    }
  ]
}
```

#### POST `/api/v1/strategies/{id}/pause`
- Auth: required
- Purpose: transition a strategy from `active` to `paused`
- Request example:

```text
POST /api/v1/strategies/11111111-1111-1111-1111-111111111111/pause
```

- Response example:

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "status": "paused",
  "skip_next_run": false
}
```

#### POST `/api/v1/strategies/{id}/resume`
- Auth: required
- Purpose: transition a strategy from `paused` to `active`
- Request example:

```text
POST /api/v1/strategies/11111111-1111-1111-1111-111111111111/resume
```

- Response example:

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "status": "active",
  "skip_next_run": false
}
```

#### POST `/api/v1/strategies/{id}/skip-next`
- Auth: required
- Purpose: set `skip_next_run=true` on an `active` strategy
- Request example:

```text
POST /api/v1/strategies/11111111-1111-1111-1111-111111111111/skip-next
```

- Response example:

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "status": "active",
  "skip_next_run": true
}
```

### Runs

#### GET `/api/v1/runs`
- Auth: required
- Query params: `strategy_id`, `ticker`, `status`, `start_date`, `end_date`, `limit`, `offset`
- Request example:

```text
GET /api/v1/runs?strategy_id=11111111-1111-1111-1111-111111111111&status=completed&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "22222222-2222-2222-2222-222222222222",
      "strategy_id": "11111111-1111-1111-1111-111111111111",
      "ticker": "AAPL",
      "trade_date": "2026-04-02T00:00:00Z",
      "status": "completed",
      "signal": "buy",
      "started_at": "2026-04-02T09:30:00Z",
      "completed_at": "2026-04-02T09:30:12Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### GET `/api/v1/runs/{id}`
- Auth: required
- Request example:

```text
GET /api/v1/runs/22222222-2222-2222-2222-222222222222
```

- Response example:

```json
{
  "id": "22222222-2222-2222-2222-222222222222",
  "strategy_id": "11111111-1111-1111-1111-111111111111",
  "ticker": "AAPL",
  "trade_date": "2026-04-02T00:00:00Z",
  "status": "completed",
  "signal": "buy",
  "started_at": "2026-04-02T09:30:00Z",
  "completed_at": "2026-04-02T09:30:12Z",
  "config_snapshot": {"pipeline_config":{"debate_rounds":2}},
  "phase_timings": {"analysis_ms": 3400}
}
```

#### GET `/api/v1/runs/{id}/decisions`
- Auth: required
- Query params: `include_prompt`, `limit`, `offset`
- Request example:

```text
GET /api/v1/runs/22222222-2222-2222-2222-222222222222/decisions?include_prompt=true&limit=10&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "55555555-5555-5555-5555-555555555555",
      "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
      "agent_role": "market_analyst",
      "phase": "analysis",
      "output_text": "Momentum remains constructive.",
      "output_structured": {
        "trend": "bullish"
      },
      "llm_provider": "openai",
      "llm_model": "gpt-5-mini",
      "prompt_text": "You are the market analyst...",
      "prompt_tokens": 312,
      "completion_tokens": 147,
      "latency_ms": 2200,
      "cost_usd": 0.01,
      "created_at": "2026-04-02T09:30:04Z"
    }
  ],
  "limit": 10,
  "offset": 0
}
```

#### POST `/api/v1/runs/{id}/cancel`
- Auth: required
- Purpose: transition a cancellable run to `cancelled`
- Request example:

```text
POST /api/v1/runs/22222222-2222-2222-2222-222222222222/cancel
```

- Response example:

```json
{
  "status": "cancelled"
}
```

#### GET `/api/v1/runs/{id}/snapshot`
- Auth: required
- Purpose: return snapshots grouped by `data_type`
- Notes: returns `501` when snapshot storage is not configured
- Request example:

```text
GET /api/v1/runs/22222222-2222-2222-2222-222222222222/snapshot
```

- Response example:

```json
{
  "market": {
    "price": 150.0
  },
  "news": {
    "headline": "test"
  }
}
```

### Portfolio

#### GET `/api/v1/portfolio/positions`
- Auth: required
- Query params: `ticker`, `side`, `limit`, `offset`
- Request example:

```text
GET /api/v1/portfolio/positions?ticker=AAPL&side=long&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "44444444-4444-4444-4444-444444444444",
      "ticker": "AAPL",
      "side": "long",
      "quantity": 10,
      "avg_entry": 182.5,
      "current_price": 185.1,
      "unrealized_pnl": 26,
      "realized_pnl": 0,
      "opened_at": "2026-04-02T09:30:04Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### GET `/api/v1/portfolio/positions/open`
- Auth: required
- Query params: `limit`, `offset`
- Request example:

```text
GET /api/v1/portfolio/positions/open?limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "44444444-4444-4444-4444-444444444444",
      "ticker": "AAPL",
      "side": "long",
      "quantity": 10,
      "avg_entry": 182.5,
      "current_price": 185.1,
      "unrealized_pnl": 26,
      "realized_pnl": 0,
      "opened_at": "2026-04-02T09:30:04Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### GET `/api/v1/portfolio/summary`
- Auth: required
- Request example:

```text
GET /api/v1/portfolio/summary
```

- Response example:

```json
{
  "open_positions": 3,
  "unrealized_pnl": 1240.55,
  "realized_pnl": 310.2
}
```

### Orders

#### GET `/api/v1/orders`
- Auth: required
- Query params: `ticker`, `status`, `side`, `limit`, `offset`
- Request example:

```text
GET /api/v1/orders?ticker=AAPL&status=filled&side=buy&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "33333333-3333-3333-3333-333333333333",
      "strategy_id": "11111111-1111-1111-1111-111111111111",
      "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
      "ticker": "AAPL",
      "side": "buy",
      "order_type": "market",
      "quantity": 10,
      "filled_quantity": 10,
      "filled_avg_price": 182.5,
      "status": "filled",
      "broker": "alpaca",
      "submitted_at": "2026-04-02T09:30:03Z",
      "filled_at": "2026-04-02T09:30:04Z",
      "created_at": "2026-04-02T09:30:03Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### GET `/api/v1/orders/{id}`
- Auth: required
- Purpose: return the order plus associated fills
- Request example:

```text
GET /api/v1/orders/33333333-3333-3333-3333-333333333333
```

- Response example:

```json
{
  "order": {
    "id": "33333333-3333-3333-3333-333333333333",
    "ticker": "AAPL",
    "side": "buy",
    "order_type": "market",
    "quantity": 10,
    "filled_quantity": 10,
    "status": "filled",
    "broker": "alpaca",
    "created_at": "2026-04-02T09:30:03Z"
  },
  "fills": [
    {
      "id": "66666666-6666-6666-6666-666666666666",
      "order_id": "33333333-3333-3333-3333-333333333333",
      "position_id": "44444444-4444-4444-4444-444444444444",
      "ticker": "AAPL",
      "side": "buy",
      "quantity": 10,
      "price": 182.5,
      "fee": 0,
      "executed_at": "2026-04-02T09:30:04Z",
      "created_at": "2026-04-02T09:30:04Z"
    }
  ]
}
```

### Trades

#### GET `/api/v1/trades`
- Auth: required
- Query params: `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date`, `limit`, `offset`
- Notes: `order_id` and `position_id` cannot be combined
- Request example:

```text
GET /api/v1/trades?position_id=44444444-4444-4444-4444-444444444444&side=buy&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "66666666-6666-6666-6666-666666666666",
      "order_id": "33333333-3333-3333-3333-333333333333",
      "position_id": "44444444-4444-4444-4444-444444444444",
      "ticker": "AAPL",
      "side": "buy",
      "quantity": 10,
      "price": 182.5,
      "fee": 0,
      "executed_at": "2026-04-02T09:30:04Z",
      "created_at": "2026-04-02T09:30:04Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

### Memories

#### GET `/api/v1/memories`
- Auth: required
- Query params: `q`, `agent_role`, `limit`, `offset`
- Purpose: search/list stored agent memories
- Request example:

```text
GET /api/v1/memories?q=momentum&agent_role=market_analyst&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "77777777-7777-7777-7777-777777777777",
      "agent_role": "market_analyst",
      "situation": "Breakout after earnings",
      "recommendation": "Favor trend continuation setups",
      "outcome": "Positive follow-through",
      "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
      "relevance_score": 0.87,
      "created_at": "2026-04-01T15:00:00Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### POST `/api/v1/memories/search`
- Auth: required
- Purpose: body-based full-text search
- Request example:

```json
{
  "query": "breakout risk"
}
```

- Response example:

```json
{
  "data": [
    {
      "id": "77777777-7777-7777-7777-777777777777",
      "agent_role": "market_analyst",
      "situation": "Breakout after earnings",
      "recommendation": "Favor trend continuation setups",
      "created_at": "2026-04-01T15:00:00Z"
    }
  ],
  "limit": 50,
  "offset": 0
}
```

#### DELETE `/api/v1/memories/{id}`
- Auth: required
- Request example:

```text
DELETE /api/v1/memories/77777777-7777-7777-7777-777777777777
```

- Response example:

```text
204 No Content
```

### Risk

#### GET `/api/v1/risk/status`
- Auth: required
- Request example:

```text
GET /api/v1/risk/status
```

- Response example:

```json
{
  "risk_status": "normal",
  "circuit_breaker": {
    "state": "open"
  },
  "kill_switch": {
    "active": false
  },
  "position_limits": {
    "max_per_position_pct": 10,
    "max_total_pct": 100,
    "max_concurrent": 10,
    "max_per_market_pct": 50
  },
  "updated_at": "2026-04-02T09:30:00Z"
}
```

#### POST `/api/v1/risk/killswitch`
- Auth: required
- Purpose: activate or deactivate the kill switch
- Notes: `reason` is required when `active=true`
- Request example:

```json
{
  "active": true,
  "reason": "manual halt during incident review"
}
```

- Response example:

```json
{
  "active": true
}
```

### Settings

#### GET `/api/v1/settings`
- Auth: required
- Request example:

```text
GET /api/v1/settings
```

- Response example:

```json
{
  "llm": {
    "default_provider": "openai",
    "deep_think_model": "gpt-5.2",
    "quick_think_model": "gpt-5-mini",
    "providers": {
      "openai": {
        "api_key_configured": true,
        "api_key_last4": "1234",
        "base_url": "https://api.openai.com/v1",
        "model": "gpt-5-mini"
      },
      "anthropic": {
        "api_key_configured": false,
        "model": "claude-3-7-sonnet-latest"
      },
      "google": {
        "api_key_configured": false,
        "model": "gemini-2.5-flash"
      },
      "openrouter": {
        "api_key_configured": false,
        "model": "openai/gpt-4.1-mini"
      },
      "xai": {
        "api_key_configured": false,
        "model": "grok-3-mini"
      },
      "ollama": {
        "base_url": "http://localhost:11434",
        "model": "llama3.2"
      }
    }
  },
  "risk": {
    "max_position_size_pct": 10,
    "max_daily_loss_pct": 3,
    "max_drawdown_pct": 10,
    "max_open_positions": 10,
    "max_total_exposure_pct": 100,
    "max_per_market_exposure_pct": 50,
    "circuit_breaker_threshold_pct": 3,
    "circuit_breaker_cooldown_min": 15
  },
  "system": {
    "environment": "development",
    "version": "development",
    "uptime_seconds": 3600,
    "connected_brokers": [
      {
        "name": "alpaca",
        "paper_mode": true,
        "configured": true
      }
    ]
  }
}
```

#### PUT `/api/v1/settings`
- Auth: required
- Purpose: replace editable LLM and risk settings; provider secrets are preserved unless a new `api_key` is supplied
- Request example:

```json
{
  "llm": {
    "default_provider": "openai",
    "deep_think_model": "gpt-5.2",
    "quick_think_model": "gpt-5-mini",
    "providers": {
      "openai": {
        "api_key": "sk-new-1234",
        "base_url": "https://api.openai.com/v1",
        "model": "gpt-5-mini"
      },
      "anthropic": {
        "model": "claude-3-7-sonnet-latest"
      },
      "google": {
        "model": "gemini-2.5-flash"
      },
      "openrouter": {
        "base_url": "https://openrouter.ai/api/v1",
        "model": "openai/gpt-4.1-mini"
      },
      "xai": {
        "base_url": "https://api.x.ai/v1",
        "model": "grok-3-mini"
      },
      "ollama": {
        "base_url": "http://localhost:11434",
        "model": "llama3.2"
      }
    }
  },
  "risk": {
    "max_position_size_pct": 10,
    "max_daily_loss_pct": 3,
    "max_drawdown_pct": 10,
    "max_open_positions": 10,
    "max_total_exposure_pct": 100,
    "max_per_market_exposure_pct": 50,
    "circuit_breaker_threshold_pct": 3,
    "circuit_breaker_cooldown_min": 15
  }
}
```

- Response example:

```json
{
  "llm": {
    "default_provider": "openai",
    "deep_think_model": "gpt-5.2",
    "quick_think_model": "gpt-5-mini"
  },
  "risk": {
    "max_position_size_pct": 10,
    "max_daily_loss_pct": 3,
    "max_drawdown_pct": 10,
    "max_open_positions": 10,
    "max_total_exposure_pct": 100,
    "max_per_market_exposure_pct": 50,
    "circuit_breaker_threshold_pct": 3,
    "circuit_breaker_cooldown_min": 15
  },
  "system": {
    "environment": "development",
    "version": "development",
    "uptime_seconds": 3600,
    "connected_brokers": [
      {
        "name": "alpaca",
        "paper_mode": true,
        "configured": true
      }
    ]
  }
}
```

### Events

#### GET `/api/v1/events`
- Auth: required
- Query params: `event_kind`, `pipeline_run_id`, `strategy_id`, `agent_role`, `after`, `before`, `limit`, `offset`
- Notes: returns `501` when event storage is not configured
- Request example:

```text
GET /api/v1/events?strategy_id=11111111-1111-1111-1111-111111111111&event_kind=signal&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "88888888-8888-8888-8888-888888888888",
      "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
      "strategy_id": "11111111-1111-1111-1111-111111111111",
      "agent_role": "risk_manager",
      "event_kind": "signal",
      "title": "Final signal",
      "summary": "BUY confirmed",
      "tags": ["signal"],
      "metadata": {"signal": "buy"},
      "created_at": "2026-04-02T09:30:10Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

### Conversations

#### GET `/api/v1/conversations`
- Auth: required
- Query params: `pipeline_run_id`, `agent_role`, `limit`, `offset`
- Notes: returns `501` when the conversation repository is not configured
- Request example:

```text
GET /api/v1/conversations?pipeline_run_id=22222222-2222-2222-2222-222222222222&agent_role=bull_researcher&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "99999999-9999-9999-9999-999999999999",
      "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
      "agent_role": "bull_researcher",
      "title": "Chat with Bull Researcher — AAPL",
      "created_at": "2026-04-02T09:45:00Z",
      "updated_at": "2026-04-02T09:46:00Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

#### POST `/api/v1/conversations`
- Auth: required
- Purpose: create a conversation for an existing pipeline run
- Notes: title is generated server-side as `Chat with <Agent Role> — <Ticker>`
- Request example:

```json
{
  "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
  "agent_role": "bull_researcher"
}
```

- Response example:

```json
{
  "id": "99999999-9999-9999-9999-999999999999",
  "pipeline_run_id": "22222222-2222-2222-2222-222222222222",
  "agent_role": "bull_researcher",
  "title": "Chat with Bull Researcher — AAPL",
  "created_at": "2026-04-02T09:45:00Z",
  "updated_at": "2026-04-02T09:45:00Z"
}
```

#### GET `/api/v1/conversations/{id}/messages`
- Auth: required
- Query params: `limit`, `offset`
- Request example:

```text
GET /api/v1/conversations/99999999-9999-9999-9999-999999999999/messages?limit=50&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
      "conversation_id": "99999999-9999-9999-9999-999999999999",
      "role": "user",
      "content": "Why did you buy AAPL?",
      "created_at": "2026-04-02T09:45:10Z"
    },
    {
      "id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
      "conversation_id": "99999999-9999-9999-9999-999999999999",
      "role": "assistant",
      "content": "Momentum and breadth supported the entry.",
      "created_at": "2026-04-02T09:45:11Z"
    }
  ],
  "limit": 50,
  "offset": 0
}
```

#### POST `/api/v1/conversations/{id}/messages`
- Auth: required
- Purpose: save a user message, then generate and save an assistant reply via the configured LLM provider
- Notes: if the LLM provider is missing, the handler returns `501` after saving the user message
- Request example:

```json
{
  "content": "What do you think about AAPL?"
}
```

- Response example:

```json
{
  "id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
  "conversation_id": "99999999-9999-9999-9999-999999999999",
  "role": "assistant",
  "content": "AI response here",
  "created_at": "2026-04-02T09:45:11Z"
}
```

### Audit log

#### GET `/api/v1/audit-log`
- Auth: required
- Query params: `event_type`, `entity_type`, `after`, `before`, `limit`, `offset`
- Notes: returns `501` when the audit-log repository is not configured
- Request example:

```text
GET /api/v1/audit-log?event_type=strategy.updated&entity_type=strategy&limit=25&offset=0
```

- Response example:

```json
{
  "data": [
    {
      "id": "cccccccc-cccc-cccc-cccc-cccccccccccc",
      "event_type": "strategy.updated",
      "entity_type": "strategy",
      "entity_id": "11111111-1111-1111-1111-111111111111",
      "actor": "alice",
      "details": {"field": "status", "new_value": "paused"},
      "created_at": "2026-04-02T09:50:00Z"
    }
  ],
  "limit": 25,
  "offset": 0
}
```

## Operational endpoints

### GET `/healthz`
- Auth: public
- Request example:

```text
GET /healthz
```

- Response example:

```json
{
  "status": "ok",
  "db": "ok",
  "redis": "ok"
}
```

### GET `/health`
- Auth: public
- Request example:

```text
GET /health
```

- Response example:

```json
{
  "status": "degraded",
  "db": "ok",
  "redis": "error"
}
```

### GET `/metrics`
- Auth: public
- Request example:

```text
GET /metrics
```

- Response example:

```text
# metrics placeholder
```

## WebSocket

### GET `/ws`
- Auth: public in the current implementation
- Purpose: upgrade to a WebSocket and stream `WSMessage` envelopes to matching subscribers

#### Subscribe command examples

```json
{ "action": "subscribe", "strategy_ids": ["11111111-1111-1111-1111-111111111111"] }
{ "action": "subscribe", "run_ids": ["22222222-2222-2222-2222-222222222222"] }
{ "action": "unsubscribe", "strategy_ids": ["11111111-1111-1111-1111-111111111111"] }
{ "action": "subscribe_all" }
{ "action": "unsubscribe_all" }
```

#### Ack example

```json
{
  "status": "ok",
  "action": "subscribe_all"
}
```

#### Event envelope example

```json
{
  "type": "pipeline_start",
  "strategy_id": "11111111-1111-1111-1111-111111111111",
  "run_id": "22222222-2222-2222-2222-222222222222",
  "data": {
    "status": "running"
  },
  "timestamp": "2026-04-02T09:30:00Z"
}
```

#### Event types emitted by the current hub

- `pipeline_start`
- `agent_decision`
- `debate_round`
- `signal`
- `order_submitted`
- `order_filled`
- `position_update`
- `circuit_breaker`
- `error`
