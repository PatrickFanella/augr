# Kalshi Live Readiness Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make Kalshi live execution technically ready behind explicit live-trading gates while keeping paper trading as the default and preventing accidental live order submission.

**Architecture:** Replace the disabled Kalshi broker stub with a real broker adapter that maps the shared `execution.Broker` interface to Kalshi order/account endpoints, but only route to it when `EnableLiveTrading`, strategy allowlist, and broker allowlist all pass. Keep the deterministic native Kalshi executor and paper discovery flow intact. Add readiness docs/API checks before any operator can intentionally flip a Kalshi strategy from paper to live.

**Tech Stack:** Go, existing `internal/data/kalshi.Client`, existing `execution.OrderManager`/`LiveGateConfig`, existing strategy runner broker routing, React/Vite dashboard only if readiness status needs UI exposure.

---

## Sprint Boundaries

### In scope

- Kalshi live broker adapter for order submit/cancel/status, positions, and account balance.
- Runtime broker routing for non-paper Kalshi strategies.
- Strict live gate coverage: global flag, strategy UUID allowlist, broker allowlist `kalshi`, and credentials.
- Reconciliation/readiness checks that do not submit orders.
- Operator runbook for dry-run, readiness verification, rollback, and first live order procedure.

### Out of scope

- Enabling any existing strategy for live trading by default.
- Changing `kalshi_discovery` to create live strategies.
- Automated live promotion of paper strategies.
- New Kalshi WebSocket streaming.
- Full exchange-specific portfolio accounting beyond enough to reconcile basic positions/balance.

---

## File Structure

### New files

- `internal/execution/kalshi/client.go` — narrow client interface used by the live broker so tests can stub HTTP/API calls.
- `internal/execution/kalshi/order_mapping.go` — shared order/request/status mapping helpers.
- `internal/execution/kalshi/order_mapping_test.go` — mapping tests.
- `internal/execution/kalshi/reconciler.go` — broker/local reconciliation helper for Kalshi positions.
- `internal/execution/kalshi/reconciler_test.go` — reconciliation tests.
- `internal/api/kalshi_readiness_handlers.go` — read-only readiness endpoint if API exposure is desired.
- `docs/runbooks/kalshi-live-readiness.md` — operator checklist and rollback procedure.

### Modified files

- `internal/execution/kalshi/broker.go` — replace disabled stub internals with gated live adapter behavior.
- `internal/execution/kalshi/broker_test.go` — live broker mapping/error/read method tests.
- `cmd/tradingagent/prod_strategy_runner.go` — permit non-paper Kalshi only through existing live gate and `kalshi` broker routing.
- `cmd/tradingagent/runtime.go` — wire Kalshi broker/client dependencies if needed.
- `cmd/tradingagent/runtime_test.go` — prove Kalshi live requires all gates and falls back to paper otherwise.
- `internal/config/config.go` and `internal/config/config_test.go` — only if Kalshi-specific broker env vars are missing from config.
- `docs/runbooks/kalshi-paper-data.md` — cross-link to live readiness, keep paper defaults explicit.

---

## Task 1: Define Kalshi Broker Client Boundary

**Files:**
- Create: `internal/execution/kalshi/client.go`
- Modify: `internal/execution/kalshi/broker.go`
- Test: `internal/execution/kalshi/broker_test.go`

- [x] **Step 1: Write interface compile test**

Add to `internal/execution/kalshi/broker_test.go`:

```go
func TestBrokerSatisfiesExecutionBroker(t *testing.T) {
    t.Parallel()

    var _ execution.Broker = NewBroker(nil)
}
```

If `execution` is not imported, add:

```go
"github.com/PatrickFanella/get-rich-quick/internal/execution"
```

- [x] **Step 2: Create live client interface**

Create `internal/execution/kalshi/client.go`:

```go
package kalshi

import "context"

type LiveClient interface {
    CreateOrder(ctx context.Context, req CreateOrderRequest) (CreateOrderResponse, error)
    CancelOrder(ctx context.Context, orderID string) error
    GetOrder(ctx context.Context, orderID string) (OrderResponse, error)
    ListPositions(ctx context.Context) ([]PositionResponse, error)
    GetBalance(ctx context.Context) (BalanceResponse, error)
}

type CreateOrderRequest struct {
    Ticker      string
    Side        string
    Action      string
    Count       int64
    Type        string
    YesPrice    *int64
    NoPrice     *int64
    ClientOrderID string
}

type CreateOrderResponse struct { OrderID string }

type OrderResponse struct {
    OrderID string
    Status  string
}

type PositionResponse struct {
    Ticker string
    Side   string
    Count  int64
    ValueCents int64
}

type BalanceResponse struct {
    CashCents        int64
    BuyingPowerCents int64
    EquityCents      int64
}
```

