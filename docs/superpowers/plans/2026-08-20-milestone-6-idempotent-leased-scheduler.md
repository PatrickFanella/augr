# Milestone 6 Idempotent Leased Scheduler Plan

> **For agentic workers:** Execute this plan test-first in coherent commits.
> Preserve the local-only, kill-switched boundary and prove every distributed
> claim in real PostgreSQL with two independent scheduler instances.

**Goal:** Complete OVR-604 with a database-clock, fenced, idempotent occurrence
contract for every financial job so two application instances cannot create a
second execution occurrence, OVR-203 intent/order identity, or settlement
effect for the same scheduled work.

**Architecture:** Add an additive `internal/financialscheduler` boundary rather
than treating either process-local `Running` flags or `sync.Map` deduplication as
distributed coordination. A closed job catalog classifies every registered
financial mutation job. Its schedule resolver creates a deterministic
occurrence UUID from the job key, schedule revision, trigger kind, and exact UTC
due time. PostgreSQL schema 98 owns occurrence admission, monotonically
increasing fencing tokens, renewable leases, terminal outcomes, and immutable
effect claims. Only the current owner/fence may renew, claim effects, or finish.
An effect claim is the required transaction-local bridge to the stable
idempotency key used by OVR-203 intent/order creation or settlement; retries and
lease takeovers therefore converge on the same effect identity. The existing
automation and strategy schedulers remain inactive until a later reviewed
cutover, but their complete financial job catalog and mutation adapters must be
representable by this contract.

**Boundary:** Local synthetic PostgreSQL only. No provider call, shared
migration, scheduler activation, broker route, lifecycle cutover, allocation,
settlement of a real position, or live trade is authorized.

## Contract decisions

1. PostgreSQL time is authoritative for acquisition, renewal, expiry, and
   completion. Caller clocks may identify a due slot but cannot extend a lease.
2. Occurrence identity is content-addressed from `job_key`, immutable
   `schedule_revision`, `trigger_kind`, and microsecond-normalized UTC due time.
   A semantic retry returns the same occurrence; changed reuse conflicts.
3. Job keys, trigger kinds, mutation classes, and effect kinds are closed,
   normalized vocabularies. Every currently registered job is inventoried;
   jobs that can create an intent, order, fill/settlement, ledger effect,
   allocation, or provider mutation must declare that class explicitly.
4. First acquisition receives fence `1`. Only an expired, nonterminal
   occurrence may be taken over, which increments the fence. Stale owners
   cannot renew, claim an effect, or complete after takeover.
5. Lease duration and heartbeat bounds are explicit and finite. Cancellation
   follows lost renewal, but context cancellation alone is not the financial
   invariant: every durable financial effect must first claim its deterministic
   effect key under the current database fence.
6. Effect identity is content-addressed from occurrence UUID, effect kind, and
   normalized business key. It is immutable and unique independent of owner or
   attempt. An identical retry/takeover replays; changed payload hash conflicts.
7. OVR-203 bridges derive intent and order idempotency keys from the effect ID;
   settlement bridges derive their durable source/effect identity from it. No
   random retry identity is permitted.
8. Completion is terminal and append-only. A completed occurrence can never be
   reacquired. Failed or lease-lost attempts remain evidence; they do not erase
   already claimed effects or authorize a differently identified replacement.
9. Manual/operator work requires an explicit request UUID and remains distinct
   from a scheduled occurrence. Reusing the request UUID is idempotent.
10. Migration 98 starts empty, is empty-only reversible, installs database-side
    digest/state/fence checks, and grants no scheduler or financial authority.

## Task 1: Domain, catalog, and deterministic effects

- [x] Implement strict job/occurrence/lease/effect value objects and canonical
  identities with semantic retry conflict detection.
- [x] Inventory every automation and strategy/backtest financial job and fail a
  catalog coverage test when registration drifts without classification.
- [x] Implement OVR-203 intent/order and settlement effect-key bridges; prove
  permutation convergence, changed-payload conflicts, timezone/DST stability,
  manual request idempotency, and no random retry identity.
- [x] Commit and push the domain slice after focused race tests.

## Task 2: Schema 98 and fenced PostgreSQL repository

- [x] Persist immutable definitions, occurrences, attempts, renewals, terminal
  outcomes, and effect claims with database-clock lease semantics.
- [x] Prove two-instance acquisition, active-owner exclusion, expiry takeover,
  monotonic fencing, stale-owner rejection, exact effect replay, changed-effect
  conflict, terminal non-reacquisition, restart reconstruction, and append-only
  enforcement in real PostgreSQL.
- [x] Prove injected rollback at every graph stage, eight-writer convergence,
  direct-SQL forgery rejection, nonempty rollback refusal, and empty
  `98 -> 97 -> 98`.
- [x] Commit and push the persistence slice after focused/database races.

## Task 3: Runner qualification and closure

- [x] Add a lease-renewing runner whose job context is cancelled on lost fence
  and whose financial mutation interface requires a current effect claim.
- [x] Qualify two independent runners against exact synthetic OVR-203 intent/
  order and settlement writers; one scheduled occurrence must retain exactly
  one intent, one order, and one settlement effect across races, crash/restart,
  timeout, delayed stale owner, and takeover.
- [x] Add inspection, recovery, takeover, and rollback runbook evidence with
  retained IDs/digests and explicit legacy/cutover limits.
- [x] Run repository-wide backend/static and pinned frontend gates, diff review,
  and isolated kill-switched schema-98 health/API/rollback/backup/restore/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` before OVR-605.

## Acceptance evidence to record

- [x] Two instances cannot create duplicate occurrences, intents, orders, or
  settlements for one scheduled financial job.
- [x] Lease takeover is fenced at the database effect boundary; cancelling a Go
  context is defense in depth, not the source of correctness.
- [x] Every current financial job is classified and every mutation-capable job
  has an explicit deterministic effect contract.
- [x] Local qualification is `VERIFIED_LOCAL`; scheduler cutover, provider
  access, shared migration, real settlement, allocation, broker routing,
  deployment, and live trading remain `BLOCKED_EXTERNAL`.
