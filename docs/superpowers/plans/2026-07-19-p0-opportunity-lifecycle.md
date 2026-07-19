# P0 Opportunity Lifecycle Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Stop repeatedly evaluating expired queued opportunities and process the complete current queue fairly rather than only the newest 100 rows.

**Architecture:** Promote due queued rows to `expired` atomically in PostgreSQL before each allocator run. Then materialize the complete remaining queue in one stable earliest-expiry query and allocate that snapshot once. Preserve shadow-mode semantics: current opportunities remain queued, but expired rows become terminal and cannot generate repeated decisions.

**Tech Stack:** Go, pgx/PostgreSQL, existing allocator repositories and job tests.

---

## File map

- Modify `internal/repository/interfaces.go`: add atomic expiry and allocation-snapshot operations.
- Modify `internal/repository/postgres/opportunity.go`: implement expiry and one-statement stable snapshot.
- Modify repository tests.
- Modify `internal/automation/jobs_portfolio_allocator.go`: expire first, load one snapshot, and report counts.
- Modify allocator job tests and repository stubs.

### Task 1: Add atomic expiry to the repository

**Files:**
- Modify: `internal/repository/interfaces.go:384-393`
- Modify: `internal/repository/postgres/opportunity.go:52-88`
- Modify: `internal/repository/postgres/opportunity_test.go`
- Modify: `internal/automation/jobs_portfolio_allocator_test.go`
- Modify: `internal/api/portfolio_allocator_handlers_test.go`

- [ ] **Step 1: Add a failing repository integration test**

Seed one past queued row, one future queued row, and one past selected row. Call `ExpireQueuedBefore(ctx, now)` and assert exactly one row changed; only the past queued row must have status `expired` and reject reason `expired_before_allocation`.

- [ ] **Step 2: Run the test and verify compile failure**

Run: `go test ./internal/repository/postgres -run 'TestOpportunityRepo_ExpireQueuedBefore' -count=1`

Expected: compile failure because the method is missing.

- [ ] **Step 3: Extend the interface and implement one atomic statement**

```go
// OpportunityRepository
ExpireQueuedBefore(ctx context.Context, before time.Time) (int64, error)
```

```go
func (r *OpportunityRepo) ExpireQueuedBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE portfolio_opportunities
		SET status = $1, reject_reason = $2, updated_at = NOW()
		WHERE status = $3 AND expires_at <= $4`,
		domain.OpportunityStatusExpired,
		"expired_before_allocation",
		domain.OpportunityStatusQueued,
		before.UTC(),
	)
	if err != nil { return 0, fmt.Errorf("postgres: expire queued opportunities: %w", err) }
	return tag.RowsAffected(), nil
}
```

Add this compile-safe no-op to API test stubs; the automation stub may initially use the same implementation and becomes behavioral in Task 3:

```go
func (r *portfolioAllocatorOpportunityRepo) ExpireQueuedBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}
```

- [ ] **Step 4: Run repository tests**

Run: `go test ./internal/repository/postgres -run 'Opportunity' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/interfaces.go internal/repository/postgres/opportunity.go internal/repository/postgres/opportunity_test.go internal/automation/jobs_portfolio_allocator_test.go internal/api/portfolio_allocator_handlers_test.go
git commit -m "feat(portfolio): expire queued opportunities atomically"
```

### Task 2: Materialize one stable, fair allocation snapshot

**Files:**
- Modify: `internal/repository/interfaces.go:384-395`
- Modify: `internal/repository/postgres/opportunity.go`
- Modify: `internal/repository/postgres/opportunity_test.go`
- Modify: `internal/automation/jobs_portfolio_allocator_test.go`
- Modify: `internal/api/portfolio_allocator_handlers_test.go`

- [ ] **Step 1: Add a failing repository integration test**

Create 205 queued current opportunities, invoke `ListQueuedForAllocation(ctx, asOf)`, and assert all 205 IDs appear exactly once in earliest-expiry order. Also assert rows expiring at or before `asOf` are excluded.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/repository/postgres -run 'TestOpportunityRepo_ListQueuedForAllocation' -count=1`

Expected: compile failure because the snapshot method does not exist.

- [ ] **Step 3: Add the snapshot contract and query**

```go
// OpportunityRepository
ListQueuedForAllocation(ctx context.Context, asOf time.Time) ([]domain.Opportunity, error)
```

Implement it with `opportunitySelectSQL` and one query:

```sql
WHERE status = 'queued' AND expires_at > $1
ORDER BY expires_at ASC, created_at ASC, id ASC
```

Do not add `LIMIT/OFFSET`; the allocation set must come from one PostgreSQL statement/MVCC snapshot. Keep the general API `List` ordering unchanged.

Add this compile-safe implementation to API and automation test stubs; Task 3 replaces the automation version with filtered/sorted behavior:

```go
func (r *portfolioAllocatorOpportunityRepo) ListQueuedForAllocation(context.Context, time.Time) ([]domain.Opportunity, error) {
	return append([]domain.Opportunity(nil), r.items...), nil
}
```

- [ ] **Step 4: Run repository tests**

Run: `go test ./internal/repository/postgres -run 'Opportunity' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/interfaces.go internal/repository/postgres/opportunity.go internal/repository/postgres/opportunity_test.go internal/automation/jobs_portfolio_allocator_test.go internal/api/portfolio_allocator_handlers_test.go
git commit -m "feat(portfolio): snapshot queued opportunities"
```

### Task 3: Expire first and allocate the materialized snapshot

**Files:**
- Modify: `internal/automation/jobs_portfolio_allocator.go:32-95`
- Modify: `internal/automation/jobs_portfolio_allocator_test.go`

- [ ] **Step 1: Extend the test stub**

Implement `ExpireQueuedBefore` on `portfolioAllocatorOpportunityRepo`; mutate only queued items whose `ExpiresAt` is not after the supplied time and return the mutation count. Implement `ListQueuedForAllocation` to return a copied, expiry-sorted slice of current queued items.

- [ ] **Step 2: Add failing job tests**

Add:
- `TestPortfolioAllocatorJobExpiresDueBeforeAllocation`: an expired queued item produces no allocation decision and becomes expired.
- `TestPortfolioAllocatorJobLoadsCompleteSnapshot`: 205 current queued items produce 205 decisions from one snapshot call.
- `TestPortfolioAllocatorJobReportsLifecycleCounts`: last summary contains `expired`, `queued_loaded`, `evaluated`, and `persisted_decisions`.

- [ ] **Step 3: Run and verify failure**

Run: `go test ./internal/automation -run 'TestPortfolioAllocatorJob(Expires|Loads|Reports)' -count=1`

Expected: FAIL because the job neither expires nor loads the allocation snapshot.

- [ ] **Step 4: Add expiry preflight and snapshot loading**

At the start of `runPortfolioAllocator`:

```go
asOf := time.Now().UTC()
expired, err := o.deps.OpportunityRepo.ExpireQueuedBefore(ctx, asOf)
if err != nil { return fmt.Errorf("portfolio_allocator: expire opportunities: %w", err) }
opportunities, err := o.deps.OpportunityRepo.ListQueuedForAllocation(ctx, asOf)
if err != nil { return fmt.Errorf("portfolio_allocator: snapshot opportunities: %w", err) }
```

Add `expired` and `queued_loaded` to both `SetLastSummary` and structured logs.

- [ ] **Step 5: Run automation tests**

Run: `go test ./internal/automation -run 'PortfolioAllocator' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/automation/jobs_portfolio_allocator.go internal/automation/jobs_portfolio_allocator_test.go
git commit -m "fix(automation): advance opportunity lifecycle"
```

### Task 4: Full verification and production acceptance

- [ ] Run `go test ./internal/repository/postgres ./internal/portfolio ./internal/automation ./internal/api -count=1`.
- [ ] Deploy the app; no schema migration is required because `(status, expires_at)` is already indexed.
- [ ] Trigger `portfolio_allocator` once.
- [ ] Verify `queued AND expires_at <= NOW()` count is zero.
- [ ] Verify the run summary reports roughly 349 expired rows during the first production run and only the current queue as `queued_loaded`.
- [ ] On the second run, verify no additional `shadow_rejected` decisions with reason `expired` are created for the same opportunity IDs.
- [ ] Accept P0 after two scheduled runs with zero expired rows left queued and no repeated expired decisions.
- [ ] Roll back the app image if list latency materially increases; the status transitions are truthful and must not be reverted.

**Concurrency boundary:** Production currently runs one app instance and the orchestrator prevents overlapping execution in-process. Do not scale allocator workers before P1 claim/lease semantics exist.

**Deferred to P1:** claim/lease semantics with `FOR UPDATE SKIP LOCKED` and transactionally persisted decisions for multiple allocator workers.
