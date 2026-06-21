# Frontend API Implementation Review

Source brief: `docs/frontend-ui-api-brief.md`.

Reviewed Go API sources include `internal/api/server.go`, `auth_handlers.go`, `responses.go`, `settings.go`, `hub.go`, `websocket.go`, and the route handler files registered by `server.go`.

Labels used below:

- **Confirmed**: directly visible in Go API source.
- **Inference**: derived from handler/repository/service names or domain type names; payload fields are not invented.
- **Ambiguous**: backend source does not provide enough frontend-contract detail.

## 1. Route-by-route endpoint inventory

All REST API paths below are under `/api/v1` unless explicitly marked ops/root. `limit`/`offset` use `parsePagination`: default `limit=50`, max `100`, default `offset=0`, except where handlers implement local parsing.

### Ops and realtime

| Method | Path | Auth | Request | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/healthz` | Public | None | `{status, db, redis}` | Confirmed. Returns `503` with degraded dependency flags. |
| `GET` | `/health` | Public | None | `{status, db, redis}` | Same as `/healthz`. |
| `GET` | `/metrics` | Public | None | Prometheus text | Uses configured metrics handler or placeholder. Treat as ops-only. |
| `GET` | `/ws` | Auth required | Header auth or `?token=` / `?api_key=` | WebSocket stream | Browser must use query token. Non-JSON `401` before upgrade. |

WebSocket client commands are confirmed as `{action, strategy_ids?, run_ids?}` where `action` is `subscribe`, `unsubscribe`, `subscribe_all`, `unsubscribe_all`, `subscribe_polymarket`, or `unsubscribe_polymarket`. Acks are `{status:"ok", action}`. Command errors are `{type:"error", error}`. Server event envelope is confirmed as `{type, strategy_id?, run_id?, data?, timestamp}`.

### Authentication, account, API keys

| Method | Path | Auth | Request | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `POST` | `/auth/login` | Public | `{username, password}` | `LoginResponse` | `200`; `Cache-Control: no-store`. Invalid username/password share same message. |
| `POST` | `/auth/register` | Public | `{username, password}` | `LoginResponse` | `201`; creates user and logs `user.registered`. Product availability ambiguous. |
| `POST` | `/auth/refresh` | Public | `{refresh_token}` | `LoginResponse` | Returns new access and refresh tokens. |
| `GET` | `/me` | Protected | None | `domain.User` | Confirmed. Exact JSON fields from domain type should be verified; brief infers `{id, username, created_at, updated_at}`. |
| `PATCH` | `/me` | Protected | `{current_password, new_password}` | `204` no body | New password min length 8. |
| `GET` | `/api-keys` | Protected | `limit`, `offset` | `ListResponse<domain.APIKey>` with `total` | List includes revoked keys; appears server-wide, not user-scoped. |
| `POST` | `/api-keys` | Protected | `{name, expires_at?}` | `{key, metadata}` | `key` is plaintext shown once; `metadata` is `domain.APIKey`. |
| `DELETE` | `/api-keys/{id}` | Protected | UUID path | `204` no body | Revokes key, audit logged. |

`LoginResponse` is confirmed: `{access_token, refresh_token, expires_at}`.

### Strategies and reports

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/strategies` | Protected | `ticker`, `market_type`, `status`, `is_paper`, `limit`, `offset` | `ListResponse<domain.Strategy>` with `total` | `status` accepted: `active`, `paused`, `inactive`. |
| `POST` | `/strategies` | Protected | `domain.Strategy` | `domain.Strategy` | `201`; backend assigns `id`. Config and cron validated. |
| `GET` | `/strategies/{id}` | Protected | UUID path | `domain.Strategy` | 404 if missing. |
| `PUT` | `/strategies/{id}` | Protected | `domain.Strategy` | `domain.Strategy` | Path `id` overwrites body `id`. |
| `DELETE` | `/strategies/{id}` | Protected | UUID path | `204` no body | Destructive. |
| `POST` | `/strategies/{id}/run` | Protected | UUID path | `{status, strategy_id, message}` | `202`; asynchronous; returns `501` if runner not configured. |
| `POST` | `/strategies/{id}/pause` | Protected | UUID path | `domain.Strategy` | Requires current status `active`; conflict otherwise. |
| `POST` | `/strategies/{id}/resume` | Protected | UUID path | `domain.Strategy` | Requires current status `paused`; conflict otherwise. |
| `POST` | `/strategies/{id}/skip-next` | Protected | UUID path | `domain.Strategy` | Requires active strategy. |
| `GET` | `/strategies/{id}/reports/latest` | Protected | `report_type?` default `paper_validation` | Report artifact + `stale_seconds` | Returns `501` if artifacts not configured. |
| `GET` | `/strategies/{id}/reports` | Protected | `report_type?`, `status?`, `limit`, `offset` | `ListResponse<ReportArtifact>` without `total` | Artifact shape from postgres repo not documented in brief. |

