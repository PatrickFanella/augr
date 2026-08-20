# Point-in-Time 13F Replay Runbook

## Boundary

This runbook inspects OVR-504 schema-91 research evidence. The retained manager
scores and filings are synthetic manifest observations. They prove deterministic
point-in-time selection and persistence, not licensed SEC acquisition, manager
fitness, strategy profitability, runtime adoption, promotion, scheduling,
deployment, broker routing, or live trading.

Use only the dedicated loopback database:

```bash
export COPY_REPLAY_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr504_qual_20260820_v2?sslmode=disable'
psql "$COPY_REPLAY_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, version 91, and `dirty=false`. Never run qualification writes
against a shared or production database.

## Inspect canonical identity and manager selection

```bash
psql "$COPY_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select id,manifest_id,manifest_sha256,manifest_cutoff,selection_cutoff,
          top_n,candidate_count,filing_count,manager_count,decision_count,
          step_count,sha256
     from copy_13f_replays"

psql "$COPY_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select candidate.sequence,candidate.manager_id,candidate.available_at,
          candidate.eligible,candidate.score,
          manager.rank is not null as selected,manager.rank
     from copy_13f_replay_candidates candidate
     left join copy_13f_replay_managers manager
       on manager.replay_id=candidate.replay_id
      and manager.manager_id=candidate.manager_id
    order by candidate.sequence"
```

The retained replay is `6316e44e-9434-3a7f-82c5-24162ab83f45` with SHA-256
`0a9749dbcbe580b2a64fa36cbaa17c205923437ebf4c71a32ae75adb3e54c28a`.
It binds manifest `b4fb186f-9a75-3379-411e-2142084487f6`. Exactly two managers
are selected: `10000000-0000-4000-8000-000000000001` at rank 0/score 10 and
`20000000-0000-4000-8000-000000000002` at rank 1/score 9. The late score-100
candidate must remain unselected because it was unavailable at selection time.

## Prove no lookahead and reconstruct research steps

```bash
psql "$COPY_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select sequence,decision_at,manager_id,status,filing_source_key,
          filing_available_at,report_period,amendment_number
     from copy_13f_replay_decisions order by sequence"

psql "$COPY_REPLAY_QUALIFICATION_DB_URL" -x -c \
  "select sequence,decision_sequence,observation_source_key,
          observation_content_sha256,available_at,decision
     from copy_13f_replay_steps order by sequence"
```

Require ten decisions in decision-time/manager order: two `no_filing`, two Q1
`selected`, two Q1 `unchanged`, manager A's Q1 amendment plus manager B's
unchanged Q1, and finally manager A's Q2 original plus manager B's unchanged
Q1. The four steps must reference only `a-q1`, `b-q1`, `a-q1-amendment`, and
`a-q2`; each is an OVR-303 no-op research decision. A later row must never
alter the canonical JSON of an earlier decision.

## Retry, recovery, and rollback

An exact retry converges on the existing replay. A changed manifest, selection
cutoff, top-N policy, decision calendar, candidate, filing, or normalized graph
under the same logical key is an idempotency conflict. Do not update or delete
retained evidence; append-only triggers intentionally reject both operations.

```bash
go test -race ./internal/copyreplay/...
export NEW_EMPTY_COPY_REPLAY_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/new_empty_schema_91_database?sslmode=disable'
COPY_REPLAY_QUALIFICATION_DB_URL="$NEW_EMPTY_COPY_REPLAY_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestCopyReplayRetainedQualification$' -count=1 -v
```

The retained writer requires a new empty schema-91 replay boundary. Migration
91 is empty-only reversible: `augr_ovr504_empty_20260820` passed
`91 -> 90 -> 91`, while the retained qualification graph refused rollback.
Never delete evidence to force a downgrade. Restore a pre-migration backup if
an operational rollback is required.
