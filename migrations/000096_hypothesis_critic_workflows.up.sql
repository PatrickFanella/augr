LOCK TABLE dataset_manifests,robustness_policy_artifacts,robustness_search_families,statistical_robustness_assessments,generated_strategy_specs,strategy_versions,generated_strategy_compilation_receipts IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE research_hypotheses (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='evidence-bound-hypothesis-v1'), state TEXT NOT NULL CHECK(state='authored'),
  workflow_key TEXT NOT NULL UNIQUE CHECK(workflow_key ~ '^[a-z][a-z0-9_-]{0,95}$'),
  manifest_id UUID NOT NULL REFERENCES dataset_manifests(id), manifest_sha256 TEXT NOT NULL CHECK(manifest_sha256 ~ '^[0-9a-f]{64}$'),
  robustness_policy_id UUID NOT NULL REFERENCES robustness_policy_artifacts(id), robustness_policy_sha256 TEXT NOT NULL CHECK(robustness_policy_sha256 ~ '^[0-9a-f]{64}$'),
  robustness_family_id UUID NOT NULL REFERENCES robustness_search_families(id), robustness_family_sha256 TEXT NOT NULL CHECK(robustness_family_sha256 ~ '^[0-9a-f]{64}$'),
  assessment_id UUID NOT NULL REFERENCES statistical_robustness_assessments(id), assessment_sha256 TEXT NOT NULL CHECK(assessment_sha256 ~ '^[0-9a-f]{64}$'),
  spec_id UUID NOT NULL REFERENCES generated_strategy_specs(id), spec_sha256 TEXT NOT NULL CHECK(spec_sha256 ~ '^[0-9a-f]{64}$'),
  version_id UUID NOT NULL REFERENCES strategy_versions(id), version_sha256 TEXT NOT NULL CHECK(version_sha256 ~ '^[0-9a-f]{64}$'),
  receipt_id UUID NOT NULL REFERENCES generated_strategy_compilation_receipts(id), receipt_sha256 TEXT NOT NULL CHECK(receipt_sha256 ~ '^[0-9a-f]{64}$'),
  source_count INT NOT NULL CHECK(source_count BETWEEN 1 AND 128), search_count INT NOT NULL CHECK(search_count BETWEEN 1 AND 64), test_count INT NOT NULL CHECK(test_count BETWEEN 1 AND 128),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds',NOW()),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('evidence-bound-hypothesis',schema_name||'@sha256:'||sha256))
);
CREATE TABLE research_hypothesis_sources (
  hypothesis_id UUID NOT NULL REFERENCES research_hypotheses(id), sequence INT NOT NULL CHECK(sequence>=0), source_key TEXT NOT NULL, canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(hypothesis_id,sequence), UNIQUE(hypothesis_id,source_key)
);
CREATE TABLE research_hypothesis_source_manifest_keys (
  hypothesis_id UUID NOT NULL, source_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0), manifest_source_key TEXT NOT NULL,
  PRIMARY KEY(hypothesis_id,source_sequence,sequence), UNIQUE(hypothesis_id,source_sequence,manifest_source_key),
  FOREIGN KEY(hypothesis_id,source_sequence) REFERENCES research_hypothesis_sources(hypothesis_id,sequence)
);
CREATE TABLE research_hypothesis_searches (
  hypothesis_id UUID NOT NULL REFERENCES research_hypotheses(id), sequence INT NOT NULL CHECK(sequence>=0), search_key TEXT NOT NULL, canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(hypothesis_id,sequence), UNIQUE(hypothesis_id,search_key)
);
CREATE TABLE research_hypothesis_search_results (
  hypothesis_id UUID NOT NULL, search_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0), source_key TEXT NOT NULL, rank INT NOT NULL CHECK(rank>0), selected BOOLEAN NOT NULL, canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(hypothesis_id,search_sequence,sequence), UNIQUE(hypothesis_id,search_sequence,rank), UNIQUE(hypothesis_id,search_sequence,source_key),
  FOREIGN KEY(hypothesis_id,search_sequence) REFERENCES research_hypothesis_searches(hypothesis_id,sequence)
);
CREATE TABLE research_hypothesis_tests (
  hypothesis_id UUID NOT NULL REFERENCES research_hypotheses(id), sequence INT NOT NULL CHECK(sequence>=0), test_key TEXT NOT NULL, test_type TEXT NOT NULL CHECK(test_type IN ('spec_property','spec_example','leakage','cost','baseline','refutation')), canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(hypothesis_id,sequence), UNIQUE(hypothesis_id,test_key)
);

