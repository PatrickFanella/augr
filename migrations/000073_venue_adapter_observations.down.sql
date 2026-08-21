-- Quiesce orders and both migration-73 fact tables before deciding whether an
-- empty rollback is safe. Every schema-72 object and fact remains untouched.
LOCK TABLE execution_orders, venue_adapter_policy_artifacts, venue_observations IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM venue_adapter_policy_artifacts) OR
       EXISTS (SELECT 1 FROM venue_observations) OR
       EXISTS (
           SELECT 1 FROM execution_orders
           WHERE policy_kind = 'venue'
       ) THEN
        RAISE EXCEPTION 'cannot roll back migration 73 while venue adapter artifacts, observations, or orders exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_execution_fills_venue_observation ON execution_fills;
DROP FUNCTION IF EXISTS validate_venue_execution_fill_observation();
DROP TRIGGER IF EXISTS trg_execution_lifecycle_venue_observation ON execution_lifecycle_events;
DROP FUNCTION IF EXISTS validate_venue_lifecycle_observation();
DROP FUNCTION IF EXISTS validate_venue_cancel_command(execution_lifecycle_events);
DROP TRIGGER IF EXISTS trg_venue_observations_semantics ON venue_observations;
DROP FUNCTION IF EXISTS validate_venue_observation_semantics();
DROP TRIGGER IF EXISTS trg_execution_orders_venue_policy ON execution_orders;
DROP FUNCTION IF EXISTS validate_venue_order_policy_artifact();
DROP TRIGGER IF EXISTS trg_venue_observations_immutable ON venue_observations;
DROP TABLE venue_observations;
DROP TRIGGER IF EXISTS trg_venue_adapter_policy_artifacts_immutable ON venue_adapter_policy_artifacts;
DROP FUNCTION IF EXISTS reject_venue_adapter_fact_mutation();
DROP TABLE venue_adapter_policy_artifacts;
DROP FUNCTION IF EXISTS venue_adapter_policy_v1_canonical_bytes(JSONB);
