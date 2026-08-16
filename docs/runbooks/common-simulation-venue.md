# Common simulation venue

This runbook covers the additive OVR-204 simulation policy, deterministic
quote/depth venue, canonical backtest and internal-paper adapters, and migration
72. It is local implementation evidence. Migration 72 grants no writer,
activates no simulator or scheduler, migrates no shared data, and does not cut
over any legacy broker, order, trade, position, backtest, or paper runtime path.

## Safety boundary

- Use only a disposable or explicitly retained loopback PostgreSQL database.
  Never aim these commands at production or a shared environment.
- Keep `ENABLE_LIVE_TRADING=false`, `TRADING_AGENT_KILL=true`, and
  `ENABLE_SCHEDULER=false`; omit every venue credential during qualification.
- Registering a policy artifact does not select a current policy and does not
  authorize routing. A simulation order names the exact artifact version that
  governs it forever.
- A legacy bar, close, spread assumption, ticker order, or paper broker fill is
  compatibility evidence only. It remains labeled `backtest-input-v1` and is
  not canonical OVR-204 evidence.
- `paper_stress` always carries `synthetic_stress`. It can never become
  promotion evidence or share a comparison population or storage namespace
  with `paper_scored`.

## Policy construction and identity

`simulation.NewPolicy` accepts explicit per-asset capabilities. There are no
implicit values for order types, time in force, quote requirements, depth
participation, latency, calendar, fees, or rounding:

```go
policy, err := simulation.NewPolicy(simulation.PolicyInput{
    Schema: simulation.PolicySchemaV1,
    Assets: []simulation.AssetPolicy{{
        AssetClass: instrument.AssetClassEquity,
        OrderTypes: []lifecycle.OrderType{
            lifecycle.OrderMarket,
            lifecycle.OrderLimit,
        },
        TimeInForce: []lifecycle.TimeInForce{
            lifecycle.TimeInForceDay,
            lifecycle.TimeInForceGTC,
            lifecycle.TimeInForceIOC,
            lifecycle.TimeInForceFOK,
        },
        QuoteRequirements: marketdata.QuoteRequirements{
            RequireSource: true, RequireVenueContract: true,
            RequireBid: true, RequireAsk: true,
            RequireBidDepth: true, RequireAskDepth: true,
            RequireMarketStatus: true, RequireSessionStatus: true,
            AllowedMarketStatuses: []string{"open"},
            AllowedSessionStatuses: []string{"regular"},
            MaxAge: 2 * time.Second,
        },
        MaxDepthParticipation: decimal.RequireFromString("0.25"),
        FixedLatency: 100 * time.Millisecond,
        Calendar: simulation.CalendarPolicy{
            Kind: simulation.CalendarExplicitSessions,
            Sessions: []simulation.SessionWindow{{
                Label: "2026-08-17-regular",
                OpenAt: openAt,
                CloseAt: closeAt,
            }},
        },
        Fees: simulation.FeePolicy{
            PerOrder: decimal.RequireFromString("1.25"),
            PerUnit: decimal.RequireFromString("0.01"),
            NotionalBPS: decimal.RequireFromString("2"),
            Scale: 4,
        },
    }},
})
```

Inspect the content identity before registration:

```go
fmt.Println(policy.Version())       // simulation-policy-v1@sha256:<64 hex>
fmt.Println(policy.Digest())        // lowercase 64-character SHA-256
fmt.Println(policy.ArtifactID())    // deterministic UUID of the full version
fmt.Println(string(policy.CanonicalBytes()))
```

Canonical bytes use fixed JSON structs, exact decimal strings, UTC
microsecond timestamps, normalized/sorted sets, and integer duration
nanoseconds. Allowed market/session statuses use lowercase canonical tokens
matching `[a-z0-9][a-z0-9_.:-]{0,63}`; explicit-session labels use
`[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}`. Those bounded alphabets make the exact
Go encoding independently reconstructable inside PostgreSQL rather than
trusting caller-authored JSON. Input ordering does not change identity; any
economic or capability change does. Never reconstruct a historical policy
from a mutable configuration file.

## Calendars and supported capabilities

An asset policy uses exactly one calendar kind:

- `explicit_sessions`: sorted, nonoverlapping `[open_at, close_at)` UTC windows.
  A holiday is a gap. A half-day uses its actual close. `DAY` is permitted only
  when the route time belongs to one declared session and expires at that
  session's close.
