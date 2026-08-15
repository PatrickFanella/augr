---
title: "Phase 1: accounting dual-run and cutover evidence"
description: "OVR-105 plan for exact legacy-versus-ledger comparison, immutable classifications, and a fail-closed parity window."
status: "verified-local-blocked-external"
updated: "2026-08-15"
tags: [phase-1, accounting, reconciliation, ledger, cutover]
---

# Phase 1 accounting dual-run implementation plan

> **Execution rule:** implement test-first in the order below. Keep every
> legacy read/write path unchanged, use only the isolated loopback database for
> migration tests, and do not wire the HMAC projection worker into the runtime
> until a separately reviewed secret store and non-owner workload identity
> exist.

**Goal:** Compare the current compatibility accounting model and the immutable
ledger at one explicit account/time/mark boundary, retain byte-exact immutable
evidence for every result, and make an accounting read cutover mechanically
impossible until a complete 30-day parity window has zero unexplained drift.

**Architecture:** A pure `internal/accountingrecon` package canonicalizes source
snapshots, compares exact decimal portfolio and instrument facts, classifies
every result, and evaluates a consecutive-daily cutover window. A small dual-run
coordinator holds one explicit per-account capture lease across both source
reads and appends one result through a narrow evidence repository. Migration 70
stores exact source/result bytes, hashes, deterministic identities, capture
fence evidence, opaque future attestation fields, and relational summaries in
append-only tables. PostgreSQL recomputes structural hashes and identities, but
the evidence is not a new accounting source and cannot itself authorize a read
switch.

**Technology:** Go 1.25, `shopspring/decimal`, canonical JSON, SHA-256,
PostgreSQL 17, pgx v5, golang-migrate, property/race tests.

## Authority and completion boundary

OVR-105 is an additive local implementation slice. It may add pure comparison
code, immutable evidence tables, read-only adapters, tests, docs, migration 70,
and a schema-version bump. It may commit and push to `codex/augr-overhaul`.

It may not:

- deploy or migrate staging/production/shared PostgreSQL;
- provision or retrieve a real checkpoint signing secret;
- run the projection repository under a database owner/migrator identity;
- infer that global legacy `orders`, `trades`, or `positions` belong to the
  schema-64 default account;
- mutate a broker, compatibility row, ledger transaction, mark, or checkpoint;
- switch API/UI/risk/allocator reads or stop a legacy write path;
- claim the production 30-day parity window from unit fixtures or local smoke.

Local completion means the comparison, evidence, and gate machinery are fully
implemented and qualified. The actual 30 consecutive daily snapshots remain a
deployment gate and must be reported as `BLOCKED_EXTERNAL` until prospectively
observed against explicitly scoped accounts and approved runtime identities.

## Contract decisions

### One comparison boundary

1. Every run names one account UUID, one UTC `as_of` timestamp, one base
   currency, one projection version, one mark source/namespace/age policy, and
   one comparison-policy version. The two sources must match that boundary.
2. A snapshot is immutable canonical evidence, not a mutable balance object.
   Its identity is derived from semantic bytes; observation time and source
   evidence are included. Caller order, map order, decimal scale, and process
   restart cannot change its bytes or checksum.
3. Money facts use exact decimal strings. Converting a legacy binary float is
   permitted only through the shortest round-trippable base-10 representation
   and must retain `binary_float` provenance. No epsilon or hidden rounding is
   used to declare equality.
4. Portfolio facts are `cash`, `buying_power`, `fees`, `realized_pnl`,
   `unrealized_pnl`, `market_value`, and `equity`. Position facts are signed
   quantity by canonical instrument UUID. Duplicate facts, foreign currency,
   nil IDs, invalid decimals, or unsorted/duplicate coverage evidence fail.
5. The ledger adapter derives facts only from one successfully validated
   OVR-104 projection. It never reads checkpoint payloads as a continuation or
   substitutes a different mark policy. Buying power is absent until a later
   margin model can derive it honestly.
