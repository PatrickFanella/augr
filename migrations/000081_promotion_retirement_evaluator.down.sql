LOCK TABLE deployment_promotion_lifecycle_events,promotion_decision_observed_gates,promotion_retirement_decisions,promotion_policy_required_gates,promotion_policy_artifacts IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM promotion_retirement_decisions) OR EXISTS(SELECT 1 FROM promotion_policy_artifacts)
  THEN RAISE EXCEPTION 'cannot roll back migration 81 while promotion evidence exists'; END IF; END $$;
DROP TABLE deployment_promotion_lifecycle_events; DROP TABLE promotion_decision_observed_gates; DROP TABLE promotion_retirement_decisions;
DROP TABLE promotion_policy_required_gates; DROP TABLE promotion_policy_artifacts;
DROP FUNCTION reject_promotion_mutation(); DROP FUNCTION validate_deployment_promotion_event(); DROP FUNCTION validate_promotion_decision_graph(); DROP FUNCTION validate_promotion_policy_graph();
