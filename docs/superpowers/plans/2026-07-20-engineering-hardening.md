# 2026-07-20 Engineering Hardening Plan

> **For agentic workers:** Execute items 9, 10, and 11 in order. Keep scope narrow, preserve provider-scoped budgets, and do not widen execution safety changes beyond the three targeted hardening tracks below. Start with failing tests, then implementation, then focused verification. Do not edit files outside this plan’s file map.

**Hard preflight (standalone, required before Item 10.2 and Item 11.2):**

- Before creating or applying migration 61, verify `schema_migrations=60` is clean and `internal/repository/postgres/schema_version.go` reports `RequiredSchemaVersion = 60`.
- Abort immediately if either check fails; do not create `000061_allocator_claims.*` until both checks pass.
- Before creating or applying migration 62, verify `schema_migrations=61` is clean and `internal/repository/postgres/schema_version.go` reports `RequiredSchemaVersion = 61`.
- Abort immediately if either check fails; do not create `000062_authoritative_live_fills.*` until both checks pass.

**Goal:** Harden the provider/governance stack in three dependent phases: (9) roll provider governance out to Polygon and Ollama without losing Kalshi behavior, (10) make allocator claims safe across multiple orchestrator instances with compare-and-claim semantics, and (11) make live fill persistence authoritative and atomic for brokers that can emit real fill events.

**Architecture:**

- Reuse `internal/providergovernor` as the neutral provider-scoped budget/retry/cooldown layer.
- Extend Polygon and Ollama client construction sites to opt into the same provider-scoped governor pattern already used by Kalshi, while preserving provider-local budgets and POST safety rules.
- Replace allocator preclaim-by-`UpdateStatus(selected)` with a repository-level claim UoW that atomically transitions `queued -> selected/processing` using CAS/lease ownership semantics, with expired-claim recovery and exactly-once decision/order creation across two orchestrators.
- Extend financial lifecycle persistence with optional authoritative live fill events so live brokers can provide durable fill identity/quantity/price without breaking the existing `Broker` interface, and keep live trading disabled until acceptance gates pass.

**Tech Stack:** Go, PostgreSQL, existing repository harness, migration SQL, existing automation/job tests, provider-specific client tests.

---

## Verified baseline

- `internal/providergovernor/governor.go` already provides `ProviderGovernor`, `CooldownStore`, `RateLimitError`, `ParseRetryAfter`, `RetryAfter`, `Jitter`, and `SleepContext`.
- Polygon client construction currently lives in `internal/data/polygon/client.go` via `NewClient(apiKey string, logger *slog.Logger) *Client`.
- Ollama embedding construction currently lives in `internal/llm/embedding/ollama.go` via `NewOllamaProvider(cfg OllamaConfig) (*OllamaProvider, error)`.
- Kalshi data and execution already consume the governor pattern; the rollout target is to make Polygon and Ollama use the same provider-scoped budget model without changing other providers’ semantics.
- `internal/automation/jobs_portfolio_allocator.go` currently snapshots opportunities, then preclaims paper-selected rows with unconditional `OpportunityRepo.UpdateStatus(..., domain.OpportunityStatusSelected, "")`, which is not safe for multiple orchestrators.
- `internal/repository/interfaces.go` already has `FinancialLifecycleRepository`, `OpportunityRepository`, and the atomic fill/settlement contract, but allocator claim semantics are still coarse-grained.
- `internal/repository/postgres/financial_lifecycle.go` already owns atomic paper/order settlement logic, but live brokers still lack an authoritative fill-event path; `internal/execution/broker.go` remains the compatibility boundary and must stay intact.

## File map

