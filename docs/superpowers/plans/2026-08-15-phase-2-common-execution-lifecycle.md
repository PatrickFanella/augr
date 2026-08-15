# Phase 2 Common Execution Lifecycle Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep each public
> contract test-first, obtain architecture approval before implementation and
> diff approval before shipping, and preserve the local-only deployment
> boundary.

**Goal:** Complete OVR-203 with one exact, append-only intent/order/fill
lifecycle whose deterministic identities, serialized transitions, recovery
queries, and economic-normalization links make retries and restarts converge
without duplicating an order, fill, or ledger effect.

**Architecture:** Add `internal/execution/lifecycle` beside the existing mutable
order manager. One immutable intent describes a desired signed position change
and its point-in-time decision evidence. At most one immutable order command is
routed for that intent in this first common contract; retries reuse its stable
client identity rather than creating another order. Append-only lifecycle
events implement the accepted ADR-017 transition graph, while immutable venue
bindings and fills provide the order/fill facts referenced by those events.
Every accepted fill, its OVR-103 normalization, and its ledger transaction
commit in the same database transaction. A crash therefore leaves either the
raw source event alone or the complete normalized fill graph; it cannot leave
an orphaned economic effect or repost one on retry. Migration 71 creates this
new boundary empty. Existing brokers, `OrderManager`, legacy `orders`, `trades`,
positions, backtests, and schedulers remain unchanged until their explicit
adapter phases.

**Tech stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256,
PostgreSQL 17/TimescaleDB, pgx v5, golang-migrate, Task, and TDD against an
isolated schema-70 database.

---

## Scope and sequencing

This plan covers OVR-203 only. It depends on OVR-102's immutable ledger and
OVR-202's canonical quote/depth observations; it also consumes the completed
OVR-103 economic-event normalization boundary. It does not activate a venue.

In scope:

- one canonical execution intent with exact signed desired quantity, explicit
  account/environment, typed origin, canonical instrument, decision time, and
  an immutable point-in-time quote snapshot;
- one deterministic order command per intent with exact quantity/price terms,
  dated venue mechanics, route snapshot, policy kind/version, and a stable
  client order ID;
- append-only proposed, allocated, risk-approved/rejected, routed, working,
  partial-fill, filled, cancelled, expired, rejected, and
  failed-reconciliation transitions;
- append-only cancel requests that preserve the current working/partial state;
- explicit unknown-state, contradictory-state, fill-correction, and fill-bust
  observations that fail closed into `failed_reconciliation`;
- separate immutable venue-order binding and fill facts;
- exact event evidence bytes, SHA-256, parsed JSON object, source and receive
  timestamps, actor, reason code/text, environment, account, origin, optional
  strategy version, and simulation/venue policy version on every transition;
- deterministic intent, order, lifecycle-event, binding, and fill identities;
- row-serialized state changes, idempotent replay, mismatch rejection,
  concurrent retry convergence, aggregate reload/replay, and recovery-candidate
  queries;
- a required one-to-one link from every canonical fill to the OVR-103 fill
  normalization and ledger transaction committed atomically with it;
- migration 71 database invariants, empty-only rollback, schema-version bump,
  tests, runbook, and completion evidence.

Out of scope:

- changing or dual-writing the current mutable `orders`, `trades`, `positions`,
  `financial_fill_idempotency`, broker, backtest, or paper paths;
- calling Alpaca, Kalshi, Binance, Polymarket, a simulator, or any provider;
- provider credentials, live submission, scheduling, feature-flag cutover,
  deployment, or shared/production migration;
- multiple routed order attempts, cancel/replace, venue fan-out, multi-leg
  atomicity, allocation across child orders, or implicit order resubmission;
- inventing canonical identities or exact decimals from legacy ticker/float
  rows, or backfilling ambiguous history;
- implementing fill correction economics, reconciliation repair, simulation
  fills, venue adapters, portfolio/risk cutover, or legacy table deletion;
