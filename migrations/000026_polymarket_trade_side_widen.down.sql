ALTER TABLE polymarket_account_trades DROP CONSTRAINT IF EXISTS polymarket_account_trades_side_check;

ALTER TABLE polymarket_account_trades
    ADD CONSTRAINT polymarket_account_trades_side_check
    CHECK (side IN ('YES', 'NO'));
