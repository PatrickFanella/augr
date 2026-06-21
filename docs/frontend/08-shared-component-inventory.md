# Shared Component Inventory

Source inputs: `docs/frontend-ui-api-brief.md`, `docs/frontend/02-user-workflows.md`, `docs/frontend/03-information-architecture.md`, `docs/frontend/05-p0-screen-specifications.md`, `docs/frontend/wireframes/README.md`, `docs/frontend/06-interaction-operational-safety.md`, and `docs/frontend/07-design-system-foundations.md`.

This document defines shared frontend components for the Augr trading operations UI. It is a specification only and does not implement components or choose libraries.

## 1. Component design rules

- Components must accept semantic status/mode/freshness inputs rather than inferring safety from arbitrary strings.
- Components must expose text labels for all icons and status colors.
- Components that render server data must support loading, empty, partial error, stale, offline, unknown enum, and not-configured states.
- Mutating components must delegate confirmation and safety checks to the global safety-policy dialog host; they do not perform high-risk mutations directly.
- Component APIs should preserve raw values for unknown enums and payloads.

```ts
type StatusTone =
  | 'healthy' | 'degraded' | 'offline' | 'stale'
  | 'running' | 'completed' | 'failed' | 'cancelled'
  | 'active' | 'paused' | 'inactive'
  | 'paper' | 'live'
  | 'normal' | 'warning' | 'breached'
  | 'open' | 'tripped' | 'cooldown'
  | 'notConfigured' | 'unknown';

type Freshness = {
  fetchedAt?: string;
  staleAt?: string;
  isStale?: boolean;
  source?: 'rest' | 'websocket' | 'cache' | 'manual';
};

type ComponentState = 'idle' | 'loading' | 'refreshing' | 'disabled' | 'submitting' | 'verifying' | 'error';
```

## 2. Inventory

### 2.1 AppShell

**Responsibility**: Persistent authenticated chrome for navigation, environment/schema/user context, WebSocket status, risk status, broker paper/live state, global activity drawer, toast stack, and dialog host.

**Variants**: desktop sidebar, tablet rail, mobile header + bottom/slide navigation; degraded/offline shell.

**Inputs**:

```ts
interface AppShellProps {
  user?: UserSummary;
  environment?: string;
  schemaStatus?: StatusTone | string;
  riskStatus?: StatusTone | string;
  brokers?: BrokerModeSummary[];
  connection: ConnectionStatusModel;
  navigation: NavigationGroupModel[];
  freshness?: Freshness;
  children: ReactNode;
}
```

**Outputs/callbacks**: `onNavigate`, `onLogout`, `onRetryShellStatus`, `onOpenActivity`, `onOpenRisk`.

**Loading/disabled**: render shell skeleton until auth/me resolves; child outlet remains visible during partial shell failures; disable logout only while local session clear is in progress.

**Accessibility**: landmarks, skip-to-content, visible labels for nav/status, throttled live region for critical status changes, focus restoration for drawer/dialog.

**Responsive**: persistent side nav on desktop; rail on tablet; compact sticky status header and bottom/slide nav on mobile.

**Use**: all authenticated routes. **Do not use** on `/login` except public system title/environment hint.

### 2.2 NavigationGroup

**Responsibility**: Group IA sections: Operations, Markets, Research, Administration.

**Variants**: expanded, collapsed rail, mobile list, disabled/unavailable group.

**Inputs**: `label`, `items`, `activePath`, `collapsed`, optional `badge` for alerts/degraded counts.

**Outputs**: `onSelect(path)`.

**Loading/disabled**: skeleton labels during route bootstrap; disabled item includes reason.

**Accessibility**: nav list semantics, `aria-current`, keyboard roving or normal tab order, text label even when icon-only via tooltip/aria-label.

**Responsive**: icons + short labels on narrow; full labels desktop.

**Use**: AppShell nav. **Do not use** for tabs inside pages.

### 2.3 PageHeader

