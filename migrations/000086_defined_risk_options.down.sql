LOCK TABLE defined_risk_v1_fills,defined_risk_v1_reports,defined_risk_v1_observations,defined_risk_v1_legs,defined_risk_v1_scenarios,defined_risk_v1_policies IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM defined_risk_v1_reports) OR EXISTS(SELECT 1 FROM defined_risk_v1_scenarios) OR EXISTS(SELECT 1 FROM defined_risk_v1_policies) THEN RAISE EXCEPTION 'cannot roll back migration 86 while defined-risk v1 evidence exists'; END IF; END $$;
DROP TABLE defined_risk_v1_fills,defined_risk_v1_reports,defined_risk_v1_observations,defined_risk_v1_legs,defined_risk_v1_scenarios,defined_risk_v1_policies;
DROP FUNCTION reject_defined_risk_v1_mutation(); DROP FUNCTION validate_defined_risk_v1_report(); DROP FUNCTION validate_defined_risk_v1_scenario();
