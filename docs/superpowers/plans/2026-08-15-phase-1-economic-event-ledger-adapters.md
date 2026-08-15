# Phase 1 Economic Event Ledger Adapter Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep each public
> contract test-first, obtain architecture approval before implementation and
> diff approval before shipping, and preserve the local-only deployment boundary.

**Goal:** Complete OVR-103 with an additive boundary that durably records raw
economic source events before normalization, then converts every supported
fill, fee/rebate, option event, and prediction payout into exactly one balanced,
exact-decimal, idempotent ledger transaction.

**Architecture:** Migration 68 introduces three append-only fact sets. First,
`economic_source_events` wire-preserves a provider/system JSON payload and its
hash under a stable account-scoped identity. Second, provenance-backed
`option_contract_terms` supplies the strike, call/put, underlying, strike
currency, and deliverable quantity missing from schema 66. Third,
`economic_event_normalizations` records the typed canonical interpretation and
links it to one ledger aggregate. Raw ingestion commits independently before
normalization. Applying a normalization writes the normalization row, ledger
parent, and ledger postings in one transaction, with a deferred PostgreSQL
constraint checking their exact semantic agreement at commit. Existing
float-based order/trade/position paths remain unchanged until OVR-105.

**Tech Stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256, PostgreSQL
17/TimescaleDB, pgx v5, golang-migrate, Task, TDD with isolated schemas.

---

## Scope and sequencing

This plan covers OVR-103 only. It depends on schema 65's immutable ledger and
schema 66's canonical instrument/venue-contract reference boundary. OVR-104
will project these events. OVR-105 will dual-run legacy accounting. OVR-203
will later introduce the common execution lifecycle and provider adapters.

In scope:

- wire-preserved raw JSON source events committed before normalization;
- stable source identity whose retry key excludes source revision;
- exact immutable option terms needed to support physical exercise/assignment;
- typed execution origins and related fill/order/position/settlement references;
- exact fills for equities, ETFs, crypto spot, options, futures, and prediction
  contracts using canonical tick, lot, multiplier, currency, and validity facts;
- fill-attached and standalone fees/rebates;
- option expiration, cash settlement, physical exercise/assignment, and binary
  prediction payout for signed long or short inventory;
- deterministic normalization, ledger transaction, and posting identities;
- database-enforced immutability, event shape, canonical posting units, exact
  posting amounts, one-normalization-per-source-event, and rollback safety;
- migration 68 rehearsal and schema-version synchronization.

Out of scope:

- changing current order, trade, position, broker, expiry, or settlement writes;
- treating legacy `FLOAT` or `NUMERIC(20,8)` values as canonical;
- backfilling unprovable legacy economic facts;
- FX, multi-currency cash, asset-denominated fees, third-currency fees, margin,
  collateral, interest, borrow, corporate actions, or correction netting;
- cash/lot/position/P&L/equity projections (OVR-104);
- legacy-versus-ledger dual-run/cutover (OVR-105);
- intent/order/fill lifecycle and provider adapters (OVR-203/205);
- shared/production migration or deployment.

## Contract decisions

### Raw source identity and evidence

1. The durable retry identity is `(account_id, source, source_namespace,
   source_event_id)`. `source_revision` is stored evidence but is deliberately
   excluded from identity and deterministic IDs. A retry with any changed
   revision, raw bytes, parsed JSON, timestamp, or account/source field is an
   idempotency conflict. A correction must arrive as an explicit new source
   event (for example a future reversal/adjustment type); revision changes can
   never silently create a second economic effect.
2. `Source` is normalized lowercase. `SourceNamespace` names the endpoint,
   subscription, simulation run, settlement batch, or import manifest in which
   the source event ID is unique. All identity components are required.
3. Raw payload must be valid JSON but may be any JSON type. The original UTF-8
   bytes are stored in `BYTEA`, SHA-256 is stored and independently checked in
   PostgreSQL, and a JSONB copy supports queries. This defines both wire-level
   and semantic evidence; JSONB alone is not called byte-preserving.