CREATE TABLE research_critics (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='independent-research-critic-v1'), state TEXT NOT NULL CHECK(state='reviewed'),
  review_key TEXT NOT NULL CHECK(review_key ~ '^[a-z][a-z0-9_-]{0,95}$'), hypothesis_id UUID NOT NULL REFERENCES research_hypotheses(id), hypothesis_sha256 TEXT NOT NULL CHECK(hypothesis_sha256 ~ '^[0-9a-f]{64}$'),
  recommendation TEXT NOT NULL CHECK(recommendation IN ('revise','reject','ready_for_experiment_review')), finding_count INT NOT NULL CHECK(finding_count BETWEEN 0 AND 128), check_count INT NOT NULL CHECK(check_count=6),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds',NOW()),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('independent-research-critic',schema_name||'@sha256:'||sha256)), UNIQUE(hypothesis_id,review_key)
);
CREATE TABLE research_critic_findings (
  critic_id UUID NOT NULL REFERENCES research_critics(id), sequence INT NOT NULL CHECK(sequence>=0), finding_key TEXT NOT NULL, category TEXT NOT NULL CHECK(category IN ('source_coverage','leakage','multiple_testing','cost_capacity','test_completeness','reproducibility')), severity TEXT NOT NULL CHECK(severity IN ('low','medium','high','critical')), status TEXT NOT NULL CHECK(status IN ('open','resolved')), canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(critic_id,sequence), UNIQUE(critic_id,finding_key)
);
CREATE TABLE research_critic_finding_references (
  critic_id UUID NOT NULL, finding_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0), reference TEXT NOT NULL,
  PRIMARY KEY(critic_id,finding_sequence,sequence), UNIQUE(critic_id,finding_sequence,reference), FOREIGN KEY(critic_id,finding_sequence) REFERENCES research_critic_findings(critic_id,sequence)
);
CREATE TABLE research_critic_checks (
  critic_id UUID NOT NULL REFERENCES research_critics(id), sequence INT NOT NULL CHECK(sequence>=0), check_name TEXT NOT NULL CHECK(check_name IN ('source_coverage','leakage','multiple_testing','cost_capacity','test_completeness','reproducibility')), check_state TEXT NOT NULL CHECK(check_state IN ('pass','fail','unknown')), canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(critic_id,sequence), UNIQUE(critic_id,check_name)
);
CREATE TABLE research_critic_check_references (
  critic_id UUID NOT NULL, check_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0), reference TEXT NOT NULL,
  PRIMARY KEY(critic_id,check_sequence,sequence), UNIQUE(critic_id,check_sequence,reference), FOREIGN KEY(critic_id,check_sequence) REFERENCES research_critic_checks(critic_id,sequence)
);

