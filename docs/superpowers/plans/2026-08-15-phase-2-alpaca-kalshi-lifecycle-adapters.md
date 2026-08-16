# Phase 2 Alpaca and Kalshi Lifecycle Adapter Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep every provider
> contract and persistence boundary test-first, obtain architecture approval
> before implementation and diff approval before shipping, and preserve the
> local-only deployment boundary.

**Goal:** Complete OVR-205 with additive Alpaca and Kalshi adapters that submit
the immutable OVR-203 order command using its stable client identity, retain
every exact provider observation before interpretation, apply fills through the
one OVR-103/203 economic graph, and map every supported provider state without
collapsing expiry, cancellation, rejection, replacement, correction, or bust.
An unrecognized, malformed, or contradictory provider fact must halt that
lifecycle in `failed_reconciliation`; it must never create a guessed fill,
terminal state, replacement, or second order.

**Architecture:** Add a provider-neutral `internal/execution/venue` boundary
beside the existing common lifecycle. Two immutable content-addressed adapter
policy artifacts pin the reviewed Alpaca Trading API v2 and Kalshi Trade API v2
request shapes, capabilities, state vocabularies, fill identities, and restart
semantics. Migration 73 stores those exact artifacts and an append-only raw
venue-observation journal. Venue-policy orders must reference a registered
artifact for the same venue. A raw provider object is journaled before a
lifecycle transition; fills are then also recorded through OVR-103's raw
economic-event boundary before OVR-203 atomically commits normalization,
ledger, binding, fill, and transition. Provider-specific packages parse exact
decimal strings and return already-validated transition plans. Thin transport
clients submit/query/cancel with canonical IDs, but no runtime path, scheduler,
credential, writer grant, or live route is activated by this phase.

**Tech stack:** Go 1.25.8+, `shopspring/decimal`, UUIDs, SHA-256, PostgreSQL
17/TimescaleDB, pgx v5, golang-migrate, `httptest`, Task, and TDD against an
isolated schema-72 database.

**Verified provider references (checked 2026-08-15):**

- Alpaca creates orders at `POST /v2/orders`, accepts a client order ID of at
  most 128 characters, and provides
  `GET /v2/orders:by_client_order_id?client_order_id=...`:
  <https://docs.alpaca.markets/us/v1.1/reference/postorder> and
  <https://docs.alpaca.markets/us/reference/getorderbyclientorderid>.
- Alpaca's full lifecycle vocabulary distinguishes `canceled`, `expired`,
  `rejected`, `replaced`, `done_for_day`, pending states, suspension, and fill
  corrections/busts. Trade updates are prompt state/fill notices, but this
  policy does not assume their execution identity equals an account-activity
  identity:
  <https://docs.alpaca.markets/us/v1.4.2/docs/websocket-streaming>.
- Alpaca's account-activity endpoint provides replayable, page-token-based
  `FILL` records filterable by order ID, with exact activity ID, quantity,
  price, side, symbol, cumulative quantity, leaves quantity, execution time,
  and order ID:
  <https://docs.alpaca.markets/us/reference/getaccountactivities-2>.
- Alpaca activity corrections/busts retain a stable `ref_id` and
  `previous_id`; reconnecting from the last event cursor is the documented
  replay path:
  <https://docs.alpaca.markets/us/docs/activity-sse>.
- Kalshi's current V2 order entry is
  `POST /portfolio/events/orders`; it requires a client order ID and exact
  fixed-point count/price strings. Its allowed time-in-force values are
  `good_till_canceled`, `immediate_or_cancel`, and `fill_or_kill`:
  <https://docs.kalshi.com/api-reference/orders/create-order-v2>.
- Kalshi documents the client order ID as its deduplication key; retrying the
  same ID cannot create another order and a duplicate returns conflict:
  <https://docs.kalshi.com/getting_started/quick_start_create_order>.
- Kalshi order status is exactly `resting`, `canceled`, or `executed`; order
  snapshots retain client ID, exact initial/fill/remaining counts, price/cost,
  and update timestamps:
  <https://docs.kalshi.com/api-reference/orders/get-orders> and
  <https://docs.kalshi.com/api-reference/orders/get-order>.
- Kalshi fills are cursor-paginated and retain `fill_id`, `trade_id`,
  `order_id`, exact fixed-point quantity, YES/NO dollar prices, fee cost,
  timestamp, and subaccount:
  <https://docs.kalshi.com/api-reference/portfolio/get-fills>.
- Kalshi moves completed orders/fills behind historical cutoffs; restart
  lookup must use current and historical endpoints rather than treating a
  current-endpoint miss as proof of absence:
  <https://docs.kalshi.com/getting_started/historical_data>.

---

## Scope and sequencing