4. Raw `ObservedAt` is required UTC microsecond receive/import time. No current
   time substitutes for a missing source timestamp. `CreatedAt` is local
   persistence time and is not part of semantic replay equality.
5. `RecordEconomicSourceEvent` commits this raw row before a normalizer runs.
   Invalid, incomplete, or temporarily unresolvable evidence therefore remains
   visible as a source event with no normalization. A failed normalization or
   failed ledger commit rolls back only the applied-normalization transaction.

### Normalized identity, origin, and linkage

6. Every applied normalization requires a typed execution origin:
   `strategy_version`, `copy_subscription`, `portfolio_rebalance`,
   `risk_reduction`, `operator`, `settlement`, or `reconciliation`. It also
   requires a separate related `ReferenceType`/`ReferenceID`.
7. One source event may have at most one applied normalization. Its UUID, ledger
   transaction UUID, and posting UUIDs are deterministic domain-separated
   hashes of the source event UUID, normalizer version, and posting keys.
   Re-running another version after application conflicts rather than reposting.
   Go and PostgreSQL share one derivation algorithm: UTF-8 encode the domain
   followed by each component as `unit-separator + decimal byte length + colon
   + value`, compute SHA-256, take the first 16 bytes, and render those bytes as
   a UUID. The migration's immutable `economic_deterministic_uuid` function and
   semantic triggers recompute every source-event, option-term, normalization,
   transaction, and posting ID; caller-supplied arbitrary UUIDs are rejected.
8. The linked ledger transaction uses
   `origin_type=economic_source_event`, `origin_id=<source event UUID>`, and
   idempotency key `economic-source-event:<source event UUID>`. Its event type,
   account, reference, effective/observed times, raw hash, typed origin,
   normalization ID, and normalizer version must match the normalization/source.
9. `EffectiveAt` is when the economic effect occurs and must not be after the
   raw event's `ObservedAt`. Both are exact UTC microsecond timestamps.
10. Normalized economic values use exact decimals with at most 12 fractional
    and 26 integer digits. Products must already fit `NUMERIC(38,12)`; no
    adapter, constructor, repository, or trigger rounds an authoritative value.

### Currency and reference policy

11. OVR-103 is deliberately single-currency per account. Every cash leg must
    use `account.base_currency`. A fill/settlement venue contract, a physical
    option term's strike currency, and any attached fee currency must all match
    that base currency. Standalone foreign-currency costs, asset-denominated
    fees, and third-currency fees fail closed pending an explicit FX model.
12. A fill uses an active, complete, non-quarantined canonical instrument and
    a matching venue contract effective at fill time. Quantity is positive and
    exact-lot. Price is nonnegative and exact-tick. Prediction fill price is
    additionally constrained to `[0,1]`.
13. Settlement may occur after a contract window ends. It must use the exact
    historical contract linked by the source event; the contract must match
    instrument and venue and must have begun by settlement, but the normalizer
    never substitutes a future/current replacement. The instrument/terms must
    be complete and non-quarantined; expired status is allowed for settlement.
14. Settlement position quantity is signed, nonzero, and exact-lot. Its
    extinguishing posting is exactly `-position_quantity`. Nonzero settlement
    price is nonnegative and exact-tick. Prediction payout is exactly `0` or
    `1`. Option expiration requires zero. Cash option settlement requires cash
    settlement mechanics; prediction payout requires binary mechanics.

### Immutable physical option terms

15. Physical exercise/assignment never accepts caller-authored strike,
    call/put, underlying, or delivery ratio as economic truth. It requires a
    persisted immutable `OptionContractTerms` record linked to the option and
    containing:

    - canonical option and underlying instrument IDs;
    - call/put;
    - exact strike and strike currency;
    - exact underlying units delivered per option contract;
    - source/namespace/record/revision, effective/observed time, original JSON
      bytes/hash/JSONB, and created time.

