# Milestone 5 Copy Multi-Session Target-Drift Reconciler Plan

**Goal:** Complete OVR-503 by turning one immutable copy target into a sequence
of independently keyed, turnover-capped paper sessions that converge from exact
origin-attributed starting values without requiring a new filing.

**Architecture:** Add a deterministic `copydrift` boundary and migration 90.
Each prepared session binds one OVR-501 subscription origin, one existing source
observation, a closed session key, exact target/current value vectors, and an
explicit per-session turnover budget. The engine walks canonical instrument
order, spends no more than the remaining budget, never crosses a target, and
persists projected post-session values plus a canonical digest. A later session
uses the prior projected values as new explicit input while retaining the same
source observation. This boundary prepares evidence only; it does not invent
positions, quotes, fills, or execution authority.

**Migration:** `000090_copy_target_drift_sessions`

## Locked contracts

1. Every run is attributed exactly to
   `copy_subscription/<subscription UUID>` and one existing source observation
   owned by that subscription's source.
2. Session keys are closed values of the form `YYYY-MM-DD/<session>`, where the
   session is `regular`, `pre_market`, or `after_hours`; a subscription may have
   at most one run for the same observation, session key, and calculation
   version.
3. Target and starting-current values are exact nonnegative decimals rounded to
   cents. Missing instruments mean zero; duplicate, blank, negative, NaN,
   infinite, or caller-ordered ambiguity is rejected.
4. Each leg moves toward its target, never beyond it. Sells and buys both consume
   positive turnover; projected value equals current plus signed requested
   notional exactly.
5. The sum of requested notionals is at most the explicit session budget and at
   most total starting absolute drift. Zero drift produces an explicit
   converged run with no legs; a positive drift and zero budget is rejected.
6. Allocation is deterministic: instruments are canonical-key ordered and each
   leg consumes `min(remaining drift, remaining budget)`. Repeated sessions
   therefore make monotonic progress and reach the exact target in finitely many
   sessions whenever positive budget continues.
7. The source observation may remain unchanged across sessions. A new filing is
   neither required nor silently substituted.
8. Same-key exact retries converge; changed starting state, target, budget,
   source, session, attribution, or calculation evidence conflicts.
9. Runs and normalized legs are append-only and independently reconstructable
   after restart. Partial graphs, arithmetic drift, reordered legs, and direct
   mutation fail closed.
10. OVR-503 is paper preparation only. A trusted origin-position snapshot,
    OVR-502 quote gate, account binding, common-lifecycle proposal, scheduler,
    provider, broker, deployment, or live-trading authority remains separate.

## Task 1: Deterministic drift engine

- [ ] Define exact target/current/session inputs and canonical prepared output.
- [ ] Derive canonical legs with hard turnover cap, monotonic progress, no
  overshoot, exact projected values, and explicit residual drift.
- [ ] Prove sells, buys, mixed books, exact boundary, sub-cent rejection, zero
  drift, insufficient budget, input permutation, and multi-session convergence.
- [ ] Commit and push the pure engine slice.

## Task 2: Migration 90 and repository

- [ ] Persist immutable run identity, source/session/policy inputs, exact totals,
  canonical bytes/JSON/digest, and normalized ordered legs.
- [ ] Enforce subscription origin/source ownership, session uniqueness, leg
  reconstruction, run totals, append-only evidence, and empty-only rollback.
- [ ] Prove exact concurrent retry convergence, changed retry conflict, restart,
  injected-stage rollback, forgery rejection, and nonempty rollback refusal.
- [ ] Bump `RequiredSchemaVersion` to 90 after real PostgreSQL races pass.
- [ ] Commit and push the persistence slice.

## Task 3: Multi-session retained qualification

- [ ] Retain at least three sessions using the same source observation, where
  each session starts from the prior projection and the final session converges.
- [ ] Prove every session remains inside its own cap, aggregate progress is
  monotonic, source/origin never drift, and no new filing row is created.
- [ ] Add an inspection/recovery/rollback runbook with exact retained IDs,
  digests, counts, budgets, and residuals.
- [ ] Run focused/database races, all backend/static and pinned frontend gates,
  diff review, and isolated kill-switched schema-90 health/API/rollback/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-504.

## Acceptance evidence to record

- [ ] Turnover-capped targets converge across multiple independently keyed
  sessions without a new source observation.
- [ ] No session exceeds its budget, reverses direction, or crosses a target.
- [ ] Retained normalized rows and canonical bytes reconstruct exact arithmetic.
- [ ] Local qualification is `VERIFIED_LOCAL`; trusted runtime origin positions,
  licensed quotes, shared migration, independent review, account/lifecycle
  adoption, scheduling, deployment, broker routing, and live trading remain
  `BLOCKED_EXTERNAL`.
