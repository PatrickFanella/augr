DO $$ BEGIN IF EXISTS(SELECT 1 FROM full_cost_attribution_lines) OR EXISTS(SELECT 1 FROM full_cost_attribution_reports) THEN RAISE EXCEPTION 'migration 100 rollback refused: full cost attribution evidence exists'; END IF; END $$;
DROP TABLE full_cost_attribution_lines;
DROP TABLE full_cost_attribution_reports;
DROP FUNCTION validate_full_cost_attribution_graph();
DROP FUNCTION validate_full_cost_attribution_actual();
DROP FUNCTION reject_full_cost_attribution_mutation();
DROP FUNCTION full_cost_ledger_evidence_sha256(UUID);
