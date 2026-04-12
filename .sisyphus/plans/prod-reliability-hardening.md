# Production Reliability Hardening — Backend Runtime + Control Plane

## TL;DR

> **Quick Summary**: Fix remaining reliability gaps from PRD #605 — LLM fallback on DeadlineExceeded, signal parse failure metrics, Polymarket side normalization at DB-insert layer, metrics threading to scheduler/evaluator, automation health endpoint, and docs truth pass. Module 1 (stale-run reconciler) already complete.
>
> **Deliverables**:
>
> - Fixed `fallback.go` DeadlineExceeded → attempts secondary provider
> - Fixed `retry.go` DeadlineExceeded → non-retryable (fail fast to fallback)
> - New metrics: `llm_fallback_total`, `signal_parse_failures_total`, `automation_job_errors_total`, `scheduler_tick_total`
> - Signal evaluator parse failure metric + `SIGNAL_FALLBACK_MODE` env var
> - Polymarket side normalization in DB-insert path
> - `/api/v1/automation/health` endpoint
> - Metrics threaded to scheduler + signal evaluator
> - Docs audit: `configuration.md`, `implementation-board.md`, `phase-7-execution-paths.md`
>
> **Estimated Effort**: Large
> **Parallel Execution**: YES — 4 waves
> **Critical Path**: T1 → T3/T4/T5/T6/T7/T8 → T9 → T12-T15 (frontend/LLM/DB) → T16-T18 → FINAL

---

## Context

### Original Request

GitHub issue #605: PRD for 7 reliability modules targeting the Go trading agent backend.

### Interview Summary

**Key Findings from Codebase Research**:

- **M1 (Stale-Run Reconciler)**: ✅ COMPLETE — `internal/agent/stale_run_reconciler.go` fully implements TTL-based sweep, audit logging, metric emission, context cancellation. Wired in `cmd/tradingagent/runtime.go:379-394` with `STALE_RUN_TTL` env var.
- **M2 (LLM Debate Timeout)**: PARTIAL — `debate_timeout_provider.go` handles per-call timeout + quick model fallback at debate level. But `internal/llm/fallback.go:54` still skips secondary on DeadlineExceeded, and `internal/llm/retry.go:190` still retries timeouts (wastes time).
- **M3 (Signal Gate)**: PRD description STALE — evaluator fallback already returns `Urgency:1, AffectedStrategies:nil` (line 173-180) which hub.go:245 correctly drops. Missing: parse failure metric + feature flag.
- **M5 (Metrics)**: Partially wired — `llm_metrics_provider.go` exists, pipeline metrics emit from `prod_strategy_runner.go:658-683`. Missing: scheduler metrics, evaluator metrics, new metric types.
- **M7 (Automation Health)**: `RegisteredJob` struct already has `ErrorCount`, `LastError`, `LastErrorAt`, `ConsecutiveFailures` fields. Missing: health API endpoint + metric.

### Metis Review

**Identified Gaps (addressed in plan)**:

- Validate M1 truly complete before skipping
- Verify no double-timeout between `debateTimeoutFallbackProvider` and `fallback.go`
- Define `SIGNAL_FALLBACK_MODE` enum values explicitly
- Check DB-insert path for Polymarket side normalization (not just strategy runner)
- Health endpoint auth decision: put behind existing auth middleware (admin-only)
- Grafana panel = provide JSON definition, not manual dashboard creation
- Every task must pass `go build ./... && go test ./... && go vet ./...`

---

## Work Objectives

### Core Objective

Fix remaining production reliability bugs in LLM fallback, signal evaluation, and metrics wiring, then expose automation health via API. Backend-only, no frontend changes.

### Concrete Deliverables

- Modified: `internal/llm/fallback.go`, `internal/llm/retry.go`
- Modified: `internal/signal/evaluator.go`
- Modified: `internal/metrics/metrics.go` (new metric types)
- Modified: `internal/scheduler/scheduler.go` (metrics threading)
- Modified: `internal/repository/postgres/polymarket_account.go` (side normalization)
- New: automation health endpoint handler in `internal/api/`
- Modified: `cmd/tradingagent/runtime.go` (wire new metrics)
- Updated: `docs/reference/configuration.md`, other stale docs
- New: Grafana panel JSON for automation health

### Definition of Done

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes with 0 failures
- [ ] `golangci-lint run` clean (no new warnings)
- [ ] All new metrics appear in Prometheus registry (test-verified)
- [ ] `curl /api/v1/automation/health` returns JSON health summary

### Must Have

- DeadlineExceeded in `fallback.go` triggers secondary provider (not skip)
- DeadlineExceeded in `retry.go` is non-retryable
- Signal parse failures emit metric and log at WARN
- Polymarket side values normalized before DB insert
- Automation health endpoint returns structured JSON
- New env vars documented in `configuration.md`

### Must NOT Have (Guardrails)
- NO new Go module dependencies (use existing imports only)
- NO refactoring of adjacent code "while we're here"
- NO changes to signal evaluation logic or urgency thresholds
- NO changes to trade execution logic
- NO manual Grafana dashboard creation (provide JSON only)
- NO docs style changes (only factual accuracy fixes)
- NO new npm dependencies in frontend unless strictly required

---

## Verification Strategy

> **ZERO HUMAN INTERVENTION** — ALL verification is agent-executed.

### Test Decision