- [x] **Step 3: Change constructor without enabling live behavior yet**

Modify `internal/execution/kalshi/broker.go`:

```go
type Broker struct { client LiveClient }

func NewBroker(client LiveClient) *Broker { return &Broker{client: client} }
```

Keep existing methods disabled in this task except nil receiver/client errors. Update existing tests from `NewBroker()` to `NewBroker(nil)`.

- [x] **Step 4: Verify**

```bash
rtk go test ./internal/execution/kalshi -run 'Broker' -count=1
```

Expected: PASS.

---

## Task 2: Map Shared Orders to Kalshi Orders

**Files:**
- Create: `internal/execution/kalshi/order_mapping.go`
- Create: `internal/execution/kalshi/order_mapping_test.go`

- [x] **Step 1: Write mapping tests**

Create `internal/execution/kalshi/order_mapping_test.go`:

```go
package kalshi

import (
    "testing"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestMapCreateOrderRequestYesLimitBuy(t *testing.T) {
    price := 0.42
    req, err := mapCreateOrderRequest(&domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, Type: domain.OrderTypeLimit, Quantity: 3, LimitPrice: &price})
    if err != nil { t.Fatalf("mapCreateOrderRequest() error = %v", err) }
    if req.Ticker != "KX-EXAMPLE" || req.Action != "buy" || req.Side != "yes" || req.Count != 3 || req.YesPrice == nil || *req.YesPrice != 42 {
        t.Fatalf("request = %#v", req)
    }
}

func TestMapCreateOrderRequestRejectsMissingLimitPrice(t *testing.T) {
    _, err := mapCreateOrderRequest(&domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, Type: domain.OrderTypeLimit, Quantity: 1})
    if err == nil { t.Fatal("mapCreateOrderRequest() error = nil, want error") }
}

func TestMapOrderStatus(t *testing.T) {
    cases := map[string]domain.OrderStatus{"resting": domain.OrderStatusOpen, "executed": domain.OrderStatusFilled, "canceled": domain.OrderStatusCancelled, "rejected": domain.OrderStatusRejected}
    for raw, want := range cases {
        got, err := mapOrderStatus(raw)
        if err != nil || got != want { t.Fatalf("mapOrderStatus(%q) = %q, %v; want %q", raw, got, err, want) }
    }
}
```

- [x] **Step 2: Implement mapping helpers**

Create `internal/execution/kalshi/order_mapping.go` with:

```go
package kalshi

import (
    "fmt"
    "math"
    "strings"

    "github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func mapCreateOrderRequest(order *domain.Order) (CreateOrderRequest, error) {
    if order == nil { return CreateOrderRequest{}, fmt.Errorf("kalshi: order is required") }
    if strings.TrimSpace(order.Ticker) == "" { return CreateOrderRequest{}, fmt.Errorf("kalshi: ticker is required") }
    if order.Quantity <= 0 { return CreateOrderRequest{}, fmt.Errorf("kalshi: quantity must be positive") }
    req := CreateOrderRequest{Ticker: strings.TrimSpace(order.Ticker), Count: int64(math.Round(order.Quantity)), Side: "yes", ClientOrderID: order.ID.String()}
    switch order.Side {
    case domain.OrderSideBuy: req.Action = "buy"
    case domain.OrderSideSell: req.Action = "sell"
    default: return CreateOrderRequest{}, fmt.Errorf("kalshi: unsupported order side %q", order.Side)
    }
    switch order.Type {
    case domain.OrderTypeLimit:
        if order.LimitPrice == nil { return CreateOrderRequest{}, fmt.Errorf("kalshi: limit price is required") }
        cents := int64(math.Round(*order.LimitPrice * 100))
        if cents <= 0 || cents >= 100 { return CreateOrderRequest{}, fmt.Errorf("kalshi: limit price must be between 0 and 1 exclusive") }
        req.Type = "limit"
        req.YesPrice = &cents
    case domain.OrderTypeMarket:
        req.Type = "market"
    default:
        return CreateOrderRequest{}, fmt.Errorf("kalshi: unsupported order type %q", order.Type)
    }
    return req, nil
}

func mapOrderStatus(raw string) (domain.OrderStatus, error) {
    switch strings.ToLower(strings.TrimSpace(raw)) {
    case "resting", "open": return domain.OrderStatusOpen, nil
    case "executed", "filled": return domain.OrderStatusFilled, nil
    case "canceled", "cancelled": return domain.OrderStatusCancelled, nil
    case "rejected": return domain.OrderStatusRejected, nil
    default: return "", fmt.Errorf("kalshi: unsupported order status %q", raw)
    }
}
```