16. The terms' underlying must match the option instrument's canonical
    underlying. The option instrument and option venue contract must be
    physical-settlement records. The dated venue multiplier and selected term
    deliverable quantity must agree; the instrument-level multiplier is a
    non-dated default and is not allowed to override sourced effective terms.
    Derived delivered quantity must be an
   exact multiple of the immutable underlying instrument lot size. These
   durable terms—not a current underlying venue contract—govern delivery.
17. Terms for one option form non-overlapping effective intervals. At most one
    term may begin at an exact `EffectiveAt`; the next later term closes the
    prior interval. A physical event carries the exact selected term ID and
    requires the term to be the latest term with
    `terms.effective_at <= event.effective_at` and
    `terms.observed_at <= source_event.observed_at`. Go validation rejects a
    future or not-yet-observed selected term; the deferred database trigger
    additionally rejects an older/ambiguous term when a later eligible term
    exists. A changed deliverable applies only at or after its effective
    boundary; same-time conflicting revisions are rejected rather than guessed.
    Term append and physical-normalization apply both acquire the same
    transaction-scoped advisory lock keyed by canonical option instrument ID.
    A term append must be in strictly increasing effective-time order and scans
    existing immutable physical normalizations; it is rejected if it would
    become a newer eligible term for any already-normalized event. After the
    shared lock serializes concurrent work, the physical-normalization trigger
    reselects the latest eligible term. A later term that cannot affect an
    existing normalization remains appendable for future events.
18. Exercise closes a positive long option position; assignment closes a
    negative short option position. With signed `position_quantity`, delivered
    underlying is `position × deliverable` for calls and its negation for puts;
    strike cash is the negative of `delivered × strike`. This yields all four
    call/put and long/short sign cases without ticker parsing.

### Database authority and rollback

19. `economic_event_normalizations` stores typed numeric columns rather than
    relying on opaque JSON: fill quantity/price, optional cost kind/currency/
    amount, settlement position/price, canonical IDs, and option-term ID. SQL
    checks enforce event-specific required/prohibited fields.
20. A deferred semantic trigger recalculates every expected posting key,
    account, unit kind, exact unit, and exact amount from those typed columns
    and reference facts. It rejects missing, extra, arbitrary-unit, wrong-sign,
    wrong-amount, wrong-contract, wrong-currency, or mismatched ledger writes,
    even when attempted directly through SQL.
21. Migration 68 inserts no legacy row. Its down migration first takes
    `ACCESS EXCLUSIVE` locks on all three new tables, then refuses rollback if
    any contains data or if a ledger transaction has
    `origin_type=economic_source_event`. Empty rollback removes only schema-68
    objects and preserves schema 67. Migration runners must quiesce writers;
    the lock makes the emptiness check race-safe.

## Posting templates

Debits are positive and credits negative. Every currency or instrument unit
balances independently.

| Posting keys | Exact business amount | Unit |
| --- | ---: | --- |
| `inventory`, `clearing-inventory` | buy `+quantity`, sell `-quantity`, opposite clearing | primary instrument UUID |
| `gross-cash`, `clearing-gross-cash` | buy `-(price*quantity*multiplier)`, sell positive, opposite clearing | account base currency |
| `fee-expense`, `fee-cash` | `+fee`, `-fee` | account base currency |
| `rebate-cash`, `rebate-income` | `+rebate`, `-rebate` | account base currency |
| `inventory-settlement`, `clearing-inventory-settlement` | `-position`, opposite clearing | primary instrument UUID |
| `settlement-cash`, `clearing-settlement-cash` | `position*price*multiplier`, opposite clearing | account base currency |
| `option-close`, `clearing-option-close` | `-position`, opposite clearing | option UUID |
| `underlying-delivery`, `clearing-underlying-delivery` | signed derived delivery, opposite clearing | underlying UUID |
| `strike-cash`, `clearing-strike-cash` | negative delivered units times strike, opposite clearing | account base currency |