This plan covers OVR-205 only. It depends on OVR-201 canonical instruments and
venue contracts, OVR-203's immutable lifecycle and raw-first fill graph, and
OVR-103's exact economic normalization. It does not depend on OVR-204 and does
not activate external execution.

In scope:

- two exact content-addressed venue-adapter policy artifacts, one for Alpaca
  Trading API v2 and one for Kalshi Trade API v2;
- a provider-neutral append-only raw order/fill/correction observation model,
  deterministic identity, exact bytes/hash/JSON, source/receive time, provider
  state, mapping result, canonical order context, and policy version;
- a migration-73 journal and deferred checks tying every provider-driven
  lifecycle transition to the matching earlier raw venue observation;
- strict provider-policy, account, environment, canonical instrument, dated
  venue contract, external order ID, client order ID, order mechanics, and
  fill-total checks;
- Alpaca canonical request mapping without binary floats, order-type changes,
  inferred extended-hours behavior, generated client IDs, or average-price
  pseudo-fills;
- Alpaca lookup by canonical client ID, lookup by external order ID, exact fill
  activity pagination, cancel request, full status/event mapping, and
  correction/bust failure evidence;
- Kalshi V2 fixed-point request mapping, exact outcome/book-side complement
  handling, current plus historical order lookup by client ID, current plus
  historical fill pagination, cancel request, and exact three-status mapping;
- raw-first persistence, exact replay, changed-payload conflict, crash/restart
  recovery, duplicate-submit handling, pagination, concurrent retry, and
  terminal contradiction tests;
- local fake-server transport qualification, isolated PostgreSQL migration and
  repository qualification, runbook, ADR/overhaul evidence, and independent
  review.

Out of scope:

- enabling `ENABLE_LIVE_TRADING`, adding credentials, granting a writer role,
  wiring a scheduler/worker, deploying, or calling a real Alpaca or Kalshi
  account;
- changing or dual-writing legacy mutable `orders`, `trades`, `positions`,
  `OrderManager`, Alpaca `Broker`, Kalshi `Broker`, or their float/status
  compatibility contracts;
- blind resubmission under a new client ID, order replacement, fan-out,
  multi-leg orders, brackets/OCO/OTO, trailing stops, notional orders, or
  provider-side advanced instructions;
- silently converting a market order into a limit order, enabling extended
  hours, inventing a Kalshi market price, or rounding authoritative provider
  decimals;
- deriving a fill from `filled_avg_price`, cumulative filled quantity, an
  order status, or a missing/zero sentinel. One fill requires one provider fill
  identity with exact event quantity and price;
- treating a cancel HTTP success as cancellation, a current-history miss as
  nonexistence, a replacement as the original common order, or a correction/
  bust as authorized ledger repair;
- Alpaca options/multileg position-intent mapping, which needs OVR-405's
  reviewed multi-leg/orphan-risk contract;
- cutover, production migration, real credential smoke, shared database
  mutation, OVR-207 reconciliation, or any accounting correction workflow.

## Contract decisions

### Immutable policy and route authorization

1. The artifact schema is `venue-adapter-policy-v1`. Its canonical bytes pin
   provider, venue, API revision, endpoint families, maximum client-order-ID
   length, retry/lookup semantics, supported asset/order/TIF combinations,
   exact state/event mappings, fill identity fields, Kalshi contract-metadata
   grammar, and fee treatment. Arrays are sorted and JSON keys use one fixed
   encoder order.
2. The version is
   `venue-adapter-policy-v1@sha256:<lowercase digest>` and the UUID is the
   OVR-103 length-prefixed deterministic UUID of the full version. Creation
   time is excluded from content identity. Exact retries converge; changed
   bytes under the same version conflict.
3. Go validates a policy by reconstructing it from canonical bytes. PostgreSQL
   independently validates the complete fixed-v1 token/value grammar and
   reconstructs the exact Go bytes. Merely supplying self-consistent JSON,
   SHA-256, and version is not enough to register a policy.
4. Migration 73 refuses to attach any pre-existing `policy_kind='venue'`
   order because its historical mapping bytes cannot be inferred. Its first
   executable statement takes `LOCK TABLE execution_orders IN SHARE ROW
   EXCLUSIVE MODE` before that precondition or any new trigger is installed, so
   a schema-72 writer cannot race an unregistered venue order through the
   boundary. A new venue order must reference a registered artifact whose venue
   matches both the order and dated venue contract. Simulation orders and
   artifacts remain unchanged.
5. There is no mutable current-policy pointer. A route records one immutable
   artifact version, and restart reloads exactly that version. A policy change
   creates another artifact and applies only to later orders.
