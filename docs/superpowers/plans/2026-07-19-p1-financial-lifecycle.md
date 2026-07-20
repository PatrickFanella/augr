# P1 Financial Lifecycle Implementation Plan

> **For agentic workers:** Execute this plan in order. Start with the failing tests, then add the implementation, then run verification, then commit. Keep the scope narrow: atomic writes for order/position/trade and prediction-settlement persistence only. Preserve existing broker APIs and do not leak `pgx.Tx` into `internal/execution`.

**Goal:** Make financial lifecycle writes atomic and idempotent for order, position, trade, and prediction-settlement updates in the application repository.

**Architecture:** Introduce a narrow application-level transactional repository in `internal/repository/postgres` that owns the transaction boundary and exposes a fill/persistence unit of work. Use durable idempotency keys for paper fills, enforce partial uniqueness for non-null trade external IDs, and keep execution-domain code transaction-agnostic.

**Tech Stack:** Go, PostgreSQL, existing repository test harness, migration SQL.

---

## Verified baseline

- `internal/execution/order_manager.go` currently persists order, position, and trade in separate calls inside `handleFill`; partial failure can leave state inconsistent.
- `internal/repository/postgres/order.go`, `position.go`, and `trade.go` are pool-backed and do not expose a transaction boundary.
- `migrations/000032_trade_external_id.up.sql` adds `trades.external_id` but only with a plain index; uniqueness is not enforced.
- `migrations/000053_prediction_order_metadata.up.sql` already persists prediction-side metadata on orders.
- `internal/repository/interfaces.go` defines repository interfaces, so the new transactional API should be added there rather than by threading `*pgx.Tx` through execution.

## File map

- Modify: `internal/repository/interfaces.go`
- Modify: `internal/repository/postgres/order.go`
- Modify: `internal/repository/postgres/position.go`
- Modify: `internal/repository/postgres/trade.go`
- Modify: `internal/repository/postgres/db.go`
- Modify: `internal/repository/postgres/schema_version.go`
- Modify: `internal/execution/order_manager.go`
- Modify: `internal/execution/kalshi/executor.go` and `internal/execution/polymarket/executor.go` only if their tests need to prove broker APIs stay unchanged; otherwise leave untouched.
- Modify tests: `internal/execution/order_manager_test.go`, `internal/repository/postgres/order_test.go`, `internal/repository/postgres/position_test.go`, `internal/repository/postgres/trade_test.go`, and add focused integration tests for the transactional unit of work and prediction-settlement replay.
- Create migration: `migrations/000055_financial_lifecycle_atomicity.up.sql`
- Create migration: `migrations/000055_financial_lifecycle_atomicity.down.sql`

## Task 1: Prove the current lifecycle is non-atomic

**Files:**
- Modify: `internal/execution/order_manager_test.go`
- Modify: `internal/repository/postgres/trade_test.go`
- Create: `internal/repository/postgres/financial_lifecycle_test.go`

- [ ] **Step 1: Add a failing rollback/replay test at the execution boundary**

Write a test around `(*execution.OrderManager).handleFill` that uses a repo double capable of failing on the third persistence call. The test should assert that when trade creation fails after order and position writes, the order and position changes are rolled back instead of remaining committed.

```go
err := mgr.handleFill(ctx, order, plan, strategyID, runID, decisionID)
if err == nil {
    t.Fatal("expected fill to fail")
}
if got := orderRepo.CountPersisted(); got != 0 {
    t.Fatalf("expected no committed orders after rollback, got %d", got)
}
if got := positionRepo.CountPersisted(); got != 0 {
    t.Fatalf("expected no committed positions after rollback, got %d", got)
}
if got := tradeRepo.CountPersisted(); got != 0 {
    t.Fatalf("expected no committed trades after rollback, got %d", got)
}
```

- [ ] **Step 2: Add a replay/idempotency test for duplicate fill delivery**

Add a repository integration test that calls the same new unit-of-work twice with identical inputs and a deterministic idempotency key. The second call must be a no-op and must return the already-persisted `order_id`, nullable `position_id`, `trade_id`, and `created_at` values.

