---
title: Phase 5 Execution Paths
type: tracking
created: 2026-03-26
tags: [tracking, phase-5, api, paper-trading, observability, production]
---

# Phase 5: Execution Paths

> 33 issues across 7 tracks. **17 ready now** (6 already implemented), 16 blocked.
> Updated: 2026-03-26
> Tracking issue: [#348](https://github.com/PatrickFanella/get-rich-quick/issues/348)

## Context

Phase 4 delivered a complete backtesting engine (30 files, full test coverage). This phase bridges the working backend engine to production-readiness: close out Phase 4, land architecture improvements, build the API layer, add observability, and begin 60-day paper trading validation.

## Summary

| Track | Name                      | Total  | Ready  | Blocked | Implemented | Models          |
| ----- | ------------------------- | :----: | :----: | :-----: | :---------: | --------------- |
| A     | Architecture Improvements |   6    |   6    |    0    |      5      | Claude Opus 4.6 |
| B     | Phase 4 Completion        |   7    |   4    |    3    |      5      | Mixed           |
| C     | API Layer                 |   3    |   1    |    2    |      0      | Mixed           |
| D     | CI/CD & Infrastructure    |   3    |   1    |    2    |      0      | GPT-5.4         |
| E     | Observability             |   3    |   1    |    2    |      0      | GPT-5.4         |
| F     | Paper Trading             |   7    |   2    |    5    |      3      | Mixed           |
| G     | Architecture Decisions    |   4    |   2    |    2    |      0      | Human           |
|       | **Total**                 | **33** | **17** | **16**  |   **13**    |                 |

**Implemented** = code exists in the repo but the issue has not been closed. These need verification against the issue acceptance criteria, then closure.

---

## Track A: Architecture Improvements

> Depends on: Nothing (fully independent)
> Source: Post-Phase 4 architecture review

| #   | Issue                                                               | Title                                                    | Size | Blocker | Status      | Model           |
| --- | ------------------------------------------------------------------- | -------------------------------------------------------- | :--: | ------- | ----------- | --------------- |
| 1   | [#342](https://github.com/PatrickFanella/get-rich-quick/issues/342) | Adopt `parse.Parse[T]` for structured LLM output parsing |  XS  | None    | IMPLEMENTED | Claude Opus 4.6 |
| 2   | [#343](https://github.com/PatrickFanella/get-rich-quick/issues/343) | Extract BacktestPersister interface from scheduler       |  S   | None    | IMPLEMENTED | Claude Opus 4.6 |
| 3   | [#344](https://github.com/PatrickFanella/get-rich-quick/issues/344) | Consolidate debate phase execution in pipeline           |  S   | None    | IMPLEMENTED | Claude Opus 4.6 |
| 4   | [#345](https://github.com/PatrickFanella/get-rich-quick/issues/345) | Add PipelineBuilder with construction-time validation    |  S   | None    | IMPLEMENTED | Claude Opus 4.6 |
| 5   | [#346](https://github.com/PatrickFanella/get-rich-quick/issues/346) | Add config validation for data providers and model names |  XS  | None    | IMPLEMENTED | Claude Opus 4.6 |
| 6   | [#347](https://github.com/PatrickFanella/get-rich-quick/issues/347) | Migrate production nodes to typed phase I/O interfaces   |  L   | None    | READY       | Claude Opus 4.6 |

**Status:** #342–#346 are implemented on `main` with passing tests. Need PRs and issue closure. #347 is a larger effort — can start anytime but not blocking.

---

## Track B: Phase 4 Completion

> Depends on: Nothing (fully independent, closes out Phase 4)

| #   | Issue                                                               | Title                                                         | Size | Blocker | Status      | Model           |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------- | :--: | ------- | ----------- | --------------- |
| 1   | [#66](https://github.com/PatrickFanella/get-rich-quick/issues/66)   | Design backtesting framework architecture                     |  S   | None    | IMPLEMENTED | —               |
| 2   | [#68](https://github.com/PatrickFanella/get-rich-quick/issues/68)   | Implement backtesting engine core                             |  L   | None    | IMPLEMENTED | —               |
| 3   | [#70](https://github.com/PatrickFanella/get-rich-quick/issues/70)   | Define backtest result schema and metrics                     |  S   | None    | IMPLEMENTED | —               |
| 4   | [#73](https://github.com/PatrickFanella/get-rich-quick/issues/73)   | Implement historical data replay manager                      |  M   | None    | IMPLEMENTED | —               |
| 5   | [#308](https://github.com/PatrickFanella/get-rich-quick/issues/308) | Build backtest report generator with structured summary       |  M   | None    | IMPLEMENTED | —               |
| 6   | [#316](https://github.com/PatrickFanella/get-rich-quick/issues/316) | Implement end-to-end backtest integration test                |  L   | None    | READY       | Claude Opus 4.6 |
| 7   | [#317](https://github.com/PatrickFanella/get-rich-quick/issues/317) | Build backtest comparison API for querying historical results |  S   | #74     | BLOCKED     | GPT-5.4         |

**Status:** #66, #68, #70, #73, #308 all exist in `internal/backtest/` (30 files with tests). Verify acceptance criteria and close. #315 (wire backtest to scheduler) is also implemented — close separately. #316 needs a real integration test. #317 blocked on API layer (#74).

---

## Track C: API Layer

> Depends on: Nothing for #74. #77 and #88 depend on #74.

| #   | Issue                                                             | Title                                               | Size | Blocker | Status  | Model           |
| --- | ----------------------------------------------------------------- | --------------------------------------------------- | :--: | ------- | ------- | --------------- |
| 1   | [#74](https://github.com/PatrickFanella/get-rich-quick/issues/74) | Implement REST API server with chi router           |  L   | None    | READY   | Claude Opus 4.6 |
| 2   | [#77](https://github.com/PatrickFanella/get-rich-quick/issues/77) | Implement WebSocket server for real-time streaming  |  M   | #74     | BLOCKED | Claude Opus 4.6 |
| 3   | [#88](https://github.com/PatrickFanella/get-rich-quick/issues/88) | Implement JWT authentication and API key management |  M   | #74     | BLOCKED | GPT-5.4         |

**Design reference:** `docs/design/api-design.md` specifies all endpoints (strategies, pipeline runs, portfolio, orders, trades, memory, risk controls, config) plus WebSocket event types. JWT + API key dual auth.

**Execution order:** #74 first (all endpoints + middleware skeleton), then #77 and #88 in parallel.

---

## Track D: CI/CD & Infrastructure

> Depends on: Nothing for #90. #91 depends on test suites existing. #94 depends on #91.

| #   | Issue                                                             | Title                                                 | Size | Blocker | Status  | Model   |
| --- | ----------------------------------------------------------------- | ----------------------------------------------------- | :--: | ------- | ------- | ------- |
| 1   | [#90](https://github.com/PatrickFanella/get-rich-quick/issues/90) | Create multi-stage Docker production build            |  S   | None    | READY   | GPT-5.4 |
| 2   | [#91](https://github.com/PatrickFanella/get-rich-quick/issues/91) | Set up GitHub Actions CI pipeline                     |  M   | None    | BLOCKED | GPT-5.4 |
| 3   | [#94](https://github.com/PatrickFanella/get-rich-quick/issues/94) | Implement GitHub Actions CD pipeline for Docker image |  S   | #91     | BLOCKED | GPT-5.4 |

**Design reference:** `docs/design/infrastructure/deployment-and-operations.md` specifies multi-stage Alpine build, PostgreSQL service containers for CI, and Docker image publishing.

**Execution order:** #90 first (Dockerfile), #91 (CI with `go test -race -cover`), then #94 (CD publishes image on merge to main).

---

## Track E: Observability

> Depends on: Nothing for #80. #83 and #85 depend on #80.

| #   | Issue                                                             | Title                                            | Size | Blocker | Status  | Model   |
| --- | ----------------------------------------------------------------- | ------------------------------------------------ | :--: | ------- | ------- | ------- |
| 1   | [#80](https://github.com/PatrickFanella/get-rich-quick/issues/80) | Implement Prometheus metrics exposition          |  M   | None    | READY   | GPT-5.4 |
| 2   | [#83](https://github.com/PatrickFanella/get-rich-quick/issues/83) | Create Grafana dashboard configurations          |  M   | #80     | BLOCKED | GPT-5.4 |
| 3   | [#85](https://github.com/PatrickFanella/get-rich-quick/issues/85) | Implement alerting rules and notification system |  M   | #80     | BLOCKED | GPT-5.4 |

**Design reference:** `docs/design/infrastructure/deployment-and-operations.md` specifies 6 metric families: `tradingagent_pipeline_runs_total`, `tradingagent_pipeline_duration_seconds`, `tradingagent_llm_calls_total`, `tradingagent_llm_tokens_total`, `tradingagent_orders_total`, `tradingagent_portfolio_value`. Grafana dashboards for Pipeline Health, LLM Usage, Trading, Risk, System. Alerting via Telegram + email.

**Execution order:** #80 first (add prometheus/client_golang, instrument pipeline + LLM + execution), then #83 and #85 in parallel.

---

## Track F: Paper Trading

> Depends on: Track C #74 (API server), Track E #80 (metrics). ADRs #93 and #96 should be decided first.

| #   | Issue                                                             | Title                                                     | Size | Blocker     | Status      | Model           |
| --- | ----------------------------------------------------------------- | --------------------------------------------------------- | :--: | ----------- | ----------- | --------------- |
| 1   | [#93](https://github.com/PatrickFanella/get-rich-quick/issues/93) | ADR-005: Position sizing strategy selection               |  XS  | None        | READY       | Human           |
| 2   | [#96](https://github.com/PatrickFanella/get-rich-quick/issues/96) | ADR-006: Paper trading slippage and fee assumptions       |  XS  | None        | READY       | Human           |
| 3   | [#82](https://github.com/PatrickFanella/get-rich-quick/issues/82) | Implement kill switch with three activation mechanisms    |  M   | None        | IMPLEMENTED | —               |
| 4   | [#86](https://github.com/PatrickFanella/get-rich-quick/issues/86) | Implement circuit breaker state machine                   |  M   | None        | IMPLEMENTED | —               |
| 5   | [#87](https://github.com/PatrickFanella/get-rich-quick/issues/87) | Implement position limit enforcement                      |  M   | None        | IMPLEMENTED | —               |
| 6   | [#89](https://github.com/PatrickFanella/get-rich-quick/issues/89) | Implement stop-loss and take-profit order management      |  M   | #93         | BLOCKED     | Claude Opus 4.6 |
| 7   | [#79](https://github.com/PatrickFanella/get-rich-quick/issues/79) | Define and implement 60-day paper trading validation plan |  M   | #74,#80,#89 | BLOCKED     | Claude Opus 4.6 |

**Status:** #82, #86, #87 are fully implemented in `internal/risk/` with tests (kill switch: API/file/env, circuit breaker: open/tripped/cooldown with auto-reset, position limits: per-position/total/concurrent/per-market). Verify and close.

#89 is **partial** — `TradingPlan` stores stop-loss/take-profit values and positions persist them, but the execution engine does not actively enforce SL/TP exits. Needs: automated exit logic in the order manager or a price-monitoring goroutine.

#79 is the capstone — requires API (#74) for strategy management, metrics (#80) for monitoring, and SL/TP enforcement (#89) for safety. The 60-day validation targets Sharpe > 1.0.

---

## Track G: Architecture Decisions

> Depends on: Nothing for decisions. Implementation depends on the chosen direction.

| #   | Issue                                                               | Title                                             | Size | Blocker | Status  | Model   |
| --- | ------------------------------------------------------------------- | ------------------------------------------------- | :--: | ------- | ------- | ------- |
| 1   | [#110](https://github.com/PatrickFanella/get-rich-quick/issues/110) | ADR-007: Deployment topology and scaling strategy |  S   | None    | READY   | Human   |
| 2   | [#111](https://github.com/PatrickFanella/get-rich-quick/issues/111) | ADR-008: Correlated asset exposure management     |  S   | None    | READY   | Human   |
| 3   | [#112](https://github.com/PatrickFanella/get-rich-quick/issues/112) | ADR-009: Human review gate before live trading    |  S   | #74     | BLOCKED | Human   |
| 4   | [#113](https://github.com/PatrickFanella/get-rich-quick/issues/113) | Implement end-to-end smoke test                   |  M   | #74     | BLOCKED | GPT-5.4 |

**Status:** ADRs #110 and #111 can be decided anytime. #112 (human review gate) and #113 (smoke test) need the API layer to exist.

---

## Dependency Graph

```
Track A (Architecture Improvements) ─── independent ─── all ready
Track B (Phase 4 Completion) ─── independent ─── mostly implemented
Track D #90 (Docker) ─── independent

Track G #93, #96 (ADRs) ─── independent
Track G #110, #111 (ADRs) ─── independent

Track C #74 (REST API) ─── independent
  ├── Track C #77 (WebSocket) ─── blocked by #74
  ├── Track C #88 (JWT auth) ─── blocked by #74
  ├── Track B #317 (Backtest comparison API) ─── blocked by #74
  ├── Track G #112 (Human review gate ADR) ─── blocked by #74
  └── Track G #113 (Smoke test) ─── blocked by #74

Track D #91 (CI) ─── blocked by test suites
  └── Track D #94 (CD) ─── blocked by #91

Track E #80 (Prometheus) ─── independent
  ├── Track E #83 (Grafana) ─── blocked by #80
  └── Track E #85 (Alerting) ─── blocked by #80

Track F #89 (SL/TP management) ─── blocked by ADR #93
  └── Track F #79 (60-day validation) ─── blocked by #74, #80, #89
```

---

## Recommended Wave Execution

### Wave 1 — Parallel, no dependencies (17 issues)

**Verify + close implemented issues:**

- Track A: #342, #343, #344, #345, #346 (arch improvements — code on main)
- Track B: #66, #68, #70, #73, #308, #315 (backtest engine — code on main)
- Track F: #82, #86, #87 (risk controls — code on main)

**Start new work:**

- Track D: #90 (Docker build)
- Track G: #93, #96 (paper trading ADRs — decisions needed)
- Track G: #110, #111 (infrastructure + risk ADRs — decisions needed)

### Wave 2 — Core infrastructure (5 issues)

- Track C: **#74** (REST API server) — largest item, unblocks 5+ issues
- Track E: **#80** (Prometheus metrics)
- Track D: **#91** (CI pipeline)
- Track B: **#316** (backtest integration test)
- Track A: **#347** (typed phase I/O migration — can start anytime)

### Wave 3 — API extensions + observability (5 issues)

- Track C: **#77** (WebSocket) — blocked by #74
- Track C: **#88** (JWT auth) — blocked by #74
- Track E: **#83** (Grafana dashboards) — blocked by #80
- Track E: **#85** (Alerting) — blocked by #80
- Track D: **#94** (CD pipeline) — blocked by #91

### Wave 4 — Paper trading preparation (3 issues)

- Track B: **#317** (backtest comparison API) — blocked by #74
- Track F: **#89** (SL/TP enforcement) — blocked by ADR #93
- Track G: **#112** (human review gate ADR) — blocked by #74

### Wave 5 — Validation (2 issues)

- Track G: **#113** (end-to-end smoke test) — blocked by #74
- Track F: **#79** (60-day paper trading validation) — blocked by #74, #80, #89

---

## Key Design Principles

1. **Close before opening** — 13 issues are already implemented. Verify and close them before starting new work to keep the issue tracker honest.
2. **API is the critical path** — #74 (REST API) unblocks 5+ downstream issues including WebSocket, backtest comparison, smoke test, and paper trading validation. Start it in Wave 2.
3. **Observability before validation** — Prometheus (#80) and alerting (#85) must be running before starting the 60-day paper trading validation so failures are visible.
4. **ADRs before implementation** — Position sizing (#93) and slippage assumptions (#96) must be decided before implementing SL/TP enforcement (#89), which in turn must be done before paper trading.
5. **SL/TP is the missing safety net** — Risk controls (kill switch, circuit breaker, position limits) are complete, but automated stop-loss/take-profit exits are stored-but-not-enforced. This is the main gap between "engine works" and "safe to paper trade."

## Model Recommendations

| Complexity | Model           | Use For                                                         |
| ---------- | --------------- | --------------------------------------------------------------- |
| High       | Claude Opus 4.6 | REST API server (#74), WebSocket (#77), SL/TP enforcement (#89) |
| Medium     | GPT-5.4         | Docker (#90), CI/CD (#91, #94), Prometheus (#80), Grafana (#83) |
| Low        | GPT-5.4         | JWT auth (#88), alerting (#85), comparison API (#317)           |
| —          | Human           | All ADRs (#93, #96, #110, #111, #112)                           |
