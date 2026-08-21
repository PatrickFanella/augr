-- Quiesce both sides of the policy/order reference before deciding whether an
-- empty rollback is safe. This preserves every schema-71 lifecycle object.
LOCK TABLE simulation_policy_artifacts, execution_orders IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM simulation_policy_artifacts) OR
       EXISTS (
           SELECT 1 FROM execution_orders
           WHERE policy_kind = 'simulation'
       ) THEN
        RAISE EXCEPTION 'cannot roll back migration 72 while simulation policy artifacts or orders exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_execution_orders_simulation_policy ON execution_orders;
DROP FUNCTION IF EXISTS validate_simulation_order_policy_artifact();
DROP TRIGGER IF EXISTS trg_simulation_policy_artifacts_immutable ON simulation_policy_artifacts;
DROP FUNCTION IF EXISTS reject_simulation_policy_artifact_mutation();
DROP TABLE simulation_policy_artifacts;
DROP FUNCTION IF EXISTS simulation_policy_v1_canonical_bytes(JSONB);