```go
key := "paper_fill:v1:" + orderID.String() + ":full"
first, err := repo.ApplyPaperFill(ctx, key, fill)
second, err := repo.ApplyPaperFill(ctx, key, fill)
if err != nil || second == nil {
    t.Fatalf("expected replay to return existing result, err=%v second=%v", err, second)
}
if second.OrderID != first.OrderID || second.TradeID != first.TradeID || !second.CreatedAt.Equal(first.CreatedAt) {
    t.Fatalf("expected deterministic replay result, got %#v %#v", first, second)
}
```

- [ ] **Step 3: Run the tests and confirm they fail for the current code**

Run:

```bash
go test ./internal/execution -run 'TestOrderManager.*Rollback|TestOrderManager.*Replay' -count=1
go test ./internal/repository/postgres -run 'TestFinancialLifecycle.*' -count=1
```

Expected: rollback/replay assertions fail because `handleFill` currently performs separate `Create`/`Update` calls and there is no transactional unit of work.

## Task 2: Add the transactional repository contract

**Files:**
- Modify: `internal/repository/interfaces.go`
- Modify: `internal/repository/postgres/db.go`

- [ ] **Step 1: Add a narrow transactional interface**

Extend the repository layer with a purpose-built contract instead of exposing `pgx.Tx` in execution code.

```go
type FinancialLifecycleRepository interface {
    ApplyOrderFill(ctx context.Context, input OrderFillInput) (OrderFillResult, error)
    SettlePredictionDecision(ctx context.Context, input PredictionDecisionSettlementInput) (PredictionDecisionSettlementResult, error)
}

type OrderFillInput struct {
    IdempotencyKey string
    Order          *domain.Order
    FillIntent     OrderFillIntent
    Trade          *domain.Trade
}

type OrderFillIntent struct {
    Side           domain.OrderSide
    Quantity       decimal.Decimal
    ExecutionPrice decimal.Decimal
}

type PredictionDecisionSettlementInput struct {
    IdempotencyKey string
    OrderID        uuid.UUID
    PositionID     uuid.UUID
    TradeID        uuid.UUID
    Decision       *domain.TradeDecision
}
```

Keep the result types small and persistence-oriented; do not include driver objects. Persist enough idempotency identity to replay the created rows (`order_id`, `position_id`, `trade_id`, `created_at`). `position_id` must be nullable when a fill does not create or update a position.

- [ ] **Step 2: Add a postgres implementation that owns `pgx.Tx` internally**

Implement `(*postgres.DB).ApplyOrderFill` and `(*postgres.DB).SettlePredictionDecision` or a dedicated repo type in `internal/repository/postgres/db.go` that uses `DB.Pool.BeginTx`, executes the required reads/writes inside the transaction, and commits/rolls back internally.

```go
tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
if err != nil { return OrderFillResult{}, err }
defer func() { _ = tx.Rollback(ctx) }()

// SELECT the current open position FOR UPDATE, then compute the mutation inside the tx.
// on success: return tx.Commit(ctx)
```

## Task 3: Enforce durable idempotency and uniqueness

**Files:**
- Modify: `migrations/000055_financial_lifecycle_atomicity.up.sql`
- Modify: `migrations/000055_financial_lifecycle_atomicity.down.sql`
- Modify: `internal/repository/postgres/trade.go`
- Modify: `internal/repository/postgres/order.go`
- Modify: `internal/repository/postgres/position.go`

- [ ] **Step 1: Add a migration that creates the lifecycle/idempotency table and partial unique index**

Migration up must include deterministic result storage for replay, with IDs returned from the same row set on duplicate delivery:

```sql
CREATE TABLE IF NOT EXISTS financial_fill_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    position_id UUID NULL REFERENCES positions(id) ON DELETE SET NULL,
    trade_id UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS prediction_settlement_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    decision_id UUID NOT NULL REFERENCES trade_decisions(id) ON DELETE CASCADE,
    position_id UUID NULL REFERENCES positions(id) ON DELETE SET NULL,
    trade_id UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
    replay_event_id UUID NULL REFERENCES replay_events(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- production preflight: detect duplicate non-null external IDs before creating the partial unique index
SELECT external_id, COUNT(*)
FROM trades
WHERE external_id IS NOT NULL
GROUP BY external_id
HAVING COUNT(*) > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_trades_external_id_unique
    ON trades (external_id)
    WHERE external_id IS NOT NULL;
```