**Responsibility**: Page/entity title, subtitle/context, breadcrumbs, primary actions, status/mode/freshness summary.

**Variants**: list page, entity detail, form page, critical operations page.

**Inputs**: `title`, `subtitle`, `breadcrumbs`, `badges`, `actions`, `freshness`, `backLink`.

**Outputs**: action callbacks and `onBack`.

**Loading/disabled**: title skeleton; actions disabled during stale/offline/precheck states with reason.

**Accessibility**: one `h1` per page, action labels explicit, status badges text-visible.

**Responsive**: action overflow menu on mobile; keep critical status badges visible.

**Use**: all P0 pages. **Do not use** inside cards/panels.

### 2.4 Breadcrumbs

**Responsibility**: Show route/entity ancestry and preserve investigation context.

**Variants**: route breadcrumbs, entity breadcrumbs, compact mobile single-back.

**Inputs**: `{label, href, current?, loading?}[]`.

**Outputs**: navigation selection.

**Loading/disabled**: skeleton entity label until loaded; current item non-link.

**Accessibility**: `nav aria-label="Breadcrumb"`, ordered list, current page marked.

**Responsive**: collapse middle items on narrow screens.

**Use**: entity/detail/form pages. **Do not use** for local tab state.

### 2.5 EnvironmentBadge

**Responsibility**: Display environment and schema context.

**Variants**: environment only, schema included, stale/degraded schema.

**Inputs**: `environment`, `schemaStatus`, `currentSchemaVersion`, `requiredSchemaVersion`, `freshness`.

**Outputs**: `onOpenDetails`, `onCopy`.

**Loading/disabled**: skeleton while settings/health loads; unknown if unavailable.

**Accessibility**: full text label, tooltip contains version details, copy button labelled.

**Responsive**: abbreviate value visually (`STG`) but expose full accessible text.

**Use**: AppShell, dialogs. **Do not use** as a generic status badge.

### 2.6 PaperLiveBadge

**Responsibility**: Explicitly distinguish paper/live strategy or broker mode.

**Variants**: paper, live, unknown; broker, strategy, run-inferred.

**Inputs**: `mode: 'paper' | 'live' | 'unknown'`, `source`, `label`, `freshness`.

**Outputs**: optional `onOpenSource`.

**Loading/disabled**: unknown mode when source data not loaded; high-risk actions consume unknown as live-strength.

**Accessibility**: text `PAPER`, `LIVE`, or `MODE UNKNOWN`; icon/shape differs by mode.

**Responsive**: keep visible in shell/dialog headers and entity headers.

**Use**: broker/strategy/run/order contexts. **Do not use** when paper/live is inferred only from broker string.

### 2.7 ConnectionStatus

**Responsibility**: Display WebSocket/API/offline connection state and reconnect attempts.

**Variants**: connected, connecting, reconnecting, degraded, disconnected, offline.

**Inputs**: `state`, `attempts`, `lastConnectedAt`, `lastEventAt`, `details`.

**Outputs**: `onReconnect`, `onOpenDiagnostics`.

**Loading/disabled**: connecting shown during bootstrap; reconnect disabled while offline.

**Accessibility**: status text visible; announce connected→degraded/offline transitions.

**Responsive**: compact icon + label in header; full details in popover/drawer.

**Use**: AppShell and ActivityDrawer. **Do not use** as authoritative action-permission indicator.

### 2.8 RiskBanner

**Responsibility**: Prominent risk/safety summary for cockpit and risk console.

**Variants**: normal, warning, breached, kill-switch active, market stopped, stale/unknown, offline.

**Inputs**: `riskStatus`, `killSwitch`, `marketStops`, `breakers`, `freshness`, `actions`.

**Outputs**: `onOpenRisk`, `onInvokeControl(action)` after safety-policy precheck.

**Loading/disabled**: skeleton until minimum risk status loads; controls disabled when stale/offline unless safety policy allows emergency path.

**Accessibility**: `role="status"` for non-critical changes, `role="alert"` for breached/protection state changes; text not color-only.