### Runs, decisions, journal, replay

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/runs` | Protected | `strategy_id`, `ticker`, `status`, `start_date`, `end_date`, `trade_date`, `limit`, `offset` | `ListResponse<domain.PipelineRun>` with `total` | `start_date/end_date` parse RFC3339Nano; `trade_date` parses RFC3339. |
| `GET` | `/runs/{id}` | Protected | UUID path | `domain.PipelineRun` | 404 if missing. |
| `GET` | `/runs/{id}/decisions` | Protected | `include_prompt`, `agent_role`, `phase`, `limit`, `offset` | `ListResponse<AgentDecision plus prompt_text?>` with `total` | `prompt_text` only included when `include_prompt=true`. |
| `POST` | `/runs/{id}/cancel` | Protected | UUID path | `{status:"cancelled"}` | Service errors may return non-500 with `ERR_BAD_REQUEST`. Retry safety ambiguous. |
| `GET` | `/runs/{id}/snapshot` | Protected | UUID path | object keyed by `data_type` | Values are raw JSON payloads. Returns `501` if snapshots not configured. |
| `GET` | `/journal/decisions` | Protected | `strategy_id`, `market_type`, `status`, `created_after`, `created_before`, `limit`, `offset` | `ListResponse<domain.TradeDecision>` without `total` | `status`: `candidate`, `rejected`, `paper`, `live`, `closed`. |
| `GET` | `/journal/decisions/{id}` | Protected | UUID path | `domain.TradeDecision` | Returns `501` if repo not configured. |
| `GET` | `/replay/decisions/{id}` | Protected | UUID path | `replay.BuildWorkbench(...)` result | Shape ambiguous; depends on replay package. |

### Portfolio, orders, trades, allocation

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/portfolio/summary` | Protected | None | `{open_positions, unrealized_pnl, realized_pnl}` | Confirmed map; only uses first `maxLimit` open positions. |
| `GET` | `/portfolio/positions` | Protected | `ticker`, `side`, `limit`, `offset` | `ListResponse<domain.Position>` with `total` | `side` validates domain enum. |
| `GET` | `/portfolio/positions/open` | Protected | `ticker`, `side`, `limit`, `offset` | `ListResponse<domain.Position>` with `total` | Current exposure list. |
| `GET` | `/portfolio/allocator/diagnostics` | Protected | Not fully reviewed | Diagnostics object | Response shape partially typed in handler; frontend contract still ambiguous. |
| `GET` | `/portfolio/allocator/opportunities` | Protected | `limit`, `offset` | `ListResponse<domain.Opportunity>` with `total` | Returns empty list if repo nil. |
| `GET` | `/portfolio/allocator/decisions` | Protected | `limit`, `offset` | `ListResponse<domain.AllocationDecision>` with `total` | Returns empty list if repo nil. |
| `GET` | `/portfolio/allocator/summary` | Protected | None | allocator summary object | Shape declared in `portfolio_allocator_handlers.go`; needs frontend type extraction. |
| `GET` | `/orders` | Protected | `ticker`, `broker`, `market_type`, `status`, `side`, `order_type`, `limit`, `offset` | `ListResponse<domain.Order>` with `total` | Brief omitted `broker`, `market_type`, `order_type`. |
| `GET` | `/orders/{id}` | Protected | UUID path | `{order, fills}` | `fills` are trades from `GetByOrder`. |
| `GET` | `/trades` | Protected | `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date`, `limit`, `offset` | `ListResponse<domain.Trade>` with `total` | `order_id` and `position_id` cannot be combined. Dates parse RFC3339. |