Zero-valued pairs are omitted; inventory/option closure still records a
zero-payout settlement. Inventory accounts are selected by canonical asset
class (`asset:security_inventory`, `asset:crypto_inventory`,
`asset:option_inventory`, or `asset:event_contract_inventory`). Clearing uses
`clearing:execution` for fills and `clearing:settlement` for settlements.

## File map

- Create `internal/economicid/id.go` and tests: the shared length-prefixed
  SHA-256 UUID byte contract mirrored by migration 68.
- Create `internal/ledger/economic_source_event.go` and tests: raw identity,
  wire bytes/hash, replay equality, deterministic UUIDs, and validation.
- Modify `internal/ledger/transaction.go` and tests: narrowly scoped
  deterministic construction without changing legacy `NewTransaction` behavior.
- Create `internal/instrument/option_terms.go` and tests: immutable sourced
  strike/call-put/deliverable facts.
- Create `internal/ledger/economic_normalization.go` and tests: typed event
  shape plus fill, cost, cash/prediction, expiration, and physical normalizers.
- Modify `internal/repository/interfaces.go`: narrow raw-event,
  applied-normalization, and option-terms methods.
- Modify `internal/repository/postgres/instrument.go` and tests: append/load
  immutable option terms with replay/conflict behavior.
- Modify `internal/repository/postgres/ledger.go` and tests: raw-event durable
  append plus atomic normalization/ledger application.
- Create migration 68 up/down/tests: all three tables, semantic constraints,
  immutability, idempotency, and rollback locking.
- Bump `internal/repository/postgres/schema_version.go` to 68.
- Update development docs, total overhaul plan, and this evidence record.

## Task 1: Raw source events and deterministic ledger construction

**Files:**

- Create: `internal/ledger/economic_source_event.go`
- Create: `internal/ledger/economic_source_event_test.go`
- Modify: `internal/ledger/transaction.go`
- Modify: `internal/ledger/transaction_test.go`

- [x] Write `TestEconomicSourceEventIdentityExcludesRevision` and verify RED.
- [x] Add `EconomicSourceEvent`/input with normalized account/source identity,
  observed time, wire payload, parsed payload, SHA-256, and deterministic ID.
- [x] Drive RED → GREEN validation for invalid JSON, any valid JSON type,
  missing timestamps, byte/hash mismatch, normalization, namespace separation,
  identical retry equality, and revision/payload mismatch conflict semantics.
- [x] Add a package-private deterministic transaction builder that accepts an
  explicit namespace/seed, refuses missing observed time, derives transaction
  and posting UUIDs from unique posting keys, and otherwise shares all existing
  validation. Keep public `NewTransaction` random-ID/default-observed behavior
  unchanged and regression-tested.

```bash
go test -count=1 ./internal/ledger
```

## Task 2: Provenance-backed immutable option terms

**Files:**

- Create: `internal/instrument/option_terms.go`
- Create: `internal/instrument/option_terms_test.go`

- [x] Write constructor tests first for a canonical physical call and put.
- [x] Require option/underlying IDs, call/put, positive exact strike, normalized
  strike currency, positive exact deliverable quantity, full source identity,
  effective/observed timestamps, original JSON bytes/hash, and metadata.
- [x] Reject self/missing links, excess precision/magnitude, invalid
  provenance, invalid JSON/hash, and observed-before-effective terms.
- [x] Derive the term UUID with the shared domain-separated SHA-256 algorithm;
  the identity includes option ID plus source/namespace/source-record ID but
  excludes revision so a revision cannot create a competing same fact.
- [x] Keep cross-record option/underlying/multiplier validation in the
  normalizer and database because the standalone aggregate cannot load them.

```bash
go test -count=1 ./internal/instrument
```

## Task 3: Exact typed economic normalizers

**Files:**

- Create: `internal/ledger/economic_normalization.go`
- Create: `internal/ledger/economic_normalization_test.go`

- [x] Define event/origin/cost vocabularies and `EconomicNormalization` with
  pointer decimals for present-zero versus absent fields.
