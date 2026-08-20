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

- [x] Define exact target/current/session inputs and canonical prepared output.
- [x] Derive canonical legs with hard turnover cap, monotonic progress, no
  overshoot, exact projected values, and explicit residual drift.
- [x] Prove sells, buys, mixed books, exact boundary, sub-cent rejection, zero
  drift, insufficient budget, input permutation, and multi-session convergence.
- [x] Commit and push the pure engine slice.

## Task 2: Migration 90 and repository

- [x] Persist immutable run identity, source/session/policy inputs, exact totals,
  canonical bytes/JSON/digest, and normalized ordered legs.
- [x] Enforce subscription origin/source ownership, session uniqueness, leg
  reconstruction, run totals, append-only evidence, and empty-only rollback.
- [x] Prove exact concurrent retry convergence, changed retry conflict, restart,
  injected-stage rollback, forgery rejection, and nonempty rollback refusal.
- [x] Bump `RequiredSchemaVersion` to 90 after real PostgreSQL races pass.
- [x] Commit and push the persistence slice.

## Task 3: Multi-session retained qualification

- [x] Retain at least three sessions using the same source observation, where
  each session starts from the prior projection and the final session converges.
- [x] Prove every session remains inside its own cap, aggregate progress is
  monotonic, source/origin never drift, and no new filing row is created.
- [x] Add an inspection/recovery/rollback runbook with exact retained IDs,
  digests, counts, budgets, and residuals.
- [x] Run focused/database races, all backend/static and pinned frontend gates,
  diff review, and isolated kill-switched schema-90 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before OVR-504.

## Acceptance evidence to record

- [x] Turnover-capped targets converge across multiple independently keyed
  sessions without a new source observation.
- [x] No session exceeds its budget, reverses direction, or crosses a target.
- [x] Retained normalized rows and canonical bytes reconstruct exact arithmetic.
- [x] Local qualification is `VERIFIED_LOCAL`; trusted runtime origin positions,
  licensed quotes, shared migration, independent review, account/lifecycle
  adoption, scheduling, deployment, broker routing, and live trading remain
  `BLOCKED_EXTERNAL`.

## Local qualification record (2026-08-20)

OVR-503 is **VERIFIED_LOCAL** at schema 90. Retained loopback database
`augr_ovr503_qual_20260820` contains subscription/origin
`2f7efcc7-e85f-4081-8603-d5331735cd13`, the single unchanged source
observation `2e84cf5e-b389-47d9-bc68-d50889d5e60c`, four prepared sessions,
and five normalized legs. Starting drift `$9,000.00` converges with residuals
`$6,500.00 -> $4,000.00 -> $1,500.00 -> $0.00`; prepared turnover is
`$2,500.00` in each of the first three sessions and `$1,500.00` in the last.

The retained run identities are:

| Session | Run | SHA-256 |
| --- | --- | --- |
| `2026-08-20/regular` | `40569201-3e41-a4e6-5e4c-695c765e2afe` | `96d6259ba29da7d5a254229a4f21f5649a562a7a03edd991627fa229ee8fc3f4` |
| `2026-08-21/regular` | `5f4c50df-93bf-1402-be0c-fcc63173ce5e` | `786571324bee65a0dfca39916cb318b4b166b8a477cb1dbfcdd1a8e927bca253` |
| `2026-08-24/regular` | `710fbccf-045f-2c5a-d828-c310801aef06` | `af90b8cf90938dd06f1fc9c30e372ad87f882456b8fc019a23ac790cc04b0e78` |
| `2026-08-25/regular` | `318e6509-876b-4802-1fa0-ac2f9c8296e3` | `91095b14befcb8496adf64edeb8ac3a57caf17a9fc2a3c094d81b78082ccb9f6` |

Eight identical writers converge. Changed same-session evidence conflicts;
injected run/leg failures leave no partial graph; restart reload, normalized
forgery rejection, and append-only enforcement pass. The separate empty
database `augr_ovr503_empty_20260820` passed `90 -> 89 -> 90`; the retained
database refused rollback and preserved all evidence.

The final exact code commit
`e6efba6387887a61645c6d6160aed584a22cdbdb` passed 4,588 race tests across
117 packages, backend build/vet/lint/gofumpt/called-symbol vulnerability gates,
and pinned Node 22.23.2 frozen install, 162 tests, lint, production build, and
the high-severity audit gate. The isolated production verifier passed fresh
`1 -> 90`, health, authenticated read-only API smoke, `90 -> 60`, schema-60
backup/restore, and `61 -> 90` reapplication.

The retained starting values are explicit synthetic origin-attributed inputs,
not a claim that runtime positions were read from a broker or shared account.
A trusted runtime origin-position snapshot, licensed quote feed, OVR-502 quote
handoff, account/lifecycle adoption, shared migration, independent review,
scheduling, deployment, broker routing, and live trading remain
**BLOCKED_EXTERNAL**.
