# Milestone 4 Defined-Risk Options Baseline Plan

**Goal:** Complete OVR-405 with a deterministic vertical-spread research
baseline whose multi-leg executable prices, package-versus-legged fill model,
orphan exposure and unwind, maximum loss, lifecycle payoff, costs, and capital
reservation are explicit and exactly reproducible.

**Architecture:** Add `internal/strategy/definedrisk`, an explicit OVR-303
adapter, and additive migration 86. A content-addressed policy pins supported
verticals, execution mode, deterministic leg order, quote/depth rules, orphan
unwind treatment, commissions, contract multiplier, position and capital caps,
expiry/assignment conventions, and decimal scale. A scenario binds an exact
underlying, two same-expiry option contracts, point-in-time entry observations,
optional leg-fill and orphan-unwind observations, and a terminal underlying
observation. The engine derives admissibility, whole-contract quantity, net
premium, maximum loss/reward, reserved capital, ordered fills, orphan exposure
and loss, expiration payoff, fees, cash flows, and after-cost return.
PostgreSQL reconstructs the normalized graph. This milestone creates research
evidence only.

**Migration:** `000086_defined_risk_options`

## Scope and non-goals

In scope:

- bull-call, bear-put, bull-put, and bear-call vertical spreads;
- two legs with one underlying, option type, expiry, multiplier, and 1:1 ratio;
- atomic package execution or deterministic sequential execution;
- explicit partial-fill/orphan state and immediate executable-side unwind;
- quote-side prices, displayed depth, whole contracts, commissions, maximum
  loss/reward, capital reservation, expiration payoff, and after-cost return;
- OVR-303 scored/stress plans, append-only persistence, retained local
  qualification, and empty-only rollback.

Out of scope:

- naked options, calendars, diagonals, ratios, butterflies, condors, straddles,
  strangles, dynamic hedging, volatility forecasts, midpoint fills, hidden
  liquidity, discretionary repair, exercise before expiry, dividends, pin-risk
  probability models, broker complex-order routing, provider calls, promotion,
  allocation, scheduling, deployment, or activation.

## Locked contracts

1. A candidate contains exactly two distinct contracts with the same nonempty
   underlying, option type, expiry, positive multiplier, and multiplier value.
   Strikes are strictly ordered and position intents must match one supported
   vertical. Ratios are exactly 1:1 and quantities are positive integers.
2. Every entry, fill, unwind, and terminal input is immutable point-in-time
   evidence available no later than its decision. Missing, stale, revised,
   reordered, crossed, nonpositive, or partial evidence fails closed.
3. Buys execute at ask and sells at bid. Displayed size caps whole contracts.
   Atomic mode fills both legs in one all-or-none package only when both legs
   have sufficient depth; otherwise neither leg fills.
4. Sequential mode uses the policy-pinned protective-leg-first order. If the
   second leg cannot fill, the first leg becomes an explicit orphan and is
   immediately unwound at the opposite executable side from a separately
   pinned observation. No fabricated package fill or discretionary retry is
   permitted.
5. Debit vertical maximum loss is debit plus entry fees. Credit vertical
   maximum loss is width less credit plus entry fees. Reservation is maximum
   loss times whole contracts plus the policy-pinned orphan-loss reserve and
   cannot exceed either per-position or scenario capital.
6. The orphan reserve is derived from the executable entry and unwind evidence,
   multiplier, whole contracts, and unwind fees. It is never inferred from a
   midpoint, theoretical value, or the completed-spread maximum loss.
7. Successful spreads remain open to the pinned terminal event. European-style
   cash settlement uses intrinsic value at expiry; exact-strike settlement is
   zero intrinsic value. American contracts, early exercise, assignment before
   expiry, dividends, and pin outcomes fail closed in version 1.
8. Net premium, maximum loss/reward, reservations, fill cash flows, orphan loss,
   expiration payoff, fees, ending cash, and after-cost return are engine-
   derived. Callers cannot supply economics or outcomes.
9. The OVR-303 adapter emits engine-derived ordered leg intents with exact
   observation evidence. It declares the engine-derived reservation for common
   capital assessment and preserves atomicity/orphan assumptions in immutable
   adapter evidence; it grants no broker package-order semantics.
10. Identical writers converge. Changed retries, gaps/forks, unknown references,
    forged structure/economics/fills/orphan/settlement, and partial graphs fail.
11. Reports cannot select, promote, allocate, schedule, deploy, call providers,
    place orders, or grant execution authority.
12. Migration 86 is additive, lock-first, append-only, and empty-only reversible.

## Task 1: Policy, scenario, structure, and economics engine

- [ ] Add immutable canonical policy/scenario objects with exact restoration.
- [ ] Validate all four vertical structures and reject every unsupported or
  ambiguous contract/ratio/style/expiry/multiplier combination.
- [ ] Derive executable net premium, width, whole-contract quantity, maximum
  loss/reward, reservation, fees, and capital admission.
- [ ] Prove debit/credit boundaries, quote/depth edges, capital caps, stable
  replay, and missing/stale/revised evidence refusal.
- [ ] Commit and push the engine slice after focused races.

## Task 2: Multi-leg lifecycle, orphan handling, and OVR-303 adapter

- [ ] Derive atomic success/refusal and sequential success/orphan-unwind paths.
- [ ] Derive terminal intrinsic payoff, ending cash, costs, and return for
  winning, losing, and exact-strike expiration.
- [ ] Bind an exact OVR-302 version and translate only engine-derived legs to an
  OVR-303 scored/stress plan with exact manifest and reservation evidence.
- [ ] Prove common simulation/capital enforcement and absence of runtime paths.
- [ ] Commit and push the lifecycle/adapter slice after focused races.

## Task 3: Migration 86 and append-only evidence

- [ ] Add policy, scenario, contract/observation, report, fill, orphan, and
  settlement tables with normalized quantities and money values.
- [ ] Reconstruct canonical identities, chronology, structure, pricing,
  reservations, fills, unwind, payoff, costs, cash, and return.
- [ ] Reject mutation/deletion, gaps/forks, changed retry, forgery, partial
  evidence, unknown references, and capital/risk violations.
- [ ] Add empty-only rollback and bump `RequiredSchemaVersion` to 86 only after
  real PostgreSQL migration races pass.
- [ ] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [ ] Add a runbook for structure/risk replay, leg/fill inspection, orphan
  reconciliation, settlement, failure/recovery, and rollback.
- [ ] Retain atomic debit/credit, sequential success, orphan unwind, losing,
  winning, and exact-strike scenarios with exact IDs/hashes/counts.
- [ ] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `86 -> 85 -> 86`.
- [ ] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-86 health/API/rollback/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-406.

## Acceptance evidence to record

- [ ] Every supported vertical's structure, price, risk, reservation, fills,
  orphan outcome, settlement, costs, and return are exact and reproducible.
- [ ] Atomic failure cannot leave a leg; sequential failure must expose and
  unwind the orphan from pinned executable evidence.
- [ ] Missing/stale/revised/partial/forged evidence and unsupported exercise or
  strategy behavior fail closed.
- [ ] Reports have no promotion, allocation, scheduling, deployment, provider,
  broker, or runtime authority.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.