- treating a recorded reconciliation failure as an authorized correction.

## Contract decisions

### Intent identity and decision evidence

1. An intent is the desired economic change, not a broker order. It contains a
   nonzero signed `DesiredQuantityDelta`: positive adds canonical instrument
   units and negative removes them. Direction is not inferred from a legacy
   BUY/SELL string.
2. The durable intent identity is `(account_id, idempotency_key)`. The UUID is
   the OVR-103 length-prefixed SHA-256 UUID of domain `execution-intent`, account
   UUID, and normalized idempotency key. A byte-for-byte/semantic identical
   retry returns the existing aggregate; changed account, environment,
   instrument, quantity, origin, quote, decision time, or metadata conflicts.
3. The intent snapshots `account_id` and `environment`. The environment must
   equal the immutable account row; callers cannot relabel scored, stress,
   shadow, or live evidence at this boundary.
4. Every intent has one canonical instrument, one decision quote snapshot, and
   one `DecisionAt`. The snapshot must belong to the instrument, have a real
   `available_at`, and satisfy `available_at <= decision_at`; provider event or
   receive time never substitutes for decision availability.
5. Origins use the OVR-103 vocabulary: `strategy_version`,
   `copy_subscription`, `portfolio_rebalance`, `risk_reduction`, `operator`,
   `settlement`, or `reconciliation`. `OriginID` is required. A strategy-origin
   intent additionally requires `StrategyVersionID == OriginID`; non-strategy
   intents may carry an explicitly related strategy version but never invent
   one.
6. Intent metadata is a JSON object and exact desired quantities support at
   most 12 fractional and 26 integer digits. No constructor, repository, or
   migration rounds an authoritative value.
7. `intent_allocated` records a nonzero exact `AllocatedQuantityDelta`. It must
   have the same sign as the desired delta and an absolute value no greater
   than the desired magnitude. Risk approval copies that allocation exactly,
   and the eventual order side/quantity must equal its sign/absolute value. A
   larger or direction-changing allocation requires a new intent identity.

### Common state machine and evidence

8. One ordered event stream is authoritative for the intent and its one order:

   ```text
   none -> proposed -> allocated -> risk_approved -> routed -> working
                                \-> risk_rejected
   routed | working -> partially_filled -> filled
   routed | working -> filled
   routed | working | partially_filled -> cancelled | expired | rejected
   any nonterminal state -> failed_reconciliation
   working | partially_filled --cancel_requested--> same state
   partially_filled --another fill--> partially_filled | filled
   any terminal state --contradiction/correction/bust--> failed_reconciliation
   ```

   `risk_rejected`, `filled`, `cancelled`, `expired`, `rejected`, and
   `failed_reconciliation` reject ordinary transitions. Only an explicit
   contradictory-state, correction, or bust observation may append after any
   other terminal state, projecting `failed_reconciliation` without editing
   the prior terminal fact. Nothing may append after `failed_reconciliation`.
   An order replacement is a new intent until a separately reviewed
   multi-attempt contract exists.
9. Event kinds are distinct from projected state:
   `intent_proposed`, `intent_allocated`, `risk_approved`, `risk_rejected`,
   `order_routed`, `order_working`, `cancel_requested`, `fill_acknowledged`,
   `fill_recorded`, `order_cancelled`, `order_expired`, `order_rejected`,
   `unknown_venue_state`, `contradictory_venue_state`,
   `fill_correction_observed`, and `fill_bust_observed`.
   `fill_acknowledged` is the first venue/simulator observation when that one
   source fact both establishes the external binding and reports a partial or
   complete fill. The last four failure kinds move any state except
   `failed_reconciliation` into that state; they never mutate history or
   manufacture economics.
10. Every event states its expected prior and next state. PostgreSQL locks the
   intent row, reads the latest ingested event, and rejects a stale expected
   state or illegal transition. The pgx repository performs the same replay
   first, but direct SQL cannot bypass the state machine.
