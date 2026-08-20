-- Quiesce account creation and both schema-74 fact tables before deciding
-- whether rollback is safe. Every schema-73 object remains untouched.
LOCK TABLE accounts, account_capital_policy_bindings, capital_margin_policy_artifacts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM account_capital_policy_bindings) OR
       EXISTS (SELECT 1 FROM capital_margin_policy_artifacts) THEN
        RAISE EXCEPTION 'cannot roll back migration 74 while capital policy artifacts or bindings exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_account_capital_policy_bindings_validate ON account_capital_policy_bindings;
DROP FUNCTION IF EXISTS validate_account_capital_policy_binding();
DROP TRIGGER IF EXISTS trg_account_capital_policy_bindings_immutable ON account_capital_policy_bindings;
DROP TABLE account_capital_policy_bindings;
DROP TRIGGER IF EXISTS trg_capital_margin_policy_artifacts_immutable ON capital_margin_policy_artifacts;
DROP FUNCTION IF EXISTS reject_capital_policy_fact_mutation();
DROP TABLE capital_margin_policy_artifacts;
DROP FUNCTION IF EXISTS capital_margin_policy_v1_canonical_bytes(JSONB);