**Responsive**: sticky/near-top on cockpit and risk console; compact row on mobile but never hidden.

**Use**: `/cockpit`, `/risk`, high-risk dialogs. **Do not use** for routine row status.

### 2.9 StatusBadge

**Responsibility**: Render semantic statuses and unknown raw enum values.

**Variants**: solid, subtle, outline, compact, with icon, with raw value.

**Inputs**: `status`, `label`, `tone`, `icon`, `description`, `rawValue`.

**Outputs**: optional `onClick` for filter shortcut.

**Loading/disabled**: skeleton pill; disabled filter pill keeps readable text.

**Accessibility**: visible text; icon has decorative or labelled semantics; contrast meets status-token requirements.

**Responsive**: compact labels in tables, full text in cards/dialogs.

**Use**: tables, headers, cards. **Do not use** for actions that should be buttons.

### 2.10 DataTable

**Responsibility**: Dense operational table with sorting, filtering hooks, selection, column visibility, sticky columns, loading/empty/error states, and keyboard navigation.

**Variants**: compact, comfortable, card-on-mobile, selectable, expandable rows, virtualized if library supports.

**Inputs**:

```ts
interface DataTableProps<T> {
  rows: T[];
  columns: TableColumn<T>[];
  rowId: (row: T) => string;
  density?: Density;
  sort?: SortState;
  filters?: Record<string, unknown>;
  selection?: SelectionState;
  columnVisibility?: Record<string, boolean>;
  loadingState?: 'initial' | 'refreshing' | 'idle';
  error?: ApiErrorDisplay;
  freshness?: Freshness;
  pagination?: PaginationModel;
}
```

**Outputs**: `onSortChange`, `onFilterChange`, `onSelectionChange`, `onRowOpen`, `onColumnVisibilityChange`, `onRetry`, `onLoadMore`.

**Loading/disabled**: table skeleton for initial; preserve rows during refresh; disabled row actions show reason.

**Accessibility**: semantic table or ARIA grid when keyboard cell navigation is implemented; sortable headers announce state; row actions not hidden behind hover only.

**Responsive**: horizontal overflow desktop/tablet; priority card list mobile.

**Use**: list/detail embedded tables. **Do not use** for freeform timelines or JSON payloads.

### 2.11 TableColumnPicker

**Responsibility**: Show/hide columns and restore defaults.

**Variants**: popover, drawer for many columns, grouped columns.

**Inputs**: `columns`, `visibleColumnIds`, `requiredColumnIds`, `defaultColumnIds`.

**Outputs**: `onChange`, `onReset`.

**Loading/disabled**: disabled until columns known; required columns cannot be hidden.

**Accessibility**: checkbox list with group labels; focus trap when popover modal; keyboard toggles.

**Responsive**: drawer/sheet on mobile.

**Use**: dense tables. **Do not use** for fixed small tables.

### 2.12 FilterBar

**Responsibility**: URL-backed filters and clear/reset actions.

**Variants**: inline compact, collapsible, sticky, advanced drawer.

**Inputs**: `filters`, `schema`, `activeCount`, `isDirty`, `validationErrors`.

**Outputs**: `onApply`, `onChange`, `onClear`, `onResetToUrl`.

**Loading/disabled**: remain visible during table loading; disable apply if invalid.

**Accessibility**: field labels visible; validation tied to fields; clear filters announced.

**Responsive**: collapse to summary button on mobile.

**Use**: strategies/runs/portfolio/orders/trades/activity. **Do not use** for page tabs.

### 2.13 DateRangeFilter

**Responsibility**: Start/end/trade-date filter with timezone clarity.

**Variants**: date-only, date-time, preset ranges, single trade date.

**Inputs**: `start`, `end`, `timezone`, `presets`, `maxRange`, `errors`.

**Outputs**: `onChange`, `onApply`, `onClear`.

**Loading/disabled**: disabled while filter options load; preserve invalid user input until corrected.

