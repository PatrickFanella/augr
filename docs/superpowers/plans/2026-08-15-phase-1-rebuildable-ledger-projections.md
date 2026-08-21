# Phase 1 Rebuildable Ledger Projections Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep each public
> contract test-first, obtain architecture approval before implementation and
> diff approval before shipping, and preserve the local-only deployment
> boundary.

**Goal:** Complete OVR-104 with one deterministic, point-in-time projection
that rebuilds cash, FIFO lots, positions, fees/rebates, realized P&L,
unrealized P&L, market value, and equity from the immutable account ledger and
canonical marks, then persists the exact replay as an immutable checkpoint.

**Architecture:** A pure `internal/ledger` replay engine consumes a complete
bitemporal ledger snapshot, the immutable mechanics attached to schema-68
economic normalizations, and one explicitly selected canonical mark per open
instrument. It orders events deterministically, matches signed inventory FIFO,
conserves opening and closing cash through exact residual-aware cost
allocation, and emits a canonical byte payload plus input/output SHA-256
checksums. A PostgreSQL repository performs the entire read/build/checkpoint
operation in one repeatable-read transaction. Migration 69 upgrades the
schema-65 mark/checkpoint shells for canonical, byte-preserved, idempotent
writes while leaving any legacy rows visibly noncanonical and unused.

**Tech Stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256, PostgreSQL
17/TimescaleDB, pgx v5, golang-migrate, Task, TDD with isolated schemas.

---

## Scope and sequencing

This plan covers OVR-104 only. It depends on schema 64 accounts, schema 65's
immutable ledger shells, schema 66 instruments/contracts, and schema 68's
typed economic normalizations. OVR-105 will dual-run this projection against
legacy accounting. No legacy financial read or write path changes here.

In scope:

- account-base-currency cash and net-capital reconstruction;
- deterministic FIFO lots for long and short inventory;
- partial close, multi-lot close, and direction-crossing fills;
- exact entry/exit fee and rebate allocation with residual conservation;
- cash option settlement, zero expiration, and signed prediction payout;
- physical option basis transfer into the delivered underlying leg;
- typed opening and closing execution origins/references on lots and matches;
- aggregate open/closed positions and realized/unrealized P&L;
- immutable zero-or-positive canonical marks with source namespace,
  no-lookahead availability, exact source identity, and stale-mark rejection;
- valuation by each lot's immutable opening/delivery multiplier;
- canonical replay-input and projection-output bytes/checksums;
- repeatable-read rebuild plus immutable, retry-convergent checkpoint writes;
- migration 69 rollback rehearsal and schema-version synchronization.

Out of scope:

- FX or non-base-currency cash and marks;
- margin, collateral, buying power, interest, borrow, dividends, corporate
  action processing, futures variation margin, or tax-reporting elections;
- specific-identification, LIFO, average-cost, wash-sale, or jurisdictional tax
  rules; FIFO is an accounting projection policy, not tax advice;
- modifying schema-65 ledger rows, schema-68 normalizations, legacy positions,
  broker balances, or current API/UI read paths;
- automatic checkpoint scheduling, incremental resume, reconciliation, or
  legacy dual-run/cutover (OVR-105/207/604);
- shared/production migration or deployment.

## Contract decisions

### Replay boundary and ordering

1. A point-in-time rebuild for account `A` at `AsOf` includes only immutable
   ledger transactions where both `effective_at <= AsOf` and
   `observed_at <= AsOf`. An economically old event that was not yet observed
   is unavailable and cannot leak into an earlier projection.
2. The PostgreSQL repository loads the account, complete ledger parents and
   postings, schema-68 mechanics, candidate instrument IDs, selected marks,
   and checkpoint conflict state in one `REPEATABLE READ` transaction. It does
   not use `created_at`, a PostgreSQL sequence, or the last UUID as an
   incremental high-water mark. A concurrently committed or backdated event
   produces a different complete input checksum on the next rebuild; it can
   never be skipped behind a cursor.
