# Portfolio Allocator Action Engine Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Move Augr from isolated strategy execution to portfolio-level capital deployment using diagnostics, an opportunity queue, a shadow allocator, and paper-only allocator execution.

**Architecture:** Strategy runs continue to produce signals and trading plans. A new portfolio allocation lane records no-action diagnostics and actionable opportunities, ranks them against portfolio exposure/diversity/buying-power targets, emits shadow decisions, then later converts selected decisions into paper orders only. Existing risk checks and live gates remain hard vetoes.

**Tech Stack:** Go, PostgreSQL migrations/repositories, existing `domain` models, `execution.OrderManager`, `automation` jobs, `internal/api` handlers, React dashboard later if needed.

---

## File Structure

- Create `internal/portfolio/diagnostics.go`: pure no-action diagnostics summaries.
- Create `internal/portfolio/allocator.go`: pure scoring, ranking, sizing, and decision rules.
- Create `internal/portfolio/opportunity_builder.go`: converts strategy runs/trade decisions into opportunities.
- Create `internal/domain/opportunity.go`: persistent opportunity model and statuses.
- Create `internal/domain/allocation_decision.go`: shadow/paper allocator decision model.
- Create `migrations/000052_portfolio_opportunity_queue.up.sql`: opportunities and allocation decision tables.
- Create `migrations/000052_portfolio_opportunity_queue.down.sql`: rollback.
- Modify `internal/repository/interfaces.go`: add opportunity and allocation decision repository interfaces.
- Create `internal/repository/postgres/opportunity.go`: Postgres repository implementation.
- Create `internal/repository/postgres/allocation_decision.go`: Postgres repository implementation.
- Create `internal/api/portfolio_allocator_handlers.go`: diagnostics/opportunity/decision summary endpoint.
- Modify `internal/api/server.go`: route/wire portfolio allocator endpoint.
- Create `internal/automation/jobs_portfolio_allocator.go`: scheduled diagnostics/shadow/paper allocation job.
- Modify `internal/automation/orchestrator.go`: register allocator job.
- Modify `cmd/tradingagent/runtime.go`: wire repositories and allocator job deps.
- Modify `internal/execution/order_manager.go` only if necessary to record richer no-action reasons; prefer additive diagnostics from existing trade decisions first.

---

## Phase 1: No-Action Diagnostics

### Task 1: Pure diagnostics model

**Files:**
- Create: `internal/portfolio/diagnostics.go`
- Test: `internal/portfolio/diagnostics_test.go`

- [ ] Add `DiagnosticsInput`, `DiagnosticsSummary`, `NoActionReason`, and `BuildDiagnosticsSummary`.
- [ ] Count strategy runs by signal/status, trade decisions by status/reason, active strategies by market, current positions, buying power utilization, and utilization gap.
- [ ] Add reason taxonomy: `hold_signal`, `risk_rejected`, `sizing_zero`, `sell_without_position`, `kill_switch`, `live_gate_denied`, `missing_data`, `unknown`.
- [ ] Test summary counts, utilization math, and reason grouping.
- [ ] Run `rtk go test ./internal/portfolio -run Diagnostics -count=1`.

### Task 2: Diagnostics API/report seam

**Files:**
- Create: `internal/api/portfolio_allocator_handlers.go`
- Modify: `internal/api/server.go`
- Test: `internal/api/portfolio_allocator_handlers_test.go`

- [ ] Add `GET /api/v1/portfolio/allocator/diagnostics` returning current diagnostic counts.
- [ ] Use existing repositories only; do not mutate DB.
- [ ] If data is incomplete, return empty counts and warnings rather than failing hard.
- [ ] Run `rtk go test ./internal/api ./internal/portfolio -run 'PortfolioAllocator|Diagnostics' -count=1`.

### Task 3: Commit Phase 1

- [ ] Run focused validation.
- [ ] Commit `feat(portfolio): add allocator diagnostics`.

---

## Phase 2: Opportunity Queue

### Task 4: Persistence schema and repositories

**Files:**
- Create: `migrations/000052_portfolio_opportunity_queue.up.sql`
- Create: `migrations/000052_portfolio_opportunity_queue.down.sql`
- Create: `internal/domain/opportunity.go`
- Create: `internal/domain/allocation_decision.go`
- Modify: `internal/repository/interfaces.go`
- Create: `internal/repository/postgres/opportunity.go`
- Create: `internal/repository/postgres/allocation_decision.go`
- Test: `internal/repository/postgres/opportunity_test.go`
- Test: `internal/repository/postgres/allocation_decision_test.go`

