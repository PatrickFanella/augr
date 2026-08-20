# Complete-Set Arbitrage Runbook

## Boundary

This runbook inspects OVR-506 schema-93 research evidence. The retained books,
fees, and outcome set are synthetic. They prove exact replay binding, complete
outcome coverage, capital reservation, and deterministic orphan-loss guards;
they do not acquire licensed data, reserve runtime cash, generate execution
intents, schedule work, deploy, route venue orders, or trade live.

Use only the dedicated loopback database:

```bash
export COMPLETE_SET_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr506_qual_20260820_v2?sslmode=disable'
psql "$COMPLETE_SET_QUALIFICATION_DB_URL" -Atc \
  "select current_database(),current_schema(),version,dirty from schema_migrations"
```

Require `public`, version 93, and `dirty=false`. Never run qualification writes
against a shared or production database.

## Inspect candidate identity and economics

```bash
psql "$COMPLETE_SET_QUALIFICATION_DB_URL" -x -c \
  "select id,state,reason,recorder_id,recorder_sha256,candidate_key,market_id,
          outcome_count,set_quantity,payout_per_set,available_capital,
          minimum_profit,entry_cost,payout,after_cost_profit,worst_orphan_key,
          worst_orphan_loss,reserved_capital,profit_after_orphan_guard,sha256
     from complete_set_candidates order by candidate_key"
```

Require three candidates. Qualified candidate
`e0780885-932e-4544-473e-37d4f1f3e615` has SHA-256
`52277d1d03f0b2b746059cfd25ce4a60b9a6f20fc5b9bf3165e79b9286056b20`.
It binds recorder `75a5cfb9-f0a9-cbf3-fbbe-ef89ae9f4f3a`, entry cost `9`,
payout `10`, after-cost profit `1`, worst orphan loss `0.2`, reserved capital
`9.2`, guarded profit `0.8`, and minimum profit `0.5`.

Candidate `4a2ee54d-eabc-6c53-5f0d-275cf4dcb286` must be rejected as
`insufficient_capital`. Candidate `e69e1f7b-2fec-818e-8aea-df862efa4cec`
sets the minimum to the exact guarded profit and must be rejected as
`orphan_guard_failure`, proving the boundary is strict rather than inclusive.

## Reconstruct legs and orphan scenarios

```bash
psql "$COMPLETE_SET_QUALIFICATION_DB_URL" -x -c \
  "select candidate_id,sequence,outcome_id,entry_sequence,unwind_sequence,
          entry_cost,unwind_proceeds,orphan_loss
     from complete_set_legs order by candidate_id,sequence"

psql "$COMPLETE_SET_QUALIFICATION_DB_URL" -x -c \
  "select candidate_id,sequence,scenario_key,entry_cost,unwind_proceeds,loss,
          leg_count
     from complete_set_orphan_scenarios order by candidate_id,sequence"

psql "$COMPLETE_SET_QUALIFICATION_DB_URL" -x -c \
  "select candidate_id,scenario_sequence,sequence,outcome_id,entry_cost,
          unwind_proceeds,loss
     from complete_set_orphan_scenario_legs
    order by candidate_id,scenario_sequence,sequence"
```

Require nine bindings, nine legs, eighteen scenarios, and twenty-seven scenario
legs. Each three-outcome candidate has all six nonempty proper subsets: three
singletons and three two-leg scenarios. Every scenario sum must reconstruct
from its normalized legs, and the candidate's worst key/loss must match the
maximum loss with canonical identity tie-breaking.

## Retry, recovery, and rollback

Exact retry converges on the existing candidate. A changed recorder, outcome
set, replay binding, capital, payout, quantity, or minimum profit under the same
recorder and candidate key is an idempotency conflict. Never update or delete
retained evidence; append-only triggers intentionally reject both.

```bash
go test -race ./internal/completeset/...
export NEW_EMPTY_COMPLETE_SET_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/new_empty_schema_93_database?sslmode=disable'
COMPLETE_SET_QUALIFICATION_DB_URL="$NEW_EMPTY_COMPLETE_SET_DB_URL" \
  go test -race ./internal/repository/postgres \
  -run '^TestCompleteSetRetainedQualification$' -count=1 -v
```

The writer requires a new empty schema-93 complete-set boundary. Migration 93
is empty-only reversible: `augr_ovr506_empty_20260820` passed
`93 -> 92 -> 93`, while the retained qualification graphs refused rollback.
Never delete evidence to force a downgrade; restore a pre-migration backup if
operational rollback is required.
