# Phase 3 Trade-Level and Portfolio Evaluation Plan

**Goal:** Complete OVR-304 with one immutable evaluation report bound to an
exact completed OVR-303 result. The report must calculate after-cost trade and
portfolio evidence from explicit ordered samples, state every frequency and
cash/benchmark assumption, and make it impossible to confuse bar-positive
frequency with closed-trade win rate or bar-return profit factor with
trade-level profit factor.

**Architecture:** Add an `internal/evaluation` bounded context and additive
migration 79. An evaluation request pins one exact OVR-303 result and supplies
ordered equity/benchmark/exposure/cost observations plus FIFO closed-trade
round trips reconstructed from retained execution, ledger, projection, and mark
evidence. The evaluator validates source identities and time order, derives a
canonical content-addressed report with explicit `portfolio`, `trade`,
`execution`, `cost`, `exposure`, `sample`, and `curve_diagnostics` sections,
and persists the report and normalized children atomically. Metrics requiring
unsupported evidence are represented by an explicit unavailable reason; they
are never silently zero. PostgreSQL reconstructs every canonical field and
ordered child. No report selects a best result or changes lifecycle state.

**Migration:** `000079_trade_portfolio_evaluations`

## Scope and non-goals

In scope:

- exact evaluation-policy identity with stated return frequency, annualization
  factor, risk-free/cash-return treatment, decimal scale, FIFO lot matching,
  and recovery definition;
- exact OVR-303 result/program/plan/experiment/account/manifest/quality/mode
  binding;
- ordered equity, benchmark, gross/net exposure, cumulative ownership-cost,
  and modeled/observed-slippage observations with point-in-time evidence IDs;
- FIFO closed-trade round trips with entry/exit fill IDs, quantities, prices,
  fees, other ownership costs, after-cost P&L, holding time, and side;
- after-cost total/annualized return, benchmark excess return, tracking error,
  information ratio, maximum drawdown, recovery time, Sharpe, Sortino, Calmar,
  turnover, total ownership cost, average/maximum exposure and concentration,
  fill ratio, and modeled-versus-observed slippage;
- trade-level count, win/loss/breakeven count, win rate, expectancy, gross
  profit/loss, profit factor, and holding-period statistics;
- explicit sample counts, interval/window coverage, metric availability, and
  separate curve descriptors including `bar_positive_return_rate`;
- deterministic calculation, exact reload, restart/concurrency convergence,
  scored/stress isolation, retained golden qualification, and documentation.

Out of scope:

- creating marks, projections, fills, trades, or benchmark observations;
- estimating missing prices, cash returns, costs, capacity, or slippage;
- walk-forward folds, purge/embargo, perturbation, bootstrap confidence
  intervals, return concentration, or multiple-testing correction (OVR-305);
- promotion, retirement, approval, best/current selection, deployment, or
  scheduling (OVR-306);
- passive benchmark implementation and candidate strategy programs
  (OVR-401 through OVR-405);
- capacity comparison across capital tiers (OVR-406);
- changing legacy `internal/backtest.Metrics`, legacy API payloads, existing
  backtest rows, or any live/paper execution path.

## Locked contracts

1. An evaluation accepts only a completed OVR-303 result reloaded from its
   normalized graph. Result ID, SHA-256, experiment, program, plan, account,
   manifest, quality result, mode, and policy versions are immutable parents.
2. Every portfolio observation pins UTC microsecond time, equity, benchmark
   value, cash return, gross/net exposure, largest-position weight, cumulative
   ownership cost, modeled slippage, observed slippage availability/value, and
   retained evidence ID/SHA-256. Times are strictly increasing and span the
   declared evaluation window without future evidence.
3. Return frequency, periods per year, risk-free/cash convention, return kind,
   decimal scale, FIFO lot method, and drawdown recovery definition are policy
   fields included in the report identity. Annualized metrics never infer a
   frequency from sample count alone.
4. Portfolio returns are after cumulative fees, spread, impact, financing, and
   other declared ownership costs already reflected in equity. Total ownership
   cost is also reported explicitly; missing cost evidence makes cost-dependent
   metrics unavailable.
5. Closed trades are exact FIFO round trips reconstructed from retained fills.
   A trade pins entry/exit fill IDs and times, side, instrument, quantity,
   prices, entry/exit fees, other costs, gross P&L, and after-cost P&L. Open lots
   are counted but never treated as closed wins or losses.
6. `trade.win_rate`, `trade.expectancy`, and `trade.profit_factor` exist only in
   the trade section and require closed-trade evidence. With no losses, profit
   factor uses an explicit `positive_infinity` state; with no closed trades it
   is unavailable, not zero.
7. Per-period positive-return frequency is named
   `curve_diagnostics.bar_positive_return_rate` and carries the description
   `descriptor_only_not_trade_win_rate`. Curve profit factor is not a primary
   or promotion metric and is not emitted by OVR-304.
8. Ratios use deterministic ordered decimal inputs and one locked rounding
   scale. Undefined denominators produce an explicit availability reason; NaN,
   implicit infinity, floating JSON sentinels, and silently clamped values are
   forbidden.
9. Scored and stress evaluations remain distinct by result, experiment,
   account namespace/evidence class, mode, report identity, and database parent
   constraints. Stress evaluation cannot satisfy scored evidence.
10. Identical writers and restart retries converge. Changed payload under an
    accepted identity conflicts. Parent, observation, trade, availability, and
    report rows are append-only; incomplete graphs fail at commit.
11. The repository lists evaluations only by explicit experiment or result and
    never selects latest/best/current. No migration 79 column or function can
    approve, promote, retire, schedule, or activate a strategy.