**Accessibility**: keyboard date entry supported; calendars not required for basic use; timezone text visible.

**Responsive**: stacked inputs on mobile.

**Use**: runs, trades, events, audit. **Do not use** where backend lacks date filters unless frontend-only filtering is explicit.

### 2.14 PaginationControls

**Responsibility**: Offset pagination when total is known.

**Variants**: numbered, previous/next, compact.

**Inputs**: `limit`, `offset`, `total`, `pageSizeOptions`.

**Outputs**: `onPageChange`, `onLimitChange`.

**Loading/disabled**: keep controls visible during refresh; disable invalid page buttons.

**Accessibility**: `nav aria-label="Pagination"`, current page marked, buttons labelled.

**Responsive**: compact prev/next + row count.

**Use**: list endpoints with `total`. **Do not use** when `total` is absent; use `LoadMoreControl`.

### 2.15 LoadMoreControl

**Responsibility**: Offset pagination when total is missing.

**Variants**: button, inline table footer, infinite-scroll sentinel only with manual fallback.

**Inputs**: `hasLoadedAny`, `isLoading`, `lastPageCount`, `limit`, `canLoadMore`.

**Outputs**: `onLoadMore`.

**Loading/disabled**: button shows loading; disabled when last page count < limit or request in flight.

**Accessibility**: button text includes loaded count where useful; loading announced.

**Responsive**: full-width button on mobile.

**Use**: any list response without `total`. **Do not use** with numbered page counts.

### 2.16 LastUpdated

**Responsibility**: Show freshness, source, and stale age.

**Variants**: inline, badge, tooltip details, stale warning.

**Inputs**: `freshness`, `threshold`, `label`, `criticality`.

**Outputs**: `onRefresh` optional.

**Loading/disabled**: unknown until first fetch; refresh disabled if request in flight/offline.

**Accessibility**: full ISO timestamp in `dateTime`/accessible label; relative text secondary.

**Responsive**: compact relative age with tooltip/details.

**Use**: every data widget/table. **Do not use** for static copy.

### 2.17 StaleIndicator

**Responsibility**: Explicit stale/offline/cached marker for data that may affect safety.

**Variants**: inline chip, banner, row marker, blocking overlay.

**Inputs**: `freshness`, `reason`, `blocksActions`, `severity`.

**Outputs**: `onRefresh`, `onExplain`.

**Loading/disabled**: hidden until stale state known; blocking overlay disables child mutations.

**Accessibility**: text says `Stale`, `Cached`, or `Offline`; not color-only.

**Responsive**: inline chip in headers; banner for critical mobile views.

**Use**: risk, cockpit, running rows, nonterminal orders. **Do not use** for timeless historical data unless refresh failed.

### 2.18 LoadingSkeleton

**Responsibility**: Reserve space during initial loading.

**Variants**: table rows, card, header, form, chart, static reduced-motion.

**Inputs**: `shape`, `rows`, `reducedMotion`, `label`.

**Outputs**: none.

**Loading/disabled**: itself represents loading; do not show for background refresh when prior data exists.

**Accessibility**: `aria-hidden` visual blocks plus parent `aria-busy`; avoid noisy announcements.

**Responsive**: matches target layout/card/table.

**Use**: initial loading. **Do not use** to mask errors or partial data.

### 2.19 EmptyState

**Responsibility**: Explain valid empty responses and next steps.

**Variants**: positive empty, filtered empty, first-use empty, no events buffered.

**Inputs**: `title`, `description`, `actions`, `isFiltered`, `freshness`.

**Outputs**: action callbacks.

**Loading/disabled**: only after successful fresh response; if data stale, avoid positive safety language.

**Accessibility**: clear heading and action labels.

**Responsive**: compact in panels/tables, centered in full-page states.

**Use**: tables/widgets. **Do not use** for unavailable features or errors.

### 2.20 ErrorState

**Responsibility**: Route-level or widget-level API/error display.