3. FIFO economic order is `(effective_at, observed_at, transaction_id)`, all
   ascending. Posting order is `idempotency_key, posting_id`. UUID text is the
   deterministic final tie-breaker when source timestamps are equal. Caller or
   query order cannot change lots, matches, checksums, or payload bytes.
4. OVR-104 always rebuilds from zero. A checkpoint is an immutable verified
   cache/evidence artifact, never a second source of truth or an incremental
   continuation promise. `through_transaction_id` identifies the last event in
   that exact sorted snapshot but is not treated as a completeness cursor.
   A request with no eligible ledger transaction is rejected and creates no
   checkpoint; the schema-64 account-opening capital flow is the first valid
   projection boundary.
5. Supported transaction types are exactly schema-65
   `capital_flow.deposit`/`capital_flow.withdrawal` and every schema-68 event.
   Any other transaction type fails closed instead of being silently omitted;
   it remains part of the immutable input evidence but cannot produce a
   checkpoint until that projection version understands it. Every transaction
   must validate and every economic transaction must have matching immutable
   normalization mechanics.

### FIFO lots and exact cash basis

6. Inventory quantity is signed. Positive lots are long and negative lots are
   short. An opposite-signed movement consumes existing lots FIFO before any
   remainder opens a new lot. Fills may cross through zero; settlement events
   must close exactly and may never manufacture a remainder.
7. Each lot stores signed `opening_cash` and `remaining_opening_cash`, not a
   lossy average-price float. A long buy normally has negative opening cash; a
   short sale normally has positive opening cash. Attached entry fees/rebates
   are included in that cash. For a matched fragment:

   `realized_pnl = allocated_opening_cash + allocated_closing_cash`

   This one equation handles long sells and short covers while including both
   entry and exit costs.
8. Event cash is the exact sum of its `asset:cash` postings. When one event
   both closes and opens inventory, cash is allocated by absolute quantity.
   When one close spans several FIFO lots, cash and the lots' remaining opening
   cash are allocated in FIFO order. Every non-final allocation uses
   PostgreSQL-compatible half-away-from-zero rounding to 12 decimal places;
   the final allocation receives the exact residual. Allocation therefore
   conserves every authoritative ledger amount with no dropped fraction.
9. A lot UUID is the shared deterministic hash of opening transaction,
   instrument, and opening segment. A match UUID is the hash of lot, closing
   transaction, and stable match ordinal. Lots retain opening origin/reference;
   matches retain both opening and closing origin/reference, exact quantity,
   allocated cash, disposition, effective time, and realized P&L.
10. Fill inventory uses the dated venue-contract multiplier from its immutable
    normalization for later marking. A direction-crossing fill uses that
    multiplier only for the newly opened remainder; existing lots retain their
    original multiplier.

### Settlement and physical basis transfer

11. Cash option settlement, option expiration, and prediction payout close the
    exact signed inventory declared by schema 68. Settlement cash may be zero;
    the lot's remaining opening cash plus payout becomes realized P&L. Missing,
    wrong-sign, or excess inventory fails the whole rebuild.
12. Physical exercise/assignment first consumes exactly the option lots but
    realizes no option P&L at zero. Their remaining signed opening cash is
    transferred into the underlying leg together with exact strike cash:

    `underlying_event_cash = strike_cash + transferred_option_opening_cash`

    Applying that cash to signed underlying delivery gives the correct basis
    or proceeds for long call exercise, long put exercise, short call
    assignment, and short put assignment. Transfer matches remain explicit in
    the payload so basis never disappears.
13. Newly opened delivered-underlying lots use the immutable canonical
    secondary-instrument multiplier loaded with the physical normalization.
    Existing underlying lots retain their own multiplier. No current symbol,
    alias, or venue contract is substituted during historical replay.

### Cash, P&L, and accounting equation

