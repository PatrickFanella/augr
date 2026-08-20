# Prediction Book and Fee Replay Runbook

## Boundary

This runbook inspects OVR-505 schema-92 research evidence. Retained books and
fees are synthetic manifest observations. They prove point-in-time depth
consumption and exact fee arithmetic, not licensed provider acquisition,
complete-set profitability, maker fill probability, strategy quality, runtime
adoption, scheduling, deployment, venue routing, or live trading.

Use only the dedicated loopback database:

```bash
export PREDICTION_REPLAY_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr505_qual_20260820_v2?sslmode=disable'
psql "$PREDICTION_REPLAY_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, version 92, and `dirty=false`. Never run qualification writes
against a shared or production database.

## Inspect identity, books, and fee policies

```bash
psql "$PREDICTION_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select id,manifest_id,manifest_sha256,manifest_cutoff,book_count,level_count,
          fee_count,replay_count,fill_count,sha256
     from prediction_book_fee_recorders"

psql "$PREDICTION_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select book.sequence,book.market_id,book.outcome_id,book.source_key,
          book.exchange_at,book.available_at,book.revision,book.correction_of,
          level.side,level.level,level.price,level.size
     from prediction_recorded_books book
     join prediction_recorded_book_levels level
       on level.recorder_id=book.recorder_id and level.book_sequence=book.sequence
    order by book.sequence,level.sequence"

psql "$PREDICTION_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select sequence,instrument_id,venue,role,source_key,available_at,
          effective_from,effective_to,formula,rate,scale,rounding
     from prediction_recorded_fee_policies order by sequence"
```

The retained recorder is `cd9a0810-a42e-a8f8-0a2f-604f45b42f92` with SHA-256
`fbfc0229ffeb3d2eacf8460b02884e98493a5b408bb6623e96744c24c07f2787`.
It binds manifest `c0578aa7-155b-a751-ee7c-c6adae73bf3b`. Require three books,
twelve levels, and three policies. `book-yes-correction` must name
`book-yes-original`, share its exchange instant, increment revision, and have a
strictly later availability time.

## Reconstruct replay economics

```bash
psql "$PREDICTION_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select sequence,decision_at,outcome_id,side,role,status,book_source_key,
          fee_source_key,quantity,filled_quantity,residual_quantity,
          weighted_price,gross_cash,fee,net_cash,fill_count
     from prediction_recorded_replays order by sequence"

psql "$PREDICTION_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select replay_sequence,sequence,book_level,price,quantity,gross
     from prediction_recorded_fills order by replay_sequence,sequence"
```

Require original/correction/maker outcomes of `filled`, `partial`, `filled`.
For every replay, `filled + residual = requested`; every fill has
`gross = price * quantity`; replay gross is the fill sum; and net cash is gross
plus fee for buys or gross minus fee for sells. The 20-contract corrected buy
must retain five unfilled contracts.

## Retry, recovery, and rollback

Exact retry converges on the existing recorder. A changed manifest, book,
correction, fee window/formula, replay time, quantity, or limit under the same
logical manifest is an idempotency conflict. Never update or delete retained
evidence; append-only triggers intentionally reject both.

```bash
go test -race ./internal/predictionreplay/...
export NEW_EMPTY_PREDICTION_REPLAY_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/new_empty_schema_92_database?sslmode=disable'
PREDICTION_REPLAY_QUALIFICATION_DB_URL="$NEW_EMPTY_PREDICTION_REPLAY_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestPredictionReplayRetainedQualification$' -count=1 -v
```

The writer requires a new empty schema-92 recorder boundary. Migration 92 is
empty-only reversible: `augr_ovr505_empty_20260820` passed `92 -> 91 -> 92`,
while the retained qualification graph refused rollback. Never delete evidence
to force a downgrade; restore a pre-migration backup if operational rollback is
required.
