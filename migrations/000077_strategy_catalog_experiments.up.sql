LOCK TABLE strategies,accounts,account_capital_policy_bindings,capital_margin_policy_artifacts,
  simulation_policy_artifacts,dataset_manifests,dataset_manifest_partitions,dataset_quality_results IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION strategy_family_identity(slug_value TEXT,name_value TEXT,thesis_value TEXT,asset_classes JSONB) RETURNS TEXT AS $$
  SELECT '{"schema":"strategy-family-v1","slug":'||dataset_json_string(slug_value)||',"name":'||dataset_json_string(name_value)||
    ',"thesis":'||dataset_json_string(thesis_value)||',"asset_classes":'||dataset_json_text_array(asset_classes)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION strategy_version_identity(family_id_value TEXT,compiler_kind_value TEXT,compiler_version_value TEXT,source_commit_value TEXT,
  source_tree_sha_value TEXT,config_schema_value TEXT,config_text TEXT,decision_contract_value TEXT,kinds_text TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"strategy-version-v1","family_id":'||dataset_json_string(family_id_value)||
    ',"compiler_kind":'||dataset_json_string(compiler_kind_value)||',"compiler_version":'||dataset_json_string(compiler_version_value)||
    ',"source_commit":'||dataset_json_string(source_commit_value)||',"source_tree_sha256":'||dataset_json_string(source_tree_sha_value)||
    ',"config_schema":'||dataset_json_string(config_schema_value)||',"config":'||config_text||
    ',"decision_contract":'||dataset_json_string(decision_contract_value)||',"required_dataset_kinds":'||kinds_text||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION strategy_experiment_identity(version_id_value TEXT,account_id_value TEXT,binding_id_value TEXT,manifest_id_value TEXT,
  quality_id_value TEXT,simulation_version_value TEXT,capital_version_value TEXT,mode_value TEXT,start_value TEXT,end_value TEXT,
  seed_value BIGINT,quarantined_value BOOLEAN) RETURNS TEXT AS $$
  SELECT '{"schema":"research-experiment-v1","state":"declared","version_id":'||dataset_json_string(version_id_value)||
    ',"account_id":'||dataset_json_string(account_id_value)||',"capital_binding_id":'||dataset_json_string(binding_id_value)||
    ',"manifest_id":'||dataset_json_string(manifest_id_value)||',"quality_result_id":'||dataset_json_string(quality_id_value)||
    ',"simulation_policy_version":'||dataset_json_string(simulation_version_value)||',"capital_policy_version":'||dataset_json_string(capital_version_value)||
    ',"mode":'||dataset_json_string(mode_value)||',"evaluation_start":'||dataset_json_string(start_value)||
    ',"evaluation_end":'||dataset_json_string(end_value)||',"seed":'||seed_value::TEXT||',"dataset_quarantined":'||quarantined_value::TEXT||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION strategy_deployment_identity(version_id_value TEXT,account_id_value TEXT,binding_id_value TEXT,budget_value TEXT,
  cron_value TEXT,timezone_value TEXT,risk_version_value TEXT,mode_value TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"strategy-deployment-v1","state":"proposed","activation_authority":"promotion-evaluator-v1","version_id":'||dataset_json_string(version_id_value)||
    ',"account_id":'||dataset_json_string(account_id_value)||',"capital_binding_id":'||dataset_json_string(binding_id_value)||
    ',"budget":'||dataset_json_string(budget_value)||',"schedule_cron":'||dataset_json_string(cron_value)||
    ',"timezone":'||dataset_json_string(timezone_value)||',"risk_policy_version":'||dataset_json_string(risk_version_value)||
    ',"mode":'||dataset_json_string(mode_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION strategy_legacy_mapping_identity(legacy_id_value TEXT,family_id_value TEXT,snapshot_sha_value TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"legacy-strategy-family-mapping-v1","state":"legacy_unvalidated","legacy_strategy_id":'||dataset_json_string(legacy_id_value)||
    ',"family_id":'||dataset_json_string(family_id_value)||',"legacy_snapshot_sha256":'||dataset_json_string(snapshot_sha_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION strategy_lifecycle_identity(entity_kind_value TEXT,entity_id_value TEXT,event_kind_value TEXT,next_state_value TEXT,evidence_sha_value TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"strategy-catalog-lifecycle-event-v1","entity_kind":'||dataset_json_string(entity_kind_value)||
    ',"entity_id":'||dataset_json_string(entity_id_value)||',"event_kind":'||dataset_json_string(event_kind_value)||
    ',"prior_state":"","next_state":'||dataset_json_string(next_state_value)||',"evidence_sha256":'||dataset_json_string(evidence_sha_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION strategy_legacy_snapshot_sha(target UUID) RETURNS TEXT AS $$
  SELECT encode(digest(convert_to(to_jsonb(s)::TEXT,'UTF8'),'sha256'),'hex') FROM strategies s WHERE s.id=target;
$$ LANGUAGE sql STABLE STRICT;

CREATE TABLE strategy_families (
  id UUID PRIMARY KEY,
  schema_name TEXT NOT NULL CHECK(schema_name='strategy-family-v1'),
  slug TEXT NOT NULL UNIQUE CHECK(slug ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND char_length(slug)<=96),
  name TEXT NOT NULL CHECK(name<>'' AND name=btrim(name) AND char_length(name)<=160),
  thesis TEXT NOT NULL CHECK(thesis<>'' AND thesis=btrim(thesis) AND char_length(thesis)<=4096),
  asset_classes JSONB NOT NULL CHECK(jsonb_typeof(asset_classes)='array' AND jsonb_array_length(asset_classes)>0),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(id=economic_deterministic_uuid('strategy-family',slug)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(convert_from(canonical_bytes,'UTF8')=strategy_family_identity(slug,name,thesis,asset_classes))
);

CREATE TABLE strategy_versions (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='strategy-version-v1'),
  family_id UUID NOT NULL REFERENCES strategy_families(id) ON DELETE RESTRICT,
  compiler_kind TEXT NOT NULL CHECK(compiler_kind<>'' AND compiler_kind=btrim(compiler_kind) AND char_length(compiler_kind)<=128),
  compiler_version TEXT NOT NULL CHECK(compiler_version<>'' AND compiler_version=btrim(compiler_version) AND char_length(compiler_version)<=256),
  source_commit TEXT NOT NULL CHECK(source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  source_tree_sha256 TEXT NOT NULL CHECK(source_tree_sha256 ~ '^[0-9a-f]{64}$'),
  config_schema TEXT NOT NULL CHECK(config_schema<>'' AND config_schema=btrim(config_schema) AND char_length(config_schema)<=256),
  config_bytes BYTEA NOT NULL CHECK(octet_length(config_bytes)>1), config JSONB NOT NULL CHECK(jsonb_typeof(config)='object'),
  decision_contract TEXT NOT NULL CHECK(decision_contract<>'' AND decision_contract=btrim(decision_contract) AND char_length(decision_contract)<=256),
  required_kind_count INTEGER NOT NULL CHECK(required_kind_count>0),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(config=convert_from(config_bytes,'UTF8')::JSONB), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(id=economic_deterministic_uuid('strategy-version',schema_name||'@sha256:'||sha256))
);

CREATE TABLE strategy_version_dataset_kinds (
  version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT,
  family_id UUID NOT NULL REFERENCES strategy_families(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence>=0),
  kind TEXT NOT NULL CHECK(kind IN ('bars','benchmark_membership','corporate_actions','depth','external_object','filings','fundamentals','option_chains','option_contracts','prediction_books','prediction_fees','prediction_rules','prediction_trades','quotes','resolutions')),
  PRIMARY KEY(version_id,sequence), UNIQUE(version_id,kind)
);

CREATE TABLE research_experiments (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='research-experiment-v1'), state TEXT NOT NULL CHECK(state='declared'),
  version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT, account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  capital_binding_id UUID NOT NULL REFERENCES account_capital_policy_bindings(id) ON DELETE RESTRICT,
  manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
  quality_result_id UUID NOT NULL REFERENCES dataset_quality_results(id) ON DELETE RESTRICT,
  simulation_policy_version TEXT NOT NULL REFERENCES simulation_policy_artifacts(policy_version) ON DELETE RESTRICT,
  capital_policy_version TEXT NOT NULL REFERENCES capital_margin_policy_artifacts(policy_version) ON DELETE RESTRICT,
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  evaluation_start TIMESTAMPTZ NOT NULL CHECK(evaluation_start=date_trunc('microseconds',evaluation_start)),
  evaluation_end TIMESTAMPTZ NOT NULL CHECK(evaluation_end=date_trunc('microseconds',evaluation_end) AND evaluation_end>evaluation_start),
  seed BIGINT NOT NULL, dataset_quarantined BOOLEAN NOT NULL,
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('research-experiment',schema_name||'@sha256:'||sha256)),
  CHECK(convert_from(canonical_bytes,'UTF8')=strategy_experiment_identity(version_id::TEXT,account_id::TEXT,capital_binding_id::TEXT,manifest_id::TEXT,
    quality_result_id::TEXT,simulation_policy_version,capital_policy_version,mode,
    to_char(evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),seed,dataset_quarantined))
);

CREATE TABLE strategy_deployments (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='strategy-deployment-v1'), state TEXT NOT NULL CHECK(state='proposed'),
  activation_authority TEXT NOT NULL CHECK(activation_authority='promotion-evaluator-v1'),
  version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT, account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  capital_binding_id UUID NOT NULL REFERENCES account_capital_policy_bindings(id) ON DELETE RESTRICT,
  budget NUMERIC(28,8) NOT NULL CHECK(budget>0), schedule_cron TEXT NOT NULL CHECK(schedule_cron<>'' AND schedule_cron=btrim(schedule_cron) AND char_length(schedule_cron)<=256),
  timezone_name TEXT NOT NULL CHECK(timezone_name<>'' AND timezone_name=btrim(timezone_name) AND char_length(timezone_name)<=128),
  risk_policy_version TEXT NOT NULL CHECK(risk_policy_version<>'' AND risk_policy_version=btrim(risk_policy_version) AND char_length(risk_policy_version)<=256),
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('strategy-deployment',schema_name||'@sha256:'||sha256)),
  CHECK(convert_from(canonical_bytes,'UTF8')=strategy_deployment_identity(version_id::TEXT,account_id::TEXT,capital_binding_id::TEXT,
    trim_scale(budget)::TEXT,schedule_cron,timezone_name,risk_policy_version,mode))
);

CREATE TABLE legacy_strategy_family_mappings (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='legacy-strategy-family-mapping-v1'), state TEXT NOT NULL CHECK(state='legacy_unvalidated'),
  legacy_strategy_id UUID NOT NULL UNIQUE REFERENCES strategies(id) ON DELETE RESTRICT,
  family_id UUID NOT NULL REFERENCES strategy_families(id) ON DELETE RESTRICT,
  legacy_snapshot_sha256 TEXT NOT NULL CHECK(legacy_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(id=economic_deterministic_uuid('legacy-strategy-family-mapping',legacy_strategy_id::TEXT)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(convert_from(canonical_bytes,'UTF8')=strategy_legacy_mapping_identity(legacy_strategy_id::TEXT,family_id::TEXT,legacy_snapshot_sha256))
);

CREATE TABLE strategy_catalog_lifecycle_events (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='strategy-catalog-lifecycle-event-v1'),
  entity_kind TEXT NOT NULL CHECK(entity_kind IN ('family','version','experiment','deployment','legacy_mapping')),
  entity_id UUID NOT NULL, event_kind TEXT NOT NULL CHECK(event_kind IN ('registered','declared','proposed','mapped')),
  prior_state TEXT NOT NULL CHECK(prior_state=''), next_state TEXT NOT NULL CHECK(next_state IN ('registered','declared','proposed','legacy_unvalidated')),
  evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'), sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)), UNIQUE(entity_kind,entity_id,event_kind),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('strategy-catalog-lifecycle-event',entity_kind,entity_id::TEXT,event_kind,evidence_sha256)),
  CHECK(convert_from(canonical_bytes,'UTF8')=strategy_lifecycle_identity(entity_kind,entity_id::TEXT,event_kind,next_state,evidence_sha256)),
  CHECK((entity_kind IN ('family','version') AND event_kind='registered' AND next_state='registered') OR
    (entity_kind='experiment' AND event_kind='declared' AND next_state='declared') OR
    (entity_kind='deployment' AND event_kind='proposed' AND next_state='proposed') OR
    (entity_kind='legacy_mapping' AND event_kind='mapped' AND next_state='legacy_unvalidated'))
);

CREATE FUNCTION validate_strategy_family() RETURNS TRIGGER AS $$
BEGIN
  PERFORM 1 FROM strategy_families f WHERE f.id=NEW.id AND
    NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(f.asset_classes) value
      WHERE value NOT IN ('crypto_spot','equity','etf','future','option','prediction_contract')) AND
    (SELECT count(*)=count(DISTINCT value) FROM jsonb_array_elements_text(f.asset_classes) value) AND
    NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(f.asset_classes) WITH ORDINALITY current_value(value,ordinal)
      JOIN jsonb_array_elements_text(f.asset_classes) WITH ORDINALITY prior_value(value,ordinal)
        ON prior_value.ordinal=current_value.ordinal-1
      WHERE prior_value.value COLLATE "C">=current_value.value COLLATE "C");
  IF NOT FOUND THEN RAISE EXCEPTION 'strategy family asset classes are not canonical'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_strategy_version_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; kinds_text TEXT;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'version_id')::UUID);
  SELECT '['||COALESCE(string_agg(dataset_json_string(kind),',' ORDER BY sequence),'')||']' INTO kinds_text FROM strategy_version_dataset_kinds WHERE version_id=target;
  PERFORM 1 FROM strategy_versions v WHERE v.id=target AND v.required_kind_count=(SELECT count(*) FROM strategy_version_dataset_kinds WHERE version_id=v.id) AND
    (SELECT min(sequence)=0 AND max(sequence)=v.required_kind_count-1 FROM strategy_version_dataset_kinds WHERE version_id=v.id) AND
    NOT EXISTS(SELECT 1 FROM strategy_version_dataset_kinds k WHERE k.version_id=v.id AND k.family_id<>v.family_id) AND
    NOT EXISTS(SELECT 1 FROM strategy_version_dataset_kinds current_kind JOIN strategy_version_dataset_kinds prior_kind
      ON prior_kind.version_id=current_kind.version_id AND prior_kind.sequence=current_kind.sequence-1 WHERE current_kind.version_id=v.id AND
        prior_kind.kind COLLATE "C">=current_kind.kind COLLATE "C") AND
    v.canonical_bytes=convert_to(strategy_version_identity(v.family_id::TEXT,v.compiler_kind,v.compiler_version,v.source_commit,v.source_tree_sha256,
      v.config_schema,convert_from(v.config_bytes,'UTF8'),v.decision_contract,kinds_text),'UTF8');
  IF NOT FOUND THEN RAISE EXCEPTION 'strategy version graph does not reconstruct'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_research_experiment() RETURNS TRIGGER AS $$
