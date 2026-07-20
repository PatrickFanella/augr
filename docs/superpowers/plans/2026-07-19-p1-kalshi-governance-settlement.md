# P1 Kalshi Governance + Settlement Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Use the exact checkbox steps in order, keep the Kalshi settlement job disabled until the dry-run gate passes, and do not widen scope to unrelated provider changes.

**Goal:** Add provider-scoped Kalshi governance so data and execution share one budget, surface typed 429/Retry-After failures, add bounded jittered retries with cancellation and POST safety, instrument metrics, rehearse settlement in dry-run, then re-enable settlement only after the 20-run acceptance gate.

**Architecture:** Introduce a Kalshi-specific governor that can be shared by both `internal/data/kalshi/client.go` and `internal/execution/kalshi/live_client.go`. The governor must understand provider-scoped tokens, typed upstream rate-limit responses, and per-attempt retry policy. Settlement should first run in dry-run mode while the job remains disabled, using a manual `RunJob` invocation (or a separate dry-run endpoint/job) that bypasses enabled-state checks, emit explicit metrics and summary counts, and remain disabled by default until the controlled re-enable gate is met.

**Tech Stack:** Go, Prometheus metrics, existing automation orchestrator, existing config loader, existing Kalshi clients/jobs/tests.

---

## Verified baseline

- `internal/data/global_limiter.go` already exposes a shared limiter hook, but it is only a bare pointer and not Kalshi-scoped.
- Kalshi data and execution use separate HTTP clients: `internal/data/kalshi/client.go` and `internal/execution/kalshi/live_client.go`.
- Kalshi settlement is a registered automation job in `internal/automation/jobs_kalshi_settlement.go` and is already easy to keep disabled via `internal/api/automation_handlers.go` / `internal/automation/orchestrator.go`; dry-run should be exercised without enabling the job.
- Metrics live in `internal/metrics/metrics.go`; config lives in `internal/config/config.go`.
- The safe path is provider-scoped: do not add a global retry policy that changes Polygon/Ollama behavior.

## File map

