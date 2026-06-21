# P0 Frontend Vertical Slice Backlog

Status: implementation planning only. Do not treat this as feature code.

Inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/02-user-workflows.md`, `03-information-architecture.md`, `04-entity-navigation-model.md`, `05-p0-screen-specifications.md`, `06-interaction-operational-safety.md`, `07-design-system-foundations.md`, `08-shared-component-inventory.md`, `09-status-and-data-display-conventions.md`, `10-frontend-architecture.md`, `11-operational-mutation-pattern.md`, current vertical-slice source/tests, current mocks, and backend route inventory.

## Shared assumptions

- All authenticated P0 routes run inside the existing app shell and shared WebSocket provider.
- Mutations follow `11-operational-mutation-pattern.md`: confirmation, no optimistic update, no auto retry, single-flight, invalidation, and server-state verification.
- Live/risk/admin mutations remain blocked or feature-gated until backend RBAC, atomic preconditions, and audit/authorization gaps are resolved.
- Every list slice uses offset pagination (`limit`, `offset`) and URL-persisted filters where backend filters are confirmed.
- Every slice must handle loading, empty, error, stale, rate-limit, unauthorized, and `501 ERR_NOT_IMPLEMENTED` states locally.

## Ordered slices

### 01. Strategies list read-only

- **User outcome:** Operator can browse, filter, and deep-link to strategies with paper/live and status clarity.
- **Routes:** `/strategies`.
- **REST queries:** `GET /api/v1/strategies?ticker&market_type&status&is_paper&limit&offset`.
- **Mutations:** None.
- **WebSocket events:** `pipeline_start`, `pipeline_health`, `signal`, `error` may mark rows stale; no cache patching required.
- **Types/schemas:** `Strategy`, `StrategyLatestRunSummary`, `ListResponse<Strategy>`, forward-compatible status/market enums.
- **Shared components:** `PageHeader`, `FilterBar`, `DataTable`, `StatusBadge`, `PaperLiveBadge`, `PaginationControls`, `EntityLink`, `LastUpdated`, `EmptyState`, `ErrorState`.
- **Mock scenarios:** success, empty list, unknown status, partial malformed latest run summary, 401, 429, 500, 501, slow response.
- **States:** table skeleton; empty with create CTA disabled/available per future slice; row-level unknown summary; stale table banner; feature-unavailable panel for 501.
- **Accessibility:** labelled filters, table headers/scopes, row Enter opens detail, action cells not focus traps, status text not color-only.
- **Safety:** no row mutations yet; live rows explicitly labelled; stale rows do not expose action buttons.
- **Tests:** filters in URL, empty state, unknown enum badge, 501, 429, row link to detail, keyboard row navigation.
- **Dependencies:** current auth/app shell; strategy endpoint wrapper; query key family.
- **Backend blockers:** confirm list filters and whether `total` is reliable.
- **Exclusions:** create/edit/actions/reports.
- **Acceptance criteria:** user can find strategy rows, identify paper/live/status, paginate, and open `/strategies/:id`.

### 02. Strategy detail read-only completion

- **User outcome:** Operator can inspect strategy identity, config, recent run summary, and linked evidence without mutations.
- **Routes:** `/strategies/:id?tab=overview|config`.
- **REST queries:** `GET /strategies/{id}`.
- **Mutations:** None; existing pause button may remain behind paper guard but is not expanded here.
- **WebSocket events:** strategy/run events mark detail stale.
- **Types/schemas:** `Strategy`, raw JSON config schema, optional `latest_run_summary`.
- **Shared components:** `PageHeader`, `Breadcrumbs`, `KeyValueSummary`, `JsonViewer`, `Tabs`, `StatusBadge`, `PaperLiveBadge`, `CopyButton`.
- **Mock scenarios:** active/paused/inactive, paper/live, missing latest summary, unknown status, config with unknown nested fields, 404, 500, 501.
- **States:** detail skeleton; 404 not-found; config empty/unknown; stale detail banner; 501 unavailable.
- **Accessibility:** tablist semantics, JSON viewer keyboard expansion/copy labels, heading hierarchy.
- **Safety:** no new actions; live mode prominent; stale state blocks existing action controls.
- **Tests:** route load, 404, unknown status, JSON config render escaped, tabs keyboard behavior.
- **Dependencies:** slice 01 useful but not required; existing detail route.
- **Backend blockers:** detail endpoint may omit `latest_run_summary`; do not depend on it.
- **Exclusions:** reports, runs/events tabs, edit/create.
- **Acceptance criteria:** strategy detail is usable and safe without relying on list context.

### 03. Strategy create form — paper-only

- **User outcome:** Operator can create a paper strategy with validated required fields.
- **Routes:** `/strategies/new`.
- **REST queries:** `GET /settings` for environment/broker context; optional no-op defaults.
- **Mutations:** `POST /strategies` with `is_paper: true` only.
- **WebSocket events:** none required; after create, list/detail may update from REST.
- **Types/schemas:** `Strategy`, create request schema, config raw JSON schema.
- **Shared components:** `PageHeader`, `FormField`, `JsonViewer`/JSON editor mode if approved, `ConfirmationDialog`, `InlineAlert`, `ErrorState`.
- **Mock scenarios:** success, validation error, conflict if duplicate name/ticker, 401, 429, 500, malformed response.
- **States:** form skeleton if settings required; validation inline; preserve input on errors; unknown completion if network failure after submit.
- **Accessibility:** visible labels/errors, first invalid field focus, confirmation focus trap, form submit via Enter only where safe.
- **Safety:** paper-only default; C1 confirmation; no optimism; refetch created strategy and list before success message.
- **Tests:** required fields, invalid JSON, confirmation required, duplicate submit, validation preservation, success navigate to detail, unknown completion.
- **Dependencies:** slices 01–02, mutation pattern, endpoint wrapper.
- **Backend blockers:** confirm create request validation and whether backend enforces `is_paper`/live flags.
- **Exclusions:** live create, edit, run/pause/resume.
- **Acceptance criteria:** new paper strategy appears via verified detail fetch and list invalidation.

### 04. Strategy edit form — paper-only safe fields

- **User outcome:** Operator can edit low-risk paper strategy fields and verify saved server state.
- **Routes:** `/strategies/:id/edit`.
- **REST queries:** `GET /strategies/{id}` before edit; `GET /settings` for mode context.
- **Mutations:** `PUT /strategies/{id}` for paper strategies only.
- **WebSocket events:** strategy events mark form stale; no patching.
- **Types/schemas:** `Strategy`, update request schema, config raw JSON.
- **Shared components:** `PageHeader`, `Breadcrumbs`, `ConfirmationDialog`, `InlineAlert`, `JsonViewer`/editor, `ErrorState`.
- **Mock scenarios:** success, live disabled, stale conflict, validation error, 401, 429, 500, failed verification.
- **States:** dirty form, stale source warning, conflict preserve user input, verification failure unknown state.
- **Accessibility:** dirty-state route warning if implemented, field-level errors, keyboard confirmation.
- **Safety:** paper-only; C1; no optimism; refetch before mutation if stale; refetch after success; invalidate list/detail/runs/events.
- **Tests:** live cannot save, stale refetch block, validation preservation, duplicate submit, success verification.
- **Dependencies:** slices 02–03.
- **Backend blockers:** no version/etag; update may overwrite concurrent changes; audit gaps.
- **Exclusions:** live edit, destructive delete, status/actions.
- **Acceptance criteria:** saved fields match refetched server detail, and conflict/unknown completion are visible.

### 05. Strategy reports read-only

- **User outcome:** Researcher/operator can inspect latest and historical strategy reports.
- **Routes:** `/strategies/:id?tab=reports`, optional `/strategies/:id/reports`.
- **REST queries:** `GET /strategies/{id}/reports/latest`, `GET /strategies/{id}/reports?limit&offset`.
- **Mutations:** None.
- **WebSocket events:** `pipeline_health`, `signal`, `error` mark reports stale when tied to strategy.
- **Types/schemas:** report summary/detail schema; raw JSON fallback for unknown report payloads.
- **Shared components:** `Tabs`, `DataTable`, `JsonViewer`, `DataChart` only if numeric report fields confirmed, `EmptyState`, `FeatureUnavailable`.
- **Mock scenarios:** no reports, latest report, malformed/unknown metadata, 404 latest, 501 reports not configured, 500.
- **States:** latest skeleton; empty history; partial latest/history failure; stale after run event.
- **Accessibility:** report tables labelled; JSON viewer accessible; charts require table fallback if used.
- **Safety:** read-only; do not infer trading safety from stale reports.
- **Tests:** latest present/empty, history pagination, 501, unknown metadata render.
- **Dependencies:** slice 02.
- **Backend blockers:** confirm report DTO fields and endpoint implementation status.
- **Exclusions:** report generation/export.
- **Acceptance criteria:** reports render without schema crashes and link back to strategy/run evidence.

### 06. Runs list read-only

- **User outcome:** Operator can browse pipeline runs by status/strategy/ticker/date.
- **Routes:** `/runs?status&strategy_id&ticker&start_date&end_date&limit&offset`.
- **REST queries:** `GET /runs` with confirmed filters.
- **Mutations:** None.
- **WebSocket events:** `pipeline_start`, `signal`, `error`, `pipeline_health` mark/refetch visible list.
- **Types/schemas:** `PipelineRun`, `ListResponse<PipelineRun>`, forward-compatible run statuses/signals.
- **Shared components:** `PageHeader`, `FilterBar`, `DateRangeFilter`, `DataTable`, `StatusBadge`, `EntityLink`, `PaginationControls`.
- **Mock scenarios:** running/completed/failed/cancelled, empty, unknown status, missing total, 401, 429, 500, 501.
- **States:** skeleton; empty by filter; rate-limited stale list; feature unavailable.
- **Accessibility:** table headers; date filters labelled; status text; row links.
- **Safety:** no cancel action yet; stale rows read-only.
- **Tests:** filters URL, empty, missing total load-more, unknown status, deep links to run/strategy.
- **Dependencies:** strategy entity links from slices 01–02.
- **Backend blockers:** exact date filter semantics and total availability.
- **Exclusions:** run detail, cancel, decisions/snapshot/timeline.
- **Acceptance criteria:** runs are searchable and link to `/runs/:id` and `/strategies/:id`.

### 07. Run detail overview read-only

- **User outcome:** Operator can inspect a single run status, signal, timings, error, config snapshot summary, and links.
- **Routes:** `/runs/:id`.
- **REST queries:** `GET /runs/{id}`.
- **Mutations:** None.
- **WebSocket events:** `agent_decision`, `debate_round`, `signal`, `error`, `pipeline_health` mark detail stale/refetch.
- **Types/schemas:** run detail schema; phase timings raw record.
- **Shared components:** `PageHeader`, `Breadcrumbs`, `KeyValueSummary`, `StatusBadge`, `JsonViewer`, `LastUpdated`.
- **Mock scenarios:** running/completed/failed/cancelled/unknown, missing error, malformed phase timings, 404, 500, 501.
- **States:** skeleton; not found; failed run error panel; stale running state.
- **Accessibility:** heading/landmarks, key-value semantics, error announced.
- **Safety:** cancel excluded; no operational controls.
- **Tests:** load by id, failed run error, unknown status, stale WS event indicator.
- **Dependencies:** slice 06.
- **Backend blockers:** confirm `GET /runs/{id}` response includes all fields from list.
- **Exclusions:** decisions, snapshot, timeline, cancel.
- **Acceptance criteria:** run status and evidence links are understandable without opening raw JSON.

### 08. Run decisions read-only

- **User outcome:** Researcher can inspect decisions/debate/signal evidence for a run.
- **Routes:** `/runs/:id?tab=decisions`.
- **REST queries:** `GET /runs/{id}/decisions`.
- **Mutations:** None.
- **WebSocket events:** `agent_decision`, `debate_round`, `signal` append stale marker/refetch.
- **Types/schemas:** decision/debate DTOs; raw JSON fallback for unknown payloads.
- **Shared components:** `Timeline`, `JsonViewer`, `StatusBadge`, `EmptyState`, `PartialError`.
- **Mock scenarios:** no decisions, multiple agents, malformed payload, unknown decision type, 500, 501.
- **States:** skeleton; empty “no decisions recorded”; partial malformed item; stale after WS event.
- **Accessibility:** timeline ordered list, timestamps, expandable details keyboard accessible.
- **Safety:** read-only; decisions are evidence, not permission source.
- **Tests:** empty, list render, unsafe HTML escaped, 501, unknown event/decision type.
- **Dependencies:** slice 07.
- **Backend blockers:** decision DTO shape and ordering.
- **Exclusions:** edit/annotation/export.
- **Acceptance criteria:** user can review decision chronology with raw fallback.

### 09. Run snapshot read-only

- **User outcome:** Researcher can inspect captured market/config/state snapshot for a run.
- **Routes:** `/runs/:id?tab=snapshot`.
- **REST queries:** `GET /runs/{id}/snapshot`.
- **Mutations:** None.
- **WebSocket events:** none required; running run events mark stale.
- **Types/schemas:** snapshot DTO or raw JSON schema.
- **Shared components:** `JsonViewer`, `KeyValueSummary`, `FeatureUnavailable`, `CopyButton`.
- **Mock scenarios:** snapshot present, not recorded, huge payload, malformed/unknown fields, 500, 501.
- **States:** loading; empty/not recorded; partial JSON; stale warning for running run.
- **Accessibility:** JSON tree keyboard navigation and copy button labels.
- **Safety:** read-only; do not expose secrets from snapshot without redaction review.
- **Tests:** raw payload render, redaction if needed, 501, large payload still usable.
- **Dependencies:** slice 07.
- **Backend blockers:** confirm snapshot may contain secrets/provider data; redaction contract.
- **Exclusions:** diff/editor/export.
- **Acceptance criteria:** snapshot is inspectable without crashing or leaking obvious secret keys.

### 10. Timeline and persisted events

- **User outcome:** Operator can inspect persisted events by strategy/run/entity, not only live WS buffer.
- **Routes:** `/events?strategy_id&run_id&type&from&limit&offset`; embedded tabs in strategy/run detail.
- **REST queries:** `GET /events` with confirmed filters.
- **Mutations:** None.
- **WebSocket events:** all envelope types; live events mark persisted list stale.
- **Types/schemas:** event envelope + persisted event schema with raw `data`.
- **Shared components:** `Timeline`, `FilterBar`, `DateRangeFilter`, `JsonViewer`, `EntityLink`, `LoadMoreControl`.
- **Mock scenarios:** empty, unknown type, unsafe HTML in data, out-of-order timestamps, missing entity IDs, 500, 501.
- **States:** skeleton; empty; partial item parse failure; stale while WS disconnected.
- **Accessibility:** timeline list semantics, time labels, filters, expandable raw details.
- **Safety:** events are evidence only; do not enable mutations solely from event data.
- **Tests:** filters, unknown event type, unsafe payload escaped, load more, stale after WS disconnect.
- **Dependencies:** slices 06–09 helpful.
- **Backend blockers:** confirm `/events` route, filters, ordering, and pagination.
- **Exclusions:** audit log/admin event management.
- **Acceptance criteria:** persisted events provide deep links to strategy/run/order when IDs exist.

### 11. Portfolio summary and positions

- **User outcome:** Operator can inspect exposure/P&L and open/current positions with paper/live clarity.
- **Routes:** `/portfolio?strategy_id&ticker&market_type`.
- **REST queries:** `GET /portfolio/summary`, `GET /portfolio/positions`, `GET /portfolio/positions/open`.
- **Mutations:** None.
- **WebSocket events:** `position_update`, `order_filled` mark/refetch portfolio.
- **Types/schemas:** `PortfolioSummary`, `Position`, `ListResponse<Position>` if wrapped.
- **Shared components:** `MetricCard`, `DataTable`, `StatusBadge`, `PaperLiveBadge`, `FilterBar`, `EntityLink`, `DataChart` if time series confirmed.
- **Mock scenarios:** no positions, open/closed positions, unknown market type, negative P&L, 500, 501, stale WS down.
- **States:** summary skeleton; empty positions; partial summary/positions failure; stale exposure warning.
- **Accessibility:** numeric values with accessible labels and signs; table semantics; color not sole P&L signal.
- **Safety:** read-only; do not mix paper/live totals without labels.
- **Tests:** empty open positions, negative P&L, partial failure, position links to strategy/orders/trades.
- **Dependencies:** strategies/runs links.
- **Backend blockers:** exact positions list envelope and filters.
- **Exclusions:** allocator diagnostics, broker reconciliation/actions.
- **Acceptance criteria:** operator can identify exposure and whether data is stale/partial.

### 12. Allocator diagnostics read-only

- **User outcome:** Operator can inspect allocator health/decisions without changing allocations.
- **Routes:** `/portfolio/allocator` or `/portfolio?tab=allocator`.
- **REST queries:** `/portfolio/allocator/*` confirmed diagnostic endpoints.
- **Mutations:** None.
- **WebSocket events:** `position_update`, `pipeline_health` mark stale.
- **Types/schemas:** allocator diagnostics DTOs; raw JSON fallback.
- **Shared components:** `KeyValueSummary`, `DataTable`, `JsonViewer`, `MetricCard`, `FeatureUnavailable`.
- **Mock scenarios:** configured/unconfigured, no diagnostics, unknown payload, 500, 501/503.
- **States:** loading; not configured; empty diagnostics; partial panel failures.
- **Accessibility:** diagnostics tables labelled; raw JSON accessible.
- **Safety:** read-only; do not infer permission to rebalance.
- **Tests:** unconfigured, unknown payload, partial failures.
- **Dependencies:** slice 11.
- **Backend blockers:** endpoint inventory and response contracts for allocator routes.
- **Exclusions:** allocation mutations/rebalance.
- **Acceptance criteria:** diagnostics render safely even when backend is absent.

### 13. Orders list read-only

- **User outcome:** Operator can browse recent orders and link to strategy/run/position evidence.
- **Routes:** `/orders?strategy_id&run_id&ticker&status&limit&offset`.
- **REST queries:** `GET /orders`.
- **Mutations:** None.
- **WebSocket events:** `order_submitted`, `order_filled` mark/refetch orders.
- **Types/schemas:** `Order`, `ListResponse<Order>`, unknown order status handling.
- **Shared components:** `DataTable`, `FilterBar`, `StatusBadge`, `EntityLink`, `PaginationControls`.
- **Mock scenarios:** empty, submitted/filled/cancelled/unknown, missing broker/external ID, 500, 501.
- **States:** skeleton; empty by filter; stale after WS disconnect; feature unavailable.
- **Accessibility:** table headers, numeric quantity/price labels, row links.
- **Safety:** read-only; no cancel/replace.
- **Tests:** filter URL, unknown status, order links, 501.
- **Dependencies:** runs/strategies links.
- **Backend blockers:** confirmed filters and pagination/total.
- **Exclusions:** order detail/fills, order mutations.
- **Acceptance criteria:** orders can be inspected and deep-linked.

### 14. Order detail and fills

- **User outcome:** Operator can inspect one order, fills/trades, and linked strategy/run/position.
- **Routes:** `/orders/:id`.
- **REST queries:** `GET /orders/{id}`, `GET /trades?order_id={id}` if supported or local-filtered from trades list.
- **Mutations:** None.
- **WebSocket events:** `order_filled` marks detail/fills stale.
- **Types/schemas:** `Order`, `Trade`, `ListResponse<Trade>`.
- **Shared components:** `PageHeader`, `Breadcrumbs`, `KeyValueSummary`, `DataTable`, `Timeline`, `EntityLink`.
- **Mock scenarios:** order with no fills, partial fills, filled order, unknown fields, 404, 500, 501.
- **States:** skeleton; not found; fills empty; partial order/fills failure.
- **Accessibility:** key-value summary, fills table, status text.
- **Safety:** read-only; no broker action.
- **Tests:** no fills, multiple fills, deep links, 404, 501.
- **Dependencies:** slice 13.
- **Backend blockers:** trade filter by order ID and fill relationship shape.
- **Exclusions:** cancel/replace/order actions.
- **Acceptance criteria:** order lifecycle and fills are explainable from one route.

### 15. Trades list read-only

- **User outcome:** Operator can browse executions/fills and link back to orders/positions.
- **Routes:** `/trades?order_id&position_id&ticker&limit&offset`.
- **REST queries:** `GET /trades`.
- **Mutations:** None.
- **WebSocket events:** `order_filled` mark/refetch trades.
- **Types/schemas:** `Trade`, `ListResponse<Trade>`.
- **Shared components:** `DataTable`, `FilterBar`, `EntityLink`, `PaginationControls`, `StatusBadge` if statuses exist.
- **Mock scenarios:** empty, fills across orders, missing external ID, unknown market/ticker, 500, 501.
- **States:** skeleton; empty; stale; rate limited; feature unavailable.
- **Accessibility:** numeric price/quantity/fee labels, table headers, links.
- **Safety:** read-only.
- **Tests:** filters, empty, deep links, unsafe external IDs escaped.
- **Dependencies:** slices 13–14.
- **Backend blockers:** confirmed trade filters and list envelope.
- **Exclusions:** trade detail route unless backend supports it.
- **Acceptance criteria:** user can trace trades to orders/positions/strategies where IDs exist.

### 16. Risk read-only console

- **User outcome:** Operator can inspect global risk, breakers, kill switches, market switches, and cockpit risk state.
- **Routes:** `/risk`.
- **REST queries:** `GET /risk/status`, `GET /risk/cockpit`, `GET /risk/breakers`.
- **Mutations:** None in this slice.
- **WebSocket events:** `circuit_breaker`, `error`, `pipeline_health` mark/refetch risk.
- **Types/schemas:** risk status, risk cockpit, breakers response, market kill switch map.
- **Shared components:** `RiskBanner`, `StatusBadge`, `MetricCard`, `KeyValueSummary`, `DataTable`, `FeatureUnavailable`.
- **Mock scenarios:** normal, degraded, breaker tripped, kill active, market stopped, unknown risk value, 500, 501/503.
- **States:** skeleton; partial risk panels; stale risk blocks future actions; feature unavailable.
- **Accessibility:** critical status announced via text/role status, no color-only, tables labelled.
- **Safety:** read-only; action controls disabled/placeholder only.
- **Tests:** normal/degraded/kill active, unknown enum, partial failure, stale WS down.
- **Dependencies:** cockpit status conventions.
- **Backend blockers:** exact `/risk/cockpit` and `/risk/breakers` schemas.
- **Exclusions:** kill switch, market stop/resume, breaker reset.
- **Acceptance criteria:** user can classify risk state without performing actions.

### 17. Remaining low-risk strategy actions — paper only

- **User outcome:** Operator can resume, skip next, and manually run paper strategies using the established mutation pattern.
- **Routes:** `/strategies/:id` action bar.
- **REST queries:** `GET /strategies/{id}` pre-submit/refetch; `GET /runs?strategy_id={id}` after run.
- **Mutations:** `POST /strategies/{id}/resume`, `POST /strategies/{id}/skip-next`, `POST /strategies/{id}/run` paper-only.
- **WebSocket events:** `pipeline_start`, `pipeline_health`, `signal`, `error` mark/refetch strategy/runs.
- **Types/schemas:** strategy response; run discovery/polling result if manual run returns no run ID.
- **Shared components:** `ConfirmationDialog`, `InlineAlert`, `Toast`, `StatusBadge`, `EntityLink`.
- **Mock scenarios:** success, conflict, validation, rate limit, network unknown, failed verification, live disabled.
- **States:** action pending/verifying; dialog preserved for correctable errors; unknown completion.
- **Accessibility:** confirmation focus trap; action buttons have distinct names; status messages announced.
- **Safety:** paper-only; C1; no optimism; no retry; refetch before/after; stale blocks action.
- **Tests:** each action success, live blocked, duplicate submit, conflict, network unknown, verification failure.
- **Dependencies:** slices 02, 06, 11-mutation pattern; pause workflow already implemented.
- **Backend blockers:** server does not enforce paper-only/RBAC; manual run response/run ID semantics.
- **Exclusions:** delete, live actions, risk controls.
- **Acceptance criteria:** paper-only actions reuse the pause pattern consistently.

### 18. Global kill switch activation/deactivation

- **User outcome:** Authorized operator can activate/deactivate global kill switch with proper warnings and verification.
- **Routes:** `/risk` action panel.
- **REST queries:** `GET /risk/status`, `GET /risk/cockpit`, `GET /risk/breakers`, `GET /settings`.
- **Mutations:** `POST /risk/killswitch {active, reason?}`.
- **WebSocket events:** `circuit_breaker`, `error`, `pipeline_health`.
- **Types/schemas:** kill switch request/response, risk status.
- **Shared components:** `ConfirmationDialog` with reason/typed token variants, `RiskBanner`, `InlineAlert`, `Toast`.
- **Mock scenarios:** activate success, deactivate disabled for live flag, validation missing reason, 401, 409 if added, 429, 500, failed verification.
- **States:** submitting/verifying, unknown completion, stale risk block, WS degraded warning.
- **Accessibility:** critical dialogs announce mode/environment/risk; typed confirmation labels; focus trap.
- **Safety:** L3; activation C2 reason; deactivation C3/live-gated; no optimism; verify risk status; disable if stale/offline.
- **Tests:** activation, deactivation gated, reason validation, duplicate submit, failed verification, WS down behavior.
- **Dependencies:** slice 16; mutation pattern; live-mutation feature flag.
- **Backend blockers:** no RBAC/step-up; deactivation reason not accepted; live authorization missing.
- **Exclusions:** market stop/resume, breaker reset.
- **Acceptance criteria:** UI never shows kill switch cleared/active until REST verification proves it.

### 19. Per-market stop/resume

- **User outcome:** Operator can stop or resume a market with confirmation and verified risk state.
- **Routes:** `/risk?market_type=...`.
- **REST queries:** `GET /risk/status`, `GET /risk/cockpit`, `GET /risk/breakers`.
- **Mutations:** `POST /risk/market/{type}/stop {reason}`, `POST /risk/market/{type}/resume`.
- **WebSocket events:** `circuit_breaker`, `error`.
- **Types/schemas:** market kill switch map; stop/resume request schemas.
- **Shared components:** `DataTable`, `ConfirmationDialog`, `ReasonDialog`, `StatusBadge`, `RiskBanner`.
- **Mock scenarios:** stop success, resume success, unknown market, validation, 429, 500, failed verification, live resume disabled.
- **States:** per-row pending; stale blocks action; unknown completion; partial market map missing.
- **Accessibility:** row action names include market; reason field labelled; status text.
- **Safety:** stop C2; resume C3/live-gated; no optimism; verify market state; invalidate cockpit/risk/strategies.
- **Tests:** stop/resume verification, duplicate row action, reason required, live flag gating, unknown completion.
- **Dependencies:** slices 16 and 18.
- **Backend blockers:** no RBAC; resume reason not accepted; exact market type enum.
- **Exclusions:** global kill switch, breaker reset.
- **Acceptance criteria:** market controls reflect only verified server state.

### 20. Circuit-breaker reset

- **User outcome:** Admin-intent operator can reset breaker with one-shot admin credential and verification.
- **Routes:** `/risk?tab=breakers`.
- **REST queries:** `GET /risk/breakers`, `GET /risk/status`, `GET /risk/cockpit`.
- **Mutations:** `POST /risk/breaker/reset` with `X-Admin-Key`, body `{scope}`.
- **WebSocket events:** `circuit_breaker`.
- **Types/schemas:** breaker list/reset request/response, admin credential local-only model.
- **Shared components:** `AdministrativeCredentialDialog`, `SecretReplacementField` pattern, `ConfirmationDialog`, `RiskBanner`.
- **Mock scenarios:** reset success, invalid admin key 401, not configured 501/503, validation scope, 429, 500, failed verification.
- **States:** credential entry, submitting/verifying, key cleared on every outcome, unknown completion.
- **Accessibility:** credential field labelled, no autocomplete if appropriate, error announced, focus restored.
- **Safety:** L4/C4; never persist/log/cache admin key; live flag/RBAC required; stale blocks reset; no retry.
- **Tests:** key cleared on success/failure, invalid key inline, duplicate submit, failed verification, 501.
- **Dependencies:** slice 16, admin dialog component.
- **Backend blockers:** static shared admin key, no RBAC, audit visibility gaps.
- **Exclusions:** kill switch/market controls.
- **Acceptance criteria:** reset cannot occur without credential and verified breaker/risk refetch.

### 21. Strategy delete — destructive paper-only first

- **User outcome:** Operator can delete a paper strategy only after strong confirmation and verification.
- **Routes:** `/strategies/:id`.
- **REST queries:** `GET /strategies/{id}`, `GET /runs?strategy_id={id}&status=running` if supported.
- **Mutations:** `DELETE /strategies/{id}` paper-only first.
- **WebSocket events:** strategy/run events mark stale.
- **Types/schemas:** delete response/empty response handling; strategy tombstone local state.
- **Shared components:** `ConfirmationDialog` C3 typed token, `InlineAlert`, `Toast`.
- **Mock scenarios:** success, live blocked, running-run warning/block, 404 already gone, 409, 429, 500, failed list verification.
- **States:** pending/verifying, unknown completion, navigate to list after verified absence.
- **Accessibility:** typed confirmation field labelled; destructive copy explicit.
- **Safety:** L4; paper-only; typed `DELETE`; no optimism; verify detail 404/list absence; invalidate linked queries.
- **Tests:** typed token required, live blocked, duplicate submit, 404 as already gone, failed verification.
- **Dependencies:** slices 01–02, 06.
- **Backend blockers:** backend does not enforce paper-only/RBAC; delete audit unknown; active run precondition unknown.
- **Exclusions:** live delete.
- **Acceptance criteria:** strategy disappears only after verified server absence or authoritative 404.

### 22. Cross-entity deep links and breadcrumbs

- **User outcome:** User can move between strategies, runs, decisions, events, portfolio, orders, trades, and risk with context preserved.
- **Routes:** all P0 read-only routes plus query filters.
- **REST queries:** existing per-route queries; no new endpoint required.
- **Mutations:** None.
- **WebSocket events:** event drawer links into entity routes when IDs exist.
- **Types/schemas:** shared entity reference model `{kind,id,label?}`.
- **Shared components:** `Breadcrumbs`, `EntityLink`, `PageHeader`, `CopyButton`, `ActivityDrawer` improvements.
- **Mock scenarios:** missing entity, stale link, unknown ID kind, event with only run_id/strategy_id.
- **States:** unresolved label fallback to ID, not-found target, stale source badge.
- **Accessibility:** link text meaningful; copy buttons labelled; breadcrumbs ordered nav.
- **Safety:** links do not imply action permission; stale action controls still gated.
- **Tests:** event-to-run/strategy/order links, breadcrumbs, missing labels, keyboard link traversal.
- **Dependencies:** slices 01–16.
- **Backend blockers:** consistent IDs in event/order/trade/position payloads.
- **Exclusions:** new entity screens beyond existing routes.
- **Acceptance criteria:** every visible entity ID has copy/link behavior where a target route exists.

### 23. Shell and cockpit completion pass

- **User outcome:** Cockpit and shell match P0 specs for operator status scanning.
- **Routes:** `/cockpit`, all authenticated shell routes.
- **REST queries:** add missing cockpit queries: `/risk/cockpit`, `/risk/breakers`, `/portfolio/positions/open`, `/orders`, `/trades`, `/health` as applicable.
- **Mutations:** None in this pass; link to risk/action routes.
- **WebSocket events:** `subscribe_all`; activity drawer filtering/deep links.
- **Types/schemas:** cockpit risk/health, positions/orders/trades summaries.
- **Shared components:** `RiskBanner`, `ConnectionStatus`, `EnvironmentBadge`, `PaperLiveBadge`, `ActivityDrawer`, `MetricCard`, `DataTable`.
- **Mock scenarios:** all healthy, partial widget failures, WS down, live broker configured, stale risk, 501 per widget.
- **States:** independent widgets, stale global safety summary, REST fallback while WS down.
- **Accessibility:** skip link, landmarks, status announcements, drawer focus management, responsive nav.
- **Safety:** no new mutations; risk summary must not call stale data safe.
- **Tests:** partial failures, live/paper clarity, drawer links, stale aggregation, mobile-ish render smoke.
- **Dependencies:** read-only entity slices.
- **Backend blockers:** `/health` integration shape, risk cockpit/breakers schemas.
- **Exclusions:** operational actions.
- **Acceptance criteria:** operator can classify safe/degraded/unknown from cockpit alone.

### 24. Final P0 accessibility and end-to-end hardening

- **User outcome:** P0 flows are keyboard-accessible, screen-reader understandable, and covered by end-to-end smoke tests.
- **Routes:** `/login`, `/cockpit`, `/strategies`, `/strategies/:id`, `/runs`, `/runs/:id`, `/portfolio`, `/orders`, `/orders/:id`, `/trades`, `/risk`, `/events`.
- **REST queries:** all P0 mocks exercised via MSW/e2e environment.
- **Mutations:** paper pause/create/edit/resume/skip/run; risk mutations only if backend blockers resolved or mocked as disabled.
- **WebSocket events:** connect, disconnect, reconnect, unknown event, 250+ buffer.
- **Types/schemas:** no new domain types; test fixtures normalized.
- **Shared components:** all shared components receive keyboard/a11y smoke coverage.
- **Mock scenarios:** happy path, expired token refresh, failed refresh, WS down, 501 widgets, unknown enums, unsafe HTML, rate-limit, network unknown completion.
- **States:** all common state vocabulary from `05-p0-screen-specifications.md` verified at least once.
- **Accessibility:** add axe/Playwright if approved; otherwise Testing Library role/name coverage, focus traps, skip links, contrast review.
- **Safety:** mutation tests prove no optimism/no retry/verification; live mutations disabled by default.
- **Tests:** e2e login-to-cockpit, strategies list/detail/pause, runs evidence, portfolio/orders/trades/risk read-only, keyboard navigation, dialog focus, responsive smoke.
- **Dependencies:** all prior slices.
- **Backend blockers:** stable test environment, seeded data or complete MSW fixture server, accessibility tooling dependency approval.
- **Exclusions:** new feature behavior; this is hardening only.
- **Acceptance criteria:** P0 can be demoed through seeded/mocked flows and reviewed for production readiness.

## Backend blockers to track

1. Public `/auth/register` can mint authenticated users unless production-gated.
2. Refresh tokens are JS-readable/stateless; safer HttpOnly/rotating refresh sessions are needed.
3. WebSocket browser auth uses token-bearing query strings; short-lived WS tickets preferred.
4. No RBAC/step-up authorization for operational/live/admin mutations.
5. Strategy pause/resume/run/delete backend does not enforce frontend paper-only gates and lacks atomic `status/is_paper` preconditions.
6. Several DTOs need confirmation or generated schemas: reports, decisions, snapshots, events, orders/trades filters, portfolio positions, allocator diagnostics, risk cockpit/breakers.
7. WebSocket `data` payloads remain untyped.
8. CORS/header config for admin key and future non-GET verbs needs production confirmation.
9. Audit visibility is incomplete for several operational/admin actions.

## Recommended next slice

Start with **01. Strategies list read-only**. It has high operational value, depends only on existing shell/auth/query infrastructure, unlocks safer navigation to the existing strategy detail/pause route, and does not introduce new mutation risk.