6. The legacy paper adapter takes one lock-consistent broker accounting capture
   containing balance and open positions. It does not reconstruct global
   database rows into the default account. Fees and realized/unrealized P&L are
   absent when the current broker cannot expose them. Unresolved or ambiguous
   ticker-to-instrument identity is retained as missing coverage.

### Shared capture protocol

7. Matching wall-clock timestamps are not proof of a matching economic state.
   Before either source read, the coordinator must acquire one account-scoped
   capture lease from an injected fence. The lease contains a normalized fence
   ID, monotonic epoch, account, `as_of`, acquisition time, and release
   capability. Both sources must return that exact fence ID/epoch in their
   canonical bytes together with their actual observation times.
8. Every relevant paper mutation and ledger normalization for that account must
   participate in the same fence. The ledger projection is built in one
   `REPEATABLE READ` transaction while the lease is held; the paper snapshot is
   cloned under one broker read lock while the same lease is held. The lease is
   released only after both source bytes have been built. Source reads may be
   ordered, but no participating economic write may interleave them.
9. The current runtime has no common broker/PostgreSQL account fence. OVR-105
   therefore defines and tests the protocol but does not pretend the existing
   independent locks satisfy it. The coordinator rejects the run and appends no
   qualifying evidence when the lease is absent, unverified, mismatched, or
   released early. Runtime use remains blocked until the common lifecycle makes
   every relevant writer participate and the workload identity can obtain the
   lease safely.
10. One adversarial test must pause between source reads and attempt both a
    simulated paper mutation and a simulated ledger normalization. Either both
    writes remain blocked until capture release, or the coordinator returns an
    error and no evidence row. Merely giving two snapshots the same timestamp
    is never sufficient.

### Classification without tolerance

11. The comparator emits exactly one result for every required portfolio fact,
   every unioned instrument quantity, and every source coverage gap:

   - `equal`: both exact values exist and are identical;
   - `unexplained`: both exact values exist and differ without an accepted
     explanation;
   - `explained`: both exact values exist, differ, and carry a reviewed allowed
     classification plus immutable evidence reference;
   - `not_comparable`: one or both values/evidence scopes are unavailable.

12. Missing is never zero. A source that does not expose fees cannot match a
   zero fee total. An unresolved legacy position cannot disappear. A global
   unscoped legacy row count cannot be attributed to an account and becomes
   `not_comparable` evidence.
13. Explanations are inputs, not arithmetic overrides. They name the exact fact
   key, an allowed code, nonempty rationale, immutable evidence reference and
   checksum, generator identity, independent reviewer identity, and review
   time. Generator and reviewer must differ. An explanation cannot classify a
   missing value or change either source byte.
14. The initial allowed codes are narrowly versioned: legacy binary-float
    representation, legacy mark-source/timing semantics, legacy option
    multiplier semantics, and a source-system correction already represented
    by linked immutable evidence. Unknown/free-form codes fail closed. The
    classification remains visible and counts separately from exact equality.
15. Run and result IDs are deterministic length-prefixed SHA-256 UUIDs over the
    comparison policy, source checksums, and fact key. Repeating identical
    evidence converges; reusing an identity with changed bytes is an
    idempotency conflict.

### Cutover gate

16. A run is qualifying only when canonical bytes validate, both snapshots are
    for the same boundary, all required facts exist, no result is
    `not_comparable` or `unexplained`, every explanation is independently
    reviewed, and the ledger checkpoint/policy identity is present.
17. The cutover evaluator requires an injected trusted evaluation clock and at
    least 30 distinct consecutive UTC dates for one account and an unchanged
    projection/comparison/mark policy. The newest eligible endpoint is the last
    fully completed UTC day; the current UTC day never counts. It selects one
    deterministic qualifying run per date and rejects missing days, duplicates
    with conflicting evidence, policy changes, future dates, or a single
    nonqualifying day.
18. A passing evaluator returns evidence only. It has no callback, flag write,
    SQL function, or API that changes a read path. Actual cutover is a separate
    reviewed change after deployment evidence and rollback instructions.
19. Local generated 30-day fixtures prove gate logic only. They must be labelled
   synthetic and cannot be persisted or cited as real parity evidence.

### Evidence attestation boundary

