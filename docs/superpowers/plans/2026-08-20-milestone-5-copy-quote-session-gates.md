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

- [x] Add maximum quote age and allowed-session policy with conservative
  defaults and validation.
- [x] Resolve mapped instruments and select only point-in-time available OVR-201
  snapshots from the configured source namespace.
- [x] Remove OHLCV/daily-close eligibility; retain it only as nonauthoritative
  liquidity context if needed.
- [x] Prove missing alias/snapshot, future availability, stale quote, and source
  namespace mismatch fail closed.
- [x] Commit and push the adapter slice.

## Task 2: Target-side executable gates

- [x] Derive exact bid/ask midpoint spread and side-specific price.
- [x] Enforce positive two-sided noncrossed book, inclusive spread threshold,
  freshness, market-open, and allowed-session policy.
- [x] Persist explicit reasons for every rejected edge without adding rejected
  notional to approved turnover.
- [x] Prove buy/sell side, threshold equality, one tick above, zero/missing/
  crossed book, regular/extended/closed/halted, and time-boundary cases.
- [x] Commit and push the engine slice.

## Task 3: Migration 89 and reconstruction

- [x] Add subscription quote policy and copy-intent quote evidence columns with
  parent/source/origin guards.
- [x] Reconstruct quote identity, exact decimals, derived spread, side price,
  statuses, time boundary, and policy result.
- [x] Reject mutation of immutable decision evidence, changed retry, quote
  forgery, partial write, origin drift, and arithmetic drift.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 89 after real
  PostgreSQL migration races pass.
- [x] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [x] Add a runbook for source selection, quote age/spread/session inspection,
  rejection diagnosis, replay, recovery, and rollback.
- [x] Retain approved and rejected buy/sell cases at exact boundaries with IDs,
  hashes, arithmetic, reasons, and counts.
- [x] Prove concurrent convergence, restart, injected-stage rollback,
  append-only evidence, nonempty refusal, and empty `89 -> 88 -> 89`.
- [x] Run focused/database races, all backend/static and pinned frontend gates,
  diff review, and isolated kill-switched schema-89 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before OVR-503.

## Acceptance evidence to record

- [x] Zero/missing/crossed or stale spread evidence cannot approve an intent.
- [x] Only an explicitly open allowed session can approve an intent.
- [x] Executable price and spread are derived from the exact persisted quote,
  never from a daily close or caller-provided zero.
- [x] OVR-501 origin attribution remains exact through OVR-203 proposal.
- [x] Local qualification is `VERIFIED_LOCAL`; licensed live quote inputs,
  shared migration, independent review, runtime adoption, deployment, broker
  routing, and live trading remain `BLOCKED_EXTERNAL`.

## Local qualification record (2026-08-20)

OVR-502 is **VERIFIED_LOCAL** at schema 89. The retained loopback database
`augr_ovr502_qual_20260820_v3` contains subscription
`5072fed9-4cfe-4a32-8fc8-bd67a8267f46`, two gate-version-1 approved intents,
and one gate-version-1 persisted skipped intent. Buy quote
`35fa2641-95f2-438a-8918-37be89d571d8`
is exactly at the inclusive 100 BPS boundary and uses its ask; sell quote
`39f33368-6517-4df7-b570-5571496abeeb` is 4 BPS and uses its bid. The skipped
intent retains the exact stale-quote reason without contributing approved
turnover. Exact retries converge, changed retries conflict, and a direct SQL
attempt to forge the derived spread is rejected by PostgreSQL reconstruction.

Eight-writer convergence, restart, injected-stage rollback, append-only
enforcement, nonempty rollback refusal, and empty disposable
`89 -> 88 -> 89` passed. The full nonexternal backend race suite covered 4,584
tests across 117 packages; backend build, vet, repository-wide lint, format,
and called-symbol vulnerability gates passed. Pinned Node 22.23.2 frozen
install/audit, 162 frontend tests, lint, and production build passed. The
isolated production image built from exact commit
`476ebbbcc25013e135728833473c93b658e5b8a7` passed fresh `1 -> 89`, health,
authenticated read-only API smoke, `89 -> 60`, schema-60 backup/restore, and
`61 -> 89` reapplication.

The canonical provider is wired but the retained fixture is synthetic. No
licensed live quote feed was claimed. Licensed inputs, shared migration,
independent review, provider population, deployment, broker routing, and live
trading remain **BLOCKED_EXTERNAL**.