- **Infrastructure exists**: YES — `go test`, table-driven tests throughout
- **Automated tests**: YES (tests-after for fixes, test-with for new code)
- **Framework**: Go stdlib `testing` + `github.com/prometheus/client_golang/prometheus/testutil`

### QA Policy

Every task verified with `go build ./... && go test ./... && go vet ./...`.
Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **Backend**: Use Bash — `go test`, `curl` endpoints
- **Metrics**: Use Bash — test Prometheus registry contains expected metric names

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Foundation — metrics infra + validation):
├── Task 1: Add new metric types to metrics.go [quick]
├── Task 2: Validate M1 complete (no code, just verify) [quick]

Wave 2 (Core fixes — MAX PARALLEL):
├── Task 3: Fix fallback.go DeadlineExceeded handling (depends: 1) [deep]
├── Task 4: Fix retry.go isRetryable for DeadlineExceeded (depends: 1) [quick]
├── Task 5: Signal evaluator parse failure metric + env var (depends: 1) [unspecified-high]
├── Task 6: Polymarket side normalization at DB-insert (depends: 1) [quick]
├── Task 7: Automation health endpoint + metric (depends: 1) [unspecified-high]
├── Task 8: Thread metrics to scheduler (depends: 1) [quick]

Wave 3 (Integration + frontend + LLM perf — MAX PARALLEL):
├── Task 9: Wire new metrics in runtime.go (depends: 3,4,5,7,8) [quick]
├── Task 11: Grafana panel JSON (depends: 7) [quick]
├── Task 12: Frontend reliability dashboard (depends: 7,9) [visual-engineering]
├── Task 13: Frontend pipeline health indicators (depends: 3) [visual-engineering]
├── Task 14: LLM model tuning + timeout config (depends: 3,4) [deep]
├── Task 15: LLM caching + prompt optimization (depends: 1) [deep]

Wave 4 (DB + docs — final):
├── Task 16: DB indexes for reliability queries (depends: 7) [quick]
├── Task 17: DB automation health columns (depends: 7) [quick]
├── Task 18: Documentation truth pass (depends: 9-17) [writing]

Wave FINAL (4 parallel reviews, then user okay):
├── F1: Plan compliance audit (oracle)
├── F2: Code quality review (unspecified-high)
├── F3: Real QA — go build/test + npm test + curl endpoints (unspecified-high)
├── F4: Scope fidelity check (deep)
-> Present results -> Get explicit user okay

