# Frontend User Modes and Workflows

Source inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/api-implementation-review.md`, and the referenced Go API registration/auth/realtime/risk/settings sources.

Labels:

- **Confirmed**: visible in reviewed API source or the implementation brief.
- **Assumption**: proposed frontend behavior that does not add undocumented API payload fields.
- **Open question**: backend/product decision needed before screen specs or strict typing.

## 1. User modes and responsibilities

### Operator

- **Primary goals**: answer “is trading safe right now?”, monitor active strategies/runs, react to risk or execution alerts, trace live orders/fills/positions, pause or stop unsafe execution.
- **Time-sensitive decisions**: activate global kill switch; stop/resume a market; pause/resume/skip strategy; cancel an active run; decide whether degraded realtime/API state means trading should be treated as unsafe.
- **Information needed**: `/health`, `/settings.system`, `/risk/status`, `/risk/cockpit`, `/risk/breakers`, `/portfolio/summary`, `/portfolio/positions/open`, `/runs?status=running`, `/orders`, `/trades`, `/automation/health`, `/marketdata/polymarket/status`, WebSocket events.
- **Actions**: risk kill switch, market stop/resume, strategy pause/resume/skip/manual run, run cancel, navigate to related run/decision/order/position evidence.
- **High-risk actions**: deactivate kill switch, resume a market, reset breaker, manual strategy run, cancel run, strategy delete. **Assumption**: all require confirmation; resume/reset dialogs must describe what protection is being removed.
- **Common degraded scenarios**: WebSocket disconnected or slow-consumer drop; `501`/`503` service not configured; stale risk/cockpit data; automation jobs failing; broker not configured in settings; Polymarket recorder lag; token refresh failure.

### Researcher

- **Primary goals**: investigate market opportunities, strategy behavior, agent decisions, snapshots, signals, discovery output, backtest results, conversations, and memories.
- **Time-sensitive decisions**: decide whether a signal or opportunity deserves strategy changes; compare live behavior to backtest/divergence; inspect decision evidence before allowing execution changes.
- **Information needed**: strategy/run/decision/snapshot/event data; market OHLCV/news/sentiment/calendar/universe/options data; research opportunity endpoints; discovery results; signal feeds/watchlist; backtest configs/runs/divergence; replay/conversation/memory endpoints where configured.
- **Actions**: run discovery, add/remove signal watch terms, run backtest config, analyze filing, inspect replay/conversation/memory evidence. **Assumption**: research actions that trigger providers or LLMs should show cost/latency expectations and avoid auto-retry.
- **High-risk actions**: editing live strategy config, running discovery synchronously, running backtests with ambiguous config payloads, deleting memories, changing watchlists that may influence future signal evaluation.
- **Common degraded scenarios**: ambiguous/unknown payload shapes; provider-backed endpoints returning empty arrays; `501` for replay/conversation/snapshot/report services; long-running discovery with no async progress endpoint.

### Administrator

- **Primary goals**: keep credentials, providers, prompts, automation, auditability, and account/API keys operational.
- **Time-sensitive decisions**: rotate compromised keys, disable broken automation, reconcile broker state, reset risk breaker only when appropriate, diagnose schema/environment/broker/provider misconfiguration.
- **Information needed**: `/settings`, `/prompts`, `/me`, `/api-keys`, `/automation/status`, `/automation/health`, `/automation/runs`, `/automation/alpaca/verify`, `/audit-log`, `/events`.
- **Actions**: update settings/prompts, change password, create/revoke API keys, enable/disable/run automation jobs, run Alpaca reconciliation, inspect audit/events, reset breaker using one-shot `X-Admin-Key` if product enables it.
- **High-risk actions**: settings provider key replacement, risk limits update, prompt update, API key creation/revoke, automation enable/disable/run, Alpaca reconcile, breaker reset.
- **Common degraded scenarios**: CORS missing `PATCH`, `X-API-Key`, or `X-Admin-Key`; settings secret update semantics ambiguous; API keys appear server-wide; `ADMIN_API_KEY` not configured; audit/events repositories not configured.

## 2. Critical workflows

Each workflow uses only documented endpoints and labels frontend-only behavior as assumptions.

### 2.1 Log in and determine health

- **Actor**: Operator, Administrator, Researcher.
- **Trigger**: user opens the app without an active session or session refresh fails.
- **Entry route**: `/login`; authenticated users redirect to `/cockpit`.
- **Preconditions**: backend reachable; user has username/password. Registration is hidden by default.
- **Ordered steps**:
  1. Submit `{username,password}` to `POST /api/v1/auth/login`.
  2. Store access token in the auth module and refresh token through the chosen isolated storage strategy.
  3. Fetch `GET /api/v1/me`, `GET /api/v1/settings`, `GET /health`, `GET /api/v1/risk/status`.
  4. Open `GET /ws?token=<access_token>` and send `subscribe_all` after connection.
  5. Show environment, schema status, connected brokers, risk state, and realtime state in shell.
- **Required API data**: `LoginResponse`, `domain.User`, `SettingsResponse.system`, health response, risk status.
- **Realtime events**: all WebSocket event types after `subscribe_all`.
- **Primary actions**: continue to cockpit; logout on refresh failure.
- **Secondary actions**: view account; retry health checks; open degraded-state details.
- **Success result**: authenticated shell with health/risk/realtime indicators.
- **Failure result**: invalid credentials inline; backend unavailable error; refresh failure clears session.
- **Stale/offline behavior**: mark app degraded if health poll or WebSocket fails; REST views remain readable from cache with last-updated timestamps.
- **Permissions/credentials**: public login/refresh; protected health context calls use bearer JWT; browser WebSocket uses query token.
- **Related deep links**: `/cockpit`, `/settings`, `/account`.
- **Unresolved questions**: whether registration is exposed; exact token persistence policy; whether `/health` remains public at app origin.

### 2.2 Identify unsafe/degraded trading condition

- **Actor**: Operator.
- **Trigger**: cockpit load, alert badge, WebSocket `circuit_breaker`, `error`, `pipeline_health`, `order_submitted`, `order_filled`, or stale health polling.
- **Entry route**: `/cockpit`.
- **Preconditions**: authenticated session.
- **Ordered steps**:
  1. Load `/risk/status`, `/risk/cockpit`, `/risk/breakers`, `/automation/health`, `/portfolio/summary`, `/portfolio/positions/open`, `/runs?status=running`, and recent `/orders`/`/trades`.
  2. Compare risk status, kill switch, circuit breaker, automation health, broker connectivity from settings, active runs, and open exposure.
  3. Open alert detail drawer when any source reports warning/breached/degraded/stale.
  4. Choose intervention or inspect linked entity.
- **Required API data**: risk status/cockpit/breakers, automation health, portfolio summary/open positions, runs/orders/trades, settings broker list.
- **Realtime events**: `circuit_breaker`, `pipeline_health`, `error`, order/position events.
- **Primary actions**: activate kill switch; stop market; pause strategy; cancel run.
- **Secondary actions**: open risk console, run detail, strategy detail, order detail, automation console.
- **Success result**: user can classify condition as safe, degraded, or unsafe with evidence.
- **Failure result**: unavailable data shown as unknown/degraded, not safe.
- **Stale/offline behavior**: if realtime disconnected or critical risk data stale, show “realtime degraded; verify before action” and prefer conservative unsafe status.
- **Permissions/credentials**: protected routes; no admin key unless breaker reset is invoked.
- **Related deep links**: `/risk`, `/runs/:id`, `/strategies/:id`, `/orders/:id`, `/automation`.
- **Unresolved questions**: risk/cockpit DTO shape; official ordering/severity rules for combined degraded states.

### 2.3 Investigate cockpit alert

- **Actor**: Operator.
- **Trigger**: alert card or event in global activity drawer.
- **Entry route**: `/cockpit?alert=<frontend-local-id>` or linked entity route.
- **Preconditions**: alert can be tied to a known `strategy_id`, `run_id`, market type, or raw event payload; otherwise use JSON evidence view.
- **Ordered steps**:
  1. Open alert detail drawer with event payload and last-updated timestamp.
  2. If `run_id` exists, fetch `/runs/{id}`, `/runs/{id}/decisions`, `/runs/{id}/snapshot`, `/events?pipeline_run_id={id}`.
  3. If `strategy_id` exists, fetch `/strategies/{id}` and strategy-scoped `/runs`/`/events`.
  4. If order/position evidence is needed, use filters on `/orders` and `/trades` only with confirmed query parameters.
  5. Decide action or mark acknowledged locally. **Assumption**: no backend acknowledgement endpoint exists.
- **Required API data**: alert event envelope plus linked entity endpoints.
- **Realtime events**: source event and subsequent events for same run/strategy where subscribed.
- **Primary actions**: inspect linked entity; intervene via risk/strategy/run action.
- **Secondary actions**: copy event JSON; open audit/events with matching filters.
- **Success result**: alert is explained or escalated with linked evidence.
- **Failure result**: unresolved alert remains visible with “payload/link ambiguous.”
- **Stale/offline behavior**: show cached alert and note that follow-up REST fetch failed/stale.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/events?pipeline_run_id=...`, `/events?strategy_id=...`, `/runs/:id`, `/strategies/:id`.
- **Unresolved questions**: no persisted alert/ack model; event `data` payloads are not typed.

