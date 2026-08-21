# Momentum / Quality Baseline V1 Runbook

## Boundary

Momentum V1 is immutable research evidence for OVR-403. It derives a
long-only, equal-weight portfolio from point-in-time universe, price-anchor,
fundamental, volatility, benchmark, and executable quote evidence. It has no
provider writer, scheduler, allocation, promotion, deployment, or live-order
authority. A persisted report is not approval to run or fund the strategy.

Migration 84 is additive and empty-only reversible. Use only an isolated local
database unless an operator separately authorizes a shared migration.

## Inspect immutable inputs

Record the policy, scenario, and report identities before investigating a run:

```sql
SELECT id,sha256,version,decimal_scale,created_at FROM momentum_v1_policies ORDER BY created_at,id;
SELECT id,sha256,policy_id,policy_sha256,mode,evaluation_start,evaluation_end,rebalance_count FROM momentum_v1_scenarios ORDER BY created_at,id;
SELECT id,sha256,scenario_id,scenario_sha256,ending_cash,ending_equity,cumulative_turnover,total_cost,after_cost_total_return FROM momentum_v1_reports ORDER BY created_at,id;
```

Every scenario rebalance must have an exact, gap-free source row and its full
point-in-time membership set:

```sql
SELECT source.scenario_id,source.sequence,source.occurred_at,
       source.benchmark_evidence_id,source.benchmark_evidence_sha256,
       source.member_count,count(member.*) AS normalized_members
FROM momentum_v1_source_rebalances source
LEFT JOIN momentum_v1_universe_members member
  ON member.scenario_id=source.scenario_id
 AND member.rebalance_sequence=source.sequence
GROUP BY source.scenario_id,source.sequence
ORDER BY source.scenario_id,source.sequence;
```

Inspect a member’s effective/available times and exact evidence without editing
the normalized row:

```sql
SELECT scenario_id,rebalance_sequence,member_sequence,instrument_id,
       venue_contract_id,evidence_sha256,canonical_member
FROM momentum_v1_universe_members
WHERE scenario_id = :'scenario_id'
ORDER BY rebalance_sequence,member_sequence;
```

## Reconcile ranks, turnover, costs, and holdings

The canonical rebalance contains engine-derived ranks, targets, trades, and
holdings. The normalized child rows must reproduce those arrays exactly; the
deferred migration constraints and repository reload enforce this.

```sql
SELECT sequence,occurred_at,regime,desired_turnover,applied_turnover,
       turnover_scale,remaining_target_drift,cost,cash,equity,
       rank_count,trade_count,holding_count
FROM momentum_v1_rebalances
WHERE report_id = :'report_id'
ORDER BY sequence;
```

Checks:

- `applied_turnover <= desired_turnover` and never exceeds the policy cap;
- whole-lot quantities use the scenario-pinned lot size;
- sells precede buys in the canonical trade array;
- sells use bid, buys use ask, and cost equals the pinned basis-point rule;
- cash and holdings reproduce ending equity;
- remaining target drift stays explicit after turnover and lot-size limits;
- cumulative turnover and total cost equal the rebalance sums;
- the final equity and initial capital reproduce after-cost total return.

Do not repair discrepancies in place. All ten tables reject updates and
deletes. Reconstruct from the pinned inputs and compare IDs/hashes.

## Review regimes

```sql
SELECT rebalance.sequence,rebalance.occurred_at,rebalance.regime,
       source.canonical_rebalance->>'benchmark_trend' AS benchmark_trend,
       source.canonical_rebalance->>'benchmark_volatility' AS benchmark_volatility
FROM momentum_v1_rebalances rebalance
JOIN momentum_v1_reports report ON report.id=rebalance.report_id
JOIN momentum_v1_source_rebalances source
  ON source.scenario_id=report.scenario_id AND source.sequence=rebalance.sequence
WHERE rebalance.report_id = :'report_id'
ORDER BY rebalance.sequence;
```

`bull`, `bear`, and `sideways` are derived solely from the policy thresholds
and pinned benchmark evidence. Regime rows summarize observations; they cannot
select positions, alter targets, or promote a result.

## Failure and recovery

- Missing/gapped source or child rows: reject the graph; rerun the entire
  append-only transaction from the original canonical object.
- Changed retry: the same identity with different bytes is an idempotency
  conflict; retain both attempted hashes for investigation.
- Stale/revised evidence: build a new scenario identity. Never overwrite the
  old evidence or move its available timestamp.
- Unknown instrument/contract: register the immutable canonical instrument and
  venue contract first, or reject the scenario.
- Negative cash/weight, over-cap turnover, forged rank/trade/economics, or
  continuity mismatch: reject the report and diagnose the engine inputs.
- Interrupted write: every scenario and report write is transactional. Confirm
  no parent row exists, then safely retry identical bytes.

## Local qualification

Create a dedicated database, migrate it through 84, and run the retained gate:

```bash
export MOMENTUM_V1_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr403_qual_20260820?sslmode=disable'
migrate -path migrations -database "$MOMENTUM_V1_QUALIFICATION_DB_URL" up
go test -race ./internal/repository/postgres -run '^TestMomentumRetainedQualification$' -count=1 -v
```

The gate refuses a non-public schema, a version other than 84, or any existing
Momentum V1 policy/scenario/report. It retains exact bull, bear, sideways,
transition, cap-hit, and multi-rebalance evidence for later inspection.

## Rollback

Stop application processes before an authorized isolated rollback. Prove all
Momentum V1 evidence tables are empty, then:

```bash
migrate -path migrations -database "$ISOLATED_DB_URL" down 1
migrate -path migrations -database "$ISOLATED_DB_URL" up 1
```

The down migration takes exclusive locks and refuses any policy, scenario, or
report evidence. It never deletes research records to make rollback succeed.

## Qualification labels

Passing local migration, repository, runner, and retained-scenario gates is
`VERIFIED_LOCAL`. Licensed real inputs, independent model review, shared
migration, strategy promotion, capital allocation, runtime adoption, and
production activation remain `BLOCKED_EXTERNAL` until separately authorized.