6. The Alpaca v1 artifact supports only common single-instrument instructions
   representable without semantic conversion: equity/ETF market, limit, stop,
   and stop-limit under DAY/GTC plus market/limit under IOC/FOK; crypto spot
   market/limit/stop-limit under GTC and market/limit under IOC. Fractional
   eligibility remains governed by exact venue lot size and policy capability.
   Options, trailing stop, OPG/CLS, extended hours, notional, and advanced
   classes fail before an HTTP call.
7. The Kalshi v1 artifact supports only prediction-contract limit orders under
   GTC, IOC, or FOK. Common DAY has no exact V2 value and common GTD has no
   expiration timestamp field, so both fail before an HTTP call. A future
   canonical order extension may add explicit expiry and another policy.

### Raw venue observations and lossless mapping

8. `VenueObservation` is an immutable provider fact, not state. It stores
   account, intent, order, optional binding, provider/venue/policy, kind,
   provider state/event type, mapped outcome, external/client order IDs,
   venue contract and provider contract/ticker IDs, canonical outcome, provider
   book side/action, source namespace/event/revision, provider source time,
   local receive time, exact JSON object bytes, SHA-256, parsed JSON, and local
   creation time. Non-applicable typed provider fields use one explicit empty
   representation; they are never inferred from raw JSON during replay.
9. Observation identity is the deterministic UUID of account, provider,
   source namespace, and source event ID. Revision is evidence, not identity;
   changed revision/time/bytes under the same provider identity conflicts.
   Provider execution/activity/fill IDs are used whenever supplied, and the
   source namespace keeps distinct provider feeds distinct. An observation ID
   is not automatically an economic fill ID: only the immutable policy's one
   declared authoritative economic-fill feed may create economics. REST order
   snapshots use external order ID plus provider `updated_at`/
   `last_update_time`; submit responses use external order ID plus the provider
   processing timestamp. A malformed response lacking a provider event ID uses
   an explicitly labelled local response identity containing endpoint, request
   identity, and raw hash; it is never relabelled as a provider execution ID.
10. The mapping-outcome vocabulary is bounded:
    `acknowledge`, `no_change`, `fill_notice`, `fill`, `cancelled`, `expired`,
    `rejected`, `correction`, `bust`, `unknown_state`, `contradiction`, and
    `malformed_observation`. The exact provider state/event string remains a
    separate field and in the raw bytes; two provider states never become
    indistinguishable evidence merely because they retain the same common
    state.
11. Every observation is recorded before interpretation is applied. For a
    provider-driven lifecycle event, migration 73 requires a preceding journal
    row with the same account/order, provider source identity, policy version,
    exact bytes/hash, and compatible mapped outcome. Local `order_routed` and
    `cancel_requested` commands are not provider observations and are exempt.
    The cancellation command nevertheless has one exact replay identity:
    source `venue_command`, namespace
    `venue-adapter-policy-v1/<provider>/<policy-version>/cancel-request-v1`, and
    source event ID `<canonical-order-uuid>/cancel-request-v1`. Its fixed
    canonical JSON evidence binds the order, provider, venue, policy version,
    client ID, nullable external binding, exact HTTP method/path, and request
    bytes or the explicit empty-body sentinel. Exact retries converge; any
    changed field or body under that identity is an idempotency conflict.
12. A known repeated nonterminal provider state is journaled with
    `no_change`; it does not fabricate a duplicate `order_working` event. An
    exact repeated observation replays. A later distinct provider observation
    remains separately inspectable even when it changes no common state.
13. Unknown provider state/event values are journaled as `unknown_state` and
    append `unknown_venue_state -> failed_reconciliation`. A recognized state
    whose identities, cumulative quantity, terminal meaning, or prior common
    state disagree is journaled as `contradiction` and appends
    `contradictory_venue_state -> failed_reconciliation`.
14. Once a lifecycle is `failed_reconciliation`, later facts can still be
    journaled but cannot mutate the lifecycle or create economics. Repair is a
    separate reviewed workflow.

### Submission, cancellation, and restart safety

15. The OVR-203 `order_routed` transaction must commit before any submit call.
    Both adapters submit `Order.ClientOrderID` unchanged. Provider-generated or
    random client identities are forbidden.
16. A provider response must echo/match the canonical client ID when the API
    supplies it. The external order ID becomes the one immutable OVR-203
    binding on the first acknowledgement or fill. Any later different external
    ID for the same canonical order is a contradiction.
17. An Alpaca routed recovery first queries
    `/v2/orders:by_client_order_id`; a bound recovery queries by external ID.
    A 404 is not an order rejection or proof that an ambiguous submit had no
    effect. It leaves the order routed for later reconciliation unless an exact
    policy-reviewed deduplicated retry is invoked with the same body and same
    client ID.
18. Kalshi duplicate submission is a safe conflict under the same client ID,
    not permission to use a new ID. Recovery searches current and historical
    order pages for the exact client ID, ticker, subaccount, and exchange index.
    Zero results remain unresolved; multiple or mismatched results fail
    reconciliation.