**Variants**: page, panel, table, entity not found, unauthorized redirect pending, rate limited.

**Inputs**: `error`, `endpoint`, `requestContext`, `actions`, `details`.

**Outputs**: `onRetry`, `onCopyDetails`, `onNavigate`.

**Loading/disabled**: retry disabled while in flight or rate-limit cooldown.

**Accessibility**: error summary announced; details expandable; no sensitive data copied by default.

**Responsive**: panel or full-width block.

**Use**: non-partial failures. **Do not use** for `501` not configured; use `FeatureUnavailable`.

### 2.21 PartialError

**Responsibility**: Contain failure of one widget/query while page remains usable.

**Variants**: inline panel, table overlay with cached rows, compact badge with details.

**Inputs**: `title`, `error`, `endpoint`, `staleDataVisible`, `actions`.

**Outputs**: `onRetry`, `onCopyDetails`.

**Loading/disabled**: retry in-flight state; preserve cached data if available.

**Accessibility**: region labelled as partial failure; avoid full-page alert unless critical.

**Responsive**: compact but readable.

**Use**: cockpit widgets, detail tabs, allocator/report panels. **Do not use** when the primary entity failed to load.

### 2.22 FeatureUnavailable

**Responsibility**: Present `501 ERR_NOT_IMPLEMENTED` or documented not-configured `503` as configuration state.

**Variants**: page, panel, dialog.

**Inputs**: `feature`, `endpoint`, `sourceRoute`, `remainingUsable`, `details`.

**Outputs**: `onBack`, `onRetry`, `onCopyDetails`.

**Loading/disabled**: retry optional; no spinner unless retrying.

**Accessibility**: title says `Not configured`, not generic failure.

**Responsive**: compact panel in partial contexts.

**Use**: snapshot/replay/reports/events/automation unavailable. **Do not use** for 404 or permission errors.

### 2.23 JsonViewer

**Responsibility**: Safe raw payload display for unknown DTOs, event data, snapshots, settings/config.

**Variants**: read-only tree, raw text, diff, redacted secrets, full-screen.

**Inputs**: `value`, `redactions`, `initialExpandedDepth`, `search`, `copyEnabled`.

**Outputs**: `onCopy`, `onSearch`, `onToggleNode`.

**Loading/disabled**: skeleton monospace block; copy disabled if value not loaded.

**Accessibility**: keyboard-expandable tree or readable pre fallback; copy labels; secrets redacted from accessible text too.

**Responsive**: full-width/full-screen option on mobile.

**Use**: ambiguous payloads, snapshots, event data. **Do not use** as replacement for known high-value fields.

### 2.24 EntityLink

**Responsibility**: Link to strategies, runs, orders, trades, positions, events, audit rows, tickers, accounts.

**Variants**: inline, table cell, with copy, unresolved, external ID.

**Inputs**: `entityType`, `id`, `label`, `href`, `existsKnown`, `sourceContext`.

**Outputs**: navigation and optional copy.

**Loading/disabled**: unresolved links render as text + reason; skeleton for labels.

**Accessibility**: link text identifies entity type, not just ID.

**Responsive**: truncate middle for UUIDs with full label tooltip/accessibility.

**Use**: all entity references. **Do not use** for actions that mutate.

### 2.25 CopyButton

**Responsibility**: Copy IDs, URLs, JSON, request details, or one-time API key plaintext.

**Variants**: icon-only with label, inline, copy-with-value, one-time secret copy.

**Inputs**: `value`, `label`, `sensitive`, `successLabel`, `disabledReason`.

**Outputs**: `onCopySuccess`, `onCopyError`.

**Loading/disabled**: disabled if value absent; one-time secrets clear success state quickly.

**Accessibility**: button label includes target; success announced politely.

**Responsive**: icon-only allowed with accessible label.

**Use**: IDs, external IDs, JSON, API key creation response. **Do not use** to copy admin credentials or redacted secrets.

### 2.26 ConfirmationDialog

