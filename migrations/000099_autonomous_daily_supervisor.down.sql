DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM daily_supervisor_attention)
    OR EXISTS (SELECT 1 FROM daily_supervisor_action_blockers)
    OR EXISTS (SELECT 1 FROM daily_supervisor_actions)
    OR EXISTS (SELECT 1 FROM daily_supervisor_checks)
    OR EXISTS (SELECT 1 FROM daily_supervisor_assessments)
    OR EXISTS (SELECT 1 FROM daily_supervisor_policy_artifacts)
  THEN RAISE EXCEPTION 'migration 99 rollback refused: daily supervisor evidence exists'; END IF;
END $$;

DROP TABLE daily_supervisor_attention;
DROP TABLE daily_supervisor_action_blockers;
DROP TABLE daily_supervisor_actions;
DROP TABLE daily_supervisor_checks;
DROP TABLE daily_supervisor_assessments;
DROP TABLE daily_supervisor_policy_artifacts;
DROP FUNCTION validate_daily_supervisor_graph();
DROP FUNCTION daily_supervisor_requires(TEXT, TEXT);
DROP FUNCTION reject_daily_supervisor_mutation();