14. Cash is the exact sum of base-currency `asset:cash` postings. Net capital
    is the same cash leg for deposit/withdrawal transactions only. Foreign cash
    or a cash/instrument unit mismatch fails closed.
15. Fees are positive `expense:fees` postings; rebates are the magnitude of
    negative `income:rebates` postings. Attached costs are already represented
    in lot cash. A standalone fee/rebate contributes its exact `asset:cash`
    amount directly to realized P&L. Totals expose costs without double-counting
    them in performance.
16. For each open lot:

    `market_value = remaining_quantity × mark_price × lot_mark_multiplier`

    `unrealized_pnl = market_value + remaining_opening_cash`

    Short quantity therefore produces a negative liability market value and a
    correct gain/loss without a separate sign special case.
17. Portfolio totals are:

    `equity = cash + market_value`

    `total_pnl = equity - net_capital`

    The engine independently sums realized lot matches plus standalone costs
    and open-lot unrealized P&L, then requires exact equality with total P&L.
    Failure indicates an unsupported or nonconserving economic path.

### Canonical marks and point-in-time valuation

18. OVR-104 canonical marks are instrument-only. They require canonical
    instrument ID, a price currency equal to the immutable instrument currency,
    normalized source and namespace,
    required source observation ID, optional revision evidence, exact
    effective/observed times, zero-or-positive exact price, immutable metadata,
    and deterministic ID. Revision is excluded from identity; changed revision
    or payload under the same source observation conflicts. A correction uses
    a new source observation ID.
19. Migration 69 changes mark price from a rounding `NUMERIC(38,12)` typmod to
    unrestricted `NUMERIC` plus explicit 12-fractional/26-integer-digit checks,
    then adds canonical columns to schema-65 `mark_observations` but
    does not invent instrument IDs or namespaces for legacy rows. A new insert
    trigger requires the canonical shape and deterministic ID. Legacy rows with
    null canonical references remain visible but cannot be selected by the new
    repository. The old uniqueness index becomes legacy-row-only and a
    namespace-aware canonical source-identity index governs new rows. Price
    zero becomes valid for worthless contracts.
    Every new canonical row also binds the inherited generic fields exactly:
    `unit_kind='instrument'`, `unit=instrument_id::TEXT`, price currency equals
    the referenced instrument currency, source is normalized lowercase, and
    the inherited `source_observation_id` is required. The three added
    canonical fields are nullable only so a pre-migration row can retain all of
    them as null; the insert trigger rejects a new legacy-shaped row.
20. A projection request names exactly one normalized `(source, namespace)`
    and a positive maximum mark age. For every nonzero position, selection
    requires matching instrument/currency/source/namespace,
    `effective_at <= AsOf`, `observed_at <= AsOf`, and
    `AsOf - effective_at <= MaxMarkAge`. Latest order is effective time,
    observed time, then deterministic ID. Missing, future, stale,
    wrong-currency, or ambiguous caller-supplied marks fail closed.
21. Closed positions require no mark. Only selected marks for open positions
    enter the payload and input checksum. A newer mark cannot rewrite an older
    checkpoint; it creates a new input checksum and checkpoint on rebuild.

### Canonical payload and immutable checkpoints

22. Projection output is one versioned struct with sorted mark, lot, match, and
    position slices. Every decimal is encoded as its normalized string, every
    UUID as lowercase text, every timestamp uses UTC with exactly six
    fractional digits (`2006-01-02T15:04:05.000000Z`), and JSON metadata is
    recursively canonicalized. Go and PostgreSQL use that same fixed timestamp
    text when a checkpoint UUID component is derived. No map or process-local
    creation time can perturb bytes.
23. The input checksum covers account/currency, request policy, every included
    ledger header/posting semantic field, every schema-68 mechanics record, and
    every selected mark. The output checksum covers the exact canonical payload
    bytes. Reordering equal input, restarting, or rebuilding on another process
    must reproduce both hashes and byte-identical output.
