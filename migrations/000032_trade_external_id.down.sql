-- 000032_trade_external_id.down.sql

DROP INDEX IF EXISTS idx_trades_external_id;

ALTER TABLE trades
    DROP COLUMN IF EXISTS external_id;
