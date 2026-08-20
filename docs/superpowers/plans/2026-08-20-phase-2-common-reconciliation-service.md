# Phase 2 Common Reconciliation Service Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep the contract
> test-first, obtain architecture review before implementation and independent
> diff approval before closure, and preserve the local-only activation boundary.

**Goal:** Complete OVR-207 with one provider-neutral, exact, read-only
reconciliation service that compares stable external venue snapshots with the
OVR-104 ledger projection and OVR-203/205 lifecycle evidence. Every cash,
fill, or position discrepancy becomes immutable evidence and an incident;
nothing silently edits orders, fills, positions, cash, or ledger postings.

**Architecture:** Add `internal/venuerecon` as the sole new comparison
authority. A fixed, content-addressed policy defines the supported providers,
snapshot stability protocol, exact comparison vocabulary, and severity. A
provider reader performs two complete read-only captures and admits a snapshot
only when both canonical provider-state digests match. Each capture retains
raw-page digests, provider cursors, source/update times, account cash and equity,
positions resolved through dated OVR-201 venue contracts, and fills resolved
through OVR-205 source identities. In one PostgreSQL `REPEATABLE READ`,
read-only transaction, the local reader rebuilds and exactly matches one
verified OVR-104 projection checkpoint, retains its complete included ledger
transaction set, and loads only OVR-203/205 fill evidence whose normalization
transaction is in that set. The pure comparer unions cash, fills, and positions,
emits exact canonical results, and creates deterministic immutable incidents for
every discrepancy or non-comparable fact. Migration 75 stores policy artifacts,
stable provider snapshots, local snapshots, runs, results, and incidents as an
append-only graph with independent PostgreSQL reconstruction. It grants no
writer, selects no current policy, schedules no work, performs no provider
mutation, and has no correction callback.

**Tech stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256, canonical
JSON, PostgreSQL 17/TimescaleDB, pgx v5, golang-migrate, Task, and TDD against
an isolated schema-74 database upgraded to schema 75.

---

## Scope and sequencing

This plan covers OVR-207 only. It depends on OVR-104's exact portfolio
projection and OVR-205's exact venue-adapter evidence. It consumes OVR-201
canonical instrument and dated venue-contract identity, OVR-203 lifecycle
aggregates, and OVR-102/103 ledger normalization. It does not replace the
OVR-105 legacy-versus-ledger dual-run, which has a different source boundary.
OVR-605 may later schedule this service only after OVR-604 supplies a leased,
idempotent financial scheduler and protected-environment identities exist.

In scope:

- one fixed canonical reconciliation-policy artifact with explicit provider,
  evidence, comparison, severity, and snapshot-stability vocabulary;
- Alpaca and Kalshi read-only snapshot adapters over captured synthetic wire
  fixtures, with exact decimals and complete page/cursor evidence;
- a two-capture stability protocol that rejects changing provider state and
  never compares a torn external snapshot as if it were authoritative;
- provider account cash/equity, complete open positions, and complete fills for
  the declared account/venue namespace and requested reconciliation horizon;
- explicit coverage windows, capture start/end, provider update times, raw page
  SHA-256 values, and a canonical provider-state digest;
- dated provider-contract resolution to canonical instrument identity, with
  ambiguous, missing, retired, duplicated, or mismatched mappings failing
  closed as incidents rather than ticker inference;
- a local snapshot pinned to one validated OVR-104 checkpoint, its attestation
  metadata, exact totals and open positions, plus the exact included ledger
  transaction IDs and complete matching OVR-203/205 fill graph captured in the
  same repeatable-read transaction;
- exact union comparison for cash, positions, and fills, including missing on
  either side and mismatched quantity, price, fee, currency, account, provider,
  instrument, or source identity;
- deterministic run, result, and incident identities; stable canonical bytes,
  hashes, reason codes, severity, and retry equality;
- distinct `matched`, `drift`, and `not_comparable` results. Missing evidence is
  never zero and provider-unavailable is never matched;
- one incident per canonical discrepancy, with identical reruns converging and
  changed evidence producing a new immutable run/incident graph;
- append-only schema-75 storage with independent SQL validation of all parent,
  child, count, identity, bytes, and hash relationships;
- concurrency, interruption, restart/reload, empty-only rollback, migration
  serialization, and real PostgreSQL qualification;
- a runbook for inspection and response that preserves raw evidence and forbids
  ad hoc balancing entries or broker/local mutation;
