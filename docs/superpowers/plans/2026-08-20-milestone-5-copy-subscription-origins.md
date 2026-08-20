# Milestone 5 Copy-Subscription Origins Plan

**Goal:** Complete OVR-501 by making the copy subscription itself the stable
execution origin, eliminating one mutable legacy strategy row per subscription
while preserving exact subscription attribution through previews, intents,
runs, orders, fills, ledger events, retries, and restart.

**Architecture:** Add an origin-native copy contract and additive migration 88.
Every subscription owns exactly one identity
`copy_subscription/<subscription UUID>`; it is not a strategy version and is
never entered into either the legacy `strategies` registry or the immutable
OVR-302 strategy catalog. Existing per-subscription backing strategies become
explicit nullable legacy references for read compatibility only. New writes
derive their origin in trusted code and PostgreSQL, never from request JSON.
Copy rebalance runs and intents carry the same origin, and execution adapters
must pass it into the OVR-203 common lifecycle. Normalized rows reconstruct the
origin graph and reject cross-subscription attribution.

**Migration:** `000088_copy_subscription_origins`

## Scope and non-goals

In scope:

- deterministic subscription identity as `copy_subscription` origin;
- origin-native creation with no strategy-registry write;
- explicit legacy backing-strategy compatibility, inventory, and retirement
  state without deleting historical strategies;
- subscription, run, intent, and common-lifecycle origin agreement;
- restart/idempotency, concurrent creation, append-only historical attribution,
  retained local qualification, and empty-only rollback;
- API compatibility that reports legacy strategy identity only when one exists.

Out of scope:

- changing quote freshness/spread/session policy (OVR-502), target drift
  convergence (OVR-503), 13F replay (OVR-504), strategy promotion, live
  trading, deleting historical registry rows, mutating production data, or
  treating a copy subscription as an OVR-302 strategy version.

## Locked contracts

1. Origin type is exactly `copy_subscription`; origin ID is exactly the
   subscription UUID. Callers cannot choose or override either value.
2. Creating N subscriptions creates zero strategy rows. No shared or per-user
   synthetic strategy is required for attribution.
3. A nullable legacy strategy ID may describe a pre-migration row, but it is
   never copied into origin ID, never created for a new subscription, and
   never required to preview or stop the subscription.
4. Every copy run and intent repeats the subscription origin. PostgreSQL
   verifies the parent subscription, source observation, and origin tuple in
   the same transaction.
5. Any common execution intent created from a copy intent uses origin type
   `copy_subscription`, origin ID equal to subscription ID, and empty strategy
   version ID. Cross-origin orders, fills, or ledger events fail closed.
6. Identical retries converge. A reused subscription, run, or intent identity
   with changed leader/source/policy/origin content is an idempotency conflict.
7. Attribution history is append-only. Policy/status lifecycle fields may
   advance through their existing guarded transitions, but origin, parent,
   source observation, and canonical economic input cannot be rewritten.
8. Legacy rows are migrated losslessly and remain inspectable. Historical
   strategy rows are not deleted automatically; retirement is an explicit
   inventory result.
9. The feature remains paper-only and grants no promotion, scheduling,
   deployment, provider, broker, or live-execution authority.
10. Migration 88 is additive/lock-first and rolls back only when no
    origin-native evidence exists.

## Task 1: Domain and service origin contract

- [x] Add a closed copy-origin value object with exact canonical restoration.
- [x] Make subscription identity caller-independent and expose nullable legacy
  strategy attribution separately.
- [x] Remove strategy creation/status mirroring from new subscription flows.
- [x] Prove N subscriptions create no strategies and cannot accept forged
  origin or strategy-version attribution.
- [x] Commit and push the focused domain/service slice.

## Task 2: Origin-native runs and execution handoff

- [x] Add copy rebalance run identity independent of legacy pipeline strategy.
- [x] Bind previews, runs, and intents to one subscription origin and source
  observation.
