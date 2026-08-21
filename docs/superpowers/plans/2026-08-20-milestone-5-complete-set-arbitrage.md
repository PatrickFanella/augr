# Milestone 5 Complete-Set Arbitrage Plan

**Goal:** Complete OVR-506 with a deterministic research engine that accepts a
prediction-market complete set only when every outcome leg, exact fees, capital
reservation, and the worst executable orphan unwind remain profitable.

**Architecture:** Add a `completeset` boundary over one immutable OVR-505
`predictionreplay.Recorder` and migration 93. A candidate names the complete
outcome set, one same-time filled taker-buy replay and one same-time filled
sell/unwind replay per outcome, exact set quantity, available capital, payout
per complete set, and a hard minimum profit. The engine reconstructs entry
cash and unwind proceeds from OVR-505 normalized fills and fees, enumerates
every nonempty proper subset as a possible orphan state, and reserves entry
cash plus the greatest executable orphan loss. Only a fully covered candidate
whose payout minus all entry costs, minimum profit, and worst orphan loss stays
strictly positive is `qualified`. It remains research evidence and cannot
reserve real cash, create intents, or route orders.

**Migration:** `000093_complete_set_arbitrage`

## Locked contracts

1. One exact OVR-505 recorder ID/SHA-256 is mandatory. Every entry and unwind
   replay sequence must exist in its canonical graph and reconstruct from its
   normalized rows.
2. A complete set has at least two unique stable outcome instrument IDs for one
   market. Caller order is irrelevant; missing, duplicated, or extra outcomes
   fail closed.
3. All legs share one UTC microsecond decision time. Entry legs are filled
   taker buys for exactly the declared set quantity; unwind legs are filled
   sells for the same outcome and quantity. Partial, no-book, no-fee,
   limit-blocked, maker-entry, mismatched-time, or cross-market rows fail closed.
4. Entry cost is the exact sum of OVR-505 buy net cash. Unwind proceeds are the
   exact sum of sell net cash. No midpoint, mark, top-price multiplication, or
   fee recomputation may replace the recorded multi-level result.
5. Complete-set payout is `set_quantity * payout_per_set`. Payout and quantity
   are exact positive decimals; prediction probability prices remain in the
   OVR-505 `(0,1)` contract.
6. Every nonempty proper outcome subset is an orphan scenario. Its loss is the
   sum of that subset's entry costs minus its exact same-time unwind proceeds,
   floored at zero. The greatest loss wins; canonical outcome identity breaks
   ties.
7. Reserved capital is `entry_cost + worst_orphan_loss`. Available capital must
   cover it exactly; unlimited paper buying power or unsettled future payout is
   never counted as current capital.
8. After-cost complete-set profit is `payout - entry_cost`. Qualified profit
   after the orphan guard is `after_cost_profit - worst_orphan_loss`. Both must
   exceed the declared minimum profit; equality is a rejection.
9. A rejected candidate retains one stable reason: incomplete set, invalid
   replay, insufficient capital, nonpositive complete-set profit, or orphan
   guard failure. Rejection never emits an intent or synthetic leg.
10. Canonical input permutation produces the same identity. Exact retries
    converge; changed recorder, set, replay binding, capital, payout, or minimum
    profit conflicts.
11. Parent, leg, scenario, and scenario-leg evidence is append-only,
    independently reconstructable, and atomic. Partial graphs, forged sums,
    mutation, or recorder mismatch fail closed.
12. OVR-506 does not model maker queue probability or inventory markout
    (OVR-507), acquire licensed data, reserve runtime capital, schedule work,
    promote strategies, deploy, or trade live.

## Task 1: Complete-set and capital model

- [x] Expose immutable OVR-505 replay results without exposing mutable
  canonical state.
- [x] Validate complete outcome coverage, same-time exact entry/unwind replay
  binding, quantity, payout, available capital, and minimum profit.