### 2.4 Inspect active pipeline run

- **Actor**: Operator or Researcher.
- **Trigger**: active run table, strategy detail, event stream.
- **Entry route**: `/runs/:id`.
- **Preconditions**: run UUID known.
- **Ordered steps**:
  1. Fetch `/runs/{id}`.
  2. Subscribe WebSocket with `{action:'subscribe', run_ids:[id]}`.
  3. Fetch `/runs/{id}/decisions`, `/runs/{id}/snapshot`, `/events?pipeline_run_id={id}`.
  4. Fetch related orders using `/orders` filters when `pipeline_run_id` is available in returned order rows; otherwise show link opportunities from rows only.
  5. Offer cancel only when run status is running. **Assumption**: UI decides visibility from documented status enum.
- **Required API data**: `domain.PipelineRun`, decision list, raw snapshot object, persisted events, orders/trades if linked by IDs.
- **Realtime events**: `pipeline_start`, `agent_decision`, `debate_round`, `signal`, order/position/error/health events for the run.
- **Primary actions**: cancel run; inspect decision; inspect snapshot; navigate to strategy.
- **Secondary actions**: copy run ID; open events; open related orders/trades.
- **Success result**: run status, timeline, reasoning, and execution side effects are visible.
- **Failure result**: run 404; snapshots/events 501 shown as unavailable; cancel can fail with API error.
- **Stale/offline behavior**: timeline uses persisted events plus cached WS buffer; after reconnect, refetch run/decisions/events/snapshot.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/runs/:id?tab=decisions`, `/runs/:id?tab=snapshot`, `/strategies/:strategy_id`, `/events?pipeline_run_id=:id`.
- **Unresolved questions**: snapshot shape; exact run status transition semantics; cancel idempotency.

### 2.5 Inspect agent decision and supporting snapshot

- **Actor**: Researcher, Operator.
- **Trigger**: decision row in run detail or decision journal.
- **Entry route**: `/runs/:runId?decision=:decisionId` or `/decisions/:id` if later added. **Assumption**: v1 can use query-state inside run route.
- **Preconditions**: run ID or decision ID known.
- **Ordered steps**:
  1. From run context, fetch `/runs/{runId}/decisions?include_prompt=false` by default.
  2. On explicit prompt reveal, refetch with `include_prompt=true`.
  3. Fetch `/runs/{runId}/snapshot` and map available `data_type` keys to the decision view without assuming fields.
  4. Optionally open `/replay/decisions/{id}` and `/journal/decisions/{id}` when configured.
  5. Show raw JSON inspectors for ambiguous decision/snapshot/replay fields.
- **Required API data**: decision list with optional `prompt_text`, snapshot raw JSON, optional journal/replay payload.
- **Realtime events**: `agent_decision`, `debate_round`, `signal`.
- **Primary actions**: reveal prompt; inspect snapshot; open replay; copy decision ID.
- **Secondary actions**: open conversation for run/agent if configured.
- **Success result**: decision reasoning and supporting raw evidence are inspectable.
- **Failure result**: prompt unavailable unless requested; replay/snapshot may return 501.
- **Stale/offline behavior**: decision list remains from cache; mark snapshot/prompt as stale if refetch fails.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/runs/:runId?tab=decisions&decision=:decisionId&include_prompt=true`, `/events?pipeline_run_id=:runId`.
- **Unresolved questions**: exact decision schema; replay workbench DTO; privacy/security expectations for prompt text.