12. Migration 79 is additive, empty-only reversible, and has no legacy
    backfill, API/UI cutover, provider call, writer grant, or runtime trigger.

## Task 1: Evaluation policy, samples, and canonical report domain

- [ ] Add immutable policy identity and strict ordered portfolio-observation,
  closed-trade, open-lot, and execution-summary inputs.
- [ ] Add metric value states (`available`, `unavailable`,
  `positive_infinity`) with canonical decimal strings and bounded reasons.
- [ ] Add canonical report sections and content identity covering every parent,
  policy assumption, input sample, metric value, and availability statement.
- [ ] Prove canonical restoration, clone safety, semantic reorder divergence,
  tamper rejection, mode separation, and stable hashes.
- [ ] Commit and push the domain slice after focused races.

## Task 2: Deterministic trade and portfolio calculations

- [ ] Validate exact time/order/window/evidence/cost/exposure inputs and reject
  duplicate, future, missing, negative, noncanonical, or inconsistent samples.
- [ ] Compute after-cost portfolio, benchmark, drawdown/recovery, risk-adjusted,
  turnover/cost, exposure/concentration, fill/slippage, and sample metrics with
  locked frequency/rounding assumptions.
- [ ] Compute FIFO closed-trade expectancy/profit factor/win rate/holding
  evidence while retaining open-lot counts and exact source fill IDs.
- [ ] Emit bar-positive return rate only under descriptor-only curve
  diagnostics; add negative tests that prevent it from populating trade fields.
- [ ] Cover empty/short series, zero denominators, all-win/all-loss/breakeven,
  unrecovered drawdown, missing observed slippage, irregular time, and extreme
  but bounded decimals without NaN or silent zero substitution.
- [ ] Commit and push the calculator slice after focused races.

## Task 3: Migration 79 append-only evaluation evidence

- [ ] Add policy artifacts, evaluation reports, ordered observations, ordered
  closed trades/source fills, open-lot summaries, metric values, and explicit
  availability rows.
- [ ] Reconstruct canonical bytes, deterministic IDs/hashes, counts/order,
  parent result/plan/experiment/account/manifest/mode pins, policy assumptions,
  metric sections, and child completeness in deferred constraints.
- [ ] Reject mutation/deletion, child omission/reordering, forged metrics,
  mismatched result/mode/account, changed retries, and unsupported availability
  claims.
- [ ] Add no best/current pointer, lifecycle status, promotion field,
  scheduler, writer grant, legacy backfill, or runtime trigger.
- [ ] Add lock-first empty-only rollback and bump `RequiredSchemaVersion` to 79
  only after isolated real-PostgreSQL tests pass.
- [ ] Commit and push the migration slice after focused database races.

## Task 4: PostgreSQL repository, service, and recovery

- [ ] Add atomic policy/report registration, exact normalized reload, and
  explicit result/experiment listing without selection.
- [ ] Revalidate all cross-aggregate pins and normalized ordered children rather
  than trusting canonical JSON alone.
- [ ] Prove identical/eight-writer convergence, changed retry conflict,
  interruption, injected rollback at every child stage, and restart reload.
- [ ] Prove a fresh-database calculation reproduces every input, metric,
  availability state, report ID, and SHA-256.
- [ ] Commit and push the repository/recovery slice after database races.

## Task 5: Golden qualification and legacy separation

- [ ] Retain scored and stress golden reports with exact within-mode replay and
  physical/semantic mode isolation.
- [ ] Retain winning, losing, breakeven, open-lot, ownership-cost, drawdown/
  recovery, turnover, exposure, fill, modeled/observed-slippage, and unavailable
  metric evidence.
- [ ] Prove the same equity curve can have a high bar-positive rate and a
  different trade win rate; labels/sections remain unambiguous in canonical,
  relational, and serialized output.
- [ ] Prove legacy `backtest.Metrics.WinRate` remains unchanged but is never
  imported or relabeled as OVR-304 trade evidence.
- [ ] Commit and push the golden slice after focused races.

## Task 6: Documentation, qualification, review, and synchronization

- [ ] Add a runbook covering evidence preparation, policy assumptions, report
  inspection, metric availability, trade/curve distinction, replay comparison,
  preservation, no-promotion response, and rollback.
- [ ] Apply migrations `1 -> 79` to fresh loopback databases, retain a complete
  report, prove clean-database reproduction, nonempty rollback refusal, and
  separate empty `79 -> 78 -> 79`.
- [ ] Run focused/database races, backend build/race/vet/lint/format/
  vulnerability gates, pinned Node 22 frozen install/audit/tests/lint/build,
  `git diff --check`, and an isolated kill-switched schema-79 health smoke.
- [ ] Complete final diff review, commit/push verified slices, fetch, and prove
  `0 0` divergence before OVR-305.

## Acceptance evidence to record after implementation

- [ ] Every evaluation is an exact immutable child of one completed OVR-303
  result and replays identically from retained evidence.
- [ ] Primary after-cost trade/portfolio metrics state assumptions, sample size,
  and availability without invented observations or silent zero substitution.
- [ ] Trade win rate/expectancy/profit factor cannot be populated from bar
  returns; bar-positive frequency remains visibly descriptor-only.
- [ ] Scored/stress evidence remains isolated and no report grants promotion or
  deployment authority.
- [ ] Migration 79 is additive, append-only, empty-only reversible, and leaves
  legacy backtests and runtimes untouched.
- [ ] Local qualification is `VERIFIED_LOCAL`; real candidate evidence,
  licensed data, independent statistical review, promotion, deployment, and
  production cutover remain `BLOCKED_EXTERNAL`.
