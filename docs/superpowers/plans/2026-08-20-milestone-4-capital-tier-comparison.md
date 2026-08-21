# Milestone 4 Capital-Tier Candidate Comparison Plan

**Goal:** Complete OVR-406 with one deterministic comparison of the passive
control and OVR-402 through OVR-405 candidate families at every reviewed finite
capital tier, reporting executable capacity and minimum viable capital without
ranking, promoting, allocating, or activating a family.

**Architecture:** Add `internal/capacity` and additive migration 87. Each
family-specific adapter consumes one exact completed OVR-304 evaluation plus
its exact OVR-401 through OVR-405 source report and derives a closed capacity
contract: minimum whole trading unit, capital reserved per unit, maximum units
from pinned executable depth/policy caps, after-cost return, and explicit
limiting reason. The common engine evaluates the identical contract at the six
OVR-206 tiers (`500`, `5000`, `25000`, `100000`, `1000000`, `5000000`) under
the reviewed finite profile, retains rejection as evidence, derives first
viable tier and capacity saturation, and creates a comparison with no winner.
PostgreSQL reconstructs the normalized family/tier graph. This milestone is
research evidence only.

**Migration:** `000087_capital_tier_candidate_comparison`

## Scope and non-goals

In scope:

- passive control, quality-filtered wheel, momentum/quality, ETF trend, and
  defined-risk options as five closed family kinds;
- exact OVR-304 evaluation and family-source report identities and hashes;
- six reviewed finite OVR-206 tiers in fixed ascending order;
- whole-unit minimum capital, source/policy depth cap, admitted units,
  executable capital, unused capital, saturation, and first viable tier;
- exact after-cost return carried from the completed source/evaluation pair;
- scored/stress input isolation, append-only persistence, retained local
  qualification, and empty-only rollback.

Out of scope:

- re-running a strategy with invented data, interpolation/extrapolation of
  return, cross-family netting, portfolio optimization, leverage optimization,
  stress/unlimited results in the scored table, ranking, best/current pointers,
  statistical approval, promotion, allocation, scheduling, deployment, or
  activation.

## Locked contracts

1. A comparison contains exactly the five family kinds once each. Every family
   binds one completed scored OVR-304 evaluation and one exact completed
   family-source report. IDs, SHA-256 values, modes, windows, and initial
   capital must agree with the adapter's derivation contract.
2. Family adapters, not callers, derive minimum unit capital, maximum units,
   capacity limit, and after-cost return from canonical source bytes. Missing,
   revised, partial, unsupported, rejected-source, or mismatched evaluation
   evidence fails closed.
3. Passive capacity uses the benchmark's pinned whole-lot executable notional;
   wheel capacity uses cash collateral per whole contract and candidate depth;
   momentum and trend use whole lots and pinned quote depth/policy exposure;
   defined-risk capacity uses engine-derived spread reservation and common
   two-leg depth. No midpoint, fractional unit, hidden liquidity, or generic
   option notional substitutes for those contracts.
4. The tier vocabulary is exactly the ordered OVR-206 finite set. Stress/
   unlimited is separate diagnostic evidence and cannot appear as a scored
   tier or determine minimum viable capital.
5. A tier is viable only when it funds at least one complete unit within both
   family and executable-depth caps. Units are floored, never rounded up.
   Rejection is a valid tier outcome with an explicit reason.
6. Executable capital is admitted units times capital per unit. Unused capital
   is tier capital minus executable capital. Saturation means the source's
   maximum executable units, not policy capital, is the binding limit.
7. Minimum viable capital is the first reviewed tier with a viable outcome. If
   none is viable it is explicitly unavailable; no off-grid threshold is
   manufactured as a reviewed tier.
8. After-cost return is source evidence and never multiplied by capital or
   units. Capacity comparison does not claim scale-invariant P&L, profitability,
   or statistical approval.
9. Identical writers converge. Changed retries, gaps/forks, unknown parents,
   forged adapter derivations, family duplication, tier reordering, arithmetic
   drift, and partial graphs fail.
10. The result contains no rank, score, winner, recommendation, allocation,
    lifecycle state, scheduler field, provider call, or execution authority.