Critical Path: T1 → T3 → T9 → T12 → T18 → F1-F4 → user okay
Parallel Speedup: ~65% faster than sequential
Max Concurrent: 6 (Waves 2 & 3)
```

### Dependency Matrix

| Task | Depends On | Blocks          | Wave |
| ---- | ---------- | --------------- | ---- |
| 1    | -          | 3-8,15          | 1    |
| 2    | -          | -               | 1    |
| 3    | 1          | 9,13,14         | 2    |
| 4    | 1          | 9,14            | 2    |
| 5    | 1          | 9,18            | 2    |
| 6    | 1          | 18              | 2    |
| 7    | 1          | 9,11,12,16,17   | 2    |
| 8    | 1          | 9               | 2    |
| 9    | 3,4,5,7,8  | 12,18           | 3    |
| 11   | 7          | -               | 3    |
| 12   | 7,9        | 18              | 3    |
| 13   | 3          | 18              | 3    |
| 14   | 3,4        | 18              | 3    |
| 15   | 1          | 18              | 3    |
| 16   | 7          | 18              | 4    |
| 17   | 7          | 18              | 4    |
| 18   | 9-17       | -               | 4    |

### Agent Dispatch Summary

- **Wave 1**: **2** — T1 → `quick`, T2 → `quick`
- **Wave 2**: **6** — T3 → `deep`, T4 → `quick`, T5 → `unspecified-high`, T6 → `quick`, T7 → `unspecified-high`, T8 → `quick`
- **Wave 3**: **6** — T9 → `quick`, T11 → `quick`, T12 → `visual-engineering`, T13 → `visual-engineering`, T14 → `deep`, T15 → `deep`
- **Wave 4**: **3** — T16 → `quick`, T17 → `quick`, T18 → `writing`
- **FINAL**: **4** — F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

---

## TODOs

- [x] 1. Add new metric types to `internal/metrics/metrics.go`

  **What to do**:
  - Add `LLMFallbackTotal *prometheus.CounterVec` with labels `{reason}` (values: `deadline_exceeded`, `provider_error`)
  - Add `SignalParseFailuresTotal prometheus.Counter`
  - Add `SchedulerTickTotal *prometheus.CounterVec` with labels `{type}` (values: `strategy`, `backtest`, `discovery`)
  - Add `AutomationJobErrorsTotal *prometheus.CounterVec` with labels `{job_name}`
  - Add helper methods: `RecordLLMFallback(reason string)`, `RecordSignalParseFailure()`, `RecordSchedulerTick(tickType string)`, `RecordAutomationJobError(jobName string)`
  - Register all new instruments in `New()`
  - Write table-driven tests verifying each new metric increments correctly using `prometheus/testutil`

  **Must NOT do**:
  - Change existing metric names or labels
  - Add histograms (only counters per PRD)
  - Add new Go module dependencies

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: [`golang-pro`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Task 2)
  - **Blocks**: Tasks 3, 4, 5, 6, 7, 8
  - **Blocked By**: None

  **References**:
  - `internal/metrics/metrics.go:31-110` — Existing `New()` constructor pattern
  - `internal/metrics/metrics.go:112-163` — Helper method pattern

  **Acceptance Criteria**:
  ```
  Scenario: New metrics register without panic
    Tool: Bash
    Steps:
      1. go test ./internal/metrics/... -run TestNew -v
    Expected Result: PASS, no duplicate-registration panic
    Evidence: .sisyphus/evidence/task-1-metrics-register.txt

  Scenario: Each new counter increments correctly
    Tool: Bash
    Steps:
      1. go test ./internal/metrics/... -v
    Expected Result: All PASS
    Evidence: .sisyphus/evidence/task-1-metrics-increment.txt
  ```
  **Commit**: `feat(metrics): add fallback, signal, scheduler, automation metric types`

- [x] 2. Validate Module 1 (Stale-Run Reconciler) is complete — READ-ONLY

  **What to do**:
  - Cross-reference `internal/agent/stale_run_reconciler.go` against PRD #605 M1 requirements
  - Verify wiring in `cmd/tradingagent/runtime.go:379-394`
  - Run existing tests: `go test ./internal/agent/... -run TestStaleRun -v`
  - **Do NOT modify any files.** If gaps found → report; otherwise mark M1 DONE.

  **Recommended Agent Profile**: `quick`, Skills: []
  **Parallelization**: Wave 1, parallel with T1. Blocks: None. Blocked By: None.

  **References**:
  - `internal/agent/stale_run_reconciler.go` — Full impl
  - `internal/agent/stale_run_reconciler_test.go` — Tests
  - `cmd/tradingagent/runtime.go:379-394` — Wiring

  **Acceptance Criteria**:
  ```
  Scenario: All M1 PRD requirements verified
    Tool: Bash
    Steps:
      1. grep -n "RecordStaleRunReconciled" internal/agent/stale_run_reconciler.go
      2. grep -n "STALE_RUN_TTL" cmd/tradingagent/runtime.go
      3. go test ./internal/agent/... -run TestStaleRun -v
    Expected Result: All greps match, tests pass
    Evidence: .sisyphus/evidence/task-2-m1-validation.txt
  ```
  **Commit**: NO

- [x] 3. Fix `fallback.go` — attempt secondary on DeadlineExceeded

  **What to do**:
  - In `internal/llm/fallback.go:54`: on DeadlineExceeded, create fresh context from parent and attempt secondary provider (instead of returning error)
  - Add `metrics` field (interface `LLMFallbackMetrics { RecordLLMFallback(reason string) }`) — nil-safe
  - Emit `metrics.RecordLLMFallback("deadline_exceeded")` on fallback
  - Keep `context.Canceled` as immediate-return (no change)
  - Tests: primary DeadlineExceeded → secondary called; primary Canceled → secondary NOT called; metric fires

  **Must NOT do**: Change behavior for non-DeadlineExceeded errors. Change Provider interface.

  **Recommended Agent Profile**: `deep`, Skills: [`golang-pro`]
  **Parallelization**: Wave 2. Blocks: T9. Blocked By: T1.

  **References**:
  - `internal/llm/fallback.go:46-62` — Complete() with DeadlineExceeded skip at line 54
  - `internal/llm/fallback.go:15-19` — FallbackProvider struct
  - `cmd/tradingagent/debate_timeout_provider.go:60-62` — Fresh context pattern
  - `internal/llm/fallback_test.go` — Existing test patterns

  **Acceptance Criteria**:
  ```
  Scenario: DeadlineExceeded triggers secondary
    Tool: Bash
    Steps: go test ./internal/llm/... -run TestFallbackProvider -v
    Expected Result: Secondary called on deadline, not on cancel
    Evidence: .sisyphus/evidence/task-3-fallback.txt
  ```
  **Commit**: `fix(llm): attempt secondary provider on DeadlineExceeded`

- [x] 4. Fix `retry.go` — DeadlineExceeded is non-retryable

  **What to do**:
  - `internal/llm/retry.go:190`: change `return true` → `return false` for DeadlineExceeded
  - Update tests: DeadlineExceeded → NOT retried, error returned immediately

  **Must NOT do**: Change retry for 429/5xx. Change backoff logic.

  **Recommended Agent Profile**: `quick`, Skills: [`golang-pro`]
  **Parallelization**: Wave 2. Blocks: T9. Blocked By: T1.

  **References**:
  - `internal/llm/retry.go:179-210` — `isRetryable()`, line 190 is THE change
  - `internal/llm/retry_test.go` — Test patterns

  **Acceptance Criteria**:
  ```
  Scenario: DeadlineExceeded not retried
    Tool: Bash
    Steps: go test ./internal/llm/... -run TestIsRetryable -v && go test ./internal/llm/... -run TestRetryProvider -v
    Expected Result: DeadlineExceeded → immediate error, no retry
    Evidence: .sisyphus/evidence/task-4-retry.txt
  ```
  **Commit**: `fix(llm): treat DeadlineExceeded as non-retryable`

- [x] 5. Signal evaluator parse failure metric + `SIGNAL_FALLBACK_MODE`

  **What to do**:
  - Add `metrics` field to `Evaluator` (interface `SignalEvalMetrics { RecordSignalParseFailure() }`)
  - On parse failure (line 126) and LLM failure (line 104): emit metric
  - Add `fallbackMode` field: `"drop"` (default, current behavior: urgency=1) or `"legacy"` (urgency=3 + all strategies)
  - Read `SIGNAL_FALLBACK_MODE` env var in `runtime.go`, pass to evaluator
  - Tests for both modes + metric emission

  **Must NOT do**: Change urgency thresholds in hub.go. Change evaluation logic.

  **Recommended Agent Profile**: `unspecified-high`, Skills: [`golang-pro`]
  **Parallelization**: Wave 2. Blocks: T9, T10. Blocked By: T1.

  **References**:
  - `internal/signal/evaluator.go:33-46` — Evaluator struct
  - `internal/signal/evaluator.go:104-111` — LLM failure path
  - `internal/signal/evaluator.go:125-131` — Parse failure path
  - `internal/signal/evaluator.go:173-181` — `fallback()` function to modify
  - `internal/signal/hub.go:245-251` — Urgency threshold (DO NOT MODIFY)

  **Acceptance Criteria**:
  ```
  Scenario: Parse failure metric + correct mode behavior
    Tool: Bash
    Steps: go test ./internal/signal/... -run TestEvaluator -v
    Expected Result: Drop mode → urgency=1, legacy → urgency=3; metric fires
    Evidence: .sisyphus/evidence/task-5-signal.txt
  ```
  **Commit**: `feat(signal): add parse failure metric and SIGNAL_FALLBACK_MODE env var`

- [x] 6. Polymarket side normalization at DB-insert

  **What to do**:
  - In `internal/repository/postgres/polymarket_account.go`, add `normalizePolymarketTradeSide(side string) string`
  - Mapping: yes/YES/Yes→YES, no/NO/No→NO, buy/Buy→YES, sell/Sell→NO
  - Call before every INSERT with `side` column
  - Table-driven tests with all known variants

  **Must NOT do**: Change DB CHECK constraint. Change trade execution logic.

  **Recommended Agent Profile**: `quick`, Skills: [`golang-pro`]
  **Parallelization**: Wave 2. Blocks: T10. Blocked By: T1.

  **References**:
  - `internal/repository/postgres/polymarket_account.go` — Insert functions
  - `internal/repository/postgres/polymarket_account_test.go` — Test (LSP shows `normalizePolymarketTradeSide` planned but undefined)
  - `cmd/tradingagent/prod_strategy_runner.go:245-264` — Strategy-level normalize (naming reference only)

  **Acceptance Criteria**:
  ```
  Scenario: Side normalization
    Tool: Bash
    Steps: go test ./internal/repository/postgres/... -run TestNormalizePolymarketTradeSide -v
    Expected Result: All variants normalize correctly
    Evidence: .sisyphus/evidence/task-6-polymarket.txt
  ```
  **Commit**: `fix(polymarket): normalize side values at DB-insert boundary`

- [x] 7. Automation health endpoint + error metric

  **What to do**:
  - Add `GET /api/v1/automation/health` in `internal/api/automation_handlers.go`
  - Response: `{"jobs":[{name,enabled,running,last_run,last_error,error_count,consecutive_failures,run_count}], "healthy":bool, "total_jobs":int, "failing_jobs":int}`
  - `healthy` = all jobs have `consecutive_failures < 3`
  - Behind existing auth middleware
  - Add `AutomationJobMetrics` interface to orchestrator; emit metric on job error
  - Tests for handler + metric

  **Must NOT do**: Build Grafana UI. Change job scheduling. Make endpoint public.

  **Recommended Agent Profile**: `unspecified-high`, Skills: [`golang-pro`]
  **Parallelization**: Wave 2. Blocks: T9, T10, T11. Blocked By: T1.

  **References**:
  - `internal/automation/orchestrator.go:62-79` — RegisteredJob with health fields
  - `internal/api/automation_handlers.go` — Existing handler patterns
  - `internal/api/server.go` — Route registration

  **Acceptance Criteria**:
  ```
  Scenario: Health endpoint returns valid JSON
    Tool: Bash
    Steps: go test ./internal/api/... -run TestAutomationHealth -v
    Expected Result: 200 with expected schema
    Evidence: .sisyphus/evidence/task-7-health.txt
  ```
  **Commit**: `feat(automation): add /api/v1/automation/health endpoint and error metric`

- [x] 8. Thread metrics to scheduler

  **What to do**:
  - Add `metrics` field + `WithMetrics(SchedulerMetrics) Option` to `internal/scheduler/scheduler.go`
  - Emit `RecordSchedulerTick("strategy"|"backtest"|"discovery")` at each tick
  - Test metric emission

  **Must NOT do**: Change cron logic. Change execution flow.

  **Recommended Agent Profile**: `quick`, Skills: [`golang-pro`]
  **Parallelization**: Wave 2. Blocks: T9. Blocked By: T1.

  **References**:
  - `internal/scheduler/scheduler.go:49` — Option pattern
  - `internal/scheduler/scheduler_test.go` — Test patterns

  **Acceptance Criteria**:
  ```
  Scenario: Scheduler tick metric
    Tool: Bash
    Steps: go test ./internal/scheduler/... -run TestSchedulerMetrics -v
    Expected Result: Metric recorded per tick type
    Evidence: .sisyphus/evidence/task-8-scheduler.txt
  ```
  **Commit**: `feat(scheduler): thread metrics for tick counting`

- [x] 9. Wire new metrics in `cmd/tradingagent/runtime.go`

  **What to do**:
  - Pass `appMetrics` to scheduler via `WithMetrics`
  - Pass `appMetrics` to signal evaluator
  - Pass `appMetrics` to automation orchestrator
  - Pass `appMetrics` to `FallbackProvider` if any constructed
  - Verify: `go build ./cmd/tradingagent/...`

  **Must NOT do**: Change init order. Add new env vars. Refactor DI.

  **Recommended Agent Profile**: `quick`, Skills: [`golang-pro`]
  **Parallelization**: Wave 3 (sequential). Blocks: T10. Blocked By: T3,T4,T5,T7,T8.

  **References**:
  - `cmd/tradingagent/runtime.go:59` — `appMetrics := metrics.New()`
  - `cmd/tradingagent/runtime.go:239-245` — Scheduler construction
  - `cmd/tradingagent/runtime.go:309-311` — Evaluator construction
  - `cmd/tradingagent/runtime.go:254-278` — Automation orchestrator

  **Acceptance Criteria**:
  ```
  Scenario: Full build
    Tool: Bash
    Steps: go build ./cmd/tradingagent/... && go vet ./cmd/tradingagent/...
    Expected Result: Exit 0
    Evidence: .sisyphus/evidence/task-9-build.txt
  ```
  **Commit**: `feat(runtime): wire new metrics to scheduler, evaluator, automation`

- [x] 10. Documentation truth pass

  **What to do**:
  - Audit `docs/reference/configuration.md` — add `STALE_RUN_TTL`, `SIGNAL_FALLBACK_MODE`; verify existing vars
  - Audit `docs/phase-7-execution-paths.md` against actual pipeline flow
  - Audit `docs/implementation-board.md` against current features
  - Add "Last verified" date header. Fix factual errors ONLY.

  **Must NOT do**: Rewrite style. Add new pages. Make subjective improvements.

  **Recommended Agent Profile**: `writing`, Skills: []
  **Parallelization**: Wave 3. Blocked By: T3-T9.

  **References**:
  - `docs/reference/configuration.md`, `docs/phase-7-execution-paths.md`, `docs/implementation-board.md`
  - `internal/config/config.go` — Actual env var bindings
  - `.env.example` — Canonical env var list

  **Acceptance Criteria**:
  ```
  Scenario: New env vars documented
    Tool: Bash
    Steps: grep "STALE_RUN_TTL" docs/reference/configuration.md && grep "SIGNAL_FALLBACK_MODE" docs/reference/configuration.md
    Expected Result: Both found
    Evidence: .sisyphus/evidence/task-10-docs.txt
  ```
  **Commit**: `docs: truth pass — fix stale config, execution paths, implementation board`

- [x] 11. Grafana automation health panel JSON

  **What to do**:
  - Create/update JSON in `monitoring/grafana/dashboards/` for `tradingagent_automation_job_errors_total`
  - Follow existing dashboard JSON conventions

  **Must NOT do**: Build multi-panel dashboard. Configure Grafana UI.

  **Recommended Agent Profile**: `quick`, Skills: []
  **Parallelization**: Wave 3. Blocked By: T7.

  **References**:
  - `monitoring/grafana/dashboards/` — Existing patterns
  - `monitoring/grafana/provisioning/` — Provisioning config

  **Acceptance Criteria**:
  ```
  Scenario: Valid JSON with correct metric
    Tool: Bash
    Steps: python3 -c "import json; json.load(open('monitoring/grafana/dashboards/automation-health.json'))" && grep "tradingagent_automation_job_errors_total" monitoring/grafana/dashboards/automation-health.json
    Expected Result: JSON valid, metric found
    Evidence: .sisyphus/evidence/task-11-grafana.txt
  ```
  **Commit**: `feat(monitoring): add Grafana automation health panel JSON`

- [x] 12. Frontend: Reliability dashboard page

  **What to do**:
  - Create `web/src/pages/reliability-page.tsx` with:
    - Automation job health table (fetches `/api/v1/automation/health` from T7)
    - Stale run summary card (fetches from existing `/api/v1/runs` with status=running filter)
    - Pipeline failure rate sparkline (fetches from existing runs endpoint, calculates fail %)
  - Add route to `App.tsx` router
  - Add nav link in `web/src/components/layout/app-shell.tsx`
  - Use existing UI components (`card`, `badge`, `button` from `components/ui/`)
  - Follow existing page patterns (see `automation-page.tsx`, `risk-page.tsx`)

  **Must NOT do**: Add new npm dependencies. Redesign existing pages. Change API contracts.

  **Recommended Agent Profile**: `visual-engineering`, Skills: [`vercel-react-best-practices`]
  **Parallelization**: Wave 3. Blocked By: T7, T9.

  **References**:
  - `web/src/pages/automation-page.tsx` — Existing page pattern to follow
  - `web/src/pages/risk-page.tsx` — Health/status display patterns
  - `web/src/lib/api/client.ts` — API client for fetch calls
  - `web/src/lib/api/types.ts` — Type definitions
  - `web/src/components/ui/` — Shared UI components

  **Acceptance Criteria**:
  ```
  Scenario: Reliability page renders
    Tool: Bash
    Steps: cd web && npm test -- --run --reporter=verbose 2>&1 | grep -i reliability
    Expected Result: Tests pass for reliability page
    Evidence: .sisyphus/evidence/task-12-frontend-reliability.txt
  ```
  **Commit**: `feat(web): add reliability dashboard page`

- [x] 13. Frontend: Pipeline health indicators on strategy detail

  **What to do**:
  - In `web/src/pages/strategy-detail-page.tsx`, add:
    - LLM fallback indicator badge (shows when pipeline used fallback model)
    - Timeout warning indicator (shows when any phase hit deadline)
    - Use WebSocket events (existing `use-websocket-client.ts`) for real-time updates
  - In `web/src/components/pipeline/phase-progress.tsx`:
    - Add visual indicator for timed-out phases
    - Add fallback model indicator when quick model was used instead of deep
  - Fetch from existing run detail API — look at `PipelineEvent` types already broadcast via WebSocket

  **Must NOT do**: Change WebSocket protocol. Add new API endpoints for this. Change pipeline execution.

  **Recommended Agent Profile**: `visual-engineering`, Skills: [`vercel-react-best-practices`]
  **Parallelization**: Wave 3 (parallel with T12). Blocked By: T3 (fallback behavior must be done first).

  **References**:
  - `web/src/pages/strategy-detail-page.tsx` — Existing page to modify
  - `web/src/components/pipeline/phase-progress.tsx` — Phase display to enhance
  - `web/src/hooks/use-websocket-client.ts` — WebSocket hook
  - `cmd/tradingagent/prod_strategy_runner.go:816-854` — `pipelineEventToWSMessage` — existing WS events

  **Acceptance Criteria**:
  ```
  Scenario: Strategy detail page renders with new indicators
    Tool: Bash
    Steps: cd web && npm test -- --run --reporter=verbose 2>&1 | grep -i strategy-detail
    Expected Result: Tests pass
    Evidence: .sisyphus/evidence/task-13-pipeline-indicators.txt
  ```
  **Commit**: `feat(web): add pipeline health indicators to strategy detail`

- [x] 14. LLM model tuning: per-strategy model config + timeout optimization

  **What to do**:
  - Verify `agent.StrategyConfig` already supports `LLMConfig.Provider`, `DeepThinkModel`, `QuickThinkModel` per strategy
  - Add `DebateTimeoutSeconds` to strategy config if not present (check `internal/agent/strategy_config.go`)
  - In `cmd/tradingagent/prod_strategy_runner.go:411-423` `effectiveDebateCallTimeout()`: ensure per-strategy timeout overrides global
  - Add `LLM_DEBATE_TIMEOUT` env var (global default) if not present, with sensible default (e.g. 120s)
  - Document recommended model configs for different use cases in `docs/reference/configuration.md`:
    - Low-latency: quick model for all phases
    - High-quality: deep model for debates, quick for analysis
    - Balanced: current defaults
  - Analyze current timeout settings and propose optimal values based on the 95% failure rate data

  **Must NOT do**: Change model selection logic beyond config. Add new LLM providers. Change prompt templates.

  **Recommended Agent Profile**: `deep`, Skills: [`golang-pro`]
  **Parallelization**: Wave 3. Blocked By: T3, T4 (timeout behavior must be fixed first).

  **References**:
  - `internal/agent/strategy_config.go` — StrategyConfig struct
  - `internal/agent/resolve_config.go` — Config resolution logic
  - `cmd/tradingagent/prod_strategy_runner.go:266-305` — prepareStrategyRun showing config flow
  - `cmd/tradingagent/prod_strategy_runner.go:380-409` — buildRunnerDefinition showing model wiring
  - `cmd/tradingagent/debate_timeout_provider.go` — Debate timeout mechanism

  **Acceptance Criteria**:
  ```
  Scenario: Per-strategy timeout override works
    Tool: Bash
    Steps: go test ./internal/agent/... -run TestResolveConfig -v && go test ./cmd/tradingagent/... -run TestEffectiveDebateCallTimeout -v
    Expected Result: Strategy-level timeout overrides global
    Evidence: .sisyphus/evidence/task-14-llm-tuning.txt
  ```
  **Commit**: `feat(llm): add per-strategy timeout config and model selection docs`

- [x] 15. LLM performance: enhance caching + prompt optimization

  **What to do**:
  - Review `internal/llm/cache.go` — cache already exists with `MemoryResponseCache`
  - Ensure `CachedProvider` is wired into the pipeline (check if it's actually used in runtime.go)
  - If not wired: add `CachedProvider` wrapping for analysis phase providers (these are most cacheable — same ticker data across strategies)
  - Add cache hit/miss metrics: `tradingagent_llm_cache_hits_total`, `tradingagent_llm_cache_misses_total`
  - For prompt optimization: add max token truncation for market data in analyst prompts
    - `internal/agent/analysts/` — check if prompts include all 400 days of OHLCV data
    - If so: truncate to last 60 bars for analyst prompts (reduce tokens by ~85%)
    - Keep full data available in state for other phases that need it
  - Add `LLM_CACHE_ENABLED` env var (default: true)

  **Must NOT do**: Change cache key algorithm. Add Redis-backed cache (future work). Change prompt content/instructions (only truncate data).

  **Recommended Agent Profile**: `deep`, Skills: [`golang-pro`]
  **Parallelization**: Wave 3 (parallel with T14). Blocked By: T1 (needs metrics).

  **References**:
  - `internal/llm/cache.go` — Existing cache implementation
  - `internal/llm/cache_test.go` — Existing cache tests
  - `cmd/tradingagent/runtime.go:422-427` — `throttleLLM` wrapper (pattern for adding cache wrapper)
  - `internal/agent/analysts/` — Analyst prompt formatting functions
  - `cmd/tradingagent/prod_strategy_runner.go:307-378` — `loadInitialState` showing 400-day lookback

  **Acceptance Criteria**:
  ```
  Scenario: Cache metrics fire on hit/miss
    Tool: Bash
    Steps: go test ./internal/llm/... -run TestCachedProvider -v
    Expected Result: Cache hits/misses recorded in metrics
    Evidence: .sisyphus/evidence/task-15-llm-cache.txt

  Scenario: Analyst prompt truncation reduces token count
    Tool: Bash
    Steps: go test ./internal/agent/analysts/... -run TestPromptTruncation -v
    Expected Result: Truncated prompt has ≤60 bars of data
    Evidence: .sisyphus/evidence/task-15-prompt-truncation.txt
  ```
  **Commit**: `feat(llm): wire response cache with metrics and truncate analyst prompts`

- [x] 16. DB schema: add indexes for reliability queries

  **What to do**:
  - Create migration `000027_reliability_indexes.up.sql` and `.down.sql`
  - Add index: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pipeline_runs_status_started ON pipeline_runs(status, started_at) WHERE status = 'running'` — speeds up stale-run reconciler query
  - Add index: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_automation_job_runs_job_name_started ON automation_job_runs(job_name, started_at DESC)` — speeds up health endpoint
  - Add index: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_decisions_run_id ON agent_decisions(run_id)` — speeds up run detail loading
  - Follow naming convention from existing migrations (000025, 000026)
  - Write migration test following existing patterns

  **Must NOT do**: Change existing table schemas. Drop existing indexes. Add columns.

  **Recommended Agent Profile**: `quick`, Skills: [`postgres-pro`]
  **Parallelization**: Wave 4. Blocked By: T7 (health endpoint defines query patterns).

  **References**:
  - `migrations/000026_polymarket_trade_side_widen.up.sql` — Latest migration naming pattern
  - `migrations/000025_risk_state.up.sql` — Migration structure
  - `internal/agent/stale_run_reconciler.go:100-103` — Stale-run query pattern
  - `internal/repository/postgres/` — Repository query patterns to optimize

  **Acceptance Criteria**:
  ```
  Scenario: Migration applies cleanly
    Tool: Bash
    Steps: task migrate:up (or equivalent migration command)
    Expected Result: Migration applies without error
    Evidence: .sisyphus/evidence/task-16-indexes.txt
  ```
  **Commit**: `feat(db): add indexes for reliability queries`

