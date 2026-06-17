# Kalshi Data + Discovery Sprint Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Turn the Kalshi paper/data foundation into useful Kalshi market ingestion, discovery, and active paper strategy creation while keeping Polymarket working.

**Architecture:** Keep Kalshi additive and provider-specific for Sprint A. Build `internal/kalshidiscovery` beside `internal/polymarketdiscovery`, persist Kalshi market/watch/discovery state in Kalshi-specific tables, then expose the results on the existing Event Markets dashboard. Do not implement Kalshi live order submission in this sprint.

**Tech Stack:** Go, PostgreSQL migrations, Kalshi Trade API v2 demo/prod read endpoints, existing automation orchestrator, existing strategy repository, existing React dashboard.

---

## Sprint Boundaries

### In scope

- Fetch real Kalshi event/market candidates from the Kalshi API.
- Persist watched Kalshi markets and latest market snapshots.
- Add a scheduled Kalshi market sync/discovery automation job.
- Screen Kalshi markets into paper strategy candidates.
- Deploy active paper Kalshi strategies with native `discovery_meta`.
- Update `/kalshi` dashboard to show real data and job state.

### Out of scope

- Kalshi live order submission.
- Kalshi WebSocket streaming.
- Cross-provider discovery abstraction.
- ForecastEx/IBKR integration.
- Removing or restructuring Polymarket discovery tables.

### Follow-up tracks after Sprint A

- **Sprint B — Shared Event Markets Platform:** extract provider registry, shared discovery interfaces, provider-neutral strategy metadata, and shared dashboard sections once Kalshi discovery proves the second provider path.
- **Sprint C — Kalshi Live Readiness:** implement Kalshi live broker/order lifecycle, reconciliation, runbook, and explicit live gates. Paper remains default.

---

## File Structure

### New files

- `migrations/000051_kalshi_discovery_tables.up.sql` — Kalshi watched markets, market snapshots, and discovery run tables.
- `migrations/000051_kalshi_discovery_tables.down.sql` — Drop Kalshi discovery tables.
- `internal/domain/kalshi_market.go` — Kalshi watched market, snapshot, and discovery domain structs.
- `internal/repository/postgres/kalshi_market.go` — Postgres repositories for Kalshi watched markets/snapshots/discovery runs.
- `internal/kalshidiscovery/client.go` — Kalshi market listing client wrappers over `internal/data/kalshi.Client`.
- `internal/kalshidiscovery/screener.go` — Deterministic market screening rules.
- `internal/kalshidiscovery/generator.go` — Proposal contract and optional LLM proposal generation.
- `internal/kalshidiscovery/orchestrator.go` — Fetch, screen, propose, and deploy active paper strategies.
- `internal/kalshidiscovery/*_test.go` — Unit coverage for mapping, screening, validation, deployment.
- `internal/automation/jobs_kalshi_discovery.go` — Scheduled Kalshi discovery/sync job.
- `internal/api/kalshi_handlers.go` — API endpoints for Kalshi page data.

### Modified files

- `internal/repository/interfaces.go` — Add Kalshi repository interfaces.
- `internal/repository/postgres/schema_version.go` — Bump required schema to `51`.
- `cmd/tradingagent/runtime.go` — Wire Kalshi repos and automation job.
- `internal/api/server.go` — Register Kalshi handlers.
- `web/src/lib/api/client.ts` — Add Kalshi API calls.
- `web/src/lib/api/types.ts` — Add Kalshi response types.
- `web/src/pages/kalshi-page.tsx` — Replace static placeholders with real Kalshi market/discovery/job data.
- `web/src/pages/kalshi-page.test.tsx` — Add real-data rendering tests.
- `docs/runbooks/kalshi-paper-data.md` — Add operator guidance for sync/discovery.

---

## Task 1: Kalshi Persistence Schema

**Files:**
- Create: `migrations/000051_kalshi_discovery_tables.up.sql`
- Create: `migrations/000051_kalshi_discovery_tables.down.sql`
- Create: `internal/domain/kalshi_market.go`
- Modify: `internal/repository/interfaces.go`
- Create: `internal/repository/postgres/kalshi_market.go`
- Modify: `internal/repository/postgres/schema_version.go`

- [ ] **Step 1: Add migration 51**

Create Kalshi-specific persistence without changing Polymarket tables:

