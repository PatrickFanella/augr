# P0 Frontend Screen Specifications

Source inputs: `docs/frontend-ui-api-brief.md`, all existing `docs/frontend/` documents, and referenced backend API source when endpoint behavior required confirmation.

Labels:

- **Confirmed**: visible in reviewed backend source or approved frontend docs.
- **Assumption**: frontend behavior that does not invent API fields or permissions.
- **Blocked**: needs backend/product contract before strict implementation.

These specifications are functional UX specs, not polished visual designs. Destructive and operational actions reference a separate safety policy; this document does not define final confirmation copy or safety thresholds.

## 1. Shared P0 conventions

### 1.1 Common shell data

Authenticated P0 routes share these background dependencies:

- `GET /api/v1/me`
- `GET /api/v1/settings`
- `GET /health`
- `GET /api/v1/risk/status`
- `GET /ws?token=<access_token>` with `subscribe_all` unless a page narrows subscriptions.

### 1.2 Common state vocabulary

Every screen below explicitly maps these states:

- **Initial loading**: first request set has no usable cached data.
- **Background refresh**: refetch is running while prior data remains visible.
- **Empty result**: valid response with no rows or no actionable content.
- **Partial data**: one or more widgets fail while the page shell and other widgets remain usable.
- **API error**: non-special API failure from a REST query or mutation.
- **Retry**: user-triggered retry for failed query/widget; safe automatic retry only for GETs.
- **Unauthorized**: `401` after one refresh attempt, or admin-key denial for breaker reset.
- **Rate limited**: `ERR_RATE_LIMITED` or HTTP 429.
- **Conflict**: `ERR_CONFLICT`, usually stale action precondition.
- **Validation failure**: `ERR_VALIDATION` or field-level backend validation message.
- **WebSocket disconnected**: realtime connection closed or reconnecting.
- **Stale data**: data older than page threshold or not refreshed after reconnect.
- **Offline**: browser/network offline or API origin unreachable.
- **Server feature unavailable with 501**: `ERR_NOT_IMPLEMENTED`; show feature panel, not crash.
- **Unknown/new enum value**: render raw value with neutral “unknown” styling and preserve actions conservatively.

### 1.3 Common table behavior

- Offset pagination with `limit` and `offset`; default `limit=50`, max `100` where backend uses common pagination.
- Default sorting is frontend-side unless a backend sort parameter is documented.
- Persist filters in the URL for shareable investigation links.
- Show full ISO timestamps in accessible labels/tooltips; display relative time only as secondary text.

### 1.4 Common safety behavior

- High-risk actions include kill switch deactivate, market resume, breaker reset, strategy delete, strategy run, strategy pause/resume/skip, run cancel, and destructive form saves.
- Each high-risk action must call the separate **P0 safety policy** before mutation. This spec only identifies where safety policy is required.
- Mutating actions are never auto-retried.
- Before showing a safety-policy action, refetch the primary entity/risk state when stale.

## 2. Login

### 2.1 Screen contract

- **Route and purpose**: `/login`; authenticate username/password and bootstrap session.
- **Primary user mode**: Operator, Researcher, Administrator.
- **Questions answered**: Can I access the app? Did my session expire? Is the API reachable enough to proceed?
- **Required REST queries**: after login success: `GET /api/v1/me`, `GET /api/v1/settings`, `GET /health`, `GET /api/v1/risk/status`.
- **Required mutations**: `POST /api/v1/auth/login`; `POST /api/v1/auth/refresh` only for existing session refresh flow.
- **Required WebSocket subscriptions**: none before auth; after redirect shell opens `/ws?token=` and sends `subscribe_all`.
- **URL query parameters and persistent filter state**: `next` internal redirect target; `reason=session_expired|unauthorized|logged_out`.
- **Page layout regions**: centered login panel; product/system title; username field; password field; submit; session/API error region; optional environment hint only after safe health check. No public registration link unless product enables it.
- **Table columns, default sorting, and filters**: none.
- **Primary actions**: submit login.
- **Secondary actions**: retry bootstrap health; clear expired session; navigate to safe `next` after success.
- **Entity deep links**: post-login redirects to `next` if internal, else `/cockpit`.
- **Permissions or credentials**: public route; uses username/password; no API key login for browser UI.
- **Paper-versus-live behavior**: not applicable on form; after bootstrap, shell must show broker paper/live flags.
- **Responsive behavior**: single-column card; fields full width on mobile; no horizontal scroll.
- **Keyboard and accessibility expectations**: first field focused; Enter submits; errors announced via `aria-live`; labels visible; password manager compatible.
- **Last-updated and stale-data behavior**: not shown until authenticated bootstrap completes.
- **Suggested polling/refresh if WebSocket unavailable**: none on login.
- **Acceptance criteria**: valid credentials redirect to `/cockpit`; invalid credentials do not reveal username existence; expired session reason is visible; unsafe `next` targets are ignored.

### 2.2 State handling

| State | Login behavior |
| --- | --- |
| Initial loading | If checking existing session, show compact “Restoring session”; otherwise render form immediately. |
| Background refresh | Disable duplicate submit only while login request is in flight. |
| Empty result | Not applicable. |
| Partial data | If login succeeds but shell bootstrap query fails, enter authenticated shell with degraded header rather than looping on login. |
| API error | Show generic connection/server error; keep credentials editable. |
| Retry | Submit button retries login; bootstrap retry belongs to shell after auth. |
| Unauthorized | Show generic invalid username/password or session expired message. |
| Rate limited | Show wait/retry later message; do not keep auto-submitting. |
| Conflict | Not expected; show generic API error if received. |
| Validation failure | Inline required username/password validation; backend validation shown above form. |
| WebSocket disconnected | Not applicable until authenticated shell. |
| Stale data | Not applicable. |
| Offline | Show offline banner and keep form disabled or retryable. |
| 501 unavailable | Not expected for auth; show server misconfiguration if received. |
| Unknown enum | Not applicable. |

## 3. Authenticated application shell

### 3.1 Screen contract