11. Ingest order, not source timestamp, determines state. Late identical source
    events replay. A late event that would regress or contradict state must be
    represented by an explicit failure event. Terminal history is never
    deleted or silently rewritten; the exceptional failure transition remains
    append-only and inspectable.
12. Ordinary transition identity is
    `(intent_id, ordinary, source, source_namespace, source_event_id)`. Source
    revision is stored evidence but excluded from that identity, so a revised
    ordinary payload conflicts rather than becoming a second transition.
    Correction and bust observations use a separate identity:
    `(intent_id, correction|bust, source, source_namespace,
    original_source_event_id, observation_discriminator)`. They must reference
    the original fill and cannot use the ordinary class. The discriminator is
    a normalized immutable provider correction ID/sequence when available; if
    a provider revises the original execution in place, it is exactly
    `revision:<nonempty source revision>`. A duplicate correction/bust under the
    same discriminator replays only when its complete payload matches; changed
    reuse conflicts. The deterministic event UUID includes the observation
    class and discriminator, while partial unique indexes retain the stricter
    ordinary identity.
13. Each event stores source time, receive time, actor, reason code, optional
    reason text, exact JSON evidence bytes, SHA-256, JSONB copy, account,
    environment, origin, optional strategy version, and optional execution
    policy kind/version. The proposed event carries desired quantity; allocated
    and every later event carry the exact allocated quantity. Timestamps use
    UTC microsecond precision and source time cannot follow receive time.
    Created time is local persistence evidence and is excluded from replay
    equality.
    Correction/bust events additionally store the original fill UUID and
    original provider execution/source-event ID. They cannot carry an order,
    binding, fill, normalization, ledger transaction, or cumulative-quantity
    change.
14. Pre-route events have no execution policy. `order_routed` and every later
    event copy exactly the order's `simulation` or `venue` policy kind and
    nonempty version. Context mismatch is rejected both in Go and PostgreSQL.

### Order command, acknowledgement, and restart safety

15. OVR-203 permits exactly one immutable order command per intent. Its UUID is
    deterministic from intent UUID and normalized order idempotency key. The
    canonical client order ID is the order UUID string; a future venue adapter
    may encode it only through a reviewed deterministic mapping recorded in
    evidence.
16. An order contains canonical side, type, time-in-force, positive exact
    quantity, optional limit/stop prices, venue, dated venue contract, route
    quote snapshot, route time, and policy kind/version. Market/limit/stop
    field combinations are explicit. Quantity, price tick, lot size,
    instrument, venue, contract window, and point-in-time route evidence are
    validated with OVR-201/202 reference facts.
17. Route construction requires an OVR-202 `QuoteAssessment` produced at the
    exact route time by `AssessForExecution`. The assessment snapshot and venue
    contract IDs must match the persisted route facts. Migration 71 also checks
    identity, effective window, and `available_at <= routed_at`; detailed
    bid/ask/depth/status requirements remain part of the versioned policy
    assessment rather than hard-coded for every asset class.
18. The order row and `order_routed` event commit before any external side
    effect. A later OVR-205 adapter must submit/query with the stable client ID.
    After a crash, `routed`, `working`, and `partially_filled` lifecycles appear
    in `ListRecoveryCandidates`; the common layer never guesses that an
    ambiguous submission failed and never creates a second order.
19. The venue external order ID is an immutable binding created atomically with
    the first binding-establishment event. That event is `order_working` when
    acknowledgement/status arrives alone, or `fill_acknowledged` when the first
    sourced observation already contains an external ID and a partial/complete
    fill. The latter atomically inserts one binding, one fill, one normalization
    plus ledger transaction, and one lifecycle event; it transitions directly
    from `routed` to `partially_filled` or `filled` under the raw observation's
    single source identity. The binding is unique within account and venue and
    can never be rebound. A definite pre-acknowledgement rejection may move
    `routed -> rejected` without a binding. Later working, fill, cancellation,
    and expiry events require the existing binding.
    Its UUID is deterministic from domain `execution-order-binding` and the
    canonical order UUID; retrying the order with a different external ID is an
    idempotency conflict rather than a rebind.
