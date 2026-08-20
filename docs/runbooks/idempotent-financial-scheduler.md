# Idempotent Financial Scheduler Runbook

## Boundary

This runbook inspects the synthetic OVR-604 schema-98 financial scheduler
boundary. The retained database is loopback-only. Schema 98 records an inert,
closed job catalog, deterministic occurrences, append-only lease/fence events,
and deterministic effect claims. It does not activate either legacy scheduler,
call a provider, submit an order, settle a real position, or grant deployment
or trading authority.

Use only the dedicated qualification database:

```bash
export FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr604_qual_20260820?sslmode=disable'
psql "$FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL" -Atc \
  'select current_database(),current_schema()'
```

Require `augr_ovr604_qual_20260820` and `public`. Never run qualification
writes against a shared or production database.

## Inspect the catalog and retained occurrences

```bash
psql "$FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL" -x -c \
  "select job_key,mutation_classes,sha256 from financial_job_definitions order by job_key;
   select id,job_key,schedule_revision,trigger_kind,due_at,sha256
     from financial_job_occurrences order by due_at,id"
```

The catalog contains every current automation job plus dynamic
`strategy_execution` and `backtest_execution`. Registration drift fails the
coverage test until its mutation class is reviewed.

Retained evidence includes:

- raced strategy occurrence `f7c14334-7796-09d5-2f69-bccd666fb576`, SHA-256
  `d1657a61bfc835679bc935dfe99d2fc4c52e67a0b221fd73cdcac8f0033aab20`;
- its exact intent effect `e01e45fb-03fe-6d0d-6ff6-8281fab7812e`;
- expiry/takeover occurrence `9cb77ece-1656-7fd0-7cb3-ce06ce917a7a`;
- its fence-2 settlement effect `d0bf9e1c-0487-6050-3a64-6eb0a9434a68`.

## Inspect ownership, fencing, and effects

```bash
psql "$FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL" -x -c \
  "select occurrence_id,sequence,event_kind,owner_id,fence_token,occurred_at,
          lease_expires_at,outcome_sha256
     from financial_job_lease_events order by occurrence_id,sequence;
   select id,occurrence_id,effect_kind,business_key,payload_sha256,owner_id,
          fence_token,sha256,claimed_at
     from financial_job_effect_claims order by occurrence_id,effect_kind,id"
```

For each occurrence, sequence is gapless. First acquisition uses fence 1. A
takeover is allowed only after database-clock expiry and increments the fence.
Only the latest unexpired owner/fence may renew, claim an effect, or complete.
Effects remain uniquely identified by occurrence, effect kind, and business
key across takeover; changed payload reuse is a conflict.

## Recovery and takeover

1. Inspect the latest lease event. A terminal `succeeded` or `failed`
   occurrence is never reacquired.
2. Do not infer expiry from an application host clock. Compare
   `lease_expires_at` with PostgreSQL `clock_timestamp()`.
3. Allow the reviewed runner to acquire an expired nonterminal occurrence. It
   receives the next monotonically increasing fence.
4. The old runner must stop after failed renewal. Regardless of cancellation,
   its stale fence is rejected by the database before any new effect claim.
5. A takeover reuses the same deterministic intent/order/settlement effect
   keys. Never mint a random replacement key.

## Qualification and rollback

```bash
go test -race ./internal/financialscheduler/...
FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL="$FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestFinancialSchedulerRetainedQualification$' -count=1 -v
```

The retained qualification proves eight-writer acquisition convergence,
active-owner exclusion, renewal, terminal non-reacquisition, database-clock
expiry takeover, monotonically increasing fences, stale-owner rejection,
exact effect replay, changed-payload conflict, two independent runners,
transaction rollback at definitions/occurrence/lease/effect/terminal stages,
direct-SQL forgery rejection, append-only enforcement, restart reconstruction,
and nonempty rollback refusal.

Migration 98 is empty-only reversible. A separate dedicated database must pass
`98 -> 97 -> 98`; never delete catalog, occurrence, lease, or effect evidence to
force rollback. Restore a verified pre-migration backup instead.

## Qualification status

- `VERIFIED_LOCAL`: deterministic job catalog, occurrence and effect identities,
  database-clock leasing, fencing, renewal, takeover, append-only persistence,
  concurrency, atomicity, restart, forgery rejection, and rollback behavior.
- `BLOCKED_EXTERNAL`: legacy scheduler cutover, provider access, shared
  migration, real settlement, allocation, broker routing, deployment, and live
  trading.