- Modify: `internal/data/polygon/client.go` and the wiring sites that call `NewClient`.
- Modify: `internal/llm/embedding/ollama.go` and the wiring sites that call `NewOllamaProvider`.
- Modify: `internal/providergovernor/governor.go` and `internal/providergovernor/governor_test.go` only if the shared governor needs a small generalization for Polygon/Ollama budget naming or retry policy hooks.
- Modify: `internal/data/polygon/*_test.go`, `internal/llm/embedding/*_test.go`, and any provider-composition tests that prove budgets remain provider-scoped.
- Modify: `internal/repository/interfaces.go`.
- Modify: `internal/repository/postgres/opportunity.go`.
- Modify: `internal/repository/postgres/opportunity_test.go`.
- Modify: `internal/automation/jobs_portfolio_allocator.go`.
- Modify: `internal/automation/jobs_portfolio_allocator_test.go`.
- Create migrations: `migrations/000061_allocator_claims.up.sql` and `migrations/000061_allocator_claims.down.sql` for allocator lease/CAS; `migrations/000062_authoritative_live_fills.up.sql` and `migrations/000062_authoritative_live_fills.down.sql` for live fill idempotency/event state.
- Modify: `internal/execution/broker.go` only if a backward-compatible optional live-fill extension point is added.
- Modify: `internal/repository/postgres/financial_lifecycle.go`.
- Modify: `internal/repository/postgres/financial_lifecycle_test.go`.
- Modify: `internal/execution/order_manager.go` and live broker adapters only as needed to wire authoritative live fill events.
- Modify: `internal/execution/alpaca/*_test.go`, `internal/execution/kalshi/*_test.go`, and any live-fill reconciliation tests.
- Create: `internal/domain/live_fill.go` to define `domain.LiveFillEvent`.
- Modify: `internal/metrics/metrics.go` and `internal/metrics/metrics_test.go` for live-fill mismatch/duplicate/shadow-drift metrics.
- Modify: `internal/repository/postgres/schema_version.go` and schema-version tests so `RequiredSchemaVersion` is updated to `61` for item 10 and `62` for item 11, with tests covering both increments and no migration gaps.

---

## Item 9: Provider governance rollout to Polygon and Ollama

**Outcome:** Polygon and Ollama use the same provider-scoped governor/cooldown/budget model as Kalshi, while preserving provider-local budgets, typed 429 handling, retry-after respect, and POST safety.

### Task 9.1: Prove Polygon/Ollama are not yet governed consistently

**Files:**
- Modify: `internal/data/polygon/*_test.go`
- Modify: `internal/llm/embedding/*_test.go`
- Modify: provider composition tests where `polygon.NewClient(...)` and `embedding.NewOllamaProvider(...)` are constructed

- [ ] **Step 1: Add failing tests for provider-scoped budget sharing**

Write tests that construct:

1. a Polygon data client using `polygon.NewClient(apiKey, logger)` and an injected `providergovernor.ProviderGovernor{Provider: "polygon", ...}`;
2. an Ollama embedding client using `embedding.NewOllamaProvider(embedding.OllamaConfig{BaseURL: embedding.DefaultBaseURL, Model: embedding.DefaultModel, APIKey: apiKey, Timeout: embedding.DefaultTimeout, BatchSize: embedding.DefaultBatchSize, HTTPClient: client})` plus an injected `providergovernor.ProviderGovernor{Provider: "ollama", ...}`;
3. a Kalshi client using its existing governor wiring as a control.

The tests must assert that:

- Polygon requests contend only with Polygon requests, not Kalshi/Ollama.
- Ollama requests contend only with Ollama requests, not Polygon/Kalshi.
- each client preserves its own POST safety behavior and does not inherit a global retry policy.

Example shape:

```go
govPolygon := &providergovernor.ProviderGovernor{Provider: "polygon", Limiter: limiterA, Cooldown: cooldownA}
govOllama := &providergovernor.ProviderGovernor{Provider: "ollama", Limiter: limiterB, Cooldown: cooldownB}

polygonClient.SetGovernor(govPolygon)
ollamaProvider.SetGovernor(govOllama)
```

- [ ] **Step 2: Run the targeted tests and verify current coverage is insufficient**

Run:

```bash
go test ./internal/data/polygon ./internal/llm/embedding -count=1
```

Expected: at least one test fails or compiles incompletely because the current wiring is not fully provider-scoped across both targets.

### Task 9.2: Extend composition to use the shared governor

**Files:**
- Modify: `internal/data/polygon/client.go`
- Modify: `internal/llm/embedding/ollama.go`
- Modify: the composition/bootstrap files that call `polygon.NewClient` and `embedding.NewOllamaProvider`
- Modify: `internal/providergovernor/governor.go` only if needed for a small neutral helper

- [ ] **Step 1: Keep the provider-governor contract neutral and reusable**

If necessary, add a small helper/alias so both clients can configure budgets without importing provider-specific internals from each other. Keep it neutral and avoid import cycles.

If a new type is introduced, define the full signature before use. Example:

```go
type ProviderGovernor struct {
    Provider string
    Limiter  Limiter
    Cooldown CooldownStore
    Sleeper  Sleeper
    Clock    func() time.Time
    Rand     *rand.Rand
}
```

- [ ] **Step 2: Wire Polygon and Ollama through the governor**

