# Phase 2 Common Simulation Venue Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep every public
> contract test-first, obtain architecture approval before implementation and
> diff approval before shipping, and preserve the local-only deployment
> boundary.

**Goal:** Complete OVR-204 with one exact, deterministic, content-addressed
simulation venue whose backtest and internal-paper adapters consume the same
OVR-202 observations, emit the same OVR-203 lifecycle transitions and OVR-103
economic normalizations, and produce identical economic outcomes for one
golden replay.

**Architecture:** Add `internal/simulation` as the sole implementation home for
simulated order execution. A validated immutable policy selects explicit
asset-class capabilities, point-in-time quote requirements, explicit UTC
session windows, maximum depth participation, fixed route latency, and exact
fee rules. Its canonical bytes produce the policy version recorded by the
OVR-203 order and every subsequent lifecycle event. Migration 72 durably
retains those exact bytes in an immutable content-addressed artifact and
requires every simulation order's policy version to resolve to that artifact,
so restart recovery never depends on a mutable “current” config. The venue
consumes a routed OVR-203 aggregate plus canonical account, instrument, dated
venue contract, and OVR-202 quote/depth snapshot. It never invents a quote or
spread. It deterministically acknowledges a resting order, expires it at its
declared session boundary, rejects an unsupported or unsatisfied instruction,
or emits one exact fill per consumed depth level. Each fill owns byte-stable
simulator evidence, one raw OVR-103 source event, one exact ledger
normalization, and one OVR-203 transition. Thin backtest and internal-paper
adapters delegate to this same venue without changing policy or economics;
the paper adapter additionally enforces the immutable ADR-018 account mode,
evidence class, and storage namespace. Existing float/ticker broker APIs remain
compatibility boundaries while their reusable fill, depth, latency, queue,
adverse-selection, and options primitives move from `internal/backtest` to
`internal/simulation`; no legacy row is promoted into canonical evidence.

**Tech stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256, canonical JSON,
PostgreSQL 17/TimescaleDB, pgx v5, golang-migrate, Task, and TDD against an
isolated schema-71 database upgraded to schema 72.

---

## Scope and sequencing

This plan covers OVR-204 only. It depends on OVR-203's immutable common
lifecycle and consumes OVR-201 reference mechanics, OVR-202 quote/depth
observations, and OVR-103 fill normalizations. OVR-205 will adapt external
Alpaca and Kalshi venue observations; OVR-206 will add capital-tier and margin
profiles. OVR-303 will make full experiment runs reproducible. OVR-405 will add
explicit multi-leg options behavior, and OVR-505 will add the durable
prediction-market book/fee recorder.

In scope:

- one content-addressed simulation policy schema with no implicit economic
  defaults;
- one immutable simulation-policy artifact repository containing canonical
  bytes, parsed JSON, schema, digest, deterministic identity, and exact replay
  conflict behavior;
- explicit support for equity, ETF, option, crypto spot, and prediction
  contracts when a corresponding asset policy is present;
- fail-closed rejection for futures, missing asset policies, unsupported order
  types, unsupported passive assumptions, and malformed or mismatched
  references;
- OVR-202 source, availability, age, bid, ask, status, and depth assessment at
  every simulated execution observation;
- exact tick/lot/multiplier/currency enforcement from the dated OVR-201 venue
  contract;
- deterministic fixed-latency eligibility, explicit UTC session windows,
  deterministic DAY expiry, continuous 24/7 capability, and exact depth
  participation;
- market and limit orders, including marketable limit prices, per-level partial
  fills, resting acknowledgements, IOC cancellation, and FOK all-or-none
  rejection;
- exact per-order, per-unit, and notional-basis-point fees with explicit
  rounding scale;
- deterministic simulator order, observation, and fill-source identities;
- byte-stable evidence that records policy version, quote identity and age,
  consumed depth, fill mechanics, fee mechanics, and latency decision;
- raw-event-first persistence followed by atomic normalization/ledger/fill
  application through the existing OVR-203 repository;
- restart-safe replay of an interrupted multi-level observation without a
  duplicate fill or economic effect;
- thin canonical adapters in `internal/backtest` and
  `internal/execution/paper` that expose exactly the same policy version and
  delegate to the same venue;
- explicit paper-scored versus paper-stress adapter isolation, including
  preserved account environment, evidence class, and storage namespace;
- relocation of the existing reusable backtest fill, depth, latency, queue,
  adverse-selection, and option-fill primitives into `internal/simulation`,
  with source-compatible aliases/wrappers and unchanged legacy tests;
- removal of the legacy paper market-order `$1.00` fallback so missing prices
  fail closed even before full runtime cutover;
- a golden replay and a real PostgreSQL persistence/restart test;
- documentation, local qualification, independent review, commit, and push.

Out of scope:

- production or shared-database migration, simulation writer activation,
  deployment, scheduling, credentials, or live/external-paper submission;