24. Migration 69 adds nullable canonical checkpoint columns for compatibility:
    projection version, as-of, FIFO method, mark policy, counts, input checksum,
    and exact `payload_bytes`. A trigger requires all canonical fields on every
    new insert, verifies byte/JSONB equality and SHA-256, recomputes the
    deterministic checkpoint UUID, and recomputes the available ledger count
    and final `(effective_at, observed_at, id)` transaction under its transaction
    snapshot so `transaction_count` and `through_transaction_id` cannot be
    caller-forged. The required through transaction is selected with
    `ORDER BY effective_at DESC, observed_at DESC, id DESC LIMIT 1`; zero
    eligible transactions reject the checkpoint.
    PostgreSQL does not duplicate the complete FIFO/P&L engine in a trigger.
    Instead, replay semantics are authenticated with a versioned HMAC over the
    exact canonical bytes and key ID. Migration 69 stores 32-byte verification
    secrets in owner-only immutable key rows, supports append-only key
    revocation, and creates one byte-only `SECURITY DEFINER` persistence
    function. Its search path is pinned to trusted schemas with `pg_temp` last,
    public execution is revoked, and no runtime role is granted by default.
    The function derives JSONB, output checksum, identity, policy, and counts
    from the exact bytes; the trigger then requires an active key and verifies
    the HMAC before applying the relational checks above. `ProjectionRepo`
    receives the matching secret from an external secret provider, never logs
    or persists that Go-side copy, and fails closed unless its database role can
    execute the controlled function, cannot directly `INSERT` checkpoints, and
    cannot read verification secrets. A holder of the database writer
    credential alone can replay an identical signed payload but cannot alter or
    manufacture one. Database owners and migration principals are explicitly
    outside this attestation boundary because they can read keys or alter
    tables, ACLs, and triggers; they must never be used for application
    projection work.
25. The obsolete uniqueness of `(account, type, through_transaction_id)` is
    replaced by `(account, type, version, as_of, input_checksum)`. This permits
    a late/backdated event or mark to create a corrected full checkpoint even
    when the last economic transaction is unchanged. Identical/concurrent
    rebuilds converge. Because a repeatable-read snapshot cannot necessarily
    see a conflicting row committed after its snapshot, the repository rolls
    back on `ON CONFLICT`/serialization, reloads the winner in a fresh snapshot,
    and compares exact payload; bounded serialization retries rebuild all input
    from zero. Any same-identity payload mismatch is an idempotency conflict.
26. The down migration takes `ACCESS EXCLUSIVE` locks on marks and checkpoints,
    refuses if any canonical mark or canonical checkpoint exists, restores the
    schema-65 price and uniqueness contracts, and removes only schema-69
    functions/columns/indexes. Migration runners must quiesce writers.

## Projection posting interpretation

Only the asset/income/expense legs below affect the portfolio projection;
balanced clearing and contributed-capital counterparts remain audit evidence.

| Event | Inventory movement | Event cash / P&L treatment |
| --- | --- | --- |
| Deposit/withdrawal | none | `asset:cash`; also changes net capital |
| Fill buy/sell | `inventory` | all `asset:cash`; match FIFO, allocate attached cost |
| Standalone fee/rebate | none | `asset:cash` directly changes realized P&L |
| Cash/expiration/prediction settlement | `inventory-settlement` | settlement cash plus opening cash realizes |
| Physical exercise/assignment | `option-close`, then `underlying-delivery` | option opening cash transfers into strike cash before underlying match/open |

## File map

- Create `internal/ledger/mark.go` and tests: canonical immutable instrument
  marks, source retry equality, zero price, and deterministic identity.
- Create `internal/ledger/projection.go` and tests: replay request/input,
  economic mechanics, FIFO lots/matches, physical transfer, marks, totals,
  canonical bytes, checksums, and validation.
- Modify `internal/repository/interfaces.go`: narrow mark/checkpoint/rebuild
  repository contract without changing legacy position repositories.
