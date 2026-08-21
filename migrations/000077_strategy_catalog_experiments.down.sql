LOCK TABLE strategy_catalog_lifecycle_events,legacy_strategy_family_mappings,strategy_deployments,research_experiments,
  strategy_version_dataset_kinds,strategy_versions,strategy_families IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM strategy_families) OR EXISTS(SELECT 1 FROM strategy_versions) OR EXISTS(SELECT 1 FROM research_experiments) OR
    EXISTS(SELECT 1 FROM strategy_deployments) OR EXISTS(SELECT 1 FROM legacy_strategy_family_mappings) OR EXISTS(SELECT 1 FROM strategy_catalog_lifecycle_events) THEN
    RAISE EXCEPTION 'cannot roll back migration 77 while strategy catalog evidence exists';
  END IF;
END $$;
DROP TABLE strategy_catalog_lifecycle_events;
DROP TABLE legacy_strategy_family_mappings;
DROP TABLE strategy_deployments;
DROP TABLE research_experiments;
DROP TABLE strategy_version_dataset_kinds;
DROP TABLE strategy_versions;
DROP TABLE strategy_families;
DROP FUNCTION reject_strategy_catalog_mutation();
DROP FUNCTION validate_strategy_catalog_lifecycle();
DROP FUNCTION validate_legacy_strategy_mapping();
DROP FUNCTION validate_strategy_deployment();
DROP FUNCTION validate_research_experiment();
DROP FUNCTION validate_strategy_version_graph();
DROP FUNCTION validate_strategy_family();
DROP FUNCTION strategy_legacy_snapshot_sha(UUID);
DROP FUNCTION strategy_canonical_json(JSONB);
DROP FUNCTION strategy_lifecycle_identity(TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION strategy_legacy_mapping_identity(TEXT,TEXT,TEXT);
DROP FUNCTION strategy_deployment_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION strategy_experiment_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,BIGINT,BOOLEAN);
DROP FUNCTION strategy_version_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION strategy_family_identity(TEXT,TEXT,TEXT,JSONB);