- a mutable “current policy” pointer or in-place policy update; policy artifacts
  are append-only and selected only by the exact version already recorded on
  the order;
- silently canonicalizing ticker/float legacy orders, bars, positions, or
  broker balances;
- treating OHLCV close, a configured fixed spread, a limit price, or a default
  price as executable quote/depth evidence;
- passive queue fills without an explicit historical trade/queue observation;
  a nonmarketable limit may rest, but quote movement alone is not proof of a
  maker fill at an earlier price;
- synthetic random latency or ghost-fill draws in the canonical v1 venue; the
  existing stochastic primitives remain available and versioned as legacy
  compatibility tools, while canonical stochastic policies require a later
  keyed-draw contract that is independent of call order;
- stop/stop-limit activation, cancel/replace, multiple routed child orders,
  venue fan-out, or smart order routing;
- multi-leg atomicity, orphan-leg repair, assignment, exercise, expiration,
  dividends, corporate-action cash flows, prediction resolution, funding,
  borrow, or futures margin/variation settlement. Existing OVR-103 economic
  event types remain authoritative for supported settlements; later backlog
  items add the missing scenario contracts;
- capital sufficiency, buying power, portfolio margin, promotion decisions, or
  legacy accounting cutover;
- deleting legacy broker APIs or claiming that compatibility-bar simulations
  are promotion-quality canonical evidence.

## Contract decisions

### Policy identity and capabilities

1. A simulation policy is immutable input, not global mutable configuration.
   It has a normalized schema name and an ordered set of asset policies. No
   slippage, fee, quote, latency, depth, or rounding value is silently
   defaulted.
2. Canonical policy bytes use a fixed struct schema, normalized enum/string
   values, exact decimal strings, sorted/deduplicated status and order-type
   lists, integer nanoseconds for durations, and UTC RFC3339 microsecond strings
   for explicit session boundaries. Maps, binary floating point, local-time
   interpretations, and process-local values are forbidden from the hashed
   form.
3. The durable policy version is
   `<schema>@sha256:<lowercase-hex-sha256(canonical-bytes)>`. It must equal the
   OVR-203 order's `PolicyVersion`, and the order must use
   `PolicyKind=simulation`. A friendly label may appear in evidence but is not
   authority.
4. A policy artifact ID is the OVR-103 length-prefixed deterministic UUID of
   domain `simulation-policy-artifact` and the full policy version. Its stored
   schema, SHA-256, canonical bytes, and parsed JSON must agree with that
   version and ID in both Go and PostgreSQL.
5. Migration 72 creates an append-only `simulation_policy_artifacts` table.
   Identical registration replays; reusing an ID/version/digest with different
   canonical bytes conflicts. Updates and deletes fail. Canonical bytes remain
   the authority; parsed JSON is an inspectable, equality-checked copy.
6. Every newly inserted OVR-203 order with `PolicyKind=simulation` must resolve
   its exact `PolicyVersion` to one artifact. PostgreSQL enforces this in the
   order insert transaction. Migration 72 refuses to proceed if schema 71
   already contains a simulation order because the governing bytes cannot be
   reconstructed defensibly; it never backfills from a guessed config. Venue
   policy orders do not reference a simulation artifact.
7. Recovery loads the artifact by the version already recorded on the order,
   revalidates its ID/digest/bytes, and constructs the venue from those bytes.
   A changed or removed process-local “current” policy cannot affect an
   existing routed order. Missing or corrupt durable bytes fail recovery
   closed.
8. Each asset policy explicitly lists supported order types and time-in-force
   values, OVR-202 quote requirements, depth participation in `(0,1]`, fixed
   nonnegative route latency, a calendar rule, and exact fees. Duplicate asset
   classes or enum values are invalid.
9. A calendar rule is either `explicit_sessions` or `continuous_24x7`.
   Explicit sessions are nonoverlapping `[open_at, close_at)` UTC microsecond
   windows with stable labels and finite policy coverage; a holiday simply has
   no session, and a half-day has its actual shorter window. Continuous 24/7
   has no close boundary and may not advertise DAY support. DAY is valid only
   for an explicit session containing `Order.RoutedAt`.
10. V1 supports equity, ETF, option, crypto spot, and prediction contracts only
   when configured. Futures and unknown/quarantined assets fail closed.
   Options use venue lot size and multiplier, so a one-contract lot and the
   contract multiplier are exact rather than inferred from a ticker. V1 is
   single-leg only.
11. Policy fee fields are nonnegative exact decimals with at most 12 fractional
   and 26 integer digits. `NotionalBPS` is divided by exactly `10000`; the sum
   of first-fill per-order fee, per-unit fee, and notional fee is rounded once
   to the declared scale `0..12` using half-away-from-zero. A zero result is
   omitted because OVR-103 cost components are strictly positive.
