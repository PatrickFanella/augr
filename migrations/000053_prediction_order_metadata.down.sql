ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_prediction_side_check,
    DROP COLUMN IF EXISTS polymarket_intent,
    DROP COLUMN IF EXISTS prediction_side;
