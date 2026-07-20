# P1 Paper Accounting Restart Persistence Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> keep broker-account changes isolated, add restart-parity tests before code,
> then roll out behind a controlled deployment gate with rollback ready.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one shared runtime paper broker/account service for normal strategy execution and allocator paper mode, reconstruct paper cash/equity/buying power/open positions on startup, and prove controlled restart parity without mixing live/Alpaca records.

**Architecture:** Keep the `execution.Broker` interface unchanged. Introduce a narrow runtime paper-account service that restores broker state from persisted paper orders/trades/positions, then injects the same broker instance into normal strategy and allocator paper paths. The broker must expose explicit restoration APIs for account snapshot plus open-position/state seeding; startup must filter strictly to paper-owned records and exclude live/Alpaca rows by broker/source provenance.

**Tech Stack:** Go, existing PostgreSQL repositories, current paper broker/runtime, restart integration tests, deployment canary and rollback checks.

---

## Verified starting point

- Normal strategies currently create and retain `realStrategyRunner.localPaperBroker` in `cmd/tradingagent/prod_strategy_runner.go`.
- Allocator paper mode currently instantiates a fresh broker per request in `internal/portfolio/paper_order_manager_processor.go`.
- Startup reconstruction already exists only for paper options in `cmd/tradingagent/options_bootstrap.go`.
- `internal/execution/paper/broker.go` already has `RestoreAccount(...)`, but there is no public `RestorePositions(...)` API and no documented way to recover order sequence state.
- `execution.Broker` still only requires submit/cancel/status/positions/balance; do not add new mandatory interface methods.
- Restart parity must cover cash, equity, buying power, and open positions, while ensuring any Alpaca/live records remain excluded from paper reconstruction.

## Required restoration APIs and filtering semantics

- Add paper-broker restoration APIs that do not alter `execution.Broker`, at minimum:
  - `RestoreAccount(balance execution.Balance)` for cash/buying power/equity.
  - `RestorePositions(positions []domain.Position)` or equivalent state-seeding API for open simulated positions.
  - A way to restore `nextOrderID`/identifier continuity if restart tests depend on stable submission sequencing.
- Reconstruction must source only paper-owned records; exact semantics:
  - include only rows whose broker/source provenance is `paper` or otherwise explicitly marked paper-simulated;
  - exclude all Alpaca/live records even when they share ticker/strategy pairs;
  - exclude closed positions from the open-position seed set;
  - ignore non-paper assets when rebuilding the paper account snapshot;
  - if a repo lacks broker provenance, require an upstream paper-only query or an explicit guard that aborts instead of guessing.
- For order/trade/position reads, prefer repository filters that are already available (`TradeFilter`, `PositionFilter`) plus an explicit paper-only scoping condition; do not broaden filters to mixed-live datasets.
- Provenance gap: `TradeFilter` and `PositionFilter` do not carry broker provenance, and trades/positions do not store broker directly. Reconstruction must therefore use repository SQL that joins `trades -> orders` with `orders.broker = 'paper'` and `positions -> strategies` with `strategies.is_paper = true`, or abort when provenance is ambiguous.

---

## File map

- Modify: `internal/execution/paper/broker.go`
- Modify: `cmd/tradingagent/prod_strategy_runner.go`
- Modify: `internal/portfolio/paper_order_manager_processor.go`
- Modify: `cmd/tradingagent/runtime.go`
- Modify: `cmd/tradingagent/options_bootstrap.go` (or replace with a generalized paper-account bootstrap path)
- Add/update tests in: `internal/execution/paper/broker_test.go`, `cmd/tradingagent/runtime_test.go`, `cmd/tradingagent/options_bootstrap_test.go`, `internal/portfolio/*` as needed
- Add repository/provenance plumbing in: `internal/repository/interfaces.go`, `internal/repository/postgres/paper_account.go`, `internal/repository/postgres/paper_account_test.go`

## Task 1: Define the shared runtime paper-account service

**Files:**
- Modify: `cmd/tradingagent/prod_strategy_runner.go`
- Modify: `internal/portfolio/paper_order_manager_processor.go`
- Modify: `cmd/tradingagent/runtime.go`

- [ ] **Step 1: Add a failing shared-instance test**

Create a runtime test that proves normal strategy paper execution and allocator paper mode receive the same shared broker/account object after startup initialization, not two separate broker instances.

```go
if runner.localPaperBroker != processorBroker {

    t.Fatalf("paper broker instances differ; want one shared runtime broker")
}
```

- [ ] **Step 2: Add the runtime account-service constructor**

Introduce a small runtime helper that owns one paper broker/account service and returns it for both code paths. Keep broker interface compatibility intact by injecting the concrete broker where needed, not by expanding `execution.Broker`.

```go
type paperAccountService struct { broker *paper.PaperBroker }
func (s *paperAccountService) Broker() *paper.PaperBroker { return s.broker }
```

- [ ] **Step 3: Route both callers through the shared service**

