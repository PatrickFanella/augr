# Frontend UI/API Implementation Brief

This document is a handoff for a frontend engineer building a full-featured Augr trading UI against the implemented Go API.

Sources of truth:

- API routes: `internal/api/server.go`
- Auth handlers: `internal/api/auth_handlers.go`
- Response envelopes: `internal/api/responses.go`
- Settings schema: `internal/api/settings.go`
- WebSocket events: `internal/api/hub.go`, `internal/api/websocket.go`
- Existing API overview: `docs/design/api-design.md`

## 1. Product goal

Build an authenticated trading operations dashboard for monitoring strategies, pipeline runs, portfolio exposure, orders/trades, risk controls, market intelligence, Polymarket/Kalshi activity, backtests, automation, settings, and realtime events.

The UI should support three modes:

1. **Operator cockpit** — fast status scanning, alerts, kill switches, current exposure, active runs, recent decisions.
2. **Research workbench** — inspect markets, strategy performance, run snapshots, decisions, conversations, memories, signals, discovery results, and backtests.
3. **Admin/settings console** — user account, API keys, LLM provider settings, risk settings, prompts, automation jobs, audit logs.

## 2. Suggested frontend stack

Existing project docs name the frontend stack as **React 19 + Vite + TypeScript**.

Recommended additions:

- Router: React Router or TanStack Router.
- Data fetching/cache: TanStack Query.
- Tables: TanStack Table or AG Grid for dense operations views.
- Forms: React Hook Form + Zod.
- Charts: Recharts, ECharts, or Visx.
- Realtime: one shared WebSocket client with typed event dispatch.
- API typing: hand-written TypeScript types initially; generate later if OpenAPI is added.

## 3. Base URLs and API conventions

```text
REST base:  /api/v1
WebSocket:  /ws
Ops:        /healthz, /health, /metrics
```

All JSON requests should send:

```http
Content-Type: application/json
Accept: application/json
```

### List response envelope

Most collection endpoints return:

```ts
type ListResponse<T> = {
  data: T[];
  total?: number;
  limit: number;
  offset: number;
};
```

Default pagination behavior in the API is `limit=50`, maximum `limit=100`, `offset=0`.

Treat all list endpoints as offset-paginated. `total` is present only when the handler/repository can compute it; if `total` is absent, show “Load more” or infinite scroll rather than numbered page counts.

### Error envelope

```ts
type ApiError = {
  error: string;
  code:
    | 'ERR_BAD_REQUEST'
    | 'ERR_NOT_FOUND'
    | 'ERR_NOT_IMPLEMENTED'
    | 'ERR_INTERNAL'
    | 'ERR_VALIDATION'
    | 'ERR_METHOD_NOT_ALLOWED'
    | 'ERR_UNAUTHORIZED'
    | 'ERR_RATE_LIMITED'
    | 'ERR_CONFLICT';
};
```

UI behavior:

- Show validation and conflict errors inline when they map to forms.
- Show unauthorized errors by redirecting to login after trying token refresh once.
- Show `501 ERR_NOT_IMPLEMENTED` as “Not configured on this server” rather than a crash.
- Show destructive-action confirmations for kill switches, strategy deletion, API key revoke, and breaker reset.

## 4. Authentication and session handling

### REST auth

