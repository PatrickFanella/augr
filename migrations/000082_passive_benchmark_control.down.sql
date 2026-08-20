LOCK TABLE benchmark_opportunity_cost_reports,passive_benchmark_observations,passive_benchmark_declarations IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM benchmark_opportunity_cost_reports) OR EXISTS(SELECT 1 FROM passive_benchmark_declarations)
  THEN RAISE EXCEPTION 'cannot roll back migration 82 while passive benchmark evidence exists'; END IF; END $$;
DROP TABLE benchmark_opportunity_cost_reports; DROP TABLE passive_benchmark_observations; DROP TABLE passive_benchmark_declarations;
DROP FUNCTION reject_passive_benchmark_mutation(); DROP FUNCTION validate_benchmark_opportunity_cost_report(); DROP FUNCTION validate_passive_benchmark_declaration();
