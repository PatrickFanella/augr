# Copy-Subscription Origins Runbook

## Boundary

This runbook verifies OVR-501 schema-88 paper evidence. A copy subscription is
its own execution origin, exactly `copy_subscription/<subscription UUID>`. New
subscriptions must not create legacy strategy rows or claim an OVR-302 strategy
version. This workflow does not enable live trading, route an order, delete a
historical strategy, or migrate a shared database.

Use a dedicated loopback database:

```bash
export COPY_ORIGIN_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr501_qual_20260820_v3?sslmode=disable'
psql "$COPY_ORIGIN_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, clean version 88, and zero copy subscriptions/origin runs
before the retained writer runs.

## Exact subscription attribution

```bash
psql "$COPY_ORIGIN_QUALIFICATION_DB_URL" -x -c \
  "select id,origin_type,origin_id,legacy_strategy_id,status,is_paper
     from copy_subscriptions order by id"
```

For every origin-native row, require `origin_type=copy_subscription`,
`origin_id=id`, `legacy_strategy_id is null`, and `is_paper=true`. A nonnull
legacy strategy is historical compatibility evidence only. Never synthesize a
new strategy to fill that column and never delete the referenced historical
row as part of this runbook.

## Run and intent reconstruction

```bash
psql "$COPY_ORIGIN_QUALIFICATION_DB_URL" -x -c \
  "select run.id,run.subscription_id,run.origin_type,run.origin_id,
          run.source_observation_id,run.calculation_version,run.intent_count,
          run.sha256,child.sequence,child.intent_id,child.instrument_key
     from copy_origin_rebalance_runs run
     join copy_origin_rebalance_intents child on child.run_id=run.id
    order by run.id,child.sequence"
```

The run, subscription, and every `copy_trade_intents` child must have the same
origin and source observation. Child order is canonical instrument-key order.
The common lifecycle proposal must repeat that origin and must have an empty
`strategy_version_id`. An approved copy intent without OVR-502 decision-quote
evidence remains prepared rather than being routed.

## Verification and recovery

```bash
go test -race ./internal/copyorigin ./internal/copytrading
COPY_ORIGIN_QUALIFICATION_DB_URL="$COPY_ORIGIN_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run TestCopyOriginRetainedQualification -count=1 -v
```

Expected retained inventory is two subscriptions, two copy intents, one origin
run, two normalized run-intent rows, and zero strategy rows. Eight identical
writers converge. Injected failure after the run or child stage leaves zero
run rows. Mutation must fail with `copy origin evidence is append-only`.

Migration 88 is empty-only reversible. On a separate empty disposable
database require `88 -> 87 -> 88`. A database containing an origin-native
subscription or run must refuse rollback. Do not delete retained evidence to
force rollback.