12. Per-order fees apply only when the aggregate has no prior fill. They apply
   to the first deterministic level fill, including when several levels from
   one observation are returned together. Retries and later snapshots cannot
   charge it again.

### Observation and execution eligibility

13. Every observation validates the account, instrument, contract, aggregate,
   snapshot, and policy before producing a transition. Account, instrument,
   venue, contract, currency, and policy identities must agree exactly.
14. The policy calls `QuoteSnapshot.AssessForExecution` at the explicit
   simulation evaluation time. Availability is always required. Canonical v1
   asset policies require real source, bid, ask, both depth sides,
   market/session status allowlists, exchange time when a positive max age is
   configured, and the OVR-201 venue contract. Missing spread is therefore
   never zero, and a stale or unavailable observation cannot fill.
15. Evaluation time, source time, receive time, route time, contract windows,
    and snapshot availability use UTC microsecond precision. A snapshot can
    fill only when `available_at >= routed_at + fixed_latency` and
    `available_at <= evaluated_at`. An earlier valid snapshot may establish a
    working acknowledgement but cannot fill before latency eligibility.
16. Before quote execution, the venue resolves the route session. An explicit-
    session order routed outside all policy windows is rejected with sourced
    evidence. Continuous 24/7 policies have no session boundary. A later
    process-local calendar cannot reinterpret the pinned policy artifact.
17. At or after an explicit route session's `close_at`, an unfilled or
    partially filled DAY order emits one deterministic sourced
    `order_expired` transition before considering later quote data. Source time
    is the exact close and receive time is the evaluation time. Restart just
    before or after close converges on the same identity. GTC remains eligible
    in a later configured session; IOC/FOK never rest past their first
    observation.
18. The simulator consumes only the opposite depth side: asks ascending for a
    buy and bids descending for a sell. Each level is independently capped at
    `floor_to_lot(level_size * max_participation)`. Zero residual capacity is
    skipped. It never exceeds the order's exact remaining quantity.
19. Each consumed level becomes one fill at that exact level price. The venue
    never reports a weighted-average fill as one off-tick execution. Aggregate
    VWAP is derived from the immutable level fills.
20. Market orders cross every eligible level until filled or capacity ends.
    Limit orders cross only asks `<= buy limit` or bids `>= sell limit`.
    Price improvement is preserved; a fill is never worsened past the limit.
21. A nonmarketable DAY/GTC limit becomes `working` through one deterministic
    simulator binding. Later quote crossing may fill it. This does not claim
    maker priority or spread capture: the fill remains a crossing/taker fill at
    then-executable depth. Passive queue execution requires a future explicit
    trade/queue-observation contract.
22. FOK preflights all eligible capacity and rejects from `routed` without a
    binding or economic effect if full remaining quantity is unavailable. IOC
    may fill available quantity, then appends a sourced cancellation for any
    remainder. DAY/GTC may remain working/partially filled for later snapshots.
23. Repeating the same `(order, snapshot, depth side, level)` identity cannot
    create a second fill. The venue recognizes already-recorded simulator
    source IDs and skips them. A changed snapshot under a new durable OVR-202
    identity may produce another partial fill.
24. Stop and stop-limit orders, unsupported TIF/calendar combinations,
    zero/negative depth sizes after policy participation, currency mismatch,
    and unavailable asset policies return stable typed errors or sourced
    rejection transitions as specified by tests. No panic, best-effort fill,
    or guessed value is valid.

### Lifecycle and economic graph

25. The simulator external order ID is `simulation/<canonical-order-uuid>`.
    It is deterministic and contains no process sequence. A resting
    acknowledgement and an immediate first fill use the same binding identity.
26. Simulator source is `simulation`; namespaces include the immutable account
    environment, storage namespace, evidence class, and versioned observation
    kind. Source event IDs are deterministic printable paths containing the
    canonical order UUID, OVR-202 snapshot UUID when applicable, kind, and
    depth level. The policy version is in evidence, not substituted for the
    source identity.
27. Evidence is marshalled from fixed structs, never a map. It includes order,
    snapshot, provider, venue, exchange/receive/availability/evaluation times,
    quote ages, policy artifact ID/version, account environment/evidence
    class/storage namespace, route session/close, latency, side, order type/TIF,
    level, displayed size, participation, quantity, price, multiplier, fee
    components, and terminal/resting reason. Repeated construction is
    byte-identical.
28. Every fill constructs `ledger.EconomicSourceEvent` first, derives the
    OVR-203 fill ID, builds `NewFillEconomicNormalization` with
    `ReferenceType=execution_fill`, and passes the exact same evidence and
    observation identity to `lifecycle.RecordFill`.
29. Fill effective time is the snapshot exchange time; receive/observed time
    is the snapshot receive time. If exchange time is absent while the policy
    permits it, effective time is the receive time and evidence says so
    explicitly. Effective time can never follow receive time.