```sql
CREATE TABLE IF NOT EXISTS kalshi_watched_markets (
  ticker TEXT PRIMARY KEY,
  event_ticker TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  close_time TIMESTAMPTZ,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS kalshi_market_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  ticker TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  yes_bid DOUBLE PRECISION NOT NULL DEFAULT 0,
  yes_ask DOUBLE PRECISION NOT NULL DEFAULT 0,
  no_bid DOUBLE PRECISION NOT NULL DEFAULT 0,
  no_ask DOUBLE PRECISION NOT NULL DEFAULT 0,
  volume DOUBLE PRECISION NOT NULL DEFAULT 0,
  open_interest DOUBLE PRECISION NOT NULL DEFAULT 0,
  close_time TIMESTAMPTZ,
  raw JSONB NOT NULL DEFAULT '{}'::jsonb,
  captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kalshi_market_snapshots_ticker_captured
  ON kalshi_market_snapshots (ticker, captured_at DESC);

CREATE TABLE IF NOT EXISTS kalshi_discovery_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  status TEXT NOT NULL DEFAULT 'running',
  fetched INTEGER NOT NULL DEFAULT 0,
  screened INTEGER NOT NULL DEFAULT 0,
  proposed INTEGER NOT NULL DEFAULT 0,
  deployed INTEGER NOT NULL DEFAULT 0,
  errors JSONB NOT NULL DEFAULT '[]'::jsonb,
  summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- [ ] **Step 2: Add repository tests**

Run:

```bash
rtk go test ./internal/repository/postgres -run 'Kalshi|Migration|Schema' -count=1
```

Expected: Kalshi tables can insert/list watched markets, latest snapshots, and discovery runs.

---

## Task 2: Kalshi Market Catalog Sync

**Files:**
- Modify: `internal/data/kalshi/client.go`
- Create: `internal/kalshidiscovery/client.go`
- Create: `internal/kalshidiscovery/client_test.go`
- Modify: `internal/repository/postgres/kalshi_market.go`

- [ ] **Step 1: Add market listing client**

Implement a read-only fetcher for Kalshi markets:

```go
type MarketCandidate struct {
    Ticker       string
    EventTicker  string
    Title        string
    Category     string
    Status       string
    YesBid       float64
    YesAsk       float64
    NoBid        float64
    NoAsk        float64
    Volume       float64
    OpenInterest float64
    CloseTime    time.Time
    Raw          json.RawMessage
}
```

Use public `GET /markets` with conservative query defaults: active/open markets only where available, bounded limit, no auth requirement for paper/data.

- [ ] **Step 2: Persist latest catalog snapshot**

Store fetched candidates into `kalshi_market_snapshots`; upsert selected candidates into `kalshi_watched_markets` only when selected by the screener.

- [ ] **Step 3: Verify mapping**

Run:

```bash
rtk go test ./internal/kalshidiscovery ./internal/data/kalshi -run 'Market|Catalog|Client' -count=1
```

---

## Task 3: Kalshi Screener and Proposal Contract

**Files:**
- Create: `internal/kalshidiscovery/screener.go`
- Create: `internal/kalshidiscovery/generator.go`
- Create: `internal/kalshidiscovery/screener_test.go`
- Create: `internal/kalshidiscovery/generator_test.go`

- [ ] **Step 1: Add deterministic screener**

Screen for paper candidates using provider-neutral, explainable filters:

```go
type ScreenerConfig struct {
    MaxCandidates int
    MinVolume float64
    MinOpenInterest float64
    MaxSpreadPct float64
    MinDaysToClose float64
    Categories []string
}
```

Reject closed/settled markets, missing YES/NO book, excessive spread, low volume/open interest, and markets too close to expiration.

- [ ] **Step 2: Add proposal validation**

Mirror the Polymarket discovery metadata contract where it fits:

```json
{
  "template": "microstructure",
  "direction": "YES",
  "conviction": 0.72,
  "entry_price_max": 0.62,
  "max_spread_pct": 8,
  "min_liquidity": 1000,
  "source_references": ["kalshi_market:<ticker>"],
  "stop_policy": "hold until invalidation or close window",
  "target_policy": "paper-only target based on probability repricing"
}
```

- [ ] **Step 3: Verify validation fails closed**

Run:

```bash
rtk go test ./internal/kalshidiscovery -run 'Screener|Proposal|Validation' -count=1
```

---

## Task 4: Kalshi Discovery Orchestrator and Paper Deployment

**Files:**
- Create: `internal/kalshidiscovery/orchestrator.go`
- Create: `internal/kalshidiscovery/orchestrator_test.go`
- Modify: `internal/discovery/deploy.go` only if shared strategy reuse needs market-type-safe behavior.

- [ ] **Step 1: Build orchestrator**

Implement:

```go
func Run(ctx context.Context, cfg Config, deps Deps) (*Result, error)
func DeployStrategy(ctx context.Context, cfg Config, deps Deps, candidate MarketCandidate, proposal Proposal) (DeployedStrategy, error)
```

Deploy strategies as:

```go
domain.Strategy{
    MarketType: domain.MarketTypeKalshi,
    IsPaper: true,
    Status: domain.StrategyStatusActive,
    ScheduleCron: cfg.ScheduleCron, // default "0 */6 * * *"
    Ticker: candidate.Ticker,
}
```

The `Config` JSON must include `discovery_meta.source = "kalshi_discovery"`, `market_ticker`, template, direction, conviction, max entry price, and risk metadata.

- [ ] **Step 2: Protect against duplicates**

Reuse existing paper strategies for the same `market_type=kalshi` + `ticker` instead of creating duplicates.

- [ ] **Step 3: Verify deployed strategies route to native Kalshi execution**

Run:

```bash
rtk go test ./internal/kalshidiscovery ./cmd/tradingagent -run 'Kalshi|Deploy|Native' -count=1
```

---

## Task 5: Automation Job and Runtime Wiring

**Files:**
- Create: `internal/automation/jobs_kalshi_discovery.go`
- Modify: `cmd/tradingagent/runtime.go`
- Modify: `internal/automation/orchestrator_test.go`

- [ ] **Step 1: Add scheduled job**

Add `kalshi_discovery` as an automation job with conservative cadence, e.g. hourly:

```go
Name: "kalshi_discovery"
Cron: "15 * * * *"
```

The job must be paper/data-only and must not submit live orders.

- [ ] **Step 2: Wire dependencies**

Use existing config defaults:

```env
KALSHI_DEMO=true
KALSHI_API_BASE_URL=https://external-api.demo.kalshi.co/trade-api/v2
```

Credentials remain optional for this sprint.

- [ ] **Step 3: Verify job registration and dry run**

Run:

```bash
rtk go test ./internal/automation ./cmd/tradingagent -run 'Kalshi|Automation|Discovery' -count=1
```

---

## Task 6: Kalshi API and Dashboard Real Data

**Files:**
- Create: `internal/api/kalshi_handlers.go`
- Modify: `internal/api/server.go`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/types.ts`
- Modify: `web/src/pages/kalshi-page.tsx`
- Modify: `web/src/pages/kalshi-page.test.tsx`

