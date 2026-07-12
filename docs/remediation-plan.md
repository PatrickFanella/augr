---
title: "Augr Remediation Plan"
status: "active"
updated: "2026-07-12"
tags: [remediation, roadmap, operations, frontend, trading]
---

# Augr Remediation Plan

This is the authoritative delivery plan for closing the repository audit. Work
is paper-first. Live trading remains disabled until the final readiness gate.
Each milestone requires implementation, focused verification, documentation,
and a focused commit before dependent work begins.

## Phase 0 — Baseline and safeguards

- Preserve user-owned worktree changes and document repository divergence.
- Maintain this plan and the implementation board as completion evidence.
- Add reproducible capability and repository-status checks.
- Record existing test, lint, runtime, migration, and provider-health failures.

Exit gate: one authoritative history, no unexplained changes, and every audit
finding mapped to a phase and acceptance test.

## Phase 1 — Active operational failures

- Make structured LLM output resilient across stock and options discovery.
- Remove full response bodies from generator logs and add outcome metrics.
- Centralize Reddit/provider cooldowns, honor retry guidance, add jitter, and
  surface source freshness.

Exit gate: malformed prose-wrapped JSON is recovered safely, invalid output is
rejected, generator outcomes are measurable, and ingestion no longer loops on
rate limits.

## Phase 2 — Engineering quality gates

- Restore warning-free frontend lint and deterministic frontend tests.
- Split the monolithic application test suite and clean up async resources.
- Add provider and runtime-composition coverage for currently untested seams.

Exit gate: Go tests/vet and frontend lint/build/tests pass repeatedly in CI.

## Phase 3 — Frontend correctness and visual integrity

- Fix shell accessibility, mobile state, broker-mode presentation, 404/error
  boundaries, activity-drawer behavior, status pills, spacing, and theme charts.
- Replace misleading P&L/allocation charts with source-backed time series and
  notional exposure; replace free-text enums with constrained controls.
- Reconcile the active design system with its documentation.

Exit gate: responsive and keyboard QA pass, charts reconcile to APIs, and the
UI never presents inferred or misleading operational state.

## Phase 4 — Product-surface recovery

- Restore settings/readiness, prediction markets, options, backtests, decision
  journal, replay, discovery/signals, reliability, memory, prompts, API keys,
  universe/calendar, and Surfers operations using the current client and shell.

Exit gate: each significant backend capability is exposed or explicitly marked
API-only, with tested loading, empty, error, stale, and unavailable states.

## Phase 5 — Options product completion

- Add a dedicated options runtime with chain-aware structured plans, contract
  selection, single-leg/spread routing, options risk, persistence, lifecycle,
  reconciliation, and operator UI.

Exit gate: an end-to-end defined-risk paper trade survives restart and closes
without using generic stock-order semantics.

## Phase 6 — Prediction-market decision architecture

- Keep deterministic execution authoritative; define the LLM as advisory
  research. Persist evidence, probabilities, calibration, edge, risk decisions,
  execution, and settlement consistently for Kalshi and Polymarket.

Exit gate: every event-market paper trade is replayable from evidence through
settlement and no LLM output can bypass deterministic gates.

## Phase 7 — Backtesting and decision quality

- Implement configurable trailing stops and asset-specific fill assumptions.
- Version simulation inputs, prevent look-ahead, report paper/live divergence,
  and calibrate outcomes by strategy, market, regime, provider, and confidence.

Exit gate: backtests are reproducible, assumptions are explicit, and divergence
and calibration are measurable.

## Phase 8 — Operations and release readiness

- Implement capability-level readiness, actionable metrics/alerts, correlated
  redacted logs, and paper soak scenarios for every supported market.
- Require restart, outage, stale-data, rejection, partial-fill, reconciliation,
  kill-switch, WebSocket, settlement, expiration, and assignment drills.

Exit gate: all automated gates and recovery drills pass, decision journals are
complete, reconciliation has no unexplained mismatches, and any live activation
is incremental by broker, market, strategy, and capital tier.

## Completion evidence

Completion requires a requirement-by-requirement audit against current code,
tests, rendered UI, runtime health, logs, migrations, runbooks, and release-gate
results. Passing narrow unit tests is not proof of a phase-wide acceptance gate.
