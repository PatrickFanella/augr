# Frontend Information Architecture and Access Rules

Source inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/api-implementation-review.md`, and Go API route/auth/realtime sources.

Labels:

- **Confirmed**: supported by reviewed API source or brief.
- **Assumption**: frontend route/navigation proposal only; does not imply backend fields or permissions.
- **Open question**: product/backend decision needed.

## 1. Navigation principles

- Top-level navigation is grouped by **Operations**, **Markets**, **Research**, and **Administration**.
- `/cockpit` is the authenticated landing page because the primary product goal is trading operations safety.
- Critical risk status, environment/schema status, active user, and WebSocket state remain visible in persistent chrome.
- Deep entity pages preserve source context with query state such as `from=`, `strategy_id=`, `run_id=`, `ticker=`, `after=`, and `before=`.
- **Assumption**: frontend authorization has only four access categories until backend roles exist: public, optional guest observer, authenticated, and admin-key action.

## 2. Route inventory by section

### Operations

| Route | Nav label | Parent | Page purpose | Priority | Required auth | Potential authorization | Breadcrumb behavior | Query-string state |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/` | Redirect | None | Send authenticated users to `/cockpit`, unauthenticated to `/login`. | P0 | Conditional | None confirmed | None | `next` for post-login redirect. |
| `/cockpit` | Cockpit | Operations | Operator landing: health, risk, active runs, exposure, recent execution, realtime feed. | P0 | Authenticated | None confirmed | Operations / Cockpit | `alert`, `strategy_id`, `run_id`, `market_type`, `from`. |
| `/strategies` | Strategies | Operations | Filterable strategy inventory and create entry. | P0 | Authenticated | None confirmed | Operations / Strategies | `ticker`, `market_type`, `status`, `is_paper`, `limit`, `offset`. |
| `/strategies/new` | New strategy | Strategies | Create full `domain.Strategy` with JSON config editor. | P0 | Authenticated | None confirmed | Operations / Strategies / New | `from`, `ticker`, `market_type`. |
| `/strategies/:id` | Strategy detail | Strategies | Strategy overview, config, runs, reports, events, linked execution. | P0 | Authenticated | None confirmed | Operations / Strategies / `:id` or strategy name when loaded | `tab`, `run_id`, `report_type`, `from`. |
| `/strategies/:id/edit` | Edit strategy | Strategies | Full-object strategy edit. | P0 | Authenticated | None confirmed | Operations / Strategies / `:id` / Edit | `from`, `tab`. |
| `/runs` | Pipeline runs | Operations | Run list with status/time/strategy/ticker filters. | P0 | Authenticated | None confirmed | Operations / Runs | `strategy_id`, `ticker`, `status`, `start_date`, `end_date`, `trade_date`, `limit`, `offset`. |
| `/runs/:id` | Run detail | Runs | Timeline, decisions, snapshot, events, related execution, cancel. | P0 | Authenticated | None confirmed | Operations / Runs / `:id` | `tab`, `decision`, `include_prompt`, `from`. |
| `/portfolio` | Portfolio | Operations | Exposure, P&L, open/all positions, allocator context. | P0 | Authenticated | None confirmed | Operations / Portfolio | `tab`, `ticker`, `side`, `limit`, `offset`, `strategy_id`. |
| `/orders` | Orders | Operations | Execution orders table with filters. | P0 | Authenticated | None confirmed | Operations / Orders | `ticker`, `broker`, `market_type`, `status`, `side`, `order_type`, `limit`, `offset`, `strategy_id`, `run_id` (frontend context only unless backend filter exists). |
| `/orders/:id` | Order detail | Orders | Order plus fills/trade chain. | P0 | Authenticated | None confirmed | Operations / Orders / `:id` | `from`, `tab`. |
| `/trades` | Trades | Operations | Trade/fill audit table and position/order filtered views. | P0 | Authenticated | None confirmed | Operations / Trades | `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date`, `limit`, `offset`. |
| `/risk` | Risk console | Operations | Risk status/cockpit/breakers and kill switch/market stop/reset actions. | P0 | Authenticated | Breaker reset additionally requires one-shot `X-Admin-Key`. | Operations / Risk | `scope`, `market_type`, `tab`, `from`. |
| `/events` | Runtime events | Operations | Persisted agent/runtime event log with JSON payload inspection. | P1 | Authenticated | None confirmed | Operations / Events | `event_kind`, `pipeline_run_id`, `strategy_id`, `agent_role`, `after`, `before`, `limit`, `offset`. |

### Markets

