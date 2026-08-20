# Milestone 4 Momentum / Quality Baseline Plan

**Goal:** Complete OVR-403 with a deterministic cross-sectional momentum
baseline whose point-in-time universe, quality and low-volatility controls,
turnover, costs, and regime evidence are exact and reproducible.

**Architecture:** Add `internal/strategy/momentum`, an explicit OVR-303 program
adapter, and additive migration 84. A content-addressed policy pins momentum
lookback/skip windows, minimum history, quality thresholds, volatility window,
ranking/tie-break rules, portfolio size, rebalance frequency, target weighting,
turnover cap, executable pricing convention, costs, and regime definitions. An
ordered scenario binds dated universe membership and exact available-at price,
fundamental, volatility, benchmark, and quote evidence. The engine derives
eligibility, ranks, targets, capped trades, holdings, costs, returns, turnover,
and regime slices. PostgreSQL reconstructs the normalized graph. This milestone
creates research evidence only.

**Migration:** `000084_momentum_quality_baseline`

## Scope and non-goals

In scope:

- cross-sectional 12-1 style momentum from policy-pinned total-return prices;
- point-in-time universe membership with effective and available timestamps;
- minimum ROIC, maximum leverage, positive cash-flow, and low-volatility
  controls using only evidence available at the decision;
- deterministic ranking, stable ties, equal-weight targets, explicit cash,
  bounded turnover, executable prices, fees/slippage, and after-cost return;
- explicit bull, bear, and sideways regime labels derived from pinned benchmark
  evidence, plus per-regime results and transition tests;
- OVR-303 scored/stress plans, append-only persistence, retained local
  qualification, and empty-only rollback.

Out of scope:

- survivorship-biased present-day universes, revised fundamentals used before
  availability, delisted-return invention, midpoint fills, hidden liquidity,
  optimizer/ML weights, shorting, leverage, taxes, live data/provider calls,
  promotion, allocation, scheduling, deployment, or activation.

## Locked contracts

1. Each rebalance binds an exact universe membership set effective and
   available no later than the decision. Missing membership or constituent
   evidence fails the rebalance; absence never means zero.
2. Momentum is total-return change from the lookback anchor to the skip anchor.
   Both anchors and every corporate-action adjustment are source evidence.
3. Quality and volatility values are point-in-time inputs; later revisions or
   observations fail no-lookahead validation.
4. Eligibility applies history, quality, and volatility controls before rank.
   Rank is momentum descending, volatility ascending, then instrument UUID.
5. Targets are equal weight across at most the policy portfolio size. Cash is
   explicit. Long-only weights and cash cannot be negative and sum to one.
6. Desired turnover is one half of absolute weight changes. When it exceeds the
   per-rebalance cap, all trades scale by one deterministic ratio and remaining
   drift stays explicit.
7. Sells execute before buys. Sells use executable bid, buys use executable ask;
   costs and residual cash are explicit. No caller supplies holdings, targets,
   ranks, turnover, cost, return, or regime results.
8. Regimes derive only from pinned benchmark trend and volatility thresholds.
   Every observation belongs to exactly one declared regime and transition
   boundaries are retained.
9. Identical writers converge. Changed retries, gaps, forks, reordered/partial
   evidence, unknown instruments/contracts, forged ranks/trades/economics, or
   incomplete normalized graphs fail.
10. The OVR-303 adapter emits only engine-derived rebalance intents and remains
    inside common simulation, capital, ledger, and execution lifecycle.
11. AI/UI/operators may display or recommend evidence but cannot edit inputs,
    select ranks, approve results, promote, allocate, schedule, or execute.
12. Migration 84 is additive, lock-first, append-only, and empty-only reversible.

## Task 1: Policy, point-in-time universe, and ranking engine

- [x] Add immutable policy/scenario canonical objects with exact restoration.
- [x] Add point-in-time universe, price-anchor, fundamental, volatility,
  benchmark, and quote evidence validation.
- [x] Derive eligibility, momentum, low-volatility control, deterministic ranks,
  equal-weight targets, and explicit cash.
- [x] Prove threshold edges, stable ties/input-order invariance, missing/stale/
  revised evidence refusal, delisting handling, and deterministic replay.
- [x] Commit and push the ranking slice after focused races.

## Task 2: Rebalance, turnover, costs, regimes, and OVR-303 adapter

- [x] Derive sell-before-buy trades, cap scaling, remaining target drift,
  holdings, cash, executable-price costs, turnover, and after-cost return.
- [x] Derive bull/bear/sideways regimes and per-regime/transition metrics.
- [x] Bind an exact OVR-302 version and translate engine trades to ordered
  OVR-303 intents with exact manifest evidence and capital notional.
- [x] Prove scored/stress replay, cap edges, multi-rebalance convergence, regime
  transitions, common capital/simulation enforcement, and no runtime path.
- [x] Commit and push the execution/adapter slice after focused races.

## Task 3: Migration 84 and append-only evidence

- [ ] Add policy, scenario, universe/source, rebalance, rank, target, trade,
  holding, regime, and report tables.
- [ ] Independently reconstruct canonical bytes/IDs/hashes, exact parents,
  chronology, eligibility/rank, turnover scaling, state continuity, costs,
  returns, and regime aggregates.
- [ ] Reject mutation/deletion, gaps/forks, changed retry, forgery, partial
  universe/evidence, unknown references, negative cash/weights, and over-cap
  turnover.
- [ ] Add no provider writer, scheduler/runtime trigger, promotion/allocation
  mutation, execution grant, UI authority, or legacy backfill.
- [ ] Add empty-only rollback and bump `RequiredSchemaVersion` to 84 only after
  real PostgreSQL migration races pass.
- [ ] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [ ] Add a runbook for universe/source inspection, ranking replay, turnover and
  cost reconciliation, regime review, failure/recovery, and rollback.
- [ ] Retain local bull, bear, sideways, regime-transition, cap-hit, and
  multi-rebalance scenarios with exact IDs/hashes and row counts.
- [ ] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `84 -> 83 -> 84`.
- [ ] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-84 health/API/rollback/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-404.

## Acceptance evidence to record

- [ ] Universe membership, quality/volatility eligibility, momentum ranks,
  targets, turnover scaling, costs, holdings, cash, returns, and regimes are
  exact and reproducible from immutable point-in-time evidence.
- [ ] Missing/stale/revised/partial/forged evidence and over-cap or negative
  portfolio states fail.
- [ ] The adapter remains deterministic inside OVR-303 boundaries and reports
  all remaining target drift rather than manufacturing fills or convergence.
- [ ] Reports cannot select/promote/allocate/schedule/deploy or call providers.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.
