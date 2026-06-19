# Shared Event Markets Platform Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Extract Polymarket and Kalshi into a shared event-market lane so discovery, strategy metadata, risk/sizing, dashboards, and stock-only maintenance routing stop duplicating provider-specific assumptions.

**Architecture:** Keep providers provider-specific at the edges, but introduce a small `internal/eventmarkets` package for canonical candidate/proposal metadata, market capability checks, and event-market dashboard summaries. Do not add Kalshi live trading in this sprint. Do not rewrite Polymarket/Kalshi discovery wholesale; migrate contracts and routing incrementally behind tests.

**Tech Stack:** Go, PostgreSQL repositories already present, existing automation orchestrator, existing strategy repository, React/TypeScript dashboard.

---

## Sprint Boundaries

### In scope

- Shared event-market provider/capability helpers for `polymarket` and `kalshi`.
- Canonical event-market `discovery_meta` parser/validator.
- Shared paper-strategy creation/reuse helper for event markets.
- Explicit stock/OHLCV vs event-market routing guards.
- Shared event-market API summary for provider dashboards.
- Shared dashboard cards reused by `/polymarket` and `/kalshi` where practical.

### Out of scope

- Kalshi live order submission.
- ForecastEx/IBKR implementation.
- Kalshi WebSocket streaming.
- Full cross-provider database schema unification.
- Rewriting all Polymarket and Kalshi discovery into one generic orchestrator.

---

## File Structure

### New files

- `internal/eventmarkets/capabilities.go` — event-market predicates and stock/OHLCV capability helpers.
- `internal/eventmarkets/metadata.go` — canonical `discovery_meta` parser/validator.
- `internal/eventmarkets/metadata_test.go` — metadata parser tests for Polymarket and Kalshi shapes.
- `internal/eventmarkets/strategy.go` — shared event-market paper strategy builder/reuse key helpers.
- `internal/eventmarkets/strategy_test.go` — strategy helper tests.
- `internal/api/event_market_handlers.go` — optional shared read endpoint for compact cross-provider event-market status.
- `web/src/components/event-markets/event-market-summary-card.tsx` — reusable event-market summary card.
- `web/src/components/event-markets/event-market-summary-card.test.tsx` — card rendering tests.

### Modified files

- `internal/discovery/deploy.go` — delegate event-market reuse semantics to `internal/eventmarkets`.
- `internal/kalshidiscovery/orchestrator.go` — use shared strategy metadata/builder helpers.
- `internal/polymarketdiscovery/orchestrator.go` — normalize metadata through shared parser/contract where safe.
- `internal/automation/jobs_postmarket.go` — use shared capability helper for `strategy_resweep` skip rules.
- `cmd/tradingagent/prod_strategy_runner.go` — replace scattered event-market branching with capability helpers where behavior stays identical.
- `cmd/tradingagent/sizing_policy.go` — centralize event-market default sizing policy hooks.
- `internal/api/server.go` — register shared event-market endpoint if Task 5 is implemented.
- `web/src/pages/kalshi-page.tsx` — consume shared summary card.
- `web/src/pages/polymarket-page.tsx` — consume shared summary card where it does not disturb scanner UX.
- `web/src/lib/api/types.ts` — add shared event-market summary types.

---

## Task 1: Event Market Capabilities

**Files:**
- Create: `internal/eventmarkets/capabilities.go`
- Create or modify tests: `internal/eventmarkets/capabilities_test.go`
- Modify: `internal/automation/jobs_postmarket.go`

- [x] **Step 1: Write capability tests**

Create `internal/eventmarkets/capabilities_test.go`:

```go
package eventmarkets

import (
    "testing"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestIsEventMarket(t *testing.T) {
    for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi} {
        if !IsEventMarket(mt) {
            t.Fatalf("IsEventMarket(%q) = false, want true", mt)
        }
    }
    for _, mt := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto, domain.MarketTypeOptions, domain.MarketType("unknown")} {
        if IsEventMarket(mt) {
            t.Fatalf("IsEventMarket(%q) = true, want false", mt)
        }
    }
}

func TestSupportsOHLCVResweep(t *testing.T) {
    for _, mt := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto} {
        if !SupportsOHLCVResweep(mt) {
            t.Fatalf("SupportsOHLCVResweep(%q) = false, want true", mt)
        }
    }
    for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi, domain.MarketTypeOptions} {
        if SupportsOHLCVResweep(mt) {
            t.Fatalf("SupportsOHLCVResweep(%q) = true, want false", mt)
        }
    }
}
```

- [x] **Step 2: Run tests and verify failure**

```bash
rtk go test ./internal/eventmarkets -run 'IsEventMarket|SupportsOHLCVResweep' -count=1
```

Expected: FAIL because `internal/eventmarkets` package/functions do not exist.

- [x] **Step 3: Implement capabilities**

Create `internal/eventmarkets/capabilities.go`:

```go
package eventmarkets

import "github.com/PatrickFanella/get-rich-quick/internal/domain"

func IsEventMarket(marketType domain.MarketType) bool {
    switch marketType.Normalize() {
    case domain.MarketTypePolymarket, domain.MarketTypeKalshi:
        return true
    default:
        return false
    }
}

func SupportsOHLCVResweep(marketType domain.MarketType) bool {
    switch marketType.Normalize() {
    case domain.MarketTypeStock, domain.MarketTypeCrypto:
        return true
    default:
        return false
    }
}
```

- [x] **Step 4: Replace local resweep helper**

Modify `internal/automation/jobs_postmarket.go`:

```go
import "github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
```

Replace:

```go
if !supportsStrategyResweepOHLCV(strat.MarketType) {
```

with:

```go
if !eventmarkets.SupportsOHLCVResweep(strat.MarketType) {
```

Delete the local `supportsStrategyResweepOHLCV` function and update its tests to call `eventmarkets.SupportsOHLCVResweep`.

- [x] **Step 5: Verify**

```bash
rtk go test ./internal/eventmarkets ./internal/automation -run 'IsEventMarket|SupportsOHLCVResweep|StrategyResweep|Kalshi' -count=1
```

Expected: PASS.

---

## Task 2: Canonical Event Discovery Metadata

**Files:**
- Create: `internal/eventmarkets/metadata.go`
- Create: `internal/eventmarkets/metadata_test.go`
- Modify: `internal/kalshidiscovery/orchestrator.go`
- Modify: `internal/polymarketdiscovery/orchestrator.go` only where tests prove safe.

- [x] **Step 1: Write metadata parser tests**

Create `internal/eventmarkets/metadata_test.go`:

```go
package eventmarkets

import (
    "encoding/json"
    "testing"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestParseDiscoveryMetaKalshi(t *testing.T) {
    raw, _ := json.Marshal(map[string]any{"discovery_meta": map[string]any{
        "source": "kalshi_discovery", "market_ticker": "KXMENWORLDCUP-26-US", "direction": "YES", "conviction": 0.72,
    }})
    meta, err := ParseDiscoveryMeta(domain.MarketTypeKalshi, raw)
    if err != nil {
        t.Fatalf("ParseDiscoveryMeta() error = %v", err)
    }
    if meta.Provider != domain.MarketTypeKalshi || meta.MarketID != "KXMENWORLDCUP-26-US" || meta.Direction != "YES" {
        t.Fatalf("meta = %#v", meta)
    }
}

func TestParseDiscoveryMetaPolymarket(t *testing.T) {
    raw, _ := json.Marshal(map[string]any{"discovery_meta": map[string]any{
        "source": "polymarket_discovery", "market_slug": "will-example-happen", "direction": "NO", "conviction": 0.66,
    }})
    meta, err := ParseDiscoveryMeta(domain.MarketTypePolymarket, raw)
    if err != nil {
        t.Fatalf("ParseDiscoveryMeta() error = %v", err)
    }
    if meta.Provider != domain.MarketTypePolymarket || meta.MarketID != "will-example-happen" || meta.Direction != "NO" {
        t.Fatalf("meta = %#v", meta)
    }
}

func TestParseDiscoveryMetaFailsClosed(t *testing.T) {
    _, err := ParseDiscoveryMeta(domain.MarketTypeKalshi, json.RawMessage(`{"discovery_meta":{"direction":"MAYBE"}}`))
    if err == nil {
        t.Fatal("ParseDiscoveryMeta() error = nil, want validation failure")
    }
}
```