- [x] 17. DB schema: automation job health columns (migration-backed)

  **What to do**:
  - Create migration `000028_automation_job_health.up.sql` and `.down.sql`
  - Add to `automation_job_runs` table (or create new table if needed):
    - `last_error_at TIMESTAMPTZ`
    - `consecutive_failures INTEGER DEFAULT 0`
    - These complement the in-memory tracking in `RegisteredJob` with persistent storage
  - Update `internal/repository/postgres/` job run repo to read/write these fields
  - Update automation orchestrator to persist health state on job completion

  **Must NOT do**: Change existing columns. Drop tables. Rename tables.

  **Recommended Agent Profile**: `quick`, Skills: [`golang-pro`, `postgres-pro`]
  **Parallelization**: Wave 4 (parallel with T16). Blocked By: T7.

  **References**:
  - `internal/automation/orchestrator.go:62-79` — In-memory health fields to persist
  - `internal/repository/postgres/` — JobRunRepo for persistence patterns
  - `migrations/000026_*` — Latest migration for naming

  **Acceptance Criteria**:
  ```
  Scenario: Migration applies and fields writable
    Tool: Bash
    Steps: go test ./internal/repository/postgres/... -run TestJobRunRepo -v
    Expected Result: New columns readable/writable
    Evidence: .sisyphus/evidence/task-17-job-health-columns.txt
  ```
  **Commit**: `feat(db): add automation job health persistence columns`