30. The normalizer version is a stable simulation-fill schema plus the policy
    digest. Side, quantity, price, instrument, venue contract, origin, and
    optional exact fee must agree with the routed aggregate and OVR-103
    mechanics. Account, instrument, and contract currencies must agree before
    simulation.
31. The venue returns an ordered result containing zero or more OVR-203
    transitions plus the fully replayed next aggregate. It validates every
    transition by applying it through `lifecycle.ApplyTransition`; callers do
    not receive a graph the domain itself rejects.
32. A narrow persistence coordinator records the policy artifact before route,
    records each raw source event before its fill, then calls
    `ApplyExecutionFill` for fill transitions or
    `ApplyExecutionTransition` otherwise. An interruption may leave raw source
    evidence alone, as designed by OVR-103/203. Retrying from a reloaded
    aggregate converges without another fill, fee, ledger transaction, or
    binding.

### Path parity and legacy boundary

33. `internal/backtest` and `internal/execution/paper` each expose a canonical
    simulation adapter with the same request/result types. Constructors accept
    the same immutable policy and expose the same digest. Adapters add no price,
    fee, timing, lifecycle, or evidence behavior.
34. The internal-paper adapter accepts only validated `paper_scored` or
    `paper_stress` account/aggregate pairs. It preserves environment, evidence
    class, and storage namespace in the result and source namespace. It rejects
    shadow/live or mismatched accounts. `paper_stress` always remains
    `synthetic_stress` and can never be exposed as promotion evidence.
35. The golden replay creates one routed lifecycle fixture and feeds identical
    timestamped snapshots to both adapters. It compares policy versions,
    normalized intent/order semantics, ordered fill `(quantity, price, fee)`
    tuples, terminal state, total quantity, gross cash, fees, environment,
    evidence class, storage namespace, and a canonical outcome hash. Parity is
    compared only within the same ADR-018 account mode. Scored and stress
    outcomes intentionally hash differently even if their fill economics are
    equal, so they cannot enter one promotion-quality population.
36. The existing backtest fill engine, depth fill, latency, queue,
    adverse-selection, and option-fill code moves to `internal/simulation`.
    Backtest names remain source-compatible aliases or wrappers. Existing
    tests continue to prove legacy behavior, but reports remain labelled with
    the legacy backtest input version until OVR-303 cuts the full experiment
    runner to canonical manifests.
37. The legacy paper broker delegates reusable fill math where safe and loses
    its missing-market-price fallback. It is not relabelled as canonical and
    cannot produce promotion evidence merely because a compatibility test
    passes. Runtime cutover requires canonical instrument/snapshot inputs and a
    separately reviewed writer activation.
38. Migration 72 starts with no policy artifact and no simulation writer. Its
    empty downgrade locks the artifact/order boundary, refuses if an artifact
    or simulation order exists, removes only the schema-72 trigger/table, and
    preserves schema 71. A nonempty local rehearsal deliberately cannot be
    downgraded without a separately reviewed data-retention decision.

## File map

- Create `internal/simulation/policy.go` and tests: exact validation,
  normalization, canonical bytes, digest/artifact identity, capabilities,
  explicit/continuous calendar rules, quote requirements, and fee arithmetic.
- Modify `internal/repository/interfaces.go`: immutable simulation-policy
  artifact registration and exact-version lookup.
- Create `internal/repository/postgres/simulation_policy.go` and tests:
  register/replay/conflict/load behavior and order-reference enforcement.
- Create migration 72 up/down/tests: content-addressed immutable artifacts,
  exact byte/hash/JSON/UUID checks, simulation-order policy lookup, safe
  precondition, empty-only rollback, and schema-version bump.
- Create `internal/simulation/venue.go` and tests: eligibility, latency, depth
  selection/participation, market/limit/TIF behavior, stable errors, and
  deterministic result folding.
- Create `internal/simulation/evidence.go` and tests: fixed-schema canonical
  evidence and stable observation identities.
- Create `internal/simulation/fill.go` and tests: raw source event, exact fee,
  OVR-103 normalization, OVR-203 fill transition, multi-level partials, and
  duplicate observation handling.
- Create `internal/simulation/persist.go` and tests: raw-first transition
  sequencing, retry behavior, and injected-failure boundaries.
- Create `internal/simulation/outcome.go` and tests: canonical economic outcome
  projection and hash.
- Create `internal/backtest/common_simulation.go` and tests: thin canonical
  adapter and policy exposure.
- Create `internal/execution/paper/common_simulation.go` and tests: thin
  canonical adapter and policy exposure.
- Create `internal/simulation/golden_replay_test.go` in external test package:
  identical backtest/internal-paper replay.
- Create `internal/repository/postgres/simulation_venue_test.go`: real
  schema-72 artifact/route/raw-first persistence, changed-current-policy
  recovery, session expiry, interrupted multi-level restart, and exact replay
  convergence.