- [x] Define a base input containing the persisted source event, immutable
  account, normalizer version, execution origin, reference, and effective time.
- [x] Drive fill buy/sell tests for exact cash/inventory signs, each asset-class
  account, multiplier, zero price, fee/rebate, and deterministic IDs.
- [x] Reject wrong account/currency, inactive or quarantined instruments,
  mismatched/out-of-window contracts, off-tick/off-lot values, prediction price
  outside `[0,1]`, non-contract fee currency, and product overflow.
- [x] Drive standalone fee/rebate tests and reject zero/negative, foreign,
  asset-denominated, or third-currency costs.
- [x] Drive signed cash/prediction/expiration settlement tests, including long
  winner, short liability, zero payout, exact lot/tick, expired historical
  contract, wrong mechanics, future replacement contract, and overflow.
- [x] Drive all four physical sign matrices from persisted option terms. Reject
  forged/mismatched terms, strike, call/put, underlying, delivery ratio,
  multiplier, strike currency, settlement method, underlying-lot result, and
  quarantined references.
- [x] Reject a term effective after the event or observed after the raw event;
  accept a historical term after the venue-contract window has ended. Leave
  latest-eligible-term and ambiguity enforcement to the repository/database
  query that can see the complete append-only term history.
- [x] Require `Validate` to reproduce the deterministic ledger header/posting
  IDs and exact posting template from typed fields, preventing manually forged
  in-memory normalizations.

```bash
go test -count=1 ./internal/ledger
```

## Task 4: Migration 68 durable truth boundary

**Files:**

- Create: `migrations/000068_economic_events_test.go`
- Create: `migrations/000068_economic_events.up.sql`
- Create: `migrations/000068_economic_events.down.sql`
- Modify: `internal/repository/postgres/schema_version.go`

- [x] Write static migration tests and verify RED for:
  `economic_source_events`, `option_contract_terms`,
  `economic_event_normalizations`, source identity excluding revision, raw
  `BYTEA` + SHA-256 + JSONB, unique source-to-normalization and ledger links,
  typed numeric fields, immutable triggers, deferred semantic trigger,
  shared SHA-256 deterministic-ID function, event-shape checks,
  exclusive-lock rollback guard, and no legacy backfill.
- [x] Create `option_contract_terms` with immutable provenance and a deferred
  semantic trigger that requires an option, its exact canonical underlying,
  physical settlement, matching immutable multiplier/deliverable mechanics,
  strictly increasing unique option/effective-time boundaries, and deterministic
  latest-eligible selection at a physical event's effective/observed time.
- [x] Make option-term append and physical-normalization validation take the
  same per-option transaction advisory lock. Reject a term that would supersede
  the selected term of any existing immutable physical normalization; after
  lock acquisition, re-evaluate the latest eligible term before commit.
- [x] Create `economic_source_events` with deterministic ID, account-scoped
  source uniqueness, wire payload/hash/parsed JSON checks, and append-only rules.
- [x] Create `economic_event_normalizations` with one row per source event,
  one ledger transaction, required/prohibited event-specific columns, and
  deferred ledger FK so the normalization may be inserted before the aggregate.
- [x] Implement a deferred semantic trigger that validates source/account/time,
  ledger header/metadata, account base currency, reference identity, contract
  identity/window/mechanics, option terms, and the complete exact posting set.
- [x] Recompute source-event, option-term, normalization, ledger-transaction,
  and posting UUIDs with `economic_deterministic_uuid`; never trust a directly
  supplied ID merely because its other foreign keys and payload are valid.
- [x] Add isolated live tests rejecting direct-SQL null/mismatched IDs,
  irrelevant fields, arbitrary instrument units, missing/extra postings,
  wrong amounts/signs, foreign currencies, invalid contract windows, forged
  option terms, future/ambiguous term selection, duplicate effects, forged
  source/term/normalization/transaction/posting UUIDs, late posting mutation,
  and raw mutation.
