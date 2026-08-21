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

- [x] Derive sell-before-buy whole-lot trades, actual turnover, remaining drift,
  holdings, cash, costs, equity, and after-cost return.
- [x] Bind an exact OVR-302 version and translate engine trades to ordered
  OVR-303 intents with exact manifest evidence and capital notional.
- [x] Prove scored/stress replay, turnover and exposure cap edges,
  multi-rebalance convergence, common capital/simulation enforcement, and no
  runtime path.
- [x] Commit and push the execution/adapter slice after focused races.

## Task 3: Migration 85 and append-only evidence

- [x] Add policy, scenario, source/member/horizon, report, rebalance, signal,
  target, trade, and holding tables.
- [x] Reconstruct canonical identities, parents, chronology, signals, scaling,
  continuity, turnover, costs, holdings, cash, and return.
- [x] Reject mutation/deletion, gaps/forks, changed retry, forgery, partial
  evidence, unknown references, negative cash/weights, and cap violations.
- [x] Keep provider/runtime/promotion/allocation/UI authority absent.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 85 only after
  real PostgreSQL migration races pass.
- [x] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [x] Add a runbook for source/signal inspection, volatility/weight replay,
  turnover/cost reconciliation, failure/recovery, and rollback.
- [x] Retain local all-long, mixed long/cash, all-cash, volatility-cap,
  turnover-cap, and multi-rebalance scenarios with exact IDs/hashes/counts.
- [x] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `85 -> 84 -> 85`.
- [x] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-85 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-405.

## Acceptance evidence to record

- [x] Multi-horizon signals, volatility-scaled targets, exposure/turnover caps,
  whole-lot trades, costs, holdings, cash, and returns are exact and reproducible.
- [x] Missing/stale/revised/partial/forged evidence and negative or over-cap
  portfolio states fail.
- [x] The adapter remains deterministic inside OVR-303 boundaries and exposes
  cash and remaining drift rather than manufacturing allocation or fills.
- [x] Reports have no promotion, allocation, scheduling, deployment, provider,
  or runtime authority.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.

## Qualification record

- Retained database: `augr_ovr404_qual_20260820`, clean schema 85.
- Retained inventory: 1 policy, 5 scenarios, 11 source rebalances, 11 universe
  members, 22 horizon anchors, 5 reports, 11 normalized rebalances, 11 signals,
  9 whole-lot trades, and 8 holdings.
- Retained scenario/report IDs: `all_cash`
  `3875a288-31a5-b168-ee49-fc16b2b0a98b` / `75c82239-5937-1233-de8f-cbc6f805048e`;
  `all_long` `2ade2f1f-ea6a-0fe8-9f82-651eaac96497` /
  `b8c96ccf-2086-5843-5bc7-84faf9c8ac71`; `mixed_long_cash`
  `21329517-70e3-dc43-1da4-e3096ffb531c` / `ff8ad549-acb8-9ab0-9594-caa525d4ef72`;
  `turnover_multi` `386d5571-8d31-8bc7-1f94-cc4d16d50b8d` /
  `51d3d988-a3d7-4f72-918a-36fa5d7562af`; `volatility_cap`
  `8a29bd17-997b-3e0c-9cd6-b64b04d5d92c` / `3735f257-0216-2c9d-fe0d-a4dba1140a91`.
- Backend: 4,555 race-enabled tests in 113 packages; build, vet, repository-wide
  lint, `gofumpt`, and `govulncheck` passed.
- Frontend: Node 22.23.2 frozen/ignored-script install, high-severity audit, 162
  tests, lint, and production build passed.
- Isolated production verifier built commit `e48f9fb`, migrated fresh 1 -> 85,
  passed health and authenticated read-only API checks, rolled back 85 -> 60,
  verified backup/restore, reapplied 61 -> 85, and returned healthy. The
  temporary empty external `monitoring` network was removed after the pass.