Add governor setters/constructor parameters in the existing client construction path. The implementation must:

- continue to work when no governor is provided (nil-safe defaults);
- call `Reserve(ctx)` before requests;
- preserve provider-scoped cooldown state;
- preserve POST safety: retries must not replay non-idempotent upstream operations after acceptance.

Illustrative pattern:

```go
if c.governor != nil {
    if err := c.governor.Reserve(ctx); err != nil { return nil, err }
}
```

- [ ] **Step 3: Add typed rate-limit and retry tests for both providers**

Add table-driven tests proving that:

- a 429 with `Retry-After` becomes a typed `providergovernor.RateLimitError`;
- retry-after wins over base backoff;
- context cancellation stops retries;
- POST requests are not retried if the request may have been accepted upstream;
- provider-scoped cooldown state is shared within a provider and isolated across providers.

Suggested test names:

- `TestPolygonClient_UsesProviderGovernor`
- `TestPolygonClient_PostSafetyDoesNotReplayAcceptedRequests`
- `TestOllamaProvider_UsesProviderGovernor`
- `TestOllamaProvider_RetryAfterOverridesBackoff`

- [ ] **Step 4: Run the provider tests**

Run:

```bash
go test ./internal/data/polygon ./internal/llm/embedding ./internal/providergovernor -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/providergovernor/governor.go internal/providergovernor/governor_test.go internal/data/polygon/client.go internal/data/polygon/*_test.go internal/llm/embedding/ollama.go internal/llm/embedding/*_test.go cmd/tradingagent/runtime.go cmd/tradingagent/runtime_test.go cmd/tradingagent/schema_version_sync_test.go cmd/tradingagent/main.go cmd/tradingagent/main_test.go cmd/tradingagent/options_bootstrap.go cmd/tradingagent/options_bootstrap_test.go cmd/tradingagent/options_runtime_test.go cmd/tradingagent/polymarket_bootstrap.go cmd/tradingagent/prod_strategy_runner.go cmd/tradingagent/prod_strategy_runner_test.go cmd/tradingagent/llm_metrics_provider.go cmd/tradingagent/llm_metrics_provider_test.go cmd/tradingagent/debate_timeout_provider.go cmd/tradingagent/debate_timeout_provider_test.go internal/llm/composition.go internal/llm/composition_test.go
git commit -m "feat(provider): roll out governance to polygon and ollama"
```

### Task 9.3: Rollout and rollback gates

- [ ] Enable Polygon and Ollama governor wiring in staging first.
- [ ] Verify provider budgets remain scoped independently from Kalshi and from each other.
- [ ] Verify no increase in unsafe POST retries.
- [ ] Roll back by removing the new governor wiring only; leave Kalshi and other providers unchanged.

---

## Item 10: Allocator compare-and-claim for multi-instance safety

**Outcome:** Two orchestrator instances can run allocator safely without duplicate claims, duplicate decisions, or duplicate order creation. The allocator must compare-and-claim queued opportunities atomically and recover expired claims.

### Task 10.1: Prove unconditional preclaim is unsafe across instances

**Files:**
- Modify: `internal/automation/jobs_portfolio_allocator_test.go`
- Modify: `internal/repository/postgres/opportunity_test.go`
- Modify: allocator repository stubs

- [ ] **Step 1: Add failing tests for duplicate orchestrator claims**

Create a test harness that simulates two orchestrators loading the same queued snapshot. The first orchestrator claims a row; the second should not be able to claim the same row unless the lease is expired or the state is still eligible under CAS.

Required assertions:

- `queued -> selected/processing` transition happens atomically;
- a second concurrent claim sees zero rows updated or receives a conflict that the job handles safely;
- no duplicate `AllocationDecisionRepo.Create` and no duplicate order creation occurs;
- paper mode and shadow mode preserve their current business logic.

Suggested failing test names:

- `TestPortfolioAllocatorJob_CompareAndClaimPreventsDoubleClaim`
- `TestOpportunityRepo_ClaimQueuedOpportunityCAS`
- `TestOpportunityRepo_ReclaimsExpiredProcessingOpportunity`

- [ ] **Step 2: Run the allocator tests and confirm current preclaim is insufficient**

Run:

```bash
go test ./internal/automation -run 'TestPortfolioAllocatorJob_(CompareAndClaim|ReclaimsExpired)' -count=1
```

Expected: FAIL because the current `UpdateStatus(...selected...)` preclaim is unconditional and not a compare-and-claim.

