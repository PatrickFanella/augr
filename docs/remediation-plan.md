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

Completed evidence (2026-07-12): stock and options generators use the shared
balanced-object extractor, reject empty/invalid results, hash rather than log
response bodies, and publish terminal outcomes through
`tradingagent_generator_outcomes_total`. All in-process Reddit consumers share
one Retry-After-aware cooldown with jitter and stop fan-out after a 429. Source
freshness and cooldown state are exported as
`tradingagent_data_source_last_success_unixtime` and
`tradingagent_data_source_cooldown_until_unixtime`. Regression coverage and the
full Go test/vet gates pass.

## Phase 2 — Engineering quality gates

- Restore warning-free frontend lint and deterministic frontend tests.
- Split the monolithic application test suite and clean up async resources.
- Add provider and runtime-composition coverage for currently untested seams.

Exit gate: Go tests/vet and frontend lint/build/tests pass repeatedly in CI.

Completed evidence (2026-07-12): previously untested Finnhub, FMP,
StockTwits, and Tradier HTTP seams now have contract tests; the tests also
closed Tradier empty-token and malformed-expiration failures. The frontend
suite uses a reusable isolated MSW/WebSocket harness, with authentication,
cockpit, and risk-control workflows split from the remaining feature suite.
All 138 frontend tests pass across nine files, lint/type-check/build are clean,
the full Go test/vet gates pass, and both CI workflows pass `actionlint` after
repairing the nested smoke job and invalid job-level `hashFiles` expressions.

## Phase 3 — Frontend correctness and visual integrity

- Fix shell accessibility, mobile state, broker-mode presentation, 404/error
  boundaries, activity-drawer behavior, status pills, spacing, and theme charts.
- Replace misleading P&L/allocation charts with source-backed time series and
  notional exposure; replace free-text enums with constrained controls.
- Reconcile the active design system with its documentation.

Exit gate: responsive and keyboard QA pass, charts reconcile to APIs, and the
UI never presents inferred or misleading operational state.

Completed evidence (2026-07-12): the shell distinguishes paper, live, mixed,
and unknown broker modes; authenticated unknown routes render a recoverable
404 and route failures have a non-trading error boundary. Mobile navigation
and the compact activity drawer expose correct expanded state, Escape dismiss,
focus containment/restoration, semantic backdrops, and touch-sized controls.
Desktop rail/drawer dimensions now match the design specification. Unknown
statuses remain text-visible, allocator enums use constrained controls, and
the hard-coded ticker `Live` claim is removed. Fabricated single-point and
cross-position P&L curves and P&L-weighted allocation pies were replaced by
explicit snapshots and source-backed position notional; the unused chart
dependency was removed. Light/dark theme and chart rules are reconciled in the
design-system documentation. Responsive/keyboard regression tests, all 141
frontend tests, lint, type-check, and production build pass.

## Phase 4 — Product-surface recovery

- Restore settings/readiness, prediction markets, options, backtests, decision
  journal, replay, discovery/signals, reliability, memory, prompts, API keys,
  universe/calendar, and Surfers operations using the current client and shell.

Exit gate: each significant backend capability is exposed or explicitly marked
API-only, with tested loading, empty, error, stale, and unavailable states.

Completed evidence (2026-07-12): the current shell now exposes read-only
settings/readiness, shared prediction-market readiness and feed health, options
chain research, persisted backtest definitions/runs, the trade-decision journal,
and deterministic decision replay. Each uses typed runtime validation and
distinguishes successful, empty, failed, and unconfigured responses without
hiding healthy sibling data. The
[[frontend/capability-surface-matrix|capability surface matrix]] classifies every
remaining significant backend capability as API-only with its operational or
safety boundary, including detailed event-market operations, discovery/signals,
memory, prompt and key administration, universe/calendar, reliability metrics,
and Surfers. No recovered surface enables live trading.

## Phase 5 — Options product completion

- Add a dedicated options runtime with chain-aware structured plans, contract
  selection, single-leg/spread routing, options risk, persistence, lifecycle,
  reconciliation, and operator UI.

Exit gate: an end-to-end defined-risk paper trade survives restart and closes
without using generic stock-order semantics.

