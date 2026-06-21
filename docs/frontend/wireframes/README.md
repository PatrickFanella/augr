# Augr Trading UI Wireframes

This document provides low-fidelity wireframes for the Augr trading UI. Wireframes are designed for repository-friendly documentation using ASCII diagrams and Mermaid charts. No production React components or styling code is included.

**Design constraints applied:**
- Cockpit attention order: 1) unsafe/degraded state, 2) current exposure and active execution, 3) recent changes and alerts, 4) investigation paths
- Run detail moves between Summary, Timeline, Decisions, Prompts when permitted, Snapshots, Events, Orders, Trades, Replay
- Risk console critical controls remain visible and cannot be confused with passive status
- Desktop ~1440px, compact desktop/laptop ~1024px, and narrow-screen behavior specified
- Dense tables use horizontal scrolling, column priority, stacked summaries, or compact views instead of shrinking

---

## Table of Contents

1. [Login](#1-login)
2. [Application Shell](#2-application-shell)
3. [Cockpit](#3-cockpit)
4. [Strategies List](#4-strategies-list)
5. [Strategy Detail](#5-strategy-detail)
6. [Strategy Create/Edit](#6-strategy-createedit)
7. [Runs List](#7-runs-list)
8. [Run Detail](#8-run-detail)
9. [Portfolio](#9-portfolio)
10. [Orders](#10-orders)
11. [Order Detail](#11-order-detail)
12. [Trades](#12-trades)
13. [Risk Console](#13-risk-console)
14. [Global Activity Drawer](#14-global-activity-drawer)
15. [Confirmation Dialog](#15-confirmation-dialog)
16. [Reason-Required Dialog](#16-reason-required-dialog)
17. [Feature-Unavailable State](#17-feature-unavailable-state)
18. [Partial-Service-Failure State](#18-partial-service-failure-state)
19. [Summary](#19-summary)

---

## 1. Login

**Route:** `/login`

**Intended user question:** Can I access the app? Did my session expire? Is the API reachable?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
|                                                                              |
|                         [PRODUCT/SYSTEM TITLE]                               |
|                                                                              |
|  +------------------------------------------------------------------------+  |
|  |                                                                        |  |
|  |                         AUGR TRADING UI                                |  |
|  |                                                                        |  |
|  |  +--------------------+  +----------------------------------------+   |  |
|  |  | Username           |  |                                        |   |  |
|  |  +--------------------+  +----------------------------------------+   |  |
|  |                                                                        |  |
|  |  +--------------------+  +----------------------------------------+   |  |
|  |  | Password           |  |                                        |   |  |
|  |  +--------------------+  +----------------------------------------+   |  |
|  |                                                                        |  |
|  |       [ LOGIN ]                                                        |  |
|  |                                                                        |  |
|  |  +--------------------------------------------------------------------+ |  |
|  |  | Error: Invalid credentials                                        | |  |
|  |  +--------------------------------------------------------------------+ |  |
|  |                                                                        |  |
|  +------------------------------------------------------------------------+  |
|                                                                              |
|  Environment hint (shown after health check):                               |
|  [Environment: STAGING | Schema: v3.2 | API: Connected]                     |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+------------------------+
|                        |
|    AUGR TRADING UI     |
|                        |
|  Username              |
|  +--------------------+|
|  |                    ||
|  +--------------------+|
|                        |
|  Password              |
|  +--------------------+|
|  |                    ||
|  +--------------------+|
|                        |
|     [ LOGIN ]          |
|                        |
|  Error message here    |
+------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | None; public route |
| **Primary visual hierarchy** | Centered card, title at top, form fields, submit button |
| **Filters** | None |
| **Tables/default columns** | None |
| **Detail panels/tabs** | None |
| **Sticky/persistent controls** | None |
| **Primary actions** | Submit login form |
| **Secondary actions** | Retry bootstrap health; clear expired session |
| **Deep links** | Post-login redirects to `next` param or `/cockpit` |
| **Realtime indicators** | None before auth |
| **Last-updated information** | Not shown until authenticated |
| **Loading placement** | "Restoring session..." compact message if checking existing session; otherwise form renders immediately |
| **Empty state** | Not applicable |
| **Error state** | Inline error above form for invalid credentials; generic message does not reveal username existence |
| **Stale/offline state** | Offline banner; form disabled or retryable |
| **Paper/live labeling** | Not applicable on form |
| **Responsive behavior** | Single-column card; fields full width on mobile |

---

## 2. Application Shell

**Route:** Wraps all authenticated routes

**Intended user question:** Who am I? Which environment/schema is active? Is realtime connected? Is risk/kill switch normal? Is broker paper/live?

### Layout (Desktop ~1440px)

```
+------+----------------------------------------------------------------------+------+
|      |  [ENV: STAGING] [Schema: v3.2] [USER: operator@augr.io]  [WS: ●]   |      |
|      |  [RISK: NORMAL] [BROKERS: Alpaca(Paper) ● | Alpaca(Live) ●]       |      |
+------+----------------------------------------------------------------------+      |
|      |                                                                      |      |
| NAV  |                                                                      |      |
|      |                    [CONTENT OUTLET]                                 |      |
| ---- |                                                                      |      |
|OPER. |                                                                      |      |
|  Cockpit                                                                              |
|  Strategies                                                                   |
|  Runs                                                                          |
|  Portfolio                                                                     |
|  Orders                                                                        |
|  Trades                                                                        |
|  Risk                                                                          |
|      |                                                                      |      |
|-------|                                                                      |      |
|MARKETS|                                                                      |      |
|-------|                                                                      |      |
|RSRCH |                                                                      |      |
|-------|                                                                      |      |
|ADMIN |                                                                      |      |
+------+----------------------------------------------------------------------+------+
| [Activity Drawer: 3 new events]                              [Toast: Error]         |
+-------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| ENV:STG | USER | [WS:●]   |
| [RISK:OK] | BROKERS ▼    |
+---------------------------+
|                           |
|     [CONTENT OUTLET]      |
|                           |
+---------------------------+
| [Cockpit] [Strat] [Runs]  |
| [Port] [Orders] [Risk]    |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Left nav grouped: Operations, Markets, Research, Administration. Top header with environment/schema/user/realtime/risk. |
| **Primary visual hierarchy** | Left rail with icons + labels; content area; global toast/error stack; realtime activity drawer |
| **Filters** | Activity drawer filters: type, strategy_id, run_id |
| **Tables/default columns** | Activity drawer: timestamp, type, strategy_id/run_id, summary, link target; newest first |
| **Detail panels/tabs** | None in shell |
| **Sticky/persistent controls** | Header always visible; nav rail on desktop; collapsible on tablet; bottom nav on mobile |
| **Primary actions** | Open activity drawer; navigate; logout; retry failed shell queries |
| **Secondary actions** | Copy environment/schema details; open risk console from risk badge |
| **Deep links** | Activity entries link to entity routes when IDs present |
| **Realtime indicators** | WebSocket status badge in header; activity drawer shows connection state |
| **Last-updated information** | Header status shows last health/risk/settings refresh |
| **Loading placement** | App skeleton with nav placeholders and header status skeleton until auth/me resolves |
| **Empty state** | Empty activity drawer says "no realtime events buffered" |
| **Error state** | Header badge identifies failing dependency; details in popover |
| **Stale/offline state** | Header badges show stale age; risk badge cannot show "safe" from stale data |
| **Paper/live labeling** | Header shows connected broker `{name, paper_mode, configured}`; live broker visually distinct |
| **Responsive behavior** | Desktop persistent side nav; tablet collapsible rail; mobile bottom/slide nav with sticky header status compacted |

---

## 3. Cockpit

**Route:** `/cockpit`

**Intended user question:** Is risk normal? Is realtime alive? Are breakers tripped? Are runs active? What is open exposure and recent execution? Are automation/brokers degraded?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| [SAFETY: SAFE / DEGRADED / UNSAFE]  Last updated: 14:32:05                  |
+------------------------------------------------------------------------------+
| [RISK BANNER]                                                               |
| [Kill Switch: INACTIVE] [Breakers: 0 tripped] [Markets: All Active]         |
+------------------------------------------------------------------------------+
| [HEALTH STRIP]                                                              |
| [API: ●] [WS: ●] [Automation: ●] [Brokers: Alpaca(P)● Alpaca(L)●]          |
+------------------------------------------------------------------------------+
|                    |                    |                    |              |
|  P&L SUMMARY       |  EXPOSURE          |  OPEN POSITIONS    |  ACTIVE RUNS |
|  +--------------+  |  +--------------+  |  +--------------+  |  +--------+  |
|  | Realized:    |  |  | Total: 23.4% |  |  | Count: 7     |  |  | 3 runs |  |
|  | +$1,234.56   |  |  | Long: 15.2%  |  |  | Long: 4      |  |  +--------+  |
|  | Unrealized:  |  |  | Short: 8.2%  |  |  | Short: 3     |  |              |
|  | +$567.89     |  |  +--------------+  |  +--------------+  |  [View All]  |
|  +--------------+  |                    |                    |              |
+--------------------+--------------------+--------------------+--------------+
| ACTIVE RUNS TABLE                                    [Filters: ▼]          |
+------------------------------------------------------------------------------+
| Status | Ticker | Strategy        | Signal | Started    | Duration | Act  |
|--------|--------|-----------------|--------|------------|----------|------|
| ●RUN   | AAPL   | Momentum-Long   | BUY    | 14:25:03   | 7m 02s   | [⋮]  |
| ●RUN   | TSLA   | MeanRev-Short   | SELL   | 14:20:11   | 12m 49s  | [⋮]  |
| ●RUN   | NVDA   | Breakout-Long   | HOLD   | 14:15:00   | 17m 00s  | [⋮]  |
+------------------------------------------------------------------------------+
| RECENT ORDERS/TRADES                                  [Filters: ▼]          |
+------------------------------------------------------------------------------+
| Type | Ticker | Side | Qty   | Status    | Broker | Time     | Links    |
|------|--------|------|-------|-----------|--------|----------|----------|
| ORD  | AAPL   | BUY  | 100   | FILLED    | Alpaca | 14:30:22 | [Order]  |
| TRD  | AAPL   | BUY  | 100   | $182.50   | Alpaca | 14:30:22 | [Trade]  |
| ORD  | TSLA   | SELL | 50    | PARTIAL   | Alpaca | 14:28:15 | [Order]  |
+------------------------------------------------------------------------------+
| AUTOMATION HEALTH                                    [View All]            |
+------------------------------------------------------------------------------+
| Job Name       | Enabled | Running | Last Run   | Errors | Consec.Fail |
|----------------|---------|---------|------------|--------|-------------|
| market-data    | ●       | ●       | 14:31:00   | 0      | 0           |
| signal-eval    | ●       | ○       | 14:25:00   | 1      | 0           |
+------------------------------------------------------------------------------+
| RECENT EVENTS / ALERTS                                                       |
+------------------------------------------------------------------------------+
| Time     | Type              | Strategy/Run | Message                    |
|----------|-------------------|--------------|----------------------------|
| 14:32:01 | order_filled      | AAPL/Momentum| Position increased         |
| 14:30:45 | pipeline_health   | TSLA/MeanRev | Analysis complete          |
| 14:28:12 | error             | NVDA/Breakout| API rate limit             |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| SAFETY: SAFE              |
| Updated: 14:32:05         |
+---------------------------+
| [RISK: Normal] [WS:●]     |
| [Runs: 3] [Pos: 7]        |
+---------------------------+
| P&L: +$1,802.45           |
| Exposure: 23.4%           |
+---------------------------+
| ACTIVE RUNS               |
| +---------------------+   |
| | AAPL ●RUN 7m    [⋮]|   |
| +---------------------+   |
| | TSLA ●RUN 12m   [⋮]|   |
| +---------------------+   |
| | NVDA ●RUN 17m   [⋮]|   |
| +---------------------+   |
+---------------------------+
| RECENT ORDERS             |
| +---------------------+   |
| | AAPL BUY 100 FILLED|   |
| +---------------------+   |
| | TSLA SELL 50 PART. |   |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Safety summary strip at top; risk banner; health/broker/realtime strip |
| **Primary visual hierarchy** | 1) Unsafe/degraded state (top), 2) Current exposure and active execution, 3) Recent changes and alerts, 4) Investigation paths |
| **Filters** | Local filters: market type, ticker, severity, paper/live |
| **Tables/default columns** | Active runs: status, ticker, strategy_name, signal, started_at, duration, actions; Recent execution: type, ticker, side, quantity, status, broker, time, links |
| **Detail panels/tabs** | None; expandable row previews |
| **Sticky/persistent controls** | Safety summary and risk banner sticky at top |
| **Primary actions** | Inspect alert/run/strategy/order; invoke risk or run intervention via safety policy |
| **Secondary actions** | Open risk console, portfolio, orders, trades, full events/activity drawer |
| **Deep links** | `/runs/:id`, `/strategies/:id`, `/orders/:id`, `/trades?order_id=...`, `/portfolio?...`, `/risk?...` |
| **Realtime indicators** | WebSocket status in health strip; event stream updates |
| **Last-updated information** | Each widget shows last updated timestamp |
| **Loading placement** | Page skeleton with independent widget skeletons; safety summary waits for risk + health minimum |
| **Empty state** | No active runs/open positions shown as positive empty only if risk/health fresh |
| **Error state** | Widget-level error with endpoint and retry; full-page only if auth/bootstrap unavailable |
| **Stale/offline state** | Safety strip says stale/unknown; high-risk actions require refetch |
| **Paper/live behavior** | Live broker/exposure rows visually elevated; paper/live broker state from settings |
| **Responsive behavior** | Desktop dashboard grid; tablet stacked two-column; mobile status strips and cards stacked, tables become priority-column lists |

---

## 4. Strategies List

**Route:** `/strategies`

**Intended user question:** Which strategies exist? Which are active/paused/inactive? Which are paper/live? What is recent behavior?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| Strategies                              [+ New Strategy]    [Filters: ▼]   |
+------------------------------------------------------------------------------+
| [Filter Bar]                                                               |
| Ticker: [______] Market: [All ▼] Status: [All ▼] Paper/Live: [All ▼]      |
+------------------------------------------------------------------------------+
| Name          | Ticker | Market   | Mode   | Status  | Schedule    | Latest   |
|---------------|--------|----------|--------|---------|-------------|----------|
| Momentum-Long | AAPL   | stock    | LIVE   | ACTIVE  | 0 9:30 *1-5 | BUY 14:30|
| MeanRev-Short | TSLA   | stock    | PAPER  | PAUSED  | 0 9:45 *1-5 | SELL 14:2|
| Breakout-Long | NVDA   | stock    | LIVE   | ACTIVE  | 0 9:30 *1-5 | HOLD 14:1|
| Crypto-Momentum| BTC   | crypto   | PAPER  | ACTIVE  | 0 * * * *   | BUY 12:00|
+---------------+--------+----------+--------+---------+-------------+----------+
| [1] [2] [3] ... [Next >]                                    Total: 24     |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| Strategies     [+ New]    |
+---------------------------+
| [Filters ▼]               |
+---------------------------+
| +-----------------------+ |
| | Momentum-Long (AAPL)  | |
| | LIVE | ACTIVE | BUY   | |
| | [Run] [Pause] [⋮]     | |
| +-----------------------+ |
| +-----------------------+ |
| | MeanRev-Short (TSLA)  | |
| | PAPER | PAUSED | SELL | |
| | [Run] [Resume] [⋮]    | |
| +-----------------------+ |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Page header with title and create action |
| **Primary visual hierarchy** | Filter bar; strategies table; pagination |
| **Filters** | Ticker, market_type, status, is_paper |
| **Tables/default columns** | name, ticker, market_type, paper/live, status, schedule_cron, skip_next_run, latest_run_summary.status, latest_run_summary.signal, updated_at, actions |
| **Detail panels/tabs** | Optional row detail preview |
| **Sticky/persistent controls** | Filter bar sticky at top; action bar sticky at bottom on mobile |
| **Primary actions** | Open strategy detail; create strategy |
| **Secondary actions** | Run/pause/resume/skip/delete through safety policy; copy ID; clear filters |
| **Deep links** | `/strategies/:id`, `/strategies/new`, `/runs?strategy_id=...`, `/events?strategy_id=...` |
| **Realtime indicators** | Shell subscription; no additional |
| **Last-updated information** | Table header shows last fetch |
| **Loading placement** | Table skeleton with filter bar available |
| **Empty state** | Empty table with create action and clear filters if filters active |
| **Error state** | Table error panel with retry |
| **Stale/offline state** | Last updated highlighted; row actions require refetch |
| **Paper/live labeling** | `is_paper=false` strategies visually distinct and sortable/groupable |
| **Responsive behavior** | Desktop table; mobile card list with key fields and overflow actions |

---

## 5. Strategy Detail

**Route:** `/strategies/:id`

**Intended user question:** What does this strategy do? Is it active/paper/live? What happened recently? What can be safely intervened?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| < Back to Strategies                    [Edit] [Run] [Pause] [Delete]      |
+------------------------------------------------------------------------------+
| MOMENTUM-LONG (AAPL)                                                         |
| Status: ACTIVE | Mode: LIVE | Schedule: 0 9:30 *1-5 | Skip Next: OFF        |
+------------------------------------------------------------------------------+
| [Tabs: Overview | Config | Runs | Reports | Events | Execution | Risk]      |
+------------------------------------------------------------------------------+
|                                                                              |
|  OVERVIEW TAB:                                                               |
|  +----------------------+  +---------------------------------------------+  |
|  | Strategy ID          |  | Latest Run                                  |  |
|  | str-abc-123-def      |  | Status: COMPLETED | Signal: BUY            |  |
|  |                      |  | Started: 2024-01-15 09:30:00              |  |
|  | Description          |  | Completed: 09:35:12 | Duration: 5m 12s     |  |
|  | Momentum strategy    |  +---------------------------------------------+  |
|  | for AAPL earnings    |                                                 |
|  |                      |  Recent Runs:                                  |
|  | Created: 2024-01-01  |  +------+----------+--------+----------------+  |
|  | Updated: 2024-01-15  |  | Date | Status  | Signal| Error          |  |
|  +----------------------+  +------+----------+--------+----------------+  |
|                             | 01/15| COMPLETE| BUY   | -              |  |
|                             | 01/12| COMPLETE| HOLD  | -              |  |
|                             | 01/11| FAILED  | -     | API timeout    |  |
|                             +------+----------+--------+----------------+  |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| < Back         [Edit] [⋮] |
+---------------------------+
| MOMENTUM-LONG (AAPL)      |
| LIVE | ACTIVE | 9:30 *1-5 |
+---------------------------+
| [Overview ▼]              |
+---------------------------+
| ID: str-abc-123-def       |
| Desc: Momentum strategy   |
| Created: 2024-01-01       |
+---------------------------+
| Latest Run: COMPLETE      |
| Signal: BUY | 5m 12s      |
+---------------------------+
| [Runs Tab]                |
| +---------------------+   |
| | 01/15 | COMPLETE |BUY|   |
| +---------------------+   |
| | 01/12 | COMPLETE |HOLD|  |
| +---------------------+   |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Back navigation; identity/status/paper-live/actions |
| **Primary visual hierarchy** | Header with strategy info; tab navigation; content area with overview/config/runs/reports/events/execution/risk |
| **Filters** | Tabs with embedded filters |
| **Tables/default columns** | Runs: status, ticker, signal, trade_date, started_at, completed_at, duration, error; Reports: report_type, status, timestamps; Events: type, run, agent, timestamp |
| **Detail panels/tabs** | Overview, Config JSON, Runs, Reports, Events, Execution, Risk |
| **Sticky/persistent controls** | Header with actions sticky at top; action bar sticky at bottom on mobile |
| **Primary actions** | Edit, run, pause/resume, skip next, open latest run |
| **Secondary actions** | Delete, copy ID, open runs/events filtered, open reports |
| **Deep links** | `/strategies/:id/edit`, `/runs/:id`, `/runs?strategy_id=...`, `/events?strategy_id=...`, `/orders?...`, `/portfolio?...` |
| **Realtime indicators** | Strategy realtime badge degraded when WS disconnected |
| **Last-updated information** | Primary strategy fetch controls action freshness; each tab shows its own timestamp |
| **Loading placement** | Header skeleton and tab skeleton until strategy loads |
| **Empty state** | Empty runs/reports/events panels independently |
| **Error state** | If strategy fetch fails 404/error, route-level entity error; subqueries inline |
| **Stale/offline state** | Actions require refetch; stale tabs labelled |
| **Paper/live labeling** | Header emphasizes `is_paper`; live strategy actions require safety policy |
| **Responsive behavior** | Tabs collapse to segmented dropdown on mobile; action bar remains sticky but compact |

---

## 6. Strategy Create/Edit

**Route:** `/strategies/new` or `/strategies/:id/edit`

**Intended user question:** What fields are required? Is this paper or live? Is config valid enough to submit?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| < Back to Strategies                                                        |
+------------------------------------------------------------------------------+
| NEW STRATEGY / EDIT STRATEGY                                                 |
+------------------------------------------------------------------------------+
|                                                                              |
|  +-----------------------------------------------+  +----------------------+ |
|  | BASICS FORM                                   |  | SCHEDULING           | |
|  |                                               |  |                      | |
|  | Name:        [_________________________]      |  | Schedule (CRON):    | |
|  |                                               |  | [0 9:30 *1-5      ] | |
|  | Ticker:      [_________________________]      |  |                      | |
|  |                                               |  | [ ] Skip next run   | |
|  | Market Type: [stock ▼]                        |  |                      | |
|  |                                               |  +----------------------+ |
|  | Status:      [active ▼]                       |                          |
|  |                                               |  +----------------------+ |
|  | Mode:        (•) Paper  ( ) Live             |  | VALIDATION           | |
|  |                                               |  |                      | |
|  +-----------------------------------------------+  | JSON is valid        | |
|                                                  |  |                      | |
|  +-----------------------------------------------+  +----------------------+ |
|  | CONFIG JSON                                   |                          |
|  |                                               |  +----------------------+ |
|  | {                                             |  | ACTIONS              | |
|  |   "lookback_days": 20,                        |  |                      | |
|  |   "signal_threshold": 0.7,                    |  | [Cancel]  [Save]     | |
|  |   "max_position_pct": 10                      |  |                      | |
|  | }                                             |  +----------------------+ |
|  |                                               |                          |
|  | [Format] [Validate]                           |                          |
|  +-----------------------------------------------+                          |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| < Back                    |
+---------------------------+
| NEW STRATEGY              |
+---------------------------+
| Name                      |
| [________________________]|
|                           |
| Ticker                    |
| [________________________]|
|                           |
| Market Type: [stock ▼]    |
|                           |
| Mode: (•) Paper ( ) Live  |
+---------------------------+
| CONFIG JSON               |
| {                         |
|   "lookback_days": 20,    |
|   ...                     |
| }                         |
+---------------------------+
| [Cancel]      [Save]      |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Back navigation with context |
| **Primary visual hierarchy** | Basics form; scheduling/status/paper-live controls; config JSON editor; validation/error panel; submit/cancel action bar |
| **Filters** | None |
| **Tables/default columns** | None |
| **Detail panels/tabs** | None |
| **Sticky/persistent controls** | None |
| **Primary actions** | Save create/update |
| **Secondary actions** | Cancel/back; reset form to loaded version; validate JSON locally |
| **Deep links** | After save `/strategies/:id`; cancel returns `from` or `/strategies` |
| **Realtime indicators** | None |
| **Last-updated information** | Edit screen warns if loaded entity becomes stale before save |
| **Loading placement** | Edit shows form skeleton; create shows blank form immediately |
| **Empty state** | Not applicable; 404 for edit is entity error |
| **Error state** | Load/save error panel; preserve user input on save failure |
| **Stale/offline state** | Save requires refetch or explicit safety-policy acknowledgement for stale edit |
| **Paper/live labeling** | Default create `is_paper=true`; live selection visibly marked and safety policy required |
| **Responsive behavior** | One-column form on mobile; JSON editor remains usable with line wrapping and full-screen option |

---

## 7. Runs List

**Route:** `/runs`

**Intended user question:** What ran, when, for which strategy/ticker, with what status/signal?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| Pipeline Runs                                         [Filters: ▼]          |
+------------------------------------------------------------------------------+
| [Filter Bar]                                                               |
| Strategy: [All ▼] Ticker: [______] Status: [All ▼] Date: [____] to [____] |
+------------------------------------------------------------------------------+
| Status | Ticker | Strategy      | Signal | Trade Date | Started    | Dur  |
|--------|--------|---------------|--------|------------|------------|------|
| RUNNING| AAPL   | Momentum-Long | BUY    | 2024-01-15 | 14:25:03   | 7m   |
| COMPLET| TSLA   | MeanRev-Short | SELL   | 2024-01-15 | 14:20:11   | 5m   |
| COMPLET| NVDA   | Breakout-Long | HOLD   | 2024-01-15 | 14:15:00   | 3m   |
| FAILED | MSFT   | Earnings-Play | -      | 2024-01-15 | 14:10:00   | 1m   |
| CANCEL | GOOGL  | Gap-Momentum  | -      | 2024-01-14 | 09:45:00   | 2m   |
+------------------------------------------------------------------------------+
| [1] [2] [3] ... [Next >]                                    Total: 156    |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| Runs           [Filters]  |
+---------------------------+
| +-----------------------+ |
| | AAPL | RUNNING | BUY  | |
| | Momentum-Long | 7m   | |
| | [View] [Cancel]       | |
| +-----------------------+ |
| +-----------------------+ |
| | TSLA | COMPLETE | SELL| |
| | MeanRev-Short | 5m   | |
| | [View]               | |
| +-----------------------+ |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Page header with title |
| **Primary visual hierarchy** | Filter bar; runs table; pagination |
| **Filters** | strategy_id, ticker, status, start_date, end_date, trade_date |
| **Tables/default columns** | status, ticker, strategy_id, signal, trade_date, started_at, completed_at, duration, error_message, actions |
| **Detail panels/tabs** | Optional selected run preview |
| **Sticky/persistent controls** | Filter bar sticky at top |
| **Primary actions** | Open run detail; cancel running run via safety policy |
| **Secondary actions** | Open strategy; copy run ID; clear filters |
| **Deep links** | `/runs/:id`, `/strategies/:strategy_id`, `/events?pipeline_run_id=...` |
| **Realtime indicators** | Shell subscription |
| **Last-updated information** | List timestamp; running rows stale quickly |
| **Loading placement** | Table skeleton |
| **Empty state** | Empty message with clear filters and link to strategies |
| **Error state** | Table error with retry |
| **Stale/offline state** | Running rows flagged if not refreshed recently |
| **Paper/live labeling** | If run/strategy data exposes paper/live, display |
| **Responsive behavior** | Table to card list on mobile |

---

## 8. Run Detail

**Route:** `/runs/:id`

**Intended user question:** What is this run doing/did it do? Why did agents decide? What data supported the decision? What orders/trades resulted?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| < Back to Runs                    [Cancel Run] [Copy ID] [Open Strategy]    |
+------------------------------------------------------------------------------+
| RUN: str-abc-123 / AAPL / 2024-01-15                                         |
| Status: RUNNING | Signal: BUY | Started: 14:25:03 | Duration: 7m 12s        |
+------------------------------------------------------------------------------+
| [Tabs: Summary | Timeline | Decisions | Snapshots | Events | Orders | Trades]|
+------------------------------------------------------------------------------+
|                                                                              |
|  SUMMARY TAB:                                                                |
|  +-------------------------+  +------------------------------------------+   |
|  | Run ID                  |  | Timeline                                 |   |
|  | run-xyz-789             |  | 14:25:03 [START] Pipeline started       |   |
|  |                         |  | 14:25:30 [AGENT] Analysis phase started |   |
|  | Strategy                |  | 14:26:15 [AGENT] Decision: BUY signal   |   |
|  | Momentum-Long           |  | 14:26:45 [ORDER] Order submitted        |   |
|  |                         |  | 14:27:00 [FILL] Order filled            |   |
|  | Ticker                  |  +------------------------------------------+   |
|  | AAPL                    |                                                 |
|  |                         |  Signal: BUY (confidence: 0.82)              |
|  | Trade Date              |  Reasoning: Strong momentum with volume      |
|  | 2024-01-15              |  confirmation above 20-day average          |
|  |                         |                                                 |
|  | Error (if any)          |  +------------------------------------------+   |
|  | -                       |  | Related Orders:                          |   |
|  +-------------------------+  | ORD-001 | AAPL | BUY | FILLED | 100 | $182.50|
|                               +------------------------------------------+   |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| < Back         [Cancel]   |
+---------------------------+
| RUN: AAPL / 2024-01-15    |
| RUNNING | BUY | 7m 12s    |
+---------------------------+
| [Summary ▼]               |
+---------------------------+
| ID: run-xyz-789           |
| Strategy: Momentum-Long   |
| Signal: BUY (0.82)        |
+---------------------------+
| Timeline                  |
| +---------------------+   |
| | 14:25:03 START      |   |
| +---------------------+   |
| | 14:26:15 BUY signal |   |
| +---------------------+   |
| | 14:27:00 FILLED     |   |
+---------------------------+
| Related Orders            |
| +---------------------+   |
| | ORD-001 | FILLED   |   |
| +---------------------+   |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Back navigation; run header/status; action bar |
| **Primary visual hierarchy** | Run header/status; tab nav; timeline; decisions table/detail; snapshot JSON browser; events table; related execution panel |
| **Filters** | Tab-based navigation; decision filter |
| **Tables/default columns** | Decisions: agent_role, phase, status/signal, confidence, created timestamp, prompt available; Events: type, timestamp, data; Orders/Trades: standard columns |
| **Detail panels/tabs** | Summary, Timeline, Decisions, Snapshots, Events, Orders, Trades, Replay (when permitted) |
| **Sticky/persistent controls** | Run header and action bar sticky at top |
| **Primary actions** | Cancel running run; inspect decision; reveal prompt explicitly; inspect snapshot; open strategy |
| **Secondary actions** | Copy run ID; open events filtered; open order/trade links |
| **Deep links** | `/strategies/:strategy_id`, `/runs/:id?tab=decisions&decision=...`, `/runs/:id?tab=snapshot&data_type=...`, `/events?pipeline_run_id=...`, `/orders/:id` |
| **Realtime indicators** | Running status with live duration counter |
| **Last-updated information** | Run status and decisions show independent refresh times |
| **Loading placement** | Header skeleton; tabs disabled until run exists |
| **Empty state** | Empty decisions/events/snapshot panels independently |
| **Error state** | Run fetch error is route-level; tab errors are inline |
| **Stale/offline state** | Running status stale warning; cancel requires refetch |
| **Paper/live labeling** | Show if run/strategy exposes it |
| **Responsive behavior** | Header collapses; timeline and tables become stacked cards; JSON viewer full-width |

---

## 9. Portfolio

**Route:** `/portfolio`

**Intended user question:** What exposure is open? What is realized/unrealized P&L? Which positions link to trades/strategies?

### Layout (Desktop ~1444px)

```
+------------------------------------------------------------------------------+
| Portfolio                                    [Tabs: Open | All | Allocator] |
+------------------------------------------------------------------------------+
| P&L SUMMARY                              Last updated: 14:32:05             |
| +---------------+---------------+---------------+---------------+----------+ |
| | Realized P&L  | Unrealized P&L| Total P&L     | Open Exposure | Positions| |
| | +$1,234.56    | +$567.89      | +$1,802.45    | 23.4%         | 7        | |
| +---------------+---------------+---------------+---------------+----------+ |
+------------------------------------------------------------------------------+
| POSITIONS TABLE                                           [Filters: ▼]      |
+------------------------------------------------------------------------------+
| Ticker | Side  | Qty   | Avg Entry | Curr Price | Unrealized  | Realized  |
|--------|-------|-------|-----------|------------|-------------|-----------|
| AAPL   | LONG  | 100   | $180.00   | $182.50    | +$250.00    | $0.00     |
| TSLA   | SHORT | 50    | $245.00   | $242.00    | +$150.00    | +$500.00  |
| NVDA   | LONG  | 25    | $520.00   | $515.00    | -$125.00    | $0.00     |
| MSFT   | LONG  | 75    | $375.00   | $380.00    | +$375.00    | +$200.00  |
+------------------------------------------------------------------------------+
| [1] [2] [Next >]                                               Total: 7    |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| Portfolio                 |
+---------------------------+
| P&L: +$1,802.45           |
| Exposure: 23.4% | 7 pos   |
+---------------------------+
| [Open ▼]                  |
+---------------------------+
| +-----------------------+ |
| | AAPL LONG 100         | |
| | $180→$182.50 (+$250)  | |
| | [Trades] [Strategy]   | |
| +-----------------------+ |
| +-----------------------+ |
| | TSLA SHORT 50         | |
| | $245→$242 (+$150)     | |
| | [Trades] [Strategy]   | |
| +-----------------------+ |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Page header with tabs |
| **Primary visual hierarchy** | P&L cards at top; exposure summary; tabs; positions table; allocator panels |
| **Filters** | Ticker, side, open/all |
| **Tables/default columns** | ticker, side, quantity, avg_entry, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, strategy_id, option greeks when present |
| **Detail panels/tabs** | Open positions, All positions, Allocator |
| **Sticky/persistent controls** | P&L summary sticky at top |
| **Primary actions** | Inspect position; open linked trades; open strategy |
| **Secondary actions** | Open risk console; copy position ID; clear filters |
| **Deep links** | `/trades?position_id=...`, `/strategies/:strategy_id`, `/portfolio?position_id=...` |
| **Realtime indicators** | Position updates via WebSocket |
| **Last-updated information** | P&L cards prominently show age |
| **Loading placement** | KPI and table skeletons |
| **Empty state** | No open positions treated neutral only with fresh data |
| **Error state** | Widget-level error; full-page only if all core portfolio queries fail |
| **Stale/offline state** | P&L/exposure marked unreliable; cockpit safety uses unknown |
| **Paper/live labeling** | If positions expose market/broker paper/live indirectly, show |
| **Responsive behavior** | Cards stacked; positions as cards with key risk/P&L fields first |

---

## 10. Orders

**Route:** `/orders`

**Intended user question:** What orders were submitted? Which filled/failed/are partial? Which strategy/run/broker produced them?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| Orders                                         [Filters: ▼]                 |
+------------------------------------------------------------------------------+
| [Filter Bar]                                                               |
| Ticker: [______] Broker: [All ▼] Status: [All ▼] Side: [All ▼] Market: ... |
+------------------------------------------------------------------------------+
| Status | Ticker | Side | Type    | Qty   | Filled | Price   | Broker  | Time  |
|--------|--------|------|---------|-------|--------|---------|---------|-------|
| FILLED | AAPL   | BUY  | MARKET  | 100   | 100    | $182.50 | Alpaca  | 14:30 |
| FILLED | TSLA   | SELL | LIMIT   | 50    | 50     | $242.00 | Alpaca  | 14:28 |
| PARTIAL| NVDA   | BUY  | STOP    | 75    | 25     | $515.00 | Alpaca  | 14:25 |
| PENDING| MSFT   | BUY  | MARKET  | 50    | 0      | -       | Alpaca  | 14:20 |
| REJECTD| GOOGL  | SELL | LIMIT   | 100   | 0      | $142.00 | Alpaca  | 14:15 |
+------------------------------------------------------------------------------+
| [1] [2] [3] ... [Next >]                                    Total: 342    |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| Orders        [Filters]   |
+---------------------------+
| +-----------------------+ |
| | AAPL | BUY | FILLED   | |
| | 100 @ $182.50         | |
| | 14:30 | Alpaca        | |
| +-----------------------+ |
| +-----------------------+ |
| | TSLA | SELL | PARTIAL | |
| | 50/75 @ $242.00       | |
| | 14:28 | Alpaca        | |
| +-----------------------+ |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Page header with title |
| **Primary visual hierarchy** | Filter bar; orders table; pagination |
| **Filters** | ticker, broker, market_type, status, side, order_type |
| **Tables/default columns** | status, ticker, side, order_type, quantity, filled_quantity, filled_avg_price, broker, market_type, submitted_at, filled_at, strategy_id, pipeline_run_id, external_id |
| **Detail panels/tabs** | Selected row preview |
| **Sticky/persistent controls** | Filter bar sticky at top |
| **Primary actions** | Open order detail |
| **Secondary actions** | Open run/strategy; copy external ID; clear filters |
| **Deep links** | `/orders/:id`, `/runs/:pipeline_run_id`, `/strategies/:strategy_id`, `/trades?order_id=...` |
| **Realtime indicators** | Order events via WebSocket |
| **Last-updated information** | Table age shown |
| **Loading placement** | Table skeleton |
| **Empty state** | Empty table with clear filters |
| **Error state** | Table error panel |
| **Stale/offline state** | Recent execution marked stale |
| **Paper/live labeling** | Show broker and market; paper/live only if confirmed |
| **Responsive behavior** | Dense desktop table; mobile cards prioritize status/ticker/side/fill/broker/time |

---

## 11. Order Detail

**Route:** `/orders/:id`

**Intended user question:** What happened to this order? What fills/trades exist? Which run/strategy/position does it relate to?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| < Back to Orders                    [Copy ID] [Copy External ID]            |
+------------------------------------------------------------------------------+
| ORDER: ORD-001-AAPL-20240115                                                  |
| Status: FILLED | Side: BUY | Type: MARKET | Broker: Alpaca                  |
+------------------------------------------------------------------------------+
| [Tabs: Summary | Fills | Raw JSON]                                           |
+------------------------------------------------------------------------------+
|                                                                              |
|  SUMMARY TAB:                                                                |
|  +-------------------------+  +------------------------------------------+   |
|  | Order Details          |  | Linked Entities                          |   |
|  |                         |  |                                          |   |
|  | Order ID                |  | Strategy:                                |   |
|  | ord-001                 |  | Momentum-Long (/strategies/...)         |   |
|  |                         |  |                                          |   |
|  | External ID             |  | Run:                                     |   |
|  | alpaca-123456           |  | run-xyz-789 (/runs/...)                 |   |
|  |                         |  |                                          |   |
|  | Ticker                  |  | Position:                                |   |
|  | AAPL                    |  | pos-abc-123 (/portfolio?position_id=...)|   |
|  |                         |  |                                          |   |
|  | Quantity                |  +------------------------------------------+   |
|  | 100                                                                     |   |
|  |                         |  +------------------------------------------+   |
|  | Filled Quantity         |  | Timeline                                 |   |
|  | 100                     |  | 14:30:00 Created                         |   |
|  |                         |  | 14:30:01 Submitted                       |   |
|  | Limit/Stop Price        |  | 14:30:02 Filled                          |   |
|  | -                       |  +------------------------------------------+   |
|  |                         |                                                  |
|  | Submitted At            |                                                  |
|  | 2024-01-15 14:30:01     |                                                  |
|  |                         |                                                  |
|  | Filled At               |                                                  |
|  | 2024-01-15 14:30:02     |                                                  |
|  +-------------------------+                                                  |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| < Back                    |
+---------------------------+
| ORDER: ORD-001            |
| FILLED | BUY | MARKET     |
+---------------------------+
| [Summary ▼]               |
+---------------------------+
| Ticker: AAPL              |
| Qty: 100 | Filled: 100    |
| Price: $182.50            |
| Broker: Alpaca            |
+---------------------------+
| Linked:                   |
| Strategy: Momentum-Long   |
| Run: run-xyz-789          |
+---------------------------+
| Fills:                    |
| +---------------------+   |
| | BUY 100 @ $182.50  |   |
| | 14:30:02           |   |
| +---------------------+   |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Back navigation; order header/status |
| **Primary visual hierarchy** | Header/status; order facts; fill/trade table; related entities; raw JSON inspector |
| **Filters** | Tab-based |
| **Tables/default columns** | Fills/trades: side, quantity, price, fee, executed_at, position_id, external_id, open_close |
| **Detail panels/tabs** | Summary, Fills, Raw JSON |
| **Sticky/persistent controls** | None |
| **Primary actions** | Open related run/strategy/trades/position context |
| **Secondary actions** | Copy order ID/external ID; return to `from` |
| **Deep links** | `/runs/:pipeline_run_id`, `/strategies/:strategy_id`, `/trades?order_id=...`, `/trades?position_id=...`, `/portfolio?position_id=...` |
| **Realtime indicators** | Live fill updates via WebSocket |
| **Last-updated information** | Header and fills have timestamps |
| **Loading placement** | Header/fills skeleton |
| **Empty state** | No fills shown as pending/unfilled only if order data fresh |
| **Error state** | Order fetch error route-level; fills inline |
| **Stale/offline state** | Nonterminal order stale warning |
| **Paper/live labeling** | Show broker and linked strategy paper/live if available |
| **Responsive behavior** | Facts as definition list; fills as cards on mobile |

---

## 12. Trades

**Route:** `/trades`

**Intended user question:** What trades executed? Which order/position/ticker/side/time range do they belong to?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| Trades                                          [Filters: ▼]                |
+------------------------------------------------------------------------------+
| [Filter Bar]                                                               |
| Order: [______] Position: [______] Ticker: [______] Side: [All ▼] Date: ...|
+------------------------------------------------------------------------------+
| Note: Cannot combine Order ID and Position ID filters                       |
+------------------------------------------------------------------------------+
| Executed    | Ticker | Side | Qty   | Price   | Fee  | Order    | Position |
|-------------|--------|------|-------|---------|------|----------|----------|
| 14:30:02    | AAPL   | BUY  | 100   | $182.50 | $0.50| ORD-001  | POS-001  |
| 14:28:15    | TSLA   | SELL | 50    | $242.00 | $0.25| ORD-002  | POS-002  |
| 14:25:30    | NVDA   | BUY  | 25    | $515.00 | $0.13| ORD-003  | -        |
| 14:20:00    | MSFT   | BUY  | 75    | $380.00 | $0.38| ORD-004  | POS-003  |
+------------------------------------------------------------------------------+
| [1] [2] [3] ... [Next >]                                    Total: 567    |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| Trades        [Filters]   |
+---------------------------+
| +-----------------------+ |
| | 14:30 | AAPL | BUY   | |
| | 100 @ $182.50         | |
| | Order: ORD-001        | |
| +-----------------------+ |
| +-----------------------+ |
| | 14:28 | TSLA | SELL  | |
| | 50 @ $242.00          | |
| | Order: ORD-002        | |
| +-----------------------+ |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Page header with title |
| **Primary visual hierarchy** | Filter bar; trades table; pagination |
| **Filters** | order_id, position_id, ticker, side, start_date, end_date |
| **Tables/default columns** | executed_at, ticker, side, quantity, price, fee, order_id, position_id, external_id, open_close, premium, exit_reason |
| **Detail panels/tabs** | Selected trade raw drawer |
| **Sticky/persistent controls** | Filter bar sticky at top |
| **Primary actions** | Open related order or position context |
| **Secondary actions** | Copy trade ID; clear filters |
| **Deep links** | `/orders/:order_id`, `/trades?position_id=...`, `/portfolio?position_id=...` |
| **Realtime indicators** | Live trade feed via WebSocket |
| **Last-updated information** | Table timestamp |
| **Loading placement** | Table skeleton |
| **Empty state** | Empty table with clear filters |
| **Error state** | Table error panel |
| **Stale/offline state** | Recent trade data marked stale |
| **Paper/live labeling** | Not directly confirmed on trade; show linked broker/strategy context |
| **Responsive behavior** | Mobile cards prioritize executed/ticker/side/qty/price/order |

---

## 13. Risk Console

**Route:** `/risk`

**Intended user question:** Is risk normal/warning/breached? Is kill switch active? Which breakers are tripped? Which markets are stopped? Can controls be verified?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| RISK CONSOLE                                           Last updated: 14:32   |
+------------------------------------------------------------------------------+
| +-----------------------------------------------------------------------+   |
| | CRITICAL STATUS                                                      |   |
| | Risk Status: [NORMAL ●]  Kill Switch: [INACTIVE]  Breakers: [0]     |   |
| | [GLOBAL KILL SWITCH: ACTIVATE]                                       |   |
| +-----------------------------------------------------------------------+                       |
+------------------------------------------------------------------------------+
| [Tabs: Status | Breakers | Controls]                                       |
+------------------------------------------------------------------------------+
|                                                                              |
|  STATUS TAB:                                                                 |
|  +-----------------------------+  +------------------------------------+     |
|  | Risk Limits                 |  | Market Stops                       |     |
|  |                             |  |                                    |     |
|  | Max Position: 10%           |  | stock:  ACTIVE  [STOP]             |     |
|  | Max Exposure: 30%           |  | crypto: ACTIVE  [STOP]             |     |
|  | Max Concurrent: 20          |  | polymarket: ACTIVE  [STOP]         |     |
|  | Current Exposure: 23.4%     |  | kalshi:  ACTIVE  [STOP]            |     |
|  | Open Positions: 7           |  | options:  ACTIVE  [STOP]           |     |
|  +-----------------------------+  +------------------------------------+     |
|                                                                              |
|  +-----------------------------+  +------------------------------------+     |
|  | Circuit Breaker             |  | Kill Switch                        |     |
|  |                             |  |                                    |     |
|  | State: CLOSED               |  | Global: INACTIVE                   |     |
|  | Threshold: 5%               |  | Mechanisms: -                      |     |
|  | Cooldown: 15 min            |  |                                    |     |
|  +-----------------------------+  +------------------------------------+     |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px)

```
+---------------------------+
| RISK CONSOLE              |
+---------------------------+
| STATUS: NORMAL ●          |
| Kill: INACTIVE | 0 break  |
+---------------------------+
| [STOP TRADING]            |
+---------------------------+
| [Status ▼]                |
+---------------------------+
| Limits:                   |
| Max Pos: 10%              |
| Max Exp: 30%              |
| Current: 23.4%            |
+---------------------------+
| Markets:                  |
| stock: ACTIVE [STOP]      |
| crypto: ACTIVE [STOP]     |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Page header with title and timestamp |
| **Primary visual hierarchy** | Critical risk banner at top; kill switch panel; market stops panel; breaker table; risk limits/status detail |
| **Filters** | Tab-based: status, breakers, controls |
| **Tables/default columns** | Breakers: scope, reason, tripped_at, reset_at, action |
| **Detail panels/tabs** | Status, Breakers, Controls |
| **Sticky/persistent controls** | Critical risk banner and kill switch panel sticky at top; cannot be confused with passive status |
| **Primary actions** | Activate/deactivate kill switch; stop/resume market; reset breaker |
| **Secondary actions** | Inspect audit/events; copy scope; open affected strategy |
| **Deep links** | `/events?event_kind=circuit_breaker`, `/audit-log?event_type=kill_switch.activated`, `/strategies/:id` |
| **Realtime indicators** | Circuit breaker realtime via WebSocket |
| **Last-updated information** | Risk state has strict freshness requirement |
| **Loading placement** | Critical banner skeleton; controls disabled until status loads |
| **Empty state** | No breakers shown as "no tripped breakers" only when breaker query fresh |
| **Error state** | Inline per panel; global risk unknown if status fails |
| **Stale/offline state** | Controls blocked pending refetch; banner says stale/unknown |
| **Paper/live labeling** | Risk controls affect trading globally/market-wide; display broker paper/live context and warn if any live broker configured |
| **Responsive behavior** | Critical controls remain visible at top; tables become cards on mobile |

---

## 14. Global Activity Drawer

**Route:** Global overlay (shell-owned), optionally `activity=open`

**Intended user question:** What just happened? Which run/strategy/order/market does it relate to? Is realtime reliable?

### Layout (Desktop ~1440px) - Drawer Open

```
+------------------------------------------------------------------------------+
| [Shell header and content visible underneath]                                |
+                                                                              |
|                                                                              |
|  +------------------------------------------------------+-----------------+  |
|  | ACTIVITY DRAWER                            [X]      |                 |  |
|  +------------------------------------------------------+-----------------+  |
|  | Connection: ● Connected | Last event: 14:32:05      | [Clear] [Filter]|  |
|  +------------------------------------------------------+-----------------+  |
|  | [Filters: Type ▼] [Strategy ▼] [Run ▼] [Polymarket]                  |  |
|  +------------------------------------------------------+-----------------+  |
|  | Time     | Type              | Strategy/Run | Summary                 |  |
|  |----------|-------------------|--------------|-------------------------|  |
|  | 14:32:05 | order_filled      | AAPL/Momen.. | Position increased      |  |
|  | 14:30:45 | pipeline_health   | TSLA/MeanRev | Analysis complete       |  |
|  | 14:28:12 | error             | NVDA/Breako. | API rate limit hit      |  |
|  | 14:25:30 | signal            | AAPL/Momen.. | BUY signal generated    |  |
|  | 14:20:15 | circuit_breaker   | Global       | No trips                |  |
|  +------------------------------------------------------+-----------------+  |
|  | [View Persisted Events]                                 [Copy JSON]   |  |
|  +------------------------------------------------------+-----------------+  |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px) - Bottom Sheet

```
+---------------------------+
| Activity         [X]      |
+---------------------------+
| ● Connected | Last: 14:32|
+---------------------------+
| [Filters ▼]               |
+---------------------------+
| 14:32 order_filled AAPL   |
| +$250 position increased  |
+---------------------------+
| 14:30 pipeline_health TSLA|
| Analysis complete         |
+---------------------------+
| 14:28 error NVDA          |
| API rate limit            |
+---------------------------+
| [View All Events]         |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Connection state header; close button |
| **Primary visual hierarchy** | Connection state header; filters; event list/table; JSON payload inspector; linked entity actions |
| **Filters** | Event type, strategy_id, run_id, polymarket-specific |
| **Tables/default columns** | timestamp, type, strategy_id, run_id, summary from known envelope, link target |
| **Detail panels/tabs** | None |
| **Sticky/persistent controls** | Connection header sticky at top |
| **Primary actions** | Open linked entity; copy event JSON |
| **Secondary actions** | Clear buffer locally; reconnect; open persisted events |
| **Deep links** | `/runs/:id`, `/strategies/:id`, `/events?pipeline_run_id=...`, `/events?strategy_id=...` |
| **Realtime indicators** | Connection state; reconnect attempts; last event timestamp |
| **Last-updated information** | Drawer shows last event time and connection age |
| **Loading placement** | Empty buffer with connecting state |
| **Empty state** | "No events received in this session." |
| **Error state** | Persisted events error inline |
| **Stale/offline state** | Event gap marker after reconnect; visible pages refetch |
| **Paper/live labeling** | Not inferred from event payload; show linked strategy/broker context if loaded |
| **Responsive behavior** | Full-screen sheet on mobile; side drawer on desktop |

---

## 15. Confirmation Dialog

**Route:** Global dialog host (overlay)

**Intended user question:** What action is about to happen? What are the consequences?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| [Shell content visible underneath with overlay]                             |
|                                                                              |
|                                                                              |
|        +------------------------------------------------------------+        |
|        |                                                            |        |
|        |  CONFIRM ACTION                                            |        |
|        |                                                            |        |
|        |  You are about to PAUSE strategy:                          |        |
|        |                                                            |        |
|        |  [Momentum-Long (AAPL)]                                    |        |
|        |                                                            |        |
|        |  This will stop new pipeline runs for this strategy.       |        |
|        |  Active runs will continue to completion.                  |        |
|        |                                                            |        |
|        |  Mode: LIVE                                                |        |
|        |                                                            |        |
|        |  Are you sure you want to proceed?                         |        |
|        |                                                            |        |
|        |  +--------------------------------------------------------+ |        |
|        |  | [ ] I understand the consequences                     | |        |
|        |  +--------------------------------------------------------+ |        |
|        |                                                            |        |
|        |  [Cancel]                              [CONFIRM PAUSE]    |        |
|        |                                                            |        |
|        +------------------------------------------------------------+        |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Layout (Narrow Screen ~375px) - Bottom Sheet

```
+---------------------------+
| CONFIRM                   |
+---------------------------+
| Pause strategy:           |
| Momentum-Long (AAPL)      |
|                           |
| This will stop new runs.  |
| Active runs continue.     |
|                           |
| Mode: LIVE                |
|                           |
| [ ] I understand          |
+---------------------------+
| [Cancel]    [PAUSE]       |
+---------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Modal title |
| **Primary visual hierarchy** | Modal title; action/error summary; endpoint/entity context; safety-policy content slot; primary/secondary buttons; details accordion |
| **Filters** | None |
| **Tables/default columns** | None |
| **Detail panels/tabs** | Details accordion with raw ApiError where safe |
| **Sticky/persistent controls** | None |
| **Primary actions** | Confirm, retry safe query, cancel/close |
| **Secondary actions** | Copy error code/request context; open related entity if known |
| **Deep links** | Caller-provided links only |
| **Realtime indicators** | None |
| **Last-updated information** | If entity data stale, dialog blocks mutation and offers refetch |
| **Loading placement** | If caller is refetching pre-action state, show verifying state before confirm button |
| **Empty state** | Not applicable unless caller precheck finds entity missing |
| **Error state** | Display error message/code and endpoint context; preserve caller page |
| **Stale/offline state** | Confirm disabled until refetch or safety policy permits |
| **Paper/live labeling** | Safety-policy slot must display live/paper context when caller knows it |
| **Responsive behavior** | Centered modal desktop; bottom sheet mobile; max height with scroll |

---

## 16. Reason-Required Dialog

**Route:** Global dialog host (overlay) - specialized for kill switch activation

**Intended user question:** Why are you activating this critical control?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| [Shell content visible underneath with overlay]                             |
|                                                                              |
|                                                                              |
|        +------------------------------------------------------------+        |
|        |                                                            |        |
|        |  ACTIVATE GLOBAL KILL SWITCH                              |        |
|        |                                                            |        |
|        |  ⚠️ This is a critical action that will halt ALL trading  |        |
|        |                                                            |        |
|        |  Reason for activation:                                    |        |
|        |  +------------------------------------------------------+ |        |
|        |  |                                                      | |        |
|        |  | [________________________________________________]  | |        |
|        |  |                                                      | |        |
|        |  +------------------------------------------------------+ |        |
|        |                                                            |        |
|        |  Required. Provide a clear reason for audit purposes.     |        |
|        |                                                            |        |
|        |  [ ] I confirm this is an emergency stop                  |        |
|        |                                                            |        |
|        |  [Cancel]                              [ACTIVATE KILL]    |        |
|        |                                                            |        |
|        +------------------------------------------------------------+        |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Modal title with warning icon |
| **Primary visual hierarchy** | Warning banner; reason input field; confirmation checkbox; action buttons |
| **Filters** | None |
| **Tables/default columns** | None |
| **Detail panels/tabs** | None |
| **Sticky/persistent controls** | None |
| **Primary actions** | Activate kill switch with reason |
| **Secondary actions** | Cancel |
| **Deep links** | None |
| **Realtime indicators** | None |
| **Last-updated information** | None |
| **Loading placement** | None |
| **Empty state** | Not applicable |
| **Error state** | Validation error for missing reason |
| **Stale/offline state** | Disabled if offline |
| **Paper/live labeling** | Shown in warning context |
| **Responsive behavior** | Centered modal desktop; bottom sheet mobile |

---

## 17. Feature-Unavailable State

**Route:** Route-local panel for 501 responses

**Intended user question:** Why is this feature not available?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| [Standard page header and shell]                                            |
+------------------------------------------------------------------------------+
|                                                                              |
|                                                                              |
|        +------------------------------------------------------------+        |
|        |                                                            |        |
|        |  FEATURE NOT CONFIGURED                                    |        |
|        |                                                            |        |
|        |  The Decision Replay feature is not available on this     |        |
|        |  server.                                                   |        |
|        |                                                            |        |
|        |  Endpoint: /replay/decisions/{id}                         |        |
|        |  Source: Run Detail > Replay Tab                          |        |
|        |                                                            |        |
|        |  This may be because:                                     |        |
|        |  • The replay service is not configured                   |        |
|        |  • The LLM provider is not set up                         |        |
|        |  • The feature requires a different subscription          |        |
|        |                                                            |        |
|        |  The rest of this page remains usable.                    |        |
|        |                                                            |        |
|        |  [Return to Run Detail]                                   |        |
|        |                                                            |        |
|        +------------------------------------------------------------+        |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Standard page header |
| **Primary visual hierarchy** | Centered panel with icon, message, endpoint info, return action |
| **Filters** | None |
| **Tables/default columns** | None |
| **Detail panels/tabs** | None |
| **Sticky/persistent controls** | None |
| **Primary actions** | Return to source page |
| **Secondary actions** | None |
| **Deep links** | Source page via `from` parameter |
| **Realtime indicators** | None |
| **Last-updated information** | None |
| **Loading placement** | None |
| **Empty state** | Not applicable |
| **Error state** | Not applicable |
| **Stale/offline state** | Not applicable |
| **Paper/live labeling** | Not applicable |
| **Responsive behavior** | Centered panel; full width on mobile |

---

## 18. Partial-Service-Failure State

**Route:** Any page with partial widget failures

**Intended user question:** Can I still use this page? What's broken?

### Layout (Desktop ~1440px)

```
+------------------------------------------------------------------------------+
| [Standard page header]                                                       |
+------------------------------------------------------------------------------+
|                                                                              |
|  +-------------+  +-------------+  +-------------+  +-------------+         |
|  | WIDGET 1    |  | WIDGET 2    |  | WIDGET 3    |  | WIDGET 4    |         |
|  |             |  |             |  |             |  |             |         |
|  | Success     |  | Success     |  | Success     |  | Success     |         |
|  |             |  |             |  |             |  |             |         |
|  +-------------+  +-------------+  +-------------+  +-------------+         |
|                                                                              |
|  +-------------+  +-------------------------+  +-------------+               |
|  | WIDGET 5    |  | WIDGET 6                |  | WIDGET 7    |               |
|  |             |  |                         |  |             |               |
|  | Success     |  | ERROR                   |  | Success     |               |
|  |             |  |                         |  |             |               |
|  |             |  | Failed to load data     |  |             |               |
|  |             |  | Endpoint: /orders       |  |             |               |
|  |             |  |                         |  |             |               |
|  |             |  | [Retry]                 |  |             |               |
|  +-------------+  +-------------------------+  +-------------+               |
|                                                                              |
+------------------------------------------------------------------------------+
```

### Annotations

| Element | Specification |
|---------|---------------|
| **Header/navigation** | Standard page header |
| **Primary visual hierarchy** | Grid of widgets; failed widgets show inline error with retry |
| **Filters** | None |
| **Tables/default columns** | None |
| **Detail panels/tabs** | None |
| **Sticky/persistent controls** | None |
| **Primary actions** | Retry failed widget |
| **Secondary actions** | Continue using other widgets |
| **Deep links** | None |
| **Realtime indicators** | None |
| **Last-updated information** | Failed widgets show error timestamp |
| **Loading placement** | None |
| **Empty state** | Not applicable |
| **Error state** | Inline widget error with endpoint and retry button |
| **Stale/offline state** | May combine with partial failure |
| **Paper/live labeling** | Not affected |
| **Responsive behavior** | Widgets stack on mobile; error states remain visible |

---

## 19. Summary

### Files Created/Changed

| File Path | Description |
|-----------|-------------|
| `docs/frontend/wireframes/README.md` | This comprehensive wireframe document |

### Key Layout Decisions

1. **Cockpit attention order**: Unsafe/degraded state → Current exposure and active execution → Recent changes and alerts → Investigation paths
2. **Run detail tabs**: Summary → Timeline → Decisions → Snapshots → Events → Orders → Trades (Replay when permitted)
3. **Risk console critical controls**: Always visible at top, cannot be confused with passive status displays
4. **Responsive behavior**: Desktop grid → Tablet stacked two-column → Mobile cards with priority columns
5. **Dense tables**: Use horizontal scrolling, column priority, stacked summaries, or compact views instead of shrinking

### Major Compromise Decisions

1. **JSON config editor**: Strategy create/edit uses JSON editor rather than form fields for flexible config
2. **Row-focus for positions**: No single position detail endpoint; uses query-state row focus
3. **Activity drawer as overlay**: Global event feed is drawer rather than separate route for realtime context
4. **Confirmation dialogs via safety policy**: High-risk actions route through global dialog host

### Wireframes Blocked by Missing Information

1. **Decision DTO and prompt sensitivity**: Run detail decisions tab needs confirmed decision schema and prompt visibility policy
2. **Snapshot data_type schemas**: Run detail snapshots need confirmed data type vocabulary
3. **Risk cockpit DTO**: Cockpit and risk console need confirmed risk status/cockpit response shapes
4. **Allocator response shapes**: Portfolio allocator panels need confirmed diagnostics/summary/opportunities payloads
5. **WebSocket data payloads**: Activity drawer needs typed event data for summary column
6. **Report artifact schema**: Strategy reports need confirmed report type and artifact schemas
7. **Exact domain enum values**: Order side/type/status, option-specific fields need confirmed vocabulary

---

*Document created: 2026-06-19*
*Last updated: 2026-06-19*