- **Route and purpose**: wraps all authenticated P0 routes; persistent navigation, status, realtime, errors, and action surfaces.
- **Primary user mode**: Operator first; supports all authenticated users.
- **Questions answered**: Who am I? Which environment/schema is active? Is realtime connected? Is risk/kill switch normal? Is broker paper/live?
- **Required REST queries**: `GET /me`, `GET /settings`, `GET /health`, `GET /risk/status`.
- **Required mutations**: logout is local/session clear; no backend logout route confirmed.
- **Required WebSocket subscriptions**: one shared `/ws?token=...`; default `subscribe_all`; page-level `subscribe` commands may add run/strategy filters.
- **URL query parameters and persistent filter state**: preserves child route parameters; shell-owned activity drawer state can use `activity=open` only if shareable.
- **Page layout regions**: left nav grouped Operations/Markets/Research/Administration; top header with environment/schema/user/realtime/risk; content outlet; global toast/error stack; realtime activity drawer; global dialog host.
- **Table columns, default sorting, and filters**: activity drawer table/list uses type, severity assumption, strategy/run link, timestamp; newest first.
- **Primary actions**: open activity drawer; navigate; logout; retry failed shell queries.
- **Secondary actions**: copy environment/schema details; open risk console from risk badge.
- **Entity deep links**: activity entries link to `/runs/:id`, `/strategies/:id`, `/events?...`, `/orders/:id` only when IDs are present and confirmed.
- **Permissions or credentials**: bearer JWT for REST; query token for browser WebSocket.
- **Paper-versus-live behavior**: header shows connected broker `{name,paper_mode,configured}` from settings; live configured broker must be visually distinct from paper.
- **Responsive behavior**: desktop persistent side nav; tablet collapsible rail; mobile bottom/slide nav with sticky header status compacted.
- **Keyboard and accessibility expectations**: skip-to-content; nav landmarks; drawer focus trap; Esc closes drawer/dialog; status badges include text.
- **Last-updated and stale-data behavior**: header status shows last health/risk/settings refresh; stale shell status colors child pages as degraded but does not hide content.
- **Suggested polling/refresh if WebSocket unavailable**: health/risk every 15s while visible; visible page data follows per-page fallback.
- **Acceptance criteria**: every authenticated route has visible environment, realtime, risk, and user context; WebSocket loss is visible; partial shell failures do not blank child content.

### 3.2 State handling

| State | Shell behavior |
| --- | --- |
| Initial loading | App skeleton with nav placeholders and header status skeleton until auth/me resolves. |
| Background refresh | Keep previous header values; show subtle refresh indicator near status cluster. |
| Empty result | Empty activity drawer says no realtime events buffered. |
| Partial data | Missing settings/health/risk appears as separate degraded badge; child outlet remains. |
| API error | Header badge identifies failing dependency; details in popover. |
| Retry | Retry per badge and global “retry shell status”. |
| Unauthorized | Refresh once; if still unauthorized redirect to `/login?reason=session_expired&next=...`. |
| Rate limited | Header shows rate-limited; slow polling until user retries. |
| Conflict | Not expected for shell queries. |
| Validation failure | Not expected. |
| WebSocket disconnected | Header status degraded; drawer shows reconnect attempts and REST fallback notice. |
| Stale data | Header badges show stale age; risk badge cannot show “safe” from stale data. |
| Offline | Offline banner; cached child data remains read-only. |
| 501 unavailable | Feature badge says not configured for the affected singleton/widget only. |
| Unknown enum | Show raw status string in neutral badge with tooltip “new value”. |

## 4. Cockpit

### 4.1 Screen contract

- **Route and purpose**: `/cockpit`; operator landing page answering “is trading safe right now?”
- **Primary user mode**: Operator.
- **Questions answered**: Is risk normal? Is realtime alive? Are breakers tripped? Are runs active? What is open exposure and recent execution? Are automation/brokers degraded?
- **Required REST queries**: `GET /risk/status`, `GET /risk/cockpit`, `GET /risk/breakers`, `GET /portfolio/summary`, `GET /portfolio/positions/open`, `GET /runs?status=running`, `GET /orders`, `GET /trades`, `GET /automation/health`, `GET /settings`, `GET /health`, optionally `GET /marketdata/polymarket/status`.
- **Required mutations**: `POST /risk/killswitch`, `POST /risk/market/{type}/stop`, `POST /risk/market/{type}/resume`, `POST /runs/{id}/cancel`; all require separate safety policy.
- **Required WebSocket subscriptions**: `subscribe_all`; `subscribe_polymarket` only if prediction-market widget enabled.
- **URL query parameters and persistent filter state**: `alert`, `strategy_id`, `run_id`, `market_type`, `from`; avoid undocumented backend filters such as `/orders?status=open` until confirmed.
- **Page layout regions**: safety summary strip; risk banner; health/broker/realtime strip; P&L/exposure cards; active runs table; open positions summary; recent orders/trades feed; automation health card; event/alert list.
- **Table columns, default sorting, and filters**: active runs columns `status`, `ticker`, `strategy_id/name if loaded`, `signal`, `started_at`, `duration`, `actions`; newest/running started first. Recent execution columns `type(order/trade)`, `ticker`, `side`, `quantity`, `status`, `broker`, `time`, `links`; newest first. Filters are local: market type, ticker, severity, paper/live where fields exist.
- **Primary actions**: inspect alert/run/strategy/order; invoke risk or run intervention via safety policy.
- **Secondary actions**: open risk console, portfolio, orders, trades, full events/activity drawer.
- **Entity deep links**: `/runs/:id`, `/strategies/:id`, `/orders/:id`, `/trades?order_id=...`, `/portfolio?strategy_id=...`, `/risk?market_type=...`.
- **Permissions or credentials**: authenticated; no admin key unless user chooses breaker reset via risk console.
- **Paper-versus-live behavior**: live broker/exposure rows visually elevated; paper/live broker state from settings; do not mix totals without labels.
- **Responsive behavior**: desktop dashboard grid; tablet stacked two-column; mobile status strips and cards stacked, tables become priority-column lists.
- **Keyboard and accessibility expectations**: status region announced on severity changes; tables keyboard navigable; operational actions reachable but separated from row navigation.
- **Last-updated and stale-data behavior**: each widget shows last updated; risk/portfolio/runs stale state contributes to overall degraded summary.
- **Suggested polling/refresh if WebSocket unavailable**: risk/health 15s; active runs 10s; positions/orders/trades 15–30s; automation 30s; back off on errors/rate limits.
- **Acceptance criteria**: user can classify safe/degraded/unsafe without opening another route; failure of one widget does not blank cockpit; stale realtime is obvious.

### 4.2 State handling

| State | Cockpit behavior |
| --- | --- |
| Initial loading | Show page skeleton with independent widget skeletons; safety summary waits for risk + health minimum. |
| Background refresh | Keep current cards/tables; show per-widget refresh spinner. |
| Empty result | No active runs/open positions/recent execution shown as positive empty only if risk/health are fresh. |
| Partial data | Failed widget displays inline error; safety summary marks unknown/degraded. |
| API error | Widget-level error with endpoint and retry; full-page only if auth/bootstrap unavailable. |
| Retry | Retry failed widget; global retry all visible cockpit queries. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Freeze last data, mark stale/rate-limited, reduce polling. |
| Conflict | Action conflict refetches affected entity/risk and reports stale precondition. |
| Validation failure | Missing reason/invalid action from risk mutation appears in action dialog owned by safety policy. |
| WebSocket disconnected | Realtime badge degraded; event feed switches to persisted/polled data note. |
| Stale data | Safety strip says stale/unknown; high-risk actions require refetch first. |
| Offline | Read-only cached cockpit; all mutations disabled by safety policy. |
| 501 unavailable | Affected widget says not configured; cockpit remains usable. |
| Unknown enum | Show raw value in neutral badge; exclude from “safe” aggregation. |

## 5. Strategies list

### 5.1 Screen contract