- [x] **Step 2: Implement parser**

Create `internal/eventmarkets/metadata.go`:

```go
package eventmarkets

import (
    "encoding/json"
    "fmt"
    "strings"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type DiscoveryMeta struct {
    Provider   domain.MarketType
    Source     string
    MarketID   string
    Direction  string
    Conviction float64
}

type configEnvelope struct {
    DiscoveryMeta map[string]any `json:"discovery_meta"`
}

func ParseDiscoveryMeta(marketType domain.MarketType, raw json.RawMessage) (DiscoveryMeta, error) {
    var env configEnvelope
    if err := json.Unmarshal(raw, &env); err != nil {
        return DiscoveryMeta{}, fmt.Errorf("eventmarkets: parse config: %w", err)
    }
    if len(env.DiscoveryMeta) == 0 {
        return DiscoveryMeta{}, fmt.Errorf("eventmarkets: missing discovery_meta")
    }

    meta := DiscoveryMeta{Provider: marketType.Normalize()}
    meta.Source = stringValue(env.DiscoveryMeta, "source")
    meta.Direction = strings.ToUpper(strings.TrimSpace(stringValue(env.DiscoveryMeta, "direction")))
    meta.Conviction = floatValue(env.DiscoveryMeta, "conviction")

    switch meta.Provider {
    case domain.MarketTypeKalshi:
        meta.MarketID = firstString(env.DiscoveryMeta, "market_ticker", "ticker")
    case domain.MarketTypePolymarket:
        meta.MarketID = firstString(env.DiscoveryMeta, "market_slug", "slug", "ticker")
    default:
        return DiscoveryMeta{}, fmt.Errorf("eventmarkets: unsupported market type %q", marketType)
    }
    if meta.MarketID == "" {
        return DiscoveryMeta{}, fmt.Errorf("eventmarkets: missing market id")
    }
    if meta.Direction != "YES" && meta.Direction != "NO" {
        return DiscoveryMeta{}, fmt.Errorf("eventmarkets: invalid direction %q", meta.Direction)
    }
    if meta.Conviction < 0 || meta.Conviction > 1 {
        return DiscoveryMeta{}, fmt.Errorf("eventmarkets: conviction %.4f outside [0,1]", meta.Conviction)
    }
    return meta, nil
}

func firstString(m map[string]any, keys ...string) string {
    for _, key := range keys {
        if v := stringValue(m, key); v != "" {
            return v
        }
    }
    return ""
}

func stringValue(m map[string]any, key string) string {
    if v, ok := m[key].(string); ok {
        return strings.TrimSpace(v)
    }
    return ""
}

func floatValue(m map[string]any, key string) float64 {
    switch v := m[key].(type) {
    case float64:
        return v
    case int:
        return float64(v)
    default:
        return 0
    }
}
```

- [x] **Step 3: Verify metadata tests**

```bash
rtk go test ./internal/eventmarkets -run 'DiscoveryMeta' -count=1
```

Expected: PASS.

---

## Task 3: Shared Event Paper Strategy Helpers

**Files:**
- Create: `internal/eventmarkets/strategy.go`
- Create: `internal/eventmarkets/strategy_test.go`
- Modify: `internal/discovery/deploy.go`
- Modify: `internal/kalshidiscovery/orchestrator.go`