- Modify: `internal/data/global_limiter.go`
- Modify: `internal/kalshi/governor/` (or another neutral shared package for provider-scoped types)
- Modify: `internal/data/kalshi/client.go`
- Modify: `internal/execution/kalshi/live_client.go`
- Modify: `internal/automation/jobs_kalshi_settlement.go`
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/config/config.go`
- Modify tests under `internal/data/kalshi`, `internal/execution/kalshi`, `internal/automation`, `internal/metrics`, and `internal/config`

## Task 1: Make Kalshi governance provider-scoped and shared

**Files:**
- Modify: `internal/data/global_limiter.go`
- Modify: `internal/data/kalshi/client.go`
- Modify: `internal/execution/kalshi/live_client.go`
- Test: `internal/data/kalshi/*_test.go`
- Test: `internal/execution/kalshi/*_test.go`

- [ ] **Step 1: Add failing tests for a shared Kalshi governor**

Add tests proving both Kalshi clients can accept the same governor instance and that the limiter is provider-scoped rather than process-wide. Include one test that sets a shared Kalshi budget and verifies data-client requests and execution-client requests contend for the same budget, while a non-Kalshi limiter remains untouched.

```go
gov := data.NewProviderGovernor("kalshi", 2)
dataClient.SetGovernor(gov)
execClient.SetGovernor(gov)
```

- [ ] **Step 2: Run the test and verify the current API is insufficient**

Run: `go test ./internal/data/kalshi ./internal/execution/kalshi -count=1`

Expected: compile or assertion failure because the Kalshi clients do not yet share a typed governor from a neutral shared package.

- [ ] **Step 3: Introduce a provider-scoped governor and wire both clients**

```go
// package governor (neutral shared package, not under data or execution)
type ProviderGovernor struct {
    Provider string
    Limiter  *data.RateLimiter
}

func (g *ProviderGovernor) Reserve(ctx context.Context) error
func (g *ProviderGovernor) RetryAfter(err error) (time.Duration, bool)
```

Wire the governor into both Kalshi clients; keep the existing shared limiter hook as a compatibility layer, but resolve Kalshi through the new provider-scoped governor first.

- [ ] **Step 4: Run the Kalshi client tests**

Run: `go test ./internal/data/kalshi ./internal/execution/kalshi -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/providergovernor/governor.go internal/providergovernor/governor_test.go internal/data/global_limiter.go internal/data/kalshi/client.go internal/data/kalshi/client_test.go internal/execution/kalshi/live_client.go internal/execution/kalshi/live_client_test.go
git commit -m "feat(kalshi): add shared provider governor"
```

## Task 2: Add typed 429 and bounded retry handling

**Files:**
- Modify: `internal/data/kalshi/client.go`
- Modify: `internal/execution/kalshi/live_client.go`
- Test: `internal/data/kalshi/*_test.go`
- Test: `internal/execution/kalshi/*_test.go`

- [ ] **Step 1: Add failing tests for typed 429 errors and POST safety**

Add tests that mock a 429 response with a `Retry-After` header and assert the client returns a typed error containing the status code, provider name, retry-after duration, and response body. Add a POST test that confirms retries only happen for safe-to-retry failures and never repeat a POST after a request body may have been accepted upstream.

```go
var rateErr *kalshi.RateLimitError
if !errors.As(err, &rateErr) || rateErr.RetryAfter <= 0 {
    t.Fatalf("expected typed retry-after error")
}
```

- [ ] **Step 2: Run the tests and verify the current behavior is untyped**

Run: `go test ./internal/data/kalshi ./internal/execution/kalshi -run 'RateLimit|RetryAfter|PostSafety' -count=1`

Expected: FAIL because 429s are currently string-formatted and retries are not policy-aware.

- [ ] **Step 3: Add typed errors, jitter, cancellation, and bounded retries**

```go
type RateLimitError struct {
    Provider   string
    StatusCode int
    RetryAfter time.Duration
    Body       string
}

for attempt := 0; attempt < maxAttempts; attempt++ {
    err := doOnce()
    if !shouldRetry(err, method) { return err }
    wait := jitter(backoffForAttempt(attempt), 0.2)
    if retryAfter, ok := retryAfterFromError(err); ok && retryAfter > wait {
        wait = retryAfter
    }
    if err := sleepContext(ctx, wait); err != nil { return err }
}
```

Requirements:
- honor `context.Context` cancellation during backoff and before each attempt
- never retry non-idempotent POSTs unless the upstream error is definitively pre-flight and unaccepted
- cap retries with a small fixed maximum per request
- use jitter so concurrent callers do not re-align

- [ ] **Step 4: Run the Kalshi client tests**

Run: `go test ./internal/data/kalshi ./internal/execution/kalshi -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/providergovernor/error.go internal/providergovernor/retry.go internal/providergovernor/retry_test.go internal/data/kalshi/client.go internal/data/kalshi/client_test.go internal/execution/kalshi/live_client.go internal/execution/kalshi/live_client_test.go
git commit -m "fix(kalshi): type 429 retries safely"
```

## Task 3: Add metrics and config for Kalshi governance

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/config/config.go`
- Test: `internal/metrics/metrics_test.go`
- Test: `internal/config/*_test.go`

- [ ] **Step 1: Add failing tests for metrics and config surfaces**

Add tests asserting new metrics are registered and emitted for Kalshi retry attempts, retry-after waits, 429 responses, and settlement dry-run results. Add config tests for the new Kalshi governance knobs (provider-scoped attempts/backoff/jitter and dry-run gate defaults). Include a test that dry-run bookkeeping persists the 20-run gate state/results durably rather than relying on an in-memory `RunCount`.

```go
m.RecordKalshiRetry("data")
m.RecordKalshiRetry("execution")
m.RecordKalshiRetryAfterSeconds(12.5)
m.RecordKalshiSettlementRun("dry_run")
```

- [ ] **Step 2: Run the tests and verify the new names are absent**

Run: `go test ./internal/metrics ./internal/config -count=1`

Expected: FAIL until the new metrics and config fields exist.

- [ ] **Step 3: Add the metrics and config fields**

Add counters/gauges/histograms for:
- Kalshi 429s by client type (`data`, `execution`)
- Kalshi retry attempts
- Kalshi retry-after seconds
- Kalshi settlement dry-run outcomes
- Kalshi settlement disable/enable transitions

Add config defaults for:
- shared Kalshi provider budget
- max retries
- base backoff
- max backoff
- jitter ratio
- settlement dry-run enable flag
- controlled re-enable gate size (20 runs)
- durable gate state/result storage for dry-run runs and re-enable eligibility

- [ ] **Step 4: Run metrics/config tests**

Run: `go test ./internal/metrics ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/metrics.go internal/metrics/metrics_test.go internal/config/config.go internal/config/config_test.go internal/config/validate.go internal/config/validate_test.go
git commit -m "feat(obs): add kalshi governance metrics"
```

## Task 4: Add settlement dry-run and controlled re-enable gate

**Files:**
- Modify: `internal/automation/jobs_kalshi_settlement.go`
- Modify: `internal/automation/orchestrator.go` only if needed for status/summary wiring
- Test: `internal/automation/*kalshi*test.go`

- [ ] **Step 1: Add failing tests for dry-run behavior and the 20-run gate**

Add tests proving the settlement job can run in dry-run mode via manual `RunJob` (or an explicit dry-run endpoint/job), records what it would settle, does not call the settler, and remains disabled by default. Add a gate test that requires 20 consecutive dry-run runs without new failures before a controlled re-enable path returns true, and persist the gate state/results durably instead of using an in-memory `RunCount`.

```go
o.SetLastSummary("kalshi_settlement", map[string]int{"fetched": 10, "resolved": 3, "dry_run": 3})
o.SaveGateState("kalshi_settlement", gateState)
```

- [ ] **Step 2: Run the tests and verify dry-run support is missing**

Run: `go test ./internal/automation -run 'KalshiSettlement|SettlementDryRun|ReenableGate' -count=1`

Expected: FAIL because the job currently settles directly and has no gate state.

- [ ] **Step 3: Implement dry-run settlement and gate bookkeeping**

```go
if o.deps.KalshiSettlementDryRun {
    dryRun++
    continue
}
settled, err := o.deps.PredictionSettler.SettleMarket(...)
```

Behavior:
- keep `kalshi_settlement` disabled by default via actual registration/config behavior
- dry-run must list markets, classify would-settle candidates, and emit summary counts
- dry-run must be invokable while disabled (manual `RunJob` bypass or explicit dry-run endpoint/job)
- a controlled re-enable requires 20 successful dry-run runs with no new errors and no unexpected settlement drift, with gate state/results persisted durably
- keep the job disabled if 429s persist or if the gate has not been satisfied

- [ ] **Step 4: Run automation tests**

Run: `go test ./internal/automation -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/jobs_kalshi_settlement.go internal/automation/jobs_kalshi_settlement_test.go internal/automation/orchestrator.go internal/automation/orchestrator_test.go
git commit -m "feat(automation): dry-run kalshi settlement gate"
```

## Task 5: Re-enable safely and validate rollback

**Files:**
- Update docs/runbook only if needed later; do not change now unless tests require it.

- [ ] **Step 1: Run the full Kalshi-focused test set**

Run: `go test ./internal/data/kalshi ./internal/execution/kalshi ./internal/automation ./internal/metrics ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 2: Exercise dry-run settlement in production-like mode**

Invoke dry-run only through the bypass path or explicit dry-run endpoint/job. Confirm the job stays disabled by default, reports `dry_run` summaries, persists gate state/results, and increments the new metrics without settling anything.

- [ ] **Step 3: Controlled re-enable**

After 20 consecutive dry-run runs succeed, enable live settlement for one controlled window, then verify:
- no unexpected 429 storms
- retries respect `Retry-After`
- cancellation stops retries promptly
- POSTs are never replayed unsafely
- settlement counts match the dry-run projections within the expected window

- [ ] **Step 4: Rollback plan**

If any gate fails, immediately disable `kalshi_settlement` again and revert to dry-run mode. Rollback is configuration-only first; code rollback is the next step if the typed error/retry path causes regressions. Keep provider-scoped behavior compatible with existing non-Kalshi providers by leaving their clients and limiters unchanged.

- [ ] **Step 5: Final acceptance commit**

```bash
git add internal/providergovernor/governor.go internal/providergovernor/governor_test.go internal/providergovernor/error.go internal/providergovernor/retry.go internal/providergovernor/retry_test.go internal/data/global_limiter.go internal/data/kalshi/client.go internal/data/kalshi/client_test.go internal/execution/kalshi/live_client.go internal/execution/kalshi/live_client_test.go internal/automation/jobs_kalshi_settlement.go internal/automation/jobs_kalshi_settlement_test.go internal/automation/orchestrator.go internal/automation/orchestrator_test.go internal/metrics/metrics.go internal/metrics/metrics_test.go internal/config/config.go internal/config/config_test.go internal/config/validate.go internal/config/validate_test.go docs/superpowers/plans/2026-07-19-p1-kalshi-governance-settlement.md
git commit -m "chore(kalshi): complete governance settlement hardening"
```

## Compatibility notes

- Existing Kalshi client call sites must continue to compile; add defaults so the new governor is optional and nil-safe.
- Put shared provider-governor types in a neutral package to avoid import cycles between provider data and execution packages.
- Preserve the shared limiter compatibility path while Kalshi moves to provider-scoped governance.
- Do not change Polygon/Ollama retry semantics.
- Do not enable live settlement until the dry-run gate and 20-run acceptance gate pass, and ensure the disabled-by-default registration/config remains the source of truth.

## Rollback notes

- Primary rollback: disable the job and switch back to dry-run.
- Secondary rollback: remove the governor wiring from Kalshi clients only.
- Tertiary rollback: revert the metric/config additions if they cause startup or scrape issues.
- No data backfill or settlement repair should be attempted as part of rollback.
