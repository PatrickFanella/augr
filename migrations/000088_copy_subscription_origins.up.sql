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

CREATE TABLE copy_origin_rebalance_runs (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'copy-origin-rebalance-v1'),
    state TEXT NOT NULL CHECK (state = 'prepared'),
    subscription_id UUID NOT NULL REFERENCES copy_subscriptions(id),
    origin_type TEXT NOT NULL CHECK (origin_type = 'copy_subscription'),
    origin_id UUID NOT NULL,
    source_observation_id UUID NOT NULL REFERENCES copy_source_observations(id),
    calculation_version INT NOT NULL CHECK (calculation_version > 0),
    intent_count INT NOT NULL CHECK (intent_count > 0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (origin_id = subscription_id),
    CHECK (octet_length(canonical_bytes) > 0),
    CHECK (convert_from(canonical_bytes, 'UTF8')::jsonb = canonical_json),
    UNIQUE (subscription_id, source_observation_id, calculation_version)
);

CREATE TABLE copy_origin_rebalance_intents (
    run_id UUID NOT NULL REFERENCES copy_origin_rebalance_runs(id),
    sequence INT NOT NULL CHECK (sequence >= 0),
    intent_id UUID NOT NULL REFERENCES copy_trade_intents(id),
    instrument_key TEXT NOT NULL CHECK (instrument_key = btrim(instrument_key) AND instrument_key <> ''),
    source_observation_id UUID NOT NULL REFERENCES copy_source_observations(id),
    canonical_intent JSONB NOT NULL,
    PRIMARY KEY (run_id, sequence),
    UNIQUE (run_id, intent_id),
    UNIQUE (run_id, instrument_key)
);

CREATE OR REPLACE FUNCTION copy_origin_run_intent_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM copy_origin_rebalance_runs run
          JOIN copy_trade_intents intent ON intent.id = NEW.intent_id
         WHERE run.id = NEW.run_id
           AND intent.subscription_id = run.subscription_id
           AND intent.origin_type = run.origin_type
           AND intent.origin_id = run.origin_id
           AND intent.source_observation_id = run.source_observation_id
           AND intent.calculation_version = run.calculation_version
           AND intent.instrument_key = NEW.instrument_key
           AND intent.source_observation_id = NEW.source_observation_id
    ) THEN
        RAISE EXCEPTION 'copy origin run intent does not match run';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_origin_run_intent_guard
BEFORE INSERT ON copy_origin_rebalance_intents
FOR EACH ROW EXECUTE FUNCTION copy_origin_run_intent_guard();

CREATE OR REPLACE FUNCTION reject_copy_origin_evidence_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'copy origin evidence is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_origin_runs_append_only
BEFORE UPDATE OR DELETE ON copy_origin_rebalance_runs
FOR EACH ROW EXECUTE FUNCTION reject_copy_origin_evidence_mutation();

CREATE TRIGGER copy_origin_run_intents_append_only
BEFORE UPDATE OR DELETE ON copy_origin_rebalance_intents
FOR EACH ROW EXECUTE FUNCTION reject_copy_origin_evidence_mutation();

COMMENT ON COLUMN copy_subscriptions.legacy_strategy_id IS
    'Read-only attribution for subscriptions created before origin-native OVR-501; new rows leave this null.';
COMMENT ON COLUMN copy_subscriptions.origin_id IS
    'Exact copy_subscription execution origin; always equal to subscription id.';
