---
title: "API Reference"
description: "REST and WebSocket reference for the current get-rich-quick API server."
status: "canonical"
updated: "2026-04-08"
tags: [api, rest, websocket, reference]
---

# API Reference

Canonical route sources:

- `internal/api/server.go`
- `internal/api/handlers.go`
- `internal/api/auth.go`
- `internal/api/websocket.go`
- `internal/api/settings.go`

## Base URLs

```text
REST API:   http://localhost:8080/api/v1
WebSocket:  ws://localhost:8080/ws
Ops:        http://localhost:8080/healthz
            http://localhost:8080/health
            http://localhost:8080/metrics
```

## Authentication model

### Public endpoints

- `GET /healthz`
- `GET /health`
- `GET /metrics`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/register`
- `GET /ws`

### Protected endpoints

Everything else under `/api/v1/*` requires one of:

```http
Authorization: Bearer <access_token>
```

or

```http
X-API-Key: <api_key>
```

### Auth notes

- JWT access and refresh tokens are minted by `AuthManager`.
- API keys are supported through `X-API-Key` header or `?api_key=` query param.
- API keys are subject to a per-key token-bucket rate limiter.
- WebSocket upgrades require the same credentials: `Authorization: Bearer`, `X-API-Key`, `?token=`, or `?api_key=` query params (for browsers that cannot send custom headers).

## Common response shapes

### Error envelope

```json
{
  "error": "authentication required",
  "code": "ERR_UNAUTHORIZED"
}
```

### List envelope

```json
{
  "data": [],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

Notes:

- `limit` defaults to `50`
- `limit` is capped at `100`
- `total` is populated for: strategies, runs, portfolio/positions, portfolio/positions/open, orders, trades, conversations, api-keys, runs/{id}/decisions, backtests/configs, backtests/runs, audit-log, events, discovery/results
- `total` is omitted (`0`) for: memories (full-text search semantics differ), conversations/{id}/messages (synthetic message injection makes DB count inaccurate)

## Route map

| Group | Routes |
| --- | --- |
| Auth | `POST /auth/login`, `POST /auth/refresh`, `POST /auth/register` |
| Account | `GET /me`, `PATCH /me` (password change) |
| API Keys | `GET /api-keys`, `POST /api-keys`, `DELETE /api-keys/{id}` |
| Strategies | `GET/POST /strategies`, `GET/PUT/DELETE /strategies/{id}`, lifecycle actions under `/run`, `/pause`, `/resume`, `/skip-next` |
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
| Backtests | `GET/POST /backtests/configs`, `GET/PUT/DELETE /backtests/configs/{id}`, `POST /backtests/configs/{id}/run`, `GET /backtests/runs`, `GET /backtests/runs/{id}` |
| Discovery | `POST /discovery/run`, `GET /discovery/results` |
| Universe | `GET /universe`, `GET /universe/watchlist`, `POST /universe/refresh`, `POST /universe/scan` |
| Options | `GET /options/chain/{underlying}` |
| Calendar | `GET /calendar/earnings`, `GET /calendar/economic`, `GET /calendar/ipo`, `GET /calendar/filings`, `POST /calendar/filings/analyze` |
| Automation | `GET /automation/status`, `POST /automation/jobs/{name}/run`, `POST /automation/jobs/{name}/enable` |
| News | `GET /news` |
| Signals | `GET /signals/evaluated`, `GET /signals/triggers`, `GET/POST /signals/watchlist`, `DELETE /signals/watchlist/{term}` |

## Endpoint reference

### Auth

#### `POST /api/v1/auth/login`

- auth: public
- body: `username`, `password`
- behavior: loads the user by username, verifies bcrypt password, and returns access plus refresh tokens

Example:

```json
{
  "username": "demo",
  "password": "demo-pass"
}
```

Response:

```json
{
  "access_token": "eyJhbGciOiJI...",
  "refresh_token": "eyJhbGciOiJI...",
  "expires_at": "2026-04-03T18:40:00Z"
}
```

#### `POST /api/v1/auth/refresh`

- auth: public
- body: `refresh_token`
- behavior: validates the refresh token and returns a new pair

#### `POST /api/v1/auth/register`

- auth: public
- body: `username`, `password`
- behavior: creates a new user and returns a token pair; returns `409 Conflict` for duplicate usernames

### Account

#### `GET /api/v1/me`

- auth: required
- returns the current user's profile (`id`, `username`, `created_at`, `updated_at`)

#### `PATCH /api/v1/me`

- auth: required
- body: `current_password`, `new_password` (minimum 8 characters)
- behavior: verifies the current password, then replaces the bcrypt hash; returns `204 No Content`

### API Keys

#### `GET /api/v1/api-keys`

- auth: required
- returns a paginated list of API key metadata (raw key value is never re-exposed)
- includes revoked keys; check `revoked_at` to filter active keys

#### `POST /api/v1/api-keys`

- auth: required
- body: `name` (required), `expires_at` (optional ISO 8601 timestamp)
- returns the plaintext key **once** in `key` alongside `metadata`; store it securely — it cannot be retrieved again

#### `DELETE /api/v1/api-keys/{id}`

- auth: required
- marks the key as revoked; returns `204 No Content`
- revocation is immediate: any in-flight request using the key will be rejected on next evaluation

### Strategies

#### `GET /api/v1/strategies`

- auth: required
- filters:
  - `ticker`
  - `market_type` (`stock`, `crypto`, `polymarket`, `options`) — invalid value returns `400`
  - `status` (`active`, `paused`, `inactive`) — invalid value returns `400`
  - `is_paper` (`true` / `false`)
  - `limit` / `offset`
- response includes `total` (total matching rows, regardless of pagination)

#### `POST /api/v1/strategies`

- auth: required
- body: `domain.Strategy`
- validation:
  - required `name`
  - required `ticker`
  - valid `market_type`
  - valid `status`
  - valid typed JSON config if `config` is present
  - valid standard cron expression in `schedule_cron` if present — invalid expression returns `400`

#### `GET /api/v1/strategies/{id}`

- auth: required
- returns one strategy

#### `PUT /api/v1/strategies/{id}`

- auth: required
- full update semantics on the strategy record

#### `DELETE /api/v1/strategies/{id}`

- auth: required
- deletes the strategy record

#### `POST /api/v1/strategies/{id}/run`

- auth: required
- behavior:
  - loads the strategy
  - invokes the configured `StrategyRunner`
  - persists and returns the run result
  - broadcasts run, signal, order, and position events over the hub

#### `POST /api/v1/strategies/{id}/pause`

- auth: required
- behavior: updates strategy status to `paused`

#### `POST /api/v1/strategies/{id}/resume`

- auth: required
- behavior: updates strategy status back to `active`

#### `POST /api/v1/strategies/{id}/skip-next`

- auth: required
- behavior: toggles skip-next-run state for scheduled execution

### Runs

#### `GET /api/v1/runs`

- auth: required
- filters:
  - `strategy_id` (UUID)
  - `ticker`
  - `status` (`running`, `completed`, `failed`, `cancelled`) — invalid value returns `400`
  - `trade_date` (RFC3339 timestamp) — filters to runs for that specific trade date
  - `start_date` / `end_date` (RFC3339 timestamp, filter on `started_at`)
  - `limit` / `offset`
- response includes `total` (total matching rows, regardless of pagination)

#### `GET /api/v1/runs/{id}`

- auth: required
- returns the pipeline run summary record

#### `GET /api/v1/runs/{id}/decisions`

- auth: required
- filters: `limit`, `offset`
- enum filters: `agent_role` (e.g. `trader`, `market_analyst`), `phase` (`analysis`, `research_debate`, `trading`, `risk_debate`) — invalid value returns `400`
- query: `include_prompt=true` to include full prompt text in each decision
- returns `total` in list envelope
- returns agent decisions associated with the run

#### `POST /api/v1/runs/{id}/cancel`

- auth: required
- cancellation surface for in-flight runs where supported

#### `GET /api/v1/runs/{id}/snapshot`

- auth: required
- returns persisted run snapshot data for richer inspection/replay

### Portfolio

#### `GET /api/v1/portfolio/positions`

- auth: required
- filters: `ticker`, `limit` / `offset`
- enum filters: `side` (`long`, `short`) — invalid value returns `400`
- response includes `total`

#### `GET /api/v1/portfolio/positions/open`

- auth: required
- filters: `ticker`, `limit` / `offset`
- enum filters: `side` (`long`, `short`) — invalid value returns `400`
- returns only positions where `closed_at` is null

#### `GET /api/v1/portfolio/summary`

- auth: required
- returns `open_positions` (count), `unrealized_pnl`, `realized_pnl`

### Orders and trades

#### `GET /api/v1/orders`

- auth: required
- filters: `ticker`, `broker`, `limit` / `offset`
- enum filters: `status` (`submitted`, `partial`, `filled`, `cancelled`, `rejected`), `side` (`buy`, `sell`), `order_type` (`market`, `limit`, `stop`, `stop_limit`, `trailing_stop`) — invalid values return `400 Bad Request`
- response includes `total`

#### `GET /api/v1/orders/{id}`

- auth: required
- returns the order record plus its fills (trades) in `fills`

#### `GET /api/v1/trades`

- auth: required
- filters: `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date`, `limit` / `offset`
- `order_id` and `position_id` are mutually exclusive
- response includes `total`

### Memories

#### `GET /api/v1/memories`

- auth: required
- filters: `agent_role`, `q` (search query), `limit` / `offset`

#### `POST /api/v1/memories/search`

- auth: required
- body: `{"query": "your natural language query"}`
- returns memories ordered by relevance

#### `DELETE /api/v1/memories/{id}`

- auth: required
- returns `204 No Content`

### Risk

#### `GET /api/v1/risk/status`

- auth: required
- returns:
  - high-level risk status
  - circuit-breaker state
  - kill-switch state
  - position/exposure limits
  - utilization figures

#### `POST /api/v1/risk/killswitch`

- auth: required
- toggles kill switch state
- body typically includes:

```json
{
  "active": true,
  "reason": "operator action"
}
```

### Settings

#### `GET /api/v1/settings`

- auth: required
- returns:
  - LLM default provider and tier model selection
  - provider configuration state with masked API-key info
  - risk threshold settings
  - system environment/version/uptime
  - configured broker summary

#### `PUT /api/v1/settings`

- auth: required
- updates non-secret settings (model selections, provider base URLs, risk thresholds)
- changes are persisted to the `app_settings` DB table and survive restarts
- API keys are never stored; they live only in the in-memory session until overwritten

### Events

#### `GET /api/v1/events`

- auth: required
- filters: `event_kind`, `pipeline_run_id`, `strategy_id`, `agent_role`, `after`, `before` (RFC3339), `limit` / `offset`

### Conversations

#### `GET /api/v1/conversations`

- auth: required
- filters: `pipeline_run_id` (UUID), `limit` / `offset`
- enum filters: `agent_role` (e.g. `trader`, `market_analyst`) — invalid value returns `400`
- returns `total` in list envelope

#### `POST /api/v1/conversations`

- auth: required
- body: `{"pipeline_run_id": "<uuid>", "agent_role": "<role>"}`
- auto-generates a title from the agent role and run ticker

#### `GET /api/v1/conversations/{id}/messages`

- auth: required
- filters: `limit`, `offset`
- at `offset=0` prepends agent pipeline decisions as synthetic assistant messages

#### `POST /api/v1/conversations/{id}/messages`

- auth: required
- body: `{"role": "user", "content": "..."}`
- appends a message; assistant replies are generated via the configured LLM provider

### Audit log

#### `GET /api/v1/audit-log`

- auth: required
- lists audit log entries for critical operator actions
- filters:
  - `event_type` — e.g. `kill_switch.activated`, `strategy.manual_run`, `api_key.created`
  - `entity_type` — e.g. `system`, `strategy`, `api_key`, `user`, `market`
  - `actor` — username who performed the action
  - `entity_id` — UUID of the specific entity affected
  - `after` / `before` — ISO 8601 timestamp bounds on `created_at`
  - `limit` / `offset`
- response includes `total` (total matching entries)

Audited event types:

| Event type | Trigger |
| --- | --- |
| `kill_switch.activated` / `.deactivated` | `POST /risk/killswitch` |
| `market_kill_switch.activated` / `.deactivated` | `POST /risk/markets/{type}/stop\|resume` |
| `settings.updated` | `PUT /settings` |
| `strategy.manual_run` | `POST /strategies/{id}/run` |
| `strategy.paused` / `.resumed` | `POST /strategies/{id}/pause\|resume` |
| `strategy.skip_next` | `POST /strategies/{id}/skip-next` |
| `user.registered` | `POST /auth/register` |
| `api_key.created` / `.revoked` | `POST/DELETE /api-keys` |

Example entry:

```json
{
  "id": "a1b2c3d4-...",
  "event_type": "kill_switch.activated",
  "entity_type": "system",
  "actor": "admin",
  "details": {"reason": "market volatility spike"},
  "created_at": "2026-04-08T14:23:00Z"
}
```

### Backtests

Backtests run the rules-engine pipeline over historical OHLCV bars. They require a strategy with a `rules_engine` key in its config.

#### `GET /api/v1/backtests/configs`

- auth: required
- filters: `strategy_id` (UUID), `limit` / `offset`
- response includes `total`

#### `POST /api/v1/backtests/configs`

- auth: required
- body: `domain.BacktestConfig`
- required fields: `strategy_id`, `start_date`, `end_date`, `simulation.initial_capital`

#### `GET /api/v1/backtests/configs/{id}`

- auth: required
- returns one backtest config

#### `PUT /api/v1/backtests/configs/{id}`

- auth: required
- full update semantics

#### `DELETE /api/v1/backtests/configs/{id}`

- auth: required
- returns `204 No Content`

#### `POST /api/v1/backtests/configs/{id}/run`

- auth: required
- runs the backtest synchronously and persists the result
- **requires** the target strategy to have a `rules_engine` key in its config JSON
- fetches ~400 days of warmup bars before `start_date` for indicator initialization (SMA-200 etc.)
- if the strategy status is `inactive` and the backtest yields a positive Sharpe ratio with at least one trade, the strategy is automatically promoted to `active`
- body: none
- returns a `BacktestRun` record with `metrics`, `trade_log`, and `equity_curve` as JSON blobs

#### `GET /api/v1/backtests/runs`

- auth: required
- filters: `backtest_config_id` (UUID), `limit` / `offset`
- response includes `total`

#### `GET /api/v1/backtests/runs/{id}`

- auth: required
- returns one backtest run record

### Automation

The automation subsystem runs named background jobs on a schedule. Requires `ENABLE_SCHEDULER=true`.

#### `GET /api/v1/automation/status`

- auth: required
- returns a map of job name → status for all registered jobs
- returns `503 Service Unavailable` when automation is not configured

#### `POST /api/v1/automation/jobs/{name}/run`

- auth: required
- triggers the named job immediately, independent of its schedule
- `{name}` must match a registered job (e.g. `ticker_discovery`, `stocktwits_trending`)
- returns `400 Bad Request` with an error message if the name is unknown

#### `POST /api/v1/automation/jobs/{name}/enable`

- auth: required
- body: `{"enabled": true}` or `{"enabled": false}`
- enables or disables the named job's scheduled execution
- returns `400 Bad Request` if the name is unknown

#### `GET /api/v1/automation/health`

- auth: required
- returns structured health summary for all registered automation jobs
- response shape:
  ```json
  {
    "healthy": true,
    "total_jobs": 3,
    "failing_jobs": 0,
    "jobs": [
      {
        "name": "ticker_discovery",
        "enabled": true,
        "running": false,
        "last_run": "2026-04-11T12:00:00Z",
        "last_error": "",
        "error_count": 0,
        "consecutive_failures": 0,
        "run_count": 42
      }
    ]
  }
  ```
- `healthy` is `false` when any job has `consecutive_failures >= 3`
- returns `503 Service Unavailable` when automation is not configured

### News

Requires `newsFeedRepo` to be wired at startup (depends on `FINNHUB_API_KEY` or a news provider).

#### `GET /api/v1/news`

- auth: required
- filters:
  - `ticker` — when present, returns news scoped to that ticker; otherwise returns recent cross-market news
  - `limit` (default `50`)
- returns `503 Service Unavailable` when the news feed is not configured

### Signals

Real-time signal and trigger events from the signal intelligence subsystem. All endpoints return empty results (not errors) when the signal hub is not running.

#### `GET /api/v1/signals/evaluated`

- auth: required
- filters: `min_urgency` (integer, 0–10), `limit` (default 50), `offset`
- returns `StoredSignal` records from the in-memory signal store
- response shape: `{"data": [...], "total": N}`

#### `GET /api/v1/signals/triggers`

- auth: required
- filters: `limit` (default 50), `offset`
- returns trigger log entries from the in-memory store
- response shape: `{"data": [...], "total": N}`

#### `GET /api/v1/signals/watchlist`

- auth: required
- returns all active watch terms (ticker symbols, keywords, strategy-scoped terms)
- response shape: `{"data": [...]}`

#### `POST /api/v1/signals/watchlist`

- auth: required
- body: `{"term": "AAPL", "strategy_id": "<uuid>"}` (`strategy_id` optional)
- adds a manual watch term to the signal index
- returns `201 Created` with `{"term": "..."}`

#### `DELETE /api/v1/signals/watchlist/{term}`

- auth: required
- removes a manual watch term
- returns `204 No Content`

### Calendar

All calendar endpoints require a configured `eventsProvider` (Finnhub is the primary source). Returns `501 Not Implemented` otherwise.

Date parameters use `YYYY-MM-DD` format. Earnings and IPO endpoints default to `now` → `now+7days` when dates are omitted.

#### `GET /api/v1/calendar/earnings`

- auth: required
- filters: `from`, `to` (YYYY-MM-DD)
- returns upcoming earnings releases for all tracked tickers

#### `GET /api/v1/calendar/economic`

- auth: required
- returns the upcoming economic event calendar (no date filter — provider returns its own window)

#### `GET /api/v1/calendar/ipo`

- auth: required
- filters: `from`, `to` (YYYY-MM-DD)
- returns expected IPO filings in the date window

#### `GET /api/v1/calendar/filings`

- auth: required
- filters: `ticker`, `form` (e.g. `10-K`, `8-K`), `from`, `to` (YYYY-MM-DD; defaults to last 30 days)
- returns SEC filings matching the filter

#### `POST /api/v1/calendar/filings/analyze`

- auth: required
- requires LLM provider configured; returns `501 Not Implemented` otherwise
- body: `{ "symbol": "AAPL", "form": "10-K", "url": "https://..." }`
  - `symbol` (string, required) — ticker symbol
  - `form` (string, required) — SEC form type (e.g. `10-K`, `8-K`)
  - `url` (string, required) — direct URL to the filing document
- returns an LLM-generated analysis of the filing (sentiment, key risks, summary)

### Universe

Requires `POLYGON_API_KEY` and `TICKER_DISCOVERY=true` for automatic refresh. The universe repo and universe engine are wired independently — some endpoints may be unavailable when only one is configured.

#### `GET /api/v1/universe`

- auth: required
- filters: `index_group` (e.g. `sp500`, `nasdaq100`), `search` (prefix match), `limit` / `offset`

#### `GET /api/v1/universe/watchlist`

- auth: required
- query: `top` (integer, default 30) — returns the top-N scored tickers
- requires the `Universe` engine (not just the repo)

#### `POST /api/v1/universe/refresh`

- auth: required
- triggers a full constituent refresh from Polygon
- returns `503` when universe engine not configured

#### `POST /api/v1/universe/scan`

- auth: required
- runs an immediate scoring pass over current constituents

### Discovery

Strategy discovery runs a screener → generator → sweep pipeline to identify and deploy new strategies. Requires `POLYGON_API_KEY` and LLM provider.

#### `POST /api/v1/discovery/run`

- auth: required
- triggers a synchronous discovery run
- runs screener (volume, momentum, volatility filters), LLM-based strategy generation, and parameter sweep
- returns `503` when discovery deps not configured

#### `GET /api/v1/discovery/results`

- auth: required
- lists past discovery runs with candidate counts and deployment outcomes
- filters: `limit` / `offset`
- returns `total` in list envelope
- returns `503` when discovery run repo not configured

## WebSocket reference

Endpoint:

```text
GET /ws
```

### Client commands

The server accepts JSON messages shaped like:

```json
{
  "action": "subscribe",
  "strategy_ids": ["..."],
  "run_ids": ["..."]
}
```

Supported actions:

- `subscribe`
- `unsubscribe`
- `subscribe_all`
- `unsubscribe_all`

Acknowledgement format:

```json
{
  "status": "ok",
  "action": "subscribe"
}
```

Error format:

```json
{
  "type": "error",
  "error": "invalid command JSON"
}
```

### Subscription semantics

- `strategy_ids` subscribes to events for one or more strategies
- `run_ids` subscribes to a specific pipeline run
- `subscribe_all` turns on broadcast delivery for all events

### Broadcast event types

The server broadcasts event envelopes for things such as:

- pipeline start
- signal emission
- order submitted
- position update

The frontend run-detail page subscribes by `run_id` while a run is active.

## Notable API caveats

- `PATCH /api/v1/me` only supports password change. There is no username change endpoint.
- API key rate-limit-per-minute is set at creation time using the server default; it cannot be changed after creation without direct DB access.
- Backtest `schedule_cron` fields are executed by the built-in cron engine only when the scheduler is initialized with `WithBacktestScheduling()`.