- **Route and purpose**: `/strategies`; browse and filter strategy inventory.
- **Primary user mode**: Operator.
- **Questions answered**: Which strategies exist? Which are active/paused/inactive? Which are paper/live? What is recent behavior?
- **Required REST queries**: `GET /strategies` with `ticker`, `market_type`, `status`, `is_paper`, `limit`, `offset`.
- **Required mutations**: none on table by default; row actions may call `POST /strategies/{id}/pause|resume|skip-next|run` or `DELETE /strategies/{id}` via safety policy.
- **Required WebSocket subscriptions**: `subscribe_all` from shell; no additional required.
- **URL query parameters and persistent filter state**: `ticker`, `market_type`, `status`, `is_paper`, `limit`, `offset`.
- **Page layout regions**: page header/actions; filter bar; strategies table; pagination; optional row detail preview.
- **Table columns, default sorting, and filters**: columns `name`, `ticker`, `market_type`, `paper/live`, `status`, `schedule_cron`, `skip_next_run`, `latest_run_summary.status`, `latest_run_summary.signal`, `updated_at`, actions. Default sort: active first, then updated newest (frontend). Filters mirror query params.
- **Primary actions**: open strategy detail; create strategy.
- **Secondary actions**: run/pause/resume/skip/delete through safety policy; copy ID; clear filters.
- **Entity deep links**: `/strategies/:id`, `/strategies/new`, `/runs?strategy_id=:id`, `/events?strategy_id=:id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: `is_paper=false` strategies must be visually distinct and sorted/groupable.
- **Responsive behavior**: desktop table; mobile card list with key fields and overflow actions.
- **Keyboard and accessibility expectations**: filter controls labelled; row Enter opens detail; action menu keyboard accessible.
- **Last-updated and stale-data behavior**: table header shows last fetch; stale rows keep actions gated by refetch.
- **Suggested polling/refresh if WebSocket unavailable**: 30s while visible; immediate refetch after mutations.
- **Acceptance criteria**: filters are shareable; unknown payload fields do not block table; user can reach detail/create and see paper/live/status quickly.

### 5.2 State handling

| State | Strategies list behavior |
| --- | --- |
| Initial loading | Table skeleton with filter bar available. |
| Background refresh | Preserve rows; show table refresh indicator. |
| Empty result | Empty table with create action and clear filters if filters active. |
| Partial data | If latest run summary malformed, row shows unknown summary but strategy row remains. |
| API error | Table error panel with retry. |
| Retry | Retry list query preserving filters. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Show rate-limited table banner and last data. |
| Conflict | Row action conflict refetches list and row. |
| Validation failure | Mutation validation shown in action dialog/form, not table. |
| WebSocket disconnected | No table replacement; show realtime updates paused. |
| Stale data | Last updated highlighted; row actions require refetch. |
| Offline | Cached read-only list; mutations disabled. |
| 501 unavailable | Not expected for list; show feature unavailable if received. |
| Unknown enum | Raw status/market type displayed with unknown badge and actions conservatively hidden. |

## 6. Strategy detail

### 6.1 Screen contract

- **Route and purpose**: `/strategies/:id`; inspect strategy configuration, recent runs, reports, events, exposure links, and operational controls.
- **Primary user mode**: Operator; Researcher secondary.
- **Questions answered**: What does this strategy do? Is it active/paper/live? What happened recently? What can be safely intervened?
- **Required REST queries**: `GET /strategies/{id}`, `GET /runs?strategy_id={id}`, `GET /events?strategy_id={id}`, `GET /strategies/{id}/reports/latest`, `GET /strategies/{id}/reports`, optionally `/portfolio/positions?ticker=...` and `/orders` local-linked where row IDs exist.
- **Required mutations**: `POST /strategies/{id}/run|pause|resume|skip-next`, `DELETE /strategies/{id}` via safety policy.
- **Required WebSocket subscriptions**: send `{action:'subscribe', strategy_ids:[id]}`; shell may still have all-events.
- **URL query parameters and persistent filter state**: `tab=overview|config|runs|reports|events|execution|risk`, `run_id`, `report_type`, `from`, `limit`, `offset` for embedded lists.
- **Page layout regions**: header with identity/status/paper-live/actions; tabs; overview summary; config JSON panel; runs table; reports panel; events timeline; linked execution/exposure panels.
- **Table columns, default sorting, and filters**: runs columns `status`, `ticker`, `signal`, `trade_date`, `started_at`, `completed_at`, `duration`, `error`; newest started first. Reports columns unknown artifact-safe `report_type`, `status`, timestamps, raw metadata; newest first. Events columns type, run, agent if present, timestamp; newest first.
- **Primary actions**: edit, run, pause/resume, skip next, open latest run.
- **Secondary actions**: delete, copy ID, open runs/events filtered, open reports.
- **Entity deep links**: `/strategies/:id/edit`, `/runs/:id`, `/runs?strategy_id=:id`, `/events?strategy_id=:id`, `/orders?...`, `/portfolio?...`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: header emphasizes `is_paper`; live strategy actions require safety policy.
- **Responsive behavior**: tabs collapse to segmented dropdown on mobile; action bar remains sticky but compact.
- **Keyboard and accessibility expectations**: tabs use proper tablist; JSON viewer keyboard expandable; action menu accessible.
- **Last-updated and stale-data behavior**: primary strategy fetch controls action freshness; each tab shows its own timestamp.
- **Suggested polling/refresh if WebSocket unavailable**: strategy 30s; runs/events 10–15s if active, otherwise 30–60s.
- **Acceptance criteria**: user can explain status/recent behavior, inspect config, and navigate to runs/events without ambiguous payload crashes.

### 6.2 State handling

| State | Strategy detail behavior |
| --- | --- |
| Initial loading | Header skeleton and tab skeleton until strategy loads. |
| Background refresh | Header/action status refresh indicator; keep tabs visible. |
| Empty result | Empty runs/reports/events panels independently. |
| Partial data | Missing reports/events shown as panel errors; overview/config remains. |
| API error | If strategy fetch fails 404/error, route-level entity error; subqueries inline. |
| Retry | Retry entity or individual tab query. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Keep cached entity; pause polling. |
| Conflict | Mutating conflict refetches strategy and explains state changed. |
| Validation failure | Action/form validation in dialog or edit screen. |
| WebSocket disconnected | Strategy realtime badge degraded; switch tab polling on. |
| Stale data | Actions require refetch; stale tabs labelled. |
| Offline | Cached read-only detail; actions disabled. |
| 501 unavailable | Reports/events panels show not configured if applicable. |
| Unknown enum | Unknown strategy/run/report status shown raw; action availability conservative. |

## 7. Strategy create/edit flow

### 7.1 Screen contract

- **Route and purpose**: `/strategies/new`, `/strategies/:id/edit`; create or update full strategy objects.
- **Primary user mode**: Operator.
- **Questions answered**: What fields are required? Is this paper or live? Is config valid enough to submit?
- **Required REST queries**: edit: `GET /strategies/{id}`; create: no required entity query; optional settings for environment/broker context.
- **Required mutations**: create `POST /strategies`; edit `PUT /strategies/{id}` with full object; no partial patch.
- **Required WebSocket subscriptions**: shell only; none specific.
- **URL query parameters and persistent filter state**: `from`, `ticker`, `market_type`, `tab=config|basics`.
- **Page layout regions**: header/back; basics form; scheduling/status/paper-live controls; config JSON editor; validation/error panel; submit/cancel action bar.
- **Table columns, default sorting, and filters**: none.
- **Primary actions**: save create/update.
- **Secondary actions**: cancel/back; reset form to loaded version; validate JSON locally.
- **Entity deep links**: after save `/strategies/:id`; cancel returns `from` or `/strategies`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: default create `is_paper=true` as UI assumption; live selection visibly marked and safety policy required before save if changing to live.
- **Responsive behavior**: one-column form on mobile; JSON editor remains usable with line wrapping and full-screen option.
- **Keyboard and accessibility expectations**: labelled fields; JSON parse errors linked to editor; submit disabled while invalid/in flight.
- **Last-updated and stale-data behavior**: edit screen warns if loaded entity becomes stale before save; refetch option.
- **Suggested polling/refresh if WebSocket unavailable**: none; edit entity may refetch on focus after 60s.
- **Acceptance criteria**: create/edit sends only documented full object; backend validation errors are visible; user cannot accidentally treat live as paper.

### 7.2 State handling

| State | Strategy form behavior |
| --- | --- |
| Initial loading | Edit shows form skeleton; create shows blank form immediately. |
| Background refresh | If dirty, do not overwrite; warn newer server data exists. |
| Empty result | Not applicable; 404 for edit is entity error. |
| Partial data | Optional settings unavailable does not block form, but broker/live context unknown. |
| API error | Load/save error panel; preserve user input on save failure. |
| Retry | Retry load or submit after user action. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Preserve form; tell user to retry later. |
| Conflict | Refetch and show server state changed; user chooses reload or copy changes. |
| Validation failure | Field/form-level errors; JSON editor highlights parse errors; backend message preserved. |
| WebSocket disconnected | No direct impact; show shell degraded. |
| Stale data | Save requires refetch or explicit safety-policy acknowledgement for stale edit. |
| Offline | Draft remains local UI state; submit disabled. |
| 501 unavailable | Not expected; show endpoint unavailable if received. |
| Unknown enum | Preserve unknown current value in edit form; do not coerce/drop it. |

## 8. Runs list

### 8.1 Screen contract

- **Route and purpose**: `/runs`; browse pipeline runs for operational/research investigation.
- **Primary user mode**: Operator.
- **Questions answered**: What ran, when, for which strategy/ticker, with what status/signal?
- **Required REST queries**: `GET /runs` with `strategy_id`, `ticker`, `status`, `start_date`, `end_date`, `trade_date`, `limit`, `offset`.
- **Required mutations**: optional row `POST /runs/{id}/cancel` via safety policy.
- **Required WebSocket subscriptions**: shell `subscribe_all`.
- **URL query parameters and persistent filter state**: all run list query params above.
- **Page layout regions**: header; filter bar; runs table; pagination; optional selected run preview.
- **Table columns, default sorting, and filters**: `status`, `ticker`, `strategy_id`, `signal`, `trade_date`, `started_at`, `completed_at`, `duration`, `error_message`, actions. Default sort newest `started_at` first; filters mirror backend.
- **Primary actions**: open run detail; cancel running run via safety policy.
- **Secondary actions**: open strategy; copy run ID; clear filters.
- **Entity deep links**: `/runs/:id`, `/strategies/:strategy_id`, `/events?pipeline_run_id=:id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: if run/strategy data exposes paper/live, display; otherwise link to strategy. Do not infer.
- **Responsive behavior**: table to card list on mobile.
- **Keyboard and accessibility expectations**: row navigation and action menus accessible; status includes text.
- **Last-updated and stale-data behavior**: list timestamp; running rows stale quickly.
- **Suggested polling/refresh if WebSocket unavailable**: 10s when `status=running`; otherwise 30s.
- **Acceptance criteria**: filters produce shareable URLs; running runs are easy to find; unknown fields do not break rows.