- [x] 18. Documentation truth pass (expanded with new features)

  **What to do**:
  - Everything from original T10, PLUS:
  - Document `LLM_DEBATE_TIMEOUT`, `LLM_CACHE_ENABLED` env vars (from T14, T15)
  - Document per-strategy model configuration in strategy config reference
  - Document new API endpoint `/api/v1/automation/health` in `docs/reference/api.md`
  - Document reliability page in `docs/reference/` or `docs/design/web-ui.md`
  - Add recommended model configs section

  **Must NOT do**: Rewrite docs style. Create tutorial content.

  **Recommended Agent Profile**: `writing`, Skills: []
  **Parallelization**: Wave 4. Blocked By: T9-T17 (needs final code state).

  **References**:
  - `docs/reference/configuration.md` — Config docs
  - `docs/reference/api.md` — API docs
  - `docs/design/web-ui.md` — UI docs
  - `.env.example` — Env var reference

  **Acceptance Criteria**:
  ```
  Scenario: All new env vars documented
    Tool: Bash
    Steps: grep "STALE_RUN_TTL\|SIGNAL_FALLBACK_MODE\|LLM_DEBATE_TIMEOUT\|LLM_CACHE_ENABLED" docs/reference/configuration.md
    Expected Result: All 4 found
    Evidence: .sisyphus/evidence/task-18-docs.txt
  ```
  **Commit**: `docs: comprehensive truth pass with new reliability features`