19. Request bodies are deterministic exact projections of the immutable common
    order and dated contract. A retry body must be byte-identical. Changed
    policy, mechanics, symbol/ticker, quantity, price, side, TIF, subaccount,
    or client identity under the same canonical order fails before transport.
20. The content-bound `cancel_requested` command commits before `DELETE`; a
    provider cancellation endpoint that requires an external order ID is not
    called without the matching immutable binding. HTTP success means only that
    the request was accepted. Retry before `DELETE`, timeout after `DELETE`,
    process restart, and concurrent calls reuse the same local command identity
    and bytes. The adapter queries/consumes a sourced provider state before
    appending `order_cancelled`; a timeout, 404, or rejection does not pretend
    cancellation.

### Fill, fee, correction, and terminal semantics

21. Provider objects from the policy's one authoritative economic-fill feed are
    processed in deterministic source order before the enclosing order
    snapshot. Each exact authoritative fill object is both a venue observation
    and an OVR-103 `EconomicSourceEvent` under the same provider namespace,
    event ID, revision, receive time, and bytes. Non-authoritative fill notices
    remain venue observations only and can trigger recovery of the authoritative
    feed; they cannot create a fill, normalization, ledger effect, or synthetic
    cross-feed correlation.
22. A fill requires provider event ID, external order ID, exact event quantity,
    exact event price, side, canonical symbol/ticker/outcome, execution time,
    and matching common context. Cumulative quantity and average fill price are
    validation facts only; they can never manufacture a fill.
23. The adapter derives the OVR-203 fill ID, builds an exact OVR-103 fill
    normalization with `ReferenceType=execution_fill`, and lets OVR-203 commit
    normalization, ledger, optional first binding, fill, and event atomically.
    Present exact zero price remains valid only where the canonical instrument
    permits it; missing price is never zero.
24. Alpaca account-activity `FILL` is the sole economic-fill authority in this
    policy. Its source namespace is `alpaca/account-activities/FILL`, its exact
    `id` is the economic source event ID, and its `qty` and `price` are the
    authoritative increment. Stream `fill`/`partial_fill` observations use a
    separate stream namespace and `fill_notice`; they may initiate paged
    account-activity recovery but never create economics. The policy does not
    assert that a stream `execution_id`/`ref_id` equals an activity `id`, and it
    never correlates feeds from order, price, quantity, or time. An absent,
    uncorrelatable, or contradictory second-feed fact remains raw evidence and
    either waits for authoritative recovery or fails reconciliation before any
    additional normalization. Commission is attached only when the activity
    unambiguously supplies an exact per-fill commission; otherwise it remains
    absent, not zero. Separate fee activities stay in OVR-103 and are not
    invented here.
25. Kalshi `fill_id` is the source event ID. `count_fp` is exact quantity;
    the configured canonical outcome selects `yes_price_dollars` or
    `no_price_dollars`. Exact `fee_cost` is an attached USD fee when present;
    absence remains absent. `trade_id` is retained but never replaces the
    account-specific fill identity.
26. A Kalshi canonical prediction instrument represents one explicit YES or NO
    outcome. For an OVR-205-eligible `venue='kalshi'` contract, the entire
    immutable `venue_contracts.metadata` object must be exactly
    `{"kalshi_v2":{"outcome":"yes"}}` or
    `{"kalshi_v2":{"outcome":"no"}}`. No other top-level or nested key,
    non-string value, case variant, or outcome alias is valid. Its
    `contract_id` is the authoritative market ticker. Go parses and validates
    that exact grammar whenever it loads the contract. Create mapping verifies
    the account-side action and maps YES buy/sell to V2 bid/ask, NO buy/sell to
    ask/bid, and NO price to exact `1 - outcome_price`; fill mapping reverses
    that projection and verifies both supplied YES/NO prices when present. No
    ticker parsing or default YES is allowed.
27. Migration 73's venue-order authorization joins a Kalshi venue-policy order
    to its dated contract and enforces `venue='kalshi'`, the exact metadata
    object, and the policy's metadata grammar before the route commits. Its
    observation semantics then require any fact mapped as an acknowledgement,
    fill notice, fill, or terminal state to carry a typed provider ticker equal
    to `venue_contracts.contract_id`, a typed canonical outcome equal to the
    immutable lowercase metadata value, and provider book side/action/price
    fields equal to the policy projection. A provider mismatch is still
    retained raw, but only as `contradiction`; PostgreSQL refuses to label it as
    a valid mapping. Deferred checks require that contradiction before the
    reconciliation-failure event and prohibit any matching raw economic event,
    fill, or ordinary provider-driven lifecycle transition. The same contract
    and projection checks run again on repository reload/replay.