**Responsibility**: C1 standard confirmation and base shell for higher levels.

**Variants**: standard, destructive, live-strength, stale-precheck.

**Inputs**: `action`, `entity`, `context`, `consequence`, `confirmLabel`, `safetyLevel`.

**Outputs**: `onConfirm`, `onCancel`, `onRefetch`.

**Loading/disabled**: states: confirming, refetching, submitting, verifying, settled; confirm disabled until required context fresh.

**Accessibility**: modal dialog, focus initial safest action, described by consequence text, Esc policy explicit.

**Responsive**: centered modal desktop, bottom/full sheet mobile.

**Use**: C1/C3 base. **Do not use** directly for reason/admin credential without specialized wrappers.

### 2.27 ReasonDialog

**Responsibility**: C2 confirmation requiring non-empty reason.

**Variants**: protection-adding, incident intervention, local-only reason if endpoint lacks field.

**Inputs**: ConfirmationDialog inputs plus `reason`, `reasonRequired`, `reasonDestination`.

**Outputs**: `onConfirm({reason})`, `onCancel`.

**Loading/disabled**: confirm disabled while reason invalid/submitting/verifying; preserve reason on validation except when security policy says clear.

**Accessibility**: field label/helper/error tied; reason requirement announced.

**Responsive**: reason textarea comfortable height.

**Use**: kill switch activation, market stop, high-accountability operations. **Do not use** for admin credential.

### 2.28 AdministrativeCredentialDialog

**Responsibility**: C4 breaker reset/admin credential prompt with one-request credential handling.

**Variants**: breaker reset currently; future step-up challenge.

**Inputs**: action context, scope, environment/mode/risk context, typed token requirement.

**Outputs**: `onConfirm({adminCredential, typedToken})`, `onCancel`, `onCredentialRejected`.

**Loading/disabled**: clear credential on every outcome; disable confirm until credential and typed token valid; verifying state blocks duplicate submit.

**Accessibility**: password-style field labelled `Admin credential`; warnings visible; failure clears field and announces rejection.

**Responsive**: high-risk modal/full sheet; context and confirm remain pinned.

**Use**: `POST /risk/breaker/reset`. **Do not use** for normal auth or API key management.

### 2.29 SecretReplacementField

**Responsibility**: Safe input for replacing redacted provider/API secrets without accidental clearing.

**Variants**: not configured, configured with last4, replace pending, clear unsupported/disabled.

**Inputs**: `configured`, `last4`, `value`, `allowClear`, `updateSemantics`, `errors`.

**Outputs**: `onChange`, `onMarkForReplacement`, `onCancelReplacement`.

**Loading/disabled**: disabled while settings stale/save in flight; never repopulate secret from server.

**Accessibility**: explicit labels and warnings; reveal toggle only if approved.

**Responsive**: stacked label/help/action.

**Use**: settings provider keys. **Do not use** for login password or admin credential.

### 2.30 Toast

**Responsibility**: Non-blocking feedback for query/mutation outcomes.

**Variants**: info, success, warning, error, critical; with action link.

**Inputs**: `tone`, `title`, `message`, `actions`, `timeout`, `persistent`.

**Outputs**: `onDismiss`, action callbacks.

**Loading/disabled**: not applicable; avoid using toast as only indicator for in-flight critical mutations.

**Accessibility**: polite for info/success, assertive for critical; dismissible; no auto-dismiss for critical/error needing action.

**Responsive**: stack bottom/right desktop; bottom mobile above nav.

**Use**: completion feedback and non-blocking notices. **Do not use** as substitute for inline validation or risk banners.

### 2.31 InlineAlert

**Responsibility**: Contextual message inside forms, panels, tables, dialogs.

**Variants**: info, warning, error, critical, stale, not configured.

**Inputs**: `tone`, `title`, `message`, `actions`, `details`.

**Outputs**: action callbacks, details toggle.

**Loading/disabled**: retry action loading state.

**Accessibility**: role status/alert based on severity; heading text.

