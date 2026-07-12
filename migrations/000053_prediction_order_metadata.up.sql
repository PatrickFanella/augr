-- Preserve the outcome side and provider intent needed to replay prediction orders.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS prediction_side TEXT,
    ADD COLUMN IF NOT EXISTS polymarket_intent TEXT;

ALTER TABLE orders
    ADD CONSTRAINT orders_prediction_side_check
    CHECK (prediction_side IS NULL OR prediction_side IN ('YES', 'NO'));
