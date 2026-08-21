LOCK TABLE dataset_quality_findings,dataset_quality_checks,dataset_quality_results,dataset_manifest_observations,dataset_manifest_partitions,dataset_manifests,dataset_quality_policy_artifacts IN ACCESS EXCLUSIVE MODE;

DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM dataset_quality_policy_artifacts) OR EXISTS(SELECT 1 FROM dataset_manifests) OR EXISTS(SELECT 1 FROM dataset_quality_results) THEN
    RAISE EXCEPTION 'cannot roll back migration 76 while dataset evidence exists';
  END IF;
END $$;

DROP TABLE dataset_quality_findings;
DROP TABLE dataset_quality_checks;
DROP TABLE dataset_quality_results;
DROP TABLE dataset_manifest_observations;
DROP TABLE dataset_manifest_partitions;
DROP TABLE dataset_manifests;
DROP TABLE dataset_quality_policy_artifacts;
DROP FUNCTION reject_dataset_evidence_mutation();
DROP FUNCTION validate_dataset_quality_graph();
DROP FUNCTION validate_dataset_manifest_graph();
DROP FUNCTION dataset_partition_content_digest(UUID,INTEGER);
DROP FUNCTION IF EXISTS dataset_check_applies(TEXT,TEXT);
DROP FUNCTION IF EXISTS dataset_finding_key(TEXT,TEXT,JSONB);
DROP FUNCTION IF EXISTS dataset_check_key(TEXT,TEXT);
DROP FUNCTION IF EXISTS dataset_json_text_array(JSONB);
DROP FUNCTION dataset_quality_identity(TEXT,TEXT,BOOLEAN,INTEGER,INTEGER,TEXT,TEXT);
DROP FUNCTION dataset_finding_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION dataset_check_identity(TEXT,TEXT,TEXT,TEXT,BOOLEAN,TEXT,TEXT,TEXT);
DROP FUNCTION dataset_manifest_identity(TEXT,INTEGER,INTEGER,TEXT);
DROP FUNCTION dataset_partition_identity(INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,INTEGER,TEXT,TEXT,TEXT);
DROP FUNCTION dataset_observation_identity(INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION dataset_json_string(TEXT);