20. Canonical bytes, reviewer strings, hashes, and SQL triggers prove internal
    structure and replay identity; they do not prove that a SQL writer actually
    captured either source or that named generator/reviewer identities are
    genuine. A database writer alone must never be able to manufacture
    qualifying cutover evidence.
21. The run canonical bytes bind the exact legacy snapshot, ledger snapshot,
    capture fence ID/epoch, generator identity, explanations, reviewer evidence,
    and policy. Persistence may also store an opaque attestation type, key ID,
    and signature bytes, but migration 70 does not create a key, grant, verifier,
    or claim authenticity.
22. `EvaluateCutover` requires an injected evidence verifier. Every selected
    run must verify under an approved deployment trust policy; absent, unknown,
    revoked, structurally valid but unsigned, and synthetic evidence fails.
    Unit tests use an explicitly fake verifier only to exercise window logic.
23. No runtime writer grant, scheduler, or production 30-day claim is valid
    until a separate review approves a non-owner workload identity plus source/
    evidence attestation design whose capability is unavailable to the SQL
    writer alone. Provisioning, rotation, revocation, generator/reviewer
    authentication, and incident recovery must be included in that review.

### Persistence and rollback

24. Migration 70 creates append-only `accounting_reconciliation_runs` and
    `accounting_reconciliation_results`. A run stores exact legacy snapshot,
    ledger snapshot, and comparison bytes plus SHA-256 checksums, deterministic
    ID, boundary fields, policy fields, source evidence IDs, classification
    counts, and creation time. Child rows store each exact fact/result for
    indexed inspection.
25. Insert triggers verify canonical shape, byte/JSON equality, SHA-256,
    deterministic IDs, exact count summaries, allowed status/reason values,
    parent boundary agreement, and child/result uniqueness. Update/delete are
    rejected. The Go repository revalidates all loaded bytes and relational
    rows before returning evidence.
26. These database checks guarantee structural immutability, atomicity, and
    replay/idempotency only. They cannot authenticate an arbitrary writer's
    source snapshot, fence assertion, generator, reviewer, or classification.
    Direct-SQL tests must describe that limited guarantee honestly.
27. Database owners can alter triggers and are outside the evidence trust
    boundary. Migration 70 grants no runtime privileges and introduces no
    secret. Production role grants and scheduling remain blocked with the
    OVR-104 signer/workload-identity decision and the new run-attestation
    prerequisite.
28. The down migration acquires exclusive locks first and refuses rollback when
    either reconciliation table contains data. Empty `69 -> 70 -> 69 -> 70`
    must preserve all schema-69 facts and signing-key state exactly.

## Test matrix

### Pure domain

- canonical bytes are identical across ordering, decimal scale, and restart;
- changed time, evidence, coverage, value, policy, or explanation changes the
  checksum and deterministic identity;
- missing versus zero, foreign currency, duplicate fact/instrument, NaN/Inf
  legacy conversion, and mismatched account/time/policy fail closed;
- exact equal, unexplained, explained, and not-comparable classifications;
- missing/extra/changed signed positions and unresolved identity remain visible;
- no epsilon: one 12th-decimal unit is drift;
- explanation allowlist, evidence checksum, independent reviewer, and fact-key
  binding are enforced;
- cutover: 30 consecutive complete days passes; 29, gap, duplicate conflict,
  policy drift, unexplained result, missing coverage, future day, or unreviewed
  explanation fails.

### Adapters and coordinator

- ledger projection maps exact totals and signed positions without mutation;
- ledger buying power is explicitly unavailable;
- paper broker returns balance/positions from one lock and clones its data;
- legacy adapter records binary-float provenance and missing fee/P&L coverage;
- unresolved aliases yield not-comparable evidence, never guessed identity;
- source failure or boundary mismatch writes no run;
- absent/mismatched/released capture lease writes no run;
- an adversarial mutation plus normalization cannot interleave a qualifying
  two-source capture;
- successful dual-run writes exactly once; identical/concurrent retry converges;
- changed evidence under the same semantic identity conflicts.

### PostgreSQL and migration

