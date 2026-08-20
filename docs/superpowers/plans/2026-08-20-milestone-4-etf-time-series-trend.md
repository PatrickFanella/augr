# Milestone 4 ETF Time-Series Trend Baseline Plan

**Goal:** Complete OVR-404 with a deterministic, long-only/cash ETF
time-series trend baseline whose multi-horizon signal, volatility scaling,
turnover, executable costs, and state are exactly reproducible.

**Architecture:** Add `internal/strategy/trend`, an explicit OVR-303 adapter,
and additive migration 85. A content-addressed policy pins horizons and weights,
signal threshold, realized-volatility window and annualization, target
volatility, per-instrument and gross caps, rebalance turnover cap, executable
pricing, costs, and decimal scale. A scenario binds an ordered point-in-time ETF
universe and exact horizon-price, volatility, quote, membership, and manifest
evidence at each rebalance. The engine derives signals, scaled targets,
sell-before-buy whole-lot trades, cash, holdings, turnover, costs, equity, and
after-cost return. PostgreSQL reconstructs the normalized graph. This milestone
creates research evidence only.

**Migration:** `000085_etf_time_series_trend`

## Scope and non-goals

In scope:

- liquid ETF long-or-cash positions from weighted multi-horizon signs;
- inverse-volatility sizing to a policy target with per-instrument and gross
  exposure caps;
- point-in-time membership, horizon anchors, realized volatility, and quotes;
- deterministic whole-lot rebalance, explicit cash, turnover cap, spread, and
  cost treatment;
- OVR-303 scored/stress plans, append-only persistence, retained local
  qualification, and empty-only rollback.

Out of scope:

- futures, shorting, leverage, covariance optimization, volatility forecasts,
  tactical overrides, revised data before availability, midpoint fills, hidden
  liquidity, provider calls, promotion, allocation, scheduling, deployment, or
  activation.

## Locked contracts

1. Horizons are unique ascending trading-day counts. Positive policy weights
   sum exactly to one; each signal is the weighted sum of horizon return signs.
2. A member is eligible only when its membership, every horizon anchor,
   realized-volatility observation, and quote were available by the decision.
   Missing, stale, reordered, revised, or partial evidence fails closed.
3. A signal strictly above the threshold is long; otherwise its target is zero
   and capital remains cash. Callers cannot supply signals or targets.
4. Raw target weight is target annualized volatility divided by pinned realized
   volatility. Per-instrument and gross caps apply deterministically; unallocated
   exposure remains explicit cash. Weights are never normalized upward.
5. Desired turnover is one half of absolute target/current weight changes. A
   deterministic common scale enforces the per-rebalance cap and preserves
   remaining target drift.
6. Sells execute before buys. Sells use bid, buys use ask; quantities floor to
   the venue lot. Costs and cash are explicit, and actual lot-rounded turnover
   is reported.
7. Identical writers converge. Changed retries, gaps/forks, unknown references,
   forged signals/targets/trades/economics, partial graphs, negative cash, and
   over-cap exposure or turnover fail.
8. The OVR-303 adapter emits only engine-derived intents with exact ordered
   observation evidence and common simulation/capital/lifecycle enforcement.
9. Reports cannot select, promote, allocate, schedule, deploy, call providers,
   or grant execution authority.
10. Migration 85 is additive, lock-first, append-only, and empty-only reversible.

## Task 1: Policy, scenario, signal, and sizing engine

- [x] Add immutable policy/scenario canonical objects with exact restoration.
- [x] Validate ordered point-in-time ETF membership, horizon anchors,
  volatility, quote, lot-size, and manifest evidence.
- [x] Derive weighted multi-horizon signals and capped volatility-scaled targets.
- [x] Prove horizon/threshold/cap edges, stable input-order replay, missing/stale/
  revised evidence refusal, cash-only behavior, and deterministic replay.
- [x] Commit and push the signal/sizing slice after focused races.

## Task 2: Rebalance, costs, and OVR-303 adapter

- [ ] Derive sell-before-buy whole-lot trades, actual turnover, remaining drift,
  holdings, cash, costs, equity, and after-cost return.
- [ ] Bind an exact OVR-302 version and translate engine trades to ordered
  OVR-303 intents with exact manifest evidence and capital notional.
- [ ] Prove scored/stress replay, turnover and exposure cap edges,
  multi-rebalance convergence, common capital/simulation enforcement, and no
  runtime path.
- [ ] Commit and push the execution/adapter slice after focused races.

## Task 3: Migration 85 and append-only evidence

- [ ] Add policy, scenario, source/member/horizon, report, rebalance, signal,
  target, trade, and holding tables.
- [ ] Reconstruct canonical identities, parents, chronology, signals, scaling,
  continuity, turnover, costs, holdings, cash, and return.
- [ ] Reject mutation/deletion, gaps/forks, changed retry, forgery, partial
  evidence, unknown references, negative cash/weights, and cap violations.
- [ ] Keep provider/runtime/promotion/allocation/UI authority absent.
- [ ] Add empty-only rollback and bump `RequiredSchemaVersion` to 85 only after
  real PostgreSQL migration races pass.
- [ ] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [ ] Add a runbook for source/signal inspection, volatility/weight replay,
  turnover/cost reconciliation, failure/recovery, and rollback.
- [ ] Retain local all-long, mixed long/cash, all-cash, volatility-cap,
  turnover-cap, and multi-rebalance scenarios with exact IDs/hashes/counts.
- [ ] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `85 -> 84 -> 85`.
- [ ] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-85 health/API/rollback/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-405.

## Acceptance evidence to record

- [ ] Multi-horizon signals, volatility-scaled targets, exposure/turnover caps,
  whole-lot trades, costs, holdings, cash, and returns are exact and reproducible.
- [ ] Missing/stale/revised/partial/forged evidence and negative or over-cap
  portfolio states fail.
- [ ] The adapter remains deterministic inside OVR-303 boundaries and exposes
  cash and remaining drift rather than manufacturing allocation or fills.
- [ ] Reports have no promotion, allocation, scheduling, deployment, provider,
  or runtime authority.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.