Normal strategy runner should keep using `localPaperBroker`, but allocator paper mode must stop creating a fresh broker per request and instead reuse the shared runtime broker/account service.

- [ ] **Step 4: Add tests for instance reuse**

Cover both paths: one test for `realStrategyRunner.newBrokerForStrategy`/fallback reuse, one test that `PaperOrderManagerProcessor` is supplied a shared broker rather than instantiating its own.

- [ ] **Step 5: Commit**

```bash
git add cmd/tradingagent/prod_strategy_runner.go cmd/tradingagent/runtime.go internal/portfolio/paper_order_manager_processor.go cmd/tradingagent/runtime_test.go
git commit -m "feat(paper): share runtime account broker"
```

## Task 2: Add startup reconstruction for paper cash and positions

**Files:**
- Modify: `internal/execution/paper/broker.go`
- Modify: `cmd/tradingagent/options_bootstrap.go`
- Modify: `cmd/tradingagent/runtime.go`
- Test: `internal/execution/paper/broker_test.go`
- Test: `cmd/tradingagent/options_bootstrap_test.go`
- Modify: `internal/repository/interfaces.go`
- Add: `internal/repository/postgres/paper_account.go`
- Test: `internal/repository/postgres/paper_account_test.go`

- [ ] **Step 1: Write a failing restoration test**

Add a broker test that seeds an account balance and a non-empty open-position book, then restarts/reads back the broker snapshot and verifies cash, equity, buying power, and positions all survive.

```go
balance, _ := broker.GetAccountBalance(ctx)
positions, _ := broker.GetPositions(ctx)
if len(positions) != 2 { t.Fatalf("got %d positions, want 2", len(positions)) }
```

- [ ] **Step 2: Add restoration APIs to the paper broker**

Implement explicit state restoration methods on `PaperBroker` for account snapshot and position book seeding. Preserve clone/sort behavior on reads and keep the broker lock-protected.

```go
func (b *PaperBroker) RestorePositions(positions []domain.Position) error
func (b *PaperBroker) RestoreAccount(balance execution.Balance) error
```

- [ ] **Step 3: Generalize bootstrap from options-only to shared paper account**

Refactor `bootstrapPaperOptionsAccount` into a broader paper-account bootstrap path that reconstructs paper cash/equity from persisted paper orders/trades and seeds open positions into the shared broker. Use the actual `internal/execution/paper/broker.go:RestoreAccount(...)` entrypoint and add the missing `RestorePositions(...)` equivalent before startup can seed open positions.

- [ ] **Step 4: Preserve live/Alpaca isolation in the reconstruction query layer**

Use only paper-owned rows when reconstructing the account. Implement the paper-only read path with dedicated repository SQL in `internal/repository/postgres/paper_account.go` that joins `trades -> orders` to require `orders.broker = 'paper'` and `positions -> strategies` to require `strategies.is_paper = true`; if those joins/guards cannot be expressed safely in the current repo method, the bootstrap must abort rather than guess or fall back to mixed rows. Add interface methods in `internal/repository/interfaces.go` for the paper-only account queries so the bootstrap can call provenance-safe reads explicitly.

Expected repository surface:
- `ListPaperTrades(...)` / equivalent, returning only trades joined through `orders.broker = 'paper'`
- `GetOpenPaperPositions(...)` / equivalent, returning only open positions joined through `strategies.is_paper = true`
- `GetMaxPaperExternalIDSequence(...)` / equivalent, returning the highest parsed numeric suffix from paper `orders.external_id` values matching `paper-N`

The repository test file `internal/repository/postgres/paper_account_test.go` must cover mixed Alpaca/live fixtures and prove the paper-only queries exclude live rows while still returning valid paper rows.

- [ ] **Step 5: Commit**

```bash
git add internal/execution/paper/broker.go cmd/tradingagent/options_bootstrap.go cmd/tradingagent/runtime.go internal/execution/paper/broker_test.go cmd/tradingagent/options_bootstrap_test.go
git commit -m "feat(paper): restore account state on startup"
```

## Task 3: Restore exact balance semantics without mixing records

**Files:**
- Modify: `cmd/tradingagent/options_bootstrap.go`
- Test: `cmd/tradingagent/options_bootstrap_test.go`

- [ ] **Step 1: Add a negative test for mixed provenance**

Write a startup reconstruction test that includes a paper record set plus an Alpaca/live record set and asserts the live rows are ignored or cause a hard failure, depending on the query guarantees in the chosen repository path. The test should exercise the exact repo methods used by bootstrap (`TradeRepo.List`/`GetByOrder` and `PositionRepo.GetOpen`/`GetByStrategy`, or paper-scoped equivalents) and verify the provenance join logic rejects ambiguous data.

- [ ] **Step 2: Encode exact filtering semantics**

Document and implement the accounting inputs as:
1. paper trades only;
2. open paper positions only;
3. no closed positions in the seed set;
4. no live/Alpaca rows;
5. no non-paper asset classes in the account restore path.