BEGIN
  PERFORM 1 FROM research_experiments e
    JOIN dataset_quality_results q ON q.id=e.quality_result_id AND q.manifest_id=e.manifest_id AND q.quarantined=e.dataset_quarantined
    JOIN dataset_manifests m ON m.id=e.manifest_id AND m.decision_cutoff>=e.evaluation_end
    JOIN account_capital_policy_bindings b ON b.id=e.capital_binding_id AND b.account_id=e.account_id AND b.policy_version=e.capital_policy_version
    WHERE e.id=NEW.id AND ((e.mode='paper_scored' AND NOT e.dataset_quarantined AND b.environment='paper_scored' AND b.evidence_class='promotion_evidence') OR
      (e.mode='paper_stress' AND b.environment='paper_stress' AND b.evidence_class='synthetic_stress')) AND
      NOT EXISTS(SELECT 1 FROM strategy_version_dataset_kinds k WHERE k.version_id=e.version_id AND NOT EXISTS(
        SELECT 1 FROM dataset_manifest_partitions p WHERE p.manifest_id=e.manifest_id AND p.kind=k.kind));
  IF NOT FOUND THEN RAISE EXCEPTION 'research experiment evidence does not match'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_strategy_deployment() RETURNS TRIGGER AS $$
BEGIN
  PERFORM 1 FROM strategy_deployments d JOIN account_capital_policy_bindings b ON b.id=d.capital_binding_id AND b.account_id=d.account_id
    WHERE d.id=NEW.id AND d.budget<=b.starting_capital AND d.mode=b.environment AND
      ((d.mode='paper_scored' AND b.evidence_class='promotion_evidence') OR (d.mode='paper_stress' AND b.evidence_class='synthetic_stress'));
  IF NOT FOUND THEN RAISE EXCEPTION 'strategy deployment assignment does not match account capital evidence'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_legacy_strategy_mapping() RETURNS TRIGGER AS $$
