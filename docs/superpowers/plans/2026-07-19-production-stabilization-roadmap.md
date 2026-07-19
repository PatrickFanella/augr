# Production Stabilization Roadmap Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Execute the three P0 reliability fixes now and preserve P1/P2 as explicitly gated future work.

**Architecture:** P0 is split into independent prompt-capacity, paper-execution, and opportunity-lifecycle plans, each with its own canary and rollback. P1 begins only after P0 operates cleanly for seven days. P2 begins only after P1 removes correctness and provider-pressure risks.

**Tech Stack:** Go services, PostgreSQL, Docker Compose on nuc, Caddy edge on almaz, production telemetry and audit records.

---

## P0 — execute now

### Task 1: Prompt capacity

- [ ] Execute `docs/superpowers/plans/2026-07-19-p0-ollama-prompt-capacity.md`.
- [ ] Gate: zero Ollama prompt-size HTTP 413 responses across at least 20 stock runs.
- [ ] Gate: stock pipeline completion rate exceeds 90% excluding unrelated provider failures.

### Task 2: Kalshi paper execution

- [ ] Execute `docs/superpowers/plans/2026-07-19-p0-kalshi-paper-execution.md`.
- [ ] Gate: legacy submitted backlog is cancelled with recorded affected count.
- [ ] Gate: every new canary paper order reaches a terminal state and produces at most one trade.
- [ ] Gate: zero Kalshi paper orders remain submitted beyond one schedule interval for 24 hours.

### Task 3: Opportunity lifecycle

- [ ] Execute `docs/superpowers/plans/2026-07-19-p0-opportunity-lifecycle.md`.
- [ ] Gate: zero queued opportunities have `expires_at <= NOW()` after two allocator runs.
- [ ] Gate: no opportunity receives repeated `shadow_rejected` decisions for reason `expired`.

### Task 4: P0 soak and review

- [ ] Run P0 in production for seven consecutive days.
- [ ] Produce the same operational report slices used for 2026-07-12 through 2026-07-19.
- [ ] Promote to P1 only if there is no data-integrity regression, no duplicate fill, and no reappearance of any P0 failure mode.

## P1 — planned next, not part of P0 implementation

### Task 5: Restore Kalshi settlement safely

- [ ] Add shared Kalshi request throttling, exponential backoff with jitter, and a bounded retry budget.
- [ ] Add tests for HTTP 429, `Retry-After`, cancellation, and five-consecutive-failure disable behavior.
- [ ] Re-enable `kalshi_settlement` only after a dry run stays under the provider limit.
- [ ] Gate: 20 consecutive settlement runs succeed without 429 or multi-hour attempts.

### Task 6: Make paper accounting restart-persistent

- [ ] Reuse one runtime paper broker for normal strategy execution and allocator paper mode.
- [ ] Reconstruct paper cash/equity from persisted paper orders and trades at startup.
- [ ] Restore open paper positions into broker simulation state without mixing Alpaca/live records.
- [ ] Make order, trade, and position fill persistence transactional and idempotent.
- [ ] Add idempotent restart integration tests and partial/delayed fill simulation.
- [ ] Gate: broker balance and positions are identical before and after a controlled restart.

### Task 7: Correct diagnostics coverage

- [ ] Replace the 100-strategy diagnostic sample with paged aggregation or repository counts.
- [ ] Expose sample size separately from total size.
- [ ] Gate: allocator diagnostics match direct SQL counts for all market types.

### Task 8: Reconcile P/L semantics

- [ ] Create a reconciliation report that compares Alpaca cash/equity, closed-position P/L, trades, fees, dividends, and manual adjustments.
- [ ] Explain or classify the observed $16.094251 variance rather than forcing values to match.
- [ ] Gate: every variance is attributed to a persisted ledger category.

## P2 — planned after P1 gates pass

### Task 9: Strategy hygiene

- [ ] Review 13 active strategies with no weekly run.
- [ ] Review 15 duplicate active ticker groups; retain intentional variants and disable accidental duplicates.
- [ ] Add an operational report for active-but-unscheduled and duplicate strategy groups.

### Task 10: Centralize provider rate governance

- [ ] Add per-provider request budgets and queues for Polygon, Kalshi, and Ollama.
- [ ] Add saturation, throttling, retry, and dropped-work metrics.
- [ ] Gate: provider pressure degrades throughput without disabling unrelated jobs.

### Task 11: Control database growth

- [ ] Define retention and aggregation policy for `kalshi_market_snapshots` before deleting data.
- [ ] Benchmark the retained query patterns and validate restore procedures.
- [ ] Gate: projected 90-day database growth fits storage budget with documented recovery guarantees.

### Task 12: PostgreSQL collation maintenance

- [ ] Inventory indexes and queries affected by the recorded/no-actual collation warning.
- [ ] Back up, rehearse, and execute `REFRESH COLLATION VERSION` plus required reindexing in a maintenance window.
- [ ] Gate: warning is gone and post-maintenance integrity/query checks pass.

## Sequencing rule

Do not parallelize changes that write the same financial lifecycle tables. Prompt-capacity work may run independently. Complete paper-execution rollout before opportunity allocator paper mode is enabled. P1 settlement and accounting work must not begin until P0 paper-order terminal-state behavior has soaked for seven days.