20. `cancel_requested` records the durable command/evidence while retaining
    `working` or `partially_filled`. Only a sourced venue/simulator confirmation
    enters `cancelled`. A timeout or unknown cancel outcome enters
    `failed_reconciliation`, not `cancelled`.

### Fill identity and one economic effect

21. A canonical fill is immutable. Quantity is present and strictly positive;
    price is present and nonnegative. Exact zero price remains valid under the
    existing OVR-103 instrument/venue rules and is never a sentinel for missing
    price. Its deterministic UUID uses domain `execution-fill`, order UUID, and
    OVR-103 economic source-event UUID. The source event, normalization, ledger
    transaction, and fill are each unique links, so one raw provider/simulator
    fill can produce at most one lifecycle fill and one economic effect.
22. The supplied OVR-103 normalization must be a fill event whose account,
    canonical instrument, venue contract, venue, side, exact quantity, exact
    price, execution origin, and `ReferenceType=execution_fill` agree with the
    lifecycle. `ReferenceID` must be the deterministic fill UUID. The database
    rechecks these facts with a deferred semantic constraint.
    The lifecycle fill event's source, namespace, source-event ID, revision,
    receive time, and evidence bytes must also match the normalization's raw
    economic source event, which must contain a JSON object. One provider fact
    therefore has one raw identity across both boundaries.
23. The crash-safe order is: commit raw economic source evidence, then invoke
    `ApplyExecutionFill`, which locks/replays the lifecycle and atomically
    applies or exact-replays the normalization and ledger aggregate, inserts
    the fill and optional first venue binding, and appends `fill_recorded` or
    `fill_acknowledged`. Migration 71 adds a deferred constraint on
    `economic_event_normalizations` with `reference_type=execution_fill`, so
    neither that normalization nor the lifecycle fill can commit without its
    one-to-one counterpart. A crash leaves raw evidence alone or the complete
    graph; no normalization/fill attachment window exists.
24. Every `fill_recorded` or `fill_acknowledged` event inserts exactly one fill.
    The repository locks
    the intent, sums existing exact fill quantities, and selects
    `partially_filled` while cumulative quantity is below order quantity or
    `filled` when equal. Zero or overfill is rejected. Additional partial fills
    may keep the partial state. The event records the resulting cumulative
    quantity; PostgreSQL recomputes it at deferred commit.
25. Fill corrections and busts are not ordinary fills. Until a future economic
    reversal contract exists, observing either records exact evidence and
    terminal `failed_reconciliation`. No prior fill, normalization, ledger
    transaction, order quantity, or projected state is edited.
    OVR-205 adapters must preserve the provider's original execution ID and map
    a distinct correction ID/sequence into `ObservationDiscriminator`; when the
    provider only increments an in-place revision, they must use the explicit
    `revision:<revision>` form. They may not invent a random event ID to bypass
    an ordinary replay conflict.

### Persistence authority and rollback

26. Migration 71 creates `execution_intents`, `execution_orders`,
    `execution_order_bindings`, `execution_fills`, and
    `execution_lifecycle_events`. All are append-only. Events have a generated
    ingest sequence and deterministic source identity; current state is a
    replay/projection, never a mutable status column.
27. Deferred completeness checks reject an intent without exactly one initial
    proposed event, an order without exactly one routed event, a binding without
    exactly one `order_working` or `fill_acknowledged` establishment event, a
    fill without exactly one matching fill event, or an `execution_fill`
    normalization without exactly one fill counterpart. Direct SQL cannot
    persist a partial graph at commit.
28. Before installing the cross-boundary constraint, migration 71 refuses to
    proceed if schema 70 already contains an `execution_fill` normalization;
    there is no defensible legacy lifecycle link to backfill automatically.
    Legacy ticker/float orders and trades are not canonicalized, and missing
    account, instrument, quote, origin, source-event, or policy facts are never
    guessed. Legacy adaptation belongs to OVR-205 and simulation adoption to
    OVR-204.