- Move reusable implementations from `internal/backtest/{fill_engine,
  depth_fill,latency_model,queue_position,adverse_selection,options_fill}.go`
  to `internal/simulation/legacy_*.go`; leave aliases/wrappers in backtest and
  preserve tests/callers.
- Modify `internal/execution/paper/broker.go` and tests: remove the `$1.00`
  market fallback and reuse moved legacy simulation math where compatible.
- Modify `internal/execution/paper/options.go` and tests: delegate single-leg
  compatibility math to the moved option-fill primitive without changing the
  public result contract.
- Create `docs/runbooks/common-simulation-venue.md`.
- Modify `docs/adr/017-common-execution-lifecycle.md` and
  `docs/adr/018-scored-and-stress-paper-modes.md` with the implemented local
  boundary.
- Modify `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md` and this
  plan with reviewed acceptance evidence.

## Architecture review — 2026-08-15

The independent architecture review approved this plan with no unresolved
P0/P1 findings after three required revisions were incorporated:

- migration 72 now retains the exact content-addressed policy bytes and makes
  every simulation order resolve its recorded version to that immutable
  artifact, so restart recovery does not trust mutable current configuration;
- explicit UTC session windows and continuous 24/7 rules now define DAY route
  eligibility, normal/half-day/holiday behavior, deterministic expiry, and
  restart convergence;
- the internal-paper adapter, simulator evidence namespace, result projection,
  and outcome hash now preserve ADR-018 environment, evidence class, and
  storage namespace so scored and stress populations cannot mix.

The reviewer confirmed that these additions remain within OVR-204 and do not
absorb external venue adapters, capital/margin profiles, experiment manifests,
multi-leg options, or prediction-market recording/settlement.

## Task 1: Content-addressed exact simulation policy

**Files:**

- Create `internal/simulation/policy.go`
- Create `internal/simulation/policy_test.go`

- [ ] Write RED tests first:

  ```go
  func TestPolicyVersionIsCanonicalAndContentAddressed(t *testing.T)
  func TestPolicyArtifactIDUsesFullVersion(t *testing.T)
  func TestPolicyVersionIgnoresInputOrderingButNotEconomics(t *testing.T)
  func TestPolicyRejectsDuplicateOrUnsupportedAssetCapabilities(t *testing.T)
  func TestPolicyValidatesExplicitSessionsHolidaysAndHalfDays(t *testing.T)
  func TestContinuous24x7PolicyRejectsDAY(t *testing.T)
  func TestPolicyRequiresExplicitQuoteDepthStatusAndAgeRules(t *testing.T)
  func TestPolicyRejectsImplicitOrInexactFeeAndParticipationValues(t *testing.T)
  func TestPolicyComputesExactFirstAndLaterFillFees(t *testing.T)
  func TestPolicyRoundTripsFromCanonicalArtifactBytes(t *testing.T)
  ```

- [ ] Implement normalized enums and exact asset/fee policy values with no
  silent defaults.
- [ ] Marshal a private fixed-schema canonical form and expose cloned bytes plus
  the `<schema>@sha256:<digest>` version and deterministic artifact UUID.
- [ ] Prove input slice ordering and duplicate allowed-status entries normalize
  deterministically while every material capability/economic change changes
  the version.
- [ ] Validate sorted nonoverlapping explicit UTC session windows, holiday gaps,
  half-day closes, route-session lookup, and continuous 24/7 restrictions.
- [ ] Parse a venue only from revalidated canonical artifact bytes; reject any
  schema/version/digest/ID disagreement.
- [ ] Prove exact fee arithmetic, scale, first-fill behavior, multiplier use,
  zero omission, and overflow/shape rejection.

Run after every RED/GREEN slice:

```bash
go test -count=1 ./internal/simulation
```

## Task 2: Durable immutable policy artifacts and order reference

**Files:**

- Modify `internal/repository/interfaces.go`
- Create `internal/repository/postgres/simulation_policy.go`
- Create `internal/repository/postgres/simulation_policy_test.go`
- Create `migrations/000072_simulation_policy_artifacts.up.sql`
- Create `migrations/000072_simulation_policy_artifacts.down.sql`
- Create `migrations/000072_simulation_policy_artifacts_test.go`
- Modify `internal/repository/postgres/schema_version.go`

- [ ] Write migration source-shape and Go repository RED tests first:

  ```go
  func TestSimulationPolicyMigrationDefinesImmutableContentAddressedArtifacts(t *testing.T)
  func TestSimulationPolicyMigrationRequiresArtifactForSimulationOrder(t *testing.T)
  func TestSimulationPolicyRepoRegistersLoadsAndReplaysExactArtifact(t *testing.T)
  func TestSimulationPolicyRepoRejectsChangedBytesForSameIdentity(t *testing.T)
  func TestSimulationPolicyRepoConcurrentRegistrationConverges(t *testing.T)
  ```