CREATE FUNCTION validate_research_hypothesis_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; h research_hypotheses%ROWTYPE;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'hypothesis_id')::UUID); SELECT * INTO h FROM research_hypotheses WHERE id=target;
  IF h.id IS NULL OR h.canonical_json->>'schema'<>h.schema_name OR h.canonical_json->>'state'<>h.state OR h.canonical_json->>'workflow_key'<>h.workflow_key OR
    h.canonical_json->'parents'<>jsonb_build_object('manifest_id',h.manifest_id::TEXT,'manifest_sha256',h.manifest_sha256,'robustness_policy_id',h.robustness_policy_id::TEXT,'robustness_policy_sha256',h.robustness_policy_sha256,'robustness_family_id',h.robustness_family_id::TEXT,'robustness_family_sha256',h.robustness_family_sha256,'assessment_id',h.assessment_id::TEXT,'assessment_sha256',h.assessment_sha256,'spec_id',h.spec_id::TEXT,'spec_sha256',h.spec_sha256,'version_id',h.version_id::TEXT,'version_sha256',h.version_sha256,'receipt_id',h.receipt_id::TEXT,'receipt_sha256',h.receipt_sha256) OR
    h.source_count<>jsonb_array_length(h.canonical_json->'sources') OR h.search_count<>jsonb_array_length(h.canonical_json->'searches') OR h.test_count<>jsonb_array_length(h.canonical_json->'tests') OR
    h.source_count<>(SELECT count(*) FROM research_hypothesis_sources WHERE hypothesis_id=h.id) OR h.search_count<>(SELECT count(*) FROM research_hypothesis_searches WHERE hypothesis_id=h.id) OR h.test_count<>(SELECT count(*) FROM research_hypothesis_tests WHERE hypothesis_id=h.id) OR
    EXISTS(SELECT 1 FROM research_hypothesis_sources s WHERE s.hypothesis_id=h.id AND (s.canonical_row<>h.canonical_json->'sources'->s.sequence OR s.source_key<>s.canonical_row->>'key' OR jsonb_array_length(s.canonical_row->'manifest_source_keys')<>(SELECT count(*) FROM research_hypothesis_source_manifest_keys k WHERE k.hypothesis_id=h.id AND k.source_sequence=s.sequence) OR EXISTS(SELECT 1 FROM research_hypothesis_source_manifest_keys k WHERE k.hypothesis_id=h.id AND k.source_sequence=s.sequence AND to_jsonb(k.manifest_source_key)<>s.canonical_row->'manifest_source_keys'->k.sequence))) OR
    EXISTS(SELECT 1 FROM research_hypothesis_searches s WHERE s.hypothesis_id=h.id AND (s.canonical_row<>h.canonical_json->'searches'->s.sequence OR s.search_key<>s.canonical_row->>'key' OR jsonb_array_length(s.canonical_row->'results')<>(SELECT count(*) FROM research_hypothesis_search_results r WHERE r.hypothesis_id=h.id AND r.search_sequence=s.sequence) OR EXISTS(SELECT 1 FROM research_hypothesis_search_results r WHERE r.hypothesis_id=h.id AND r.search_sequence=s.sequence AND (r.canonical_row<>s.canonical_row->'results'->r.sequence OR r.source_key<>r.canonical_row->>'source_key' OR r.rank<>(r.canonical_row->>'rank')::INT OR r.selected<>(r.canonical_row->>'selected')::BOOLEAN)))) OR
    EXISTS(SELECT 1 FROM research_hypothesis_tests t WHERE t.hypothesis_id=h.id AND (t.canonical_row<>h.canonical_json->'tests'->t.sequence OR t.test_key<>t.canonical_row->>'key' OR t.test_type<>t.canonical_row->>'type')) OR
    h.manifest_sha256<>(SELECT sha256 FROM dataset_manifests WHERE id=h.manifest_id) OR h.robustness_policy_sha256<>(SELECT sha256 FROM robustness_policy_artifacts WHERE id=h.robustness_policy_id) OR h.robustness_family_sha256<>(SELECT sha256 FROM robustness_search_families WHERE id=h.robustness_family_id) OR h.assessment_sha256<>(SELECT sha256 FROM statistical_robustness_assessments WHERE id=h.assessment_id) OR h.spec_sha256<>(SELECT sha256 FROM generated_strategy_specs WHERE id=h.spec_id) OR h.version_sha256<>(SELECT sha256 FROM strategy_versions WHERE id=h.version_id) OR h.receipt_sha256<>(SELECT sha256 FROM generated_strategy_compilation_receipts WHERE id=h.receipt_id)
  THEN RAISE EXCEPTION 'research hypothesis graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_research_critic_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; c research_critics%ROWTYPE;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'critic_id')::UUID); SELECT * INTO c FROM research_critics WHERE id=target;
  IF c.id IS NULL OR c.hypothesis_sha256<>(SELECT sha256 FROM research_hypotheses WHERE id=c.hypothesis_id) OR c.canonical_json->>'schema'<>c.schema_name OR c.canonical_json->>'state'<>c.state OR c.canonical_json->>'review_key'<>c.review_key OR c.canonical_json->>'hypothesis_id'<>c.hypothesis_id::TEXT OR c.canonical_json->>'hypothesis_sha256'<>c.hypothesis_sha256 OR c.canonical_json->>'recommendation'<>c.recommendation OR
    c.finding_count<>jsonb_array_length(c.canonical_json->'findings') OR c.check_count<>jsonb_array_length(c.canonical_json->'checks') OR c.finding_count<>(SELECT count(*) FROM research_critic_findings WHERE critic_id=c.id) OR c.check_count<>(SELECT count(*) FROM research_critic_checks WHERE critic_id=c.id) OR
    EXISTS(SELECT 1 FROM research_critic_findings f WHERE f.critic_id=c.id AND (f.canonical_row<>c.canonical_json->'findings'->f.sequence OR f.finding_key<>f.canonical_row->>'key' OR f.category<>f.canonical_row->>'category' OR f.severity<>f.canonical_row->>'severity' OR f.status<>f.canonical_row->>'status' OR jsonb_array_length(f.canonical_row->'references')<>(SELECT count(*) FROM research_critic_finding_references r WHERE r.critic_id=c.id AND r.finding_sequence=f.sequence) OR EXISTS(SELECT 1 FROM research_critic_finding_references r WHERE r.critic_id=c.id AND r.finding_sequence=f.sequence AND to_jsonb(r.reference)<>f.canonical_row->'references'->r.sequence))) OR
    EXISTS(SELECT 1 FROM research_critic_checks k WHERE k.critic_id=c.id AND (k.canonical_row<>c.canonical_json->'checks'->k.sequence OR k.check_name<>k.canonical_row->>'name' OR k.check_state<>k.canonical_row->>'state' OR jsonb_array_length(k.canonical_row->'references')<>(SELECT count(*) FROM research_critic_check_references r WHERE r.critic_id=c.id AND r.check_sequence=k.sequence) OR EXISTS(SELECT 1 FROM research_critic_check_references r WHERE r.critic_id=c.id AND r.check_sequence=k.sequence AND to_jsonb(r.reference)<>k.canonical_row->'references'->r.sequence)))
  THEN RAISE EXCEPTION 'research critic graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_research_workflow_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'research workflow evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['research_hypotheses','research_hypothesis_sources','research_hypothesis_source_manifest_keys','research_hypothesis_searches','research_hypothesis_search_results','research_hypothesis_tests','research_critics','research_critic_findings','research_critic_finding_references','research_critic_checks','research_critic_check_references'] LOOP EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_research_workflow_mutation()','trg_'||name||'_immutable',name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_research_hypothesis_graph AFTER INSERT ON research_hypotheses DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_research_hypothesis_graph();
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['research_hypothesis_sources','research_hypothesis_source_manifest_keys','research_hypothesis_searches','research_hypothesis_search_results','research_hypothesis_tests'] LOOP EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_research_hypothesis_graph()','trg_'||name||'_graph',name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_research_critic_graph AFTER INSERT ON research_critics DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_research_critic_graph();
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['research_critic_findings','research_critic_finding_references','research_critic_checks','research_critic_check_references'] LOOP EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_research_critic_graph()','trg_'||name||'_graph',name); END LOOP; END $$;
CREATE INDEX idx_research_hypotheses_version ON research_hypotheses(version_id,created_at,id);
CREATE INDEX idx_research_critics_hypothesis ON research_critics(hypothesis_id,created_at,id);
