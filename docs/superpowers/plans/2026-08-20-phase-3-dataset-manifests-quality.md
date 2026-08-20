# Phase 3 Dataset Manifests and Quality Service Implementation Plan

**Goal:** Complete OVR-301 with immutable, content-addressed dataset manifests
and deterministic quality results so every future experiment can pin exact
point-in-time inputs without promoting mutable legacy caches into research
evidence.

**Architecture:** Add an `internal/dataset` bounded context and additive
migration 76. A manifest owns ordered source partitions and ordered observation
facts. Every fact preserves effective, published, observed, and available-at
time separately; exact content hashes, query/file hashes, symbology,
adjustment, calendar, timezone, revision/correction lineage, licensing, and
retention stay in canonical evidence. A pure quality evaluator applies one
fixed reviewed policy to the manifest and emits immutable per-check findings.
PostgreSQL independently reconstructs all canonical graphs. Nothing in this
slice starts an experiment, changes a legacy backtest, writes market data,
contacts a provider, or activates a scheduler.

**Migration:** `000076_dataset_manifests_quality`

## Scope and non-goals

In scope:

- exact manifests for bars, quotes, depth, corporate actions, fundamentals,
  benchmark membership, option chains/contracts, filings, prediction-market
  books/trades/fees/rules/resolutions, and generic external objects;
- one explicit decision cutoff and no-lookahead validation;
- source request/query or file identity, exact content digest, row count,
  symbology version, adjustment policy, timezone/calendar, revision lineage,
  license, and retention facts;
- deterministic checks for uniqueness, ordering, time consistency, availability,
  session coverage, bid/ask, nonnegative volume/depth, instrument validity, and
  declared cross-provider/corporate-action checks;
- quarantine for material defects or required checks that were not assessed;
- append-only persistence, exact replay, restart convergence, and empty-only
  rollback.

Out of scope:

- changing current `historical_ohlcv`, provider caches, quote ingestion, or
  backtest reads;
- trusting current float OHLCV rows as exact or inventing historical
  `available_at` timestamps;
- downloading data, implementing a scheduler, or contacting a provider;
- strategy families, experiments, deployments, or mandatory experiment foreign
  keys (OVR-302);
- the reproducible experiment runner (OVR-303), evaluation statistics
  (OVR-304/305), promotion (OVR-306), or provider cost attribution (OVR-606);
- production/shared migration, legacy backfill, cutover, or deletion.

## Locked contracts

1. A manifest is immutable canonical JSON with a deterministic UUID and SHA-256.
2. A manifest contains at least one partition and one observation. Partition
   sequence is contiguous from zero; observation sequence is contiguous within
   its partition.
3. Each partition records a bounded kind, provider, source, namespace,
   request/query or file SHA-256, aggregate content SHA-256, media type,
   effective window, observed/available bounds, symbology version, adjustment
   policy, timezone, calendar, revision/correction lineage, row count, license,
   and retention policy.
4. Each observation records a stable source key, optional canonical instrument,
   effective time, optional publication time, observed time, available-at time,
   revision, optional correction target, exact content SHA-256, and typed
   optional bid/ask/volume/depth facts as canonical decimal strings.
5. `published_at <= observed_at <= available_at <= decision_cutoff`; effective
   time is never substituted for availability. Unknown availability is invalid
   for experiment-grade evidence.
6. Partition aggregate time bounds, row counts, and content digest are derived
   from the ordered observations, never caller-authored independently.
7. Source keys are unique within `(provider, namespace, revision)`. Corrections
   must point to a prior observation in the same partition and cannot form
   chains or cycles that hide the original fact.
8. A fixed reviewed quality policy is itself content-addressed. A quality result
   pins both manifest and policy versions and has a deterministic identity.
9. Required checks are `passed`, `failed`, or `not_assessed`. Any failed or
   required-not-assessed check quarantines the manifest; no silent repair exists.
10. A quality result contains stable issue codes and evidence only; it cannot
    mutate the manifest or source data.
11. Database tables are append-only, child rows repeat parent scope, deferred
    constraints reconstruct every count/hash/canonical graph, and nonempty
    rollback refuses.
12. Existing historical caches and legacy backtests are not backfilled and remain
    visibly unpinned until OVR-302/303 add an explicit adapter and experiment
    contract.

## Task 1: Domain vocabulary and fixed quality policy

- [x] Add `internal/dataset/policy.go` with bounded kinds, check/status/severity
  codes, fixed v1 policy construction, canonical bytes, digest, deterministic
  ID, artifact construction, and restore/validate helpers.
- [x] Lock the exact policy bytes in tests. Include required checks per dataset
  kind and a stable definition of quarantine.
- [x] Reject unknown kinds/checks/statuses, duplicate policy rows, mutable
  canonical state, non-UTC/non-microsecond timestamps, and changed-version
  reuse.
- [ ] Commit and push the policy slice after focused race tests.

## Task 2: Immutable point-in-time manifest graph

- [x] Add `internal/dataset/manifest.go` with typed partition/observation inputs,
  canonical output, deterministic sorting/identity, clone-safe getters, restore,
  and validation.
- [x] Derive partition row count, content hash, and min/max temporal facts from
  observations. Use domain-separated length-prefixed hashing so concatenation
  cannot collide.