### Risk

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/risk/status` | Protected | None | risk engine status | Exact shape comes from risk engine; brief type is an inference. |
| `GET` | `/risk/cockpit` | Protected | None | `risk.BuildCockpitSummary(...)` | Shape ambiguous; requires risk package contract. |
| `GET` | `/risk/breakers` | Protected | None | `{tripped}` | `tripped` elements are `domain.RiskBreakerState`. |
| `POST` | `/risk/killswitch` | Protected | `{active, reason}` | `{active}` | `reason` required only when `active=true`. |
| `POST` | `/risk/breaker/reset` | Protected + `X-Admin-Key` | `{scope}` | `{scope, reset, message?}` | Requires `ADMIN_API_KEY`; returns `501` if unset. |
| `POST` | `/risk/market/{type}/stop` | Protected | `{reason}` | `{market_type, active:true}` | No explicit market enum validation in handler beyond non-empty. |
| `POST` | `/risk/market/{type}/resume` | Protected | None | `{market_type, active:false}` | No reason accepted/required. |

### Market intelligence, options, calendar, universe

These `GET` endpoints have confirmed guest read-only access: `/news`, `/market/ohlcv/{ticker}`, `/social/sentiment/{ticker}`, `/calendar/earnings`, `/calendar/economic`, `/calendar/filings`, `/calendar/ipo`, `/universe`, `/universe/watchlist`, `/options/chain/{underlying}`, `/options/contracts/{symbol}/bars`.

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/market/ohlcv/{ticker}` | Guest GET allowed | `timeframe?` default `1d`, `from?`, `to?`, `provider?` | historical OHLCV array | `from/to` format `YYYY-MM-DD`, default last year. |
| `GET` | `/social/sentiment/{ticker}` | Guest GET allowed | `limit?` default 20, max 100 | sentiment array | Uses news feed repo. |
| `GET` | `/news` | Guest GET allowed | `ticker?`, `limit?` | news array | Not `ListResponse`; direct array. |
| `GET` | `/event-markets/summary` | Protected | None | `{providers}` | Provider item fields confirmed: `provider`, `watched_markets`, `active_paper`, `last_run_status`, `live_trading_ready`. |
| `GET` | `/options/chain/{underlying}` | Guest GET allowed | `expiry?`, `type?` | options snapshots | `expiry` format `YYYY-MM-DD`; `type=call|put`. |
| `GET` | `/options/contracts/{symbol}/bars` | Guest GET allowed | required `from`, `to`; `timeframe?` default `1d` | options OHLCV bars | `from/to` format `YYYY-MM-DD`. |
| `GET` | `/calendar/earnings` | Guest GET allowed | `from?`, `to?` | events array | Defaults today to today+30d. |
| `GET` | `/calendar/economic` | Guest GET allowed | None | events array or `[]` | Provider 403 maps to empty array. |
| `GET` | `/calendar/filings` | Guest GET allowed | `ticker`, `form?`, `from?`, `to?` | filings array or `[]` | If no ticker, returns empty array. Default last 30d. |
| `POST` | `/calendar/filings/analyze` | Protected | `{symbol, form, url}` | filing analysis object | Requires LLM provider. Exact analysis shape ambiguous. |
| `GET` | `/calendar/ipo` | Guest GET allowed | `from?`, `to?` | events array | Defaults today to today+30d. |
| `GET` | `/universe` | Guest GET allowed | `index_group?`, `search?`, `limit`, `offset` | `ListResponse<TrackedTicker>` without `total` | Returns `ERR_INTERNAL` if universe repo not configured. |
| `GET` | `/universe/watchlist` | Guest GET allowed | `top?` default 30 | tracked ticker array | Merges open positions if repos configured. |
| `POST` | `/universe/refresh` | Protected | None | `{count}` | Triggers full constituent refresh. |
| `POST` | `/universe/scan` | Protected | None | scored tickers | Uses fixed params internally. |

