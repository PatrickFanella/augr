# Milestone 5 Copy Quote, Spread, and Session Gates Plan

**Goal:** Complete OVR-502 by ensuring a copy intent is approved only from one
exact point-in-time bid/ask snapshot that is decision-available, fresh, crossed
correctly, inside the configured real spread, and in an explicitly allowed
market session.

**Architecture:** Replace the OHLCV-close approval adapter with an OVR-201/203
quote-snapshot adapter backed by canonical instrument aliases and immutable
`quote_snapshots`. Add migration 89 for subscription quote/session policy and
intent decision evidence. The provider selects by instrument, provider, venue,
namespace, and `available_at <= decision_at`; the target engine derives the
executable side (ask for buys, bid for sells) and spread from exact decimals.
Daily closes may remain liquidity context but can never approve execution.

**Migration:** `000089_copy_quote_session_gates`

## Locked contracts

1. Approved intents identify one immutable quote snapshot and exact decision
   time. Missing or future-available evidence fails closed.
2. Bid and ask must both exist and be positive, with `ask >= bid`. Zero,
   one-sided, crossed, NaN, infinite, or caller-authored spread is rejected.
3. Spread BPS is `(ask-bid)/midpoint*10000`, derived in trusted decimal code.
   The subscription maximum is inclusive at equality and rejects above it.
4. Buy executable price is ask; sell executable price is bid. Midpoint, last,
   mark, daily close, and a zero default cannot substitute.
5. Quote age is `decision_at-available_at`; negative or above the configured
   maximum is rejected. Exchange/receive/ingest time cannot make unavailable
   data eligible.
6. Market status must be `open`; session must be in the subscription's closed
   allowlist, initially `regular`. Missing/unknown/closed/halted/auction and
   disallowed extended sessions reject approval.
7. Quote identity, bid, ask, side price, derived spread, statuses, available
   time, decision time, and rejection reasons persist with the copy intent and
   reconstruct exactly after restart.
8. The OVR-501 subscription origin is unchanged through proposal. A quote
   cannot change origin, source filing, target economics, or calculation
   version.
9. The gate remains paper-only and grants no promotion, scheduler, deployment,
   provider-routing, broker-routing, or live-trading authority.
10. Migration 89 is additive/lock-first and empty-only reversible.

## Task 1: Exact quote adapter and policy model

- [ ] Add maximum quote age and allowed-session policy with conservative
  defaults and validation.
- [ ] Resolve mapped instruments and select only point-in-time available OVR-201
  snapshots from the configured source namespace.
- [ ] Remove OHLCV/daily-close eligibility; retain it only as nonauthoritative
  liquidity context if needed.
- [ ] Prove missing alias/snapshot, future availability, stale quote, and source
  namespace mismatch fail closed.
- [ ] Commit and push the adapter slice.

## Task 2: Target-side executable gates

- [ ] Derive exact bid/ask midpoint spread and side-specific price.
- [ ] Enforce positive two-sided noncrossed book, inclusive spread threshold,
  freshness, market-open, and allowed-session policy.
- [ ] Persist explicit reasons for every rejected edge without adding rejected
  notional to approved turnover.
- [ ] Prove buy/sell side, threshold equality, one tick above, zero/missing/
  crossed book, regular/extended/closed/halted, and time-boundary cases.
- [ ] Commit and push the engine slice.

## Task 3: Migration 89 and reconstruction

- [ ] Add subscription quote policy and copy-intent quote evidence columns with
  parent/source/origin guards.
- [ ] Reconstruct quote identity, exact decimals, derived spread, side price,
  statuses, time boundary, and policy result.
- [ ] Reject mutation of immutable decision evidence, changed retry, quote
  forgery, partial write, origin drift, and arithmetic drift.
- [ ] Add empty-only rollback and bump `RequiredSchemaVersion` to 89 after real
  PostgreSQL migration races pass.
- [ ] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [ ] Add a runbook for source selection, quote age/spread/session inspection,
  rejection diagnosis, replay, recovery, and rollback.
- [ ] Retain approved and rejected buy/sell cases at exact boundaries with IDs,
  hashes, arithmetic, reasons, and counts.
- [ ] Prove concurrent convergence, restart, injected-stage rollback,
  append-only evidence, nonempty refusal, and empty `89 -> 88 -> 89`.
- [ ] Run focused/database races, all backend/static and pinned frontend gates,
  diff review, and isolated kill-switched schema-89 health/API/rollback/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-503.

## Acceptance evidence to record

- [ ] Zero/missing/crossed or stale spread evidence cannot approve an intent.
- [ ] Only an explicitly open allowed session can approve an intent.
- [ ] Executable price and spread are derived from the exact persisted quote,
  never from a daily close or caller-provided zero.
- [ ] OVR-501 origin attribution remains exact through OVR-203 proposal.
- [ ] Local qualification is `VERIFIED_LOCAL`; licensed live quote inputs,
  shared migration, independent review, runtime adoption, deployment, broker
  routing, and live trading remain `BLOCKED_EXTERNAL`.
