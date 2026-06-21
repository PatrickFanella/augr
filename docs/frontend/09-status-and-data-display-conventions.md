# Status and Data Display Conventions

Source inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/02-user-workflows.md`, `docs/frontend/03-information-architecture.md`, `docs/frontend/05-p0-screen-specifications.md`, `docs/frontend/wireframes/README.md`, `docs/frontend/06-interaction-operational-safety.md`, `docs/frontend/07-design-system-foundations.md`, and `docs/frontend/08-shared-component-inventory.md`.

This document defines semantic status tokens, table conventions, and chart/data-display rules for the Augr trading operations UI. It is a specification only.

## 1. Status token requirements

Every status must communicate through at least two channels:

1. Visible text label.
2. Icon, shape, border style, or layout treatment.
3. Color may reinforce meaning but is never the only cue.

Minimum contrast expectations:

- Status text and icons: WCAG 2.1 AA contrast at 4.5:1 against their background.
- Large/bold status labels: at least 3:1, but prefer 4.5:1.
- Status badge boundaries/icons that convey state: at least 3:1 against adjacent background.
- Focus indicators: at least 3:1 against adjacent colors and visible in high-contrast modes.
- Critical/warning backgrounds must not reduce body text below 4.5:1.

## 2. Semantic status tokens

Implementation may map these tokens to CSS variables, TypeScript objects, or design-token files. The semantic names are stable.

| Status | Token | Meaning | Text label | Icon/shape cue | Color intent | Use |
| --- | --- | --- | --- | --- | --- | --- |
| Healthy | `status.healthy` | Dependency/system is functioning. | `Healthy` | Check circle | Positive green/teal accent | API/automation/broker health. |
| Degraded | `status.degraded` | Usable but impaired. | `Degraded` | Warning diamond | Amber accent | Partial service, WS reconnecting, fallback polling. |
| Offline | `status.offline` | Browser/API dependency unavailable. | `Offline` | Slash circle | Neutral/danger mix | Browser offline, origin unreachable. |
| Stale | `status.stale` | Data exists but is older than threshold or failed refresh. | `Stale` | Clock badge | Amber/neutral | Risk/entity/table freshness. |
| Running | `status.running` | Active in-progress process. | `Running` | Spinner/dot with text; static under reduced motion | Blue accent | Pipeline runs, jobs. |
| Completed | `status.completed` | Process ended successfully. | `Completed` | Check | Positive accent | Runs/backtests/jobs. |
| Failed | `status.failed` | Process/order/service ended with failure. | `Failed` | X/octagon | Danger accent | Runs, orders, jobs. |
| Cancelled | `status.cancelled` | Process intentionally cancelled. | `Cancelled` | Stop square | Neutral/slate | Runs/orders. |
| Active | `status.active` | Enabled and expected to operate. | `Active` | Filled dot/play | Positive or blue accent | Strategies, market active. |
| Paused | `status.paused` | Temporarily stopped by schedule/operator. | `Paused` | Pause icon | Amber/neutral | Strategies/jobs. |
| Inactive | `status.inactive` | Disabled/not operating. | `Inactive` | Hollow circle | Neutral | Strategies/settings. |
| Paper | `status.paper` | Paper/simulated mode. | `Paper` | Document/sandbox icon; dashed outline | Neutral/blue low emphasis | Broker/strategy/run context. |
| Live | `status.live` | Live trading/broker context. | `LIVE` | Solid border + alert triangle | Danger/high emphasis | Broker/strategy/dialogs. |
| Normal | `status.normal` | Risk within configured thresholds. | `Normal` | Check shield | Positive accent | Risk engine status. |
| Warning | `status.warning` | Risk or validation nearing unsafe threshold. | `Warning` | Warning triangle | Amber accent | Risk/cockpit. |
| Breached | `status.breached` | Risk threshold violated or unsafe. | `Breached` | Octagon/alert | Danger accent | Risk/cockpit critical state. |
| Open | `status.open` | Circuit breaker is open/not tripped, or market open when used by backend. | `Open` | Open lock/circle | Neutral/positive depending context | Circuit breaker phase; clarify label in context. |
| Tripped | `status.tripped` | Circuit breaker/protection has tripped. | `Tripped` | Locked/stop icon | Danger accent | Breakers/risk. |
| Cooldown | `status.cooldown` | Protection is waiting before reset/resume. | `Cooldown` | Timer icon | Amber/blue | Breakers/risk. |
| Not configured | `status.notConfigured` | Feature/provider endpoint unavailable by configuration. | `Not configured` | Wrench/plug icon | Neutral/amber | 501/selected 503. |
| Unknown | `status.unknown` | Value is missing, new, ambiguous, or cannot be verified. | `Unknown` or raw value | Question mark diamond | Neutral with strong border | Unknown enum/mode/freshness. |

Additional domain statuses map into these tokens:

- `StrategyStatus`: `active`, `paused`, `inactive`.
- `PipelineStatus`: `running`, `completed`, `failed`, `cancelled`.
- `RiskStatus`: `normal`, `warning`, `breached`.
- `CircuitBreakerPhase`: `open`, `tripped`, `cooldown`.
- `OrderStatus`: `pending/submitted/partial` use running/active-like treatment with text; `filled` maps completed; `cancelled` maps cancelled; `rejected` maps failed.
- `AutomationHealthResponse.healthy=false` maps degraded unless failing critical jobs make parent context breached by policy.

## 3. Paper/live conventions

- `LIVE` must be uppercase, high-emphasis, and present in text wherever live broker/strategy context is known.
- `Paper` can be lower-emphasis but must still be explicit.
- Unknown mode uses `Mode unknown` and high-risk confirmation treats it as live-strength until refreshed state proves otherwise.
- Do not infer paper/live from broker name, ticker, or route. Use confirmed settings/strategy/run fields only.
- Do not aggregate paper and live totals without separate labels.

## 4. Freshness conventions

Freshness is safety-critical.

| Data | Fresh threshold | Stale behavior |
| --- | ---: | --- |
| Risk status/cockpit/breakers for live actions | 15s | Controls blocked until refetch. |
| Risk status/cockpit/breakers for paper actions | 30s | Controls require refetch or policy exception. |
| Strategy/run primary entity for live actions | 30s | Actions blocked until refetch. |
| Strategy/run primary entity for paper actions | 60s | Actions require refetch if mutation. |
| Settings/broker mode before live mutation | 60s | Live mutation blocked. |
| Running run status | 5–10s target | Row/header marked stale. |
| Portfolio/P&L exposure | 15s cockpit/open positions | Exposure not treated as safe if stale. |

Display rules:

- Show `Last updated {relative}` visually and full ISO timestamp in accessible label/tooltip.
- During background refresh, keep old data visible and mark `Refreshing` without layout shift.
- If refresh fails and data ages past threshold, show `Stale` and preserve cached values as cached, not current.
- After WebSocket reconnect, mark visible realtime-dependent data stale until REST refetch completes.

## 5. Table conventions

### 5.1 Default row height and density

- Default P0 table density: compact, 32px row height.
- Comfortable density: 40px for portfolio or mixed metric tables.
- Mobile: table becomes priority-card list rather than shrinking to unreadable columns.
- Header height: 36px compact, 40px comfortable.
- Row actions must fit without hover-only discovery.

### 5.2 Sorting

- Default sorting is frontend-side unless backend sort parameters are documented.
- Sortable headers show text label, sort direction icon, and `aria-sort`.
- Default P0 sorts:
  - Strategies: active first, then updated newest.
  - Runs: newest `started_at` first; running lists prioritize running.
  - Decisions: chronological by decision order/time.
  - Portfolio positions: open first, then newest `opened_at`.
  - Orders: newest `submitted_at || created_at` first.
  - Order fills: executed time ascending for chronology.
  - Trades: newest `executed_at` first.
  - Breakers: active/tripped first, then newest `tripped_at`.
  - Activity drawer: newest event first.
- Unknown or missing sort values sort last unless that would hide unsafe state; tripped/breached/failed states may be pinned by domain sort.

### 5.3 Filtering

- Persist supported filters in the URL for shareable investigation links.
- Use backend-supported query parameters when confirmed; mark frontend-only filters as local.
- Filter validation appears near the `FilterBar`, not as a full-page error.
- `Clear filters` is visible when active filters produce an empty result.
- Date filters show timezone and accepted inclusivity if known; if not known, label as frontend assumption.

### 5.4 Selection

- Row click/Enter opens the primary detail route.
- Selection checkboxes are only for true bulk actions; do not add bulk high-risk actions in P0 unless approved.
- Selected row state uses background + left border + `aria-selected`, not color alone.
- Row action buttons are separate from row navigation to prevent accidental mutation.

### 5.5 Sticky columns

- First identity column may be sticky for wide tables.
- Last action column may be sticky if row actions are frequent.
- Sticky columns require surface/background and border/shadow separation.
- Avoid more than two sticky column groups; if more are needed, use column picker or detail panel.

### 5.6 Horizontal overflow

- Desktop/tablet dense tables may overflow horizontally inside the table container, not the whole page.
- Show an overflow affordance when columns extend beyond viewport.
- Important columns remain visible by default; low-priority columns can be hidden or moved to row detail.
- Mobile should use cards/lists with priority fields and an explicit detail action.

### 5.7 Column visibility

- Provide `TableColumnPicker` for wide operational tables.
- Required columns cannot be hidden: primary identity, status, time, and actions where applicable.
- Persist user choices locally per table; URL should not become noisy unless sharing exact table layout becomes a requirement.
- Reset to default must be one action.

### 5.8 Timestamp display

- Display relative time as secondary text only; canonical value remains absolute.
- Tooltips/accessible labels include full ISO timestamp and timezone.
- Use consistent timezone: browser local for operator-facing display unless product chooses exchange/UTC; raw ISO available in details.
- For trading dates, distinguish date-only `trade_date` from execution timestamps.

### 5.9 ID display and copying

- Show shortened UUIDs with middle truncation in dense cells, e.g. `8f31…a92c`.
- Full ID available via tooltip/accessibility and `CopyButton`.
- Entity links include entity type in accessible label: `Open run 8f31…a92c`.
- Broker external IDs display separately from internal IDs.

### 5.10 Empty values

- Absent optional value: `—`.
- Zero numeric value: `0`, not `—`.
- Empty string from backend: treat as absent unless field semantics say otherwise.
- Empty array/list from a fresh response: use `EmptyState` with positive or filtered copy as appropriate.

### 5.11 Unknown enum values

- Render raw value exactly as received in a neutral/unknown `StatusBadge`.
- Preserve raw values in forms and table exports/copy.
- Do not coerce unknown values to safe/normal/active.
- Hide or disable actions whose eligibility depends on a known enum.

### 5.12 Pagination when total is missing

- API list responses may omit `total`; use `LoadMoreControl` rather than numbered page counts.
- Default `limit=50`; maximum `limit=100` where backend uses common pagination.
- If `total` exists, numbered/prev-next pagination is allowed.
- Preserve `limit`/`offset` in URL for shareable pages when applicable.
- On filter changes, reset `offset=0`.

### 5.13 Loading and refresh behavior

- Initial load: table skeleton with headers/filter bar visible where possible.
- Background refresh: preserve rows, show table-level refresh indicator and per-row update markers if useful.
- Error with cached rows: keep rows visible with `PartialError`/stale banner.
- Rate limited: keep cached rows, reduce polling, and show retry guidance.
- Offline: cached read-only table; all mutations disabled.

### 5.14 Keyboard navigation

- Tab reaches filter controls, table headers/actions, and row action menus.
- Enter on focused row opens detail if row navigation is enabled.
- Space toggles selection only when a selection checkbox/focus target is active.
- Arrow-key cell navigation is required only if the chosen table library implements ARIA grid correctly; otherwise keep native table semantics and normal tab order for interactive controls.
- Escape closes row action menus, column picker, and detail panel.
- Sort/filter controls are reachable and announce state changes.

## 6. Chart conventions

Charts are supporting evidence, not decoration. They must not obscure exact data tables/values.

### 6.1 Number formatting

- Use the same numeric formatters as tables for currency, percent, exposure, quantity, and confidence.
- Axis ticks may abbreviate (`$1.2k`, `23%`) but tooltips/details show full precision appropriate to domain.
- P&L sign is explicit with `+`, `-`, or `0`.
- If values are normalized/indexed, label the basis clearly.

### 6.2 Time zones

- Chart axes show timezone in axis label, subtitle, or tooltip.
- Use consistent timezone per chart; do not mix local and UTC without visible labels.
- For market charts, final product must decide whether operator local, exchange local, or UTC is canonical.
- Tooltips include full timestamp with timezone.

### 6.3 Tooltips

- Tooltips are supplemental; data must be understandable from chart title/legend/summary and accessible table/summary.
- Tooltip content includes series name, timestamp, value, units, and mode (`Paper`/`LIVE`) where relevant.
- Tooltip hover must have keyboard/focus equivalent if chart points are interactive.
- Do not put secrets, raw prompts, or admin credentials in tooltips.

### 6.4 Missing data

- Gaps remain gaps; do not connect lines across missing intervals unless explicitly labelled as interpolated.
- Missing points have an accessible summary: `No data from 14:10 to 14:20`.
- Empty chart from a fresh valid response uses `EmptyState`, not a blank axis.
- Provider-not-configured uses `FeatureUnavailable`.

### 6.5 Stale data

- Stale chart data shows a visible stale badge and last-updated timestamp.
- Do not animate or extend a stale series to the current time.
- If realtime is disconnected, mark the gap and require REST refetch before claiming current state.

### 6.6 Accessible summaries

Each chart has:

- Clear title and question answered.
- Text summary of latest value, trend, and any warnings.
- Accessible data table or export/copy path for exact values.
- Legend visible and text-labelled.
- Color palette that works for color-vision deficiencies and does not rely on red/green alone.

### 6.7 Avoid misleading axes

- Use zero baseline for bar charts unless a documented analytical reason exists.
- Line charts may use non-zero y-axis only when labelled and not used to exaggerate small moves.
- Do not use dual axes unless units and scales are unambiguous and the chart has a clear textual warning.
- Show sample size/window and aggregation interval.
- Distinguish realized values from forecasts, simulations, and backtests.

### 6.8 Distinguishing paper and live series

- Live series: solid stroke, stronger border/legend label `LIVE`.
- Paper series: dashed stroke or lower-emphasis pattern, legend label `Paper`.
- Unknown mode: dotted/neutral series, legend label `Mode unknown`; do not combine with live or paper totals.
- Tooltips include mode for each series.
- If chart combines paper and live contexts, title/subtitle must state that explicitly.

## 7. First vertical-slice data-display requirements

For the first vertical slice, implement/specify these conventions before feature screen buildout:

1. Status token map and `StatusBadge` behavior for all known P0 enums.
2. Freshness model and `LastUpdated`/`StaleIndicator` behavior.
3. Compact `DataTable` defaults with URL-backed filters and load-more behavior for missing totals.
4. ID truncation/copying and unknown enum rendering.
5. Paper/live badge treatment for shell, strategy rows, and confirmation dialogs.
6. `DataChart` accessibility summary, stale-data overlay, and paper/live series treatment before any charted cockpit, portfolio, market, or backtest evidence ships.

## 8. Decisions deferred to final component/table/chart libraries

- Whether the table implementation can provide accessible ARIA-grid keyboard navigation or should remain native-table plus tabbable controls.
- Column pinning, resizing, virtualization, row expansion, and mobile card rendering strategy.
- Date/time picker behavior and timezone handling details.
- Chart library accessibility capabilities, tooltip keyboard support, and data-table fallback pattern.
- Numeric formatter implementation and locale policy.
