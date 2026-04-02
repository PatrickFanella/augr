---
title: "API Design"
date: 2026-04-02
tags: [api, rest, websocket, endpoints]
---

# API Design

This document describes the API surface currently implemented by the Go server. For endpoint-by-endpoint payload examples, see [[../reference/api.md|API Reference]].

## Base URLs

```text
REST:      http://localhost:8080/api/v1
WebSocket: ws://localhost:8080/ws
Ops:       http://localhost:8080/healthz | /health | /metrics
```

## Authentication model

- Public REST endpoints: `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`
- Protected REST endpoints: every other `/api/v1/*` route
- Supported credentials for protected routes:
  - `Authorization: Bearer <jwt>`
  - `X-API-Key: <api_key>`
- Public operational endpoints: `GET /healthz`, `GET /health`, `GET /metrics`
- WebSocket auth is not enforced by the current `/ws` handler

## Response conventions

### List endpoints

Most collection endpoints return:

```json
{
  "data": [],
  "limit": 50,
  "offset": 0
}
```

Notes:

- Default `limit=50`
- Maximum `limit=100`
- `total` exists in the Go response type but is not currently populated by handlers

### Error envelope

```json
{
  "error": "strategy not found",
  "code": "ERR_NOT_FOUND"
}
```

## Implemented REST surface

### Auth

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `POST` | `/auth/login` | Public | Username/password login |
| `POST` | `/auth/refresh` | Public | Refresh-token exchange |

### Strategies

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/strategies` | Required | Filters: `ticker`, `market_type`, `status`, `is_paper` |
| `POST` | `/strategies` | Required | Validates `domain.Strategy` + typed strategy config |
| `GET` | `/strategies/{id}` | Required | Fetch one strategy |
| `PUT` | `/strategies/{id}` | Required | Replaces the strategy payload for the target id |
| `DELETE` | `/strategies/{id}` | Required | Deletes the strategy |
| `POST` | `/strategies/{id}/run` | Required | Manual run; returns `501` if runner not configured |
| `POST` | `/strategies/{id}/pause` | Required | Requires current status `active` |
| `POST` | `/strategies/{id}/resume` | Required | Requires current status `paused` |
| `POST` | `/strategies/{id}/skip-next` | Required | Sets `skip_next_run=true`; requires current status `active` |

### Runs

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/runs` | Required | Filters: `strategy_id`, `ticker`, `status`, `start_date`, `end_date` |
| `GET` | `/runs/{id}` | Required | Looks up by id by scanning paginated run lists |
| `GET` | `/runs/{id}/decisions` | Required | Query: `include_prompt`, `limit`, `offset` |
| `POST` | `/runs/{id}/cancel` | Required | Only valid when current run state can transition to `cancelled` |
| `GET` | `/runs/{id}/snapshot` | Required | Returns snapshots grouped by `data_type`; `501` if snapshot repo missing |

### Portfolio

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/portfolio/positions` | Required | Filters: `ticker`, `side` |
| `GET` | `/portfolio/positions/open` | Required | Lists open positions only |
| `GET` | `/portfolio/summary` | Required | Aggregates realized/unrealized P&L from open positions |

### Orders and trades

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/orders` | Required | Filters: `ticker`, `status`, `side` |
| `GET` | `/orders/{id}` | Required | Returns `{order, fills}` |
| `GET` | `/trades` | Required | Filters: `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date` |

### Memories

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/memories` | Required | Query-driven search/list with optional `q`, `agent_role` |
| `POST` | `/memories/search` | Required | Body: `{ "query": "..." }` |
| `DELETE` | `/memories/{id}` | Required | Deletes one memory |

### Risk and settings

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/risk/status` | Required | Returns `risk.EngineStatus` |
| `POST` | `/risk/killswitch` | Required | Body: `{active, reason}`; `reason` required when activating |
| `GET` | `/settings` | Required | Returns editable LLM/risk settings plus system metadata |
| `PUT` | `/settings` | Required | Replaces editable settings; preserves existing provider secrets unless a new key is supplied |

### Events, conversations, audit log

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| `GET` | `/events` | Required | Filters: `event_kind`, `pipeline_run_id`, `strategy_id`, `agent_role`, `after`, `before`; `501` if repo missing |
| `GET` | `/conversations` | Required | Filters: `pipeline_run_id`, `agent_role`; `501` if repo missing |
| `POST` | `/conversations` | Required | Creates a conversation for an existing run; title auto-generated |
| `GET` | `/conversations/{id}/messages` | Required | Lists persisted messages |
| `POST` | `/conversations/{id}/messages` | Required | Saves user message then generates assistant reply; `501` if LLM missing |
| `GET` | `/audit-log` | Required | Filters: `event_type`, `entity_type`, `after`, `before`; `501` if repo missing |

## Deliberately absent from the current implementation

These endpoints appeared in older design docs but are not registered by `internal/api/server.go` and should not be treated as live routes:

- `GET /portfolio/history`
- `GET /config`
- `PUT /config`
- `GET /config/llm-providers`
- `GET /risk/limits`
- `PUT /risk/limits`
- `DELETE /risk/killswitch`

## WebSocket design

### Endpoint

- `GET /ws`
- Public upgrade route in current code
- Subscription scope is client-side, not path-based

### Client commands

```json
{ "action": "subscribe", "strategy_ids": ["<strategy-uuid>"] }
{ "action": "subscribe", "run_ids": ["<run-uuid>"] }
{ "action": "unsubscribe", "strategy_ids": ["<strategy-uuid>"] }
{ "action": "subscribe_all" }
{ "action": "unsubscribe_all" }
```

### Server event envelope

```json
{
  "type": "pipeline_start",
  "strategy_id": "11111111-1111-1111-1111-111111111111",
  "run_id": "22222222-2222-2222-2222-222222222222",
  "data": {},
  "timestamp": "2026-04-02T09:30:00Z"
}
```

### Event types emitted by the current hub

- `pipeline_start`
- `agent_decision`
- `debate_round`
- `signal`
- `order_submitted`
- `order_filled`
- `position_update`
- `circuit_breaker`
- `error`

## Related

- [[../reference/api.md|API Reference]]
- [[backend/websocket-server]]
- [[frontend/frontend-overview]]
- [[system-architecture]]