28. Alpaca `fill`/`partial_fill` and Kalshi `executed`/nonzero fill totals are
    accepted only after all available exact fills have been applied and local
    cumulative quantity equals the provider cumulative fact. A terminal filled
    status never appends a fill-less terminal transition. Missing fill detail
    is a contradiction and halts safely.
29. Alpaca maps `canceled`, `expired`, and `rejected` to three distinct common
    terminal events. `accepted`, `pending_new`, `new`, `held`,
    `accepted_for_bidding`, `pending_cancel`, `done_for_day`, `stopped`,
    `suspended`, and a quantity-consistent `calculated` are explicit
    acknowledgement/no-change states. `pending_replace` and `replaced` are
    recognized but unsupported one-order-contract contradictions. Cancel/
    replace rejection event types are retained without claiming a terminal
    order state.
30. Kalshi `resting` acknowledges or retains working/partial state;
    `canceled` appends cancellation after all fills and exact totals agree;
    `executed` is valid only after fills bring local cumulative quantity to the
    exact order quantity. No legacy synonyms such as `open`, `filled`,
    `partial`, `cancelled`, or `rejected` are accepted by the common V2 policy.
31. Alpaca `trade_correct` and `trade_bust` require `previous_execution_id` or
    `previous_id`, locate that exact existing canonical fill, and append the
    OVR-203 correction/bust reconciliation failure with the new immutable event
    discriminator. They never edit the prior fill or ledger. Kalshi has no
    reviewed correction/bust mapping in this policy; any such new event value
    is unknown and halts.

### Activation boundary

32. The new adapters are constructors and explicit methods only. Existing
    runtime composition, legacy brokers, scheduler, automation, API routes,
    feature flags, and credential loading remain unchanged. Tests use fake
    transports and loopback `httptest` servers only.
33. No test may contact a provider hostname. A kill-switched local runtime
    smoke proves startup behavior remains unchanged and leaves the retained
    migration rehearsal graph untouched; it is not external-paper or live
    qualification.

## File map

- Create `internal/execution/venue/policy.go` and tests: fixed-v1 canonical
  adapter policies, content identity, capabilities, and provider mappings.
- Create `internal/execution/venue/observation.go` and tests: exact raw evidence,
  deterministic identity, mapped outcome, and replay equality.
- Create `internal/execution/venue/result.go` and tests: ordered observation /
  transition plans and raw-first persistence.
- Modify `internal/repository/interfaces.go`: narrow venue-policy and
  observation repositories.
- Create `internal/repository/postgres/venue_adapter.go` and tests: artifact and
  raw-observation persistence, replay, lookup, and concurrency.
- Create migration 73 up/down/tests: artifacts, observations, venue-order
  authorization, transition/observation linkage, append-only protection,
  deterministic IDs, and empty-only rollback.
- Modify `internal/repository/postgres/schema_version.go`: require schema 73.
- Create `internal/execution/alpaca/common_lifecycle.go` and tests: exact order,
  status, fill, correction/bust, terminal, and recovery mapping.
- Modify `internal/execution/alpaca/client.go` and tests: exact canonical order
  lookup and account-activity pagination without altering legacy broker APIs.
- Create `internal/execution/kalshi/common_lifecycle.go` and tests: exact V2
  request, outcome mapping, statuses, fills, terminal, and recovery mapping.
- Modify `internal/execution/kalshi/client.go`, `live_client.go`, and tests:
  raw V2 order/fill/current/historical transport methods while preserving the
  legacy `LiveClient` compatibility surface.
- Create `docs/runbooks/alpaca-kalshi-common-lifecycle.md`.
- Modify `docs/adr/017-common-execution-lifecycle.md`, the total overhaul plan,
  and this plan with final evidence.

## Task 1: Canonical venue policy and observation domain

**Files:**

- Create `internal/execution/venue/policy.go`
- Create `internal/execution/venue/policy_test.go`
- Create `internal/execution/venue/observation.go`
- Create `internal/execution/venue/observation_test.go`

- [ ] Write RED tests for exact policy identity, canonical-byte reconstruction,
  sorted bounded vocabularies, full Alpaca/Kalshi state tables, capability
  checks, one declared authoritative economic-fill feed, changed-byte conflict,
  and malformed-but-rehashed rejection.
- [ ] Implement the two reviewed fixed-v1 policy artifacts and parse only from
  revalidated canonical bytes.
- [ ] Write RED tests for observation identity, exact bytes/hash/JSON,
  provider-vs-local source identity labels, normalized UTC microsecond times,
  bounded mapped outcomes, exact replay, and changed revision/time/bytes
  conflict.
- [ ] Prove every documented provider state/event appears exactly once in its
  policy and legacy synonyms do not enter the common policy.