### 2.6 Inspect strategy and recent behavior

- **Actor**: Operator, Researcher.
- **Trigger**: strategy table row, run detail strategy link, event stream.
- **Entry route**: `/strategies/:id`.
- **Preconditions**: strategy UUID known.
- **Ordered steps**:
  1. Fetch `/strategies/{id}`.
  2. Fetch `/runs?strategy_id={id}`, `/events?strategy_id={id}`, `/strategies/{id}/reports/latest`, and `/strategies/{id}/reports`.
  3. Show overview, config JSON, runs, reports, orders/trades links, risk/exposure links.
  4. Subscribe WebSocket with `{action:'subscribe', strategy_ids:[id]}`.
  5. Expose actions based on current strategy status.
- **Required API data**: strategy object, runs, events, report artifacts, linked order/trade/position data when available.
- **Realtime events**: all strategy-scoped WS events.
- **Primary actions**: run, pause, resume, skip next, edit, delete.
- **Secondary actions**: open latest run/report; copy strategy ID; filter orders/trades/events.
- **Success result**: recent behavior and operational controls are available from one detail page.
- **Failure result**: report 501/unavailable; action conflict shown inline.
- **Stale/offline behavior**: disable high-risk actions if current strategy state cannot be confirmed recently. **Assumption**.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/strategies/:id?tab=runs`, `/runs?strategy_id=:id`, `/events?strategy_id=:id`.
- **Unresolved questions**: strict `domain.Strategy` schema; report artifact schema; whether strategy actions are idempotent.

### 2.7 Pause/intervene in strategy execution

- **Actor**: Operator.
- **Trigger**: unsafe condition, bad signal, manual intervention.
- **Entry route**: `/strategies/:id` or `/cockpit` intervention drawer.
- **Preconditions**: authenticated; latest strategy status known; user has confirmed consequence.
- **Ordered steps**:
  1. Refetch `/strategies/{id}` before showing final confirmation.
  2. Choose `POST /strategies/{id}/pause`, `/resume`, `/skip-next`, or `/run`.
  3. On success, invalidate strategy/runs/events and show audit/event links.
  4. If run-level intervention is needed, navigate to `/runs/:id` and use `POST /runs/{id}/cancel`.
- **Required API data**: strategy object, optional active runs.
- **Realtime events**: pipeline/health/error events after action.
- **Primary actions**: pause/resume/skip/manual run/cancel active run.
- **Secondary actions**: activate kill switch or market stop if strategy action fails or risk is broader.
- **Success result**: backend returns updated strategy or accepted run/cancel response.
- **Failure result**: conflict if status precondition fails; 501 if runner not configured; bad request if cancellation invalid.
- **Stale/offline behavior**: block action when offline; require refetch before mutating.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/strategies/:id`, `/runs?strategy_id=:id&status=running`, `/risk`.
- **Unresolved questions**: no reason field for strategy intervention; no idempotency keys.

