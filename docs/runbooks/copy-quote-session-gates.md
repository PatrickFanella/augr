# Copy Quote and Session Gates Runbook

## Boundary

This runbook inspects OVR-502 schema-89 paper evidence. It never sources a
quote, routes an order, mutates a shared database, or authorizes live trading.
The retained qualification uses synthetic point-in-time fixtures; it is not
evidence of a licensed live feed.

Use the dedicated loopback database:

```bash
export COPY_QUOTE_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr502_qual_20260820_v3?sslmode=disable'
psql "$COPY_QUOTE_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, clean version 89. Do not run these checks against a shared or
production database.

## Inspect subscription policy

```bash
psql "$COPY_QUOTE_QUALIFICATION_DB_URL" -x -c \
  "select id,origin_type,origin_id,max_quote_age_seconds,
          max_spread_bps,allowed_sessions,status,is_paper
     from copy_subscriptions order by id"
```

Require exact `copy_subscription/<id>` attribution, a positive quote-age
limit, a nonnegative spread limit, a nonempty closed session allowlist, and
paper-only status. The retained subscription ID is
`5072fed9-4cfe-4a32-8fc8-bd67a8267f46`.

## Reconstruct every decision

```bash
psql "$COPY_QUOTE_QUALIFICATION_DB_URL" -x -c \
  "select id,subscription_id,side,status,policy_status,policy_reasons,
          quote_gate_version,decision_quote_snapshot_id,decision_bid,
          decision_ask,decision_spread_bps,executable_price,
          decision_available_at,decision_at,decision_market_status,
          decision_session_status
     from copy_trade_intents
    where quote_gate_version=1
    order by created_at,id"
```

Earlier schema-88 compatibility rows intentionally have gate version zero and
null quote evidence, so they are excluded from this OVR-502 audit.

For an approved buy, `executable_price=decision_ask`; for an approved sell,
`executable_price=decision_bid`. Recompute spread as
`(ask-bid)/((ask+bid)/2)*10000`, rounded to 12 decimal places. Require an open
market, an allowed session, and `decision_at-quote_available_at` within policy.
The retained schema-89 inventory is two gate-version-1 approved intents and one
gate-version-1 skipped stale intent.
The exact-boundary buy quote is
`35fa2641-95f2-438a-8918-37be89d571d8`; the sell quote is
`39f33368-6517-4df7-b570-5571496abeeb`.

## Diagnose a rejection

Treat the persisted `policy_reasons` as the engine result, then inspect the exact
snapshot and canonical alias. Missing alias, missing or one-sided values,
crossed markets, future availability, staleness, closed/halted status,
disallowed session, excessive derived spread, inadequate ADV, or minimum-price
failure must remain rejected. A daily close is liquidity context only and can
never supply execution approval.

Never repair a rejection by updating immutable intent evidence. Correct the
upstream fixture or provider adapter and create a newly keyed decision. An
exact retry may converge; a changed retry must return an idempotency conflict.

## Verification and recovery

```bash
go test -race ./internal/copytrading ./internal/copyorigin
COPY_QUOTE_QUALIFICATION_DB_URL="$COPY_QUOTE_QUALIFICATION_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run TestCopyQuoteRetainedQualification -count=1 -v
```

Migration 89 is empty-only reversible. On a separate empty disposable
database require `89 -> 88 -> 89`. A database containing subscription policy
or quote-gated intent evidence must refuse rollback. Do not delete retained
evidence to force rollback; recover from a pre-migration backup if rollback is
operationally required.
