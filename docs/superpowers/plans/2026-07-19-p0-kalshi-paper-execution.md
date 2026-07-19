# P0 Kalshi Paper Execution Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make newly created Kalshi paper limit orders fill deterministically from the quote used to create the trading plan, while safely cancelling the legacy unfillable backlog.

**Architecture:** Carry the explicit Kalshi snapshot quote from `TradingPlan` into `domain.Order`; the paper broker uses it to decide whether the limit crosses without confusing the limit itself with market data. Persist the broker's actual simulated fill through the existing `OrderManager`, and cancel—not retroactively fill—the legacy submitted backlog with one audit row per affected order.

**Tech Stack:** Go, PostgreSQL operational runbook, existing paper broker and order manager.

---

## Verified cause

Production has 1,231 `broker='paper'`, `market_type='kalshi'`, `order_type='limit'` orders in `submitted`. `internal/execution/kalshi/executor.go` emits a quoted limit plan; `PaperBroker.resolveReferencePrice` deliberately refuses to use a limit as its own market reference, so `simulateFillPrice` returns `shouldFill=false`. These orders came from the normal Kalshi strategy runner, not the shadow allocator.

## File map

- Modify `cmd/tradingagent/prod_strategy_runner.go`: identify the Kalshi snapshot quote as the simulation reference when mapping the native decision.
- Modify `internal/execution/order_manager.go`: forward the explicit reference and preserve broker fill facts.
- Modify `internal/domain/order.go`: add a non-persisted `ReferencePrice` field.
- Modify `internal/execution/paper/broker.go`: resolve the explicit reference first.
- Modify tests in `internal/execution/paper/broker_test.go` and `internal/execution/order_manager_test.go`.
- Create `docs/runbooks/kalshi-paper-order-backlog.md`: guarded one-time cancellation and verification.

### Task 1: Carry an explicit quote reference into paper orders

**Files:**
- Modify: `cmd/tradingagent/prod_strategy_runner.go:815-833`
- Modify: `internal/execution/order_manager.go:34-60,450-472`
- Modify: `internal/domain/order.go:103-138`
- Test: `cmd/tradingagent/prod_strategy_runner_test.go`
- Test: `internal/execution/order_manager_test.go`

- [ ] **Step 1: Write a failing order-manager test**

Extract the Kalshi native decision-to-plan mapping into `kalshiTradingPlan(strategy, snapshot, decision)` and test that the quoted `EntryPrice` is copied to `ReferencePrice`. Then create a Kalshi limit plan with both fields set to `0.04` and assert the broker stub receives both values.

```go
plan := execution.TradingPlan{MarketType: domain.MarketTypeKalshi, Ticker: "KXTEST", EntryType: "limit", EntryPrice: 0.04, ReferencePrice: 0.04, PositionSize: 10, StopLoss: 0.01, Side: "YES"}
if broker.submitted.ReferencePrice == nil || *broker.submitted.ReferencePrice != 0.04 {
	t.Fatalf("reference price = %v, want 0.04", broker.submitted.ReferencePrice)
}
```

- [ ] **Step 2: Run the test and verify compile failure**

Run: `go test ./internal/execution -run 'TestOrderManager.*ReferencePrice' -count=1`

Expected: compile failure because the fields do not exist.

- [ ] **Step 3: Add the transient fields and assignment**

```go
// TradingPlan
ReferencePrice float64 `json:"reference_price,omitempty"`

// domain.Order; this is simulation input, not durable order state.
ReferencePrice *float64 `json:"-"`
```

Set `ReferencePrice: decision.EntryPrice` only in the Kalshi plan mapping in `prod_strategy_runner.go`. When creating an order, forward only an explicit reference; do not generically copy `EntryPrice`, because that would make unrelated paper limits self-fill:

```go
if plan.ReferencePrice > 0 {
	referencePrice := plan.ReferencePrice
	order.ReferencePrice = &referencePrice
}
```

- [ ] **Step 4: Run execution tests**

Run: `go test ./internal/execution -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tradingagent/prod_strategy_runner.go cmd/tradingagent/prod_strategy_runner_test.go internal/execution/order_manager.go internal/domain/order.go internal/execution/order_manager_test.go
git commit -m "feat(execution): carry paper reference prices"
```

### Task 2: Fill crossing paper limits from the explicit quote

**Files:**
- Modify: `internal/execution/paper/broker.go:281-310,403-417`
- Modify: `internal/execution/paper/broker_test.go`

- [ ] **Step 1: Add failing tests**

Add three table cases:

```go
{name: "buy fills when reference at limit", side: domain.OrderSideBuy, limit: 0.04, reference: 0.04, wantStatus: domain.OrderStatusFilled},
{name: "buy rests when reference above limit", side: domain.OrderSideBuy, limit: 0.04, reference: 0.05, wantStatus: domain.OrderStatusSubmitted},
{name: "missing reference still rests", side: domain.OrderSideBuy, limit: 0.04, reference: 0, wantStatus: domain.OrderStatusSubmitted},
```

The first test must also assert `FilledAvgPrice <= LimitPrice`, a non-zero `FilledAt`, and full `FilledQuantity`.

- [ ] **Step 2: Run tests and verify the crossing case fails**

Run: `go test ./internal/execution/paper -run 'TestPaperBrokerSubmitOrder_LimitOrderReferencePrice' -count=1`