- focused and repository-wide verification, kill-switched local startup,
  independent review, reviewable commits, push/fetch, and hash convergence.

Out of scope:

- shared or production migration, deployment, scheduler activation, runtime
  cutover, credentials, real-provider calls, external orders, or account writes;
- automatic correction, balancing entries, position edits, fill synthesis,
  cancellation, liquidation, deposit/withdrawal, or provider mutation;
- treating the provider as an accounting ledger or replacing OVR-104 as the
  local economic source of truth;
- claiming provider snapshots are atomic when the API does not supply an atomic
  snapshot token; unstable double reads remain explicit evidence and incidents;
- ticker parsing, current-only alias lookup for historical fills, float
  tolerance, netting unexplained errors, or offsetting independent discrepancies;
- reconciliation across accounts, currencies, providers, subaccounts, or
  evidence namespaces;
- legacy Polymarket reconciliation, old mutable position tables, or the
  OVR-105 legacy-accounting comparison;
- incident acknowledgement/resolution workflow, alert delivery, dashboard/API
  exposure, or autonomous supervision before later milestones;
- treating a clean synthetic/local run as protected staging, production, or
  real-broker reconciliation evidence.

## Contract decisions

### Policy and evidence identity

1. `venue-reconciliation-policy-v1` is a fixed schema. It names exactly
   `alpaca` and `kalshi`, requires two equal captures, exact decimal equality,
   complete pagination, complete fill coverage, canonical venue-contract
   identity, and immutable incident creation for every mismatch.
2. Canonical policy bytes contain only fixed structs, sorted arrays, normalized
   enum strings, integer limits, durations, and booleans. There are no maps,
   floating-point values, process time, environment variables, or hidden
   defaults.
3. The policy version is
   `venue-reconciliation-policy-v1@sha256:<lowercase digest>`. Its deterministic
   artifact UUID uses the existing length-prefixed `economicid` function and
   domain `venue-reconciliation-policy-artifact`.
4. PostgreSQL independently reconstructs the fixed policy bytes and rejects
   forged JSON, digest, version, UUID, duplicate/missing provider definitions,
   reordered arrays, unknown fields, or changed values.
5. There is no current-policy pointer. Every run pins the exact policy version.

### Stable provider snapshot

6. One capture contains account identity, provider, venue/subaccount namespace,
   requested horizon start/end, capture start/end, provider source/update times,
   exact cash/equity, every open position, every fill in the horizon, pagination
   termination evidence, and the ordered raw-page hashes.
7. A normalized position requires provider contract identity, signed exact
   quantity, canonical instrument/venue-contract identity, and currency. A
   normalized fill additionally requires provider fill ID, external and client
   order IDs, source revision/time, side, exact quantity, price, and fee.
8. Raw provider pages remain byte-preserved in the existing OVR-205 observation
   vocabulary where order-scoped, or in schema-75 snapshot-page evidence where
   account-scoped. Parsed facts never replace raw bytes.
9. The reader performs two complete captures. The provider-state digest excludes
   local receive times but includes every provider fact, cursor, page hash,
   account value, position, and fill. Only equal digests produce a stable
   snapshot. Unequal captures produce `snapshot_unstable` evidence and no cash,
   fill, or position match claim.
10. Duplicate provider IDs, repeated cursors, incomplete pagination, malformed
    numbers, contradictory rows, facts outside the declared namespace/horizon,
    or changing state fail closed. Empty collections are valid only with a
    complete terminal-page proof.

### Local snapshot

11. One local snapshot is account/provider scoped and is built inside one
    PostgreSQL `REPEATABLE READ`, read-only transaction. Within that transaction
    the source rebuilds OVR-104 from eligible immutable ledger rows, loads the
    resulting checkpoint, and requires exact checkpoint ID, canonical bytes,
    input/output checksums, transaction count, and `ThroughTransactionID`
    agreement. It retains attestation metadata without claiming the database
    owner is outside the OVR-104 trust boundary.
12. Local cash and positions come only from that exact projection. The snapshot
    also retains the complete sorted set of ledger transaction IDs that entered
    the rebuild. A lifecycle fill is eligible only when its OVR-103
    normalization transaction is a member of that exact set; `as_of`, checkpoint
    creation time, or UUID ordering alone can never establish membership. The
    source loads the complete OVR-203/205 graph for those fills in the same
    database snapshot.