- Create `internal/repository/postgres/projection.go` and tests: exact mark
  append/load, repeatable-read replay load, point-in-time mark selection,
  checkpoint convergence/reload, and concurrency.
- Create migration 69 up/down/tests: canonical mark and checkpoint upgrades,
  insert triggers, controlled non-owner persistence, exact payload bytes,
  identity, uniqueness, and rollback.
- Bump `internal/repository/postgres/schema_version.go` to 69.
- Update development docs, total overhaul plan, this evidence record, and the
  ignored deep-work ledger.

## Task 1: Canonical mark observations

**Files:**

- Create: `internal/ledger/mark.go`
- Create: `internal/ledger/mark_test.go`

- [x] Write failing constructor tests for a positive equity mark and a zero
  expired/prediction mark.
- [x] Normalize source/currency/timestamps while preserving metadata semantics;
  require instrument/source/namespace/observation identity and observed time no
  earlier than effective time.
- [x] Derive the deterministic ID without revision; validate exact 12-place/
  26-integer-digit shape and reject negative, foreign-format, invalid metadata,
  forged ID, future observation order, and changed retry evidence.

```bash
go test -count=1 ./internal/ledger
```

## Task 2: Pure FIFO replay engine

**Files:**

- Create: `internal/ledger/projection.go`
- Create: `internal/ledger/projection_test.go`

- [x] Define versioned request/input/mechanics/mark, lot, match, position,
  totals, and projection types using exact decimals and immutable identities.
- [x] TDD deposit-only cash/net-capital/equity and deterministic transaction
  sorting independent of caller order.
- [x] TDD long and short opens, partial/multi-lot FIFO close, direction
  crossing, zero-price entry, attached fee/rebate, and residual allocation at
  repeating decimal boundaries. Assert quantity and cash conservation.
- [x] TDD cash option settlement, expiration, prediction winner/loser and short
  liability; require exact closure and reject missing/wrong-sign inventory.
- [x] TDD all four physical option cases with basis transfer into underlying
  close/open behavior and immutable lot multipliers.
- [x] TDD standalone fees/rebates, fee totals, net capital, realized/unrealized,
  short liabilities, market value, equity, and exact P&L equation.
- [x] TDD canonical mark source/namespace/currency/no-lookahead/staleness/zero
  handling; require one selected mark for every open instrument and none for
  closed-only instruments.
- [x] Generate canonical input/output bytes and SHA-256; prove shuffled inputs,
  restart-equivalent objects, and rebuild-from-zero produce byte-identical
  totals, lots, matches, positions, and checksums.
- [x] Add property/fuzz coverage for signed inventory conservation, FIFO order,
  allocation residual conservation, and P&L equation across deterministic fill
  sequences.

```bash
go test -count=1 ./internal/ledger
```

## Task 3: Migration 69 canonical mark/checkpoint contract

**Files:**

- Create: `migrations/000069_ledger_projections.up.sql`
- Create: `migrations/000069_ledger_projections.down.sql`
- Create: `migrations/000069_ledger_projections_test.go`
- Modify: `internal/repository/postgres/schema_version.go`

- [x] Write static tests first for canonical mark columns/index/trigger, zero
  marks, canonical checkpoint columns/bytes/hash/identity, corrected uniqueness,
  append-only behavior, no legacy assertion, and lock-first rollback guard.
- [x] Upgrade `mark_observations` to unrestricted exact numeric price plus
  canonical instrument/source namespace/revision fields; retain legacy null
  rows under a legacy-only identity index, reject all new noncanonical inserts,
  require price currency to equal instrument currency, and recompute
  deterministic IDs in PostgreSQL.
- [x] Upgrade `projection_checkpoints` with exact payload bytes, input evidence,
  version/policy/count fields, deterministic IDs, and late-input-safe uniqueness.