**Responsive**: wraps actions below content on mobile.

**Use**: validation summary, stale warning, rate limit, conflict. **Do not use** for global shell status.

### 2.32 ActivityDrawer

**Responsibility**: Inspect WebSocket event buffer and optional persisted events fallback.

**Variants**: desktop side drawer, mobile full-screen sheet, disconnected diagnostics.

**Inputs**: `connection`, `events`, `filters`, `selectedEvent`, `fallbackStatus`.

**Outputs**: `onFilterChange`, `onOpenEntity`, `onCopyJson`, `onReconnect`, `onClearBuffer`.

**Loading/disabled**: empty connecting state; persisted fallback panel has independent loading/error.

**Accessibility**: focus trap, Esc closes, event list keyboard navigable, arrival announcements throttled.

**Responsive**: full-screen mobile; drawer width 420–520px desktop.

**Use**: global realtime investigation. **Do not use** as canonical audit log.

### 2.33 KeyValueSummary

**Responsibility**: Dense facts list for entity headers/details.

**Variants**: two-column, compact inline, card, with copy/entity links.

**Inputs**: `{label, value, formatter, copyValue, status, helpText}[]`, `density`.

**Outputs**: copy/link callbacks.

**Loading/disabled**: row skeletons; absent values as `—`.

**Accessibility**: definition list semantics when appropriate; labels visible.

**Responsive**: one column mobile.

**Use**: strategy/run/order facts, risk details. **Do not use** for editable forms.

### 2.34 MetricCard

**Responsibility**: KPI summary with value, unit, delta, freshness, and status.

**Variants**: P&L, exposure, counts, health, risk, compact cockpit.

**Inputs**: `label`, `value`, `unit`, `delta`, `tone`, `freshness`, `description`, `link`.

**Outputs**: `onOpenDetail`, `onRefresh`.

**Loading/disabled**: value skeleton; stale overlay/chip; error variant if metric query fails.

**Accessibility**: full numeric label includes sign/unit/context; negative not color-only.

**Responsive**: grid desktop, stacked/2-column mobile.

**Use**: cockpit/portfolio summaries. **Do not use** for high-risk controls.

### 2.35 DataChart

**Responsibility**: Render compact, accessible analytical charts for market, portfolio, exposure, P&L, health, and backtest evidence while preserving exact values through summaries/tooltips/data-table fallback.

**Variants**: line, bar, area, stacked area, sparkline, threshold band, paper/live comparison, stale-data overlay, no-data state.

**Inputs**:

```ts
interface DataChartProps<TPoint> {
  title: string;
  question?: string;
  data: TPoint[];
  series: ChartSeries<TPoint>[];
  xAxis: ChartAxis<TPoint>;
  yAxis: ChartAxis<TPoint>;
  timezone?: string;
  formatterSet: 'currency' | 'percent' | 'quantity' | 'duration' | 'generic';
  freshness?: Freshness;
  mode?: 'paper' | 'live' | 'mixed' | 'unknown';
  summary: string;
  tableFallback?: TableColumn<TPoint>[];
}
```

**Outputs/callbacks**: `onPointFocus`, `onPointSelect`, `onZoomChange`, `onResetZoom`, `onExportData`, `onOpenSourceTable`.

**Loading/disabled**: chart skeleton on initial load; preserve previous data during refresh with `Refreshing` marker; stale overlay when freshness fails; `EmptyState` for fresh empty data; `FeatureUnavailable` for not-configured providers.

**Accessibility**: visible title and text summary; keyboard/focus equivalent for interactive points; legend text labels; accessible data-table fallback; chart meaning does not depend on color alone; paper/live uses stroke pattern plus label.

**Responsive**: sparklines or simplified single-series charts in cards; full chart width in detail panels; avoid forcing page-level horizontal scroll.

**Use**: portfolio trends, exposure over time, OHLCV evidence, backtest results, health/rate charts. **Do not use** where exact row-level audit is the primary task; use `DataTable` instead.

