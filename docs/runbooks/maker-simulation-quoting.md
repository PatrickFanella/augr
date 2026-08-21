# Maker Simulation and Quoting Runbook

## Boundary

This runbook inspects OVR-507 schema-94 research evidence. Retained books,
fees, queue outflow, weights, and inventory costs are synthetic. They prove
deterministic queue consumption and net-capture arithmetic, not licensed trade
flow, real queue priority, calibrated scenario probability, runtime capital
reservation, scheduling, deployment, venue routing, or live trading.

Use only the dedicated loopback database:

```bash
export MAKER_QUOTE_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr507_qual_20260820_v2?sslmode=disable'
psql "$MAKER_QUOTE_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, version 94, and `dirty=false`. Never run qualification writes
against a shared or production database.

## Inspect quote identity and expected economics

```bash
psql "$MAKER_QUOTE_QUALIFICATION_DB_URL" -x -c \
  "select id,state,reason,recorder_id,recorder_sha256,candidate_key,market_id,
          outcome_id,side,decision_at,quote_book_source_key,venue,quote_price,
          quote_quantity,displayed_queue,prior_queue,queue_ahead,
          starting_inventory,inventory_limit,hourly_inventory_cost_rate,
          minimum_expected_net,filled_scenario_count,expected_gross_capture,
          expected_maker_fee,expected_inventory_cost,expected_net_capture,sha256
     from maker_quote_candidates order by candidate_key"
```

Require five candidates. Qualified sell candidate
`c06f1c08-93fd-b589-acd5-b8aad1df1382` has SHA-256
`7d5e2f7283a47fb7970da1417f6f1ef1bba1ecb3a4fe873e521c8f1494a1768e`
and expected net capture `0.02985`, strictly above minimum `0.01` after exact
maker fees and inventory cost. Qualified buy candidate
`05c7b77b-6a8e-71d0-86c4-4dafaadd6e39` has SHA-256
`18ca8721832ba48d8a14ac0505831cbf5526d63c87080056984ceabbd207b632`
and expected net capture `0.0408425`. High-cost candidate
`06553e27-fdb1-475e-416e-4f944fd72ccf` must be nonpositive; queue-equality
candidate `e5609fa5-39e2-bf57-d9f8-ab09de160191` must be `no_fill`; strict
boundary candidate `2d05c9c6-93a9-da84-0ff1-f3925383946c` sets minimum to
`0.02985` and must be rejected as `nonpositive_net_capture`.

## Reconstruct queue fills, marks, and costs

```bash
psql "$MAKER_QUOTE_QUALIFICATION_DB_URL" -x -c \
  "select candidate_id,sequence,scenario_key,weight,horizon_at,queue_outflow,
          mark_book_source_key,mark_price,filled_quantity,residual_quantity,
          post_fill_inventory,gross_capture,maker_fee,inventory_cost,net_capture
     from maker_quote_scenarios order by candidate_id,sequence"
```

Require ten scenarios. For each candidate, weights sum exactly to one.
Displayed size plus prior queue equals queue ahead; outflow equal to queue ahead
fills zero; only excess outflow fills, capped at quote quantity. Recompute each
net as gross capture minus fee minus inventory cost, then weight and sum it to
the parent expected values. Quote and mark book source keys must reconstruct
from recorder `cd9a0810-a42e-a8f8-0a2f-604f45b42f92` at their respective
decision/horizon availability times.

## Retry, recovery, and rollback

Exact retry converges. A changed recorder, quote, queue, scenario, inventory,
cost rate, or minimum under the same recorder and candidate key is an
idempotency conflict. Never update or delete retained evidence; append-only
triggers intentionally reject both.

```bash
go test -race ./internal/makerquote/...
export NEW_EMPTY_MAKER_QUOTE_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/new_empty_schema_94_database?sslmode=disable'
MAKER_QUOTE_QUALIFICATION_DB_URL="$NEW_EMPTY_MAKER_QUOTE_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestMakerQuoteRetainedQualification$' -count=1 -v
```

Migration 94 is empty-only reversible: `augr_ovr507_empty_20260820` passed
`94 -> 93 -> 94`, while retained qualification evidence refused rollback. An
expected failed `golang-migrate down 1` can leave only its version metadata at
dirty 93 even though transactional DDL preserved every table and row. On a
dedicated qualification database only, first inspect the retained tables and
then restore metadata with `migrate ... force 94`. Do not use `force` as an
operational rollback procedure; restore a verified pre-migration backup.