29. The migration starts empty. It adds no role grant, writer activation, or
    provider capability.
30. The down migration takes `ACCESS EXCLUSIVE` locks in dependency order and
    refuses rollback if any schema-71 table contains data. Empty rollback drops
    only schema-71 functions/tables and preserves schema 70. Migration runners
    must quiesce writers; the locks make the emptiness check race-safe.

## File map

- Create `internal/execution/lifecycle/types.go` and tests: states, event kinds,
  policy/order/observation enums, exact event evidence, stable ordinary versus
  correction identities, and replay equality.
- Create `internal/execution/lifecycle/intent.go` and tests: canonical intent
  construction and decision-evidence validation.
- Create `internal/execution/lifecycle/order.go` and tests: route assessment,
  exact order mechanics, client identity, and venue binding.
- Create `internal/execution/lifecycle/fill.go` and tests: deterministic fill
  identity and exact OVR-103 normalization agreement.
- Create `internal/execution/lifecycle/aggregate.go` and tests: legal transition
  fold, cumulative fills, fail-closed observations, and recovery state.
- Modify `internal/repository/interfaces.go`: narrow execution-lifecycle
  persistence methods and recovery selector.
- Create `internal/repository/postgres/execution_lifecycle.go` and tests:
  serialized/idempotent append, aggregate reload, economic link validation,
  and recovery queries.
- Modify `internal/repository/postgres/ledger.go` and tests: extract a private
  transaction-scoped exact-normalization helper so fill normalization, ledger,
  lifecycle fill, optional binding, and event share one commit.
- Create migration 71 up/down/tests: immutable lifecycle graph, deterministic
  IDs, transition/economic semantic triggers, completeness, concurrency, and
  rollback protection.
- Bump `internal/repository/postgres/schema_version.go` to 71.
- Create `docs/runbooks/common-execution-lifecycle.md` and update ADR/overhaul
  evidence after implementation.

## Task 1: Exact intent and lifecycle-event domain

**Files:**

- Create `internal/execution/lifecycle/types.go`
- Create `internal/execution/lifecycle/types_test.go`
- Create `internal/execution/lifecycle/intent.go`
- Create `internal/execution/lifecycle/intent_test.go`

- [ ] Write deterministic identity and replay tests first:

  ```go
  func TestIntentIdentityUsesAccountAndIdempotencyKey(t *testing.T)
  func TestIntentReplayRejectsChangedEconomicPayload(t *testing.T)
  func TestOrdinaryLifecycleEventIdentityExcludesSourceRevision(t *testing.T)
  func TestLifecycleEventReplayRequiresExactEvidenceBytes(t *testing.T)
  func TestCorrectionIdentityUsesOriginalFillKindAndDiscriminator(t *testing.T)
  func TestCorrectionRevisionIsDistinctFromOrdinaryFillIdentity(t *testing.T)
  ```

- [ ] Add normalized enums, exact-byte evidence hashing, deterministic UUID
  constructors, and semantic replay comparators. Verify each RED test turns
  GREEN with the smallest contract implementation.
- [ ] Add intent invariants test-first:

  ```go
  func TestIntentRequiresAccountEnvironmentAndCanonicalInstrument(t *testing.T)
  func TestIntentRequiresNonzeroExactDesiredQuantity(t *testing.T)
  func TestIntentRequiresDecisionAvailableQuote(t *testing.T)
  func TestIntentRejectsLookaheadDecisionQuote(t *testing.T)
  func TestStrategyOriginRequiresMatchingStrategyVersion(t *testing.T)
  func TestAllocationMustKeepDirectionAndNotExceedDesiredQuantity(t *testing.T)
  func TestRiskApprovalAndOrderCopyExactAllocatedQuantity(t *testing.T)
  func TestIntentRejectsNonObjectMetadata(t *testing.T)
  ```

