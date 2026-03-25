---
title: Phase 4 Execution Paths
type: tracking
created: 2026-03-25
tags: [tracking, phase-4, backtesting, simulation]
---

# Phase 4: Execution Paths

> 25 issues across 5 tracks. **6 ready now**, 19 blocked by dependencies.
> Updated: 2026-03-25
> Tracking issue: [#292](https://github.com/PatrickFanella/get-rich-quick/issues/292)

## Summary

| Track | Name                      | Total  | Ready | Blocked | Models          |
| ----- | ------------------------- | :----: | :---: | :-----: | --------------- |
| A     | Historical Data Infra     |   4    |   4   |    0    | GPT-5.4         |
| B     | Backtest Engine Core      |   7    |   0   |    7    | Mixed           |
| C     | Performance Analytics     |   5    |   0   |    5    | GPT-5.3-Codex   |
| D     | Walk-Forward & Robustness |   4    |   0   |    4    | Claude Opus 4.6 |
| E     | API & Integration         |   5    |   2   |    3    | GPT-5.4         |
|       | **Total**                 | **25** | **6** | **19**  |                 |

---

## Track A: Historical Data Infrastructure

> Depends on: Nothing (fully independent)

| #   | Issue                                                               | Title                                                     | Size | Blocker | Status  | Model           |
| --- | ------------------------------------------------------------------- | --------------------------------------------------------- | :--: | ------- | ------- | --------------- |
| 1   | [#293](https://github.com/PatrickFanella/get-rich-quick/issues/293) | Implement historical data loader with bulk OHLCV download |  M   | None    | READY   | GPT-5.4         |
| 2   | [#294](https://github.com/PatrickFanella/get-rich-quick/issues/294) | Implement historical data cache with date-range indexing  |  S   | #293    | BLOCKED | GPT-5.4         |
| 3   | [#295](https://github.com/PatrickFanella/get-rich-quick/issues/295) | Build historical news/sentiment snapshot loader           |  M   | None    | READY   | GPT-5.4         |
| 4   | [#296](https://github.com/PatrickFanella/get-rich-quick/issues/296) | Implement data replay iterator with look-ahead prevention |  M   | #293    | BLOCKED | Claude Opus 4.6 |

**Sequential start:** #1 first (or #1 and #3 in parallel), then #2 and #4.

---

## Track B: Backtest Engine Core

> Depends on: Track A (#4 data replay iterator)

| #   | Issue                                                               | Title                                                                   | Size | Blocker   | Status  | Model           |
| --- | ------------------------------------------------------------------- | ----------------------------------------------------------------------- | :--: | --------- | ------- | --------------- |
| 1   | [#297](https://github.com/PatrickFanella/get-rich-quick/issues/297) | Implement backtest clock with simulated wall-clock                      |  S   | #296      | BLOCKED | GPT-5.4         |
| 2   | [#298](https://github.com/PatrickFanella/get-rich-quick/issues/298) | Implement simulated order fill engine (slippage, spread, partial fills) |  L   | #297      | BLOCKED | Claude Opus 4.6 |
| 3   | [#299](https://github.com/PatrickFanella/get-rich-quick/issues/299) | Build backtest broker adapter implementing existing Broker interface    |  M   | #298      | BLOCKED | GPT-5.4         |
| 4   | [#300](https://github.com/PatrickFanella/get-rich-quick/issues/300) | Implement backtest pipeline runner with historical data context         |  L   | #299      | BLOCKED | Claude Opus 4.6 |
| 5   | [#301](https://github.com/PatrickFanella/get-rich-quick/issues/301) | Build LLM response cache (prompt hash + model version keying)           |  M   | None      | BLOCKED | GPT-5.4         |
| 6   | [#302](https://github.com/PatrickFanella/get-rich-quick/issues/302) | Implement backtest position and P&L tracker with equity curve           |  M   | #298      | BLOCKED | GPT-5.4         |
| 7   | [#303](https://github.com/PatrickFanella/get-rich-quick/issues/303) | Build backtest orchestrator (strategy + date range + params → results)  |  L   | #300,#302 | BLOCKED | Claude Opus 4.6 |

**Execution order:** B#1 → B#2 → B#3, B#6 (parallel) → B#4 → B#7. B#5 can start anytime.

---

## Track C: Performance Analytics

> Depends on: Track B (#6 position tracker, #7 orchestrator)

| #   | Issue                                                               | Title                                                                      | Size | Blocker     | Status  | Model         |
| --- | ------------------------------------------------------------------- | -------------------------------------------------------------------------- | :--: | ----------- | ------- | ------------- |
| 1   | [#304](https://github.com/PatrickFanella/get-rich-quick/issues/304) | Implement core metrics: Sharpe, Sortino, max drawdown, Calmar, win rate    |  M   | #303        | BLOCKED | GPT-5.3-Codex |
| 2   | [#305](https://github.com/PatrickFanella/get-rich-quick/issues/305) | Implement trade analytics: holding periods, frequency, consecutive streaks |  S   | #303        | BLOCKED | GPT-5.3-Codex |
| 3   | [#306](https://github.com/PatrickFanella/get-rich-quick/issues/306) | Build equity curve generator with drawdown overlay                         |  S   | #302        | BLOCKED | GPT-5.4       |
| 4   | [#307](https://github.com/PatrickFanella/get-rich-quick/issues/307) | Implement benchmark comparison: alpha/beta against buy-and-hold            |  S   | #304        | BLOCKED | GPT-5.3-Codex |
| 5   | [#308](https://github.com/PatrickFanella/get-rich-quick/issues/308) | Build backtest report generator: structured summary with all analytics     |  M   | #304–#307   | BLOCKED | GPT-5.4       |

**Execution order:** C#1, C#2, C#3 in parallel → C#4 → C#5.

---

## Track D: Walk-Forward & Robustness

> Depends on: Track B (#7 orchestrator) + Track C (#1 metrics)

| #   | Issue                                                               | Title                                                               | Size | Blocker    | Status  | Model           |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | :--: | ---------- | ------- | --------------- |
| 1   | [#309](https://github.com/PatrickFanella/get-rich-quick/issues/309) | Implement walk-forward analysis with rolling calibrate/test windows |  L   | #303,#304  | BLOCKED | Claude Opus 4.6 |
| 2   | [#310](https://github.com/PatrickFanella/get-rich-quick/issues/310) | Build multi-run aggregator for non-determinism quantification       |  M   | #303       | BLOCKED | Claude Opus 4.6 |
| 3   | [#311](https://github.com/PatrickFanella/get-rich-quick/issues/311) | Implement prompt versioning integration for A/B comparison          |  S   | #310       | BLOCKED | GPT-5.4         |
| 4   | [#312](https://github.com/PatrickFanella/get-rich-quick/issues/312) | Build strategy comparison runner with side-by-side metrics          |  M   | #304       | BLOCKED | GPT-5.4         |

**Execution order:** D#1, D#2 in parallel → D#3 → D#4.

---

## Track E: API & Integration

> Depends on: Tracks B-D for full functionality; repos can start early.

| #   | Issue                                                               | Title                                                                    | Size | Blocker    | Status  | Model           |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------------ | :--: | ---------- | ------- | --------------- |
| 1   | [#313](https://github.com/PatrickFanella/get-rich-quick/issues/313) | Implement backtest configuration repository (CRUD)                       |  M   | None       | READY   | GPT-5.4         |
| 2   | [#314](https://github.com/PatrickFanella/get-rich-quick/issues/314) | Build backtest run repository (results, metrics, trade logs)             |  M   | None       | READY   | GPT-5.4         |
| 3   | [#315](https://github.com/PatrickFanella/get-rich-quick/issues/315) | Wire backtest orchestrator into scheduler for cron-triggered runs        |  S   | #303       | BLOCKED | GPT-5.4         |
| 4   | [#316](https://github.com/PatrickFanella/get-rich-quick/issues/316) | Implement end-to-end integration test: data → pipeline → fills → metrics |  L   | All        | BLOCKED | Claude Opus 4.6 |
| 5   | [#317](https://github.com/PatrickFanella/get-rich-quick/issues/317) | Build backtest comparison API: query and compare historical run results  |  S   | #314,#312  | BLOCKED | GPT-5.4         |

**Sequential start:** E#1, E#2 (parallel, ready now) → E#3 → E#5 → E#4 last.

---

## Cross-Track Dependencies

```mermaid
graph LR
    A[Track A: Historical Data] --> B[Track B: Backtest Engine]
    B --> C[Track C: Performance Analytics]
    B --> D[Track D: Walk-Forward & Robustness]
    C --> D
    B --> E[Track E: API & Integration]
    C --> E
    D --> E

    style A fill:#3b82f6
    style B fill:#a855f7
    style C fill:#22c55e
    style D fill:#ef4444
    style E fill:#eab308
```

---

## Key Design Principles

### Same Execution Engine

Reuse existing `Broker` interface, `Pipeline`, risk engine, and order management. The only new adapter is a simulated broker filling against historical prices. Backtest behavior must match live trading.

### LLM Cost Control

Each pipeline invocation costs $10-40 in inference fees. The LLM response cache (keyed by prompt hash + model version) prevents redundant calls during parameter sweeps. Log cache hit rates per run.

### Look-Ahead Prevention

The data replay iterator and backtest clock enforce strict temporal ordering. The pipeline only receives data with timestamps <= current simulated time. No future data leaks.

### Non-Determinism Mitigation

LLM outputs vary even at temperature 0. The multi-run aggregator runs each configuration 3+ times and reports mean +/- std for all metrics. Treat single-run results with skepticism.

### Transaction Cost Realism

Model commissions, bid-ask spreads, and exchange-specific fees. The simulated fill engine supports configurable slippage models (fixed, proportional, volatility-scaled).

---

## Model Selection Guide

| Task type                            | Recommended model | Why                                        |
| ------------------------------------ | ----------------- | ------------------------------------------ |
| Backtest orchestration, walk-forward | Claude Opus 4.6   | Complex control flow, temporal consistency |
| Simulated fill engine                | Claude Opus 4.6   | Financial logic, edge cases, partial fills |
| HTTP clients, data loaders           | GPT-5.4           | Straightforward API integration            |
| Performance metrics (math)           | GPT-5.3-Codex     | Algorithm-focused, precise computation     |
| Repository CRUD, cache wiring        | GPT-5.4           | Follows established patterns               |
| Prompt versioning, comparison API    | GPT-5.4           | Standard patterns, well-scoped             |
| E2E integration test                 | Claude Opus 4.6   | Multi-component orchestration              |

---

## Recommended Assignment Order

### Wave 1 (start now — 4 parallel, all independent)

| Issue | Title                             | Track    | Model   |
| ----- | --------------------------------- | -------- | ------- |
| [#293](https://github.com/PatrickFanella/get-rich-quick/issues/293) | Historical data loader            | A - Data | GPT-5.4 |
| [#295](https://github.com/PatrickFanella/get-rich-quick/issues/295) | Historical news/sentiment loader  | A - Data | GPT-5.4 |
| [#313](https://github.com/PatrickFanella/get-rich-quick/issues/313) | Backtest configuration repository | E - API  | GPT-5.4 |
| [#314](https://github.com/PatrickFanella/get-rich-quick/issues/314) | Backtest run repository           | E - API  | GPT-5.4 |

### Wave 2 (after wave 1)

| Issue | Title                 | Track      | Model           | Unblocked by |
| ----- | --------------------- | ---------- | --------------- | ------------ |
| [#294](https://github.com/PatrickFanella/get-rich-quick/issues/294) | Historical data cache | A - Data   | GPT-5.4         | #293         |
| [#296](https://github.com/PatrickFanella/get-rich-quick/issues/296) | Data replay iterator  | A - Data   | Claude Opus 4.6 | #293         |
| [#301](https://github.com/PatrickFanella/get-rich-quick/issues/301) | LLM response cache    | B - Engine | GPT-5.4         | —            |

### Wave 3 (after wave 2)

| Issue | Title                 | Track      | Model           | Unblocked by |
| ----- | --------------------- | ---------- | --------------- | ------------ |
| [#297](https://github.com/PatrickFanella/get-rich-quick/issues/297) | Backtest clock        | B - Engine | GPT-5.4         | #296         |
| [#298](https://github.com/PatrickFanella/get-rich-quick/issues/298) | Simulated fill engine | B - Engine | Claude Opus 4.6 | #297         |

### Wave 4 (after wave 3)

| Issue | Title                   | Track         | Model   | Unblocked by |
| ----- | ----------------------- | ------------- | ------- | ------------ |
| [#299](https://github.com/PatrickFanella/get-rich-quick/issues/299) | Backtest broker adapter | B - Engine    | GPT-5.4 | #298         |
| [#302](https://github.com/PatrickFanella/get-rich-quick/issues/302) | Backtest P&L tracker    | B - Engine    | GPT-5.4 | #298         |
| [#306](https://github.com/PatrickFanella/get-rich-quick/issues/306) | Equity curve generator  | C - Analytics | GPT-5.4 | #302         |

### Wave 5 (after wave 4)

| Issue | Title                    | Track          | Model           | Unblocked by |
| ----- | ------------------------ | -------------- | --------------- | ------------ |
| [#300](https://github.com/PatrickFanella/get-rich-quick/issues/300) | Backtest pipeline runner | B - Engine     | Claude Opus 4.6 | #299         |
| [#304](https://github.com/PatrickFanella/get-rich-quick/issues/304) | Core performance metrics | C - Analytics  | GPT-5.3-Codex   | #303         |
| [#305](https://github.com/PatrickFanella/get-rich-quick/issues/305) | Trade analytics          | C - Analytics  | GPT-5.3-Codex   | #303         |
| [#310](https://github.com/PatrickFanella/get-rich-quick/issues/310) | Multi-run aggregator     | D - Robustness | Claude Opus 4.6 | #303         |

### Wave 6 (after wave 5)

| Issue | Title                         | Track          | Model           | Unblocked by |
| ----- | ----------------------------- | -------------- | --------------- | ------------ |
| [#303](https://github.com/PatrickFanella/get-rich-quick/issues/303) | Backtest orchestrator         | B - Engine     | Claude Opus 4.6 | #300, #302   |
| [#307](https://github.com/PatrickFanella/get-rich-quick/issues/307) | Benchmark comparison          | C - Analytics  | GPT-5.3-Codex   | #304         |
| [#309](https://github.com/PatrickFanella/get-rich-quick/issues/309) | Walk-forward analysis         | D - Robustness | Claude Opus 4.6 | #303, #304   |
| [#312](https://github.com/PatrickFanella/get-rich-quick/issues/312) | Strategy comparison runner    | D - Robustness | GPT-5.4         | #304         |
| [#311](https://github.com/PatrickFanella/get-rich-quick/issues/311) | Prompt versioning integration | D - Robustness | GPT-5.4         | #310         |

### Wave 7 (after wave 6)

| Issue | Title                       | Track         | Model           | Unblocked by |
| ----- | --------------------------- | ------------- | --------------- | ------------ |
| [#308](https://github.com/PatrickFanella/get-rich-quick/issues/308) | Backtest report generator   | C - Analytics | GPT-5.4         | #304–#307    |
| [#315](https://github.com/PatrickFanella/get-rich-quick/issues/315) | Scheduler integration       | E - API       | GPT-5.4         | #303         |
| [#317](https://github.com/PatrickFanella/get-rich-quick/issues/317) | Comparison API              | E - API       | GPT-5.4         | #314, #312   |
| [#316](https://github.com/PatrickFanella/get-rich-quick/issues/316) | End-to-end integration test | E - API       | Claude Opus 4.6 | All          |