- exact byte/hash/JSON/ID/count round-trip and reload validation;
- direct SQL rejects structurally forged IDs/hashes/counts/status/delta/fact
  keys, malformed
  decimals/evidence, changed replay, mutation, orphan child, and parent mismatch;
- eight concurrent identical writers converge to one run and one result set;
- a forced child failure leaves no parent or partial children;
- empty down/up round-trip, data-bearing rollback refusal, lock-first writer
  blocking, schema-version synchronization, and no legacy-row mutation.

## Task 1: Pure snapshots, comparator, and parity gate

**Files:**

- Create: `internal/accountingrecon/snapshot.go`
- Create: `internal/accountingrecon/snapshot_test.go`
- Create: `internal/accountingrecon/comparison.go`
- Create: `internal/accountingrecon/comparison_test.go`
- Create: `internal/accountingrecon/gate.go`
- Create: `internal/accountingrecon/gate_test.go`

- [x] TDD exact snapshot/fact/coverage/explanation types and canonical bytes.
- [x] TDD deterministic comparison and all four classifications.
- [x] TDD exact portfolio and unioned-position comparisons without tolerance.
- [x] TDD strict explanation validation and immutable fact-key binding.
- [x] TDD 30-consecutive-UTC-day cutover evaluation and synthetic-evidence
  labels.

```bash
go test -race -count=1 ./internal/accountingrecon
```

## Task 2: Read-only source adapters and dual-run coordinator

**Files:**

- Modify: `internal/execution/types.go`
- Modify: `internal/execution/paper/broker.go`
- Modify: `internal/execution/paper/broker_test.go`
- Create: `internal/accountingrecon/source.go`
- Create: `internal/accountingrecon/source_test.go`
- Create: `internal/accountingrecon/runner.go`
- Create: `internal/accountingrecon/runner_test.go`

- [x] Add one lock-consistent, clone-safe paper accounting capture; do not
  change any balance or order behavior.
- [x] Convert OVR-104 projections and legacy captures into canonical snapshots,
  retaining unavailable metrics and float provenance explicitly.
- [x] Resolve legacy position identity only through an injected point-in-time
  canonical resolver; ambiguity/missing identity becomes coverage evidence.
- [x] Define an account-scoped capture lease/fence interface and require both
  sources to bind the same verified fence ID/epoch in their canonical bytes.
- [x] Coordinate both reads while one lease is held, compare, append evidence,
  and prove any failure is side-effect-free apart from independently completed
  source reads.
- [x] Add the adversarial paused-read test proving participating paper and
  ledger writes block through capture release, or no qualifying run is emitted.

```bash
go test -race -count=1 ./internal/accountingrecon ./internal/execution/paper
```

## Task 3: Migration 70 immutable reconciliation evidence

**Files:**

- Create: `migrations/000070_accounting_dual_run.up.sql`
- Create: `migrations/000070_accounting_dual_run.down.sql`
- Create: `migrations/000070_accounting_dual_run_test.go`
- Modify: `internal/repository/postgres/schema_version.go`

- [x] Write static contract tests before SQL.
- [x] Create exact append-only parent/child evidence schema with database hash,
  ID, byte/JSON, count, boundary, capture-fence, opaque-attestation, and
  classification checks.
- [x] Add lock-first empty-only rollback and schema 70 synchronization.
- [x] Prove live direct-SQL rejection, atomicity, concurrent convergence, and
  `69 -> 70 -> 69 -> 70` preservation in the isolated database.

```bash
go test -count=1 -run '^TestAccountingDualRunMigrationDefines' ./migrations
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 \
  -run '^TestAccountingDualRunMigration' ./migrations
go test -count=1 -run '^TestSchemaVersionSync$' ./cmd/tradingagent
```

## Task 4: PostgreSQL evidence repository

**Files:**

- Modify: `internal/repository/interfaces.go`
- Create: `internal/repository/postgres/accounting_reconciliation.go`
- Create: `internal/repository/postgres/accounting_reconciliation_test.go`

- [x] TDD atomic append/reload, semantic retry, mismatch conflict, pagination,
  and exact revalidation.
