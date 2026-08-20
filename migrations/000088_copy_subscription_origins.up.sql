-- Replace per-subscription backing strategies with exact execution origins.
-- Historical strategy references remain readable but new origin-native rows do
-- not require or create a strategy-registry entry.

LOCK TABLE copy_subscriptions, copy_trade_intents IN ACCESS EXCLUSIVE MODE;

ALTER TABLE copy_subscriptions RENAME COLUMN strategy_id TO legacy_strategy_id;
ALTER TABLE copy_subscriptions ALTER COLUMN legacy_strategy_id DROP NOT NULL;

ALTER TABLE copy_subscriptions
    ADD COLUMN origin_type TEXT NOT NULL DEFAULT 'copy_subscription',
    ADD COLUMN origin_id UUID;

UPDATE copy_subscriptions SET origin_id = id WHERE origin_id IS NULL;

ALTER TABLE copy_subscriptions
    ALTER COLUMN origin_id SET NOT NULL,
    ADD CONSTRAINT copy_subscriptions_origin_exact
        CHECK (origin_type = 'copy_subscription' AND origin_id = id),
    ADD CONSTRAINT copy_subscriptions_origin_unique UNIQUE (origin_type, origin_id);

ALTER TABLE copy_trade_intents
    ADD COLUMN origin_type TEXT NOT NULL DEFAULT 'copy_subscription',
    ADD COLUMN origin_id UUID;

UPDATE copy_trade_intents SET origin_id = subscription_id WHERE origin_id IS NULL;

ALTER TABLE copy_trade_intents
    ALTER COLUMN origin_id SET NOT NULL,
    ADD CONSTRAINT copy_trade_intents_origin_type
        CHECK (origin_type = 'copy_subscription');

CREATE OR REPLACE FUNCTION copy_intent_origin_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM copy_subscriptions subscription
         WHERE subscription.id = NEW.subscription_id
           AND subscription.origin_type = NEW.origin_type
           AND subscription.origin_id = NEW.origin_id
    ) THEN
        RAISE EXCEPTION 'copy intent origin does not match subscription';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_trade_intents_origin_guard
BEFORE INSERT OR UPDATE ON copy_trade_intents
FOR EACH ROW EXECUTE FUNCTION copy_intent_origin_guard();

CREATE INDEX idx_copy_intents_origin_created
    ON copy_trade_intents (origin_type, origin_id, created_at DESC);

COMMENT ON COLUMN copy_subscriptions.legacy_strategy_id IS
    'Read-only attribution for subscriptions created before origin-native OVR-501; new rows leave this null.';
COMMENT ON COLUMN copy_subscriptions.origin_id IS
    'Exact copy_subscription execution origin; always equal to subscription id.';