11. Migration 87 is additive, lock-first, append-only, and empty-only
    reversible.

## Task 1: Family evidence adapters

- [x] Add immutable family capacity contracts and exact restoration.
- [x] Implement passive, wheel, momentum, trend, and defined-risk adapters from
  canonical completed evaluation/source reports.
- [x] Prove unit capital, depth/policy caps, after-cost return, source identity,
  mode/window, and rejection edges without caller-authored economics.
- [x] Commit and push the adapter slice after focused races.

## Task 2: Six-tier comparison engine

- [x] Evaluate every family at all six finite tiers in stable order.
- [x] Derive whole units, executable/unused capital, saturation, explicit
  rejection, and first viable tier.
- [x] Prove boundary equality, below-unit, source-cap saturation, no-viable,
  input-order independence, deterministic replay, and no ranking fields.
- [x] Commit and push the comparison engine after focused races.

## Task 3: Migration 87 and append-only evidence

- [x] Add comparison, family, and tier-outcome tables with exact normalized
  identities, quantities, money, reasons, and source references.
- [x] Reconstruct the five-family/six-tier graph and all arithmetic.
- [x] Reject mutation/deletion, gaps/forks, changed retry, duplicate/missing
  family/tier, normalized forgery, and partial writes.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 87 only after
  real PostgreSQL migration races pass.
- [x] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [x] Add a runbook for source replay, tier arithmetic, capacity saturation,
  failure/recovery, inventory, and rollback.
- [x] Retain one complete five-family by six-tier comparison with exact IDs,
  hashes, counts, viable thresholds, and saturation evidence.
- [x] Prove eight-writer convergence, restart, every-stage rollback, normalized
  forgery rejection, nonempty rollback refusal, and empty `87 -> 86 -> 87`.
- [x] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-87 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  Milestone 5.

## Acceptance evidence to record

- [x] All five families have exactly six finite tier outcomes and an explicit
  minimum viable reviewed tier or unavailable state.
- [x] Capacity is whole-unit, source-capped, cost-aware evidence rather than a
  scaled-return or profitability claim.
- [x] Missing/revised/partial/forged source and tier evidence fails closed.
- [x] The comparison has no ranking, promotion, allocation, scheduling,
  deployment, provider, broker, or runtime authority.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed real inputs,
  independent review, shared migration, promotion, runtime adoption, and
  production activation remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

OVR-406 is **VERIFIED_LOCAL** at code commit
`d99ec872c3fc04d015bd12c03d021698218f2fd9`. The corrected retained database
`augr_ovr406_qual_20260820_v2` is clean schema 87 and contains 5 immutable
family contracts, 1 comparison, 5 normalized family rows, and 30 normalized
tier rows. Comparison `62d4b849-5529-097a-9897-db4610176028` has SHA-256
`8917e9f7d42b8ad6f3b714173ffc9348c07204b75a76c44aeefbcf566a9b80d5`.
Its canonical JSON contains the required snake-case `schema`, `state`, and
`capital_policy_version` keys and none of the superseded Go-name variants.

The defined-risk fixture derives `$122` reserved capital per complete spread,
ten spreads of common two-leg executable depth, a first viable reviewed tier
of `$500`, and source-depth saturation from `$5,000` upward. Passive, wheel,
momentum/quality, and ETF trend are explicitly unavailable with
`source_capacity_not_observed`; no capacity or minimum is invented. Focused
capacity and PostgreSQL races pass after the canonical-envelope correction.
The preceding full backend qualification covered 4,570 race tests in 116
packages plus build, vet, repository-wide lint, formatting, and vulnerability
gates; pinned Node 22.23.2 frontend qualification covered 162 tests, lint, and
production build. The isolated production-image verifier then passed on the
exact corrected code commit: fresh migration `1 -> 87`, authenticated
read-only API smoke, lossless `87 -> 60`, schema-60 backup/restore, reapply
`61 -> 87`, and post-reapply health.

This evidence contains no rank, winner, recommendation, allocation, runtime
pointer, scheduler instruction, or execution authority. Licensed real inputs,
independent review, shared-database migration, promotion, runtime adoption,
deployment, and production activation remain **BLOCKED_EXTERNAL**.