### 2.36 Timeline

**Responsibility**: Chronological event/decision/run story.

**Variants**: chronological, newest-first toggle, grouped by phase, realtime gap marker.

**Inputs**: `items`, `order`, `selectedId`, `freshness`, `gaps`.

**Outputs**: `onSelect`, `onOpenEntity`, `onCopyItemJson`.

**Loading/disabled**: skeleton events; partial gaps labelled.

**Accessibility**: list semantics; timestamps machine-readable; keyboard selection.

**Responsive**: compact list cards on mobile.

**Use**: run detail, activity/event summaries. **Do not use** for sortable tabular audit where table is better.

### 2.37 Tabs

**Responsibility**: Switch local content panels using URL query state when shareable.

**Variants**: standard tablist, compact dropdown, segmented control for few tabs.

**Inputs**: `tabs`, `activeTab`, `persistToUrl`, `disabledTabs`.

**Outputs**: `onTabChange`.

**Loading/disabled**: tabs disabled until primary entity exists; disabled reason visible.

**Accessibility**: WAI-ARIA tablist pattern or native link tabs; keyboard arrow support if JS tablist.

**Responsive**: dropdown on narrow with active label visible.

**Use**: strategy/run/portfolio/risk detail sections. **Do not use** for global nav.

### 2.38 ResponsiveDetailPanel

**Responsibility**: Show selected row/entity details without losing table/list context.

**Variants**: side panel, bottom sheet, inline expansion, full route fallback.

**Inputs**: `open`, `title`, `entity`, `content`, `actions`, `freshness`, `mode`.

**Outputs**: `onClose`, action callbacks, `onOpenFullPage`.

**Loading/disabled**: detail skeleton; actions disabled when stale/offline per policy.

**Accessibility**: drawer/dialog semantics when overlay; focus management; close button labelled.

**Responsive**: side panel desktop/wide, bottom/full sheet mobile.

**Use**: row preview, selected event/trade/order details. **Do not use** as replacement for primary detail pages when deep link exists.

## 3. Components required for the first vertical slice

Minimum shared components before building the first P0 feature slice:

1. `AppShell`
2. `NavigationGroup`
3. `PageHeader`
4. `EnvironmentBadge`
5. `PaperLiveBadge`
6. `ConnectionStatus`
7. `RiskBanner`
8. `StatusBadge`
9. `DataTable`
10. `FilterBar`
11. `PaginationControls`
12. `LoadMoreControl`
13. `LastUpdated`
14. `StaleIndicator`
15. `LoadingSkeleton`
16. `EmptyState`
17. `ErrorState`
18. `PartialError`
19. `FeatureUnavailable`
20. `JsonViewer`
21. `EntityLink`
22. `CopyButton`
23. `ConfirmationDialog`
24. `ReasonDialog`
25. `AdministrativeCredentialDialog`
26. `Toast`
27. `InlineAlert`
28. `ActivityDrawer`
29. `KeyValueSummary`
30. `MetricCard`
31. `DataChart`
32. `Tabs`
33. `ResponsiveDetailPanel`

Add `TableColumnPicker` before any first-slice table exceeds the default visible column set. Add `DateRangeFilter` with the first runs/trades/events view. Add `Timeline` with run detail or any activity/event chronology. Add `SecretReplacementField` with the first settings/provider-key slice.

## 4. Decisions deferred to final component/table libraries

- Whether `DataTable` uses native table semantics, ARIA grid, or a library abstraction for keyboard navigation.
- Virtualization, row height measurement, sticky header/column limitations, column resizing, and column pinning APIs.
- Form library adapter shape for filter bars, reason dialogs, and secret fields.
- Overlay implementation for popovers, drawers, modals, focus traps, scroll locking, and mobile sheets.
- JSON viewer/editor library and how redaction/copy/search are implemented.
- Chart library rendering model, keyboard tooltip/focus support, zoom API, and SVG/canvas tradeoffs.