- `continuous_24x7`: no close boundary and no `DAY` support.

V1 supports equity, ETF, option, crypto-spot, and prediction-contract
instruments only when that asset class is explicitly configured. Futures,
stop/stop-limit orders, GTD, passive maker assumptions, and missing asset
policies fail closed. Options remain single-leg and use the dated venue
contract's exact lot and multiplier.

## Durable registration and recovery

Register the exact artifact before persisting an order that names it:

```go
artifact, err := simulation.RegisterPolicy(
    ctx,
    postgres.NewSimulationPolicyRepo(pool),
    policy,
    createdAt,
)
```

Identical registration replays. Changed bytes under the same identity conflict.
Recovery starts from `execution_orders.policy_version`, never a current-policy
pointer:

```go
artifact, err := postgres.NewSimulationPolicyRepo(pool).
    GetSimulationPolicyByVersion(ctx, aggregate.Order.PolicyVersion)
policy, err := simulation.PolicyFromArtifact(*artifact)
venue, err := simulation.NewVenue(policy)
```

Migration 72 independently validates the complete fixed v1 shape, supported
assets/capabilities, exact decimals/integers, required quote flags, sorted
unique token arrays, calendar/session rules, UTC timestamps, and bounded token
alphabets. It reconstructs the exact fixed-order bytes and requires them to
equal `canonical_bytes`, then checks SHA-256, version, deterministic UUID,
parsed JSON equality, schema, and append-only behavior. Correctly rehashed JSON
with empty assets, missing fields, reordered arrays/fields, duplicate keys, or
trailing whitespace therefore cannot register and cannot authorize an order.
The migration refuses to apply over a schema-71 simulation order because
missing historical bytes cannot be guessed defensibly. Venue-policy orders
remain independent.

## Executable observation requirements

Every evaluation supplies one active account, canonical instrument, dated
venue contract, routed OVR-203 aggregate, OVR-202 snapshot, and explicit UTC
evaluation time. `QuoteSnapshot.AssessForExecution` enforces availability,
age, source, bid, ask, both depth sides, market/session status, reference
identity, contract window, tick, and lot rules selected by the policy.

The venue never treats an OHLCV close, configured spread, limit, stop, or
default number as executable evidence. A missing, stale, future, mismatched, or
mechanically invalid observation fails closed. Eligible depth is the opposite
book side, price-time ordered, capped by exact participation, floored to venue
lot size, and emitted as one immutable fill per consumed level.

## Evaluation, raw-first persistence, and restart

Both canonical adapters accept the same `simulation.EvaluationRequest` and
return the same `simulation.Result`:

```go
backtestPath, err := backtest.NewCommonSimulation(policy)
paperPath, err := paper.NewCommonSimulation(policy)

result, err := backtestPath.Evaluate(request)
paperResult, err := paperPath.Evaluate(request)
```

The internal-paper adapter adds only the ADR-018 check: the account and
aggregate must match and must be either `paper_scored` or `paper_stress`.
It does not modify timing, price, fee, evidence, transition, or policy behavior.

Persist each result through the raw-first coordinator:

```go
aggregate, err := simulation.PersistResult(
    ctx,
    persistence,
    request.Account.ID,
    result,
)
```

For every fill, `PersistResult` records its `economic_source_events` row first,
then calls the OVR-203 atomic fill writer. That second transaction commits the
normalization, ledger transaction/postings, optional first binding, lifecycle
fill, and event together. Non-fill transitions never create raw economic
evidence. A crash can therefore leave a raw event alone or a complete economic
graph, never a partial graph.

On restart:

1. Keep routing disabled and load the aggregate through
   `GetExecutionLifecycle`.
2. Load the policy artifact by the order's recorded version and construct a new
   venue from those bytes.
3. Re-evaluate the same durable snapshot or the next snapshot against the
   reloaded aggregate.
4. Persist normally. Already-recorded `(order, snapshot, depth side, level)`
   source identities are skipped; later levels retain their original identities
   and fees.

Do not invent a new source identity to work around an idempotency conflict.

## Outcome hash and ADR-018 population boundary

`simulation.NewOutcome` projects ordered `(quantity, price, fee)` fills, final
state, total quantity, gross cash, total fee, policy version, side, currency,
multiplier, environment, evidence class, and storage namespace into fixed JSON
and SHA-256. Process-local creation times and opaque UUIDs are excluded.

