# Trade and Portfolio Evaluation Runbook

This runbook operates the local OVR-304 evaluation boundary introduced by
migration 79. It produces immutable evidence under one exact completed OVR-303
result. It does not select a candidate, approve a strategy, promote an
experiment, schedule execution, deploy code, or change a live/paper account.

## Preconditions and evidence preparation

Use a dedicated loopback PostgreSQL database at clean schema 79. Do not point
qualification commands at production or a shared developer database. The
parent result must reload successfully through `ExperimentRunRepo.GetResult`;
the evaluation service deliberately reloads that result by ID instead of
trusting an in-memory caller object.

Prepare and retain:

- the exact OVR-303 result, plan, experiment, account, manifest, quality result,
  mode, simulation policy, and capital policy graph;
- a reviewed evaluation policy with explicit frequency, periods per year,
  simple-return convention, per-period cash return, FIFO lot method, recovery
  definition, and decimal scale;
- observations at every declared frequency boundary from the exact evaluation
  start through end, inclusive;
- equity already reflecting all declared costs, plus benchmark value, cash
  return, exposure, concentration, cumulative ownership cost, turnover, and
  modeled/observed slippage evidence at each observation;
- each FIFO closed trade with its retained entry and exit execution-fill IDs,
  prices, quantity, timestamps, fees, other costs, gross P&L, and reconciled
  after-cost P&L; and
- the count of still-open FIFO lots, which is reported but never counted as a
  win, loss, or breakeven trade.

Missing evidence is not estimated. Use an explicit unavailable state where the
contract permits it, such as absent observed slippage. A missing parent, fill,
observation, cost, or incomplete graph is an error.

## Policy and metric interpretation

The policy is content-addressed. A frequency change, annualization-factor
change, cash convention change, lot-method change, recovery-definition change,
or rounding-scale change creates a different policy and therefore a different
report.

Metric states have exact meanings:

- `available`: `value` is a canonical decimal or integer string and `reason`
  is empty;
- `unavailable`: `value` is empty and `reason` explains the missing sample,
  zero denominator, missing observation, unrecovered drawdown, or numeric-range
  condition; and
- `positive_infinity`: used only where a mathematically supported ratio has a
  positive numerator and no negative denominator contribution, such as a
  profitable closed-trade set with no losing trades. It is never encoded as a
  floating JSON sentinel.

`trade.win_rate` means the fraction of FIFO closed trades with positive
after-cost P&L. Its description is
`closed_trade_after_cost_win_rate_not_bar_return_rate`.

`curve_diagnostics.bar_positive_return_rate` means the fraction of sampled
portfolio periods with a positive simple return. Its description is
`descriptor_only_not_trade_win_rate`. It is diagnostic only. Never relabel the
legacy `backtest.Metrics.WinRate` bar-return field as OVR-304 trade evidence.

## Record and inspect a report

Construct an `evaluation.Request` with the exact result ID, policy, declared
window, ordered observations, closed trades, open-lot count, and execution
summary. Call `evaluation.Service.Evaluate`. The service performs this flow:

1. reload the completed result by exact ID;
2. canonicalize and calculate the report;
3. register the immutable policy;
4. insert the report and all normalized children atomically; and
5. reload normalized observations, trades, source fills, and metrics, then
   recalculate the report and require the same ID, SHA-256, and canonical bytes.

Inspect by exact report ID with `EvaluationRepo.GetEvaluation`, or list only by
an explicit result ID or experiment ID. There is intentionally no latest,
best, current, promoted, or active query.

Useful read-only SQL:

```sql
SELECT id, sha256, result_id, experiment_id, account_id, mode,
       policy_id, evaluation_start, evaluation_end,
       observation_count, closed_trade_count, metric_count
FROM trade_portfolio_evaluations
WHERE id = '<evaluation UUID>';

SELECT sequence, section, name, state, value, unit, reason, description
FROM evaluation_metrics
WHERE evaluation_id = '<evaluation UUID>'
ORDER BY sequence;

SELECT t.sequence, t.instrument_id, t.side, t.quantity,
       t.gross_pnl, t.after_cost_pnl, f.kind, f.sequence, f.fill_id
FROM evaluation_closed_trades t
JOIN evaluation_trade_fill_ids f
  ON f.evaluation_id=t.evaluation_id AND f.trade_sequence=t.sequence
WHERE t.evaluation_id = '<evaluation UUID>'
ORDER BY t.sequence, f.kind, f.sequence;
```

## Replay comparison and preservation

An identical retry converges on the same policy/report rows. Compare report ID,
SHA-256, canonical bytes, normalized child counts, every metric state/value,
and every ordered fill ID. A mismatch is not a newer result; it is distinct
evidence or corruption and must remain separate.

Preserve the qualification database, report ID/SHA-256, policy ID/SHA-256,
parent result ID/SHA-256, mode, schema version, and gate output. A successful
local replay is `VERIFIED_LOCAL`; it is not evidence of licensed candidate
data, independent statistical review, promotion approval, deployment, or
production cutover.

## Failure and no-promotion response

On validation, deferred-commit, or reload failure:

1. stop the evaluation attempt and retain the error;
2. verify the exact result/policy/report identities and ordered child counts;
3. inspect missing or mismatched observations, fills, costs, availability
   states, and scored/stress mode pins;
4. do not repair accepted rows in place—every migration-79 table is
   append-only; and
5. create corrected upstream evidence and a new content identity only when the
   source facts justify it.

Never interpret a report as promotion authority. OVR-306 owns explicit
promotion policy after OVR-305 statistical gates. Until those gates and the
external evidence exist, report `BLOCKED_EXTERNAL` for candidate selection,
approval, deployment, and production use.

## Migration rollback

Migration 79 is empty-only reversible. The down migration takes access-exclusive
locks and refuses while either a policy or report exists. Preserve nonempty
evidence; do not delete it to force rollback. To qualify reversibility, use a
separate empty loopback database and prove `79 -> 78 -> 79`. For a retained
nonempty database, the expected result is a refusal containing
`cannot roll back migration 79 while evaluation evidence exists`, with all
rows preserved.