13. Every included fill must retain provider fill source identity, canonical
    instrument, exact quantity/price/fee, external/client order identity, and
    normalization/transaction IDs. A provider fill observation without its
    complete lifecycle and normalization is `local_fill_incomplete`, not absent.
14. A ledger transaction in the checkpoint without its required complete
    lifecycle/venue graph is `local_fill_incomplete`. A lifecycle fill outside
    the checkpoint transaction set is excluded from economic comparison and
    retained as `local_fill_after_frontier`; it cannot be mixed into the pinned
    cash/position state. Facts after `as_of`, from another account/provider/
    namespace, or lacking a canonical venue binding cannot enter local values or
    hashes.
15. The local source owns the single repeatable-read transaction and returns the
    projection, exact transaction membership, fills, observations,
    normalizations, and correction/bust events together. Composing separate
    repository reads for one snapshot is forbidden.

### Comparison and incidents

16. The comparer validates both snapshots and policy before reading any mutable
    field. It requires equal account, provider, currency, namespace, horizon,
    and a stable provider snapshot covering the local boundary.
17. Result kinds are `cash`, `position`, `fill`, and `snapshot`. Status is
    exactly `matched`, `drift`, or `not_comparable`. Each result carries both
    optional exact values, an optional exact delta only when meaningful, and one
    closed reason code.
18. Cash compares provider cash with projection cash. Provider equity is retained
    as context and may receive its own comparison only when its mark basis and
    coverage are provably equivalent; otherwise it is explicitly
    `not_comparable`, never silently equated to local equity.
19. Position comparison unions canonical instrument IDs. Missing provider/local
    rows remain missing, not zero. Exact signed quantity equality is required.
20. Ordinary fill comparison unions `(provider, authoritative fill namespace,
    provider fill source ID)`. Quantity, economic price, fee, instrument, side,
    external order, and client order identity must all agree.
21. Correction/bust evidence uses a separate key:
    `(provider, authoritative fill namespace, original provider fill source ID,
    observation class, observation discriminator)`. Provider normalization must
    retain the original-fill link. The local side must match it to OVR-203's
    `OriginalSourceEventID`, `OriginalFillID`, class, and discriminator. A later
    revision event ID is never an ordinary-fill key and never replaces the
    original fill.
22. OVR-203/205 intentionally record correction/bust as
    `failed_reconciliation` without a second economic effect. Matching revision
    evidence therefore remains a non-clean `correction_pending` or
    `bust_pending` incident until a separately reviewed accounting-correction
    workflow exists; it cannot prove corrected economics by itself.
23. Every `drift` or `not_comparable` result produces one deterministic incident
    linked to the run/result. Severity is policy-derived and never supplied by
    a caller. Cash, position, and fill economic drift are `critical`; unstable,
    incomplete, unsupported, and mapping failures are `high`.
24. A matched run has zero incidents. Any incident makes the run non-clean. One
    mismatch cannot be netted against another and no tolerance applies.
25. Persisting evidence has no callback capable of editing provider or local
    state. The package has no broker mutation interface, ledger writer, capital
    flow writer, or mutable position repository.

### Persistence and rollback

26. Migration 75 creates immutable policy, provider snapshot/page/position/fill,
    local snapshot/position/fill, run/result, and incident tables. Child counts
    are declared and verified by deferred constraints at commit.
27. Deterministic IDs and canonical bytes/hashes are reconstructed in SQL. Parent
    account/provider/policy/namespace/horizon facts are copied into children and
    must agree independently.
28. Registration and run persistence are atomic and idempotent. An identical
    retry converges; reused identity with changed bytes conflicts; partial
    failures leave no graph; concurrent writers converge to one graph.
29. All evidence is append-only. Update/delete fails. Public grants are revoked,
    and migration 75 creates no runtime writer role or security-definer write
    function.
30. The down migration takes the complete lock set before checking emptiness and
    refuses while any schema-75 evidence exists. Empty `75 -> 74 -> 75` must
    preserve schema 74 exactly.

## Expected files

- Create `internal/venuerecon/policy.go` and tests.
- Create `internal/venuerecon/provider_snapshot.go` and tests.
- Create `internal/venuerecon/local_snapshot.go` and tests.
- Create `internal/venuerecon/compare.go` and tests.
- Create `internal/venuerecon/runner.go` and tests.
- Create `internal/venuerecon/alpaca.go` and `kalshi.go` with captured-fixture
  contract tests; these adapters remain read-only and unwired at runtime.