BEGIN
  IF NEW.legacy_snapshot_sha256 IS DISTINCT FROM strategy_legacy_snapshot_sha(NEW.legacy_strategy_id) THEN
    RAISE EXCEPTION 'legacy strategy snapshot digest does not match';
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_strategy_catalog_lifecycle() RETURNS TRIGGER AS $$
DECLARE target_kind TEXT; target UUID; event_row RECORD; parent_sha TEXT;
BEGIN
  IF TG_TABLE_NAME='strategy_catalog_lifecycle_events' THEN target_kind:=NEW.entity_kind; target:=NEW.entity_id;
  ELSE target_kind:=CASE TG_TABLE_NAME WHEN 'strategy_families' THEN 'family' WHEN 'strategy_versions' THEN 'version'
    WHEN 'research_experiments' THEN 'experiment' WHEN 'strategy_deployments' THEN 'deployment' ELSE 'legacy_mapping' END; target:=NEW.id; END IF;
  SELECT * INTO event_row FROM strategy_catalog_lifecycle_events WHERE entity_kind=target_kind AND entity_id=target;
  EXECUTE format('SELECT sha256 FROM %I WHERE id=$1',CASE target_kind WHEN 'family' THEN 'strategy_families' WHEN 'version' THEN 'strategy_versions'
    WHEN 'experiment' THEN 'research_experiments' WHEN 'deployment' THEN 'strategy_deployments' ELSE 'legacy_strategy_family_mappings' END) INTO parent_sha USING target;
  IF parent_sha IS NULL OR event_row.id IS NULL OR event_row.evidence_sha256<>parent_sha THEN
    RAISE EXCEPTION 'strategy catalog lifecycle evidence does not match parent';
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION reject_strategy_catalog_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'strategy catalog evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['strategy_families','strategy_versions','strategy_version_dataset_kinds','research_experiments','strategy_deployments','legacy_strategy_family_mappings','strategy_catalog_lifecycle_events'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_strategy_catalog_mutation()','trg_'||name||'_immutable',name); END LOOP; END $$;