- [ ] Create the minimal artifact table with deterministic ID, full version,
  schema, SHA-256, exact canonical `BYTEA`, parsed JSONB copy, UTC creation
  evidence, append-only mutation trigger, and indexes. PostgreSQL independently
  recomputes digest, ID, version, JSON validity/object shape, and byte/JSON
  equivalence.
- [ ] Add a simulation-order insert constraint trigger requiring an exact
  artifact for `policy_kind='simulation'` while leaving venue policies
  independent. Direct SQL cannot insert an unknown or digest-mismatched policy
  version.
- [ ] Refuse migration when any preexisting schema-71 simulation order exists;
  do not infer its policy bytes. Prove a venue-policy order does not block.
- [ ] Implement insert-or-load exact replay and `GetByVersion`; changed reuse
  wraps `repository.ErrIdempotencyConflict`. Run eight concurrent registrations
  and assert one row.
- [ ] Prove recovery after the caller's configured current policy changes or is
  removed: load the routed version's bytes by order policy version and rebuild
  the original policy exactly.
- [ ] Prove direct forged bytes/digest/version/UUID/JSON, update/delete, and
  simulation order without an artifact fail. Prove nonempty rollback refuses;
  isolated empty `72 -> 71 -> 72` preserves all schema-71 objects.
- [ ] Bump `RequiredSchemaVersion` only after real PostgreSQL migration and
  repository tests pass.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'SimulationPolicy' ./migrations ./internal/repository/postgres
```

## Task 3: Deterministic quote/depth venue and lifecycle graph

**Files:**

- Create `internal/simulation/venue.go`
- Create `internal/simulation/venue_test.go`
- Create `internal/simulation/evidence.go`
- Create `internal/simulation/evidence_test.go`
- Create `internal/simulation/fill.go`
- Create `internal/simulation/fill_test.go`
- Create `internal/simulation/outcome.go`
- Create `internal/simulation/outcome_test.go`

- [ ] Add routed-aggregate fixture builders using real OVR-201/202/203
  constructors rather than hand-forged child structs.
- [ ] Add RED eligibility tests:

  ```go
  func TestVenueRequiresSimulationPolicyDigestMatch(t *testing.T)
  func TestVenueFailsClosedOnMissingStaleOrFutureQuote(t *testing.T)
  func TestVenueRejectsReferenceVenueCurrencyTickAndLotMismatch(t *testing.T)
  func TestVenueRejectsUnsupportedAssetOrderTypeAndTimeInForce(t *testing.T)
  func TestVenueHonorsFixedLatencyWithoutEarlyFill(t *testing.T)
  func TestVenueRejectsRouteOnExplicitCalendarHoliday(t *testing.T)
  func TestVenueRejectsRouteAfterSessionClose(t *testing.T)
  func TestContinuous24x7VenueHasNoImplicitClose(t *testing.T)
  ```

- [ ] Add RED fill-mechanics tests:

  ```go
  func TestMarketBuyConsumesAskDepthAsExactLevelFills(t *testing.T)
  func TestMarketSellConsumesBidDepthAsExactLevelFills(t *testing.T)
  func TestDepthParticipationRoundsDownToVenueLot(t *testing.T)
  func TestLimitCrossingPreservesPriceImprovementAndLimit(t *testing.T)
  func TestNonMarketableLimitAcknowledgesWithoutInventingFill(t *testing.T)
  func TestFOKRejectsWithoutPartialEconomicEffect(t *testing.T)
  func TestIOCPartiallyFillsThenCancelsRemainder(t *testing.T)
  func TestDayOrderRemainsPartialForLaterSnapshot(t *testing.T)
  func TestDayOrderExpiresAtNormalSessionClose(t *testing.T)
  func TestDayOrderExpiresAtExplicitHalfDayClose(t *testing.T)
  func TestLaterSnapshotCannotFillExpiredDayOrder(t *testing.T)
  func TestSameSnapshotLevelCannotFillTwice(t *testing.T)
  ```

- [ ] Build one transition per depth level, applying each through the OVR-203
  aggregate before constructing the next.
- [ ] Add raw-event/normalization tests:

  ```go
  func TestSimulationFillCreatesExactRawNormalizationLifecycleGraph(t *testing.T)
  func TestSimulationFillEvidenceIsByteStable(t *testing.T)
  func TestSimulationFillChargesPerOrderFeeOnce(t *testing.T)
  func TestSimulationFillUsesContractMultiplierAndExactCurrency(t *testing.T)
  func TestPredictionFillRetainsExactZeroPrice(t *testing.T)
  func TestSimulationFillIDsConvergeAndChangedObservationConflicts(t *testing.T)
  ```

- [ ] Add canonical outcome projection/hash tests covering ordered fills,
  quantity, gross cash, fee, policy version, final state, environment, evidence
  class, and storage namespace. Local creation timestamps and opaque source IDs
  are not economic hash inputs; ADR-018 classification always is.

Run:

```bash
go test -race -count=1 ./internal/simulation ./internal/execution/lifecycle \
  ./internal/ledger ./internal/marketdata ./internal/instrument