| Route | Nav label | Parent | Page purpose | Priority | Required auth | Potential authorization | Breadcrumb behavior | Query-string state |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/markets` | Market intelligence | Markets | Market overview, watchlist/universe, calendars, recent news. | P1 | Authenticated by default | Optional guest observer for selected GET data if product enables it. | Markets | `ticker`, `tab`, `from`, `to`. |
| `/markets/:ticker` | Ticker detail | Markets | OHLCV, news, sentiment, filings/calendar, options context. | P1 | Authenticated by default | Optional guest observer for confirmed market/news/calendar/options GETs. | Markets / `:ticker` | `tab`, `timeframe`, `from`, `to`, `provider`, `form`, `expiry`, `type`. |
| `/markets/options/:underlying` | Options chain | Markets | Options chain and bars for underlying/contracts. | P1 | Authenticated by default | Optional guest observer for confirmed options GETs. | Markets / Options / `:underlying` | `expiry`, `type`, `symbol`, `from`, `to`, `timeframe`. |
| `/markets/polymarket` | Polymarket | Markets | Prediction-market provider status, accounts, trades, signals, watched markets, discovery. | P2 | Authenticated | None confirmed | Markets / Polymarket | `tab`, `slug`, `address`, `tracked`, `sort`, `limit`, `offset`. |
| `/markets/polymarket/accounts/:address` | Polymarket account | Polymarket | Account detail and account trades. | P2 | Authenticated | None confirmed | Markets / Polymarket / Accounts / `:address` | `from`, `to`, `limit`. |
| `/markets/polymarket/:slug` | Polymarket market | Polymarket | Market detail by slug. | P2 | Authenticated | None confirmed | Markets / Polymarket / Markets / `:slug` | `from`. |
| `/markets/kalshi` | Kalshi | Markets | Kalshi watched/snapshot/discovery/strategy summary. | P2 | Authenticated | None confirmed | Markets / Kalshi | `tab`. |

### Research

| Route | Nav label | Parent | Page purpose | Priority | Required auth | Potential authorization | Breadcrumb behavior | Query-string state |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/research` | Research workbench | Research | Opportunity scanners and discovery entry. | P2 | Authenticated | None confirmed | Research | `market_type`, `underlying`, `strategy_id`, `limit`, `slug`, `token_id`, `outcome`. |
| `/research/discovery` | Discovery | Research | Run discovery and inspect historical results. | P2 | Authenticated | None confirmed | Research / Discovery | `market_type`, `tickers`, `dry_run`, `limit`, `offset`. |
| `/signals` | Signals | Research | Evaluated signals, trigger log, watch terms. | P2 | Authenticated | None confirmed | Research / Signals | `tab`, `min_urgency`, `term`, `limit`, `offset`. |
| `/backtests` | Backtests | Research | Backtest configs, runs, divergence. | P2 | Authenticated | None confirmed | Research / Backtests | `tab`, `strategy_id`, `backtest_config_id`, `limit`, `offset`. |
| `/backtests/configs/:id` | Backtest config | Backtests | Config detail/edit/run. | P2 | Authenticated | None confirmed | Research / Backtests / Configs / `:id` | `from`, `tab`. |
| `/backtests/runs/:id` | Backtest run | Backtests | Run detail/results. | P2 | Authenticated | None confirmed | Research / Backtests / Runs / `:id` | `from`, `tab`. |
| `/journal/decisions` | Decision journal | Research | Trade decision list/detail, separate from run decisions. | P2 | Authenticated | None confirmed | Research / Decision journal | `strategy_id`, `market_type`, `status`, `created_after`, `created_before`, `limit`, `offset`. |
| `/journal/decisions/:id` | Decision detail | Decision journal | Journal decision plus replay link. | P2 | Authenticated | None confirmed | Research / Decision journal / `:id` | `include_prompt`, `from`. |
| `/memory` | Memory | Research | Search/delete memory records. | P2 | Authenticated | None confirmed | Research / Memory | `q`, `agent_role`, `limit`, `offset`. |
| `/conversations` | Conversations | Research | Conversation list by run/agent and message viewer. | P2 | Authenticated | None confirmed | Research / Conversations | `pipeline_run_id`, `agent_role`, `limit`, `offset`. |
| `/conversations/:id` | Conversation detail | Conversations | Messages and optional message creation. | P2 | Authenticated | None confirmed | Research / Conversations / `:id` | `from`, `offset`, `limit`. |

### Administration