CREATE CONSTRAINT TRIGGER trg_strategy_version_graph AFTER INSERT ON strategy_versions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_version_graph();
CREATE CONSTRAINT TRIGGER trg_strategy_family_graph AFTER INSERT ON strategy_families DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_family();
CREATE CONSTRAINT TRIGGER trg_strategy_version_kind_graph AFTER INSERT ON strategy_version_dataset_kinds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_version_graph();
CREATE CONSTRAINT TRIGGER trg_research_experiment_graph AFTER INSERT ON research_experiments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_research_experiment();
CREATE CONSTRAINT TRIGGER trg_strategy_deployment_graph AFTER INSERT ON strategy_deployments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_deployment();
CREATE CONSTRAINT TRIGGER trg_legacy_strategy_mapping_graph AFTER INSERT ON legacy_strategy_family_mappings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_legacy_strategy_mapping();
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['strategy_families','strategy_versions','research_experiments','strategy_deployments','legacy_strategy_family_mappings','strategy_catalog_lifecycle_events'] LOOP
  EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_strategy_catalog_lifecycle()','trg_'||name||'_lifecycle',name); END LOOP; END $$;

CREATE INDEX idx_strategy_versions_family ON strategy_versions(family_id,created_at,id);
CREATE INDEX idx_research_experiments_version ON research_experiments(version_id,mode,created_at,id);
CREATE INDEX idx_research_experiments_dataset ON research_experiments(manifest_id,quality_result_id);
CREATE INDEX idx_strategy_deployments_account ON strategy_deployments(account_id,state,created_at,id);
CREATE INDEX idx_legacy_strategy_family ON legacy_strategy_family_mappings(family_id,legacy_strategy_id);
