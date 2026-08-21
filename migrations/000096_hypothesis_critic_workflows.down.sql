LOCK TABLE research_hypotheses,research_critics IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM research_hypotheses) OR EXISTS(SELECT 1 FROM research_critics) THEN RAISE EXCEPTION 'cannot roll back hypothesis critic workflows with retained evidence'; END IF; END $$;
DROP FUNCTION reject_research_workflow_mutation() CASCADE;
DROP FUNCTION validate_research_critic_graph() CASCADE;
DROP FUNCTION validate_research_hypothesis_graph() CASCADE;
DROP TABLE research_critic_check_references,research_critic_checks,research_critic_finding_references,research_critic_findings,research_critics;
DROP TABLE research_hypothesis_tests,research_hypothesis_search_results,research_hypothesis_searches,research_hypothesis_source_manifest_keys,research_hypothesis_sources,research_hypotheses;