```

## Task 4: Raw-first persistence and restart convergence

**Files:**

- Create `internal/simulation/persist.go`
- Create `internal/simulation/persist_test.go`
- Create `internal/repository/postgres/simulation_venue_test.go`

- [ ] Define the narrow local persistence interface from the existing economic
  event and execution lifecycle methods; do not introduce a repository cycle.
- [ ] Add unit tests with a recording/fault-injecting repository proving raw
  source evidence is always attempted before an economic fill, non-fill
  transitions never write raw economic evidence, and a stopped sequence can be
  retried without reordering.
- [ ] On isolated schema 72, register policy A, persist a complete routed
  lifecycle, change the caller's current policy to B, reload A by the order's
  recorded version through a fresh repository, and prove the original venue is
  reconstructed from durable bytes.
- [ ] Evaluate a multi-level partial fill, interrupt after the first committed
  level, reload through a fresh repository, and reevaluate the same snapshot.
  Assert the already-recorded source identity is skipped and only the remaining
  level commits.
- [ ] Restart one DAY order immediately before and after a normal close and a
  half-day close; assert exactly one expiry identity, no later fill, and no
  economic row. A holiday/out-of-session route must reject deterministically.
- [ ] Retry the full observation concurrently through eight workers. Assert one
  binding, one fill per eligible level, one normalization and ledger
  transaction per fill, one per-order fee, exact cumulative quantity, and no
  orphaned or duplicate economic effect.
- [ ] Add FOK/no-liquidity and stale-quote persistence cases proving they create
  no raw fill or ledger row.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'SimulationVenue' ./internal/repository/postgres
```

## Task 5: Canonical path adapters and golden replay

**Files:**

- Create `internal/backtest/common_simulation.go`
- Create `internal/backtest/common_simulation_test.go`
- Create `internal/execution/paper/common_simulation.go`
- Create `internal/execution/paper/common_simulation_test.go`
- Create `internal/simulation/golden_replay_test.go`

- [ ] Add thin adapter tests proving each constructor validates the same policy,
  exposes the same digest, delegates the same request, and does not alter
  evidence or transitions.
- [ ] Add paper-adapter tests proving only `paper_scored` and `paper_stress`
  account/aggregate pairs are accepted; environment, evidence class, and
  storage namespace are preserved; mismatches plus shadow/live accounts fail;
  stress results can never report promotion evidence.
- [ ] Build one timestamped fixture with route-time and later depth snapshots,
  a partial first fill, and an exact final fill. Feed independent cloned routed
  aggregates to the backtest and internal-paper adapters.
- [ ] Compare policy version, normalized intent/order semantics, every ordered
  `(quantity, price, fee)` tuple, final state, total quantity, gross cash,
  total fee, environment, evidence class, storage namespace, and canonical
  outcome hash within one scored fixture and one separate stress fixture.
- [ ] Prove otherwise identical scored and stress outcomes hash differently and
  cannot be merged into one comparison population.
- [ ] Add negative parity cases for stale data, insufficient FOK depth,
  nonmarketable GTC limits, unsupported stop orders, and missing asset policy;
  both paths must return the same typed result/error and no economic effect.
- [ ] Run the golden replay repeatedly and concurrently to prove that adapter
  call ordering cannot change outputs.

Run:

```bash
go test -race -count=20 ./internal/simulation ./internal/backtest \
  ./internal/execution/paper
```

## Task 6: Relocate legacy models and close the paper fallback

**Files:**

- Create `internal/simulation/legacy_fill_engine.go`
- Create `internal/simulation/legacy_depth_fill.go`
- Create `internal/simulation/legacy_latency_model.go`
- Create `internal/simulation/legacy_queue_position.go`
- Create `internal/simulation/legacy_adverse_selection.go`
- Create `internal/simulation/legacy_options_fill.go`
- Modify corresponding `internal/backtest/*.go` files into aliases/wrappers
- Modify `internal/execution/paper/broker.go`
- Modify `internal/execution/paper/broker_test.go`
- Modify `internal/execution/paper/options.go`
- Modify `internal/execution/paper/options_test.go`

- [ ] Move one implementation at a time under `internal/simulation`; after each
  move, make the backtest public name an alias/wrapper and run its focused tests
  before proceeding.
- [ ] Preserve legacy float behavior and error identity exactly except for the
  explicitly reviewed paper missing-price change.
- [ ] Add a RED paper test proving a market order without an executable
  reference is rejected and leaves cash, positions, and fills unchanged; remove
  `defaultReferencePrice`.
- [ ] Delegate the paper single-leg option compatibility calculation to the
  moved simulation primitive and retain explicit-price fail-closed behavior.