- [x] Add immutable owner-only HMAC keys, append-only revocation, and a
  byte-only security-definer persistence function with a pinned safe search
  path and no public execution. Prove a dedicated non-owner writer cannot read
  keys, directly insert, alter signed bytes, or use a revoked key.
- [x] Add live direct-SQL tests rejecting missing/forged IDs, negative or
  over-precision marks, revision/payload identity reuse, byte/hash/JSONB
  mismatch, foreign mark currency, mismatched inherited mark unit/unit-kind/
  observation identity, wrong through-account/time, an earlier through ID when
  a later or late-observed transaction is eligible, zero-event checkpoints,
  forged checkpoint IDs, and post-insert mutation.
- [x] Test empty migration 68→69→68→69, preservation of schema-68 counts and
  legacy-null rows, canonical-data rollback refusal, exclusive-lock writer
  blocking, no backfill, and required schema version 69.

```bash
go test -count=1 -run '^TestLedgerProjectionMigrationDefines' ./migrations
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestLedgerProjectionMigration' ./migrations
go test -count=1 -run '^TestSchemaVersionSync$' ./cmd/tradingagent
```

## Task 4: PostgreSQL mark and projection repository

**Files:**

- Modify: `internal/repository/interfaces.go`
- Create: `internal/repository/postgres/projection.go`
- Create: `internal/repository/postgres/projection_test.go`

- [x] Add narrow contracts for recording/loading canonical marks, rebuilding a
  portfolio projection, and loading a checkpoint by ID.
- [x] TDD mark append/load: original semantic values round-trip; identical and
  concurrent writes converge; changed revision, price, timestamp, metadata, or
  mechanics conflict under the same source identity.
- [x] Load all replay inputs in one repeatable-read transaction. Use both
  effective and observed cutoffs, complete parent/posting sets, schema-68
  mechanics, exact account identity, and one explicitly scoped latest mark for
  each nonzero inventory unit.
- [x] TDD no-lookahead and late-data behavior: future-observed transactions and
  marks are absent; stale/missing marks fail; a later committed backdated event
  changes the full input checksum and creates a new checkpoint even when the
  through transaction is unchanged.
- [x] Insert canonical checkpoint bytes and JSONB atomically with rebuild;
  identical/concurrent rebuilds converge, payload mismatch conflicts, and
  reload reproduces exact bytes and passes domain validation.
- [x] Sign exact checkpoint bytes using a separately injected versioned secret;
  fail closed when it is missing/mismatched or the repository connection is an
  owner/direct writer, can read verifier keys, or lacks the controlled function.
- [x] Prove rebuilding from zero equals the stored checkpoint after mixed
  capital, long/short fills, costs, settlements, physical transfer, and marks.
  Prove a forced failure leaves marks/ledger untouched and no partial
  checkpoint.

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 \
  -run '^TestMarkObservationRepo|^TestPortfolioProjectionRepo' \
  ./internal/repository/postgres
```

## Task 5: Qualification, review, documentation, commit, and sync

**Files:**

- Modify: `docs/development-setup.md`
- Modify: `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify: this plan

- [x] Run focused tests under the race detector, vet, lint, touched-file
  `gofumpt -d`, and `git diff --check`.
- [x] Rehearse empty schema 68→69→68→69 on the persistent isolated PostgreSQL
  instance, proving all schema-68 counts unchanged and final `69|false`.
- [x] Run `task test:race`, `task build`, `task vet`, `task lint`, frontend
  tests/lint/build, and compiled kill-switched health smoke against isolated
  PostgreSQL/Redis. Re-run `task fmt:check`, DB-enabled all-package tests, and
  `task vulncheck`, classifying inherited failures separately.
- [x] Document OVR-104 as local/additive, full-rebuild/FIFO, exact,
  point-in-time, and not deployed or cut over; record OVR-105 as next.