Run after every RED/GREEN slice:

```bash
go test -count=1 ./internal/execution/venue
```

## Task 2: Migration 73 artifacts and raw observation journal

**Files:**

- Create `migrations/000073_venue_adapter_observations.up.sql`
- Create `migrations/000073_venue_adapter_observations.down.sql`
- Create `migrations/000073_venue_adapter_observations_test.go`
- Modify `internal/repository/postgres/schema_version.go`

- [ ] Add source-shape RED tests for exact artifact reconstruction, append-only
  artifact/observation tables, deterministic identities, policy-to-venue-order
  enforcement, provider-transition raw linkage, indexes, absence of grants or
  activation, the first-statement up lock, the complete down lock set, and
  empty-only rollback.
- [ ] Add direct PostgreSQL tests proving forged canonical bytes/JSON/hash/
  version, missing state entries, unsupported capability entries, wrong venue,
  preexisting venue orders, malformed observations, invalid Kalshi outcome
  metadata or projection, changed identity reuse, event-before-observation, and
  mismatched event bytes all fail.
- [ ] Prove distinct no-change observations remain durable, provider-driven
  fill/terminal/failure events require exact prior observations, local route and
  cancel-request events remain valid, and simulation orders remain unchanged.
- [ ] Prove concurrent exact observation/artifact retries converge, changed
  retries conflict, update/delete fails, nonempty downgrade refuses, and empty
  `73 -> 72 -> 73` preserves schema 72.
- [ ] In real PostgreSQL, race a schema-72 venue-order insert against migration
  up. The writer must either commit before the locked precondition and make
  migration fail, or wait for the post-trigger schema and require a registered
  artifact; it must never leave an unregistered venue order. Migration down
  takes `ACCESS EXCLUSIVE` locks on `execution_orders`, the artifact table,
  and the observation journal before its emptiness checks, refuses any
  migration-73 fact, and is likewise covered by a concurrent insert race.
- [ ] Bump `RequiredSchemaVersion` only after the isolated migration suite
  passes.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'VenueAdapter|VenueObservation' ./migrations
```

## Task 3: PostgreSQL repositories and raw-first persistence coordinator

**Files:**

- Modify `internal/repository/interfaces.go`
- Create `internal/repository/postgres/venue_adapter.go`
- Create `internal/repository/postgres/venue_adapter_test.go`
- Create `internal/execution/venue/result.go`
- Create `internal/execution/venue/result_test.go`

- [ ] Register/reload exact policy artifacts and record/reload raw observations
  through narrow interfaces. Exact insert-or-load retries converge; a changed
  identity wraps `repository.ErrIdempotencyConflict`.
- [ ] Persist a result in strict order: venue observation; for a fill, matching
  economic raw event; then OVR-203 fill/transition. No-change observations stop
  after the journal insert.
- [ ] Reject a transition lacking its observation, a fill lacking either raw
  boundary, nonmatching source identity/bytes/times, results for another
  account/order/policy, nil steps, and transitions out of order.
- [ ] Persist the fixed cancellation-command event independently of provider
  observations. Prove retry-before-transport, timeout-after-transport,
  fresh-process retry, and eight concurrent callers converge to one event;
  changed endpoint, binding, policy, client ID, or body conflicts.
- [ ] Inject failure after each child write and prove restart converges from the
  retained raw journal/economic event without duplicate state or economics.
- [ ] Run eight-writer exact replay and changed-payload races against policy,
  no-change observation, acknowledgement, partial fill, final fill, terminal,
  and unknown-state results.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  ./internal/execution/venue ./internal/repository/postgres
```

## Task 4: Alpaca exact common-lifecycle adapter

**Files:**

- Create `internal/execution/alpaca/common_lifecycle.go`
- Create `internal/execution/alpaca/common_lifecycle_test.go`
- Modify `internal/execution/alpaca/client.go`
- Modify `internal/execution/alpaca/client_test.go`

- [ ] Add request-mapping RED tests for stable `client_order_id`, exact decimal
  quantity/prices, common order type/TIF, canonical venue symbol, and explicit
  unsupported asset/instruction rejection. Prove no market-to-limit,
  extended-hours, notional, trailing, bracket, or float conversion occurs.
- [ ] Add loopback transport tests for submit, get by client ID, get by external
  ID, cancel, order-filtered ascending fill activities, page tokens, 404,
  duplicate conflict, malformed JSON, repeated cursor, and context
  cancellation. Assert credentials never appear in logs or returned evidence.
- [ ] Add a table-driven test for every Alpaca policy state/event. Distinguish
  canceled/expired/rejected; preserve all nonterminal values; fail replacement
  safely; and make unknown/malformed values produce explicit failure results.