- [x] Hand approved intents to OVR-203 with `copy_subscription` origin and no
  strategy version; preserve paper-only risk and failure behavior.
- [x] Prove concurrent retry, restart, partial execution, rejection, and
  cross-subscription forgery edges.
- [x] Commit and push the execution-handoff slice.

## Task 3: Migration 88 and append-only reconstruction

- [x] Add/backfill subscription origins and explicit nullable legacy strategy
  references without deleting history.
- [x] Add normalized copy-origin run/intent attribution and cross-table guards.
- [x] Reconstruct the complete subscription-to-execution attribution graph and
  reject mutation, deletion, changed retry, partial write, and normalized
  forgery.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 88 only after
  real PostgreSQL migration races pass.
- [x] Commit and push the persistence slice.

## Task 4: Operations and qualification

- [x] Add a runbook for origin inspection, legacy inventory, replay,
  reconciliation, failure recovery, and rollback.
- [x] Retain multiple origin-native subscriptions and at least one complete
  paper attribution graph with exact IDs, hashes, and counts.
- [x] Prove eight-writer convergence, restart, every-stage rollback,
  append-only behavior, legacy preservation, zero registry growth, and empty
  `88 -> 87 -> 88`.
- [x] Run focused/database races, all backend and pinned frontend gates, diff
  review, and isolated kill-switched schema-88 health/API/rollback/reapply.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-502.

## Acceptance evidence to record

- [x] Subscription attribution remains exact across every retained layer.
- [x] New subscription count can grow while strategy-registry counts do not.
- [x] Legacy attribution remains readable without authorizing new legacy
  backing strategies.
- [x] Missing/revised/partial/forged origin evidence fails closed.
- [x] Local qualification is `VERIFIED_LOCAL`; shared migration, historical
  cleanup, independent review, runtime adoption, deployment, and live trading
  remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

OVR-501 is **VERIFIED_LOCAL**. The application behavior qualified in the
isolated production image is commit
`7ec15c386c85615e48de04e53805b684dbe81006`; the subsequent
`651cfdfe9a5cc7a224b65d0ebea62c1d23309da9` commit expands only retained test
coverage to a second subscription. Clean retained database
`augr_ovr501_qual_20260820_v3` is schema 88 and contains 2 origin-native
subscriptions, 2 copy intents, 1 canonical rebalance run, 2 normalized run
intent rows, and 0 strategy rows. The complete retained graph is rooted at
subscription/origin `4b9ae278-021a-4847-a3a8-075b19876641`; run
`4e7f0d9a-b0d7-d634-1fec-43f4a2531c51` has SHA-256
`178ba50ed49364cc9b080c81a1505920058df57cc7e7e5bdeec70f60435f34cf`.

Eight identical writers converge after restart. Injected failure after either
the run or normalized-intent stage leaves zero run graph rows. Cross-parent
origin/source/calculation mismatches fail at trusted Go and PostgreSQL
boundaries; mutation fails with `copy origin evidence is append-only`.
Nonempty rollback refuses origin-native evidence, while the separate
`augr_ovr501_empty_20260820` database completed clean `88 -> 87 -> 88`.

Full backend qualification passed 4,574 race tests in 117 packages, build,
vet, repository-wide lint, gofumpt, and vulnerability scanning with no called
symbol vulnerabilities. Pinned Node 22.23.2 frontend qualification passed
frozen script-free installation, no known dependency vulnerabilities, 162
tests, lint, and production build. The isolated production verifier passed
fresh migration `1 -> 88`, authenticated read-only API smoke, lossless
`88 -> 60`, schema-60 backup/restore, reapply `61 -> 88`, and post-reapply
health.

Origin-native runs remain prepared until OVR-502 supplies a fresh executable
quote and valid session to the common-lifecycle proposal adapter. Historical
strategy deletion, shared migration, independent review, runtime adoption,
deployment, provider/broker routing, and live trading remain
**BLOCKED_EXTERNAL**.
