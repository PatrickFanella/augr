# Frontend Entity Navigation Model

Source inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/api-implementation-review.md`, and reviewed Go API routes.

Labels:

- **Confirmed**: route or relationship implied by documented IDs/endpoints.
- **Assumption**: frontend navigation/context behavior; does not add backend fields.
- **Open question**: backend/product decision needed.

## 1. Core relationship map

```text
Strategy
  ├─ Pipeline runs
  │   ├─ Agent decisions
  │   │   ├─ Replay workbench (optional/configured)
  │   │   └─ Conversation context (optional/configured)
  │   ├─ Snapshot data by data_type
  │   ├─ Runtime events
  │   └─ Orders
  │       └─ Fills / Trades
  │           └─ Position impact
  ├─ Reports
  ├─ Events
  ├─ Backtest configs/runs/divergence
  └─ Portfolio exposure where strategy_id exists

Signal / Discovery / Research opportunity
  ├─ Strategy when strategy_id exists
  ├─ Market/ticker/slug/token/outcome context
  └─ Backtest or strategy creation/editing as a user decision, not direct backend relation

Audit event
  ├─ Entity route when entity_type/entity_id can be mapped
  └─ Runtime events for adjacent operational timeline
```

## 2. Entity routes and primary endpoints

| Entity | Primary route | Primary endpoint(s) | Confirmed links | Notes |
| --- | --- | --- | --- | --- |
| Strategy | `/strategies/:id` | `/strategies/{id}`, `/runs?strategy_id=`, `/events?strategy_id=`, strategy reports | Runs, events, reports; orders/trades/positions when returned rows include strategy ID | Strategy object schema is domain-derived and needs extraction. |
| Pipeline run | `/runs/:id` | `/runs/{id}`, `/runs/{id}/decisions`, `/runs/{id}/snapshot`, `/events?pipeline_run_id=` | Strategy via `strategy_id`; decisions; snapshot; events; orders when rows include `pipeline_run_id` | Run detail is the central investigation route. |
| Decision | `/runs/:runId?tab=decisions&decision=:id` or `/journal/decisions/:id` | `/runs/{id}/decisions`, `/journal/decisions/{id}`, `/replay/decisions/{id}` | Run context, replay, conversation by run/agent | **Assumption**: run decision detail uses query state until a first-class route is needed. |
| Snapshot | `/runs/:id?tab=snapshot&data_type=:key` | `/runs/{id}/snapshot` | Run and decisions | Snapshot payload is raw JSON keyed by `data_type`; do not invent fields. |
| Runtime event | `/events?...` | `/events` | Strategy/run/agent filters | WebSocket events share vocabulary but persisted event shape is separate/ambiguous. |
| Order | `/orders/:id` | `/orders/{id}`, `/trades?order_id=` | Strategy/run if IDs present; fills/trades | Order detail returns `{order,fills}` where fills are trades. |
| Fill | Within `/orders/:id` | `/orders/{id}` | Trade/order | No separate fill route confirmed; treat as trade rows from order detail. |
| Trade | `/trades?...` | `/trades` | Order via `order_id`, position via `position_id` | Cannot combine `order_id` and `position_id` query filters. |
| Position | `/portfolio?position_id=:id` or `/portfolio` row focus | `/portfolio/positions`, `/portfolio/positions/open`, `/trades?position_id=` | Trades, strategy if ID present | No single position detail endpoint is confirmed; use row focus/query state. |
| Report | `/strategies/:id?tab=reports&report_type=...` | `/strategies/{id}/reports/latest`, `/strategies/{id}/reports` | Strategy | Report artifact shape is not documented. |
| Signal | `/signals?...` | `/signals/evaluated`, `/signals/triggers`, `/signals/watchlist`, Polymarket recent signals | Strategy/market if IDs/terms appear in payload | Signal payload schemas and error codes are inconsistent. |
| Backtest config | `/backtests/configs/:id` | `/backtests/configs/{id}` | Strategy via `strategy_id` when present | Full-object create/update. |
| Backtest run | `/backtests/runs/:id` | `/backtests/runs/{id}` | Backtest config via `backtest_config_id` when present | No WebSocket events confirmed. |
| Conversation | `/conversations/:id` | `/conversations`, `/conversations/{id}/messages` | Run via `pipeline_run_id`, agent role | Messages may include synthetic decision messages on first page. |
| Memory | `/memory?...` | `/memories`, `/memories/search`, `/memories/{id}` | Agent role/search terms | No direct route to a single memory endpoint confirmed; use focused row. |
| Audit event | `/audit-log?...` | `/audit-log` | Entity by `entity_type`/`entity_id` when mappable | Exact entity vocabulary unconfirmed. |

## 3. Deep-link conventions

### Stable route parameters

- Use UUID path params only where backend has a detail endpoint: strategies, runs, orders, backtest configs, backtest runs, conversations, journal decisions.
- Use query-state row focus where no detail endpoint exists: positions, fills, memories, reports, events, audit rows, snapshot `data_type` keys.
- Use URL-safe slugs/addresses for Polymarket routes because backend routes accept `slug` and `address` path params.

### Query-state rules

- Preserve filters exactly when navigating from lists to details using `from=<encoded-route>` or browser history state.
- Keep entity-specific filters in the URL so incident links are shareable: `strategy_id`, `pipeline_run_id`, `order_id`, `position_id`, `ticker`, `event_type`, `entity_id`, `after`, `before`.
- Do not put secret values, bearer tokens, refresh tokens, API keys, prompt text, provider keys, or `X-Admin-Key` in the URL.
- `include_prompt=true` is allowed only as explicit user-controlled query state because prompt text can be sensitive. **Assumption**.

### Breadcrumb model

- Prefer source-context breadcrumbs when present: `Cockpit / Alert / Run :id`, `Strategy / :id / Run :id`, `Audit log / Entity :id`.
- Fall back to canonical hierarchy when opened directly: `Operations / Runs / :id`.
- Use loaded entity display names only after data loads; before that use ID/slug.
- Breadcrumb links preserve parent filters where possible.

## 4. Entity-specific navigation behavior

### Strategy

- **Inbound links**: cockpit active runs, portfolio positions, orders/trades, backtests, events, audit rows.
- **Outbound links**: runs filtered by `strategy_id`, events filtered by `strategy_id`, reports, orders/trades when row IDs support it, backtests filtered by `strategy_id`.
- **Context preservation**: if opened from an incident, keep `from=/cockpit?...`; after action/refetch, return to incident context.
- **Open questions**: strict strategy schema; whether interventions should record a user reason.

### Pipeline run

- **Inbound links**: cockpit, strategy detail, events, order rows, audit/entity rows.
- **Outbound links**: strategy detail, decisions, snapshot, persisted events, related orders/trades.
- **Context preservation**: run detail tabs use `tab=timeline|decisions|snapshot|orders|events`; selected decision uses `decision=`.
- **Open questions**: no confirmed order query by `pipeline_run_id`; relation may rely on returned row fields and client filtering.

### Decision

- **Inbound links**: run decisions tab, decision journal, replay, conversations.
- **Outbound links**: snapshot tab for the same run, replay workbench, journal detail, events around timestamp if event fields support it.
- **Context preservation**: keep `include_prompt` opt-in state and return to same run tab.
- **Open questions**: decision ID field names in run decision payload; replay result shape.

### Snapshot

- **Inbound links**: run detail and decision detail.
- **Outbound links**: none guaranteed beyond run/decision context.
- **Context preservation**: use `data_type` query key and raw JSON viewer expansion state in local UI state.
- **Open questions**: snapshot data type vocabulary and payload-specific schemas.

### Runtime event

- **Inbound links**: cockpit activity drawer, run/strategy timelines, audit review.
- **Outbound links**: strategy/run when IDs are present; JSON payload viewer always available.
- **Context preservation**: filters remain in URL; selected event row can use `event_id` only if the returned payload has a stable ID. **Do not assume one until DTO is confirmed.**
- **Open questions**: persisted `AgentEvent` schema and event `data` shapes.

### Order, fill, trade, position

- **Inbound links**: cockpit execution feed, run detail, strategy detail, portfolio.
- **Outbound links**:
  - Order → fills/trades via `/orders/{id}` and `/trades?order_id=`.
  - Trade → order via `order_id` and position trades via `position_id` when present.
  - Position → `/trades?position_id=` and strategy when `strategy_id` exists.
- **Context preservation**: keep execution-chain context with `from=run|strategy|portfolio|cockpit`.
- **Open questions**: no single position endpoint; no order cancel endpoint; exact fill/trade distinction is domain-derived.

### Report

- **Inbound links**: strategy detail and audit/events when artifact/entity IDs are present.
- **Outbound links**: strategy and report history.
- **Context preservation**: use `report_type`, `status`, and selected row query state.
- **Open questions**: report artifact schema; total absent in list response.

### Signal and research opportunity

- **Inbound links**: research workbench, signals page, Polymarket workspace, cockpit top signals if implemented.
- **Outbound links**: strategy when `strategy_id` appears; market/ticker/slug route when fields are present; backtest/strategy creation as user-initiated follow-up.
- **Context preservation**: keep scanner filters in URL and raw payload in row detail.
- **Open questions**: DTOs for evaluated signals, triggers, watch terms, opportunities.

### Backtest

- **Inbound links**: strategy detail, research/backtests nav.
- **Outbound links**: strategy, configs, runs, divergence.
- **Context preservation**: backtests list uses `tab=configs|runs|divergence`; selected strategy retained as `strategy_id`.
- **Open questions**: comparison routes not registered; run progress model unknown.

### Conversation and memory

- **Inbound links**: run decision detail, research nav.
- **Outbound links**: run via `pipeline_run_id`, agent role filters, memory search.
- **Context preservation**: conversation list preserves `pipeline_run_id` and `agent_role`; message offset preserved.
- **Open questions**: message DTO, synthetic decision-message shape, memory schema.

### Audit event

- **Inbound links**: post-action toast, administration nav, incident review.
- **Outbound links**: mapped entity route if `entity_type`/`entity_id` vocabulary is known; events with matching IDs/time ranges.
- **Context preservation**: keep audit filters and selected row; entity route receives `from=/audit-log?...`.
- **Open questions**: canonical entity type names and audit payload schema.

## 5. Preserving investigation context

### Incident path example

```text
/cockpit?alert=local-123
  → /runs/:runId?from=/cockpit%3Falert%3Dlocal-123&tab=timeline
  → /runs/:runId?tab=decisions&decision=:decisionId
  → /orders/:orderId?from=/runs/:runId%3Ftab%3Ddecisions
  → /audit-log?entity_id=:orderId&from=/orders/:orderId
