LOCK TABLE wheel_v1_selected_contracts,wheel_v1_economic_effects,wheel_v1_transitions,wheel_v1_reports,wheel_v1_source_observations,wheel_v1_scenarios,wheel_v1_policies IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM wheel_v1_reports) OR EXISTS(SELECT 1 FROM wheel_v1_scenarios) OR EXISTS(SELECT 1 FROM wheel_v1_policies)
  THEN RAISE EXCEPTION 'cannot roll back migration 83 while wheel v1 evidence exists'; END IF; END $$;
DROP TABLE wheel_v1_selected_contracts; DROP TABLE wheel_v1_economic_effects; DROP TABLE wheel_v1_transitions; DROP TABLE wheel_v1_reports;
DROP TABLE wheel_v1_source_observations; DROP TABLE wheel_v1_scenarios; DROP TABLE wheel_v1_policies;
DROP FUNCTION reject_wheel_v1_mutation(); DROP FUNCTION validate_wheel_v1_report(); DROP FUNCTION validate_wheel_v1_scenario();