- [x] **Step 3: Verify**

```bash
rtk go test ./internal/execution/kalshi -run 'Map|OrderStatus|CreateOrderRequest' -count=1
```

Expected: PASS.

---

## Task 3: Implement Live Broker Methods Behind Client

**Files:**
- Modify: `internal/execution/kalshi/broker.go`
- Modify: `internal/execution/kalshi/broker_test.go`

- [x] **Step 1: Add fake client tests**

Add a fake client to `broker_test.go` and tests:

```go
type fakeLiveClient struct {
    created CreateOrderRequest
    orderID string
    orderStatus string
    positions []PositionResponse
    balance BalanceResponse
}

func (f *fakeLiveClient) CreateOrder(_ context.Context, req CreateOrderRequest) (CreateOrderResponse, error) { f.created = req; return CreateOrderResponse{OrderID: f.orderID}, nil }
func (f *fakeLiveClient) CancelOrder(context.Context, string) error { return nil }
func (f *fakeLiveClient) GetOrder(context.Context, string) (OrderResponse, error) { return OrderResponse{OrderID: f.orderID, Status: f.orderStatus}, nil }
func (f *fakeLiveClient) ListPositions(context.Context) ([]PositionResponse, error) { return f.positions, nil }
func (f *fakeLiveClient) GetBalance(context.Context) (BalanceResponse, error) { return f.balance, nil }
```

Then test `SubmitOrder`, `GetOrderStatus`, `GetPositions`, and `GetAccountBalance` map responses correctly.

- [x] **Step 2: Implement broker methods**

In `broker.go`, replace disabled errors with client calls when `b.client != nil`. Preserve nil client errors:

```go
if b.client == nil { return "", errors.New("kalshi: live client is required") }
```

Use `mapCreateOrderRequest`, `mapOrderStatus`, and convert cents to dollars for balances/positions.

- [x] **Step 3: Verify**

```bash
rtk go test ./internal/execution/kalshi -run 'Broker|Map' -count=1
```

Expected: PASS.

---

## Task 4: Wire Kalshi Live Broker Routing and Gates

**Files:**
- Modify: `cmd/tradingagent/prod_strategy_runner.go`
- Modify: `cmd/tradingagent/runtime.go` if a live client dependency must be constructed there.
- Modify: `cmd/tradingagent/runtime_test.go`

- [x] **Step 1: Add runtime tests for strict live gating**

Add tests proving:

1. Non-paper Kalshi with `EnableLiveTrading=false` fails before broker construction.
2. Non-paper Kalshi with `EnableLiveTrading=true` but missing `LIVE_TRADING_ALLOWED_BROKERS=kalshi` does not submit.
3. Non-paper Kalshi with all gates but missing Kalshi credentials fails clearly.
4. Paper Kalshi still returns local paper broker.

Use existing `newBrokerForStrategy` and `liveGateForStrategy` tests in `cmd/tradingagent/runtime_test.go` as the pattern.

- [x] **Step 2: Add Kalshi live branch to broker routing**

In `newBrokerForStrategy`, add:

```go
case domain.MarketTypeKalshi:
    kc := r.cfg.Brokers.Kalshi
    if strings.TrimSpace(kc.APIKey) == "" || strings.TrimSpace(kc.APISecret) == "" {
        return nil, "", errors.New("kalshi credentials are required for live kalshi trading")
    }
    return kalshiexecution.NewBroker(r.kalshiLiveClient), "kalshi", nil
```

Use the actual config/client fields that exist after inspecting `config.BrokerConfig` and Kalshi data client. No reusable live client exists yet, so routing remains intentionally blocked with a clear `kalshi live client is not initialised` error until Task 5 wires one in.

- [x] **Step 3: Remove unconditional live rejection from `runKalshiNative` only after broker routing/gates are covered**

Replace:

```go
if !strategy.IsPaper { return nil, errors.New("kalshi live execution is disabled; set strategy is_paper=true") }
```

with a call path that allows non-paper only through `newOrderManager`/live gate. Do not bypass `OrderManager`.

- [x] **Step 4: Verify**

```bash
rtk go test ./cmd/tradingagent ./internal/execution/kalshi -run 'Kalshi|LiveGate|Broker|Paper' -count=1
```

Expected: PASS.

---

## Task 5: Add Readiness and Reconciliation Checks