---

## Final Verification Wave

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.

- [x] F1. **Plan Compliance Audit** — `oracle`
      Read the plan end-to-end. For each "Must Have": verify implementation exists (`go test`, `grep`, read file). For each "Must NOT Have": search codebase for forbidden patterns — reject with file:line if found. Check evidence files exist in `.sisyphus/evidence/`. Compare deliverables against plan.
      Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [x] F2. **Code Quality Review** — `unspecified-high`
      Run `go build ./...` + `golangci-lint run` + `go test ./...`. Review all changed files for: `//nolint` additions, empty error handling, commented-out code, unused imports. Check AI slop: excessive comments, over-abstraction, generic names.
      Output: `Build [PASS/FAIL] | Lint [PASS/FAIL] | Tests [N pass/N fail] | Files [N clean/N issues] | VERDICT`

- [x] F3. **Real QA** — `unspecified-high` (+ `playwright` skill if UI)
  Start from clean state. Run `go build ./... && go test ./... && go vet ./...`. Run `cd web && npm test -- --run`. `curl localhost:8080/api/v1/automation/health` if server available. Verify new metrics exist in Prometheus registry via test. Save to `.sisyphus/evidence/final-qa/`.
  Output: `Go [PASS/FAIL] | Frontend [PASS/FAIL] | Scenarios [N/N pass] | VERDICT`

