# Alpaca P/L residual evidence

## Snapshot

- Read-only production DB snapshot time UTC: 2026-07-20T17:24:01Z
- Residual under investigation: `-151.514251`

## DB evidence

```text
broker_cash=100135.42
broker_equity=100135.42
local_closed_pnl=151.514251
local_open_pnl=0
trade_count=23
fee_total=0
known_adjustments=0
unexplained_residual=-151.5142510000005
adjustment_details=["no persisted adjustment source discovered"]
```

## Authenticated endpoint verification

- Generated a short-lived JWT from the deployed `JWT_SECRET` without printing the secret.
- Authenticated GET `/api/v1/automation/alpaca/reconciliation` returned HTTP 200.
- The endpoint confirmed the explicit residual above without forcing it to zero.

## Notes

- No manual correction was applied.
- No ledger mutation was performed.
- The reported `-151.514251` is explained as a formula artifact: current broker cash already contains realized P/L, so adding `local_closed_pnl` to broker cash double-counts it. The residual exactly equals `-local_closed_pnl`.
- Current asset-state reconciliation is `broker_equity - (broker_cash + local_open_market_value + current_asset_adjustments)`. With no open positions or known adjustments, current state residual is `0`.
- Historical P/L reconciliation remains blocked: it requires baseline equity plus broker deposits, withdrawals, journals, dividends, interest, fees, and corporate actions. Assuming—but not proving—a `$100,000` baseline and zero external flows yields a separate P/L difference of `-16.094251`.
- The API should add `state_residual`, `local_open_market_value`, nullable `pnl_residual`, and a blocked reason; the existing misleading field should remain only for compatibility until consumers migrate.
