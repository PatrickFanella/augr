LOCK TABLE robustness_gates,robustness_statistics,robustness_assessment_scenarios,robustness_assessment_folds,robustness_assessment_candidates,
  statistical_robustness_assessments,robustness_search_family_candidates,robustness_search_families,robustness_policy_perturbations,robustness_policy_artifacts IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM statistical_robustness_assessments) OR EXISTS(SELECT 1 FROM robustness_search_families) OR EXISTS(SELECT 1 FROM robustness_policy_artifacts)
  THEN RAISE EXCEPTION 'cannot roll back migration 80 while robustness evidence exists'; END IF; END $$;
DROP TABLE robustness_gates; DROP TABLE robustness_statistics; DROP TABLE robustness_assessment_scenarios; DROP TABLE robustness_assessment_folds;
DROP TABLE robustness_assessment_candidates; DROP TABLE statistical_robustness_assessments; DROP TABLE robustness_search_family_candidates;
DROP TABLE robustness_search_families; DROP TABLE robustness_policy_perturbations; DROP TABLE robustness_policy_artifacts;
DROP FUNCTION reject_robustness_mutation(); DROP FUNCTION validate_robustness_assessment_graph(); DROP FUNCTION validate_robustness_family_graph(); DROP FUNCTION validate_robustness_policy_graph();