### Polymarket and Kalshi

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/polymarket/accounts` | Protected | `min_win_rate`, `min_resolved`, `min_volume`, `min_trades`, `sort`, `tracked`, `limit`, `offset` | `{data, limit, offset, total}` | Local parsing: default limit 100; `total` is `len(items)`, not full count. |
| `GET` | `/polymarket/accounts/{address}` | Protected | address path | account | Domain shape ambiguous. |
| `GET` | `/polymarket/accounts/{address}/trades` | Protected | `from?`, `to?`, `limit?` default 200 | `{data, limit, offset:0}` | Invalid dates silently become zero time. |
| `PATCH` | `/polymarket/accounts/{address}/tracked` | Protected | `{tracked}` | `{ok:true}` | Track/untrack account. |
| `GET` | `/polymarket/trades/recent` | Protected | `limit?` default 100 | `{data, limit, offset:0}` | Domain shape ambiguous. |
| `GET` | `/polymarket/signals/recent` | Protected | `min_urgency?`, `limit?` default 100 | `{data, total}` | Filters source to polymarket signal sources. |
| `GET` | `/polymarket/markets/{slug}` | Protected | slug path | prediction market data | Requires polymarket client. |
| `GET` | `/polymarket/watched` | Protected | None | watched markets array | Not enveloped. |
| `POST` | `/polymarket/watched` | Protected | `{slug, note}` | `{ok:true}` | `201`; no visible validation of blank slug. |
| `PATCH` | `/polymarket/watched/{slug}` | Protected | `{enabled}` | `{ok:true}` | Brief says edit watched market; actual request only `enabled`. |
| `DELETE` | `/polymarket/watched/{slug}` | Protected | slug path | `204` no body | Handler ignores repo error. |
| `GET` | `/polymarket/jobs/status` | Protected | None | automation job status array | Only jobs with `polymarket_` prefix. |
| `GET` | `/polymarket/discovery/last` | Protected | None | `{last}` | `last` may be `null`; shape ambiguous. |
| `POST` | `/polymarket/discovery/run` | Protected | None | `{status, message}` | `202`; `automation nil` currently returns `ERR_INTERNAL` not `ERR_NOT_IMPLEMENTED`. |
| `GET` | `/marketdata/polymarket/status` | Protected | None | `PolymarketStatus` | Fields confirmed in `marketdata_handlers.go`. |
| `GET` | `/kalshi/summary` | Protected | None | `KalshiSummaryResponse` | Includes watched markets, latest snapshots, discovery, strategies. |

### Research, discovery, signals

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/research/options/opportunities/{underlying}` | Protected | `limit?` default 5, `strategy_id?`, `expiry?`, `type?` | `ListResponse` with `total` | `expiry` `YYYY-MM-DD`; `type=call|put`. |
| `GET` | `/research/polymarket/opportunities` | Protected | `limit?`, `strategy_id?`, `best_bid?`, `best_ask?`, `probability?`, `ask_depth_usd?`, `ask_size?`, `slug?`, `token_id?`, `outcome?` | `ListResponse` with `total` | Payload item type from service; ambiguous. |
| `POST` | `/discovery/run` | Protected | `{tickers, market_type?, dry_run?, max_winners?}` | discovery result | Requires `tickers`; default `market_type=stock`, `max_winners=3`. Synchronous long-running request. |
| `GET` | `/discovery/results` | Protected | `limit`, `offset` | `ListResponse` with `total` | Historical discovery runs. |
| `GET` | `/signals/evaluated` | Protected | `min_urgency?`, `limit?`, `offset?` | `{data,total}` | If store nil, returns empty success. Max local limit 200. |
| `GET` | `/signals/triggers` | Protected | `limit?`, `offset?` | `{data,total}` | If store nil, returns empty success. |
| `GET` | `/signals/watchlist` | Protected | None | `{data}` | Terms array, possibly empty. |
| `POST` | `/signals/watchlist` | Protected | `{term, strategy_id?}` | `{term}` | Uses nonstandard error codes `signal_hub_unavailable` / `invalid_request`. |
| `DELETE` | `/signals/watchlist/{term}` | Protected | term path | `204` no body | Removes manual term. |

### Backtests and divergence

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/backtest/divergence` | Protected | required `strategy_id` | `{strategy_id, backtest, live, tolerance, max_abs_delta, status}` | Legacy alias. |
| `GET` | `/backtests/divergence` | Protected | required `strategy_id` | same | Uses stub source if no source configured. |
| `GET` | `/backtests/configs` | Protected | `strategy_id?`, `limit`, `offset` | `ListResponse<domain.BacktestConfig>` with `total` | 400 on invalid `strategy_id`. |
| `POST` | `/backtests/configs` | Protected | `domain.BacktestConfig` | `domain.BacktestConfig` | `201`; backend assigns id; cron validated. |
| `GET` | `/backtests/configs/{id}` | Protected | UUID path | `domain.BacktestConfig` | 404 if missing. |
| `PUT` | `/backtests/configs/{id}` | Protected | `domain.BacktestConfig` | `domain.BacktestConfig` | Path `id` overwrites body id. |
| `DELETE` | `/backtests/configs/{id}` | Protected | UUID path | `204` no body | Destructive. |
| `POST` | `/backtests/configs/{id}/run` | Protected | UUID path | `domain.BacktestRun` | `201`; may return 400/404/422 via service. |
| `GET` | `/backtests/runs` | Protected | `backtest_config_id?`, `limit`, `offset` | `ListResponse<domain.BacktestRun>` with `total` | 400 on invalid id. |
| `GET` | `/backtests/runs/{id}` | Protected | UUID path | `domain.BacktestRun` | 404 if missing. |

`internal/api/backtest_comparison.go` defines comparison types but no registered routes were found in `server.go`.

### Automation

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/automation/status` | Protected | None | automation job status array | Exact job status fields from automation package; not fully documented. |
| `GET` | `/automation/health` | Protected | None | `AutomationHealthResponse` | Shape confirmed. |
| `GET` | `/automation/runs` | Protected | `limit`, `offset` | `ListResponse<JobRun>` with `total` | Requires job run repo. |
| `GET` | `/automation/alpaca/verify` | Protected | None | `automation.AlpacaVerificationReport` | Shape from automation package; ambiguous to frontend. |
| `POST` | `/automation/alpaca/reconcile` | Protected | None | `{summary, verification}` | Live broker effect; response subtypes from automation package. |
| `POST` | `/automation/jobs/{name}/run` | Protected | name path | `{status:"triggered"}` | May return 400 from orchestrator. Retry safety ambiguous. |
| `POST` | `/automation/jobs/{name}/enable` | Protected | `{enabled}` | `{enabled}` | Endpoint both enables and disables. |

