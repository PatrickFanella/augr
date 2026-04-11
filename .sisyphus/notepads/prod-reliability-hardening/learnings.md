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
