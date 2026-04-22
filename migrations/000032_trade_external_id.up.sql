-- 000032_trade_external_id.up.sql
-- Persist broker fill activity identifiers on trades for deterministic reconciliation dedupe.

ALTER TABLE trades
    ADD COLUMN IF NOT EXISTS external_id TEXT;

CREATE INDEX IF NOT EXISTS idx_trades_external_id ON trades (external_id);