- [x] Compute exact entry cost, payout, after-cost profit, and capital coverage
  only from recorded fills/fees.
- [x] Prove missing/duplicate/extra outcomes, partial/cross-market/cross-time/
  wrong-side/wrong-role/wrong-quantity rows, and insufficient capital fail.
- [x] Commit and push the complete-set slice.

## Task 2: Orphan enumeration and qualification

- [x] Enumerate all nonempty proper subsets deterministically and retain every
  scenario plus its exact entry/unwind leg economics.
- [x] Select worst nonnegative orphan loss with stable tie-breaking and require
  strict profit after the orphan guard and declared minimum.
- [x] Retain qualified and rejected candidates with exact reasons; no rejection
  may emit an execution artifact.
- [x] Prove all-leg success, each singleton orphan, multi-leg orphan, zero-loss
  floor, equality boundaries, input permutation, and canonical reconstruction.
- [x] Commit and push the orphan-guard slice.

## Task 3: Migration 93 and retained qualification

- [x] Persist immutable candidate/leg/scenario/scenario-leg identity and
  normalized rows with PostgreSQL reconstruction and OVR-505 recorder guards.
- [x] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `93 -> 92 -> 93`.
- [x] Retain at least one qualified set, one insufficient-capital rejection,
  all singleton orphan paths, a multi-leg orphan path, and an exact strict
  profitability boundary.
- [x] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [x] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-93 health/API/
  rollback/backup/restore/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before OVR-507.

## Acceptance evidence to record

- [x] Every qualified candidate covers every outcome at exact executable size,
  exact recorded fees, and one same-time executable unwind per leg.
- [x] Available capital covers entry plus the enumerated worst orphan loss, and
  profit after that guard strictly exceeds the declared minimum.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed inputs, independent
  review, shared migration, runtime capital reservation, scheduling, deployment,
  venue routing, and live trading remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

OVR-506 is `VERIFIED_LOCAL` on retained loopback database
`augr_ovr506_qual_20260820_v2` at schema 93. Recorder
`75a5cfb9-f0a9-cbf3-fbbe-ef89ae9f4f3a` produced qualified candidate
`e0780885-932e-4544-473e-37d4f1f3e615` with SHA-256
`52277d1d03f0b2b746059cfd25ce4a60b9a6f20fc5b9bf3165e79b9286056b20`,
insufficient-capital rejection `4a2ee54d-eabc-6c53-5f0d-275cf4dcb286`,
and exact strict-boundary rejection `e69e1f7b-2fec-818e-8aea-df862efa4cec`.
The retained graph has three candidates, nine bindings, nine legs, eighteen
orphan scenarios, and twenty-seven scenario legs.

The qualified three-outcome fixture records entry cost `9`, payout `10`,
after-cost profit `1`, worst orphan loss `0.2`, reserved capital `9.2`, and
profit after the orphan guard `0.8`, strictly above minimum profit `0.5`.
The equality fixture sets minimum profit to `0.8` and is rejected with
`orphan_guard_failure`. Eight concurrent writers converge, a changed retry
conflicts, every persistence stage rolls back atomically, restart reconstruction
matches the canonical digest, and forged or mutated evidence is rejected.
Migration 93 passed empty `93 -> 92 -> 93`; the retained graph refused rollback.

Repository-wide race tests (4,599 tests in 124 packages), build, vet, lint,
format, and reachable-vulnerability checks passed. Pinned Node 22.23.2 passed
162 frontend tests, lint, and production build; `npm audit --omit=dev` retains
one low-severity Windows-only development-server advisory in esbuild. The
isolated production verifier passed fresh `1 -> 93`, health, authenticated
read-only API smoke, `93 -> 60` rollback, schema-60 backup/restore, reapply to
93, and post-reapply health. Licensed inputs, independent review, shared
migration, runtime capital reservation, scheduling, deployment, venue routing,
and live trading remain `BLOCKED_EXTERNAL`.
