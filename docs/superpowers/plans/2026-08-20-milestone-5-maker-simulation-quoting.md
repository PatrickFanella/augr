# Milestone 5 Maker Simulation and Quoting Plan

**Goal:** Complete OVR-507 with a deterministic research quote evaluator whose
expected spread capture remains strictly positive after point-in-time markouts,
exact maker fees, queue position, and inventory carrying cost.

**Architecture:** Add a `makerquote` boundary over one immutable OVR-505
`predictionreplay.Recorder` and migration 94. A candidate names one passive
inside quote, exact size, starting inventory, inventory limit, hourly carrying
rate, and a complete weighted scenario set. Each scenario names an exact
point-in-time mark horizon and observed queue outflow. The engine selects only
books and maker fee policy available at the quote or horizon instant, fills
only outflow strictly beyond displayed queue ahead, marks filled inventory to
the later executable midpoint, applies exact OVR-505 fee rounding, charges
inventory cost for the elapsed microsecond duration, and qualifies only when
weighted net capture strictly exceeds the declared minimum. It remains
research evidence and cannot create intents, reserve capital, schedule work,
or route orders.

**Migration:** `000094_maker_simulation_quoting`

## Locked contracts

1. One exact OVR-505 recorder ID/SHA-256 is mandatory. Quote books, mark books,
   and maker fees must reconstruct from that recorder's immutable graph.
2. A quote names one market, outcome, side, UTC-microsecond decision time,
   exact positive quantity, and passive price. Buy price must equal the selected
   best bid; sell price must equal the selected best ask. Crossed, off-inside,
   unavailable, corrected-too-late, or missing-fee quotes fail closed.
3. Displayed queue ahead is the selected same-price book size plus explicit
   nonnegative prior queue. No caller may erase displayed size or assume front
   position.
4. Each scenario has a unique stable key, exact positive weight, horizon after
   the decision, and nonnegative observed queue outflow. Weights must sum to one
   exactly and caller order is irrelevant.
5. Filled quantity is `min(max(queue_outflow - queue_ahead, 0), quote_quantity)`.
   Equality with queue ahead produces no fill. Residual quantity is retained;
   no synthetic fill or probabilistic rounding is allowed.
6. The horizon mark is the midpoint of the latest book available at the
   scenario horizon for the same market/outcome. Exchange time, availability
   time, revision, and correction lineage remain distinct; later availability
   cannot rewrite an earlier scenario.
7. Gross spread capture is `(mark - quote) * fill` for buys and
   `(quote - mark) * fill` for sells. Adverse markout is retained as a negative
   value, never floored or discarded.
8. Maker fee is reconstructed with the exact point-in-time OVR-505 formula,
   scale, and rounding over the simulated fill. Inventory cost is
   `abs(post_fill_inventory) * quote_price * hourly_rate * elapsed_hours` with
   exact decimal arithmetic at microsecond duration.
9. A scenario's net capture is gross spread capture minus fee and inventory
   cost. Weighted expected net capture is the exact sum of `weight * net`.
   Qualification requires at least one fill, inventory within the hard limit,
   and expected net capture strictly greater than both zero and the declared
   minimum; equality is rejected.
10. Rejections retain one stable reason: invalid quote, invalid scenarios, no
    fill, inventory limit, or nonpositive net capture. They never emit orders,
    intents, or reservations.
11. Canonical input permutation produces the same identity. Exact retries
    converge; changed recorder, quote, queue, scenario, cost, inventory, or
    minimum under the same key conflicts.
12. Parent and scenario evidence is append-only, independently reconstructable,
    and atomic. OVR-507 does not infer real queue priority, acquire licensed
    trades, calibrate probabilities, deploy, or trade live.

## Task 1: Immutable OVR-505 simulation view

- [x] Expose detached point-in-time books, levels, and exact maker-fee
  calculation without exposing mutable recorder canonical state.
- [x] Prove corrections obey availability, inside-price lookup is stable, and
  fee formulas retain exact scale and rounding.
- [x] Commit and push the OVR-505 simulation-view slice.

## Task 2: Queue, markout, inventory, and quote evaluator

- [x] Validate passive inside quotes, displayed plus prior queue, complete
  weighted scenario sets, inventory state/limit, carry rate, and minimum.
- [x] Compute deterministic no/partial/full fills, horizon midpoint markouts,
  exact maker fees, elapsed inventory cost, and weighted expected net capture.
- [x] Prove buy/sell symmetry, adverse marks, queue equality, corrections,
  inventory-limit rejection, no-fill rejection, strict-profit equality,
  permutation, and canonical reconstruction.
- [x] Commit and push the pure maker evaluator.

## Task 3: Migration 94 and retained qualification

- [x] Persist immutable candidate and scenario identity with recorder guards and
  PostgreSQL reconstruction.
- [x] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `94 -> 93 -> 94`.
- [x] Retain one positive buy quote, one adverse/nonpositive rejection, one
  no-fill boundary, and one exact strict-profit boundary.
- [x] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [x] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-94 health/API/
  rollback/backup/restore/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before Milestone 6.

## Acceptance evidence to record

- [x] Every qualified quote is passive at decision time, consumes only observed
  outflow beyond immutable queue ahead, and uses later point-in-time marks.
- [x] Expected net spread capture remains strictly positive after exact maker
  fees, adverse/favorable markouts, and inventory carrying cost.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed trade flow, queue
  calibration, independent review, shared migration, scheduling, deployment,
  venue routing, and live trading remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

OVR-507 is `VERIFIED_LOCAL` on retained loopback database
`augr_ovr507_qual_20260820_v2` at schema 94. Recorder
`cd9a0810-a42e-a8f8-0a2f-604f45b42f92` produced qualified candidate
`c06f1c08-93fd-b589-acd5-b8aad1df1382` with SHA-256
`7d5e2f7283a47fb7970da1417f6f1ef1bba1ecb3a4fe873e521c8f1494a1768e`
and expected net capture `0.02985`. Qualified buy candidate
`05c7b77b-6a8e-71d0-86c4-4dafaadd6e39` has SHA-256
`18ca8721832ba48d8a14ac0505831cbf5526d63c87080056984ceabbd207b632`
and expected net capture `0.0408425`. The retained graph also includes high-cost
nonpositive rejection `06553e27-fdb1-475e-416e-4f944fd72ccf`, queue-equality
no-fill rejection `e5609fa5-39e2-bf57-d9f8-ab09de160191`, and exact expected-
net equality rejection `2d05c9c6-93a9-da84-0ff1-f3925383946c`. It has five
candidates and ten normalized scenarios.

Eight concurrent writers converge, a changed retry conflicts, both persistence
stages roll back atomically, restart reconstruction matches the canonical
digest, and forged or mutated evidence is rejected. Migration 94 passed empty
`94 -> 93 -> 94`; retained rollback refused with all four candidates and eight
scenarios intact. The expected `golang-migrate` refusal set only its version
metadata dirty, so the dedicated qualification database was inspected and
forced back to clean version 94 without changing evidence.

Repository-wide race tests, build, vet, lint, format, and reachable-
vulnerability checks passed. Pinned Node 22.23.2 passed 162 frontend tests,
lint, and production build; `npm audit --omit=dev` retains one low-severity
Windows-only development-server advisory in esbuild. The isolated production
verifier passed fresh `1 -> 94`, health, authenticated read-only API smoke,
`94 -> 60` rollback, schema-60 backup/restore, reapply to 94, and post-reapply
health. Licensed trade flow, queue calibration, independent review, shared
migration, scheduling, deployment, venue routing, and live trading remain
`BLOCKED_EXTERNAL`.