Down migration must remove the index first, then the settlement and fill idempotency tables.

```sql
DROP INDEX IF EXISTS idx_trades_external_id_unique;
DROP TABLE IF EXISTS prediction_settlement_idempotency;
DROP TABLE IF EXISTS financial_fill_idempotency;
```

Update `internal/repository/postgres/schema_version.go` so `RequiredSchemaVersion`
is `55`; the repository's schema synchronization test and runtime startup gate
require this to match the newest migration.

- [ ] **Step 2: Make trade external IDs idempotent at the database boundary**

Update `internal/repository/postgres/trade.go` so inserts still allow `external_id` to be NULL, but duplicate non-null IDs fail fast at the database level. No change to broker APIs; only persistence semantics change.

- [ ] **Step 3: Preserve broker APIs while passing the durable key through persistence**

The execution layer must derive the key from order identity only and pass it to the repository. For immediate paper fills use:

```go
const paperFillKeyPrefix = "paper_fill:v1:"
idempotencyKey := paperFillKeyPrefix + order.ID.String() + ":full"
```

Future sequence-based fills must follow:

```go
paper_fill:v1:<order_id>:<fill_sequence>
```

## Task 4: Convert execution fill handling to the new repository

**Files:**
- Modify: `internal/execution/order_manager.go`
- Modify: `internal/execution/order_manager_test.go`

- [ ] **Step 1: Add a failing end-to-end fill test with injected rollback and replay**

Create a test that injects a repository failure after order and position staging, then replays the same fill. The first run must roll back completely; the second run must succeed and persist exactly once.

```go
repo.failAfter = 2
err := mgr.handleFill(ctx, order, plan, strategyID, runID, decisionID)
if err == nil { t.Fatal("expected first fill attempt to fail") }

repo.failAfter = 0
if err := mgr.handleFill(ctx, order, plan, strategyID, runID, decisionID); err != nil {
    t.Fatalf("replay fill failed: %v", err)
}
```

- [ ] **Step 2: Replace the multi-write sequence with one repository call**

Refactor `(*OrderManager).handleFill` so it prepares the order, fill intent, trade, and optional settlement state, then calls the new transactional repository once. The repository must load the current open position `FOR UPDATE` and derive the resulting position mutation inside the transaction.

```go
result, err := m.financialLifecycleRepo.ApplyOrderFill(ctx, repository.OrderFillInput{
    IdempotencyKey: "paper_fill:v1:" + order.ID.String() + ":full",
    Order: order,
    FillIntent: repository.OrderFillIntent{Side: order.Side, Quantity: order.Quantity, ExecutionPrice: fill.Price},
    Trade: trade,
})
if err != nil { return fmt.Errorf("order_manager: persist fill: %w", err) }
```

Keep `orderManager` free of `pgx.Tx`, `*pgxpool.Pool`, or any transaction-scope type. Preserve optional wiring compatibility for `NewOrderManager` call sites by adding the new repository dependency in a nil-safe/defaulted way so existing constructors continue to compile.

- [ ] **Step 3: Ensure prediction-settlement persistence participates when applicable**

Add the settlement update path only when a prediction order has an associated settlement decision/state. The settlement UoW must read the exit position inside the transaction with `FOR UPDATE` before writing settlement state. Do not alter broker interfaces; settlement persistence is a repository concern.

- [ ] **Step 4: Add a durable prediction-settlement replay contract**

Create a settlement-specific idempotency contract distinct from order-fill replay. It must persist `idempotency_key`, `decision_id`, nullable `position_id`, `trade_id`, nullable `replay_event_id`, and `created_at`, and return the original result on retry without creating a duplicate payout.

```go
type PredictionDecisionSettlementResult struct {
    DecisionID    uuid.UUID
    PositionID    *uuid.UUID
    TradeID       uuid.UUID
    ReplayEventID *uuid.UUID
    CreatedAt     time.Time
}
```