- [ ] Add opportunity statuses: `queued`, `selected`, `rejected`, `expired`, `executed`.
- [ ] Add decision modes: `shadow`, `paper`.
- [ ] Use dedupe key `(strategy_id, market_type, ticker, side, signal, date bucket)`.
- [ ] Store scoring inputs, rationale, expires_at, selected/rejected metadata.
- [ ] Run repository tests.
- [ ] Commit `feat(portfolio): add opportunity queue persistence`.

### Task 5: Opportunity builder

**Files:**
- Create: `internal/portfolio/opportunity_builder.go`
- Test: `internal/portfolio/opportunity_builder_test.go`

- [ ] Convert actionable BUY/SELL strategy outputs/trade decisions into queued opportunities.
- [ ] Reject or tag HOLD/no-action outputs with taxonomy reasons.
- [ ] Expire stocks after one trading day and event markets after six hours.
- [ ] Do not create opportunities for inactive strategies.
- [ ] Run `rtk go test ./internal/portfolio -run Opportunity -count=1`.

---

## Phase 3: Shadow Portfolio Allocator

### Task 6: Pure scoring/ranking/sizing

**Files:**
- Create: `internal/portfolio/allocator.go`
- Test: `internal/portfolio/allocator_test.go`

- [ ] Implement default paper config: target gross exposure 35% initially, hard gross 50%, cash reserve 20%, max 2 new orders/run, max 5/day.
- [ ] Score candidates using edge, confidence, liquidity, spread, diversification, and freshness.
- [ ] Reject below market score/edge/liquidity or above spread cap.
- [ ] Size selected opportunities by account value, score multiplier, market cap, cash reserve, and per-position cap.
- [ ] Emit shadow decisions only.
- [ ] Run `rtk go test ./internal/portfolio -run Allocator -count=1`.

### Task 7: Shadow allocator automation/API

**Files:**
- Create: `internal/automation/jobs_portfolio_allocator.go`
- Modify: `internal/automation/orchestrator.go`
- Modify: `cmd/tradingagent/runtime.go`
- Modify: `internal/api/portfolio_allocator_handlers.go`

- [ ] Add `portfolio_allocator` job in shadow mode by default.
- [ ] Persist allocation decisions with `mode=shadow` and `action=shadow_selected`/`shadow_rejected`.
- [ ] Expose last shadow summary via API.
- [ ] Do not submit orders.
- [ ] Commit `feat(portfolio): add shadow allocator`.

---

## Phase 4: Paper-Only Allocator Execution

### Task 8: Paper execution bridge

**Files:**
- Create: `internal/portfolio/paper_executor.go`
- Test: `internal/portfolio/paper_executor_test.go`
- Modify: `internal/automation/jobs_portfolio_allocator.go`

- [ ] Add explicit mode `paper` that converts selected decisions into paper order intents.
- [ ] Refuse execution unless strategy `is_paper=true`.
- [ ] Refuse live broker paths regardless of live adapter availability.
- [ ] Keep Polymarket inactive unless manually reactivated later.
- [ ] Respect max orders/run and max orders/day.
- [ ] Record decision status `executed` or `execution_rejected` with reason.

### Task 9: Validation, review, and commit

- [ ] Run backend validation for portfolio, repositories, automation, API, execution, runner.
- [ ] Ask @oracle for safety review.
- [ ] Commit `feat(portfolio): enable paper allocator execution`.

---

## Rollout Defaults

- Mode starts as `diagnostics`/`shadow`; `paper` mode requires config/env in code before any execution.
- Live trading is never triggered by allocator.
- Initial paper caps: max 2 new orders per run, max 5/day, max 2% account value per stock, max 1% per Kalshi/event market, target gross 35%, hard gross 50%, cash reserve 20%.
- No forced buying below score threshold.
- Every rejected opportunity must keep a reason.

## Acceptance Criteria

- Diagnostics explain why capital is idle.
- Opportunities are persisted and deduped.
- Shadow allocator produces ranked, budgeted decisions without submitting orders.
- Paper allocator can submit paper-only orders through existing order manager with strategy IDs attached.
- All live paths remain gated and disabled unless explicitly configured outside this sprint.
