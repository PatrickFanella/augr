DO $$BEGIN
 IF EXISTS(SELECT 1 FROM milestone_evidence_assessments) THEN
  RAISE EXCEPTION 'migration 103 rollback refused: milestone evidence exists';
 END IF;
END$$;
DROP TABLE milestone_evidence_parents;
DROP TABLE milestone_evidence_blockers;
DROP TABLE milestone_evidence_assessments;
DROP FUNCTION validate_milestone_evidence_graph();
DROP FUNCTION reject_milestone_evidence_mutation();