- [x] TDD concurrent convergence and no partial parent/child write.
- [x] Connect the dual-run coordinator only through the narrow repository
  interface; do not add server/runtime/scheduler wiring.
- [x] Keep attestation verification injected at the gate boundary; prove
  unsigned/unknown/revoked/synthetic evidence cannot qualify and state that
  local fake-verifier tests are logic tests, not authenticity evidence.

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 \
  -run '^TestAccountingReconciliationRepo' ./internal/repository/postgres
```

## Task 5: Qualification, review, documentation, commit, and sync

**Files:**

- Modify: `docs/development-setup.md`
- Create: `docs/runbooks/accounting-read-cutover.md`
- Modify: `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify: this plan

- [x] Focused race tests, migration tests, vet/lint, touched-file `gofumpt`, and
  `git diff --check`.
- [x] Persistent isolated database rehearsal `69 -> 70 -> 69 -> 70`, exact
  schema-69 count/key preservation, and final clean schema 70.
- [x] Repository-wide race/build/vet/lint, pinned Node 22 frontend tests/lint/
  build, inherited formatter/DB/vulnerability gates, and compiled kill-switched
  health smoke.
- [x] Independent implementation review for missing-as-zero, float tolerance,
  forged classifications, snapshot races, position omission, nonconsecutive
  windows, SQL bypasses, rollback races, secret leakage, runtime wiring, and
  accidental legacy/production writes.
- [x] Record `VERIFIED_LOCAL` implementation evidence separately from the
  `BLOCKED_EXTERNAL` 30-day parity/common-fence/runtime-identity/attestation
  gate. The runbook must forbid any runtime writer grant or scheduler before
  the separate trust-boundary review is approved.
- [x] Stage only OVR-105, inspect the cached diff, commit, fetch, fast-forward
  push, and verify local/remote identity plus a clean tracked worktree.

## Review and qualification record

- The architecture review initially required a genuine shared capture fence
  and a separate attestation authenticity boundary. The revised plan was
  approved before implementation.
- The first post-implementation review found one P1: a caller-controlled
  `throughDate` could make future dates or the incomplete current UTC date look
  eligible. `EvaluateCutover` now requires an injected trusted clock, captures
  its instant once, and permits no endpoint or candidate after the last fully
  completed UTC date. Missing-clock, current-day, future-endpoint, and
  future-candidate tests fail closed. Independent re-review approved the
  revision with no remaining P0/P1.
- Focused reconciliation/paper tests and live schema-70 migration/repository
  suites passed under `-race -count=1`. Migration tests cover forged evidence,
  deferred completeness, append-only behavior, rollback refusal, empty
  down/up, and rollback/writer lock contention. Repository tests cover exact
  reload, replay/conflict, newest-first pagination, forced-child rollback, and
  eight-writer convergence.
- The disposable persistent database is clean at `70|false`. A finalized
  downgrade/reapply preserved one account, one capital flow, one ledger
  transaction, two postings, and zero reconciliation rows.
- `task test:race`, `task build`, `task vet`, and `task lint` passed. Node
  `v22.23.2` passed all 162 frontend tests, lint, and production build. Every
  touched Go file is clean under the installed `gofumpt`; `git diff --check`
  passed. The rebuilt binary ran with live trading disabled and
  `TRADING_AGENT_KILL=true`; `/health`, `/healthz`, and `/api/v1/health` each
  returned database and Redis `ok`, then shutdown completed with zero in-flight
  pipeline runs.
- Inherited gates are recorded, not hidden: repository-wide `task fmt:check`
  still reports only nine untouched files; DB-enabled all-package testing still
  fails on the legacy trade column, overnight JSON, shared schema/type,
  vector-extension, pipeline-count, and report-nullability assumptions; and
  `govulncheck` still reports five reachable advisories in existing dependency
  versions.
- `VERIFIED_LOCAL` covers the additive mechanism only. `BLOCKED_EXTERNAL`
  covers prospective real parity, complete source coverage, shared writer
  fencing, secret/workload/reviewer identities, attestation lifecycle,
  protected-environment rehearsal, and any read cutover. No production/shared
  database, live financial state, or runtime read/write path was changed.