Public endpoints:

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/register`

Every other `/api/v1/*` route is protected by `authMiddleware`, except guest read-only behavior exists for selected market/news/calendar/universe/options GET endpoints. Treat the product as authenticated by default.

Supported credentials:

```http
Authorization: Bearer <jwt>
X-API-Key: <api_key>
```

Browser UI should use bearer JWTs. API keys are for programmatic access and account management screens.

### Guest read-only endpoints

The backend allows unauthenticated `GET` access to a limited observation surface. The main dashboard should still require login unless a public market-observer mode is intentionally designed.

| Method | Path/prefix | Guest access |
| --- | --- | --- |
| `GET` | `/news` | Yes |
| `GET` | `/market/ohlcv/{ticker}` | Yes |
| `GET` | `/social/sentiment/{ticker}` | Yes |
| `GET` | `/calendar/earnings` | Yes |
| `GET` | `/calendar/economic` | Yes |
| `GET` | `/calendar/filings` | Yes |
| `GET` | `/calendar/ipo` | Yes |
| `GET` | `/universe` | Yes |
| `GET` | `/universe/watchlist` | Yes |
| `GET` | `/options/chain/{underlying}` | Yes |
| `GET` | `/options/contracts/{symbol}/bars` | Yes |

### Auth payloads

```ts
type LoginRequest = { username: string; password: string };
type RefreshRequest = { refresh_token: string };
type LoginResponse = {
  access_token: string;
  refresh_token: string;
  expires_at: string; // ISO timestamp
};

type User = {
  id: string;
  username: string;
  created_at: string;
  updated_at: string;
};
```

Endpoints:

| Method | Path | Use |
| --- | --- | --- |
| `POST` | `/auth/login` | Login form |
| `POST` | `/auth/register` | Registration flow, if enabled in product |
| `POST` | `/auth/refresh` | Silent refresh |
| `GET` | `/me` | Current user/account header |
| `PATCH` | `/me` | Change password: `{current_password, new_password}` |

### Token storage recommendation

Prefer an in-memory access token plus refresh token in secure storage appropriate to deployment. If localStorage is used for a first version, isolate token access in one auth module and document the XSS tradeoff.

### API key management

```ts
type APIKey = {
  id: string;
  name: string;
  key_prefix: string;
  rate_limit_per_minute: number;
  last_used_at?: string;
  expires_at?: string;
  revoked_at?: string;
  created_at: string;
  updated_at: string;
};
```

| Method | Path | Use |
| --- | --- | --- |
| `GET` | `/api-keys` | List keys |
| `POST` | `/api-keys` | Create key: `{name, expires_at?}`; response includes one-time plaintext `key` |
| `DELETE` | `/api-keys/{id}` | Revoke key |

When creating a key, show the plaintext key once with copy-to-clipboard and a warning that it cannot be recovered.

## 5. Realtime WebSocket

Endpoint: `GET /ws`

Authentication options:

- `Authorization: Bearer <jwt>`
- `X-API-Key: <api_key>`
- `?token=<jwt>`
- `?api_key=<api_key>`

For browsers, use `?token=<access_token>` because WebSocket constructors cannot set arbitrary headers.

### Client commands

```ts
type WSClientCommand =
  | { action: 'subscribe'; strategy_ids?: string[]; run_ids?: string[] }
  | { action: 'unsubscribe'; strategy_ids?: string[]; run_ids?: string[] }
  | { action: 'subscribe_all' }
  | { action: 'unsubscribe_all' }
  | { action: 'subscribe_polymarket' }
  | { action: 'unsubscribe_polymarket' };
```

### Server event envelope

```ts
type WebSocketEvent = {
  type:
    | 'pipeline_start'
    | 'agent_decision'
    | 'debate_round'
    | 'signal'
    | 'order_submitted'
    | 'order_filled'
    | 'position_update'
    | 'circuit_breaker'
    | 'error'
    | 'pipeline_health'
    | 'polymarket_whale_trade'
    | 'polymarket_price_move'
    | 'polymarket_account_tracked';
  strategy_id?: string;
  run_id?: string;
  data?: unknown;
  timestamp: string;
};
```

UI requirements:

- Maintain one shared connection per authenticated session.
- Reconnect with exponential backoff: start at 1s, then 2s, 4s, 8s, max 30s. Continue retrying while the user remains authenticated, but show a degraded realtime state after 5 failed attempts.
- Refresh token before reconnect if the current access token is expired or expires within the next 60 seconds.
- Re-send active subscriptions after reconnect.
- Keep a bounded event buffer for the global activity feed.
- Suggested buffer size: 250 global events, with per-screen filters derived from that stream plus cached API data.
- Patch visible query caches when events reference known entities.
- Show connection state in the app shell.
- Allow screen-level subscriptions for strategy detail and run detail pages.

## 6. Core domain types

These are enough to start building typed views; add endpoint-specific refinements as the frontend discovers payloads.

```ts
type UUID = string;
type ISODate = string;

type MarketType = 'stock' | 'crypto' | 'polymarket' | 'kalshi' | 'options';
type StrategyStatus = 'active' | 'paused' | 'inactive';
type OrderSide = 'buy' | 'sell';
type OrderType = 'market' | 'limit' | 'stop' | 'stop_limit' | 'trailing_stop';
type OrderStatus = 'pending' | 'submitted' | 'partial' | 'filled' | 'cancelled' | 'rejected';
type RiskStatus = 'normal' | 'warning' | 'breached';
type CircuitBreakerPhase = 'open' | 'tripped' | 'cooldown';

type Strategy = {
  id: UUID;
  name: string;
  description?: string;
  ticker: string;
  market_type: MarketType;
  schedule_cron?: string;
  config: unknown;
  status: StrategyStatus;
  skip_next_run: boolean;
  is_paper: boolean;
  created_at: ISODate;
  updated_at: ISODate;
  latest_run_summary?: StrategyLatestRunSummary;
};

type StrategyLatestRunSummary = {
  id: UUID;
  strategy_id: UUID;
  ticker: string;
  status: PipelineStatus;
  signal?: PipelineSignal;
  started_at: ISODate;
  completed_at?: ISODate;
};

type PipelineStatus = 'running' | 'completed' | 'failed' | 'cancelled';
type PipelineSignal = 'buy' | 'sell' | 'hold';

type PipelineRun = {
  id: UUID;
  strategy_id: UUID;
  ticker: string;
  trade_date: ISODate;
  status: PipelineStatus;
  signal?: PipelineSignal;
  started_at: ISODate;
  completed_at?: ISODate;
  error_message?: string;
  config_snapshot?: unknown;
  phase_timings?: unknown;
};

type PositionSide = 'long' | 'short';

type Position = {
  id: UUID;
  strategy_id?: UUID;
  market_type?: MarketType;
  ticker: string;
  side: PositionSide;
  quantity: number;
  avg_entry: number;
  current_price?: number;
  unrealized_pnl?: number;
  realized_pnl: number;
  stop_loss?: number;
  take_profit?: number;
  opened_at: ISODate;
  closed_at?: ISODate;
  asset_class?: string;
  underlying_ticker?: string;
  option_type?: string;
  strike?: number;
  expiry?: ISODate;
  contract_multiplier?: number;
  leg_group_id?: UUID;
  delta?: number;
  gamma?: number;
  theta?: number;
  vega?: number;
};

type Order = {
  id: UUID;
  strategy_id?: UUID;
  pipeline_run_id?: UUID;
  external_id?: string;
  ticker: string;
  market_type?: MarketType;
  side: OrderSide;
  order_type: OrderType;
  quantity: number;
  limit_price?: number;
  stop_price?: number;
  filled_quantity: number;
  filled_avg_price?: number;
  status: OrderStatus;
  broker: string;
  submitted_at?: ISODate;
  filled_at?: ISODate;
  created_at: ISODate;
  asset_class?: string;
  underlying_ticker?: string;
  option_type?: string;
  strike?: number;
  expiry?: ISODate;
  prediction_side?: string;
  polymarket_intent?: string;
};

type Trade = {
  id: UUID;
  order_id?: UUID;
  position_id?: UUID;
  external_id?: string;
  ticker: string;
  side: OrderSide;
  quantity: number;
  price: number;
  fee: number;
  executed_at: ISODate;
  created_at: ISODate;
  asset_class?: string;
  open_close?: 'open' | 'close' | string;
  contract_multiplier?: number;
  premium?: number;
  exit_reason?: string;
};

type RiskSettings = {
  max_position_size_pct: number;
  max_daily_loss_pct: number;
  max_drawdown_pct: number;
  max_open_positions: number;
  max_total_exposure_pct: number;
  max_per_market_exposure_pct: number;
  circuit_breaker_threshold_pct: number;
  circuit_breaker_cooldown_min: number;
};

type RiskEngineStatus = {
  risk_status: RiskStatus;
  circuit_breaker: {
    state: CircuitBreakerPhase;
    reason?: string;
    tripped_at?: ISODate;
    cooldown_end?: ISODate;
  };
  kill_switch: {
    active: boolean;
    reason?: string;
    mechanisms?: ('api_toggle' | 'file_flag' | 'env_var' | 'unknown')[];
    activated_at?: ISODate;
  };
  market_kill_switches?: Partial<Record<MarketType, RiskEngineStatus['kill_switch']>>;
  position_limits: {
    max_per_position_pct: number;
    max_total_pct: number;
    max_concurrent: number;
    max_per_market_pct: number;
    current_open_positions?: number;
    current_total_exposure_pct?: number;
  };
  updated_at: ISODate;
};

type RiskBreakerState = {
  scope: string; // "global" or "strategy:<uuid>"
  tripped_at: ISODate;
  reason: string;
  reset_at?: ISODate;
};

type RiskBreakersResponse = { tripped: RiskBreakerState[] };

type AutomationJobHealth = {
  name: string;
  enabled: boolean;
  running: boolean;
  last_run?: ISODate;
  last_error?: string;
  error_count: number;
  consecutive_failures: number;
  run_count: number;
};

type AutomationHealthResponse = {
  jobs: AutomationJobHealth[];
  healthy: boolean;
  total_jobs: number;
  failing_jobs: number;
  degraded_jobs: number;
};
```

## 7. Information architecture and routes

Recommended app navigation:

```text
/login
/register                         optional
/
/cockpit                          operator landing page
/strategies
/strategies/:id
/runs
/runs/:id
/portfolio
/orders
/trades
/risk
/markets
/markets/polymarket
/markets/kalshi
/research
/signals
/backtests
/automation
/memory
/conversations
/audit-log
/settings
/account
```

Use persistent top-level chrome:

- Left nav grouped by Operations, Markets, Research, Admin.
- Header with environment, schema status, active user, websocket state, kill-switch state.
- Global realtime activity drawer.
- Global error/toast system.

## 8. Screen specifications

### 8.1 Login/register

Endpoints:

- `POST /auth/login`
- `POST /auth/register`
- `POST /auth/refresh`

Requirements:

- Username/password login.
- Do not build public registration unless product explicitly enables it; the backend route exists, but production availability is an open decision.
- Store token pair and expiry.
- Redirect authenticated users to `/cockpit`.
- Handle invalid credentials without revealing whether username exists.

### 8.2 Operator cockpit

Purpose: single screen for “is the trading system healthy and safe?”

Primary endpoints:

- `GET /risk/status`
- `GET /risk/cockpit`
- `GET /risk/breakers`
- `GET /portfolio/summary`
- `GET /portfolio/positions/open`
- `GET /runs?status=running`
- `GET /orders?status=open` or equivalent status filters supported by backend data
- `GET /automation/status`
- `GET /automation/health`
- `GET /marketdata/polymarket/status`
- WebSocket `subscribe_all`, `subscribe_polymarket`

Widgets:

- System health strip: API, websocket, automation health, broker connectivity.
- Risk banner: global kill switch, breaker state, per-market stop/resume state.
- P&L summary: realized/unrealized, open exposure, open position count.
- Active runs table.
- Recent orders/trades table.
- Live event stream.
- Top signals/alerts.

Actions:

- Trigger global kill switch: `POST /risk/killswitch` with `{active: true, reason}`.
- Deactivate global kill switch: `POST /risk/killswitch` with `{active: false}`.
- Market stop/resume: `POST /risk/market/{type}/stop`, `POST /risk/market/{type}/resume`.
- Cancel run: `POST /runs/{id}/cancel`.

### 8.3 Strategies

Endpoints:

| Method | Path | UI use |
| --- | --- | --- |
| `GET` | `/strategies` | Strategy table; filters: `ticker`, `market_type`, `status`, `is_paper` |
| `POST` | `/strategies` | Create strategy |
| `GET` | `/strategies/{id}` | Detail page |
| `PUT` | `/strategies/{id}` | Edit strategy |
| `DELETE` | `/strategies/{id}` | Delete strategy |
| `POST` | `/strategies/{id}/run` | Manual run |
| `POST` | `/strategies/{id}/pause` | Pause active strategy |
| `POST` | `/strategies/{id}/resume` | Resume paused strategy |
| `POST` | `/strategies/{id}/skip-next` | Skip next scheduled run |
| `GET` | `/strategies/{id}/reports/latest` | Latest report panel |
| `GET` | `/strategies/{id}/reports` | Report history |

List columns:

- Name, ticker, market type, paper/live, status, schedule, skip next, latest signal, latest run status, updated time.

Detail tabs:

- Overview
- Config JSON editor/form
- Runs
- Reports
- Orders/trades
- Risk/exposure
- Realtime events

Create/edit form:

- Required: `name`, `ticker`, `market_type`.
- Defaults: `status=active` if omitted; `is_paper=true` recommended default in UI.
- `config` is flexible JSON. First version can use a JSON editor with validation.

### 8.4 Pipeline runs and decisions

Endpoints:

| Method | Path | UI use |
| --- | --- | --- |
| `GET` | `/runs` | Run table; filters: `strategy_id`, `ticker`, `status`, `start_date`, `end_date` |
| `GET` | `/runs/{id}` | Run detail |
| `GET` | `/runs/{id}/decisions` | Agent decisions; query: `include_prompt`, `limit`, `offset` |
| `POST` | `/runs/{id}/cancel` | Cancel running run |
| `GET` | `/runs/{id}/snapshot` | Market/input snapshots grouped by `data_type` |
| `GET` | `/journal/decisions` | Decision journal |
| `GET` | `/journal/decisions/{id}` | Decision detail |
| `GET` | `/replay/decisions/{id}` | Replay workbench detail |

Run detail layout:

- Summary: status, signal, ticker, strategy, trade date, duration, error.
- Timeline: WebSocket events + persisted events from `/events`.
- Decisions: agent, phase, confidence, reasoning, prompt toggle.
- Snapshot inspector: market data/raw payload by `data_type`.
- Orders/trades created from run.
- Replay panel for decision review.

### 8.5 Portfolio

Endpoints:

| Method | Path | UI use |
| --- | --- | --- |
| `GET` | `/portfolio/summary` | KPI cards |
| `GET` | `/portfolio/positions` | All positions; filters: `ticker`, `side` |
| `GET` | `/portfolio/positions/open` | Current exposure |
| `GET` | `/portfolio/allocator/diagnostics` | Allocator health/debug |
| `GET` | `/portfolio/allocator/opportunities` | Candidate allocations |
| `GET` | `/portfolio/allocator/decisions` | Allocation decision log |
| `GET` | `/portfolio/allocator/summary` | Allocator summary |

UI components:

- P&L cards.
- Exposure by market type/ticker/strategy.
- Positions table with open/closed filter.
- Option greeks columns when present.
- Allocator diagnostics page for debugging sizing decisions.

### 8.6 Orders and trades

Endpoints:

- `GET /orders` with filters `ticker`, `status`, `side`
- `GET /orders/{id}` returns `{order, fills}`
- `GET /trades` with filters `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date`

Requirements:

- Dense filterable tables.
- Link order rows to strategy, run, fills, and position where IDs exist.
- Show broker external IDs.
- Highlight partial fills, rejects/failures if statuses expose them.
- Support asset-specific columns for options and prediction markets.

### 8.7 Risk console

Endpoints:

| Method | Path | UI use |
| --- | --- | --- |
| `GET` | `/risk/status` | Current risk engine status |
| `GET` | `/risk/cockpit` | Cockpit-specific risk aggregate |
| `GET` | `/risk/breakers` | Breaker list/state |
| `POST` | `/risk/killswitch` | Toggle global kill switch: `{active, reason}` |
| `POST` | `/risk/breaker/reset` | Admin-only reset: `{scope}` with `X-Admin-Key` |
| `POST` | `/risk/market/{type}/stop` | Stop a market type |
| `POST` | `/risk/market/{type}/resume` | Resume a market type |

Risk UX requirements:

- Never hide kill-switch controls behind navigation.
- Require reason when activating kill switch.
- Require reason when stopping a market: `POST /risk/market/{type}/stop` body is `{reason}`.
- Require explicit confirmation for resume/reset actions.
- Clearly distinguish paper/live modes.
- Show stale data warnings when websocket is disconnected or API polling fails.
- Avoid browser persistence for `X-Admin-Key`. Prefer a backend-mediated admin session/role in the future. If the current endpoint must be exposed, request the key per reset action in a password-style field, send it once, and discard it immediately.

### 8.8 Market intelligence

Endpoints:

General:

- `GET /market/ohlcv/{ticker}`
- `GET /social/sentiment/{ticker}`
- `GET /news`
- `GET /event-markets/summary`

Options:

- `GET /options/chain/{underlying}`
- `GET /options/contracts/{symbol}/bars`

Calendars:

- `GET /calendar/earnings`
- `GET /calendar/economic`
- `GET /calendar/filings`
- `POST /calendar/filings/analyze`
- `GET /calendar/ipo`

Universe:

- `GET /universe`
- `GET /universe/watchlist`
- `POST /universe/refresh`
- `POST /universe/scan`

UI components:

- Ticker detail page with OHLCV chart, sentiment, news, filings, upcoming calendar events.
- Universe/watchlist tables.
- Options chain with filters by expiry/strike/moneyness.
- Filing analyzer action with job/result feedback.

### 8.9 Polymarket and Kalshi

Polymarket endpoints:

| Method | Path | UI use |
| --- | --- | --- |
| `GET` | `/polymarket/accounts` | Tracked accounts table |
| `GET` | `/polymarket/accounts/{address}` | Account detail |
| `GET` | `/polymarket/accounts/{address}/trades` | Account trades |
| `PATCH` | `/polymarket/accounts/{address}/tracked` | Track/untrack account |
| `GET` | `/polymarket/trades/recent` | Recent market trades |
| `GET` | `/polymarket/signals/recent` | Recent signals |
| `GET` | `/polymarket/markets/{slug}` | Market detail |
| `GET` | `/polymarket/watched` | Watched markets |
| `POST` | `/polymarket/watched` | Add watched market |
| `PATCH` | `/polymarket/watched/{slug}` | Edit watched market |
| `DELETE` | `/polymarket/watched/{slug}` | Remove watched market |
| `GET` | `/polymarket/jobs/status` | Job status |
| `GET` | `/polymarket/discovery/last` | Last discovery result |
| `POST` | `/polymarket/discovery/run` | Start discovery |
| `GET` | `/marketdata/polymarket/status` | Data provider status |

Kalshi endpoints:

- `GET /kalshi/summary`

Requirements:

- Dedicated prediction-market workspace.
- Account tracking with clear address display/copy.
- Watched markets management by slug.
- Discovery run button with progress/status.
- Subscribe to Polymarket WebSocket events for whale trades, price moves, tracked accounts.

### 8.10 Research, discovery, and signals

Endpoints:

- `GET /research/options/opportunities/{underlying}`
- `GET /research/polymarket/opportunities`
- `POST /discovery/run`
- `GET /discovery/results`
- `GET /signals/evaluated`
- `GET /signals/triggers`
- `GET /signals/watchlist`
- `POST /signals/watchlist`
- `DELETE /signals/watchlist/{term}`

UI components:

- Research opportunity cards/tables.
- Discovery result history.
- Signal evaluation feed.
- Trigger log table.
- Watch term manager.

### 8.11 Backtests

Endpoints:

| Method | Path | UI use |
| --- | --- | --- |
| `GET` | `/backtest/divergence` | Legacy divergence endpoint |
| `GET` | `/backtests/divergence` | Divergence results |
| `GET` | `/backtests/configs` | List configs |
| `POST` | `/backtests/configs` | Create config |
| `GET` | `/backtests/configs/{id}` | Config detail |
| `PUT` | `/backtests/configs/{id}` | Update config |
| `DELETE` | `/backtests/configs/{id}` | Delete config |
| `POST` | `/backtests/configs/{id}/run` | Start run |
| `GET` | `/backtests/runs` | List runs |
| `GET` | `/backtests/runs/{id}` | Run detail |

Requirements:

- Config CRUD with JSON-capable advanced fields.
- Run history table.
- Result charts and metrics.
- Compare runs/configs when data shape supports it.

### 8.12 Automation

Endpoints:

- `GET /automation/status`
- `GET /automation/health`
- `GET /automation/runs`
- `GET /automation/alpaca/verify`
- `POST /automation/alpaca/reconcile`
- `POST /automation/jobs/{name}/run`
- `POST /automation/jobs/{name}/enable`

Requirements:

- Job status table.
- Manual run action per job.
- Enable/disable action per job.
- Alpaca reconciliation verify/run actions.
- Audit-friendly confirmation around live broker reconciliation.

### 8.13 Memory and conversations

Memory endpoints:

- `GET /memories` with optional `q`, `agent_role`
- `POST /memories/search` with `{query}`
- `DELETE /memories/{id}`

Conversation endpoints:

- `GET /conversations` with `pipeline_run_id`, `agent_role`
- `POST /conversations`
- `GET /conversations/{id}/messages`
- `POST /conversations/{id}/messages`

Requirements:

- Searchable memory browser.
- Delete memory with confirmation.
- Conversation list linked to runs/agents.
- Message view with assistant/user distinction.
- Gracefully handle `501` if conversation LLM or repo is not configured.

### 8.14 Events and audit log

Endpoints:

- `GET /events` filters: `event_kind`, `pipeline_run_id`, `strategy_id`, `agent_role`, `after`, `before`
- `GET /audit-log` filters: `event_type`, `entity_type`, `after`, `before`

Requirements:

- Immutable audit table.
- Filter by actor/entity/event/time.
- Event payload JSON viewer.
- Link events to strategies/runs when IDs exist.

### 8.15 Settings and prompts

Settings endpoints:

- `GET /settings`
- `PUT /settings`

Prompt endpoints:

- `GET /prompts`
- `PUT /prompts`

Settings response shape:

```ts
type SettingsResponse = {
  llm: {
    default_provider: string;
    deep_think_model: string;
    quick_think_model: string;
    providers: {
      openai: LLMProvider;
      anthropic: LLMProvider;
      google: LLMProvider;
      openrouter: LLMProvider;
      xai: LLMProvider;
      ollama: LLMProvider;
    };
  };
  risk: RiskSettings;
  system: {
    environment: string;
    version: string;
    current_schema_version: number;
    required_schema_version: number;
    schema_status: string;
    uptime_seconds: number;
    connected_brokers: { name: string; paper_mode: boolean; configured: boolean }[];
  };
};

type LLMProvider = {
  api_key_configured: boolean;
  api_key_last4?: string;
  base_url?: string;
  model: string;
};
```

Settings update shape:

```ts
type SettingsUpdateRequest = {
  llm: {
    default_provider: string;
    deep_think_model: string;
    quick_think_model: string;
    providers: Record<string, { api_key?: string; base_url?: string; model: string }>;
  };
  risk: RiskSettings;
};
```

Secret handling:

- API never returns raw provider keys.
- Empty/omitted `api_key` should preserve existing secrets.
- UI should show “configured” + last four only.
- Provide explicit “replace key” action per provider.

## 9. API client requirements

Create a single API layer with these capabilities:

- Base URL configuration.
- Bearer token injection.
- Refresh-once flow for `401`.
- Typed `get/post/put/patch/delete` helpers.
- Standard parsing of `ApiError`.
- Pagination helpers for `ListResponse<T>`.
- Query key factory for each resource.
- Request cancellation via `AbortSignal`.
- One place to map `501` to feature-unavailable UI states.

Suggested query invalidation:

- Strategy mutations invalidate `strategies`, specific `strategy`, `runs` for that strategy.
- Run cancel invalidates `runs`, specific `run`, `events`.
- Risk mutations invalidate `risk/status`, `risk/cockpit`, `risk/breakers`.
- Settings/prompts mutations invalidate corresponding singleton query.
- API key create/revoke invalidates `api-keys`.
- Watchlist mutations invalidate relevant watchlist and signal queries.

## 10. Screen-to-endpoint dependency matrix

| Screen | Primary endpoints | MVP priority | Notes |
| --- | --- | --- | --- |
| Login | `/auth/login`, `/auth/refresh`, `/me` | P0 | Registration is optional/not assumed. |
| Cockpit | `/risk/status`, `/risk/cockpit`, `/risk/breakers`, `/portfolio/summary`, `/runs`, `/automation/health`, `/ws` | P0 | Highest-risk screen; must handle stale/offline states. |
| Strategies | `/strategies`, `/strategies/{id}`, strategy action endpoints | P0 | JSON config editor acceptable for v1. |
| Runs | `/runs`, `/runs/{id}`, `/runs/{id}/decisions`, `/runs/{id}/snapshot`, `/events` | P0 | Needed to explain agent behavior. |
| Portfolio | `/portfolio/summary`, `/portfolio/positions`, allocator endpoints | P0 | Core exposure view. |
| Orders/trades | `/orders`, `/orders/{id}`, `/trades` | P0 | Core execution audit. |
| Risk | `/risk/status`, `/risk/cockpit`, `/risk/breakers`, risk mutation endpoints | P0 | Destructive actions require confirmation. |
| Settings/account | `/settings`, `/prompts`, `/me`, `/api-keys` | P1 | Secrets are write-only from UI perspective. |
| Automation | `/automation/status`, `/automation/health`, `/automation/runs`, job action endpoints | P1 | Some deployments may return service-unavailable if not configured. |
| Polymarket/Kalshi | `/polymarket/*`, `/kalshi/summary`, `/marketdata/polymarket/status` | P2 | Use websocket Polymarket subscriptions. |
| Research/signals | `/research/*`, `/discovery/*`, `/signals/*` | P2 | Research workbench. |
| Backtests | `/backtests/*`, `/backtest/divergence` | P2 | Advanced workflow. |
| Memory/conversations | `/memories/*`, `/conversations/*` | P2 | May return `501` if repos/LLM are not configured. |
| Audit log | `/audit-log`, `/events` | P1 | Important for operator accountability. |

## 11. Visual/UX requirements

This product is operational and risk-sensitive; optimize for clarity over decoration.

Required states for every data surface:

- Loading skeleton.
- Empty state with next action.
- Error state with retry.
- Stale/offline indicator.
- Last updated timestamp.

Design priorities:

- Dense but scannable tables.
- Strong status colors, with icons/text for accessibility.
- Sticky critical controls for risk/kill switch.
- Clear paper-vs-live labeling.
- JSON inspectors for flexible payloads.
- Deep links for all important entities.
- Copy buttons for IDs, addresses, API keys, and market slugs.

Accessibility:

- Keyboard-navigable tables/actions.
- Focus traps in dialogs.
- Non-color status labels.
- Confirmation dialogs announce destructive consequences.
- Time values should expose full ISO timestamp on hover/title.

## 12. Implementation phases

### Phase 1 — foundation

- App shell, routing, auth, API client, token refresh.
- WebSocket client and global activity drawer.
- Shared table, filters, pagination, JSON viewer, confirmation dialog, toast system.

### Phase 2 — operator MVP

- Cockpit.
- Strategies list/detail/actions.
- Runs list/detail/decisions/snapshots.
- Portfolio summary/open positions.
- Orders/trades tables.
- Risk console with kill switches.

### Phase 3 — research and markets

- Market intelligence ticker pages.
- Polymarket/Kalshi workspace.
- Research opportunities.
- Signals/watchlist.
- Discovery results.

### Phase 4 — admin and advanced workflows

- Settings/prompts.
- API keys/account.
- Automation console.
- Backtests.
- Memory/conversations.
- Audit log.

## 13. Backend gaps or clarification requests

Ask the backend developer to confirm or provide:

1. OpenAPI spec or generated JSON schema for all endpoint payloads.
2. Exact request/response shapes for Polymarket, Kalshi, risk cockpit, prompts, automation, backtests, discovery, signals, and market-data endpoints.
3. Stable enum values for order side/type/status and option-specific fields.
4. Whether registration should be available in production UI; do not include it by default.
5. Whether API keys should be user-scoped; current API key listing appears server-wide.
6. Admin key UX for `POST /risk/breaker/reset`; browser storage of `X-Admin-Key` is sensitive and should be avoided if possible.
7. CORS and deployment origin expectations.
8. Whether guest read-only access should be exposed intentionally in the frontend.
9. Treat `/metrics` as Prometheus/Grafana-only unless product explicitly asks for a metrics UI.
10. Whether all mutating actions are safe to retry or need idempotency keys.

## 14. Acceptance criteria for the UI

- Authenticated user can log in, refresh session, log out, and change password.
- User can see live system health, risk state, active runs, open positions, and recent events on the cockpit.
- User can create/edit/pause/resume/run/delete strategies.
- User can inspect a run’s timeline, agent decisions, snapshots, and related orders/trades.
- User can inspect portfolio exposure, orders, trades, and allocation decisions.
- User can activate/deactivate risk controls with clear confirmations.
- User can inspect Polymarket/Kalshi and market intelligence surfaces supported by the API.
- User can manage backtest configs/runs, discovery, signals, automation, prompts, settings, API keys, memories, conversations, and audit logs.
- WebSocket events update visible views or appear in a global event feed.
- All screens handle loading, empty, error, stale, and `501 not configured` states gracefully.
- No secret values returned by the API are displayed or stored unsafely by the UI.