- [ ] Add the initial proposed event and verify it snapshots the exact intent
  context with no execution policy.

Run after every RED/GREEN slice:

```bash
go test -count=1 ./internal/execution/lifecycle
```

## Task 2: Order command, transition fold, and recovery semantics

**Files:**

- Create `internal/execution/lifecycle/order.go`
- Create `internal/execution/lifecycle/order_test.go`
- Create `internal/execution/lifecycle/aggregate.go`
- Create `internal/execution/lifecycle/aggregate_test.go`

- [ ] Define the state graph as data and test every allowed and forbidden edge,
  including the two intentional same-state event kinds.
- [ ] Add test-first transition and aggregate replay coverage:

  ```go
  func TestLifecycleFoldsAcceptedADRTransitions(t *testing.T)
  func TestLifecycleRejectsStaleExpectedState(t *testing.T)
  func TestLifecycleRejectsEventContextMismatch(t *testing.T)
  func TestLifecycleRejectsOrdinaryTransitionAfterTerminalState(t *testing.T)
  func TestCancelRequestRetainsWorkingState(t *testing.T)
  func TestUnknownContradictoryCorrectionAndBustFailClosed(t *testing.T)
  func TestCorrectionAfterFilledAppendsOneTerminalFailure(t *testing.T)
  ```

- [ ] Construct a routed order only from a risk-approved lifecycle, canonical
  instrument/contract, matching route snapshot/assessment, exact mechanics,
  and a versioned simulation or venue policy.
- [ ] Test market/limit/stop field combinations, tick/lot checks, route
  no-lookahead, contract window, stable order/client identity, and changed-key
  replay conflicts.
- [ ] Add the immutable external binding and require it for working and later
  venue/simulator events, except that a first `fill_acknowledged` observation
  creates binding and fill in one event.
- [ ] Prove one raw first-fill observation can transition directly from routed
  to partial or filled without a fabricated working event, and that concurrent
  replay creates exactly one binding/fill/event graph.
- [ ] Add `RecoveryEligible` for routed, working, and partial lifecycles; prove
  terminal and pre-route intents are excluded.

Run:

```bash
go test -race -count=1 ./internal/execution/lifecycle
```

## Task 3: Exact fill-to-ledger attachment

**Files:**

- Create `internal/execution/lifecycle/fill.go`
- Create `internal/execution/lifecycle/fill_test.go`
- Modify `internal/execution/lifecycle/aggregate.go`
- Modify `internal/execution/lifecycle/aggregate_test.go`

- [ ] Derive fill UUID before normalization so OVR-103 can use
  `execution_fill/<fill UUID>` as its immutable reference.
- [ ] Add RED tests for every required normalization match:

  ```go
  func TestFillGraphAppliesMatchingNormalizationAtomically(t *testing.T)
  func TestFillRequiresMatchingAccountInstrumentVenueAndContract(t *testing.T)
  func TestFillRequiresMatchingSideQuantityPriceAndOrigin(t *testing.T)
  func TestFillRequiresDeterministicExecutionFillReference(t *testing.T)
  func TestFillSourceNormalizationAndLedgerLinksAreOneToOne(t *testing.T)
  func TestFillAllowsPresentExactZeroPrice(t *testing.T)
  func TestFillRejectsMissingPrice(t *testing.T)
  func TestImmediatePartialFillEstablishesBindingAtomically(t *testing.T)
  func TestImmediateCompleteFillEstablishesBindingAtomically(t *testing.T)
  ```

- [ ] Add multi-fill fold tests proving first partial, repeated partial, exact
  final fill, duplicate retry convergence, and overfill rejection.
- [ ] Extract the OVR-103 normalization write into a transaction-scoped helper;
  prove standalone `ApplyEconomicNormalization` behavior is unchanged for all
  non-`execution_fill` references.
- [ ] Prove normalization/ledger/fill/binding/event rollback together on every
  injected failure and appear together after retry from retained raw evidence.