### Memory, conversations, events, audit, settings, prompts

| Method | Path | Auth | Request/query | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/memories` | Protected | `q?`, `agent_role?`, `limit`, `offset` | `ListResponse<Memory>` without `total` | Memory shape from repository/domain. |
| `POST` | `/memories/search` | Protected | `{query}` + pagination query | `ListResponse<Memory>` without `total` | `query` required. |
| `DELETE` | `/memories/{id}` | Protected | UUID path | `204` no body | Destructive. |
| `GET` | `/conversations` | Protected | `pipeline_run_id?`, `agent_role?`, `limit`, `offset` | `ListResponse<Conversation>` with `total` | Invalid `pipeline_run_id` is silently ignored, not 400. |
| `POST` | `/conversations` | Protected | `{pipeline_run_id, agent_role}` | `domain.Conversation` | Validates run exists; title generated server-side. |
| `GET` | `/conversations/{id}/messages` | Protected | UUID path + pagination | `ListResponse<ConversationMessage>` without `total` | Offset 0 may prepend synthetic assistant messages from decisions. |
| `POST` | `/conversations/{id}/messages` | Protected | `{content}` | `domain.ConversationMessage` | Service may return `501` when LLM unavailable. |
| `GET` | `/events` | Protected | `event_kind`, `pipeline_run_id`, `strategy_id`, `agent_role`, `after`, `before`, `limit`, `offset` | `ListResponse<AgentEvent>` with `total` | Returns `501` if events not configured. |
| `GET` | `/audit-log` | Protected | `event_type`, `entity_type`, `actor`, `entity_id`, `after`, `before`, `limit`, `offset` | `ListResponse<AuditLogEntry>` with `total` | Brief omitted `actor` and `entity_id`. |
| `GET` | `/settings` | Protected | None | `SettingsResponse` | Shape confirmed in `settings.go`. |
| `PUT` | `/settings` | Protected | `SettingsUpdateRequest` | `SettingsResponse` | API keys optional pointer fields; empty/omitted behavior should be confirmed. |
| `GET` | `/prompts` | Protected | None | `{prompts}` | Prompt definition shape confirmed. |
| `PUT` | `/prompts` | Protected | `{overrides: Record<string,string>}` | `{prompts}` | Empty/whitespace override clears it. |

## 2. Screen-to-endpoint traceability matrix

| Screen | Confirmed endpoint dependencies | Traceability notes |
| --- | --- | --- |
| Login/register | `POST /auth/login`, `POST /auth/register`, `POST /auth/refresh`, `GET /me` | Registration route exists, but product exposure is undecided. |
| App shell/header | `GET /me`, `GET /settings`, `GET /risk/status`, `GET /health`, `GET /ws` | Header schema/environment values come from `/settings.system`; API health is outside `/api/v1`. |
| Operator cockpit | `/risk/status`, `/risk/cockpit`, `/risk/breakers`, `/portfolio/summary`, `/portfolio/positions/open`, `/runs?status=running`, `/orders`, `/automation/status`, `/automation/health`, `/marketdata/polymarket/status`, `/ws` | **Inference**: brief suggests `/orders?status=open`; backend validates order status enum, but `open` is not confirmed. Use domain order statuses until backend confirms an “open” filter semantic. |
| Strategies list/detail | `/strategies`, `/strategies/{id}`, `/strategies/{id}/run`, `/pause`, `/resume`, `/skip-next`, `/reports/latest`, `/reports` | Strategy create/update bodies are full `domain.Strategy`; frontend should avoid partial updates. |
| Runs detail/workbench | `/runs`, `/runs/{id}`, `/runs/{id}/decisions`, `/runs/{id}/snapshot`, `/events`, `/journal/decisions`, `/replay/decisions/{id}`, `/orders`, `/trades` | `snapshot` response is raw JSON by data type. Replay payload is ambiguous. |
| Portfolio | `/portfolio/summary`, `/portfolio/positions`, `/portfolio/positions/open`, allocator endpoints | Portfolio summary is small fixed map; allocator diagnostics/summary need type confirmation. |
| Orders/trades | `/orders`, `/orders/{id}`, `/trades` | Order detail returns order and fills; no order mutation/cancel endpoint exists. |
| Risk console | `/risk/status`, `/risk/cockpit`, `/risk/breakers`, `/risk/killswitch`, `/risk/breaker/reset`, `/risk/market/{type}/stop`, `/risk/market/{type}/resume` | Reset requires one-shot `X-Admin-Key`; market type validation ambiguity. |
| Market intelligence | `/market/ohlcv/{ticker}`, `/social/sentiment/{ticker}`, `/news`, `/event-markets/summary`, options/calendar/universe endpoints | Several read endpoints are guest-accessible, but main UI can still require auth. |
| Polymarket/Kalshi | `/polymarket/*`, `/kalshi/summary`, `/marketdata/polymarket/status`, `/event-markets/summary`, WS `subscribe_polymarket` | Polymarket list response envelopes are inconsistent with `ListResponse`. |
| Research/signals | `/research/options/opportunities/{underlying}`, `/research/polymarket/opportunities`, `/discovery/run`, `/discovery/results`, `/signals/*` | Discovery run is synchronous and could be slow; no async job endpoint except polymarket discovery. |
| Backtests | `/backtest/divergence`, `/backtests/*` | Comparison API types exist but no route registered. |
| Automation | `/automation/status`, `/automation/health`, `/automation/runs`, `/automation/alpaca/verify`, `/automation/alpaca/reconcile`, `/automation/jobs/{name}/run`, `/automation/jobs/{name}/enable` | Reconcile may affect live broker state; require confirmation/guardrails. |
| Memory/conversations | `/memories`, `/memories/search`, `/memories/{id}`, `/conversations`, `/conversations/{id}/messages` | Conversation messages may include synthetic messages on first page. |
| Audit log/events | `/audit-log`, `/events` | Both support time filters and pagination. Audit has extra actor/entity filters. |
| Settings/account/prompts/API keys | `/settings`, `/prompts`, `/me`, `/api-keys` | Secrets are write-only/redacted. API key list appears global/server-wide. |

## 3. Missing or ambiguous request and response types

Do not create strict frontend types for these without backend confirmation or generated schema:

1. `domain.User` response from `GET /me`: brief fields are plausible, but exact JSON tags should be checked in `internal/domain`.
2. `domain.Strategy`, `domain.PipelineRun`, `domain.AgentDecision`, `domain.Position`, `domain.Order`, `domain.Trade`, `domain.TradeDecision`, `domain.BacktestConfig`, `domain.BacktestRun`, `domain.AuditLogEntry`, `domain.Conversation`, `domain.ConversationMessage`, `domain.APIKey`: handlers expose domain structs directly; frontend needs a schema extraction pass from domain source.
3. Strategy `config` schema: backend validates `agent.StrategyConfig`, `rules_engine`, and `options_rules`; brief treats it as `unknown`, but create/update can fail validation.
4. `/portfolio/allocator/diagnostics` and `/portfolio/allocator/summary`: handler-specific response shapes should be extracted and frozen for frontend use.
5. `/risk/status` and `/risk/cockpit`: response shapes come from risk engine/builders, not an API DTO in reviewed files.
6. `/replay/decisions/{id}`: result is `replay.BuildWorkbench`; shape is not documented in the brief.
7. Market-data arrays: OHLCV, options chain, options bars, calendars, filings, news, sentiment are returned as provider/domain arrays without API DTOs in reviewed handler files.
8. Filing analysis response from `POST /calendar/filings/analyze`: returns `automation.AnalyzeFiling` result; shape unspecified.
9. Polymarket account/trade/signal/market/watched-market response shapes: returned from repositories, signal store, or agent data structs; no stable API DTOs.
10. Polymarket `PATCH /polymarket/watched/{slug}` only accepts `{enabled}`. The brief says “edit watched market,” which implies fields that are not implemented.
11. Polymarket discovery `last` result shape is not a DTO and may be `null`.
12. Kalshi nested domain types in `KalshiSummaryResponse` need frontend-facing schemas.
13. Research opportunity item shapes for options and Polymarket come from service return types, not API DTOs.
14. Discovery result and discovery run history shapes are not documented.
15. Signal store payloads (`StoredSignal`, `StoredTrigger`, `WatchTerm`) need source-derived types; signal endpoints use nonstandard envelopes and some nonstandard error codes.
16. Automation status, job run, Alpaca verification, and reconcile summary shapes come from automation/postgres packages; not documented in brief.
17. Report artifact shape from `pgrepo.ReportArtifact` is not documented.
18. WebSocket `data` payload by event type is `any`; no event-specific discriminated union exists.
19. Error code vocabulary is mostly standardized, but signal handlers return `signal_hub_unavailable` and `invalid_request`; polymarket discovery nil automation returns `ERR_INTERNAL` for not configured.

## 4. Authentication, authorization, security, and retry concerns

### Authentication and authorization

- Protected `/api/v1/*` routes accept either `Authorization: Bearer <access JWT>` or `X-API-Key`.
- Public auth routes are `/api/v1/auth/login`, `/api/v1/auth/refresh`, `/api/v1/auth/register`.
- Guest read-only exceptions are implemented in `isGuestObservationRequest`; only confirmed `GET` observation endpoints bypass auth.
- WebSocket auth supports headers and `?token=` / `?api_key=`. Browser clients must use query token; this can leak in logs/proxies, so keep token TTL short and avoid logging full URLs at the edge.
- There is no role model in reviewed auth source. The only additional admin gate is `X-Admin-Key` for breaker reset.
- API keys authenticate as subject `key.Name`; API key endpoints appear global, not user-scoped.

### Security

- Security headers confirmed: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`.
- Request body limit is 1 MiB globally.
- CORS default is permissive (`*`) with allowed headers `Accept`, `Authorization`, `Content-Type`. `X-API-Key` and `X-Admin-Key` are not in default allowed headers, so browser use of those headers may fail unless configured.
- Default CORS allowed methods omit `PATCH`, but `/me`, Polymarket account tracking, and watched-market edits use `PATCH`; browser preflight may fail unless deployment config includes `PATCH`.
- WebSocket origin check follows CORS allowed origins. `*` allows all origins.
- Login uses a dummy bcrypt check for missing users, reducing username timing oracle risk.
- Tokens are JWTs signed HS256 with token type claims. Access TTL defaults to 1 hour; refresh TTL defaults to 24 hours.
- Refresh tokens are stateless JWTs in reviewed source; no server-side revocation/rotation tracking was visible.
- Raw provider API keys are redacted in settings responses. Update request accepts optional API key pointers.
- `X-Admin-Key` should never be stored in browser persistence; use one-shot prompt per reset if this endpoint must be exposed.
- `/metrics` is public in router; if deployed publicly, protect at ingress or keep ops-only.

### Rate limits and retry

- Global fixed-window IP rate limiter defaults to 100 requests/minute if enabled; WebSocket upgrades are excluded.
- API keys also have a token-bucket rate limit based on each key’s `rate_limit_per_minute`.
- Frontend should implement refresh-once for `401`; do not retry refresh indefinitely.
- Safe automatic retries: `GET` endpoints, except be careful with long-running/provider-backed endpoints that may be expensive.
- Do not automatically retry mutating endpoints unless product/backend confirms idempotency: strategy run, run cancel, kill switch, market stop/resume, breaker reset, universe refresh/scan, discovery run, polymarket discovery run, backtest run, automation job run/enable, Alpaca reconcile, settings/prompts update, account/password update, API key create/revoke, watched-market mutations, conversation/message creation.
- No idempotency-key support was found.
- WebSocket retry behavior in the brief is reasonable, but actual server will drop slow consumers and has a send buffer of 256 messages; frontend should resync via REST after reconnect.

## 5. Decisions requiring product or backend confirmation

1. Should public registration be exposed in production UI, hidden, or disabled server-side?
2. Should guest read-only market mode be a product feature, or should the frontend force login for all screens?
3. Should API keys be user-scoped? Current listing appears server-wide.
4. What is the intended browser/admin UX for breaker reset? Prefer role/session-based admin authorization over `X-Admin-Key`.
5. Confirm production CORS allowed origins, methods (`PATCH`), and headers (`X-API-Key`, `X-Admin-Key` if browser-used).
6. Confirm whether `/metrics` and health endpoints remain public at the app origin.
7. Confirm stable enum values for `market_type`, `order_type`, `order_status`, `position.side`, `pipeline.status`, `agent_role`, backtest statuses, job statuses, discovery statuses, and risk breaker scopes.
8. Confirm whether `/orders?status=open` is valid. Go source validates `domain.OrderStatus`; the brief’s `open` status is an inference and may not exist.
9. Confirm whether strategy and backtest create/update are full-object PUT/POST contracts or whether frontend-friendly DTOs/patch endpoints are planned.
10. Confirm exact frontend DTOs for all domain structs currently returned directly.
11. Confirm whether Polymarket watched-market update should support `note` or only `enabled`.
12. Confirm whether Polymarket list `total` should be full count instead of page length.
13. Confirm whether discovery run should remain synchronous or have async job status/progress.
14. Confirm whether all configured-but-missing services should return `ERR_NOT_IMPLEMENTED` instead of several current `ERR_INTERNAL`/`503` variants.
15. Confirm event-specific WebSocket `data` payloads and cache-invalidation semantics.
16. Confirm whether settings `api_key: ""` clears, preserves, or replaces a provider key. Brief says empty/omitted should preserve; source uses pointer fields but update semantics require confirmation.
17. Confirm if changing password should invalidate existing refresh tokens; source does not show token revocation.
18. Confirm retry/idempotency expectations for all mutating endpoints, especially live-trading and broker reconciliation actions.

## 6. Explicit assumptions that allow frontend work to proceed

These assumptions are sufficient to start a frontend without inventing payload fields:

1. Build a single API client using `/api/v1` base, with typed envelopes and per-endpoint `unknown` payloads where DTOs are not confirmed.
2. Treat all non-auth, non-guest routes as requiring login with bearer JWT.
3. Hide public registration by default; leave a feature flag or route stub until product confirms.
4. Use in-memory access token storage and isolate refresh token persistence behind one auth module.
5. Implement refresh-once on `401`; after refresh failure, clear session and redirect to login.
6. Implement REST polling fallback/resync for realtime-dependent views after WebSocket reconnect.
7. Use `ListResponse<T>` only for endpoints that actually return `{data, limit, offset}`; handle direct arrays and custom envelopes separately.
8. Treat all domain structs returned by handlers as generated/source-derived types to be filled in later; initial UI can render known brief fields plus raw JSON inspector for unknown parts.
9. For strategy/backtest create and edit, send full objects, not partial patch payloads.
10. For strategy config, start with JSON editor and display backend validation errors verbatim.
11. For `/orders`, do not use `status=open` until backend confirms; instead filter client-side for open-like statuses only after enum confirmation.
12. Gate all destructive/live actions behind confirmation dialogs and do not auto-retry them.
13. Treat `501`, `503` with “not configured” messages, and empty success from signal endpoints as feature-unavailable/degraded states, not fatal app crashes.
14. Prompt for `X-Admin-Key` only at breaker reset time and discard immediately.
15. Mark Polymarket/Kalshi/research/backtest/automation advanced screens as tolerant of ambiguous payloads via JSON viewers and progressive enhancement.
16. Do not build a frontend metrics UI unless product explicitly asks.

## 7. P0/P1/P2 scope proposal

### P0 — operator-safe foundation and core trading operations

- Auth: login, refresh, logout, current user, password change.
- App shell: route protection, environment/schema display, WebSocket state, global error/toast, JSON viewer, confirmation dialog, pagination utilities.
- API client: bearer injection, refresh-once, `ApiError` parsing, endpoint-specific envelope handling, cancellation support.
- Cockpit: risk status/cockpit/breakers, portfolio summary/open positions, running runs, recent orders/trades, automation health, WebSocket activity.
- Strategies: list/detail/create/edit/delete/run/pause/resume/skip-next with JSON config editor.
- Runs: list/detail/decisions/snapshot/events and cancel action.
- Portfolio/orders/trades: core read-only exposure and execution audit.
- Risk console: kill switch, market stop/resume, breaker list; breaker reset only behind one-shot admin key UX if explicitly enabled.

### P1 — admin, accountability, and operational maintenance

- Settings and prompts with secret-redaction UX.
- API key management with one-time plaintext display.
- Audit log and event log screens.
- Automation status/health/runs, manual job run/enable with confirmations.
- Alpaca verify/reconcile, with reconcile guarded as a live-broker action.
- Reports panels under strategies.
- Market intelligence basics: ticker OHLCV, news, sentiment, calendars, universe/watchlist, options chain/bars.

### P2 — research and advanced workflows

- Polymarket workspace, account tracking, watched markets, jobs/discovery, provider status, realtime Polymarket events.
- Kalshi summary workspace.
- Research opportunity scanners, discovery results, signal feeds/watch terms.
- Backtest config CRUD, run history/details, divergence view.
- Memory and conversations browser/chat.
- Replay workbench.
- Any public guest market-observer mode.

## Appendix: frontend contract gaps to resolve before strict TypeScript typing

Prioritize backend-generated OpenAPI or JSON schema for:

1. All domain structs returned directly by handlers.
2. Risk status/cockpit, allocator summary/diagnostics, replay workbench.
3. Polymarket/Kalshi market/account/trade/watched/discovery DTOs.
4. Market data/news/calendar/options provider payloads.
5. Automation/Alpaca/job run DTOs.
6. WebSocket event `data` payload per event type.
