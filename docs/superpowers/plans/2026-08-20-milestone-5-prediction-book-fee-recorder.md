# Milestone 5 Prediction Book and Fee Recorder Plan

**Goal:** Complete OVR-505 with immutable, point-in-time prediction-market
books and versioned fee evidence so every replay consumes only executable size
and the exact policy available at its decision time.

**Architecture:** Add a `predictionreplay` boundary over one immutable OVR-301
`dataset.Manifest` and migration 92. Canonical book snapshots bind every
outcome, ordered bid/ask level, exchange/availability timestamp, and source
content hash to `prediction_books` observations. Canonical fee policies bind
venue, maker/taker role, effective window, availability, exact formula,
rounding mode, and source content hash to `prediction_fees` observations. A
pure replay quote consumes book levels in price-time order at a declared
decision time and returns filled quantity, residual, weighted price, gross
cash, exact fee, and net cash. It records evidence only and cannot create an
intent, reserve capital, place an order, promote a strategy, or schedule work.

**Migration:** `000092_prediction_book_fee_recorder`

## Locked contracts

1. Every book and fee policy must match one exact OVR-301 manifest observation
   by kind, partition content SHA-256, source key, content SHA-256, instrument,
   and availability time. Evidence outside the manifest fails closed.
2. Exchange/effective time never substitutes for decision availability. A
   book or policy is eligible only when `available_at <= decision_at <=`
   manifest cutoff and its own effective window covers the replayed fill.
3. A market has a stable venue/market identity and two or more stable outcome
   instruments. Each snapshot covers exactly one outcome instrument; outcome
   identity cannot be inferred from a display label or current provider state.
4. Bid levels strictly descend, ask levels strictly ascend, levels are
   contiguous from zero, price is in `(0,1)`, size is positive, and top bid is
   below top ask. Exact decimal scale is bounded; float input is forbidden.
5. Source revision ordering is explicit. For the same outcome and exchange
   instant, a correction must name the prior source key, have a later
   availability time, and increment revision. It cannot rewrite decisions made
   before that availability time.
6. Point-in-time selection takes the latest available exchange snapshot, then
   the highest eligible revision, with exact source identity as the final
   deterministic tie-break. Missing books or unavailable corrections return an
   explicit no-book result.
7. Buy replay walks asks from best to worse; sell replay walks bids from best to
   worse. It never fills beyond displayed size, silently crosses a caller
   limit, assumes top-of-book size for deeper quantity, or manufactures a fill
   for residual quantity.
8. Fee policies are venue- and liquidity-role-specific. V1 supports exact
   notional-BPS and contract-curve formulas, one declared decimal scale, and
   explicit half-up or ceiling rounding. Zero fee must be explicit evidence;
   missing or ambiguous fee policy fails closed.
9. Fee calculation uses the policy's declared aggregation boundary and exact
   filled level economics. Gross, fee, and net cash must reconstruct exactly;
   partial fills remain partial and carry their residual quantity.
10. Canonical input permutation produces the same recorder identity. Exact
    retries converge; changed manifest, market/outcome identity, book, fee,
    selection rule, or normalized replay conflicts.
11. Parent, book, level, fee, replay, and fill evidence is append-only,
    independently reconstructable, and atomic. Partial graphs, forged
    arithmetic, mutation, or unmanifested evidence fail closed.
12. This boundary records historical research evidence only. OVR-506 owns
    complete-set capital/orphan economics and OVR-507 owns maker/markout/
    inventory simulation; neither can bypass this recorder.

## Task 1: Manifest-bound book and fee model

- [ ] Define stable market/outcome, book/revision, and fee-policy inputs using
  exact decimals and UTC microsecond timestamps.
- [ ] Validate exact `prediction_books` and `prediction_fees` manifest
  membership, ordered depth, correction lineage, effective windows, and
  deterministic input permutation.
- [ ] Implement exact notional-BPS and contract-curve fee formulas with explicit
  maker/taker role, aggregation, scale, and rounding.
- [ ] Prove missing/unmanifested/late evidence, crossed/duplicate/malformed
  depth, correction lookahead, overlapping policy ambiguity, invalid scale,
  and float-like/inexact values fail closed.
- [ ] Commit and push the evidence-model slice.

## Task 2: Point-in-time executable replay

- [ ] Select the latest eligible outcome book and fee policy independently at
  each decision time without future revisions rewriting earlier results.
- [ ] Consume exact displayed levels for limit-bounded buys and sells, retaining
  fills, weighted price, gross cash, fees, net cash, and residual quantity.
- [ ] Retain explicit no-book, no-fee-policy, limit-blocked, partial, and filled
  outcomes; no rejection may become a synthetic fill.
- [ ] Prove multi-level price-time consumption, boundary equality, partial
  depth, correction availability, maker/taker fee differences, deterministic
  restart, canonical reconstruction, and no-lookahead.
- [ ] Commit and push the replay slice.

## Task 3: Migration 92 and retained qualification

- [ ] Persist immutable recorder/book/level/fee/replay/fill identity and
  normalized rows with PostgreSQL reconstruction and manifest-membership guards.
- [ ] Prove eight-writer convergence, changed retry conflict, every-stage atomic
  rollback, forgery rejection, append-only evidence, nonempty rollback refusal,
  and empty `92 -> 91 -> 92`.
- [ ] Retain two outcomes, multi-level books, one later correction, maker/taker
  policies, pre-correction and post-correction decisions, a partial fill, and an
  exact fee-rounding boundary.
- [ ] Add an inspection/recovery/rollback runbook with exact IDs and digests.
- [ ] Run focused/database races, repository-wide backend/static and pinned
  frontend gates, diff review, and isolated kill-switched schema-92 health/API/
  rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-506.

## Acceptance evidence to record

- [ ] Every replay uses only book size and fee policy decision-available at its
  declared time; later corrections never rewrite prior results.
- [ ] Exact displayed depth caps fills and every gross/fee/net value
  reconstructs from normalized levels under the bound policy.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed historical acquisition,
  independent review, shared migration, runtime adoption, strategy evaluation,
  scheduling, deployment, venue routing, and live trading remain
  `BLOCKED_EXTERNAL`.