### 8.2 State handling

| State | Runs list behavior |
| --- | --- |
| Initial loading | Table skeleton. |
| Background refresh | Preserve rows with refresh indicator. |
| Empty result | Empty message with clear filters and link to strategies. |
| Partial data | Malformed latest fields rendered raw/unknown per row. |
| API error | Table error with retry. |
| Retry | Retry preserving filters. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Keep cached rows; reduce polling. |
| Conflict | Cancel conflict refetches row/list. |
| Validation failure | Invalid filter/date from backend shown near filter bar. |
| WebSocket disconnected | Running list relies on polling banner. |
| Stale data | Running rows flagged if not refreshed recently. |
| Offline | Cached read-only. |
| 501 unavailable | Not expected; show feature unavailable if received. |
| Unknown enum | Raw run status/signal displayed; cancel hidden unless status known running. |

## 9. Run detail

### 9.1 Screen contract

- **Route and purpose**: `/runs/:id`; inspect timeline, decisions, snapshots, events, and execution side effects.
- **Primary user mode**: Operator; Researcher secondary.
- **Questions answered**: What is this run doing/did it do? Why did agents decide? What data supported the decision? What orders/trades resulted?
- **Required REST queries**: `GET /runs/{id}`, `GET /runs/{id}/decisions?include_prompt=...`, `GET /runs/{id}/snapshot`, `GET /events?pipeline_run_id={id}`, optional `/orders` and `/trades` linked from returned IDs/rows.
- **Required mutations**: `POST /runs/{id}/cancel` via safety policy.
- **Required WebSocket subscriptions**: `{action:'subscribe', run_ids:[id]}`.
- **URL query parameters and persistent filter state**: `tab=timeline|decisions|snapshot|orders|events`, `decision`, `include_prompt`, `data_type`, `from`, `limit`, `offset`.
- **Page layout regions**: run header/status; tab nav; timeline; decisions table/detail; snapshot JSON browser; events table; related execution panel.
- **Table columns, default sorting, and filters**: decisions columns `agent_role`, `phase`, status/signal if present, confidence if present, created timestamp, prompt available; default chronological by decision order/time. Events newest first or timeline chronological toggle. Related orders/trades use their P0 columns.
- **Primary actions**: cancel running run; inspect decision; reveal prompt explicitly; inspect snapshot; open strategy.
- **Secondary actions**: copy run ID; open events filtered; open order/trade links.
- **Entity deep links**: `/strategies/:strategy_id`, `/runs/:id?tab=decisions&decision=...`, `/runs/:id?tab=snapshot&data_type=...`, `/events?pipeline_run_id=:id`, `/orders/:id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: show if run/strategy exposes it; otherwise show “paper/live unknown until strategy loaded”.
- **Responsive behavior**: header collapses; timeline and tables become stacked cards; JSON viewer full-width.
- **Keyboard and accessibility expectations**: tabs accessible; decision rows open detail with Enter; JSON tree keyboard navigable; prompt reveal announced.
- **Last-updated and stale-data behavior**: run status and decisions show independent refresh times; action bar requires fresh run.
- **Suggested polling/refresh if WebSocket unavailable**: running run 5–10s for run/events/decisions; completed run 30–60s/manual.
- **Acceptance criteria**: user can explain run status, decision evidence, and execution links; snapshot/replay unavailability is contained.

### 9.2 State handling

| State | Run detail behavior |
| --- | --- |
| Initial loading | Header skeleton; tabs disabled until run exists. |
| Background refresh | Keep timeline/decisions; show refresh indicators per tab. |
| Empty result | Empty decisions/events/snapshot panels independently. |
| Partial data | Snapshot/events/order panels can fail without replacing run header/decisions. |
| API error | Run fetch error is route-level; tab errors are inline. |
| Retry | Retry entity or specific tab. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Freeze current timeline; reduce polling. |
| Conflict | Cancel conflict refetches run and reports status changed. |
| Validation failure | Prompt/filter validation shown near controls. |
| WebSocket disconnected | Timeline marks realtime gap; REST polling enabled. |
| Stale data | Running status stale warning; cancel requires refetch. |
| Offline | Cached read-only run; cancel disabled. |
| 501 unavailable | Snapshot/events/replay panels show not configured. |
| Unknown enum | Unknown run status/signal/phase displayed raw; action eligibility conservative. |

## 10. Portfolio

### 10.1 Screen contract

- **Route and purpose**: `/portfolio`; inspect current and historical exposure.
- **Primary user mode**: Operator.
- **Questions answered**: What exposure is open? What is realized/unrealized P&L? Which positions link to trades/strategies?
- **Required REST queries**: `GET /portfolio/summary`, `GET /portfolio/positions/open`, `GET /portfolio/positions`, optional allocator P0 diagnostics if enabled: `/portfolio/allocator/summary|diagnostics|opportunities|decisions`.
- **Required mutations**: none confirmed.
- **Required WebSocket subscriptions**: shell `subscribe_all`; position/order events consumed.
- **URL query parameters and persistent filter state**: `tab=open|all|allocator`, `ticker`, `side`, `limit`, `offset`, `strategy_id`, `position_id` row focus.
- **Page layout regions**: P&L cards; exposure summary; tabs; positions table; allocator panels; linked trades drawer.
- **Table columns, default sorting, and filters**: positions columns `ticker`, `side`, `quantity`, `avg_entry`, `current_price`, `unrealized_pnl`, `realized_pnl`, `opened_at`, `closed_at`, `strategy_id`, option greeks when present. Default sort open first then newest `opened_at`; filters ticker/side/open/all.
- **Primary actions**: inspect position; open linked trades; open strategy.
- **Secondary actions**: open risk console; copy position ID; clear filters.
- **Entity deep links**: `/trades?position_id=:id`, `/strategies/:strategy_id`, `/portfolio?position_id=:id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: if positions expose market/broker paper/live indirectly, show; otherwise do not infer. Live broker header remains visible.
- **Responsive behavior**: cards stacked; positions as cards with key risk/P&L fields first.
- **Keyboard and accessibility expectations**: numeric columns have accessible labels; negative P&L not color-only.
- **Last-updated and stale-data behavior**: P&L cards prominently show age; stale prices make exposure unknown/degraded.
- **Suggested polling/refresh if WebSocket unavailable**: summary/open positions every 15s; all positions 30–60s.
- **Acceptance criteria**: user can inspect open exposure and trace to trades/strategy where IDs exist; allocator failures do not hide core positions.