Expected: FAIL because `resolveReferencePrice` ignores `ReferencePrice`.

- [ ] **Step 3: Resolve explicit reference before all legacy fallbacks**

```go
func resolveReferencePrice(order *domain.Order) (float64, bool) {
	if order.ReferencePrice != nil && *order.ReferencePrice > 0 {
		return *order.ReferencePrice, true
	}
	if order.FilledAvgPrice != nil && *order.FilledAvgPrice > 0 {
		return *order.FilledAvgPrice, true
	}
	if order.OrderType == domain.OrderTypeMarket && order.LimitPrice != nil && *order.LimitPrice > 0 {
		return *order.LimitPrice, true
	}
	if order.StopPrice != nil && *order.StopPrice > 0 {
		return *order.StopPrice, true
	}
	return 0, false
}
```

- [ ] **Step 4: Run paper broker and execution tests**

Run: `go test ./internal/execution/paper ./internal/execution -count=1`

Expected: PASS, including the existing test that a limit with no reference remains submitted.

- [ ] **Step 5: Commit**

```bash
git add internal/execution/paper/broker.go internal/execution/paper/broker_test.go
git commit -m "fix(paper): fill limits from quoted reference"
```

### Task 3: Preserve the broker's actual fill price

**Files:**
- Modify: `internal/execution/order_manager.go:807-831`
- Test: `internal/execution/order_manager_test.go`

- [ ] **Step 1: Add a failing fill-integrity test**

Use a paper broker with non-zero slippage and a crossing Kalshi limit. Assert the persisted order `FilledAvgPrice`, created trade `Price`, and created position `AvgEntry` all equal the broker-simulated fill rather than `plan.EntryPrice`.

- [ ] **Step 2: Run the test and verify failure**

Run: `go test ./internal/execution -run 'TestOrderManager_PreservesBrokerFillPrice' -count=1`

Expected: FAIL because `handleFill` overwrites `FilledAvgPrice` with `plan.EntryPrice`.

- [ ] **Step 3: Use broker fill facts when present**

```go
now := m.currentTime()
if order.FilledQuantity <= 0 { order.FilledQuantity = order.Quantity }
if order.FilledAt == nil { order.FilledAt = &now }
if order.FilledAvgPrice == nil && plan.EntryPrice > 0 { order.FilledAvgPrice = &plan.EntryPrice }
```

Keep the existing `fillPrice` selection from `order.FilledAvgPrice`; it now retains the broker-simulated price. Do not change transaction boundaries in this P0.

- [ ] **Step 4: Run processor and runtime tests**

Run: `go test ./internal/execution ./internal/execution/paper -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/execution/order_manager.go internal/execution/order_manager_test.go
git commit -m "fix(execution): preserve broker fill facts"
```

### Task 4: Add the guarded legacy-backlog runbook

**Files:**
- Create: `docs/runbooks/kalshi-paper-order-backlog.md`

- [ ] **Step 1: Document the exact preflight query**

```sql
SELECT count(*) AS affected,
       min(created_at) AS oldest,
       max(created_at) AS newest,
       sum(filled_quantity) AS filled_quantity
FROM orders
WHERE broker = 'paper'
  AND market_type = 'kalshi'
  AND order_type = 'limit'
  AND status = 'submitted'
  AND filled_quantity = 0;
```

- [ ] **Step 2: Document the transaction-wrapped cancellation**

```sql
BEGIN;
WITH cancelled AS (
  UPDATE orders
  SET status = 'cancelled'
  WHERE broker = 'paper'
    AND market_type = 'kalshi'
    AND order_type = 'limit'
    AND status = 'submitted'
    AND filled_quantity = 0
  RETURNING id
)
INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details)
SELECT 'legacy_paper_order_cancelled', 'order', id, 'operator:p0_backfill',
       jsonb_build_object('reason', 'missing_historical_reference_price')
FROM cancelled
RETURNING entity_id;
-- Compare returned/audited IDs and row count with preflight before COMMIT.
COMMIT;
```

The runbook must say never to backfill fills, trades, positions, or P/L for these rows because no historical execution reference was persisted.

- [ ] **Step 3: Document rollback safety**

Rollback means stop before `COMMIT`. After commit, do not restore `submitted`; preserve cancellation as the truthful terminal state and restore the database backup only for a proven operator error.

- [ ] **Step 4: Commit**

```bash
git add docs/runbooks/kalshi-paper-order-backlog.md
git commit -m "docs(ops): add paper order backlog cleanup"
```

### Task 5: Deploy and accept

- [ ] Pause affected Kalshi schedules during rollout.
- [ ] Deploy the app and verify health.
- [ ] Run the guarded backlog cancellation and record affected rows in the deployment log.
- [ ] Re-enable one Kalshi paper strategy as a canary.
- [ ] Verify its new order reaches `filled`, creates exactly one trade, and creates or updates exactly one position.
- [ ] Verify order `filled_avg_price`, trade `price`, and position `avg_entry` agree exactly for each canary fill.
- [ ] Re-enable remaining Kalshi schedules only after five canary runs complete without a new stale `submitted` order.
- [ ] Accept P0 after 24 hours with zero paper Kalshi orders older than one schedule interval in `submitted`.

**Deferred to P1:** restart-persistent cross-market paper cash/equity reconstruction, shared allocator broker wiring, transactional/idempotent fill persistence, realistic delayed/partial fills, and quote refresh for resting limits.
