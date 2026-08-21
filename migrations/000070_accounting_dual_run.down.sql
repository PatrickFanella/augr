-- Quiesce structural evidence writers before inspecting rollback safety.
LOCK TABLE accounting_reconciliation_results, accounting_reconciliation_runs
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM accounting_reconciliation_results) OR
       EXISTS (SELECT 1 FROM accounting_reconciliation_runs) THEN
        RAISE EXCEPTION 'cannot roll back migration 70 while accounting reconciliation evidence exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_accounting_reconciliation_results_complete ON accounting_reconciliation_results;
DROP TRIGGER IF EXISTS trg_accounting_reconciliation_run_complete ON accounting_reconciliation_runs;
DROP FUNCTION IF EXISTS validate_accounting_reconciliation_result_set_complete();
DROP FUNCTION IF EXISTS validate_accounting_reconciliation_parent_complete();
-- Compatibility cleanup for a locally rehearsed pre-release draft of
-- migration 70. Qualify it explicitly so an isolated test schema never drops
-- a same-named function from a later search_path schema.
DO $$
BEGIN
    EXECUTE format(
        'DROP FUNCTION IF EXISTS %I.validate_accounting_reconciliation_run_complete()',
        current_schema()
    );
END;
$$;
DROP FUNCTION IF EXISTS assert_accounting_reconciliation_run_complete(UUID);
DROP TRIGGER IF EXISTS trg_accounting_reconciliation_results_validate ON accounting_reconciliation_results;
DROP FUNCTION IF EXISTS validate_accounting_reconciliation_result();
DROP TRIGGER IF EXISTS trg_accounting_reconciliation_runs_validate ON accounting_reconciliation_runs;
DROP FUNCTION IF EXISTS validate_accounting_reconciliation_run();
DROP TRIGGER IF EXISTS trg_accounting_reconciliation_results_immutable ON accounting_reconciliation_results;
DROP TRIGGER IF EXISTS trg_accounting_reconciliation_runs_immutable ON accounting_reconciliation_runs;
DROP FUNCTION IF EXISTS reject_accounting_reconciliation_mutation();
DROP TABLE accounting_reconciliation_results;
DROP TABLE accounting_reconciliation_runs;
