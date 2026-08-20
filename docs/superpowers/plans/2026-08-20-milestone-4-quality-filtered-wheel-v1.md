# Milestone 4 Quality-Filtered Wheel V1 Plan

**Goal:** Complete OVR-402 with a deterministic, point-in-time wheel strategy
whose contract selection, collateral, assignment, dividends, capped upside,
costs, and after-cost total return are explicit and reproducible.

**Architecture:** Add an `internal/strategy/wheel` bounded context, an explicit
OVR-303 program adapter, and additive migration 83. A content-addressed policy
defines the quality screen, liquidity floor, put/call delta and DTE bands,
contract multiplier, sizing/collateral rules, exercise/assignment handling,
dividend treatment, pricing side, and decimal scale. An ordered scenario binds
exact underlying, option-chain, fundamentals, corporate-action, quote, and
assignment evidence. The engine derives every state transition and economic
effect and emits an immutable lifecycle report. The adapter may translate only
engine-approved opening/closing orders into OVR-303 plan steps; assignment,
expiry, dividend, and marking remain explicit modeled events rather than fake
broker orders. PostgreSQL independently validates the complete graph and
reconstructs totals. This milestone creates research evidence only.

**Migration:** `000083_quality_filtered_wheel_v1`

## Scope and non-goals

In scope:

- deterministic quality eligibility from point-in-time fundamentals;
- deterministic contract selection within configured delta/DTE/liquidity bands;
- cash-secured put and covered-call lifecycle with exact collateral/coverage;
- put and call expiry/assignment, including evidence-backed early assignment;
- cash dividends, premiums, fees, option liability marks, shares, cash,
  collateral, capped upside, and after-cost total return;
- OVR-303-compatible plan decisions for actual option/equity order intents;
- append-only persistence, exact reload, recovery, retained qualification, and
  empty-only rollback.

Out of scope:

- naked options, margin borrowing, portfolio margin, tax-lot optimization, or
  dividend forecasts not present in point-in-time evidence;
- manufacturing Greeks, fills, assignment notices, dividends, or prices;
- early-exercise prediction as a substitute for a sourced assignment event;
- dynamic optimization, ML/AI contract selection, discretionary overrides, or
  changing a strategy after seeing results;
- promotion, allocation, scheduling, live activation, provider calls, or
  production deployment.

## Locked contracts

1. Wheel V1 starts from cash and supports one underlying sleeve at a time. It
   may hold cash, one cash-secured short put, assigned shares, or covered short
   calls; it may never create naked short option quantity.
2. Quality eligibility is derived from exact available-at fundamentals under
   policy thresholds. Missing, stale, revised-after-decision, or failed quality
   evidence yields no opening order.
3. Contract candidates must bind canonical option terms and point-in-time quote/
   Greek evidence. Selection sorts by target-delta distance, then DTE, strike,
   and instrument ID; input ordering cannot change the result.
4. Short puts reserve `strike * deliverable quantity * contracts` cash. Premium
   does not reduce required collateral. Available cash cannot go negative.
5. Covered calls require deliverable shares for every contract. Shares already
   covering another call cannot be reused.
6. Short-option opening fills use executable bid and explicit fees/slippage;
   buy-to-close uses executable ask. Missing/crossed/stale/zero quotes fail.
7. Put assignment purchases the exact deliverable at strike and releases the
   matching collateral. Call assignment sells the exact covered deliverable at
   strike. Assignment cannot be caller-inferred from spot alone.
8. Expiry without an assignment notice deterministically exercises/assigns only
   under the policy's pinned settlement convention and exact expiry mark.
9. Sourced cash dividends credit only shares held through the policy-pinned
   entitlement instant. Ex-date and payment evidence are retained separately.
10. Capped upside is reported when covered shares are called away below the
    contemporaneous underlying mark; it is descriptive and never added to P&L.
11. Net liquidation equals cash plus marked shares minus short-option close
    liability. Collateral is restricted cash, not a second deduction. Total
    return is terminal net liquidation over initial capital after every cost.
12. Every transition reloads the exact prior state and ordered evidence. A
    caller supplies no next state, P&L, return, quality verdict, or selection.
13. Identical writers converge; changed retry, fork, omission/reordering,
    canonical/normalized divergence, forged effect, or incomplete graph fails.
14. The OVR-303 adapter emits only engine-derived deterministic plan steps and
    cannot activate a deployment or bypass simulation/capital/risk boundaries.
15. AI/UI/operators may recommend or display evidence but cannot edit inputs,
    approve a result, select a winner, or trigger execution.
16. Migration 83 is additive, lock-first, append-only, and empty-only reversible.

## Task 1: Policy, selection, and lifecycle engine

- [x] Add immutable Wheel V1 policy, canonical restoration, and exact decimal/
  time/evidence validation.
- [x] Add quality-screen and deterministic put/call candidate selection.
- [x] Add cash, collateral, premiums, costs, shares, marks, dividends, expiry,
  assignment, call-away, capped-upside, and total-return transitions.