- [ ] Parse authoritative account-activity fills using exact decimal strings
  and activity IDs. Journal stream fill/partial-fill payloads as non-economic
  `fill_notice` observations, then recover account activities. Validate order/
  client/symbol/side/cumulative/leaves facts and never derive a fill from a
  stream execution ID, average price, cumulative delta, or cross-feed
  price/quantity/time similarity.
- [ ] Map fill corrections and busts to the exact prior canonical fill without
  changing economics. Reject missing/unknown previous IDs and changed reuse.
- [ ] Prove routed submit response, immediate partial/complete first fill,
  repeated partials, terminal status, cancel confirmation, duplicate submit,
  crash after external submit, crash after raw observation, stream disconnect,
  paged REST recovery, and fresh-process replay all converge.
- [ ] Against real PostgreSQL, prove stream-first then activity, activity-first
  then stream, two equal-price/equal-quantity activities with distinct IDs,
  and an uncorrelatable or contradictory second-feed payload. The first two
  produce exactly one economic graph, the independent activity IDs produce
  exactly two, and the bad second-feed fact produces no additional economics.

Run:

```bash
go test -race -count=1 ./internal/execution/alpaca \
  ./internal/execution/venue ./internal/execution/lifecycle
```

## Task 5: Kalshi V2 exact common-lifecycle adapter

**Files:**

- Create `internal/execution/kalshi/common_lifecycle.go`
- Create `internal/execution/kalshi/common_lifecycle_test.go`
- Modify `internal/execution/kalshi/client.go`
- Modify `internal/execution/kalshi/live_client.go`
- Modify `internal/execution/kalshi/live_client_test.go`

- [ ] Add request-mapping RED tests for stable `client_order_id`, exact V2
  fixed-point count/price, GTC/IOC/FOK, immutable ticker/outcome/subaccount/
  exchange-index facts, bid/ask mapping, and exact NO complement. Reject DAY,
  GTD, market, missing outcome, float rounding, unsupported mechanics, and
  metadata/ticker mismatch before transport.
- [ ] Validate the whole Kalshi venue-contract metadata object as exactly one
  `kalshi_v2.outcome` string with lowercase `yes` or `no`, in Go and PostgreSQL.
  Reject missing/misspelled/non-string/case-variant values, any extra top-level
  or nested key, and disagreement with request action/book side or fill price
  fields before transport or economics.
- [ ] Add loopback transport tests for V2 submit/get/cancel, current and
  historical order scans, exact client-ID selection, current and historical
  fill pagination, cursor termination, duplicate conflict, rate/error
  propagation, malformed JSON, and context cancellation.
- [ ] Accept exactly `resting`, `canceled`, and `executed`; prove every legacy
  synonym and arbitrary new status journals raw evidence and fails unknown.
- [ ] Parse fills from exact `fill_id`, `count_fp`, outcome price, `fee_cost`,
  and provider timestamp. Verify order/ticker/outcome/book side/action,
  subaccount/exchange index, fill/remaining/initial totals, and provider order
  identity before creating economics.
- [ ] Prove resting-after-partial, immediate partial/complete submit, IOC
  partial-plus-cancel, FOK cancel/no-fill, full execution, duplicate submit,
  current-to-historical cutoff, paginated recovery, crash/restart, and
  eight-writer replay converge without a duplicate order/fill/fee.
- [ ] Prove missing fill details behind an executed/nonzero cumulative order,
  multiple client-ID matches, mismatched IDs, impossible totals, or an unknown
  event halts in reconciliation rather than guessing.

Run:

```bash
go test -race -count=1 ./internal/execution/kalshi \
  ./internal/execution/venue ./internal/execution/lifecycle
```

## Task 6: Integrated recovery rehearsal and adversarial qualification

**Files:**

- Create `internal/repository/postgres/venue_adapter_integration_test.go`
- Extend provider adapter tests as needed

- [ ] Build complete canonical Alpaca and Kalshi fixtures from active accounts,
  instruments, dated venue contracts, quote snapshots, routed venue-policy
  orders, registered artifacts, and scripted provider observations.
- [ ] Rehearse acknowledgement, no-change states, multiple fills, attached
  Kalshi fee, cancellation, expiry, rejection, unknown state, replacement,
  correction, bust, duplicate submit, current/historical recovery, and fresh
  repository restart through the real PostgreSQL boundary.
- [ ] Inject crashes after external-response receipt, venue journal, economic
  raw insert, and lifecycle commit; restart with the same provider facts and
  assert one order, one binding, one fill/normalization/ledger effect per
  authoritative provider fill, plus separately durable no-change evidence.
- [ ] Rehearse the fixed cancellation command across pre-DELETE retry,
  post-DELETE timeout, concurrent callers, and changed-payload attack; assert
  one command event, no false cancellation, and conflict on changed evidence.