Completed evidence (2026-07-12): options strategies now route through a
dedicated paper-only runtime. `options_rules` selects contracts by delta and
DTE from executable chain quotes; long calls/puts and same-expiry 1:1 bull-call
and bear-put debit verticals use explicit OCC identity, bid/ask pricing, whole-
contract sizing, and options-specific open/close intents. Live options,
uncovered shorts, credit/complex spreads, invalid quotes, and ambiguous leg
groups fail closed. Premium/max-risk exposure includes the contract multiplier,
and deterministic portfolio caps enforce delta, one-percent-move gamma, vega,
and daily theta. Orders, positions, trades, Greeks, fees, premiums, and leg
groups round-trip through PostgreSQL; startup reconstructs paper cash/equity.
Strategy exits atomically close single positions or both spread legs, while a
registered after-hours workflow cash-settles expiration and a reconciliation
workflow detects partial lifecycle graphs. Current Orders and Portfolio screens
show contract, intent, multiplier, group, and Greeks metadata. Full Go tests/vet
and all 158 frontend tests, lint, type-check, and production build pass.

## Phase 6 — Prediction-market decision architecture

- Keep deterministic execution authoritative; define the LLM as advisory
  research. Persist evidence, probabilities, calibration, edge, risk decisions,
  execution, and settlement consistently for Kalshi and Polymarket.

Exit gate: every event-market paper trade is replayable from evidence through
settlement and no LLM output can bypass deterministic gates.

Status: complete. Both providers now pass discovery output through native
deterministic quote, liquidity, confidence, and spread-adjusted edge gates.
The shared decision journal persists the executable snapshot, outcome,
probability proxy and explicit uncalibrated status, evidence sources, gate
results, risk review, side-qualified order, fill, position, and 0/1 settlement.
Replay events are written for each lifecycle transition. Polymarket resolution
processing and scheduled Kalshi settlement use the same idempotent paper
settler; live activation remains independently gated and disabled by default.

## Phase 7 — Backtesting and decision quality

- Implement configurable trailing stops and asset-specific fill assumptions.
- Version simulation inputs, prevent look-ahead, report paper/live divergence,
  and calibrate outcomes by strategy, market, regime, provider, and confidence.

Exit gate: backtests are reproducible, assumptions are explicit, and divergence
and calibration are measurable.

Status: complete. Rules simulations now consume persisted fill assumptions,
support configurable trailing stops, reject ambiguous bar order, and execute
close-derived signals no earlier than the next bar. Options simulations apply
their dedicated spread-slippage and per-contract fee model. Every run stores a
versioned hash over exact bars and assumptions. Deterministic runs exclude LLM
review and random options variants, in-sample rules runs cannot auto-promote a
strategy, the divergence API reads persisted backtest/paper evidence, and
calibration reports segment by strategy, market, provider, regime, and
confidence decile.

## Phase 8 — Operations and release readiness

- Implement capability-level readiness, actionable metrics/alerts, correlated
  redacted logs, and paper soak scenarios for every supported market.
- Require restart, outage, stale-data, rejection, partial-fill, reconciliation,
  kill-switch, WebSocket, settlement, expiration, and assignment drills.

Exit gate: all automated gates and recovery drills pass, decision journals are
complete, reconciliation has no unexplained mismatches, and any live activation
is incremental by broker, market, strategy, and capital tier.

Status: complete for paper-release implementation. The authenticated
capability-readiness endpoint checks database/schema/journal, scheduler,
provider data, settlement jobs, and recovery-drill attestation independently;
live execution remains a separate blocked capability. Requests carry validated
correlation IDs, both prediction providers expose reconciliation metrics,
Prometheus ships actionable runbook-linked alerts, and the executable release
gate validates all Go/frontend/Compose/alert rules plus a machine-checked
11-scenario drill manifest. Deployment-specific soak attestation remains
fail-closed through `RELEASE_DRILLS_VERIFIED` and cannot enable live trading.

## Completion evidence

Completion requires a requirement-by-requirement audit against current code,
tests, rendered UI, runtime health, logs, migrations, runbooks, and release-gate
results. Passing narrow unit tests is not proof of a phase-wide acceptance gate.
