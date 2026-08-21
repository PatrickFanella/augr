# ETF Time-Series Trend V1 Runbook

## Boundary

ETF Trend V1 is immutable OVR-404 research evidence. It derives long-or-cash
ETF targets from point-in-time multi-horizon prices and realized volatility,
then models whole-lot executable rebalances. It cannot call providers, schedule
runs, allocate capital, promote a strategy, deploy code, or place live orders.

Migration 85 is additive and empty-only reversible. Do not apply it to a shared
database without separate operator authorization.

## Inspect policy and source evidence

```sql
SELECT id,sha256,version,decimal_scale,canonical_json FROM trend_v1_policies ORDER BY created_at,id;
SELECT id,sha256,policy_id,mode,evaluation_start,evaluation_end,rebalance_count FROM trend_v1_scenarios ORDER BY created_at,id;
SELECT source.scenario_id,source.sequence,source.occurred_at,source.member_count,
       count(member.*) AS normalized_members
FROM trend_v1_source_rebalances source
LEFT JOIN trend_v1_universe_members member
  ON member.scenario_id=source.scenario_id AND member.rebalance_sequence=source.sequence
GROUP BY source.scenario_id,source.sequence
ORDER BY source.scenario_id,source.sequence;
```

Each member’s `canonical_member` pins membership effective/available times,
current price, realized volatility, bid, ask, lot size, manifest partition,
source key, evidence hash, and availability. Horizon anchors are independently
normalized and must reproduce the canonical array:

```sql
SELECT member.scenario_id,member.rebalance_sequence,member.member_sequence,
       member.instrument_id,member.venue_contract_id,price.horizon_sequence,price.price
FROM trend_v1_universe_members member
JOIN trend_v1_horizon_prices price USING(scenario_id,rebalance_sequence,member_sequence)
WHERE member.scenario_id=:'scenario_id'
ORDER BY member.rebalance_sequence,member.member_sequence,price.horizon_sequence;
```

Missing, reordered, future, stale, revised, or partial evidence is a rejection.
Create a new immutable scenario for changed source evidence; never edit a row.

## Replay signals and volatility sizing

For each policy horizon, compare current price with its anchor. The sign is
`-1`, `0`, or `1`; multiply by the exact horizon weight and sum. A score must be
strictly above the threshold to be long. Raw weight is target volatility divided
by realized volatility. Apply the per-instrument cap, then proportionally apply
the gross cap if needed. Never normalize unused exposure upward; it remains cash.

```sql
SELECT rebalance.sequence,rebalance.occurred_at,rebalance.gross_target_weight,
       signal.sequence AS signal_sequence,signal.canonical_value
FROM trend_v1_rebalances rebalance
JOIN trend_v1_signals signal
  ON signal.report_id=rebalance.report_id
 AND signal.rebalance_sequence=rebalance.sequence
WHERE rebalance.report_id=:'report_id'
ORDER BY rebalance.sequence,signal.sequence;
```

## Reconcile turnover, trades, holdings, and cost

```sql
SELECT sequence,occurred_at,desired_turnover,applied_turnover,turnover_scale,
       remaining_target_drift,cost,cash,equity,gross_target_weight,
       signal_count,trade_count,holding_count
FROM trend_v1_rebalances WHERE report_id=:'report_id' ORDER BY sequence;
```

Verify that:

- desired turnover is half the absolute current/target weight changes;
- applied turnover does not exceed the policy cap;
- sells precede buys, use bid, and never exceed held quantity;
- buys use ask and never make cash negative;
- every quantity is floored to the scenario-pinned venue lot;
- costs use the pinned basis-point rule;
- actual lot-rounded turnover and remaining drift stay explicit;
- holdings plus cash reproduce equity; and
- final equity versus initial capital reproduces after-cost return.

The deferred database graph checks normalized arrays, parent hashes, chronology,
exposure caps, turnover caps, and exact canonical bytes. Repository reload also
reruns the engine and refuses forged economics.

## Failure and recovery

- Interrupted write: scenario and report writes are transactional. Confirm the
  parent is absent and retry identical bytes.
- Changed retry: treat the idempotency conflict as a new immutable identity.
- Missing instrument/contract: register exact canonical reference evidence or
  reject the scenario.
- Gap/fork/partial child graph: reject and replay the full transaction.
- Negative cash/weight, over-cap exposure/turnover, forged signal/trade/holding,
  or continuity mismatch: reject the report; do not repair rows in place.

All ten Trend V1 tables reject updates and deletes.

## Retained local qualification

```bash
export TREND_V1_QUALIFICATION_DB_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/augr_ovr404_qual_20260820?sslmode=disable'
migrate -path migrations -database "$TREND_V1_QUALIFICATION_DB_URL" up
go test -race ./internal/repository/postgres -run '^TestTrendRetainedQualification$' -count=1 -v
```

The gate requires an empty public schema at version 85 and retains exact
all-long, mixed long/cash, all-cash, volatility-cap, turnover-cap, and
multi-rebalance scenarios.

## Rollback

Stop application processes, prove every Trend V1 evidence table is empty, and
only then run on an authorized isolated database:

```bash
migrate -path migrations -database "$ISOLATED_DB_URL" down 1
migrate -path migrations -database "$ISOLATED_DB_URL" up 1
```

The down migration locks the graph and refuses any policy, scenario, or report.
It never deletes research evidence to force rollback.

Passing local engine, runner, migration, repository, retained-data, and
production-image gates is `VERIFIED_LOCAL`. Licensed real ETF inputs,
independent review, shared migration, promotion, allocation, runtime adoption,
deployment, and production activation remain `BLOCKED_EXTERNAL`.