### 10.2 State handling

| State | Portfolio behavior |
| --- | --- |
| Initial loading | KPI and table skeletons. |
| Background refresh | Keep values; animate refresh per card/table. |
| Empty result | No open positions treated neutral only with fresh data. |
| Partial data | Summary/open/all/allocator fail independently. |
| API error | Widget-level error; full-page only if all core portfolio queries fail. |
| Retry | Retry failed widget or all portfolio. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Keep cached exposure; mark stale/rate-limited. |
| Conflict | Not expected. |
| Validation failure | Invalid filters shown near filter bar. |
| WebSocket disconnected | Position updates paused; polling fallback enabled. |
| Stale data | P&L/exposure marked unreliable; cockpit safety uses unknown. |
| Offline | Cached read-only. |
| 501 unavailable | Allocator panels can show not configured; core endpoints not expected 501. |
| Unknown enum | Unknown side/market displayed raw; no aggregation assumptions. |

## 11. Orders

### 11.1 Screen contract

- **Route and purpose**: `/orders`; inspect order execution list.
- **Primary user mode**: Operator.
- **Questions answered**: What orders were submitted? Which filled/failed/are partial? Which strategy/run/broker produced them?
- **Required REST queries**: `GET /orders` with `ticker`, `broker`, `market_type`, `status`, `side`, `order_type`, `limit`, `offset`.
- **Required mutations**: none confirmed; no order cancel endpoint exists.
- **Required WebSocket subscriptions**: shell `subscribe_all`.
- **URL query parameters and persistent filter state**: all documented order filters; `strategy_id` and `run_id` only frontend context/client filtering unless backend confirms.
- **Page layout regions**: header; filters; orders table; pagination; selected row preview.
- **Table columns, default sorting, and filters**: `status`, `ticker`, `side`, `order_type`, `quantity`, `filled_quantity`, `filled_avg_price`, `broker`, `market_type`, `submitted_at`, `filled_at`, `strategy_id`, `pipeline_run_id`, `external_id`; default newest `submitted_at || created_at` first.
- **Primary actions**: open order detail.
- **Secondary actions**: open run/strategy; copy external ID; clear filters.
- **Entity deep links**: `/orders/:id`, `/runs/:pipeline_run_id`, `/strategies/:strategy_id`, `/trades?order_id=:id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: show broker and market; paper/live only if confirmed by linked strategy/settings, not inferred from broker string.
- **Responsive behavior**: dense desktop table; mobile cards prioritize status/ticker/side/fill/broker/time.
- **Keyboard and accessibility expectations**: status text; row navigation; copy buttons labelled.
- **Last-updated and stale-data behavior**: table age shown; recent orders stale quickly.
- **Suggested polling/refresh if WebSocket unavailable**: 15s for active/recent orders; 30–60s otherwise.
- **Acceptance criteria**: user can filter orders and open execution chain; no cancel action is shown because endpoint is absent.

### 11.2 State handling

| State | Orders behavior |
| --- | --- |
| Initial loading | Table skeleton. |
| Background refresh | Preserve rows with refresh indicator. |
| Empty result | Empty table with clear filters. |
| Partial data | Unknown optional asset fields omitted/JSON-viewed per row. |
| API error | Table error panel. |
| Retry | Retry list preserving filters. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Keep cached rows; reduce polling. |
| Conflict | Not expected without mutations. |
| Validation failure | Invalid filter enum shown near filter bar. |
| WebSocket disconnected | Order events paused; polling fallback. |
| Stale data | Recent execution marked stale. |
| Offline | Cached read-only. |
| 501 unavailable | Not expected; show feature unavailable if received. |
| Unknown enum | Unknown order status/type displayed raw; status grouping excludes unknown. |

## 12. Order detail

### 12.1 Screen contract

- **Route and purpose**: `/orders/:id`; inspect one order and its fills/trades.
- **Primary user mode**: Operator.
- **Questions answered**: What happened to this order? What fills/trades exist? Which run/strategy/position does it relate to?
- **Required REST queries**: `GET /orders/{id}`; optional `GET /trades?order_id={id}` if separate fills table needed.
- **Required mutations**: none confirmed.
- **Required WebSocket subscriptions**: shell `subscribe_all`; no order-specific command confirmed.
- **URL query parameters and persistent filter state**: `from`, `tab=summary|fills|raw`.
- **Page layout regions**: header/status; order facts; fill/trade table; related entities; raw JSON inspector.
- **Table columns, default sorting, and filters**: fills/trades columns `side`, `quantity`, `price`, `fee`, `executed_at`, `position_id`, `external_id`, `open_close`; default executed time ascending for execution chronology.
- **Primary actions**: open related run/strategy/trades/position context.
- **Secondary actions**: copy order ID/external ID; return to `from`.
- **Entity deep links**: `/runs/:pipeline_run_id`, `/strategies/:strategy_id`, `/trades?order_id=:id`, `/trades?position_id=:position_id`, `/portfolio?position_id=:position_id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: show broker and linked strategy paper/live if available; otherwise unknown.
- **Responsive behavior**: facts as definition list; fills as cards on mobile.
- **Keyboard and accessibility expectations**: copy buttons labelled; raw JSON collapsible accessibly.
- **Last-updated and stale-data behavior**: header and fills have timestamps; refetch after reconnect.
- **Suggested polling/refresh if WebSocket unavailable**: if status not terminal, poll 10–15s; terminal 60s/manual.
- **Acceptance criteria**: user can trace order to fills/trades and linked run/strategy where IDs exist.

