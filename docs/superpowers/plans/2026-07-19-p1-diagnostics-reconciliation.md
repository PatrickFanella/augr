# P1 Diagnostics + Reconciliation Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Use checkbox (`- [ ]`) steps exactly as written, keep API compatibility explicit, and do not force numeric parity when the report reveals an unexplained residual.

**Goal:** Remove the allocator’s 100-row diagnostic sampling limit, expose total-vs-sample semantics compatibly, and add a read-only Alpaca P/L reconciliation report that compares broker cash/equity against local closed/open P&L, trades, fees, and any discoverable adjustments while preserving an explicit unexplained residual.

**Architecture:** Replace sample-only diagnostics with repository-backed counts/aggregates and keep existing sample payloads as a bounded preview for backward compatibility. Add a read-only reconciliation surface that reports broker cash/equity, local closed and open P&L, trade/fee totals, any known adjustments that can be derived from persisted data, and a residual field that is never auto-zeroed. No ledger migration, forced match, or write-path mutation is part of this phase.

**Tech Stack:** Go, PostgreSQL/pgx, existing API handlers, existing Alpaca reconciliation adapter, SQL parity tests, production canary.

---

## File map

- Modify `internal/portfolio/diagnostics.go` and `internal/portfolio/diagnostics_test.go`: replace fixed 100-row sampling with total-count/aggregate-based diagnostics while retaining sample metadata.
- Modify `internal/api/portfolio_allocator_handlers.go` and API tests: expose compatible total/sample fields in responses without breaking existing callers.
- Modify `internal/repository/postgres/*` repositories involved in allocator diagnostics: add count/aggregate methods where needed and back them with SQL parity coverage.
- Modify `internal/automation/alpaca_reconciliation.go` and `internal/api/automation_alpaca_handlers.go`: add a read-only reconciliation report endpoint and response model.
- Add/modify reconciliation tests in automation/API/repository layers, including SQL parity and canary-oriented validation.

## Task 1: Replace 100-row sampling with repository-backed totals

**Files:**
- Modify: `internal/portfolio/diagnostics.go`
- Modify: `internal/portfolio/diagnostics_test.go`
- Modify: `internal/repository/postgres/*` repository files used by allocator diagnostics

- [ ] **Step 1: Add a failing test that proves sampling is no longer the source of truth**

Create a diagnostics test with more than 100 runs, decisions, strategies, and positions so the old fixed sample would miss rows. Assert that the summary exposes both the total population and the sample population, and that counts match repository totals rather than a hard-coded 100-row subset.

- [ ] **Step 2: Define compatible total/sample semantics**

Keep existing sample-oriented fields for backward compatibility, but add explicit total fields (for example `total_runs`, `total_decisions`, `total_strategies`, `total_positions`, or equivalent repository-backed totals) and make sample fields clearly represent the bounded preview rather than the full population.

- [ ] **Step 3: Implement repository-backed counts/aggregates**

Use existing repository `Count`/`CountOpen` methods plus repository aggregates to populate totals. If a list is still needed for preview output, keep it bounded and page-aware; do not infer totals from returned slice length when the query is capped.

```go
// Example shape only; preserve current public API compatibility.
type DiagnosticsSummary struct {
    TotalStrategies int `json:"total_strategies"`
    SampleStrategies int `json:"sample_strategies"`
    TotalPositions   int `json:"total_positions"`
    SamplePositions  int `json:"sample_positions"`
}
```

- [ ] **Step 4: Add SQL parity tests for repository totals**

For each repository method used by diagnostics, add parity tests that compare the repository result against direct SQL `COUNT(*)` / aggregate queries on the same fixture rows.

- [ ] **Step 5: Run diagnostics and repository tests**

Run:

```bash
go test ./internal/portfolio ./internal/repository/postgres -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/portfolio/diagnostics.go internal/portfolio/diagnostics_test.go internal/repository/postgres
git commit -m "feat(diagnostics): use repository totals for sampling"
```

## Task 2: Expose API compatibility for total/sample fields

**Files:**
- Modify: `internal/api/portfolio_allocator_handlers.go`
- Modify: API tests for portfolio diagnostics/summary responses

- [ ] **Step 1: Add response-shape tests first**

Add tests asserting that current clients still receive the sample-oriented fields, while new clients can read explicit totals and sample sizes separately.

- [ ] **Step 2: Update the handler response without removing existing keys**

Keep the current response contract stable. Add new total fields alongside existing fields, and document sample semantics in the JSON response structure or response builder.

- [ ] **Step 3: Ensure pagination/limits are not misrepresented**

When a response is bounded, report the sample size as the bounded page size and the total as the full repository count. Do not overload `len(items)` to mean total.

- [ ] **Step 4: Run API tests**

Run:

```bash
go test ./internal/api -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/portfolio_allocator_handlers.go internal/api/*test.go
git commit -m "feat(api): preserve diagnostics totals and samples"
```

## Task 3: Add read-only Alpaca P/L reconciliation report

**Files:**
- Modify: `internal/automation/alpaca_reconciliation.go`
- Modify: `internal/api/automation_alpaca_handlers.go`
- Modify: automation/API tests for Alpaca reconciliation

- [ ] **Step 1: Add a failing reconciliation report test**

Create a test that expects a read-only report containing: broker cash, broker equity, local closed P&L, local open/unrealized P&L, trade count, fee total, discoverable adjustments (if any), and an explicit unexplained residual. Verify the report does **not** auto-reconcile by mutating local records or zeroing the residual.

- [ ] **Step 2: Extend the reconciliation domain model**

Add a dedicated report struct and API response model for reconciliation output. Keep the existing reconcile/verify endpoints working; this report is additive.

```go
type AlpacaPLReconciliationReport struct {
    BrokerCash                float64 `json:"broker_cash"`
    BrokerEquity              float64 `json:"broker_equity"`
    LocalClosedPnL            float64 `json:"local_closed_pnl"`
    LocalOpenPnL              float64 `json:"local_open_pnl"`
    TradeCount                int     `json:"trade_count"`
    FeeTotal                  float64 `json:"fee_total"`
    KnownAdjustments          float64 `json:"known_adjustments"`
    UnexplainedResidual       float64 `json:"unexplained_residual"`
    AdjustmentDetails         []string `json:"adjustment_details,omitempty"`
}
```

- [ ] **Step 3: Compute report values from persisted data and broker snapshot**

Use existing Alpaca broker snapshot data plus local repositories to compute. The current `AlpacaReconciliationBroker` lacks cash/equity, so plan a read-only account snapshot method on the Alpaca adapter/interface and consume that snapshot here:

  - broker cash/equity from a read-only account snapshot method on the Alpaca adapter/interface
  - local closed P&L from closed positions
  - local open P&L from open positions
  - trade count and fee sum from trades
  - known adjustments only when discoverable from persisted rows or already-classified non-P&L items
  - residual as the remaining difference, not force-matched to zero

If an adjustment source is not discoverable, surface that as an explicit note rather than inventing a new ledger entry.

- [ ] **Step 4: Add a read-only API endpoint**

Expose the report through the Alpaca automation API alongside the existing reconcile/verify endpoints. The new endpoint must be read-only and must not trigger writes, retries, or reconciliation mutations.

- [ ] **Step 5: Add parity and canary-oriented tests**

Add tests that validate the report against known fixtures and assert the residual remains visible when broker and local values diverge. Include a production-canary expectation that the report can run safely without mutating data and without requiring ledger migration.

- [ ] **Step 6: Run automation/API tests**

Run:

```bash
go test ./internal/automation ./internal/api -run 'Alpaca|reconcile|Reconciliation' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/automation/alpaca_reconciliation.go internal/api/automation_alpaca_handlers.go internal/automation/*test.go internal/api/*test.go
git commit -m "feat(automation): add alpaca pl reconciliation report"
```

## Task 4: Validate SQL parity and production canary

**Files:**
- Modify: parity tests only; no schema migration files

- [ ] **Step 1: Add SQL parity tests for reconciliation inputs**

Verify repository-derived totals used by the report match direct SQL aggregates for the same fixture data, especially trade counts, fee sums, and open/closed position totals.

- [ ] **Step 2: Run full targeted validation**

Run:

```bash
go test ./internal/portfolio ./internal/repository/postgres ./internal/automation ./internal/api -count=1
```

- [ ] **Step 3: Production canary**

Deploy the read-only diagnostics/reconciliation changes behind existing API routing. Canary the new report endpoint and the updated diagnostics response on production traffic without enabling any write-path behavior or ledger migration.

- [ ] **Step 4: Acceptance gates**

Accept only if:

  - totals are repository-backed and no longer limited to a 100-row sample
  - API consumers still receive compatible sample fields
  - reconciliation report shows broker cash/equity, local P&L, fees, known adjustments when discoverable, and an explicit residual
  - residual is not forced to zero
  - no schema migration or ledger backfill was required

- [ ] **Step 5: Final commit**

```bash
git add internal/portfolio internal/repository/postgres internal/automation internal/api docs/superpowers/plans/2026-07-19-p1-diagnostics-reconciliation.md
git commit -m "feat: add diagnostics reconciliation plan and canary"
```

## Sequencing rule

Do not merge the diagnostics total-change and Alpaca reconciliation report until parity tests pass. Keep the reconciliation endpoint read-only throughout. Do not introduce a ledger table, forced balancing entry, or automatic adjustment synthesis in this phase.