- Create `internal/repository/postgres/venue_reconciliation.go` and tests.
- Extend `internal/repository/interfaces.go` with one narrow reconciliation
  repository interface.
- Create migration 75 up/down SQL and migration tests; bump required schema only
  after isolated database qualification passes.
- Create `docs/runbooks/venue-reconciliation.md`, update the runbook index,
  ADR-018, and the total overhaul plan after implementation evidence exists.

## Task 1: RED policy and canonical evidence vocabulary

- [x] Test the exact fixed policy, providers, stability rule, comparison kinds,
  statuses, reasons, severities, canonical ordering, digest, version, UUID,
  defensive copies, and round-trip decode.
- [x] Reject missing, extra, duplicate, unknown, reordered, malformed, overlong,
  unbounded, or caller-defaulted policy facts.
- [x] Implement the minimum immutable policy artifact and closed enums.

Run:

```bash
go test -count=1 ./internal/venuerecon -run 'Policy|Vocabulary'
```

## Task 2: RED provider snapshot and stability protocol

- [x] Test exact Alpaca and Kalshi account/position/fill fixture normalization,
  canonical contract resolution, raw page preservation, pagination completion,
  and deterministic snapshot bytes/hash/identity.
- [x] Test duplicate IDs/cursors, malformed decimals/times, ambiguous contracts,
  namespace/account/provider drift, incomplete pages, unsupported facts, and
  changed bytes under reused source identity fail closed.
- [x] Test two identical captures admit one stable snapshot while any cash,
  equity, position, fill, cursor, raw page, revision, or source-time change
  returns `snapshot_unstable` evidence and no comparable economic snapshot.
- [x] Implement provider-neutral capture types, pure adapter normalization, and
  a read-only double-capture coordinator with no mutation interface.

Run:

```bash
go test -race -count=1 ./internal/venuerecon -run 'Provider|Snapshot|Stable|Alpaca|Kalshi'
```

## Task 3: RED local snapshot

- [x] Test exact cash/open-position derivation from an independently validated
  OVR-104 checkpoint and exact fill derivation from complete OVR-203/205 graphs.
- [x] Test exact checkpoint transaction membership, correction/bust provenance,
  fees, canonical instrument identity, account/provider/horizon isolation,
  deterministic ordering, bytes, and hash.
- [x] Reject incomplete lifecycle/normalization, unmatched observations,
  unsupported instruments, projection checksum drift, facts after `as_of`, and
  every caller-authored total or identity mismatch.
- [x] Prove a lifecycle fill committed after the checkpoint, or merely satisfying
  `as_of`, cannot enter unless its exact ledger transaction is in the rebuilt
  checkpoint set. Prove projection and local graph capture share one
  repeatable-read transaction.
- [x] Implement the pure local snapshot builder and one transaction-owning,
  read-only source interface.

Run:

```bash
go test -race -count=1 ./internal/venuerecon -run 'Local|Projection|Lifecycle'
```

## Task 4: RED comparison, incident, and runner contract

- [x] Test empty and nonempty exact matches for cash, positions, and fills with
  zero incidents and stable canonical run evidence.
- [x] Test every missing-side and mismatched cash/quantity/price/fee/currency/
  side/instrument/order/source fact produces the exact result and incident.
- [x] Test unstable/unavailable/incomplete snapshots, non-equivalent equity mark
  basis, and unsupported facts remain `not_comparable`, never matched or zero.
- [x] Test correction/bust links use original source/fill identity plus class and
  discriminator, never a revision event as an ordinary fill, and always remain
  explicit non-clean pending incidents without new economics.
- [x] Test one discrepancy cannot offset another, changed cents change identity,
  retry convergence, deterministic ordering, and no partial clean result.
- [x] Prove the runner has no write-capable provider, ledger, capital-flow,
  position, order, or fill dependency and invokes no correction callback.
- [x] Implement the pure comparer, incident builder, and orchestration runner.

Run:

```bash
go test -race -count=1 ./internal/venuerecon -run 'Compare|Incident|Runner|ReadOnly'
```

## Task 5: Migration 75 and PostgreSQL repository

- [x] Add source-shape RED tests for complete tables/constraints/lock sets,
  canonical reconstruction, parent/child agreement, append-only triggers, no
  grants or activation, and empty-only rollback.