### 12.2 State handling

| State | Order detail behavior |
| --- | --- |
| Initial loading | Header/fills skeleton. |
| Background refresh | Keep order visible; refresh badge. |
| Empty result | No fills shown as pending/unfilled only if order data fresh. |
| Partial data | Fills/trades query may fail while order facts remain. |
| API error | Order fetch error route-level; fills inline. |
| Retry | Retry order or fills. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Keep cached order; reduce polling. |
| Conflict | Not expected. |
| Validation failure | Invalid UUID route shows route error/not found. |
| WebSocket disconnected | Live fill updates paused; polling fallback. |
| Stale data | Nonterminal order stale warning. |
| Offline | Cached read-only. |
| 501 unavailable | Not expected. |
| Unknown enum | Unknown order/trade status rendered raw. |

## 13. Trades

### 13.1 Screen contract

- **Route and purpose**: `/trades`; inspect trades/fills across orders and positions.
- **Primary user mode**: Operator.
- **Questions answered**: What trades executed? Which order/position/ticker/side/time range do they belong to?
- **Required REST queries**: `GET /trades` with `order_id`, `position_id`, `ticker`, `side`, `start_date`, `end_date`, `limit`, `offset`. Do not combine `order_id` and `position_id`.
- **Required mutations**: none confirmed.
- **Required WebSocket subscriptions**: shell `subscribe_all`.
- **URL query parameters and persistent filter state**: all documented trade filters above.
- **Page layout regions**: header; filter bar; trades table; pagination; selected trade raw drawer.
- **Table columns, default sorting, and filters**: `executed_at`, `ticker`, `side`, `quantity`, `price`, `fee`, `order_id`, `position_id`, `external_id`, `open_close`, `premium`, `exit_reason`; default newest executed first.
- **Primary actions**: open related order or position context.
- **Secondary actions**: copy trade ID; clear filters.
- **Entity deep links**: `/orders/:order_id`, `/trades?position_id=:position_id`, `/portfolio?position_id=:position_id`.
- **Permissions or credentials**: authenticated.
- **Paper-versus-live behavior**: not directly confirmed on trade; show linked broker/strategy context if available.
- **Responsive behavior**: mobile cards prioritize executed/ticker/side/qty/price/order.
- **Keyboard and accessibility expectations**: numeric values labelled; filters keyboard accessible.
- **Last-updated and stale-data behavior**: table timestamp; new fills require websocket or polling.
- **Suggested polling/refresh if WebSocket unavailable**: 15–30s for recent windows; manual for historical ranges.
- **Acceptance criteria**: user can filter by order or position and navigate execution chain.

### 13.2 State handling

| State | Trades behavior |
| --- | --- |
| Initial loading | Table skeleton. |
| Background refresh | Preserve rows. |
| Empty result | Empty table with clear filters. |
| Partial data | Unknown optional derivative fields hidden or raw-expanded. |
| API error | Table error panel. |
| Retry | Retry preserving filters. |
| Unauthorized | Redirect after refresh failure. |
| Rate limited | Cached rows with rate-limit banner. |
| Conflict | Not expected. |
| Validation failure | Combined `order_id` and `position_id` or invalid dates shown near filters. |
| WebSocket disconnected | Live trade feed paused; polling fallback. |
| Stale data | Recent trade data marked stale. |
| Offline | Cached read-only. |
| 501 unavailable | Not expected. |
| Unknown enum | Unknown side/open_close rendered raw. |

## 14. Risk console

### 14.1 Screen contract

- **Route and purpose**: `/risk`; inspect risk state and invoke risk controls.
- **Primary user mode**: Operator; Administrator for breaker reset.
- **Questions answered**: Is risk normal/warning/breached? Is kill switch active? Which breakers are tripped? Which markets are stopped? Can controls be verified?
- **Required REST queries**: `GET /risk/status`, `GET /risk/cockpit`, `GET /risk/breakers`.
- **Required mutations**: `POST /risk/killswitch`, `POST /risk/market/{type}/stop`, `POST /risk/market/{type}/resume`, `POST /risk/breaker/reset` with `X-Admin-Key`; all through safety policy.
- **Required WebSocket subscriptions**: shell `subscribe_all`; watch `circuit_breaker`, `pipeline_health`, `error`.
- **URL query parameters and persistent filter state**: `tab=status|breakers|controls`, `scope`, `market_type`, `from`.
- **Page layout regions**: critical risk banner; kill switch panel; market stops panel; breaker table; risk limits/status detail; cockpit aggregate raw panel for ambiguous fields.
- **Table columns, default sorting, and filters**: breakers columns `scope`, `reason`, `tripped_at`, `reset_at`, action; default active/tripped first then newest `tripped_at`. Market stop rows from risk status market map where present.
- **Primary actions**: activate/deactivate kill switch; stop/resume market; reset breaker.
- **Secondary actions**: inspect audit/events; copy scope; open affected strategy if scope is `strategy:<uuid>`.
- **Entity deep links**: `/events?event_kind=circuit_breaker`, `/audit-log?event_type=kill_switch.activated`, `/strategies/:id` for strategy scopes.
- **Permissions or credentials**: authenticated; breaker reset requires one-shot `X-Admin-Key`, never persisted.
- **Paper-versus-live behavior**: risk controls affect trading globally/market-wide; display broker paper/live context from shell and warn if any live broker configured.
- **Responsive behavior**: critical controls remain visible at top; tables become cards on mobile.
- **Keyboard and accessibility expectations**: critical status announced; controls have explicit labels; dialogs focus-trapped by safety policy.
- **Last-updated and stale-data behavior**: risk state has strict freshness; deactivate/resume/reset require fresh data.
- **Suggested polling/refresh if WebSocket unavailable**: risk status/breakers every 10–15s while visible; refetch immediately after mutation.
- **Acceptance criteria**: user can see and verify risk controls; reset unavailable/admin errors are contained; stale risk never appears safe.