- [x] Prove input-order invariance, threshold edges, stale/missing evidence,
  naked-risk refusal, assignment/dividend entitlement, cap, costs, and replay.
- [x] Commit and push the engine slice after focused races.

## Task 2: OVR-303 adapter and strategy-version qualification

- [x] Add a content-addressed adapter identity bound to an exact OVR-302 Wheel
  V1 version and immutable typed scenario evidence.
- [x] Translate engine open/close decisions to exact OVR-303 plan steps while
  preserving non-order lifecycle events in the wheel report.
- [x] Reject program-input evidence mismatch, unrecognized instruments/venue
  contracts, reordered/partial evidence, invalid capital state, and mode drift.
- [x] Prove scored/stress deterministic plans, plan replay equality, capital and
  common simulation enforcement, and no direct provider/runtime path.
- [x] Commit and push the adapter slice after focused races.

## Task 3: Migration 83 and append-only evidence

- [x] Add policy, scenario, source observation, transition, economic effect,
  selected-contract, and lifecycle-report tables.
- [x] Independently reconstruct canonical bytes, IDs/hashes, exact parents,
  selection, state continuity, collateral/coverage, effects, marks, and totals.
- [x] Reject mutation/deletion, forks, gaps, changed retries, forged economics,
  naked states, missing sources, and incomplete normalized graphs.
- [x] Add no scheduler/runtime trigger, promotion/allocation mutation, provider
  writer, execution grant, UI authority, or legacy backfill.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 83 only after
  real PostgreSQL migration races pass.
- [ ] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [x] Add a runbook for policy review, source inspection, replay, accounting
  reconciliation, assignment/dividend response, failure, and rollback.
- [x] Retain local put-expiry, put-assignment, dividend, covered-call expiry,
  and call-away scenarios with exact IDs/hashes and row counts.
- [x] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `83 -> 82 -> 83`.
- [ ] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-83 health/API/rollback/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-403.

## Acceptance evidence to record

- [ ] Quality, delta/DTE/liquidity selection, collateral, share coverage,
  assignment, dividends, capped upside, costs, marks, and total return are exact
  and reproducible from immutable evidence.
- [ ] Missing/stale/partial/forged evidence and naked short-option states fail.
- [ ] The adapter is deterministic and remains inside OVR-303 simulation,
  capital, ledger, and execution-lifecycle boundaries.
- [ ] Reports are append-only and cannot select/promote/allocate/schedule/deploy.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.

## Qualification record (2026-08-20)

Retained database: `augr_ovr402_qual_20260820`, schema 83. Counts are policy /
scenario / source / report / transition / effect / selected contract =
`1 / 5 / 22 / 5 / 22 / 32 / 14`.

| Lifecycle | Scenario ID / SHA-256 | Report ID / SHA-256 |
|---|---|---|
| put expiry | `72c07d89-6882-811a-76fe-114a3e827ba9` / `c0e9202e25ecd0d8fc1f2d7633c49de03036be7d4f03c3bc307fde58924f2da7` | `2e9be806-9c0d-4af9-ea93-c6f17cef3d72` / `6b504da5a773e739df4c3f4f4c43cc06fd9b13b62c9387cc2190e77256f89018` |
| put assignment | `85f38f38-ac60-891e-0cfe-e97e7cbd8f91` / `47372fde752dc0fe5f25f9cf38f90336fcba713c346fc88fa7695a975e76dc06` | `78fdde7d-0708-9035-20a7-44f1806476b1` / `d0169140486c271905c45e8c2fdbba518bdbcde2b620c6dce97a819e0c170772` |
| dividend | `69d5e658-f8df-a1ee-fb70-b7ee41923ec8` / `1c0f9edf68aa8dba4db3b69296c6deb238fe4cd1c242d1074982480ac237e351` | `0089aa46-d341-a90a-425b-47672ed9987f` / `59f158322b69fb9e93abcefac988e05b500ff472ca00b2b072dcb84af59a527f` |
| covered-call expiry | `486efe92-0a67-b9f3-ecaa-42db7add8a00` / `487101c31fe332ee17c8e3b10aabd51bfd3d6d85db05398ca3b61e211f93daa4` | `9a550c88-d94e-3e2d-7a30-df227029cbc7` / `db76cf529929d3137a97fe46592a47af2376a35673bb8c12a1dd7a3a0131ff4a` |
| call-away | `76584aef-6dcb-347f-1ba9-86e448f89e91` / `c444c553c65c0a293b8a3c830859ff2d96d1109434395fd99de782228c34e59b` | `6cbab571-9dcd-26cf-4798-6a8a9ce694b2` / `a468e8e465c798f2e9d3ee4ce4ed706e4c966979e4a226c153dd757f8b7eea81` |

Focused races cover deterministic domain replay, scored/stress OVR303 runner
execution, common capital rejection, PostgreSQL eight-writer convergence,
restart, all write-stage interruption, append-only enforcement, normalized
forgery reload refusal, nonempty rollback refusal, and empty `83 -> 82 -> 83`.