- [x] Add direct PostgreSQL attacks for forged policy/snapshot/run/result/
  incident bytes, JSON, hashes, IDs, counts, optional values, severity, copied
  facts, child omission/duplication, mutation, deletion, and orphan insertion.
- [x] Add repository tests for exact register/load/reconstruct/run reload,
  identical retry, changed-payload conflicts, crash rollback, eight-writer
  convergence, and restart without duplicate incidents.
- [x] Race migration up with a snapshot/run attempt and migration down with an
  evidence insert; prove serialization and no orphan facts.
- [x] Prove nonempty downgrade refusal and empty `75 -> 74 -> 75` preservation.
- [x] Bump `RequiredSchemaVersion` only after isolated suites pass.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'VenueRecon|VenueReconciliation|Migration75' \
  ./internal/repository/postgres ./migrations
```

## Task 6: Golden reconciliation and restart campaign

- [x] Build one Alpaca and one Kalshi canonical graph from raw provider fixtures
  through OVR-205 observation, OVR-203 lifecycle, OVR-103 normalization/ledger,
  OVR-104 projection, stable provider/local snapshots, and a clean run.
- [x] Perturb cash, one position, and one fill independently and prove exact
  critical incidents without any mutation or balancing transaction.
- [x] Inject failure after each persisted graph stage and prove restart/replay
  converges to the same run and incident identities.
- [x] Prove correction/bust evidence links exactly to the original fill and stays
  non-clean without deleting that fill or inventing corrected economics;
  ambiguous/unstable provider state cannot produce a clean run.

Run:

```bash
go test -race -count=1 ./internal/venuerecon ./internal/execution/...
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'VenueReconciliationGolden|VenueReconciliationRestart' \
  ./internal/repository/postgres
```

## Task 7: Qualification, documentation, review, and synchronization

- [ ] Document policy, snapshot stability, comparison/reason semantics, read-only
  inspection SQL, incident response, preservation rules, empty-only rollback,
  approximation limits, and the no-cutover boundary.
- [ ] Apply migrations `1 -> 75` to dedicated loopback-only PostgreSQL; retain
  one policy, one stable Alpaca run, one stable Kalshi run, and explicit drift
  incidents; reload and recompute all evidence; prove nonempty rollback refusal.
- [ ] In a separate empty database, prove `75 -> 74 -> 75`.
- [ ] Run focused races and repository-wide gates:

  ```bash
  go test -race -count=1 ./internal/venuerecon ./internal/execution/...
  DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
    -run 'VenueRecon|VenueReconciliation|Migration75' \
    ./internal/repository/postgres ./migrations
  task test:race
  task build
  task vet
  task lint
  task fmt:check
  govulncheck ./...
  ```

- [ ] Run pinned Node 22 install/audit/test/lint/build; report the residual
  low-severity transitive Windows development-server advisory separately.
- [ ] Start the rebuilt binary with the global kill switch active, live trading
  and schedulers false, no provider credentials, isolated schema-75 PostgreSQL,
  and isolated Redis. Check all health routes, stop cleanly, and prove retained
  evidence unchanged.
- [ ] Obtain independent final diff approval with no unresolved P0/P1.
- [ ] Commit verified slices, push `codex/augr-overhaul`, fetch, prove local and
  remote hashes equal with `0 0` divergence, then begin OVR-301.

## Acceptance evidence to record after implementation

- [ ] Stable external snapshot admission requires two byte/evidence-equivalent
  complete reads; changing or incomplete provider state cannot be called clean.
- [ ] Exact cash, fill, and position agreement produces deterministic matched
  results with zero incidents for both Alpaca and Kalshi fixtures.
- [ ] Every drift or non-comparable fact produces immutable run/result/incident
  evidence and never silently mutates provider or local economic state.
- [ ] Provider fills reconcile to exact OVR-205 observations, OVR-203 lifecycle,
  OVR-103 normalization, and exact OVR-104 checkpoint transaction membership.
- [ ] Correction/bust evidence keys through the original fill relationship and
  remains an incident until a separately reviewed correction workflow exists.
- [ ] Migration 75 is additive, append-only, empty-only reversible, and activates
  no runtime path, grant, scheduler, correction, or current-policy pointer.
- [ ] Focused races, real PostgreSQL, full backend/frontend gates, kill-switched
  startup, independent review, commits, push, and synchronization are recorded
  honestly with local synthetic evidence separated from external qualification.