### 2.8 Inspect portfolio exposure

- **Actor**: Operator, Researcher.
- **Trigger**: cockpit P&L/exposure card or portfolio nav.
- **Entry route**: `/portfolio`.
- **Preconditions**: authenticated.
- **Ordered steps**:
  1. Fetch `/portfolio/summary` and `/portfolio/positions/open`.
  2. Fetch `/portfolio/positions` for all/closed views with `ticker`, `side`, `limit`, `offset` filters.
  3. Optionally fetch allocator diagnostics/opportunities/decisions/summary for sizing context.
  4. Link position rows to strategy/orders/trades when IDs are present in returned domain rows.
- **Required API data**: summary map, position lists, allocator endpoints.
- **Realtime events**: `position_update`, `order_filled`.
- **Primary actions**: inspect position, open linked trades/orders/strategy.
- **Secondary actions**: open risk console; export/copy row ID. **Assumption**: export is frontend-only if added.
- **Success result**: current exposure and P&L visible with last-updated timestamp.
- **Failure result**: allocator unavailable shown separately; core positions remain usable.
- **Stale/offline behavior**: mark P&L/exposure stale; do not infer safety from stale prices.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/portfolio?side=long&ticker=...`, `/trades?position_id=:id`, `/strategies/:id`.
- **Unresolved questions**: portfolio summary only uses first max-limit open positions; allocator response shapes ambiguous.

### 2.9 Trace order through fills/trades/positions

- **Actor**: Operator, Administrator.
- **Trigger**: execution alert, order row, position row, trade row.
- **Entry route**: `/orders/:id` or `/orders` with filters.
- **Preconditions**: order UUID known or filters chosen.
- **Ordered steps**:
  1. Fetch `/orders/{id}` for `{order,fills}`.
  2. Show fills as trades returned by `GetByOrder` without assuming additional fill fields.
  3. Fetch `/trades?order_id={id}` when a separate trades table view is needed.
  4. If trade rows include `position_id`, fetch `/trades?position_id={positionId}` and link to portfolio position if row ID is known.
  5. Link order to run/strategy if `pipeline_run_id`/`strategy_id` fields are present.
- **Required API data**: order detail envelope, trade list, optional position/strategy/run objects.
- **Realtime events**: `order_submitted`, `order_filled`, `position_update`.
- **Primary actions**: inspect related run/strategy/position/trades.
- **Secondary actions**: copy broker external ID; filter by ticker/status/side/order type/broker/market type.
- **Success result**: execution chain is explainable from decision/run to order to fills/trades to position impact.
- **Failure result**: missing IDs leave chain partially unresolved; show raw JSON and “link unavailable.”
- **Stale/offline behavior**: execution views should show last fetched time; after reconnect refetch order/trade/position queries.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/orders/:id`, `/trades?order_id=:id`, `/trades?position_id=:id`, `/runs/:id`, `/strategies/:id`.
- **Unresolved questions**: no order cancel endpoint; exact domain order/trade/position schemas.