## Task 5: Add integration coverage for rollback and replay

**Files:**
- Create: `internal/repository/postgres/financial_lifecycle_test.go`
- Modify: `internal/repository/postgres/order_test.go`
- Modify: `internal/repository/postgres/position_test.go`
- Modify: `internal/repository/postgres/trade_test.go`

- [ ] **Step 1: Test both atomic units of work independently**

Use the postgres integration harness to assert that order, position, and trade rows appear together after a successful `ApplyOrderFill` call and none appear on failure. Separately call `SettlePredictionDecision` and assert its position, payout trade, decision resolution, replay event, and idempotency row appear together or all roll back together.

```go
result, err := repo.ApplyOrderFill(ctx, input)
if err != nil { t.Fatal(err) }
gotOrder, _ := orderRepo.Get(ctx, result.OrderID)
if result.PositionID != nil {
    gotPosition, _ := positionRepo.Get(ctx, *result.PositionID)
    _ = gotPosition
}
gotTrade, _ := tradeRepo.Get(ctx, result.TradeID)
_ = gotTrade

settlementResult, err := repo.SettlePredictionDecision(ctx, settlementInput)
if err != nil { t.Fatal(err) }
gotDecision, _ := decisionRepo.Get(ctx, settlementResult.DecisionID)
gotReplayEvent, _ := replayRepo.Get(ctx, *settlementResult.ReplayEventID)
_, _ = gotDecision, gotReplayEvent
```

- [ ] **Step 2: Test duplicate delivery with replay semantics**

Assert the same `paper_fill:v1:<order_id>:full` key returns the original persisted rows and does not create duplicates, then assert the settlement-specific replay key returns the original settlement result without issuing a duplicate payout.

```go
settlementKey := "prediction_settlement:v1:" + decisionID.String()
first, err := repo.SettlePredictionDecision(ctx, settlementInput)
second, err := repo.SettlePredictionDecision(ctx, settlementInput)
if err != nil || second == nil {
    t.Fatalf("expected settlement replay to return existing result, err=%v second=%v", err, second)
}
if second.DecisionID != first.DecisionID || second.TradeID != first.TradeID {
    t.Fatalf("expected deterministic settlement replay result, got %#v %#v", first, second)
}
_ = settlementKey
```

- [ ] **Step 3: Test unique external_id behavior explicitly**

Add a trade repo test proving two non-null trades with the same `external_id` now violate the partial unique index while two NULL `external_id` rows still succeed.

## Task 6: Verify, rollout, and rollback

**Files:**
- Update: `docs/runbooks/` only if the rollout/rollback notes need a dedicated entry; otherwise keep this plan self-contained.

- [ ] **Step 1: Run the focused tests**

Run:

```bash
go test ./internal/repository/postgres -count=1
go test ./internal/execution -count=1
```

Expected: PASS.

- [ ] **Step 2: Run migration validation**

Apply `000055_financial_lifecycle_atomicity.up.sql` on a disposable database, confirm the fill and settlement idempotency tables plus unique trade index exist, then apply the down migration and confirm all are removed.

- [ ] **Step 3: Rollout plan**

1. Deploy code with the new repository contract and migration.
2. Run the migration.
3. Canary one strategy or one paper execution path.
4. Confirm no duplicate trades, no half-written fills, and idempotent replay behavior.

- [ ] **Step 4: Rollback plan**

1. Stop the canary or execution path.
2. Deploy the previous binary.
3. Apply the down migration only if the new table/index must be removed; otherwise leave data intact and disable the new call site.
4. Verify broker APIs remain unchanged and execution resumes on the old code path.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/interfaces.go internal/repository/postgres/db.go internal/repository/postgres/schema_version.go internal/repository/postgres/order.go internal/repository/postgres/position.go internal/repository/postgres/trade.go internal/execution/order_manager.go internal/execution/order_manager_test.go internal/repository/postgres/financial_lifecycle_test.go migrations/000055_financial_lifecycle_atomicity.up.sql migrations/000055_financial_lifecycle_atomicity.down.sql
git commit -m "feat(execution): make financial lifecycle atomic"
```
