CREATE TABLE IF NOT EXISTS financial_fill_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    position_id UUID NULL REFERENCES positions(id) ON DELETE SET NULL,
    trade_id UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
    fill_quantity NUMERIC(20,8) NOT NULL,
    fill_price NUMERIC(20,8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS prediction_settlement_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    decision_id UUID NOT NULL UNIQUE REFERENCES trade_decisions(id) ON DELETE CASCADE,
    position_id UUID NULL REFERENCES positions(id) ON DELETE SET NULL,
    trade_id UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
    replay_event_id UUID NULL REFERENCES replay_events(id) ON DELETE SET NULL,
    payout NUMERIC(20,8) NOT NULL,
    resolved_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM trades
        WHERE external_id IS NOT NULL
        GROUP BY external_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'migration 000055 aborted: duplicate non-null external_id values exist in trades';
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_trades_external_id_unique
    ON trades (external_id)
    WHERE external_id IS NOT NULL;