```

Rules:

1. Preserve the original incident route through `from` or browser history state.
2. Every detail route has a canonical URL that works without the original context.
3. Avoid adding frontend-only IDs to backend requests.
4. Refetch canonical entity data after WebSocket reconnect before showing action controls.

### Cache and realtime context

- WebSocket event envelope supplies `strategy_id` and/or `run_id`; use those only for routing/cache invalidation.
- Because `data` is untyped, do not derive high-risk mutation eligibility solely from WebSocket payloads.
- After reconnect, refetch visible detail queries and list queries related to active subscriptions.
- Slow-consumer drops must appear as realtime degraded and trigger REST resync.

## 6. Questions before screen specs

1. Provide exact DTO/schema extraction for all domain structs returned directly.
2. Confirm event-specific WebSocket `data` payloads and cache invalidation semantics.
3. Confirm whether order/run/strategy relations should have dedicated filters beyond current endpoints.
4. Confirm entity type vocabulary in audit log.
5. Confirm whether positions, events, reports, memories, and signals need first-class detail endpoints or row-focus views are acceptable for v1.

## 7. Handoff summary before screen specs

### Files changed in this documentation pass

- `docs/frontend/02-user-workflows.md`: user modes/responsibilities and critical workflow specifications.
- `docs/frontend/03-information-architecture.md`: Operations/Markets/Research/Administration route map and access rules.
- `docs/frontend/04-entity-navigation-model.md`: entity relationship/deep-link model and context-preservation rules.

### Major UX decisions

1. Make `/cockpit` the authenticated landing page and persistent safety surface.
2. Treat stale risk/realtime data as degraded and never implicitly safe.
3. Keep high-risk actions guarded by confirmation and fresh refetch; never auto-retry mutations.
4. Use row-focus/query-state for entities without confirmed detail endpoints instead of inventing API routes.
5. Keep guest observer mode and public registration hidden until product explicitly enables them.
6. Use raw JSON inspectors for ambiguous payload areas until backend schemas are generated or confirmed.

### Blockers

1. Stable DTOs/enums are missing for many domain structs and provider/service payloads.
2. WebSocket event-specific `data` payloads are untyped.
3. Admin authorization is only confirmed as one-shot `X-Admin-Key` for breaker reset.
4. Browser deployment CORS may block `PATCH`, `X-API-Key`, or `X-Admin-Key` unless configured.
5. Several features can return `501`/`503` or ambiguous empty success when backing services are absent.

### Questions to resolve before visual screen specs

1. Should registration and guest market observer mode be exposed in production?
2. What role/permission model should replace or supplement the current authenticated-only API surface?
3. Which row-focused entities need first-class frontend pages or backend detail endpoints?
4. What are the canonical severity rules for cockpit degraded/unsafe status aggregation?
5. Should discovery/backtests/automation actions expose async progress/idempotency guarantees?
