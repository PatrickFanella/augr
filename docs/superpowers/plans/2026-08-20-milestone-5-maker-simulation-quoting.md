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

- [ ] Expose detached point-in-time books, levels, and exact maker-fee
  calculation without exposing mutable recorder canonical state.
- [ ] Prove corrections obey availability, inside-price lookup is stable, and
  fee formulas retain exact scale and rounding.
- [ ] Commit and push the OVR-505 simulation-view slice.

## Task 2: Queue, markout, inventory, and quote evaluator

- [ ] Validate passive inside quotes, displayed plus prior queue, complete
  weighted scenario sets, inventory state/limit, carry rate, and minimum.
- [ ] Compute deterministic no/partial/full fills, horizon midpoint markouts,
  exact maker fees, elapsed inventory cost, and weighted expected net capture.
- [ ] Prove buy/sell symmetry, adverse marks, queue equality, corrections,
  inventory-limit rejection, no-fill rejection, strict-profit equality,
  permutation, and canonical reconstruction.
- [ ] Commit and push the pure maker evaluator.

## Task 3: Migration 94 and retained qualification

- [ ] Persist immutable candidate and scenario identity with recorder guards and
  PostgreSQL reconstruction.
- [ ] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `94 -> 93 -> 94`.
- [ ] Retain one positive buy quote, one adverse/nonpositive rejection, one
  no-fill boundary, and one exact strict-profit boundary.
- [ ] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [ ] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-94 health/API/
  rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before Milestone 6.

## Acceptance evidence to record

- [ ] Every qualified quote is passive at decision time, consumes only observed
  outflow beyond immutable queue ahead, and uses later point-in-time marks.
- [ ] Expected net spread capture remains strictly positive after exact maker
  fees, adverse/favorable markouts, and inventory carrying cost.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed trade flow, queue
  calibration, independent review, shared migration, scheduling, deployment,
  venue routing, and live trading remain `BLOCKED_EXTERNAL`.
