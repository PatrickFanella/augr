LOCK TABLE copy_subscriptions, copy_trade_intents, quote_snapshots IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM copy_trade_intents WHERE quote_gate_version=1) THEN
        RAISE EXCEPTION 'cannot roll back copy quote gates with decision evidence';
    END IF;
END;
$$;

DROP TRIGGER copy_intent_quote_gate_guard ON copy_trade_intents;
DROP FUNCTION copy_intent_quote_gate_guard();

ALTER TABLE copy_trade_intents
    DROP CONSTRAINT copy_intent_quote_gate_shape,
    DROP COLUMN decision_session_status,
    DROP COLUMN decision_market_status,
    DROP COLUMN decision_at,
    DROP COLUMN decision_available_at,
    DROP COLUMN decision_spread_bps,
    DROP COLUMN decision_ask,
    DROP COLUMN decision_bid,
    DROP COLUMN decision_quote_snapshot_id,
    DROP COLUMN quote_gate_version;

ALTER TABLE copy_subscriptions
    DROP COLUMN allowed_sessions,
    DROP COLUMN max_quote_age_seconds;
