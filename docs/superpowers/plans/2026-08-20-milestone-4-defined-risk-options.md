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
   observation evidence. It exposes the exact spread reservation and uses the
   greater conservative leg notional where the common capital service requires
   it; immutable adapter evidence preserves both values and all atomicity/orphan
   assumptions. It grants no broker package-order semantics.
10. Identical writers converge. Changed retries, gaps/forks, unknown references,
    forged structure/economics/fills/orphan/settlement, and partial graphs fail.
11. Reports cannot select, promote, allocate, schedule, deploy, call providers,
    place orders, or grant execution authority.
12. Migration 86 is additive, lock-first, append-only, and empty-only reversible.

## Task 1: Policy, scenario, structure, and economics engine

- [x] Add immutable canonical policy/scenario objects with exact restoration.
- [x] Validate all four vertical structures and reject every unsupported or
  ambiguous contract/ratio/style/expiry/multiplier combination.
- [x] Derive executable net premium, width, whole-contract quantity, maximum
  loss/reward, reservation, fees, and capital admission.
- [x] Prove debit/credit boundaries, quote/depth edges, capital caps, stable
  replay, and missing/stale/revised evidence refusal.
- [x] Commit and push the engine slice after focused races.

## Task 2: Multi-leg lifecycle, orphan handling, and OVR-303 adapter

- [x] Derive atomic success/refusal and sequential success/orphan-unwind paths.
- [x] Derive terminal intrinsic payoff, ending cash, costs, and return for
  winning, losing, and exact-strike expiration.
- [x] Bind an exact OVR-302 version and translate only engine-derived legs to an
  OVR-303 scored/stress plan with exact manifest and reservation evidence.
- [x] Prove common simulation/capital enforcement and absence of runtime paths.
- [x] Commit and push the lifecycle/adapter slice after focused races.

## Task 3: Migration 86 and append-only evidence

- [x] Add policy, scenario, contract/observation, report, fill, orphan, and
  settlement tables with normalized quantities and money values.
- [x] Reconstruct canonical identities, chronology, structure, pricing,
  reservations, fills, unwind, payoff, costs, cash, and return.
- [x] Reject mutation/deletion, gaps/forks, changed retry, forgery, partial
  evidence, unknown references, and capital/risk violations.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 86 only after
  real PostgreSQL migration races pass.
- [x] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [x] Add a runbook for structure/risk replay, leg/fill inspection, orphan
  reconciliation, settlement, failure/recovery, and rollback.
- [x] Retain atomic debit/credit, sequential success, orphan unwind, losing,
  winning, and exact-strike scenarios with exact IDs/hashes/counts.
- [x] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `86 -> 85 -> 86`.
- [x] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-86 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-406.

## Acceptance evidence to record

- [x] Every supported vertical's structure, price, risk, reservation, fills,
  orphan outcome, settlement, costs, and return are exact and reproducible.
- [x] Atomic failure cannot leave a leg; sequential failure must expose and
  unwind the orphan from pinned executable evidence.
- [x] Missing/stale/revised/partial/forged evidence and unsupported exercise or
  strategy behavior fail closed.
- [x] Reports have no promotion, allocation, scheduling, deployment, provider,
  broker, or runtime authority.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.

## Qualification record

- Retained database: `augr_ovr405_qual_20260820`, clean schema 86.
- Retained inventory: 2 policies, 7 scenarios, 14 legs, 16 observations, 7
  reports, and 12 fills.
- Retained scenario/report IDs: `atomic_bear_call_exact_strike`
  `6bb751df-3ec2-4eb7-a472-79e22d5d9efd` /
  `dcb7ec1e-a56e-7412-0882-cc10352089cd`; `atomic_bear_put_winner`
  `cc4c6713-6e7a-1e83-6627-60bef8f1e459` /
  `f8c7eccb-e08d-01a0-edfb-b162d9e56182`; `atomic_bull_call_winner`
  `6a0520c7-9471-a52d-72d6-0f4164419d8c` /
  `b63692d4-1b38-d70f-82c4-e7604449cce3`; `atomic_bull_put_loser`
  `1f275455-5bab-2c48-971b-ca1a3af47586` /
  `52921d56-86f3-5a30-85cd-3ef04f181a2e`; `atomic_depth_rejected`
  `ad12a616-67e1-d58b-4bf3-78292b827fe1` /
  `a7c82800-4c14-031f-5a6a-47f650e8e342`; `sequential_orphan_unwind`
  `3148df76-d08d-bdcd-ef59-3d69e6a32479` /
  `a9227b97-9756-33f9-e965-cdaabe572102`; `sequential_success`
  `8cd8f663-5f7d-ac59-4a7d-ea86545ea31b` /
  `645941d5-6959-4a2e-1b4c-a3c907439d5c`.
- Backend: 4,568 race-enabled tests in 115 packages; build, vet, repository-wide
  lint, `gofumpt`, and symbol-level vulnerability checks passed. The complete
  PostgreSQL repository race suite passed in 331 seconds. A combined
  all-package DB run exposed six pre-existing migration-package shared-schema
  isolation failures; the same legacy tests also conflict serially on a fresh
  database. OVR405's focused migration/repository races and fresh migration
  chain passed.
- Frontend: pinned Node 22.23.2 frozen/ignored-script install, high-severity
  audit, 162 tests, lint, and production build passed with no known advisory.
- Isolated production verifier built commit `9c544a0`, migrated fresh 1 -> 86,
  passed health and authenticated read-only API checks, rolled back 86 -> 60,
  verified backup/restore, reapplied 61 -> 86, and returned healthy. The
  temporary external `monitoring` network was removed after the pass.
- This is **VERIFIED_LOCAL** only. Licensed real option data, broker complex-
  order semantics, independent review, shared migration, promotion, runtime
  adoption, deployment, and production activation remain **BLOCKED_EXTERNAL**.