### Task 10.2: Add repository-level CAS claim semantics

**Files:**
- Modify: `internal/repository/interfaces.go`
- Modify: `internal/repository/postgres/opportunity.go`
- Modify: `internal/repository/postgres/opportunity_test.go`
- Create/modify migration `000061_allocator_claims` for claim owner / lease / recovery metadata

- [ ] **Step 1: Extend the repository contract for claim ownership**

Add a narrow claim contract instead of threading transactions into automation.

```go
type OpportunityRepository interface {
    ClaimQueuedForAllocation(ctx context.Context, id uuid.UUID, owner string, leaseUntil time.Time) (bool, error)
    ReclaimExpiredAllocationClaims(ctx context.Context, before time.Time) (int64, error)
    // existing methods preserved
}
```

If a single-row claim API is not enough for the final allocator flow, add a batch variant that still preserves CAS semantics and returns claimed IDs.

Required data model additions:

- claim owner / allocator instance identifier;
- claim lease expiry / renewal timestamp;
- claim state that distinguishes `queued`, `selected`, and `processing` (or equivalent) without changing the existing domain status semantics for other flows;
- recovery columns/indexes to find expired claims efficiently.

- [ ] **Step 2: Implement atomic compare-and-claim in PostgreSQL**

Use one of these acceptable strategies:

1. `UPDATE ... WHERE status = 'queued' AND (claim_owner IS NULL OR claim_lease_until < now()) RETURNING ...` CAS;
2. `SELECT ... FOR UPDATE SKIP LOCKED` over the candidate set followed by a state transition inside the same transaction;
3. keyset pagination plus transaction-local claim updates;
4. a transaction/advisory-lock hybrid that still guarantees exactly-once claim ownership.

The implementation must guarantee:

- exactly-once allocation decision/order creation across two orchestrators;
- no duplicate claims while a lease is valid;
- safe recovery when a claim owner crashes or misses its lease;
- expired claims can be reclaimed deterministically;
- queued rows not eligible for claim are skipped without blocking the whole batch.

Example shape:

```go
tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{})
// SELECT candidate rows ORDER BY ... FOR UPDATE SKIP LOCKED
// UPDATE selected row with claim_owner/claim_lease_until/state
// COMMIT only after the claim is durable
```

- [ ] **Step 3: Add migration coverage**

Hard preflight before migration 61: confirm `schema_migrations=60` is clean and `internal/repository/postgres/schema_version.go` is still `RequiredSchemaVersion = 60`; abort if not.

Migration up should add the claim metadata and supporting indexes; down should remove them. Include preflight/validation if needed to avoid duplicate states before adding unique constraints. Add a migration test that verifies:

- queued rows can be claimed once;
- expired processing claims can be reclaimed;
- non-expired claims are not stolen;
- batch acquisition skips locked rows.

- [ ] **Step 4: Add allocator job tests for two orchestrators**

Add job-level tests asserting:

- one orchestrator claims and persists decisions/orders exactly once;
- a second orchestrator on the same snapshot does not duplicate work;
- the job remains correct if the claim is lost or expires before persistence;
- the summary reports claimed/expired/reclaimed counts.

Illustrative assertions:

```go
if got := repo.ClaimCalls(); got != 1 { t.Fatalf("claim calls = %d, want 1", got) }
if got := decisionRepo.CreatedCount(); got != 1 { t.Fatalf("decisions = %d, want 1", got) }
if got := orderRepo.CreatedCount(); got != 1 { t.Fatalf("orders = %d, want 1", got) }
```

- [ ] **Step 5: Run the allocator test suite**

Run:

```bash
go test ./internal/repository/postgres -run 'Opportunity' -count=1
go test ./internal/automation -run 'PortfolioAllocator' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/repository/interfaces.go internal/repository/postgres/opportunity.go internal/repository/postgres/opportunity_test.go internal/repository/postgres/schema_version.go cmd/tradingagent/schema_version_sync_test.go internal/automation/jobs_portfolio_allocator.go internal/automation/jobs_portfolio_allocator_test.go migrations/000061_allocator_claims.up.sql migrations/000061_allocator_claims.down.sql migrations/000061_allocator_claims_test.go
git commit -m "feat(portfolio): add compare-and-claim allocator"
```

### Task 10.3: Rollout and rollback gates

- [ ] Enable claim-based allocation behind a deploy flag or configuration gate.
- [ ] Verify two orchestrators can run concurrently without duplicate decisions/orders.
- [ ] Verify claim expiry recovery after a forced crash.
- [ ] Roll back by disabling the claim path and reverting to single-instance allocator behavior only if required.

