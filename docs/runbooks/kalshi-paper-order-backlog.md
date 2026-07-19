# Kalshi Paper Order Backlog Cleanup

Guarded one-time runbook for legacy Kalshi paper limit orders stuck in `submitted` because no historical reference price was persisted.

## Preconditions

- Confirm the affected rows match the legacy pattern only.
- Do **not** run this if any submitted paper limit order has fill facts or any related trade rows.
- Do **not** backfill fills, trades, positions, or P/L for these rows.
- This runbook accepts the known P1 crash window where order, trade, and position persistence are not atomic; do **not** expand transaction scope here.

### Preflight query

```sql
SELECT id,
       created_at,
       status,
       filled_quantity,
       filled_avg_price,
       filled_at,
       EXISTS (
         SELECT 1
         FROM trades
         WHERE trades.order_id = orders.id
       ) AS has_trade
FROM orders
WHERE broker = 'paper'
  AND market_type = 'kalshi'
  AND order_type = 'limit'
  AND status = 'submitted'
  AND (
    filled_quantity <> 0
    OR filled_avg_price IS NOT NULL
    OR filled_at IS NOT NULL
    OR EXISTS (
      SELECT 1
      FROM trades
      WHERE trades.order_id = orders.id
    )
  );
```

If this query returns any rows, abort the run and resolve the inconsistency first.

### Safe candidate preflight

```sql
SELECT count(*) AS affected,
       min(created_at) AS oldest,
       max(created_at) AS newest,
       sum(filled_quantity) AS filled_quantity
FROM orders
WHERE broker = 'paper'
  AND market_type = 'kalshi'
  AND order_type = 'limit'
  AND status = 'submitted'
  AND filled_quantity = 0
  AND filled_avg_price IS NULL
  AND filled_at IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM trades
    WHERE trades.order_id = orders.id
  );
```

Record the result and compare it to the cancellation/audit row counts before committing.
If this query returns zero, abort the run.

## Transaction-wrapped cancellation

```sql
BEGIN;
WITH cancelled AS (
  UPDATE orders
  SET status = 'cancelled'
  WHERE broker = 'paper'
    AND market_type = 'kalshi'
    AND order_type = 'limit'
    AND status = 'submitted'
    AND filled_quantity = 0
    AND filled_avg_price IS NULL
    AND filled_at IS NULL
    AND NOT EXISTS (
      SELECT 1
      FROM trades
      WHERE trades.order_id = orders.id
    )
  RETURNING id
)
INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details)
SELECT 'legacy_paper_order_cancelled', 'order', id, 'operator:p0_backfill',
       jsonb_build_object('reason', 'missing_historical_reference_price')
FROM cancelled
RETURNING entity_id;
-- Compare returned/audited IDs and row count with preflight before COMMIT.
COMMIT;
```

## Rollback safety

- Rollback means stop before `COMMIT`.
- After commit, do not restore `submitted`.
- Preserve cancellation as the truthful terminal state.
- Restore from database backup only for proven operator error.