### 14.2 State handling

| State | Risk console behavior |
| --- | --- |
| Initial loading | Critical banner skeleton; controls disabled until status loads. |
| Background refresh | Keep status; show refresh pulse; controls wait for latest before action. |
| Empty result | No breakers shown as “no tripped breakers” only when breaker query fresh. |
| Partial data | Status/cockpit/breakers fail independently; critical banner becomes unknown if status fails. |
| API error | Inline per panel; global risk unknown if status fails. |
| Retry | Retry status/cockpit/breakers individually or all. |
| Unauthorized | Session 401 redirects; admin-key 401 stays in reset dialog. |
| Rate limited | Freeze state; disable controls until refetch. |
| Conflict | Mutation conflict refetches risk and reports state changed. |
| Validation failure | Missing reason/scope shown in safety-policy dialog. |
| WebSocket disconnected | Circuit breaker realtime paused; polling fallback. |
| Stale data | Controls blocked pending refetch; banner says stale/unknown. |
| Offline | Cached read-only; controls disabled. |
| 501 unavailable | Breaker reset/list panel not configured; other panels continue. |
| Unknown enum | Unknown risk/breaker/market values raw and treated as unknown severity. |

## 15. Global realtime activity drawer

### 15.1 Screen contract

- **Route and purpose**: global overlay/drawer, optionally `activity=open`; inspect recent realtime events and connection state.
- **Primary user mode**: Operator.
- **Questions answered**: What just happened? Which run/strategy/order/market does it relate to? Is realtime reliable?
- **Required REST queries**: optional fallback `GET /events` for persisted events; otherwise WebSocket buffer.
- **Required mutations**: WebSocket commands `subscribe_all`, `unsubscribe_all`, `subscribe`, `unsubscribe`, `subscribe_polymarket`, `unsubscribe_polymarket`.
- **Required WebSocket subscriptions**: shared session connection; buffer 250 events as brief recommends.
- **URL query parameters and persistent filter state**: `activity=open`, local filters `type`, `strategy_id`, `run_id`; if shareable, mirror to URL only without sensitive data.
- **Page layout regions**: connection state header; filters; event list/table; JSON payload inspector; linked entity actions.
- **Table columns, default sorting, and filters**: `timestamp`, `type`, `strategy_id`, `run_id`, summary from known envelope only, link target; newest first. Filters event type/run/strategy/polymarket.
- **Primary actions**: open linked entity; copy event JSON.
- **Secondary actions**: clear buffer locally; reconnect; open persisted events.
- **Entity deep links**: `/runs/:id`, `/strategies/:id`, `/events?pipeline_run_id=...`, `/events?strategy_id=...`.
- **Permissions or credentials**: authenticated WebSocket query token.
- **Paper-versus-live behavior**: not inferred from event payload; show linked strategy/broker context if loaded elsewhere.
- **Responsive behavior**: full-screen sheet on mobile; side drawer on desktop.
- **Keyboard and accessibility expectations**: focus trap; Esc closes; event arrival announcements throttled; list navigable.
- **Last-updated and stale-data behavior**: drawer shows last event time and connection age; gap after reconnect marked.
- **Suggested polling/refresh if WebSocket unavailable**: fallback link/query to `/events`; shell/page polling handles canonical data.
- **Acceptance criteria**: user sees realtime connection health, recent events, and can navigate without relying on untyped `data` fields.

### 15.2 State handling

| State | Activity drawer behavior |
| --- | --- |
| Initial loading | Empty buffer with connecting state. |
| Background refresh | Not applicable for WS; persisted events query can refresh inline. |
| Empty result | “No events received in this session.” |
| Partial data | WS buffer visible even if `/events` fallback fails, and vice versa. |
| API error | Persisted events error inline. |
| Retry | Reconnect WS or retry `/events`. |
| Unauthorized | WS 401 triggers token refresh then reconnect; failure redirects login. |
| Rate limited | REST fallback rate limited; WS remains if connected. |
| Conflict | Not applicable. |
| Validation failure | Invalid subscribe UUID command error shown in drawer diagnostics. |
| WebSocket disconnected | Drawer header shows reconnect attempts and last event time. |
| Stale data | Event gap marker after reconnect; visible pages refetch. |
| Offline | Drawer shows offline; buffer remains local. |
| 501 unavailable | `/events` fallback not configured; WS buffer still usable. |
| Unknown enum | Unknown event type shown raw with generic icon and JSON payload. |

## 16. Global confirmation and error dialogs

### 16.1 Screen contract

- **Route and purpose**: global dialog host; display safety-policy confirmations and recoverable errors.
- **Primary user mode**: all; Operator for high-risk actions.
- **Questions answered**: What action is about to happen? What failed? Can I retry safely? Do I need fresh data or credentials?
- **Required REST queries**: none directly; callers may require refetch before action.
- **Required mutations**: none directly; dialog confirms or cancels caller-owned mutation.
- **Required WebSocket subscriptions**: none.
- **URL query parameters and persistent filter state**: none; dialogs must not put secrets or admin key in URL.
- **Page layout regions**: modal title; action/error summary; endpoint/entity context; safety-policy content slot; primary/secondary buttons; details accordion with raw `ApiError` where safe.
- **Table columns, default sorting, and filters**: none.
- **Primary actions**: confirm, retry safe query, cancel/close.
- **Secondary actions**: copy error code/request context; open related entity if known.
- **Entity deep links**: caller-provided links only.
- **Permissions or credentials**: admin-key prompt only for breaker reset; key discarded immediately.
- **Paper-versus-live behavior**: safety-policy slot must display live/paper context when caller knows it; this spec does not define final policy text.
- **Responsive behavior**: centered modal desktop; bottom sheet mobile; max height with scroll.
- **Keyboard and accessibility expectations**: focus trap; initial focus on safest action; Esc behavior disabled for irreversible safety-policy confirmation only if policy requires; `aria-describedby` links to consequence text.
- **Last-updated and stale-data behavior**: if entity data stale, dialog blocks mutation and offers refetch.
- **Suggested polling/refresh if WebSocket unavailable**: none; caller controls.
- **Acceptance criteria**: all P0 operational mutations route through one dialog host/policy boundary; API errors are actionable without losing page context.

### 16.2 State handling