- [x] Add real-PostgreSQL tests proving a later historical superseding term is
  rejected, concurrent term append/physical normalization serializes to one
  valid outcome, and a genuinely non-retroactive later term is accepted.
- [x] Test raw-only commit, normalization-before-ledger commit, normalization
  rollback with raw retention, no-backfill, empty 68→67→68 rehearsal, nonempty
  rollback refusal, and a writer blocked by the down migration's exclusive lock.
- [x] Bump and test required schema version 68.

```bash
go test -count=1 -run '^TestEconomicEventMigrationDefines' ./migrations
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestEconomicEventMigration' ./migrations
go test -count=1 -run '^TestSchemaVersionSync$' ./cmd/tradingagent
```

## Task 5: PostgreSQL repositories and retry convergence

**Files:**

- Modify: `internal/repository/interfaces.go`
- Modify: `internal/repository/postgres/instrument.go`
- Modify: `internal/repository/postgres/instrument_test.go`
- Modify: `internal/repository/postgres/ledger.go`
- Modify: `internal/repository/postgres/ledger_test.go`

- [x] Add narrow contracts:

```go
type EconomicEventRepository interface {
    RecordEconomicSourceEvent(context.Context, *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error)
    GetEconomicSourceEventByID(context.Context, uuid.UUID) (*ledger.EconomicSourceEvent, error)
    ApplyEconomicNormalization(context.Context, *ledger.EconomicNormalization) (*ledger.EconomicNormalization, error)
    GetEconomicNormalizationBySourceEventID(context.Context, uuid.UUID) (*ledger.EconomicNormalization, error)
}
```

Add option-term append/get methods to `InstrumentRepository` without widening
unrelated repositories.

- [x] TDD `RecordEconomicSourceEvent`: commit raw first; identical retry returns
  existing; same identity with changed revision/bytes/hash/time conflicts;
  namespaces remain distinct; concurrent identical writes converge to one row.
- [x] TDD immutable option-term append/load: exact replay converges and any
  conflicting economic/provenance field fails with `ErrIdempotencyConflict`.
- [x] Refactor ledger insert SQL into a private pgx-transaction helper shared by
  `PostTransaction` and normalization application without altering existing
  public ledger behavior.
- [x] TDD `ApplyEconomicNormalization`: require the source event already exists
  with identical evidence; insert normalization first, ledger parent second,
  postings third; commit only after deferred checks; then reload exact values.
- [x] Verify identical/concurrent apply creates one aggregate, changed version
  or economics conflicts, unknown source/reference rows fail, and a forced
  ledger failure leaves the raw source row durable with no normalization or
  partial ledger aggregate.

```bash
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 -run '^TestLedgerRepo|^TestEconomicEventRepo|^TestOptionTermsRepo' ./internal/repository/postgres
```

## Task 6: Qualification, review, documentation, commit, and sync

**Files:**