**Files:**
- Create: `internal/execution/kalshi/reconciler.go`
- Create: `internal/execution/kalshi/reconciler_test.go`
- Optional create: `internal/api/kalshi_readiness_handlers.go`
- Modify: `internal/api/server.go` only if adding the endpoint.

- [x] **Step 1: Reconciler tests**

Test that broker positions compare to local open Kalshi positions by ticker/side and produce drift records without submitting orders.

- [x] **Step 2: Implement read-only reconciler**

Create a `Reconciler` with dependencies:

```go
type ReconcilerDeps struct {
    Broker execution.Broker
    PositionRepo repository.PositionRepository
    Logger *slog.Logger
}
```

Expose:

```go
func (r *Reconciler) Check(ctx context.Context) (Result, error)
```

where `Result` contains counts and drift descriptions. Do not mutate DB in Sprint C unless explicitly needed.

- [ ] **Step 3: Optional readiness endpoint**

Not implemented in this sprint.

If implemented, add `GET /api/v1/kalshi/readiness` returning:

```json
{
  "credentials_configured": true,
  "live_trading_enabled": false,
  "broker_allowlisted": false,
  "paper_default": true
}
```

Do not expose secrets.

- [x] **Step 4: Verify**

```bash
rtk go test ./internal/execution/kalshi ./internal/api -run 'Kalshi|Readiness|Reconciler' -count=1
```

Expected: PASS.

---

## Task 6: Operator Runbook and First-Live Checklist

**Files:**
- Create: `docs/runbooks/kalshi-live-readiness.md`
- Modify: `docs/runbooks/kalshi-paper-data.md`

- [x] **Step 1: Write runbook**

Create `docs/runbooks/kalshi-live-readiness.md` with sections:

- Current default: paper/data only.
- Required gates:
  - `ENABLE_LIVE_TRADING=true`
  - `LIVE_TRADING_ALLOWED_BROKERS=kalshi`
  - `LIVE_TRADING_ALLOWED_STRATEGIES=<uuid>`
  - Kalshi credentials configured.
- Preflight commands:
  - `curl -sf http://10.0.0.56:3030/healthz`
  - readiness endpoint if implemented
  - check latest Kalshi discovery runs
  - check active paper strategy performance
- First live strategy procedure:
  - clone one paper strategy
  - set `is_paper=false`
  - use tiny max position sizing
  - run manually once
  - verify order and position reconciliation
- Rollback:
  - unset broker allowlist
  - set strategy `is_paper=true`
  - restart app

- [x] **Step 2: Cross-link from paper runbook**

Modify `docs/runbooks/kalshi-paper-data.md` to point operators to the live readiness runbook and explicitly state that discovery still creates paper strategies only.

---

## Task 7: Validation, Review, and Deploy Readiness

**Files:**
- Modify this plan file to check completed validation steps.

- [x] **Step 1: Backend validation**

```bash
rtk go test ./internal/execution/kalshi ./cmd/tradingagent ./internal/api ./internal/eventmarkets ./internal/kalshidiscovery ./internal/automation -count=1
```

Expected: PASS.

- [x] **Step 2: Safety review**

Request review focused on:

- No Kalshi strategy becomes live automatically.
- Live Kalshi requires all gates and credentials.
- Paper Kalshi still uses local paper broker.
- Kalshi discovery still creates active paper strategies only.
- Broker mapping cannot submit malformed orders.
- Runbook rollback is clear.

- [ ] **Step 3: Deploy checklist**

Prepared but not executed in this implementation pass. No migration expected;
deploy app only when the user asks to commit/deploy.

No migration expected. Deploy only app unless UI/readiness endpoint docs require web rebuild:

```bash
docker compose --project-name augr -f docker-compose.nuc.yml up -d --build app
```

Verify:

```bash
curl -sf http://10.0.0.56:3030/healthz
docker compose --project-name augr -f docker-compose.nuc.yml logs --since=5m app | rg 'kalshi|ERROR|panic|fatal'
```

---

## Acceptance Criteria

- Kalshi broker implements `execution.Broker` with live client-backed submit/cancel/status/positions/balance methods, and the runtime wires the live adapter when Kalshi credentials exist.
- Non-paper Kalshi is impossible unless global live trading, strategy allowlist, broker allowlist, credentials, and live client wiring all pass.
- Paper Kalshi behavior remains unchanged.
- Kalshi discovery still creates/reuses active paper strategies only.
- Readiness/reconciliation checks are read-only and safe.
- Operator runbook documents preflight, first-live, and rollback.
- No live Kalshi strategy is enabled or deployed by this sprint.
