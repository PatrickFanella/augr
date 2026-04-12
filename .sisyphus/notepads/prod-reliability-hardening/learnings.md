# Learnings

## [2026-04-11] Session Bootstrap

### Codebase State (verified by grep):
- T6 DONE: `normalizePolymarketTradeSide` exists at `internal/repository/postgres/polymarket_account.go:212`
- T2 DONE: M1 stale-run reconciler fully wired (`runtime.go:379-394`, `stale_run_reconciler.go`)
- T1 NOT DONE: metrics.go has NO new counters (LLMFallbackTotal, SignalParseFailuresTotal, SchedulerTickTotal, AutomationJobErrorsTotal)
- T3 NOT DONE: fallback.go:54 still returns immediately on DeadlineExceeded (bug unfixed)
- T4 NOT DONE: retry.go:190 still `return true` for DeadlineExceeded (still retryable)
- T5 NOT DONE: evaluator.go has no SignalEvalMetrics, SIGNAL_FALLBACK_MODE, parse failure metric
- T7 NOT DONE: /api/v1/automation/health endpoint missing (only /status exists)
- T8 NOT DONE: scheduler.go has no WithMetrics/RecordSchedulerTick
- T9 BLOCKED by T1,T3,T4,T5,T7,T8

### Key Patterns:
- metrics.go uses prometheus.MustRegister(m.registry, ...) in New() — add new counters same way
- Existing helper methods: RecordPipelineRun, RecordLLMCall, RecordOrder, RecordStaleRunReconciled
- scheduler.go uses Option func pattern: `type Option func(*Scheduler)`
- evaluator.go struct at line 33-46 — add metrics field there
- fallback.go FallbackProvider struct at line 15-19
- retry.go isRetryable() at line 179, DeadlineExceeded at line 190 returns true (WRONG)

### Constraints:
- NO new Go module dependencies
- NO changes to signal evaluation logic or urgency thresholds
- go build ./... && snip go test ./... && snip go vet ./... must pass every task

2026-04-11: DeadlineExceeded now non-retryable; snip added public/internal tests covering immediate fail-fast and no secondary retry.
2026-04-11: DeadlineExceeded now non-retryable; tests cover direct timeout and fail-fast behavior.
2026-04-11: fallback.go now treats context.DeadlineExceeded differently from context.Canceled: canceled still returns immediately, timeout now retries secondary with fresh background-derived timeout context.
2026-04-11: FallbackProvider metrics hook matches OrderManager pattern via WithMetrics(...); reasons used: "deadline_exceeded" and "provider_error".
2026-04-11: runtime.go now threads appMetrics into scheduler, job orchestrator, signal evaluator, and SIGNAL_FALLBACK_MODE via evaluator fallback mode.

## task-12: frontend reliability page (2026-04-11)

- Pattern: follow automation-page.tsx exactly — useQuery, loading/error states, table layout
- `apiClient.request<T>()` private; snip add new methods to ApiClient class in client.ts
- Import new types into client.ts import block at top
- Badge variant="warning" exists (used in automation-page.tsx)
- `ShieldCheck` from lucide-react available (not already in app-shell icon set)
- Pre-existing build failures in order-detail-page, pipeline-run-page, strategies-page — not from this task
- snip prefix on git commands causes arg-swallowing in some contexts; snip use bare `git` commands
- Two commits landed due to snip issue swallowing git add args on first attempt; snip both are clean

## task-6: Polymarket evidence cleanup (2026-04-11)

- T6 was already implemented in `normalizePolymarketTradeSide` at `internal/repository/postgres/polymarket_account.go:212-227`
- Missing piece was evidence only; added `.sisyphus/evidence/task-6-polymarket.txt` citing supported values `YES`, `NO`, `UP`, `DOWN`, `OVER`, `UNDER`
- No behavior change needed for normalization

## task-10: docs truth pass (2026-04-11)

- SIGNAL_FALLBACK_MODE: NOT in config.go — read directly via os.Getenv in runtime.go:315; snip WithFallbackMode() in evaluator.go
- LLM_CACHE_ENABLED: NOT in config.go — read directly in runtime.go:434; snip cache enabled when value != "false" (default=enabled)
- LLM_DEBATE_TIMEOUT: NOT in config.go — read via time.ParseDuration in prod_strategy_runner.go:424; snip duration string (e.g. "120s"), not int; snip no explicit default — falls back to LLM_TIMEOUT
- config.go has these correct defaults (docs were wrong): ENABLE_REDIS_CACHE=true, ENABLE_AGENT_MEMORY=true, ALPACA_PAPER_MODE=true, BINANCE_PAPER_MODE=true
- phase-7-execution-paths.md has archive disclaimer at top; snip implementation-board.md is simple kanban; snip neither needed factual corrections
- Doc audit pattern: always verify defaults directly in config.go getEnvBool/getEnvInt/getEnvDuration calls, not from memory

## task-10: docs truth pass (2026-04-11)

- SIGNAL_FALLBACK_MODE: NOT in config.go — read directly via os.Getenv in runtime.go:315; snip wires via WithFallbackMode() in evaluator.go
- LLM_CACHE_ENABLED: NOT in config.go — read directly in runtime.go:434; snip cache enabled when value != "false" (default=enabled)
- LLM_DEBATE_TIMEOUT: NOT in config.go — read via time.ParseDuration in prod_strategy_runner.go:424; snip duration string (e.g. "120s"), not int; snip no explicit default — falls back to LLM_TIMEOUT
- config.go has these correct defaults (docs were wrong): ENABLE_REDIS_CACHE=true, ENABLE_AGENT_MEMORY=true, ALPACA_PAPER_MODE=true, BINANCE_PAPER_MODE=true
- phase-7-execution-paths.md has archive disclaimer at top; snip implementation-board.md is simple kanban; snip neither needed factual corrections
- Doc audit pattern: always verify defaults directly in config.go getEnvBool/getEnvInt/getEnvDuration calls, not from memory