- Modify: `docs/development-setup.md`
- Modify: `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify: this plan

- [x] Run focused tests under the race detector, vet, lint, touched-file
  `gofumpt -d`, and `git diff --check`.
- [x] Rehearse empty schema 67→68→67→68 on the persistent isolated PostgreSQL
  instance, proving all schema-67 counts unchanged and final `68|false`.
- [x] Run `task test:race`, `task build`, `task vet`, `task lint`, frontend
  tests/lint/build, and compiled kill-switched health smoke against isolated
  PostgreSQL/Redis. Re-run `task fmt:check`, DB-enabled all-package tests, and
  `task vulncheck`, classifying inherited failures separately.
- [x] Document OVR-103 as local/additive, raw-first, exact, balanced, and not
  deployed/cut over; record no legacy backfill and OVR-104 as the next item.
- [x] Obtain post-implementation diff review for duplicate effects, currency,
  signs, multipliers, rounding, physical terms, raw durability, SQL bypasses,
  rollback races, and accidental compatibility writes.
- [x] Stage only the OVR-103 slice, inspect cached diff, commit
  `feat: adapt economic events to the ledger`, fetch, and fast-forward push the
  current branch. No force push or shared database mutation.

## Review record

- [x] Initial architecture review completed 2026-08-15: **REVISE**. Accepted
  findings required immutable physical option terms, underlying delivery
  mechanics, base-currency enforcement, exact direct-SQL posting checks,
  deterministic constructor coverage, settlement tick/lot/range rules,
  race-safe rollback locking, and wire-level raw evidence.
- [x] Author self-review completed after revision: source revision no longer
  creates economic identity; raw ingestion is a separate durable commit; the
  database can reconstruct all expected postings from typed columns; option
  strike/direction/deliverable terms are provenance-backed rather than caller
  assertions; and later projection/lifecycle work remains out of scope.
- [x] Second architecture review completed 2026-08-15: **REVISE**. Accepted
  findings required temporal applicability/latest-term selection and database
  recomputation of every claimed deterministic UUID.
- [x] Second author revision: option terms now form deterministic effective
  intervals with no-lookahead observation rules, and Go/PostgreSQL share an
  explicit length-prefixed SHA-256 UUID algorithm with direct-SQL forgery tests.
- [x] Third architecture review completed 2026-08-15: **REVISE**. Accepted the
  remaining finding that latest-term selection must remain stable under a later
  historical insert or concurrent term/normalization transactions.
- [x] Third author revision: both paths now share a per-option advisory lock;
  term append rejects retroactive supersession of immutable normalizations and
  concurrency/non-retroactive append have named live-database tests.
- [x] Final implementation-plan approval: independent review returned
  **APPROVE** with no remaining P0/P1 implementation-plan defects.
- [x] Post-implementation diff approval: **APPROVE** after the repository paths
  acquired the shared per-option advisory lock in a standalone statement
  before writing, guaranteeing a fresh post-contention `READ COMMITTED`
  snapshot while retaining trigger-level defense in depth.

## Qualification evidence

Qualification completed locally on 2026-08-15:

- Focused domain suites passed for `internal/economicid`,
  `internal/instrument`, and `internal/ledger`. Focused PostgreSQL repository
  and migration suites passed with `-race -count=1`; the forced option-term
  append versus physical-normalization race also passed five consecutive
  race-detected runs.
- Migration tests proved Go/PostgreSQL deterministic UUID agreement, raw-only
  commit, normalization-before-ledger commit, raw retention after failed
  normalization, exact semantic reconstruction, direct-SQL ID/unit/currency/
  amount/reference/window/count corruption rejection, append-only mutation
  rejection, nonempty rollback refusal, and writer blocking behind the down
  migration's exclusive lock.
- The persistent loopback-only database rehearsed schema
  `67|false → 68|false → 67|false → 68|false`. It preserved one account, one
  capital flow, one ledger transaction, two postings, and all schema-67 facts;
  the three new schema-68 tables were empty before and after rehearsal.
- `task test:race`, `task build`, `task vet`, `task lint`, all 162 frontend
  tests, frontend lint/build, focused vet/lint, touched-file `gofumpt`, and
  `git diff --check` passed.
- The compiled binary ran against isolated PostgreSQL/Redis with live trading
  disabled and `TRADING_AGENT_KILL=true`. `/health`, `/healthz`, and
  `/api/v1/health` each returned
  `{"status":"ok","db":"ok","redis":"ok"}`, followed by clean shutdown.
- Independent post-implementation review first identified the subtle
  trigger-local advisory-lock snapshot race. Moving the shared lock into its
  own transaction statement on both repository paths and forcing both writers
  to block behind a test gate resolved it; the final verdict was **APPROVE**
  with no remaining P0/P1 finding.
- Inherited evidence gates remain explicit: repository-wide
  `task fmt:check` reports the same nine untouched files; the database-enabled
  all-package run retains legacy integration/shared-migration-harness failures;
  and `govulncheck` reports the same five reachable advisories in existing
  pgx, gRPC, x/net, and x/text dependencies. Focused schema-68 suites remain
  green and none of those failures was suppressed or changed in this slice.