- [ ] Prove all current direct callers still compile and legacy backtest
  reports retain `backtest-input-v1` rather than being relabelled as canonical.

Run:

```bash
go test -race -count=1 ./internal/backtest ./internal/execution/paper \
  ./internal/discovery/options ./internal/portfolio ./internal/copytrading
```

## Task 7: Documentation, qualification, independent review, and sync

**Files:**

- Create `docs/runbooks/common-simulation-venue.md`
- Modify `docs/adr/017-common-execution-lifecycle.md`
- Modify `docs/adr/018-scored-and-stress-paper-modes.md`
- Modify `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify this plan with final evidence

- [ ] Document policy construction/hash inspection, durable artifact
  registration/lookup, explicit/continuous calendars, supported capabilities,
  quote/depth requirements, raw-first persistence, restart replay, ADR-018
  classification, outcome hash, adapter use, legacy compatibility labels,
  inspection commands, and empty-only schema rollback.
- [ ] Record explicitly that OVR-204 is local, schema 72 is not deployed, no
  runtime writer or scheduler is activated, no shared data is migrated, and no
  legacy or stress result becomes promotion evidence.
- [ ] Run focused race suites and the repository-wide gates:

  ```bash
  go test -race -count=1 ./internal/simulation ./internal/backtest \
    ./internal/execution/paper ./internal/repository/postgres ./migrations
  task test:race
  task build
  task vet
  task lint
  task fmt:check
  govulncheck ./...
  ```

- [ ] Run the frontend Node 22 test/lint/build gates even though OVR-204 should
  not change frontend source. Preserve inherited failures separately from new
  regressions.
- [ ] Build a new loopback-only database from migrations `1 -> 72` for the
  persistent golden replay because the retained OVR-203 rehearsal intentionally
  contains simulation orders whose old policy bytes cannot be guessed. Retain
  the OVR-203 database unchanged as provenance. Verify the new database reports
  `72|false` and record exact artifact, lifecycle, normalization,
  ledger-transaction, posting, and fill counts.
- [ ] On a disposable empty database prove `72 -> 71 -> 72`; on the retained
  nonempty OVR-204 rehearsal prove downgrade refusal preserves every row.
- [ ] Start the rebuilt binary only with the global kill switch active,
  scheduler disabled, no venue credentials, isolated schema-72 PostgreSQL, and
  isolated Redis. Check `/health`, `/healthz`, and `/api/v1/health`, then stop
  cleanly. This remains local runtime evidence, not deployment proof.
- [ ] Obtain independent post-implementation diff approval with no unresolved
  P0/P1 findings.
- [ ] Commit the reviewed OVR-204 implementation, push
  `codex/augr-overhaul`, fetch, verify local/remote equality and `0 0`
  divergence, then advance immediately to the next dependency-ready item.

## Acceptance evidence to record after implementation

- [ ] Policy bytes and digest are canonical, exact, immutable, durably
  recoverable by version, and referenced by every simulation order/later
  lifecycle event.
- [ ] Changed current configuration, concurrent artifact replay, digest/byte
  mismatch, and missing simulation-order artifacts behave fail-closed.
- [ ] Missing/stale/future quote, source, bid, ask, depth, status, contract,
  policy, currency, tick, or lot facts fail closed.
- [ ] Fixed latency prevents early fill; exact depth participation and one-fill-
  per-level mechanics cannot exceed available capacity or order quantity.
- [ ] Market, marketable/nonmarketable limit, DAY/GTC, IOC, and FOK semantics
  match their documented deterministic v1 behavior; explicit normal/half-day/
  holiday windows and continuous 24/7 rules cannot retain a DAY order past its
  close.
- [ ] Every simulated fill owns byte-stable evidence, one raw event, one exact
  OVR-103 normalization/ledger transaction, and one OVR-203 fill transition.
- [ ] Per-order fee occurs once; per-unit/notional fees, multiplier, rounding,
  and zero-fee omission are exact.
- [ ] Same-snapshot and concurrent retries converge; interrupted multi-level
  replay resumes without a duplicate fill, fee, binding, or economic effect.
- [ ] Canonical backtest and internal-paper adapters produce identical golden
  replay policy versions and economic outcome hashes within the same ADR-018
  mode; scored/stress namespaces and hashes remain distinct.
- [ ] Reusable legacy models live under `internal/simulation`, compatibility
  APIs/tests still pass, and missing-price paper execution no longer invents
  `$1.00`.
- [ ] Schema 72 is additive, immutable, concurrency-safe, refuses ambiguous
  schema-71 simulation history, and rolls back only while empty. No shared,
  runtime, or deployment boundary changed, and legacy compatibility output
  remains visibly noncanonical.
- [ ] Focused/database race tests, whole-repository gates, frontend gates,
  kill-switched smoke, independent review, commit, and push evidence are
  recorded honestly.