Backtest and internal paper must produce the same outcome hash only when fed the
same observations under the same ADR-018 mode. Otherwise identical scored and
stress economics intentionally hash differently because classification is an
economic-evidence boundary.

## Legacy compatibility boundary

The reusable float/bar fill engine, historical depth fill, latency, passive
queue, adverse-selection/ghost-fill, and option-fill primitives now live in
`internal/simulation/legacy_*.go`. `internal/backtest` retains source-compatible
aliases and wrappers. Their stochastic draws and bar assumptions are not the
canonical deterministic v1 venue and their reports remain
`backtest-input-v1`.

The legacy in-memory paper broker also remains noncanonical. Its compatibility
option calculation delegates to the relocated primitive, but an explicitly
priced order is still required. A market order without an executable reference
is rejected; it no longer invents `$1.00`.

## Read-only inspection

Confirm schema state:

```sql
SELECT version, dirty FROM schema_migrations;
```

Inspect artifacts and independently recompute their identity:

```sql
SELECT
    id,
    schema_name,
    policy_version,
    sha256,
    encode(digest(canonical_bytes, 'sha256'), 'hex') AS recomputed_sha256,
    canonical_bytes = simulation_policy_v1_canonical_bytes(canonical_json)
        AS database_canonical_bytes_match,
    canonical_json,
    created_at
FROM simulation_policy_artifacts
ORDER BY created_at, id;
```

Inspect simulation orders with their governing bytes:

```sql
SELECT
    execution_order.id AS order_id,
    execution_order.policy_version,
    artifact.id AS artifact_id,
    artifact.sha256,
    octet_length(artifact.canonical_bytes) AS canonical_byte_count
FROM execution_orders AS execution_order
JOIN simulation_policy_artifacts AS artifact
  ON artifact.policy_version = execution_order.policy_version
WHERE execution_order.policy_kind = 'simulation'
ORDER BY execution_order.created_at, execution_order.id;
```

Inspect the raw-to-ledger-to-lifecycle fill graph:

```sql
SELECT
    source_event.id AS raw_id,
    source_event.source_event_id,
    normalization.id AS normalization_id,
    normalization.ledger_transaction_id,
    fill.id AS fill_id,
    fill.quantity,
    fill.price,
    lifecycle_event.kind,
    lifecycle_event.policy_version
FROM economic_source_events AS source_event
JOIN economic_event_normalizations AS normalization
  ON normalization.source_event_id = source_event.id
JOIN execution_fills AS fill
  ON fill.normalization_id = normalization.id
JOIN execution_lifecycle_events AS lifecycle_event
  ON lifecycle_event.fill_id = fill.id
WHERE source_event.source = 'simulation'
ORDER BY lifecycle_event.ingest_sequence;
```

## Migration and rollback rehearsal

Build a new loopback-only database from migration 1 through 72:

```bash
DB_URL="$AUGR_PHASE2_DB_URL" task migrate:up
DB_URL="$AUGR_PHASE2_DB_URL" task migrate:status
DB_URL="$AUGR_PHASE2_DB_URL" go test -race -count=1 \
  -run 'SimulationPolicy|SimulationVenue' \
  ./migrations ./internal/repository/postgres
```

An empty disposable database may rehearse `72 -> 71 -> 72`:

```bash
DB_URL="$AUGR_PHASE2_EMPTY_DB_URL" task migrate:down
DB_URL="$AUGR_PHASE2_EMPTY_DB_URL" task migrate:status
DB_URL="$AUGR_PHASE2_EMPTY_DB_URL" task migrate:up
DB_URL="$AUGR_PHASE2_EMPTY_DB_URL" task migrate:status
```

The down migration first takes `ACCESS EXCLUSIVE` locks on artifacts and
orders. It refuses if any artifact or simulation order exists. On a retained
rehearsal database, verify that refusal preserves counts; never delete evidence
to force a downgrade. Quiesce writers before even an empty rollback rehearsal.

## Local qualification boundary

The authoritative OVR-204 checks are the focused simulation, adapter,
repository, and migration race suites plus an isolated persistent replay.
Repository-wide database tests contain inherited shared-schema and legacy
JSON/column assumptions; record them separately instead of weakening focused
assertions. A kill-switched health smoke proves only that the rebuilt local
binary starts against schema 72 and isolated Redis. It does not prove
deployment, provider readiness, scheduler activation, protected migration,
external-paper fidelity, or live execution safety.