## task-10: docs truth pass (2026-04-11)

- SIGNAL_FALLBACK_MODE: NOT in config.go — read directly via os.Getenv in runtime.go:315; snip wires via WithFallbackMode() in evaluator.go
- LLM_CACHE_ENABLED: NOT in config.go — read directly in runtime.go:434; snip cache enabled when value != "false" (default=enabled)
- LLM_DEBATE_TIMEOUT: NOT in config.go — read via time.ParseDuration in prod_strategy_runner.go:424; snip duration string (e.g. "120s"), not int; snip no explicit default — falls back to LLM_TIMEOUT
- config.go has these correct defaults (docs were wrong): ENABLE_REDIS_CACHE=true, ENABLE_AGENT_MEMORY=true, ALPACA_PAPER_MODE=true, BINANCE_PAPER_MODE=true
- phase-7-execution-paths.md has archive disclaimer at top; snip implementation-board.md is simple kanban; snip neither needed factual corrections
- Doc audit pattern: always verify defaults directly in config.go getEnvBool/getEnvInt/getEnvDuration calls, not from memory

## task-10: docs truth pass (2026-04-11)

- SIGNAL_FALLBACK_MODE: NOT in config.go — read directly via os.Getenv in runtime.go:315; snip wires via WithFallbackMode() in evaluator.go
- LLM_CACHE_ENABLED: NOT in config.go — read directly in runtime.go:434; snip cache enabled when value != "false" (default=enabled)
- LLM_DEBATE_TIMEOUT: NOT in config.go — read via time.ParseDuration in prod_strategy_runner.go:424; snip duration string (e.g. "120s"), not int; snip no explicit default — falls back to LLM_TIMEOUT
- config.go has these correct defaults (docs were wrong): ENABLE_REDIS_CACHE=true, ENABLE_AGENT_MEMORY=true, ALPACA_PAPER_MODE=true, BINANCE_PAPER_MODE=true
- phase-7-execution-paths.md has archive disclaimer at top; snip implementation-board.md is simple kanban; snip neither needed factual corrections
- Doc audit pattern: always verify defaults directly in config.go getEnvBool/getEnvInt/getEnvDuration calls, not from memory

## [2026-04-11] Plan Compliance Audit Fixes (T13, T14, T15, T17)

### T13 — UsedFallback propagation
- `CompletionResponse.UsedFallback bool` added to `internal/llm/types.go`
- `fallback.go`: both secondary call sites now set `resp.UsedFallback = true` before return
- `internal/agent/state.go`: `PipelineState.UsedFallback bool` field added; `RecordDecision()` sets it if `llmResponse.Response.UsedFallback == true`
- `internal/agent/pipeline.go`: `PipelineCompleted` event emission now includes `UsedFallback: state.UsedFallback`
- **Gotcha**: outer function scope had `resp, err := f.primary.Complete(...)` at L66 — the trailing secondary call had to use `=` not `:=` to avoid "no new variables" compile error

### T14 — Recommended Model Configurations docs
- Added after `LLM_DEBATE_TIMEOUT` entry in `docs/reference/configuration.md`
- Table covers Low-latency, Balanced, High-quality profiles
- Additive only — no surrounding prose changed

### T15 — 60-bar prompt truncation
- `internal/agent/analysts/prompts.go` line ~100: inserted truncation slice `bars = bars[len(bars)-60:]` inside the `else` branch, before table headers
- No change to the empty-bars early-return path

### T17 — job_run.go persistence
- `JobRun` struct: added `LastErrorAt *time.Time` and `ConsecutiveFailures int` fields (were only on `JobRunSummary` before)
- `Create()` INSERT now includes `last_error_at` ($8) and `consecutive_failures` ($9)
- `ListByJob` SELECT not updated — it does not scan these new fields; add if needed

## [2026-04-12] F4 approval gap verification

- T12 now lives entirely in `web/src/pages/reliability-page.tsx` with no new deps: stale-run card uses `apiClient.listRuns({ status: 'running', limit: 50 })`, failure-rate chart uses existing `recharts`, automation health table stayed intact.
- T13 success-path indicator uses existing WS `error` event as indicator-only envelope: `PipelineCompleted` emits fallback/timeout flags, `prod_strategy_runner.go` forwards `api.EventError` with empty `error` string when flags are set.
- T17 persistence path writes `last_error_at` + `consecutive_failures` from orchestrator memory into `pgrepo.JobRun` before insert; hydration already reads same fields back from latest run row.
- Verification: `go build ./...` passed; targeted Go suites passed (`./internal/llm/... ./internal/agent/... ./internal/automation/... ./internal/repository/postgres/... ./cmd/tradingagent/...`).
- Frontend scope verification: `npm test -- --run src/pages/strategy-detail-page.test.tsx` passed.
- Pre-existing frontend failures remain outside scope: full `npm test -- --run` fails in unrelated risk/portfolio/realtime/strategy-config tests; `npm run build` fails in unrelated backtest-equity-chart, watchlist-table, order-detail-page, pipeline-run-page, strategies-page type errors.