- [x] **Step 1: Write helper tests**

Create `internal/eventmarkets/strategy_test.go`:

```go
package eventmarkets

import (
    "testing"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestReuseIgnoresNameForEventMarkets(t *testing.T) {
    for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi} {
        if !ReuseByTickerOnly(mt) {
            t.Fatalf("ReuseByTickerOnly(%q) = false, want true", mt)
        }
    }
}

func TestBuildPaperStrategyDefaults(t *testing.T) {
    s := BuildPaperStrategy(BuildPaperStrategyInput{
        Provider: domain.MarketTypeKalshi,
        Name: "auto: kalshi KX",
        Ticker: "KX",
        ScheduleCron: "0 */6 * * *",
        ConfigJSON: []byte(`{"discovery_meta":{"source":"kalshi_discovery","market_ticker":"KX","direction":"YES","conviction":0.7}}`),
    })
    if s.MarketType != domain.MarketTypeKalshi || !s.IsPaper || s.Status != domain.StrategyStatusActive || s.Ticker != "KX" {
        t.Fatalf("strategy = %#v", s)
    }
}
```

- [x] **Step 2: Implement helper**

Create `internal/eventmarkets/strategy.go`:

```go
package eventmarkets

import (
    "encoding/json"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type BuildPaperStrategyInput struct {
    Provider     domain.MarketType
    Name         string
    Ticker       string
    ScheduleCron string
    ConfigJSON   json.RawMessage
}

func ReuseByTickerOnly(marketType domain.MarketType) bool {
    return IsEventMarket(marketType)
}

func BuildPaperStrategy(input BuildPaperStrategyInput) domain.Strategy {
    return domain.Strategy{
        Name:         input.Name,
        Ticker:       input.Ticker,
        MarketType:   input.Provider.Normalize(),
        IsPaper:      true,
        Status:       domain.StrategyStatusActive,
        ScheduleCron: input.ScheduleCron,
        Config:       append(json.RawMessage(nil), input.ConfigJSON...),
    }
}
```

- [x] **Step 3: Use helper in shared deploy**

Modify `internal/discovery/deploy.go` to replace direct Polymarket/Kalshi checks:

```go
if eventmarkets.ReuseByTickerOnly(strategy.MarketType) {
    copy := existing[i]
    return &copy, nil
}
```

- [x] **Step 4: Use helper in Kalshi deploy path**

Modify `internal/kalshidiscovery/orchestrator.go` where `domain.Strategy{...}` is constructed:

```go
strategy := eventmarkets.BuildPaperStrategy(eventmarkets.BuildPaperStrategyInput{
    Provider:     domain.MarketTypeKalshi,
    Name:         name,
    Ticker:       candidate.Ticker,
    ScheduleCron: cfg.ScheduleCron,
    ConfigJSON:   rawConfig,
})
```

Use the actual local variable names from the file; keep the resulting fields identical to current behavior.

- [x] **Step 5: Verify**

```bash
rtk go test ./internal/eventmarkets ./internal/discovery ./internal/kalshidiscovery -run 'PaperStrategy|Reuse|Kalshi|Deploy' -count=1
```

Expected: PASS.

---

## Task 4: Event-Market Routing Audit and Guards

**Files:**
- Modify: `cmd/tradingagent/prod_strategy_runner.go`
- Modify: `cmd/tradingagent/sizing_policy.go`
- Modify: `cmd/tradingagent/prod_strategy_runner_test.go`
- Modify: `cmd/tradingagent/runtime_test.go`

- [x] **Step 1: Add a test that event-market strategies do not enter stock OHLCV analysis path**

In `cmd/tradingagent/prod_strategy_runner_test.go`, add a regression around the relevant strategy-runner helper if one already exists. If the helper is not directly testable, add a small unexported predicate in `prod_strategy_runner.go`:

```go
func usesStockOHLCVAnalysis(strategy domain.Strategy) bool {
    return !eventmarkets.IsEventMarket(strategy.MarketType) && strategy.MarketType.Normalize() != domain.MarketTypeOptions
}
```

Then test:

```go
func TestUsesStockOHLCVAnalysisSkipsEventMarkets(t *testing.T) {
    for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi} {
        if usesStockOHLCVAnalysis(domain.Strategy{MarketType: mt}) {
            t.Fatalf("usesStockOHLCVAnalysis(%q) = true, want false", mt)
        }
    }
}
```

- [ ] **Step 2: Replace scattered event-market checks with helper where safe**

In `cmd/tradingagent/prod_strategy_runner.go`, use:

```go
if eventmarkets.IsEventMarket(strategy.MarketType) {
```

only for shared event-market behavior. Keep provider-specific branches where Polymarket and Kalshi actually differ.

- [x] **Step 3: Verify execution tests**

```bash
rtk go test ./cmd/tradingagent -run 'Kalshi|Polymarket|OHLCV|Sizing|Paper' -count=1
```

Expected: PASS.

---

## Task 5: Shared Event-Market API Summary

**Files:**
- Create: `internal/api/event_market_handlers.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/kalshi_handlers.go`
- Modify: `internal/api/polymarket_handlers.go` only if duplication is obvious.
- Create: `internal/api/event_market_handlers_test.go`

- [x] **Step 1: Add a compact summary type**

Create `internal/api/event_market_handlers.go`:

```go
package api

type EventMarketProviderSummary struct {
    Provider         string `json:"provider"`
    WatchedMarkets   int    `json:"watched_markets"`
    ActivePaper      int    `json:"active_paper"`
    LastRunStatus    string `json:"last_run_status"`
    LiveTradingReady bool   `json:"live_trading_ready"`
}

type EventMarketsSummaryResponse struct {
    Providers []EventMarketProviderSummary `json:"providers"`
}
```

- [x] **Step 2: Add endpoint skeleton**

Register in `internal/api/server.go`:

```go
v1.Get("/event-markets/summary", s.handleGetEventMarketsSummary)
```

Implement `handleGetEventMarketsSummary` by composing existing repository counts used by `/api/v1/kalshi/summary` and the existing Polymarket job/status handlers. Return `live_trading_ready=false` for Kalshi.

- [x] **Step 3: Test endpoint shape**

Create `internal/api/event_market_handlers_test.go` with a direct handler test using stub repos. Assert response includes `kalshi` and `polymarket` entries when dependencies are configured, and that Kalshi `live_trading_ready=false`.

- [x] **Step 4: Verify API tests**

```bash
rtk go test ./internal/api -run 'EventMarket|Kalshi|Polymarket' -count=1
```

Expected: PASS.

---

## Task 6: Shared Dashboard Components

**Files:**
- Create: `web/src/components/event-markets/event-market-summary-card.tsx`
- Create: `web/src/components/event-markets/event-market-summary-card.test.tsx`
- Modify: `web/src/pages/kalshi-page.tsx`
- Modify: `web/src/pages/polymarket-page.tsx`
- Modify: `web/src/lib/api/types.ts`

- [x] **Step 1: Add reusable component test**

Create `web/src/components/event-markets/event-market-summary-card.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { EventMarketSummaryCard } from './event-market-summary-card'

describe('EventMarketSummaryCard', () => {
  it('renders paper-only provider status without live claims', () => {
    render(<EventMarketSummaryCard provider="Kalshi" watchedMarkets={15} activePaper={1} lastRunStatus="completed" liveTradingReady={false} />)

    expect(screen.getByText('Kalshi')).toBeInTheDocument()
    expect(screen.getByText('15')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText(/paper\/data only/i)).toBeInTheDocument()
  })
})
```

- [x] **Step 2: Implement component**

