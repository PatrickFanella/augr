# Common execution lifecycle

This runbook covers the additive OVR-203 intent, order, binding, fill, and
lifecycle-event boundary introduced by migration 71. It is local implementation
evidence, not authorization to route an order. Migration 71 grants no writer,
activates no provider or simulator, changes no scheduler, and does not dual-write
the legacy `orders`, `trades`, or `positions` tables.

## Safety boundary

- Use only a reviewed schema-71 database. Do not point the rehearsal commands at
  production or a shared environment.
- Keep `ENABLE_LIVE_TRADING=false`, `TRADING_AGENT_KILL=true`, every scheduler
  disabled, and venue credentials absent during local qualification.
- For the scheduler-disabled smoke, do not provide an Ollama/embedding API key.
  The inherited runtime currently constructs the automation orchestrator when
  an embedding provider is available even if `ENABLE_SCHEDULER=false`.
  OVR-203 does not change that legacy startup behavior; the qualified smoke
  deliberately leaves the embedding provider unavailable and confirms that no
  automation orchestrator starts.
- A lifecycle intent records a desired signed economic change. It is not proof
  that an order was submitted.
- `routed`, `working`, and `partially_filled` mean that recovery must inspect the
  already-created stable client order identity. They never authorize creating a
  replacement order.
- `failed_reconciliation` is terminal evidence. It does not authorize editing a
  fill, normalization, ledger transaction, or venue binding.

## Crash boundary

Raw provider or simulator evidence is committed first in
`economic_source_events`. `ApplyExecutionFill` then uses one PostgreSQL
transaction and one locked intent row to write all of the following:

1. the OVR-103 `execution_fill` normalization;
2. its deterministic ledger transaction and exact postings;
3. the external order binding, when the first observation is already a fill;
4. the immutable lifecycle fill; and
5. `fill_acknowledged` or `fill_recorded` with exact cumulative quantity.

Migration 71 installs deferred reciprocal constraints. Commit succeeds only when
the normalization and lifecycle fill identify each other one-to-one and the fill
has exactly one matching lifecycle event. A crash therefore leaves either the
raw source row alone or the complete economic graph. Retrying from the retained
raw row is safe; inventing a new source identity is not.

## Required source and idempotency facts

An adapter must retain these facts exactly:

- account and immutable environment;
- canonical instrument and dated venue contract;
- decision and route quote snapshot IDs, each available by the recorded
  decision time;
- intent and order idempotency keys;
- stable client order ID derived from the canonical order UUID;
- source, namespace, provider event ID, revision, source time, receive time, and
  exact JSON object bytes;
- versioned simulation or venue policy;
- execution origin and optional strategy version;
- positive exact fill quantity and present nonnegative price. Exact zero is a
  valid present price; SQL `NULL` is not.

Ordinary event identity deliberately excludes `source_revision`. Reusing an
ordinary provider event ID with changed revision or evidence conflicts. A real
correction or bust uses the separate `correction` or `bust` class, references the
original fill and source execution ID, and supplies an immutable provider
discriminator. When the provider only revises in place, the discriminator is
`revision:<source_revision>`. These observations append one terminal
reconciliation failure and create no new economic effect.

Correction and bust UUIDs are derived from the original source execution ID,
not the provider ID of the later correction observation. The later
`source_event_id` remains exact immutable payload evidence: changing it under
the same original source ID and discriminator is an idempotency conflict, not a
new correction. `cumulative_fill_quantity` is present exactly on
`fill_acknowledged` and `fill_recorded`; both Go replay and PostgreSQL reject it
on correction, bust, or every other non-fill event.

## Read-only inspection

Confirm the schema version before inspecting evidence:

```sql
SELECT version, dirty FROM schema_migrations;
```

Inspect one account's intent headers and replay order:

```sql
SELECT
    intent.id,
    intent.idempotency_key,
    intent.environment,
    intent.instrument_id,
    intent.desired_quantity_delta,
    intent.decision_at,
    event.ingest_sequence,
    event.kind,
    event.prior_state,
    event.next_state,
    event.source,
    event.source_namespace,
    event.source_event_id,
    event.source_revision,
    event.reason_code
FROM execution_intents AS intent
JOIN execution_lifecycle_events AS event ON event.intent_id = intent.id
WHERE intent.account_id = '<account UUID>'::UUID
ORDER BY intent.id, event.ingest_sequence;
```