- [ ] Prove correction/bust event kinds cannot carry a `Fill` or change any
  prior exact quantity/price/economic link.
- [ ] Prove an in-place revision of the original provider execution ID uses the
  separate correction/bust discriminator, converges under duplicate replay,
  conflicts under changed same-discriminator payload, and leaves exactly one
  terminal failure with no additional normalization, ledger, or fill.

Run:

```bash
go test -race -count=1 ./internal/execution/lifecycle ./internal/ledger
```

## Task 4: Migration 71 immutable lifecycle graph

**Files:**

- Create `migrations/000071_common_execution_lifecycle.up.sql`
- Create `migrations/000071_common_execution_lifecycle.down.sql`
- Create `migrations/000071_common_execution_lifecycle_test.go`
- Modify `internal/repository/postgres/schema_version.go`

- [ ] Write source-shape tests before SQL implementation for every table,
  append-only trigger, deterministic identity, legal-state function, deferred
  completeness/semantic constraint, index, empty-only rollback, and absence of
  legacy inserts/grants.
- [ ] Implement the smallest migration that passes source-shape tests, then run:

  ```bash
  go test -count=1 -run '^TestCommonExecutionLifecycleMigrationDefines' ./migrations
  ```

- [ ] Add isolated database tests that build canonical account, instrument,
  venue-contract, quote, raw economic event, fill normalization, and ledger
  fixtures, then prove:

  - Go/PostgreSQL deterministic UUID agreement;
  - direct invalid transition/context/environment/policy inserts fail;
  - intent/order/binding/fill incomplete graphs fail at commit;
  - forged event bytes/hash/JSON and forged fill-normalization links fail;
  - an `execution_fill` normalization cannot commit without exactly one
    lifecycle fill, and a fill cannot commit without that normalization;
  - first-observation immediate partial/complete fills create one binding and
    one fill event without a fabricated working source identity;
  - present zero fill price survives exactly while SQL NULL/missing fails;
  - ordinary event identity cannot be weakened by source revision, while a
    correction/bust identity requires original fill plus an immutable
    discriminator and cannot carry economic child facts;
  - one original fill followed by an in-place revised/busted report yields
    exactly one terminal failure and no second normalization/ledger/fill;
  - concurrent duplicate events converge and competing transitions serialize;
  - updates/deletes fail on every new table;
  - no legacy order/trade row is inserted or changed;
  - nonempty downgrade refuses and empty `71 -> 70 -> 71` preserves schema 70.

- [ ] Bump `RequiredSchemaVersion` only after migration tests pass.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run '^TestCommonExecutionLifecycleMigration' ./migrations
go test -count=1 ./cmd/tradingagent ./internal/repository/postgres
```

## Task 5: PostgreSQL repository and restart convergence

**Files:**

- Modify `internal/repository/interfaces.go`
- Create `internal/repository/postgres/execution_lifecycle.go`
- Create `internal/repository/postgres/execution_lifecycle_test.go`

- [ ] Add a narrow repository interface for proposing an intent, applying one
  validated transition atomically, loading the full replayed lifecycle, finding
  an intent by account/idempotency key, applying one normalized fill graph, and
  listing recovery candidates.
- [ ] Implement proposal replay as insert-or-load-and-compare. A mismatched key
  wraps `repository.ErrIdempotencyConflict`.
- [ ] In `ApplyExecutionTransition`, begin a transaction, lock the intent row,
  reload/replay the aggregate, validate exact expected state and optional
  order/binding fact, insert the fact plus event, commit, and reload.
- [ ] In `ApplyExecutionFill`, use the same locked replay and the private
  transaction-scoped OVR-103 helper to atomically write or replay the
  normalization/ledger, optional first binding, fill, and one lifecycle event.
- [ ] Add repository RED/GREEN tests for identical replay, changed replay,
  competing events, partial/final fills, recovery after constructing a fresh
  repository instance, and atomic rollback when any child insert fails.
- [ ] Run eight-writer race tests for proposal, route, acknowledgement, and fill
  retries, including immediate partial and immediate complete first fills.
  Assert one intent, one order, one binding, one fill, one lifecycle event per
  source identity, one normalization, and one ledger transaction.
- [ ] Run an eight-writer correction/bust retry against the same original fill
  and discriminator; assert one failure event, no changed economic rows, exact
  duplicate convergence, and changed-payload conflict.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'ExecutionLifecycle' ./internal/repository/postgres
```