### 2.10 Activate and verify risk control

- **Actor**: Operator; Administrator for breaker reset.
- **Trigger**: unsafe condition, manual risk drill, circuit breaker alert.
- **Entry route**: `/risk` or sticky cockpit risk control.
- **Preconditions**: authenticated; reason supplied for activation/market stop; admin key only for breaker reset.
- **Ordered steps**:
  1. Refetch `/risk/status` and `/risk/breakers`.
  2. For global kill switch, submit `POST /risk/killswitch` `{active:true,reason}`.
  3. For market stop, submit `POST /risk/market/{type}/stop` `{reason}`.
  4. Refetch risk endpoints and wait for related WebSocket `circuit_breaker`/health events where available.
  5. Verify status banner matches returned `{active}` or `{market_type,active:true}`.
  6. For reset, prompt one-shot `X-Admin-Key`, submit `POST /risk/breaker/reset` `{scope}`, then discard key.
- **Required API data**: risk status/cockpit/breakers and mutation responses.
- **Realtime events**: `circuit_breaker`, `pipeline_health`, `error`.
- **Primary actions**: activate/deactivate kill switch; stop/resume market; reset breaker.
- **Secondary actions**: inspect audit log for risk events; inspect affected strategies/runs.
- **Success result**: risk state reflects active protection or reset result.
- **Failure result**: validation error for missing reason/scope; 401 for admin key; 501 when admin/breaker unavailable.
- **Stale/offline behavior**: do not allow deactivate/resume/reset while risk status cannot be refetched.
- **Permissions/credentials**: protected bearer token; breaker reset also requires `X-Admin-Key`.
- **Related deep links**: `/risk?scope=global`, `/audit-log?event_type=kill_switch.activated`, `/events?event_kind=circuit_breaker`.
- **Unresolved questions**: market type enum validation; better role/session-based admin UX; breaker reset scope vocabulary.