- [ ] **Step 1: Add API endpoint**

Expose a compact page payload:

```http
GET /api/kalshi/summary
```

Response shape:

```json
{
  "watched_markets": [],
  "latest_snapshots": [],
  "discovery": { "last_run": null, "status": "not_started" },
  "strategies": { "active_paper": 0 }
}
```

- [ ] **Step 2: Render real dashboard sections**

Update `/kalshi` to show:

- latest discovery run status
- watched markets
- candidate snapshots
- active paper strategies
- setup/runbook links

- [ ] **Step 3: Verify frontend**

Run:

```bash
cd web && npm test -- --run src/pages/kalshi-page.test.tsx src/components/layout/app-shell.test.tsx
cd web && npm run build
```

---

## Task 7: Sprint A Validation and Deploy Readiness

**Files:**
- Modify: `docs/runbooks/kalshi-paper-data.md`
- Modify: this plan file

- [ ] **Step 1: Run backend validation**

```bash
rtk go test ./internal/domain ./internal/config ./internal/data ./internal/data/kalshi ./internal/kalshidiscovery ./internal/execution/prediction ./internal/execution/kalshi ./cmd/tradingagent ./internal/scheduler ./internal/repository/postgres ./internal/api ./internal/automation -count=1
```

- [ ] **Step 2: Run frontend validation**

```bash
cd web && npm test -- --run src/pages/kalshi-page.test.tsx src/pages/polymarket-page.test.tsx src/components/layout/app-shell.test.tsx
cd web && npm run build
```

- [ ] **Step 3: Review**

Request final review focused on:

- Kalshi discovery cannot submit live orders.
- Kalshi paper strategies are active by default.
- Polymarket still routes normally.
- Dashboard does not overclaim live support.
- Migration and repository code are idempotent.

- [ ] **Step 4: Deploy checklist**

After commit/push:

```bash
docker compose --project-name augr -f docker-compose.nuc.yml --profile tools run --rm migrate
docker compose --project-name augr -f docker-compose.nuc.yml up -d --build app web
```

Verify:

```bash
curl -sf http://10.0.0.56:3030/healthz
curl -sf http://10.0.0.56:3029/kalshi
```

---

## Sprint B Preview: Shared Event Markets Platform

After Sprint A proves Kalshi discovery, plan a dedicated extraction sprint:

- provider-neutral event-market candidate model
- provider-neutral discovery interfaces
- shared `discovery_meta` schema
- shared event-market dashboard cards
- provider registry for Polymarket/Kalshi/ForecastEx
- provider-neutral risk/sizing policy with provider overrides

Do not start Sprint B until Kalshi discovery has run at least one clean paper cycle.

---

## Sprint C Preview: Kalshi Live Readiness

After Sprint B, plan live readiness separately:

- Kalshi live broker implementation
- order submit/cancel/status mapping
- portfolio reconciliation
- live readiness helper/runbook
- explicit `LIVE_TRADING_ALLOWED_BROKERS=kalshi` gate
- no live defaults; paper remains default

Do not start Sprint C until paper discovery and native execution produce stable decisions.
