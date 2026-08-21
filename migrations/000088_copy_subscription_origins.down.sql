LOCK TABLE copy_subscriptions, copy_trade_intents IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM copy_subscriptions WHERE legacy_strategy_id IS NULL)
       OR EXISTS (SELECT 1 FROM copy_origin_rebalance_runs) THEN
        RAISE EXCEPTION 'cannot roll back copy subscription origins with origin-native subscriptions';
    END IF;
END;
$$;

DROP TRIGGER copy_origin_run_intents_append_only ON copy_origin_rebalance_intents;
DROP TRIGGER copy_origin_runs_append_only ON copy_origin_rebalance_runs;
DROP TRIGGER IF EXISTS copy_origin_run_intent_guard ON copy_origin_rebalance_intents;
DROP FUNCTION IF EXISTS copy_origin_run_intent_guard();
DROP FUNCTION reject_copy_origin_evidence_mutation();
DROP TABLE copy_origin_rebalance_intents;
DROP TABLE copy_origin_rebalance_runs;

DROP INDEX idx_copy_intents_origin_created;
DROP TRIGGER copy_trade_intents_origin_guard ON copy_trade_intents;
DROP FUNCTION copy_intent_origin_guard();

ALTER TABLE copy_trade_intents
    DROP COLUMN origin_id,
    DROP COLUMN origin_type;

ALTER TABLE copy_subscriptions
    DROP CONSTRAINT copy_subscriptions_origin_unique,
    DROP CONSTRAINT copy_subscriptions_origin_exact,
    DROP COLUMN origin_id,
    DROP COLUMN origin_type;

ALTER TABLE copy_subscriptions ALTER COLUMN legacy_strategy_id SET NOT NULL;
ALTER TABLE copy_subscriptions RENAME COLUMN legacy_strategy_id TO strategy_id;