### 2.11 Research market opportunity

- **Actor**: Researcher.
- **Trigger**: research nav, ticker from strategy/position, signal feed.
- **Entry route**: `/research` or `/markets/:ticker` by proposed route.
- **Preconditions**: authenticated for research endpoints; guest may read selected market data only if product enables observer mode.
- **Ordered steps**:
  1. Load market context: `/market/ohlcv/{ticker}`, `/news`, `/social/sentiment/{ticker}`, calendar/universe/options endpoints as relevant.
  2. Load opportunity endpoints: `/research/options/opportunities/{underlying}` or `/research/polymarket/opportunities`.
  3. Optionally run `POST /discovery/run` with documented body only.
  4. Inspect `/discovery/results`, `/signals/evaluated`, `/signals/triggers`, `/signals/watchlist`.
  5. Add/delete signal watch term if needed.
- **Required API data**: market arrays, opportunities list envelope, discovery result/history, signal feeds/watchlist.
- **Realtime events**: `signal`; Polymarket events if subscribed.
- **Primary actions**: run discovery; manage watch term; open strategy/backtest from context when IDs are present.
- **Secondary actions**: analyze filing; open ticker market page; copy raw opportunity JSON.
- **Success result**: opportunity is supported, rejected, or deferred with evidence.
- **Failure result**: provider unavailable, empty result, or ambiguous payload shown as degraded/unknown.
- **Stale/offline behavior**: show stale market data; no automatic retry for synchronous discovery.
- **Permissions/credentials**: research endpoints protected; selected market GETs optionally public.
- **Related deep links**: `/markets/:ticker`, `/research?underlying=...`, `/signals?term=...`, `/backtests?strategy_id=...`.
- **Unresolved questions**: opportunity item DTOs; whether discovery should become async; guest observer product decision.

### 2.12 Run and inspect backtest

- **Actor**: Researcher.
- **Trigger**: strategy evaluation or research/backtests nav.
- **Entry route**: `/backtests`.
- **Preconditions**: authenticated; backtest config exists or user creates full `domain.BacktestConfig` object.
- **Ordered steps**:
  1. Fetch `/backtests/configs` optionally by `strategy_id`.
  2. Create/update config using full object contract, with JSON editor for ambiguous fields.
  3. Run `POST /backtests/configs/{id}/run`.
  4. Fetch `/backtests/runs` and `/backtests/runs/{id}`.
  5. Fetch `/backtests/divergence?strategy_id={id}` when comparing live/backtest behavior.
- **Required API data**: backtest config/run domain objects, divergence response.
- **Realtime events**: none confirmed for backtests.
- **Primary actions**: create/edit/delete config; run config; inspect run/divergence.
- **Secondary actions**: link to strategy; compare results when payload supports it.
- **Success result**: run result/detail visible and linked to config/strategy.
- **Failure result**: validation/service errors; no comparison routes despite comparison types in source.
- **Stale/offline behavior**: no auto-retry mutating run; refetch list manually.
- **Permissions/credentials**: protected routes.
- **Related deep links**: `/backtests/configs/:id`, `/backtests/runs/:id`, `/strategies/:strategy_id`.
- **Unresolved questions**: exact backtest DTOs; comparison route availability; status/progress model for long runs.