Also document the safe order-ID restoration sequence: after transactional lifecycle recovery, restore the persisted account snapshot, then seed open positions, then advance `nextOrderID` from the highest numeric suffix parsed from `orders.external_id` values matching `paper-N` where `orders.broker = 'paper'`; take the max `N` and set `nextOrderID = N + 1`, never derive sequence state from UUID order IDs or malformed identifiers.

Add exact coverage for malformed IDs and collision prevention: malformed `external_id` values must be ignored or rejected explicitly, and replayed `paper-N` identifiers must not allow the restored broker to reuse an existing sequence value.

- [ ] **Step 3: Reconcile cash/equity/buying power deterministically**

Ensure the startup math matches broker simulation rules: cash from executed paper trades and fees, equity from cash plus open-position mark-to-market, buying power equal to restored cash unless the broker simulation defines a different invariant.

- [ ] **Step 4: Commit**

```bash
git add cmd/tradingagent/options_bootstrap.go cmd/tradingagent/options_bootstrap_test.go
git commit -m "fix(paper): filter startup account reconstruction"
```

## Task 4: Add controlled restart parity tests

**Files:**
- Modify: `cmd/tradingagent/runtime_test.go`
- Modify: `internal/execution/paper/broker_test.go`

- [ ] **Step 1: Add a restart-parity integration test**

Simulate a controlled restart by creating paper orders/trades/positions, rebuilding the runtime broker from persistence, and asserting identical cash, equity, buying power, and open positions before/after restart.

```go
beforeCash, _ := broker.GetAccountBalance(ctx)
afterCash, _ := restored.GetAccountBalance(ctx)
if beforeCash != afterCash { t.Fatalf("cash changed across restart") }
```

- [ ] **Step 2: Include delayed and partial simulation coverage**

Add cases where a paper order remains resting, partially fills, then restarts. Verify the broker and repositories agree on open positions and remaining order state after reload.

- [ ] **Step 3: Add a no-duplication guard**

Assert that a restarted broker does not duplicate existing positions, double-count fills, or rehydrate live records into the paper book.

- [ ] **Step 4: Commit**

```bash
git add cmd/tradingagent/runtime_test.go internal/execution/paper/broker_test.go
git commit -m "test(paper): add controlled restart parity"
```

## Task 5: Support delayed and partial paper simulation

**Files:**
- Modify: `internal/execution/paper/broker.go`
- Modify: `internal/execution/paper/broker_test.go`
- Potentially modify: `internal/portfolio/paper_order_manager_processor.go`

- [ ] **Step 1: Add failing tests for delayed and partial fill behavior**

Create tests that cover a submitted-but-resting order, a partial fill that leaves remainder open, and a restart that preserves the partially filled state.

- [ ] **Step 2: Extend broker simulation state**

Add the smallest paper-broker state needed to persist delayed/resting/partial semantics across process restarts while leaving the `execution.Broker` interface unchanged.

- [ ] **Step 3: Keep allocator and normal strategy semantics aligned**

Ensure allocator paper mode and normal strategy paper mode both use the same delayed/partial simulation rules.

- [ ] **Step 4: Commit**

```bash
git add internal/execution/paper/broker.go internal/execution/paper/broker_test.go internal/portfolio/paper_order_manager_processor.go
git commit -m "feat(paper): persist delayed fill simulation"
```

## Task 6: Deployment gates and rollback

**Files:**
- Add/update: `docs/runbooks/` or deployment notes if needed in a later implementation pass

- [ ] **Step 1: Define deployment gate checks**

Gate rollout on:
1. restart-parity tests passing;
2. zero divergence in cash/equity/buying power after controlled restart;
3. no mixed live/Alpaca rows in paper reconstruction;
4. delayed/partial simulation tests passing.

- [ ] **Step 2: Stage canary release**

Deploy only to a controlled paper-only canary first. Verify the same account snapshot is observed before and after one restart.

- [ ] **Step 3: Rollback plan**

Rollback by disabling the shared paper-account bootstrap path and falling back to the pre-change isolated paper broker behavior. If restart reconstruction misreads provenance, stop startup bootstrap and retain the prior in-memory state policy rather than mixing records.

- [ ] **Step 4: Final verification**

Run the smallest targeted test set plus one controlled restart in the deployment environment. Do not promote if any parity or provenance guard fails.

---

## Sequencing rule

Do not parallelize any changes that write the same paper financial lifecycle tables or mutate the shared broker restoration path. Finish shared broker/account restoration before enabling allocator paper reuse and delayed/partial restart simulation. The restoration sequence is: transactional lifecycle replay from paper-only order/trade/position queries, `RestoreAccount(...)`, `RestorePositions(...)`, then safe `nextOrderID` advancement by parsing the maximum `paper-N` `orders.external_id` suffix from paper-broker rows and setting the next ID to `N + 1`.