- [ ] Attack direct SQL with valid hashes but invalid policy values, missing
  policy registration, forged observation mapping, terminal-before-fill,
  provider/external/client mismatch, provider-state synonym, and changed
  source revision. All must fail closed without modifying prior facts.
- [ ] Attack Kalshi contract/observation linkage with absent, misspelled,
  non-string, uppercase, extra, ambiguous, and wrong outcome metadata; wrong
  ticker/action/book-side/price projections; and reload after contract lookup.
  Invalid contract metadata must fail before an order commits. Mismatched
  provider facts must persist only as raw `contradiction` evidence and then a
  reconciliation-failure event; no raw economic event, fill, or ordinary
  provider transition may commit.
- [ ] Confirm legacy Alpaca/Kalshi broker tests and runtime composition are
  byte-for-byte behaviorally unchanged by the additive common adapters.

Run:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'VenueAdapter|AlpacaCommonLifecycle|KalshiCommonLifecycle' \
  ./internal/repository/postgres ./migrations
```

## Task 7: Runbook, review, synchronization, and handoff

**Files:**

- Create `docs/runbooks/alpaca-kalshi-common-lifecycle.md`
- Modify `docs/adr/017-common-execution-lifecycle.md`
- Modify `docs/superpowers/plans/2026-08-14-total-overhaul-plan.md`
- Modify this plan with implementation evidence

- [ ] Document policy registration, raw-first ordering, submission ambiguity,
  client-ID recovery, provider mappings, fill pagination, historical cutoff,
  cancellation semantics, correction/bust handling, read-only inspection,
  incident states, empty-only rollback, and explicit activation boundaries.
- [ ] Apply reviewed migrations `1 -> 73` to a dedicated loopback-only
  database, retain one complete Alpaca and one complete Kalshi rehearsal graph,
  prove nonempty rollback refuses, and prove empty `73 -> 72 -> 73` separately.
- [ ] Run focused races and whole-repository gates:

  ```bash
  go test -race -count=1 ./internal/execution/venue \
    ./internal/execution/alpaca ./internal/execution/kalshi \
    ./internal/execution/lifecycle ./internal/repository/postgres ./migrations
  task test:race
  task build
  task vet
  task lint
  task fmt:check
  govulncheck ./...
  ```

- [ ] Run the pinned Node 22 frontend test/lint/build gates even though OVR-205
  changes no frontend source. Keep inherited failures separate from
  regressions.
- [ ] Start the rebuilt binary only with the global kill switch active, live
  trading and scheduler disabled, no provider credentials, isolated schema-73
  PostgreSQL, and isolated Redis. Check `/health`, `/healthz`, and
  `/api/v1/health`, then stop cleanly and prove the retained graph is unchanged.
- [ ] Obtain independent final diff approval with no unresolved P0/P1.
- [ ] Commit the reviewed slices, push `codex/augr-overhaul`, fetch, verify
  local/remote hash equality and `0 0` divergence, then continue to the next
  dependency-ready overhaul item.

## Acceptance evidence to record after implementation

- [ ] Exact Alpaca and Kalshi adapter policies are content-addressed,
  independently reconstructed in Go/PostgreSQL, immutable, and required before
  a venue-policy route can persist.
- [ ] Every known provider state/event has one explicit mapping and retains its
  original value/raw bytes even when the common state does not change.
- [ ] Unknown, malformed, replaced, contradictory, correction, and bust facts
  halt visibly without a guessed order/fill/economic repair.
- [ ] Stable client IDs and deterministic request bodies make retries and fresh
  process recovery converge without a second order.
- [ ] Cancel submission never masquerades as cancellation; current-history
  misses never masquerade as authoritative absence.
- [ ] Every authoritative provider fill has one raw venue observation, one raw
  economic event, one normalization, one ledger transaction, one canonical
  fill, and one lifecycle event; non-authoritative notices and cumulative/
  average facts create no pseudo-fill.
- [ ] Alpaca cancellation, expiry, and rejection remain distinct; Kalshi
  accepts only its exact V2 state vocabulary, fixed-point semantics, exact
  whole-object outcome metadata, and SQL-enforced ticker/book-side projection.
- [ ] Current/historical pagination, duplicate conflicts, partial fills,
  immediate fills, fees, restart, and concurrent retry have real PostgreSQL
  evidence.
- [ ] Migration 73 is additive, immutable, starts empty, rolls back only while
  empty, preserves schema 72, and grants/activates nothing.
- [ ] Legacy runtime behavior remains unchanged; no provider credential, live
  route, scheduler, shared database, or deployment was used.
- [ ] Focused races, repository-wide gates, isolated migration rehearsal,
  kill-switched startup, and independent review are recorded honestly.
