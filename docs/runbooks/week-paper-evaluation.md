---
title: "Seven-day paper evaluation"
status: "canonical"
updated: "2026-07-12"
---

# Seven-day paper evaluation

This is an operational soak, not enough evidence for live activation. Live
trading must remain disabled. The longer 60-day validation thresholds still
govern any later live-capital decision.

## Start gate

1. Confirm `ENABLE_LIVE_TRADING=false`, broker sandbox/paper modes, schema 54,
   healthy database/Redis, current app/web images, and a healthy `augr-api`
   Prometheus target.
2. Confirm one usable LLM provider. A fallback-only week measures deterministic
   resilience, not LLM decision quality.
3. Run `./scripts/release-gate.sh`.
4. Initialize exactly once with `./scripts/paper-week.sh init`. The ignored
   `var/paper-week/start.env` fixes the timestamp and Git commit for all reports.
   `cohort.ids` freezes the active paper-strategy population at that moment.

## Daily workflow

Run `./scripts/paper-week.sh status` before market activity and
`./scripts/paper-week.sh snapshot` after the final scheduled cycle. Review:

- pipeline completion/failure and BUY/SELL/HOLD distribution by market;
- order fill percentage, rejection/partial-fill state, fees, and notional;
- open/closed positions plus realized and unrealized P&L;
- missing decision evidence, missing paper-order links, and replay coverage;
- validation report errors, provider/model latency, tokens, and recorded cost;
- provider freshness, generator outcomes, reconciliation, and alert state.

For every anomaly, retain the snapshot path, request/correlation ID, affected
strategy/run/order/decision IDs, and the corrective action. Do not edit a
strategy mid-window without recording the timestamp and reason; otherwise its
before/after results cannot be compared honestly.

## Week acceptance criteria

- No live orders and no unexplained broker/local reconciliation drift.
- Schema, API, scheduler, database, Redis, and Prometheus remain available.
- At least one complete scheduled cycle for each enabled paper market.
- No stuck runs; explain every failed run and report error.
- Every paper-ordered decision has its order link and every decision has replay
  evidence appropriate to its lifecycle.
- Settlement/expiration jobs complete for any contracts that resolve or expire.
- Latency, token use, cost, and provider failures remain within the operator's
  stated budget; fallback behavior is identifiable rather than silent.
- P&L, win rate, fill rate, and drawdown are reported, but are explicitly
  inconclusive if the week has too few closed round trips.

At week end, preserve the final snapshot and compare it with the initialization
snapshot. A clean week permits a longer paper-validation period; it does not by
itself set `RELEASE_DRILLS_VERIFIED=true` or enable live trading.
