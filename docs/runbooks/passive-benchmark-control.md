# Passive Benchmark Control Runbook

This runbook operates OVR-401 research controls introduced by migration 82.
The boundary records an experiment-scoped passive benchmark declaration and a
deterministic opportunity-cost report. It does not choose a benchmark after a
result is known, rank a strategy, promote a candidate, allocate capital,
schedule work, activate a runtime, or submit an order.

## Preconditions

- Use a dedicated loopback database or explicitly authorized environment.
- Confirm the runtime and database both require schema 82.
- Load the exact OVR-302 experiment, dataset manifest, canonical benchmark
  instrument, and completed OVR-304 evaluation by ID and SHA-256.
- Confirm the proposed benchmark was declared for the experiment before using
  the evaluation as decision evidence. The service cannot prove wall-clock
  intent; review provenance and creation history independently.
- Verify licensed point-in-time evidence covers every evaluation timestamp.
  Never synthesize missing values or substitute a later benchmark constituent.

## Declaration review

Inspect `passive_benchmark_declarations` and require:

- exact experiment and manifest IDs/hashes;
- one canonical benchmark instrument;
- `buy_and_hold` or `total_return_index` benchmark kind;
- `single_asset` weighting and `reinvested` distributions;
- `explicit_per_period` cash returns;
- exact frequency, experiment window, initial notional, and decimal scale;
- a complete ordered observation graph spanning both window endpoints.

For every row in `passive_benchmark_observations`, verify the timestamp,
benchmark value, cash return, source evidence ID, and source SHA-256. The
deferred migration constraints reject gaps, frequency drift, parent mismatch,
canonical divergence, and incomplete graphs at commit.

## Deterministic report replay

Use `benchmark.Service.Evaluate` with the exact declaration and evaluation ID.
It reloads the experiment, manifest, and evaluation before calculating. A
caller supplies no return, score, pass boolean, ranking, or recommendation.

The report derives:

- after-cost strategy total return from terminal/initial portfolio equity;
- benchmark total return from terminal/initial declared benchmark value;
- cash total return by compounding explicit per-period cash returns;
- benchmark and cash opportunity cost as control return minus strategy return;
- normalized terminal wealth for each control and both wealth differences.

PostgreSQL recalculates the graph and all values at deferred commit. On reload,
the repository reconstructs both canonical parents and compares every
normalized declaration observation.

## Inspection queries

Always scope queries to explicit IDs. There is intentionally no latest, best,
winner, or current-benchmark query.

```sql
SELECT id, sha256, experiment_id, experiment_sha256, manifest_id,
       manifest_sha256, benchmark_instrument_id, benchmark_kind, frequency,
       evaluation_start, evaluation_end, observation_count
FROM passive_benchmark_declarations
WHERE id = $1;

SELECT sequence, observed_at, benchmark_value, cash_return, evidence_id,
       evidence_sha256
FROM passive_benchmark_observations
WHERE declaration_id = $1
ORDER BY sequence;

SELECT id, sha256, declaration_id, evaluation_id,
       strategy_total_return, benchmark_total_return, cash_total_return,
       benchmark_opportunity_cost, cash_opportunity_cost,
       strategy_terminal_wealth, benchmark_terminal_wealth,
       cash_terminal_wealth, benchmark_wealth_difference,
       cash_wealth_difference
FROM benchmark_opportunity_cost_reports
WHERE id = $2;
```

## Failure response

If registration, evaluation, deferred validation, or reload fails:

1. Stop use of that experiment/evaluation as benchmark-qualified evidence.
2. Preserve the canonical payloads, normalized rows, exact parent IDs/hashes,
   source evidence, transaction error, and database logs.
3. Classify the problem as parent mismatch, incomplete/frequency-drifted curve,
   source substitution, cash mismatch, canonical divergence, changed retry, or
   arithmetic mismatch.
4. Create new upstream evidence and a new content-addressed declaration or
   evaluation. Never update or delete accepted evidence to make it pass.
5. Do not select a more favorable benchmark or discard the cash comparison.

Identical concurrent writers converge. Every declaration child is written in
one transaction, and the single report row is also transactionally staged.
Injected failures must leave no partial graph.

## Rollback and recovery

Migration 82 is empty-only reversible. It locks all OVR-401 tables and refuses
rollback when any declaration or report exists. Never disable append-only
triggers or delete evidence to force a downgrade.

On a separate empty loopback database, rehearse:

```bash
migrate -path migrations -database "$ISOLATED_DB_URL" down 1
migrate -path migrations -database "$ISOLATED_DB_URL" up 1
```

Expected transition: `82 -> 81 -> 82`. On a retained schema, attempt the down
migration only to prove refusal; preserve all rows afterward.

## Qualification boundary

Run focused domain races and real-PostgreSQL repository races, then the full
backend, pinned frontend, migration, image, health/API, backup/restore, and
rollback/reapply gates. A passing test is not real market-data, independent
benchmark review, strategy-runtime adoption, deployment, or user-experience
proof.

Label synthetic retained evidence `VERIFIED_LOCAL`. Real benchmark selection,
licensed data, independent review, shared migration, strategy activation,
allocation, scheduling, deployment, and production cutover remain
`BLOCKED_EXTERNAL` until separately authorized and verified.