---

## Item 11: Authoritative atomic live fill handling

**Outcome:** Live execution can persist authoritative fills atomically when a broker emits fill events with stable external fill IDs, while preserving the existing `Broker` interface and keeping live trading disabled until acceptance.

### Task 11.1: Prove current live fill persistence is paper-only

**Files:**
- Modify: `internal/execution/order_manager_test.go`
- Modify: `internal/execution/alpaca/*_test.go`
- Modify: `internal/execution/kalshi/*_test.go`
- Modify: `internal/repository/postgres/financial_lifecycle_test.go`

- [ ] **Step 1: Add failing tests for authoritative live fills**

Write tests that show the current atomic financial lifecycle path cannot fully represent live fills because broker status adapters do not supply authoritative quantity/price and stable external fill identity. The tests should cover:

- partial fills arriving in multiple events;
- duplicate live fill delivery with the same external fill ID;
- reconciliation of cumulative quantity/price across fills;
- shadow comparison against broker-reported status updates;
- broker-specific acceptance for Alpaca and Kalshi.

Suggested test names:

- `TestOrderManager_HandleLiveFill_UsesAuthoritativeFillEvent`
- `TestFinancialLifecycle_ApplyLiveFill_IdempotentByExternalFillID`
- `TestAlpacaBroker_LiveFillEventReconciliation`
- `TestKalshiBroker_LiveFillEventReconciliation`

- [ ] **Step 2: Run the tests and confirm current code is insufficient**

Run:

```bash
go test ./internal/execution ./internal/repository/postgres -run 'LiveFill|Authoritative|Reconciliation' -count=1
```

Expected: FAIL because the current live path lacks an authoritative fill-event interface and stable external fill IDs.

### Task 11.2: Add an optional authoritative fill-event interface

**Files:**
- Modify: `internal/execution/broker.go`
- Modify: `internal/execution/order_manager.go`
- Modify: live broker adapters (`internal/execution/alpaca/*`, `internal/execution/kalshi/*`)
- Modify: `internal/repository/interfaces.go` if a new repository method is needed
- Modify: `internal/repository/postgres/financial_lifecycle.go`

- [ ] **Step 1: Add a backward-compatible optional interface**

Do **not** break existing `Broker`. Add a separate optional interface for brokers that can emit authoritative fill data.

```go
type LiveFillEventProvider interface {
    FillEvents(ctx context.Context, since time.Time) ([]domain.LiveFillEvent, error)
}
```

The fill event model must include at minimum:

- stable external fill ID;
- order/execution reference;
- authoritative quantity;
- authoritative price;
- executed timestamp;
- optional fee/side metadata;
- broker/provider identifier;
- partial sequence ID so multiple events for the same fill can be ordered and deduplicated without synthesizing fills.

- [ ] **Step 2: Extend financial lifecycle persistence for live fills**

Hard preflight before migration 62: confirm `schema_migrations=61` is clean and `internal/repository/postgres/schema_version.go` is still `RequiredSchemaVersion = 61`; abort if not.

Add a live-fill UoW that atomically persists:

- order fill state,
- partial-fill accumulation,
- trade row(s),
- idempotency row keyed by stable external fill ID,
- optional reconciliation state.

Requirements:

- support partial fills across multiple events;
- keep sequence/idempotency transactional;
- return the original persisted result on duplicate delivery;
- reject conflicting payloads for the same external fill ID;
- remain atomic across order/position/trade rows;
- keep paper fill behavior intact;
- never synthesize a fill from broker status when an authoritative live fill event is unavailable; only brokers with the optional fill-event interface may participate in live-fill persistence.

Illustrative contract:

```go
type LiveFillInput struct {
    IdempotencyKey string
    ExternalFillID  string
    PartialSequenceID int64
    OrderID         uuid.UUID
    Quantity        float64
    Price           float64
    ExecutedAt      time.Time
    Broker          string
}
```

Validation must reject non-finite or non-positive quantity/price values in the same manner as the repository layer.

If the repo needs a new method, add it to `FinancialLifecycleRepository` without changing the existing `Broker` interface.

- [ ] **Step 3: Reconcile status-adapter shadow comparisons**

Live fill handling must compare authoritative fill-event data against broker status adapters in shadow mode and emit mismatches, but the authoritative event remains the source of truth for writes.

Add tests proving:

- a status adapter may disagree on quantity/price but does not overwrite authoritative fill state;
- shadow comparison logs or metrics surface the mismatch;
- acceptance criteria can block live enablement if drift exceeds threshold.

Metrics plan: emit provider/broker drift counters using existing naming conventions, e.g. `tradingagent_execution_live_fill_mismatch_total`, `tradingagent_execution_live_fill_duplicate_total`, and `tradingagent_execution_live_fill_shadow_drift_total`, with labels `broker`, `provider`, `field`, and `reason` as appropriate.

- [ ] **Step 4: Add broker-specific acceptance tests**

Add focused tests for Alpaca and Kalshi adapters proving:

- duplicate external fill IDs are idempotent;
- partial fills aggregate correctly;
- the final order status transitions are atomic;
- live fill events do not require changing the `Broker` interface contract;
- only adapters that implement `LiveFillEventProvider` can enable authoritative live-fill writes.

### Task 11.3: Keep live trading disabled until acceptance passes

**Files:**
- Modify: `internal/automation/orchestrator.go` or live-trading gating/config code as needed
- Modify: any live-trading acceptance tests
- Modify: docs/runbooks only if the rollout gate needs a note; otherwise keep scope in this plan

- [ ] **Step 1: Add gating tests**

Add a test asserting live trading remains disabled until:

- the authoritative live fill path is implemented,
- broker-specific Alpaca and Kalshi tests pass,
- shadow comparison is enabled and clean,
- exactly-once/idempotency semantics are proven for duplicate fill IDs,
- no unsafe fallback to paper-only assumptions remains for live brokers,
- only brokers with `LiveFillEventProvider` are eligible for live-fill authority.

- [ ] **Step 2: Run the targeted live-fill suite**

Run:

```bash
go test ./internal/execution ./internal/repository/postgres ./internal/automation -run 'LiveFill|Authoritative|Reconciliation|Acceptance' -count=1
```

Expected: PASS only after the new live-fill UoW and gating logic exist.

- [ ] **Step 3: Commit**

```bash
git add internal/execution/broker.go internal/execution/order_manager.go internal/execution/alpaca/*_test.go internal/execution/kalshi/*_test.go internal/repository/interfaces.go internal/repository/postgres/financial_lifecycle.go internal/repository/postgres/financial_lifecycle_test.go internal/repository/postgres/schema_version.go cmd/tradingagent/schema_version_sync_test.go internal/domain/live_fill.go internal/metrics/metrics.go internal/metrics/metrics_test.go internal/automation/orchestrator.go migrations/000062_authoritative_live_fills.up.sql migrations/000062_authoritative_live_fills.down.sql migrations/000062_authoritative_live_fills_test.go
git commit -m "feat(execution): add authoritative live fill handling"
```

---

## Dependency order

1. **Provider governance rollout** (Item 9): establish shared provider-scoped budgets and POST safety for Polygon/Ollama.
2. **Allocator compare-and-claim** (Item 10): make multi-instance opportunity allocation safe after provider budgets are stable.
3. **Authoritative live fills** (Item 11): only after provider governance and allocator safety are in place, because live fill persistence depends on the same durability/rollback discipline.

---

## Rollout / rollback / security gates

- Roll out in order: provider governance -> allocator claim path -> live-fill authority.
- Keep live trading disabled until Item 11 acceptance passes.
- Roll back each item independently:
  - Item 9: remove Polygon/Ollama governor wiring only.
  - Item 10: disable claim-based allocator path and revert to single-instance behavior.
  - Item 11: keep live brokers disabled or shadow-only; do not enable live trading if authoritative fill events or idempotency fail.
- Security/operational gates:
  - preserve provider-scoped budgets and do not introduce a global retry policy;
  - keep POST safety rules explicit and conservative;
  - ensure allocator claims cannot be stolen before lease expiry;
  - ensure live fill writes are atomic and replay-safe.

---

## Suggested verification commands

```bash
go test ./internal/providergovernor ./internal/data/polygon ./internal/llm/embedding -count=1
go test ./internal/repository/postgres -run 'Opportunity|FinancialLifecycle' -count=1
go test ./internal/automation -run 'PortfolioAllocator|LiveFill|Acceptance' -count=1
go test ./internal/execution ./internal/repository/postgres ./internal/automation -count=1
```

---

## Commit sequence

1. `feat(provider): roll out governance to polygon and ollama`
2. `feat(portfolio): add compare-and-claim allocator`
3. `feat(execution): add authoritative live fill handling`