List only current recovery candidates. Filtering must happen after selecting the
latest event; filtering all historical `routed` rows would produce false
positives:

```sql
SELECT intent_id, next_state, ingest_sequence
FROM (
    SELECT DISTINCT ON (intent_id)
        intent_id,
        next_state,
        ingest_sequence
    FROM execution_lifecycle_events
    WHERE account_id = '<account UUID>'::UUID
    ORDER BY intent_id, ingest_sequence DESC
) AS latest
WHERE next_state IN ('routed', 'working', 'partially_filled')
ORDER BY ingest_sequence, intent_id;
```

Inspect the one-to-one fill graph:

```sql
SELECT
    fill.id AS fill_id,
    fill.order_id,
    fill.economic_source_event_id,
    fill.normalization_id,
    fill.ledger_transaction_id,
    fill.quantity,
    fill.price,
    normalization.reference_type,
    normalization.reference_id,
    event.kind,
    event.cumulative_fill_quantity
FROM execution_fills AS fill
JOIN economic_event_normalizations AS normalization
  ON normalization.id = fill.normalization_id
JOIN execution_lifecycle_events AS event
  ON event.fill_id = fill.id
ORDER BY fill.intent_id, event.ingest_sequence;
```

The following query must return no row:

```sql
SELECT normalization.id
FROM economic_event_normalizations AS normalization
LEFT JOIN execution_fills AS fill
  ON fill.normalization_id = normalization.id
 AND fill.id::TEXT = normalization.reference_id
WHERE normalization.reference_type = 'execution_fill'
  AND fill.id IS NULL;
```

## Recovery procedure

1. Keep routing disabled and preserve the raw source row.
2. Load the full lifecycle through `GetExecutionLifecycle`; do not infer state
   from a mutable status column.
3. For `routed`, query the simulator or venue with the existing
   `client_order_id`. Do not submit a second order.
4. For `working` or `partially_filled`, query using the immutable external
   binding and ingest each provider observation under its original identity.
5. Replay an identical observation normally. A mismatched retry is an
   idempotency conflict and requires investigation.
6. Record ambiguous, contradictory, corrected, or busted provider state as the
   corresponding reconciliation-failure event. Do not edit history.
7. Escalate `failed_reconciliation`; OVR-203 intentionally contains no repair or
   economic-reversal authority.

## Migration and rollback rehearsal

Use the loopback-only development URL from `docs/development-setup.md`:

```bash
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" version
migrate -path migrations -database "$AUGR_PHASE1_DB_URL" up 1
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 \
  -run '^TestCommonExecutionLifecycleMigration' ./migrations
DB_URL="$AUGR_PHASE1_DB_URL" go test -race -count=1 \
  -run '^TestExecutionLifecycleRepo' ./internal/repository/postgres
```

The down migration takes `ACCESS EXCLUSIVE` locks on the normalization and all
schema-71 tables. It succeeds only when all lifecycle tables and all
`execution_fill` normalizations are empty. Any retained lifecycle evidence makes
rollback fail without deleting it. Quiesce writers before even an empty
rehearsal.

On an explicitly disposable schema-71 database, the opt-in retained-evidence
rehearsal is:

```bash
AUGR_EXECUTION_LIFECYCLE_REHEARSAL_DB_URL="$AUGR_PHASE1_DB_URL" \
  go test -race -count=1 \
  -run '^TestExecutionLifecyclePersistentRehearsal$' \
  ./internal/repository/postgres
```

It creates one immediate-fill and one multiple-partial-fill lifecycle, reloads
them through a fresh repository, and proves nonempty rollback refusal. The
explicit environment name is intentional because this test retains evidence.

## Local qualification boundary

Focused domain, repository, and migration suites are the authoritative OVR-203
checks. Repository-wide database tests still contain inherited shared-schema and
legacy JSON/column assumptions; record those separately rather than weakening
the focused tests. Local health checks establish only that a kill-switched build
can start against schema 71. They do not prove deployment, provider readiness,
protected-environment migration, or live execution safety.