| State | Dialog behavior |
| --- | --- |
| Initial loading | If caller is refetching pre-action state, show verifying state before confirm button. |
| Background refresh | Keep dialog open; disable confirm until latest state known for high-risk actions. |
| Empty result | Not applicable unless caller precheck finds entity missing; show entity unavailable. |
| Partial data | Show missing context explicitly; safety policy decides whether action can proceed. |
| API error | Display error message/code and endpoint context; preserve caller page. |
| Retry | Retry only safe query/precheck automatically; mutations require user re-confirm. |
| Unauthorized | Session 401 closes to login; admin-key 401 stays in dialog with key cleared. |
| Rate limited | Disable retry until user/manual wait; show rate-limited code. |
| Conflict | Explain server state changed; offer refetch, not blind retry. |
| Validation failure | Field errors inline; preserve entered non-secret fields; clear admin key on failure. |
| WebSocket disconnected | Warn realtime is degraded if action depends on current state. |
| Stale data | Confirm disabled until refetch or safety policy permits. |
| Offline | Mutation confirm disabled; query retry waits for online. |
| 501 unavailable | Show feature not configured, endpoint, and return-to-page action. |
| Unknown enum | Show raw enum in context and ask caller/policy to treat as unknown risk. |

## 17. Traceability table

| Screen | User workflow | Endpoint(s) | WebSocket event(s) | Required state | Acceptance criterion |
| --- | --- | --- | --- | --- | --- |
| Login | 2.1 Log in and determine health | `POST /auth/login`, `POST /auth/refresh`, `GET /me`, `GET /settings`, `GET /health`, `GET /risk/status` | Shell opens `subscribe_all` after auth | Unauthorized, validation, API error, partial bootstrap | Valid login reaches cockpit; invalid login is generic; failed bootstrap becomes degraded shell. |
| Authenticated shell | 2.1 Log in and determine health; all workflows | `GET /me`, `GET /settings`, `GET /health`, `GET /risk/status` | all event types via `subscribe_all` | WebSocket disconnected, stale, partial data | Every route shows user/environment/realtime/risk and survives partial shell failures. |
| Cockpit | 2.2 Identify unsafe/degraded condition; 2.3 Investigate cockpit alert | `/risk/*`, `/portfolio/summary`, `/portfolio/positions/open`, `/runs`, `/orders`, `/trades`, `/automation/health`, `/settings`, `/health` | `circuit_breaker`, `pipeline_health`, `error`, `order_submitted`, `order_filled`, `position_update` | Partial data, stale, WS disconnected, unknown enum | Operator can classify safe/degraded/unsafe; failed widget does not replace page. |
| Strategies list | 2.6 Inspect strategy; 2.7 Intervene | `GET /strategies`, strategy action endpoints | strategy-scoped events through shell | Empty, conflict, stale, unknown enum | User can filter/share list and open detail/create; actions refetch on stale/conflict. |
| Strategy detail | 2.6 Inspect strategy; 2.7 Intervene | `GET /strategies/{id}`, `/runs?strategy_id=`, `/events?strategy_id=`, reports endpoints, strategy mutations | strategy subscription; all strategy-scoped events | Partial data, 501 reports/events, stale | User sees config/recent behavior and can navigate to runs/events/reports. |
| Strategy create/edit | 2.7 Intervene in strategy execution | `GET /strategies/{id}`, `POST /strategies`, `PUT /strategies/{id}` | none specific | Validation, conflict, stale, unknown enum | Full-object create/edit preserves unknown values and displays backend validation. |
| Runs list | 2.4 Inspect active run | `GET /runs`, `POST /runs/{id}/cancel` | `pipeline_start`, `pipeline_health`, `error`, `signal` | Empty, stale running rows, conflict | User can find running/recent runs and open detail; cancel gated by policy. |
| Run detail | 2.4 Inspect active run; 2.5 Inspect decision/snapshot | `GET /runs/{id}`, `/runs/{id}/decisions`, `/runs/{id}/snapshot`, `/events`, `/orders`, `/trades`, cancel | `pipeline_start`, `agent_decision`, `debate_round`, `signal`, order/position/error events | Partial data, 501 snapshot/events, WS disconnected | User can inspect status, reasoning, snapshots, and execution links even with partial failures. |
| Portfolio | 2.8 Inspect portfolio exposure | `/portfolio/summary`, `/portfolio/positions/open`, `/portfolio/positions`, allocator endpoints | `position_update`, `order_filled` | Partial data, stale, empty | User can see open exposure/P&L and trace positions to trades/strategy where IDs exist. |
| Orders | 2.9 Trace order through fills/trades/positions | `GET /orders` | `order_submitted`, `order_filled` | Empty, validation filters, unknown enum | User can filter orders and open detail; no unsupported cancel action appears. |
| Order detail | 2.9 Trace order through fills/trades/positions | `GET /orders/{id}`, `GET /trades?order_id=` | `order_submitted`, `order_filled`, `position_update` | Partial fills failure, stale nonterminal, unknown enum | User can see order facts/fills and navigate to run/strategy/trades. |
| Trades | 2.9 Trace order through fills/trades/positions | `GET /trades` | `order_filled`, `position_update` | Validation for mutually exclusive IDs, empty, stale | User can filter by order or position and inspect execution chain. |
| Risk console | 2.10 Activate and verify risk control | `/risk/status`, `/risk/cockpit`, `/risk/breakers`, risk mutations | `circuit_breaker`, `pipeline_health`, `error` | Partial data, stale, unauthorized admin, 501 | User can inspect/verify controls; stale risk blocks unsafe actions. |
| Activity drawer | 2.3 Investigate cockpit alert; all realtime workflows | optional `GET /events` | all event types | WS disconnected, empty buffer, 501 events fallback, unknown event | User can inspect recent events and navigate via confirmed IDs. |
| Dialog host | 2.7, 2.10, operational mutations | caller-owned endpoints | none direct | conflict, validation, unauthorized, stale, offline | High-risk actions are mediated by policy boundary and errors preserve page context. |

## 18. Finish summary

### Files created or changed

- Created `docs/frontend/05-p0-screen-specifications.md`.

### Screens fully specified

- Login
- Authenticated application shell
- Cockpit
- Strategies list
- Strategy detail
- Strategy create/edit flow
- Runs list
- Run detail
- Portfolio
- Orders
- Order detail
- Trades
- Risk console
- Global realtime activity drawer
- Global confirmation and error dialogs

### Screens blocked by missing API contracts

- **Strategy detail/form**: strict `domain.Strategy` schema and strategy config schema are not confirmed.
- **Run detail**: decision DTO, prompt sensitivity policy, snapshot `data_type` schemas, and event payloads are not confirmed.
- **Portfolio**: allocator diagnostics/summary/opportunities/decisions payloads are ambiguous.
- **Orders/trades/positions**: exact domain schemas and enum vocabularies need schema extraction.
- **Risk console/cockpit widgets**: risk status/cockpit DTO and severity aggregation rules are not confirmed.
- **Activity drawer**: WebSocket `data` payloads are untyped; persisted `AgentEvent` schema is ambiguous.

### Assumptions introduced

1. P0 is delivered as a single screen-spec document using route sections rather than one file per route.
2. Safety-policy details are intentionally separated; screen specs only identify policy boundaries.
3. Unknown enum values are rendered raw and treated conservatively for action eligibility.
4. Strategy create defaults to `is_paper=true` in the UI, matching earlier approved assumption.
5. Row-focus/query-state is used for positions/fills/events where no detail endpoint is confirmed.
6. Cockpit may query `/orders` and `/trades` for recent execution, but must not rely on undocumented `status=open`.