- [x] Enforce exact decimal grammar and reject NaN/Inf/exponent notation,
  crossed quotes, negative volume/depth, invalid correction lineage, duplicate
  source identities, noncontiguous sequence, invalid instrument IDs, and all
  lookahead/time-order violations.
- [x] Add deterministic reordering, tamper, boundary-time, correction, mixed-kind,
  and legacy-unavailable regression tests.
- [ ] Commit and push the manifest slice after focused race tests.

## Task 3: Deterministic quality evaluator

- [ ] Add `internal/dataset/quality.go`. Evaluate one validated manifest against
  one exact policy and injected expected-session plus instrument-validity facts.
- [ ] Compute uniqueness, monotonic time, no-lookahead, missing-session,
  bid/ask, nonnegative volume/depth, identifier-window, declared corporate-action
  reconciliation, and declared provider-spot-comparison checks.
- [ ] Treat missing required external comparison/session/instrument evidence as
  `not_assessed`, never pass. Material failures and required not-assessed checks
  set `quarantined=true`.
- [ ] Produce deterministic check results/findings, canonical bytes, digest,
  UUID, and restore/validate helpers. Prove input reordering does not change
  output and tampering cannot persist.
- [ ] Commit and push the evaluator slice after focused race tests.

## Task 4: Migration 76 append-only evidence graph

- [ ] Add `000076_dataset_manifests_quality.up.sql` with policy artifacts,
  manifests, manifest partitions, observations, quality results, check results,
  and findings.
- [ ] Repeat parent scope on every child and use deferred constraint triggers to
  reconstruct contiguous sequences, derived bounds/counts, canonical JSON,
  digests, deterministic IDs, check/finding counts, and quarantine state.
- [ ] Add append-only mutation rejection, source/instrument foreign keys where
  authoritative, exact numeric bounds, canonical UTC microseconds, and indexes
  for manifest/policy lookup. Do not grant writers or create current pointers.
- [ ] Add an empty-only down migration that locks first, refuses any evidence,
  and leaves all legacy data/instrument/quote tables untouched.
- [ ] Add migration source and real PostgreSQL tests for direct-SQL forged IDs,
  child scope, counts, time/lookahead, decimal facts, correction lineage,
  canonical bytes, mutation, nonempty rollback, and empty `76 -> 75 -> 76`.
- [ ] Bump `RequiredSchemaVersion` only after the isolated migration suite passes.
- [ ] Commit and push the migration slice after focused race tests.

## Task 5: PostgreSQL repository and restart replay

- [ ] Add `internal/repository/postgres/dataset.go` for atomic policy, manifest,
  and quality-result persistence plus exact reload.
- [ ] Identical retries converge; changed payload under the same identity returns
  `ErrIdempotencyConflict`; concurrent writers produce one complete graph.
- [ ] Inject failures after each parent/child stage and prove no partial graph
  commits. Reload must revalidate canonical bytes and every relational child.
- [ ] Add one retained golden graph covering bars, quotes, a correction, session
  facts, instrument validity, a clean result, and one separately quarantined
  perturbation. Restart/recompute must reproduce all IDs and hashes.
- [ ] Commit and push the repository slice after focused real-PostgreSQL races.

## Task 6: Documentation, qualification, review, and synchronization

- [ ] Add a runbook covering manifest construction, exact inspection SQL,
  quality codes, quarantine response, preservation, license/retention facts,
  legacy-unpinned boundaries, empty-only rollback, and no-cutover status.
- [ ] Apply migrations `1 -> 76` to a fresh loopback-only database, retain the
  golden policy/manifests/results, reload/recompute, and prove nonempty rollback
  refusal. Prove empty `76 -> 75 -> 76` in a separate database.
- [ ] Run focused race suites, repository-wide backend tests/build/vet/lint/
  format/vulnerability gates, pinned Node 22 frontend install/audit/test/lint/
  build, and `git diff --check`.
- [ ] Run only a kill-switched isolated local health smoke with live trading and
  schedulers false, no provider credentials, schema-76 PostgreSQL, and isolated
  Redis; prove evidence unchanged and stop cleanly.
- [ ] Obtain independent final diff approval with no unresolved P0/P1.
- [ ] Commit verified slices, push, fetch, and prove `0 0` divergence before
  beginning OVR-302.

## Acceptance evidence to record after implementation

- [ ] Every manifest and quality result is deterministic, immutable,
  content-addressed, and reconstructable in Go and PostgreSQL.
- [ ] Effective, published, observed, and available-at time remain distinct;
  future-available or unknown-availability evidence cannot pass.
- [ ] Required uniqueness, ordering, coverage, quote, quantity, instrument,
  corporate-action, and provider-comparison checks pass, fail, or remain
  explicitly not assessed; material defects quarantine rather than repair.
- [ ] Legacy float/mutable caches are not promoted or backfilled, and no current
  experiment/runtime path is activated.
- [ ] Migration 76 is additive, append-only, empty-only reversible, creates no
  writer grant/current pointer, and preserves every prior table.
- [ ] Focused races, real PostgreSQL, full backend/frontend gates, kill-switched
  startup, independent review, commits, push, and synchronization are recorded
  with `VERIFIED_LOCAL` separated from external qualification.