| Route | Nav label | Parent | Page purpose | Priority | Required auth | Potential authorization | Breadcrumb behavior | Query-string state |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/automation` | Automation | Administration | Job status/health/runs/actions and Alpaca verify/reconcile. | P1 | Authenticated | None confirmed; reconcile should be treated as high-risk. | Administration / Automation | `job`, `tab`, `limit`, `offset`. |
| `/settings` | Settings | Administration | LLM/risk/system settings with redacted secrets. | P1 | Authenticated | None confirmed | Administration / Settings | `tab`, `provider`. |
| `/prompts` | Prompts | Administration | Prompt overrides and reset/clear behavior. | P1 | Authenticated | None confirmed | Administration / Prompts | `prompt`, `filter`. |
| `/account` | Account | Administration | Current user, password change, API key management. | P1 | Authenticated | None confirmed; API keys appear server-wide until confirmed. | Administration / Account | `tab`, `limit`, `offset`. |
| `/audit-log` | Audit log | Administration | Immutable audit table and incident review filters. | P1 | Authenticated | None confirmed | Administration / Audit log | `event_type`, `entity_type`, `actor`, `entity_id`, `after`, `before`, `limit`, `offset`. |

### Public and error routes

| Route | Nav label | Parent | Page purpose | Priority | Required auth | Potential authorization | Breadcrumb behavior | Query-string state |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/login` | Login | Public | Username/password auth and session bootstrap. | P0 | Public | None | None | `next`, `reason`. |
| `/register` | Register | Public | Optional registration route. Hidden unless enabled. | P2 | Public if product enables | Backend route exists; product exposure unresolved. | None | `next`. |
| `/observer` | Guest observer | Public | Optional market-only read surface. | P2 | Optional guest | Only confirmed guest GET endpoints; no trading/account data. | Observer | `ticker`, `tab`, `from`, `to`. |
| `/unauthorized` | Unauthorized | Public | Explain missing/invalid credentials or admin key denial. | P0 | Public | None | None | `reason`, `next`. |
| `/not-configured` | Not configured | Public shell | Feature unavailable state for `501`/selected `503`. | P0 | Mirrors source route | None | Source route / Not configured | `feature`, `from`. |
| `*` | Not found | Public shell | Unknown route or entity 404. | P0 | Public shell; entity details may require auth | None | None | `from`. |

## 3. Access rules

### Public

- `/login` is public and posts to `POST /api/v1/auth/login`.
- `/register` is public at backend (`POST /api/v1/auth/register`) but hidden by default. **Open question**: production exposure.
- `/health`, `/healthz`, and `/metrics` are public backend routes. **Assumption**: UI uses `/health`; `/metrics` remains ops/Grafana-only unless requested.

### Authenticated

- Treat every non-auth `/api/v1/*` UI route as authenticated by default.
- Browser UI uses bearer JWT for REST and `?token=` for WebSocket.
- On `401`, try refresh once; on refresh failure, clear session and redirect to `/login?next=...`.

### Optional guest observer

- Confirmed unauthenticated GET surface: `/news`, `/market/ohlcv/{ticker}`, `/social/sentiment/{ticker}`, calendar GETs, `/universe`, `/universe/watchlist`, `/options/chain/{underlying}`, `/options/contracts/{symbol}/bars`.
- **Assumption**: do not expose guest observer in main product until explicitly approved. If enabled, guest routes must not show protected nav, WebSocket, account, risk, strategies, runs, orders, trades, automation, audit, settings, prompts, API keys, research mutations, or filing analysis.

### Admin routes and admin actions

- No role model is confirmed in auth source.
- Only confirmed extra admin gate is `POST /risk/breaker/reset` requiring `X-Admin-Key`.
- **Assumption**: `/risk` remains authenticated, while breaker reset action prompts for one-shot admin key and discards it immediately. Do not persist this key.
- **Open question**: replace header key with role/session-based authorization.

### Redirects

- `/` → `/cockpit` when authenticated; `/login` when unauthenticated.
- Protected route without session → `/login?next=<encoded-current-route>`.
- Authenticated user visiting `/login` → `next` if safe and internal, else `/cockpit`.
- `401` after refresh failure → `/login?reason=session_expired&next=...`.
- Entity `404` → route-local not-found panel if inside a section; global not-found for unknown route.

### Unauthorized

- Standard `ERR_UNAUTHORIZED` means login/session failure except breaker reset, where it can mean invalid/missing `X-Admin-Key`.
- Show breaker-reset authorization failures inline in the reset dialog; do not log or retain entered key.

### Feature not configured

- Map `501 ERR_NOT_IMPLEMENTED` to feature-unavailable panels.
- Also treat documented service-unavailable/not-configured cases as degraded: risk breaker lister missing, admin key missing, events/replay/snapshot/report/conversation/automation providers not configured.
- Feature-not-configured panels should include endpoint, source page, and whether the rest of the page remains usable.

### CORS/preflight risk

- Confirm default CORS currently omits `PATCH`, `X-API-Key`, and `X-Admin-Key` in reviewed source.
- **Assumption**: frontend should surface a deployment error if browser preflight blocks password update, Polymarket patch actions, or breaker reset.

## 4. Major IA decisions

1. Keep `/cockpit` as the only default landing page; research/admin routes are secondary.
2. Put runtime `/events` under Operations and immutable `/audit-log` under Administration.
3. Keep orders and trades separate because order detail has fills while trades have position/time filters.
4. Use query-state tabs rather than inventing backend subresources for details whose shapes are ambiguous.
5. Keep guest observer as P2 and separate from authenticated app until product approves it.

## 5. Blockers before screen specs

1. Confirm role/authorization model or accept “authenticated plus one-shot admin key” for v1.
2. Confirm guest observer exposure.
3. Confirm exact DTOs/enums for domain structs, risk cockpit/status, automation, backtests, research, signals, reports, market data, Polymarket/Kalshi, and WebSocket `data`.
4. Confirm CORS methods/headers for browser deployment.
5. Confirm `settings.llm.providers.*.api_key` empty-string semantics.