## Task 6: Rehearsal, documentation, independent review, and synchronization

**Files:**

- Create `docs/runbooks/common-execution-lifecycle.md`
- Modify `docs/adr/017-common-execution-lifecycle.md`
- Modify `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify this plan with implementation evidence

- [ ] Document the raw-event -> atomic normalization/ledger/lifecycle-fill
  crash boundary, source/idempotency requirements, recovery candidate handling,
  failure-state policy, read-only inspection queries, and empty-only rollback.
- [ ] Rehearse migration `70 -> 71`, create complete intents through immediate
  fill and multiple-partial-fill paths, retry every identity, construct a fresh
  repository, verify the same aggregates and one economic effect per fill, then
  prove a nonempty rollback refuses without deleting evidence.
- [ ] On a disposable empty schema, prove `71 -> 70 -> 71`; on the persistent
  phase database, retain all prior schema-64..70 facts and the new schema-71
  lifecycle facts.
- [ ] Run focused and repository race suites, then whole-repository gates:

  ```bash
  go test -race -count=1 ./internal/execution/lifecycle \
    ./internal/repository/postgres ./migrations
  task test:race
  task build
  task vet
  task lint
  task fmt:check
  govulncheck ./...
  ```

- [ ] Re-run the established frontend Node 22 test/lint/build gates even though
  OVR-203 changes no frontend source. Preserve inherited failures separately
  from regressions introduced by this slice.
- [ ] Start the rebuilt binary only with the global kill switch active,
  scheduler disabled, no venue credentials, isolated schema-71 PostgreSQL, and
  isolated Redis. Check `/health`, `/healthz`, and `/api/v1/health`, then stop it
  cleanly. This is local runtime evidence, not deployment proof.
- [ ] Obtain independent diff approval with no unresolved P0/P1 findings.
- [ ] Commit only the reviewed OVR-203 slice, push
  `codex/augr-overhaul`, verify local/remote hash equality and `0 0`
  divergence, then continue with the next dependency-ready backlog item.

## Acceptance evidence to record after implementation

- [ ] Domain and PostgreSQL enforce the same deterministic identity and state
  transition contract.
- [ ] Identical proposal/event/order/binding/fill retries converge; mismatched
  identity reuse fails closed.
- [ ] A fresh repository reconstructs the same state and lists only genuinely
  recoverable lifecycles.
- [ ] Multiple exact partial fills end at exactly the order quantity; zero and
  overfill fail. A present zero fill price remains exact; a missing price fails.
- [ ] A first venue observation that is already partial or filled atomically
  establishes the binding and fill with one source event and no invented
  working acknowledgement.
- [ ] Every fill maps one-to-one to raw source evidence, applied normalization,
  and ledger transaction in one commit; retries cannot duplicate or orphan
  economic effects.
- [ ] Unknown, contradictory, correction, and bust observations are visibly
  distinct terminal reconciliation failures.
- [ ] A provider revision of an existing fill identity is retained through the
  explicit correction/bust discriminator without weakening ordinary replay or
  creating another fill, normalization, or ledger transaction.
- [ ] Migration 71 is additive, immutable, concurrency-safe, starts empty, and
  rolls back only while empty without touching schema 70.
- [ ] Legacy runtime behavior and all venue/scheduler/live boundaries remain
  unchanged and disabled.
- [ ] Focused race tests, repository-wide gates, isolated migration rehearsal,
  kill-switched runtime smoke, and independent review results are recorded
  honestly.