### 2.13 Manage automation and settings

- **Actor**: Administrator.
- **Trigger**: degraded automation/provider state, setup, credential rotation.
- **Entry route**: `/automation`, `/settings`, `/prompts`, `/account`.
- **Preconditions**: authenticated; user understands action consequences.
- **Ordered steps**:
  1. Automation: fetch `/automation/status`, `/automation/health`, `/automation/runs`.
  2. For job actions, confirm then call `/automation/jobs/{name}/run` or `/automation/jobs/{name}/enable` `{enabled}`.
  3. For broker state, fetch `/automation/alpaca/verify`; confirm then call `/automation/alpaca/reconcile`.
  4. Settings: fetch `/settings`; update with complete `SettingsUpdateRequest` only.
  5. Prompts: fetch/update `/prompts` using documented override map.
  6. Account/API keys: use `/me` password update and `/api-keys` create/revoke flows.
- **Required API data**: automation job/health/run data, settings response/update request, prompt definitions, API key metadata.
- **Realtime events**: `pipeline_health`, `error`; audit/events are persisted not guaranteed realtime.
- **Primary actions**: run/enable jobs; verify/reconcile Alpaca; update settings/prompts; create/revoke key; change password.
- **Secondary actions**: inspect audit log; copy one-time API key plaintext.
- **Success result**: admin state updated and refetched; secrets remain redacted.
- **Failure result**: service not configured, validation errors, CORS/preflight issues for PATCH/admin headers.
- **Stale/offline behavior**: block settings/prompts/secrets updates while offline; show last known provider configured flags.
- **Permissions/credentials**: protected routes; no role model confirmed.
- **Related deep links**: `/automation?job=...`, `/settings?provider=...`, `/account?tab=api-keys`, `/audit-log?actor=...`.
- **Unresolved questions**: settings `api_key: ""` semantics; user/API-key scoping; automation DTOs; live broker reconcile guardrails.

### 2.14 Inspect audit event

- **Actor**: Administrator, Operator.
- **Trigger**: after high-risk action, incident review, linked audit/event row.
- **Entry route**: `/audit-log`.
- **Preconditions**: authenticated; audit repository configured.
- **Ordered steps**:
  1. Fetch `/audit-log` with `event_type`, `entity_type`, `actor`, `entity_id`, `after`, `before`, pagination as needed.
  2. Open row detail with raw JSON/context.
  3. If `entity_id` maps to a known route, navigate with return context.
  4. Use `/events` for agent/runtime events when audit is not the right source.
- **Required API data**: audit log entries and event entries.
- **Realtime events**: none confirmed for audit log; related runtime events may arrive over WebSocket.
- **Primary actions**: filter, inspect, copy event/entity IDs, navigate to entity.
- **Secondary actions**: save filter URL for incident review. **Assumption**: URL state only.
- **Success result**: high-risk action is attributable and linked to relevant entity where possible.
- **Failure result**: 501/unavailable or missing entity route leaves raw audit row only.
- **Stale/offline behavior**: audit table can remain cached but must show last-updated time.
- **Permissions/credentials**: protected route.
- **Related deep links**: `/audit-log?event_type=...&entity_id=...`, `/events?...`, linked entity routes.
- **Unresolved questions**: exact audit entry schema and entity type vocabulary.

## 3. Cross-workflow assumptions before screen specs

1. Treat unknown domain payloads as source-derived future types and render raw JSON for ambiguous subtrees.
2. Keep route/query state shareable; preserve `from=` or breadcrumb context when navigating from incident workflows.
3. Do not auto-retry mutating actions; require fresh data before destructive/live interventions.
4. Treat missing realtime or stale risk data as degraded, never implicitly safe.
5. Hide registration and public guest observer mode until product explicitly confirms exposure.