- [x] Obtain post-implementation diff review for lookahead, event omission,
  long/short signs, FIFO/crossing, fee allocation, physical basis transfer,
  multiplier retention, mark source/age, P&L equation, checkpoint races,
  SQL bypasses, rollback races, and accidental legacy writes.
- [x] Stage only OVR-104, inspect cached diff, commit
  `feat: add rebuildable ledger projections`, fetch, and fast-forward push the
  current branch. No force push or shared database mutation.

## Review record

- 2026-08-15 architecture review: **APPROVE** after two P1 revisions. The
  final plan rejects zero-event checkpoints, recomputes the exact final
  bitemporally eligible transaction in replay order, and binds every canonical
  mark to its inherited instrument unit, source observation, currency, and
  normalized source fields. No P0/P1 plan defect remains.
- 2026-08-15 first post-implementation review: **REVISE**. The relational
  trigger verified bytes, identity, counts, and the bitemporal ledger boundary
  but could not establish that a structurally coherent payload came from the
  pure replay engine.
- 2026-08-15 first trust-boundary review: **REVISE**. Revoking direct table DML
  and exposing a security-definer byte function merely moved the same forgery
  capability to any holder of the database writer credential.
- 2026-08-15 final post-implementation review: **APPROVE** with no remaining
  P0/P1. Exact bytes now carry a domain-separated, versioned-key HMAC generated
  from a separately injected secret. The non-owner database role cannot read
  verifier keys, direct-insert, alter captured signed bytes, or reuse a revoked
  key. The review independently checked Go/PostgreSQL message parity, pinned
  definer search path, privilege preflight, wrong-secret rollback, conflict
  convergence, and lock-first rollback safety.

## Qualification evidence

Qualification completed locally on 2026-08-15:

- Pure-domain tests passed under the race detector. Focused schema-69
  repository and migration suites passed against the isolated PostgreSQL
  instance under `-race -count=1`, including eight-writer mark convergence,
  six-worker rebuild convergence, no-lookahead selection, late/backdated input,
  direct-SQL semantic failures, real non-owner privilege isolation, forged-HMAC
  rejection, key revocation, wrong-secret rollback, and rollback-lock blocking.
- Focused vet and lint passed with zero findings. Every touched Go file is
  clean under the installed `gofumpt`; `git diff --check` passed.
- The persistent loopback-only database rehearsed the finalized migration as
  `68|false -> 69|false -> 68|false -> 69|false`. It preserved one account,
  one capital flow, one ledger transaction, two ledger postings, and empty
  instrument, quote, economic-event, mark, checkpoint, signing-key, and
  revocation tables. The final schema is clean at `69|false`.
- `task test:race`, `task build`, `task vet`, and `task lint` passed across the
  backend. Under the repository-pinned Node 22 toolchain, all 162 frontend
  tests, frontend lint, and frontend production build passed.
- The compiled binary ran against isolated PostgreSQL and Redis with live
  trading disabled and `TRADING_AGENT_KILL=true`. `/health`, `/healthz`, and
  `/api/v1/health` each returned
  `{"status":"ok","db":"ok","redis":"ok"}`, followed by a clean shutdown
  with zero in-flight pipeline runs.
- Independent final review approved the HMAC-attested implementation with no
  remaining P0/P1 findings. The source contains no signing secret; isolated
  tests generate ephemeral 32-byte keys. A shared environment still requires
  a separately reviewed secret store, workload identity, key provisioning, and
  rotation ceremony before OVR-105 may wire or cut over the projection.
- Inherited gates remain explicit. Repository-wide `task fmt:check` reports
  only the same nine untouched files. The DB-enabled all-package run retains
  legacy `trades.exit_reason`, overnight JSON formatting, shared migration
  schema/type, vector-extension, stale pipeline-column-count, and report-
  artifact-nullability assumptions. `govulncheck` reports the same five
  reachable advisories in existing gRPC, x/text, x/net, and pgx dependencies.
  Focused schema-69 suites remain green; none of these failures was suppressed
  or changed in OVR-104.