- [x] F4. **Scope Fidelity Check** — `deep`
      For each task: read "What to do", read actual `git diff`. Verify 1:1 — everything in spec was built (no missing), nothing beyond spec was built (no creep). Check "Must NOT do" compliance. Flag unaccounted changes.
      Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

| #   | Message                                                                      | Files                                                                                                | Pre-commit                                             |
| --- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| 1   | `feat(metrics): add fallback, signal, scheduler, automation metric types`    | `internal/metrics/metrics.go`, `internal/metrics/metrics_test.go`                                    | `go test ./internal/metrics/...`                       |
| 2   | `fix(llm): attempt secondary provider on DeadlineExceeded`                   | `internal/llm/fallback.go`, `internal/llm/fallback_test.go`                                          | `go test ./internal/llm/...`                           |
| 3   | `fix(llm): treat DeadlineExceeded as non-retryable`                          | `internal/llm/retry.go`, `internal/llm/retry_test.go`                                                | `go test ./internal/llm/...`                           |
| 4   | `feat(signal): add parse failure metric and SIGNAL_FALLBACK_MODE env var`    | `internal/signal/evaluator.go`, `internal/signal/evaluator_test.go`                                  | `go test ./internal/signal/...`                        |
| 5   | `fix(polymarket): normalize side values at DB-insert boundary`               | `internal/repository/postgres/polymarket_account.go`, `*_test.go`                                    | `go test ./internal/repository/postgres/...`           |
| 6   | `feat(automation): add /api/v1/automation/health endpoint and error metric`  | `internal/api/automation_handlers.go`, `internal/automation/orchestrator.go`                         | `go test ./internal/api/... ./internal/automation/...` |
| 7   | `feat(scheduler): thread metrics for tick counting`                          | `internal/scheduler/scheduler.go`, `*_test.go`                                                       | `go test ./internal/scheduler/...`                     |
| 8   | `feat(runtime): wire new metrics to scheduler, evaluator, automation`        | `cmd/tradingagent/runtime.go`                                                                        | `go build ./cmd/tradingagent/...`                      |
| 9   | `docs: truth pass — fix stale config, execution paths, implementation board` | `docs/reference/configuration.md`, `docs/phase-7-execution-paths.md`, `docs/implementation-board.md` | -                                                      |
| 10  | `feat(monitoring): add Grafana automation health panel JSON`                 | `monitoring/grafana/dashboards/`                                                                     | -                                                      |

---

## Success Criteria

### Verification Commands
```bash
go build ./...          # Expected: exit 0
go test ./...           # Expected: all PASS
go vet ./...            # Expected: exit 0
golangci-lint run       # Expected: no new issues
cd web && npm test -- --run  # Expected: all PASS
```

### Final Checklist
- [ ] DeadlineExceeded in fallback.go → attempts secondary provider
- [ ] DeadlineExceeded in retry.go → non-retryable
- [ ] Signal parse failures emit metric
- [ ] SIGNAL_FALLBACK_MODE env var documented and functional
- [ ] Polymarket side normalized at DB-insert
- [ ] /api/v1/automation/health returns JSON
- [ ] Scheduler emits tick metric
- [ ] All new env vars in docs/reference/configuration.md
- [ ] Grafana panel JSON provided
- [ ] Frontend reliability dashboard page renders
- [ ] Frontend pipeline health indicators show fallback/timeout status
- [ ] Per-strategy LLM timeout config works
- [ ] LLM response caching wired with metrics
- [ ] Analyst prompt truncation reduces token count
- [ ] DB reliability indexes created
- [ ] Automation job health columns persisted
- [ ] No new Go module dependencies added