Create `web/src/components/event-markets/event-market-summary-card.tsx`:

```tsx
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { HudRow, StatusLed } from '@/components/ui/hud'

export function EventMarketSummaryCard({
  provider,
  watchedMarkets,
  activePaper,
  lastRunStatus,
  liveTradingReady,
}: {
  provider: string
  watchedMarkets: number
  activePaper: number
  lastRunStatus: string
  liveTradingReady: boolean
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <StatusLed state={liveTradingReady ? 'ok' : 'warn'} label={liveTradingReady ? 'live-ready' : 'paper/data only'} />
          {provider}
        </CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm">
        <HudRow label="Watched" value={String(watchedMarkets)} />
        <HudRow label="Active paper" value={String(activePaper)} />
        <HudRow label="Discovery" value={lastRunStatus} />
      </CardContent>
    </Card>
  )
}
```

- [x] **Step 3: Use on Kalshi page**

Modify `web/src/pages/kalshi-page.tsx` to render `EventMarketSummaryCard` in the overview section using existing summary query values:

```tsx
<EventMarketSummaryCard
  provider="Kalshi"
  watchedMarkets={watchedMarkets.length}
  activePaper={activePaper}
  lastRunStatus={discoveryStatus}
  liveTradingReady={false}
/>
```

- [x] **Step 4: Verify frontend**

```bash
cd web && npm test -- --run src/components/event-markets/event-market-summary-card.test.tsx src/pages/kalshi-page.test.tsx src/pages/polymarket-page.test.tsx
cd web && npm run build
```

Expected: tests and build pass.

---

## Task 7: Sprint B Validation and Deploy Readiness

**Files:**
- Modify: `docs/runbooks/kalshi-paper-data.md`
- Modify or create: `docs/runbooks/event-markets-paper-data.md`
- Modify: this plan file to check completed steps.

- [x] **Step 1: Run backend validation**

```bash
rtk go test ./internal/eventmarkets ./internal/discovery ./internal/polymarketdiscovery ./internal/kalshidiscovery ./internal/automation ./internal/api ./cmd/tradingagent ./internal/risk ./internal/position -count=1
```

Expected: PASS.

- [x] **Step 2: Run frontend validation**

```bash
cd web && npm test -- --run src/components/event-markets/event-market-summary-card.test.tsx src/pages/kalshi-page.test.tsx src/pages/polymarket-page.test.tsx src/components/layout/app-shell.test.tsx
cd web && npm run build
```

Expected: PASS. Large chunk warning is acceptable if unchanged.

- [x] **Step 3: Review**

Request final review focused on:

- No Kalshi live order submission.
- Kalshi and Polymarket still route through their native/paper execution paths.
- OHLCV-based jobs still skip event markets.
- Shared metadata parser accepts current Kalshi and Polymarket configs.
- Dashboard copy remains paper/data-first for Kalshi.
- No unnecessary full abstraction rewrite occurred.

- [ ] **Step 4: Deploy checklist**

No migration is expected for this sprint. If that remains true, deploy with app/web rebuild only:

```bash
docker compose --project-name augr -f docker-compose.nuc.yml up -d --build app web
```

Verify:

```bash
curl -sf http://10.0.0.56:3030/healthz
curl -sf http://10.0.0.56:3029/kalshi
curl -sf http://10.0.0.56:3029/polymarket
```

---

## Acceptance Criteria

- Event-market predicates live in `internal/eventmarkets`, not scattered local helpers.
- `strategy_resweep` and stock-only OHLCV paths skip Kalshi and Polymarket by shared capability helper.
- Kalshi and Polymarket discovery metadata can be parsed through one shared contract.
- Existing Sprint A Kalshi discovery continues creating/reusing active paper strategies.
- Polymarket native routing and dashboard tests still pass.
- Kalshi dashboard still explicitly says paper/data only.
- No live Kalshi order path is introduced.
