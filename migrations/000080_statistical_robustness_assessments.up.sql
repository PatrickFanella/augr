LOCK TABLE trade_portfolio_evaluations,evaluation_policy_artifacts,experiment_run_results,experiment_programs,strategy_versions IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE robustness_policy_artifacts (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='robustness-policy-v1'), version TEXT NOT NULL CHECK(version<>'' AND version=btrim(version) AND char_length(version)<=128),
  fold_count INTEGER NOT NULL CHECK(fold_count BETWEEN 2 AND 1000), purge_seconds BIGINT NOT NULL CHECK(purge_seconds>=0), embargo_seconds BIGINT NOT NULL CHECK(embargo_seconds>=0),
  bootstrap_algorithm TEXT NOT NULL CHECK(bootstrap_algorithm='xorshift64star-iid-percentile-v1'), bootstrap_seed NUMERIC(20,0) NOT NULL CHECK(bootstrap_seed>=0),
  bootstrap_iterations INTEGER NOT NULL CHECK(bootstrap_iterations BETWEEN 100 AND 100000), confidence_level TEXT NOT NULL CHECK(evaluation_decimal_valid(confidence_level) AND confidence_level::NUMERIC BETWEEN 0.5 AND 1),
  family_wise_alpha TEXT NOT NULL CHECK(evaluation_decimal_valid(family_wise_alpha) AND family_wise_alpha::NUMERIC>0 AND family_wise_alpha::NUMERIC<=1),
  multiple_testing_correction TEXT NOT NULL CHECK(multiple_testing_correction='holm_bonferroni'),
  max_largest_positive_share TEXT NOT NULL CHECK(evaluation_decimal_valid(max_largest_positive_share) AND max_largest_positive_share::NUMERIC BETWEEN 0 AND 1),
  max_top_decile_positive_share TEXT NOT NULL CHECK(evaluation_decimal_valid(max_top_decile_positive_share) AND max_top_decile_positive_share::NUMERIC BETWEEN 0 AND 1),
  max_perturbation_degradation TEXT NOT NULL CHECK(evaluation_decimal_valid(max_perturbation_degradation) AND max_perturbation_degradation::NUMERIC>=0),
  perturbation_count INTEGER NOT NULL CHECK(perturbation_count BETWEEN 1 AND 64), decimal_scale INTEGER NOT NULL CHECK(decimal_scale BETWEEN 6 AND 18),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)), CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB), CHECK(id=economic_deterministic_uuid('robustness-policy',schema_name||'@sha256:'||sha256))
);

CREATE TABLE robustness_policy_perturbations (
  policy_id UUID NOT NULL REFERENCES robustness_policy_artifacts(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  kind TEXT NOT NULL CHECK(kind<>'' AND kind=lower(btrim(kind)) AND char_length(kind)<=128), PRIMARY KEY(policy_id,sequence), UNIQUE(policy_id,kind)
);

CREATE TABLE robustness_search_families (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='robustness-search-family-v1'), name TEXT NOT NULL CHECK(name<>'' AND name=btrim(name) AND char_length(name)<=256),
  hypothesis_sha256 TEXT NOT NULL CHECK(hypothesis_sha256 ~ '^[0-9a-f]{64}$'), candidate_count INTEGER NOT NULL CHECK(candidate_count BETWEEN 1 AND 10000),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)), CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB), CHECK(id=economic_deterministic_uuid('robustness-search-family',schema_name||'@sha256:'||sha256))
);

CREATE TABLE robustness_search_family_candidates (
  family_id UUID NOT NULL REFERENCES robustness_search_families(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT, PRIMARY KEY(family_id,sequence), UNIQUE(family_id,version_id)
);

CREATE TABLE statistical_robustness_assessments (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='statistical-robustness-assessment-v1'), state TEXT NOT NULL CHECK(state='completed'),
  family_id UUID NOT NULL REFERENCES robustness_search_families(id) ON DELETE RESTRICT, family_sha256 TEXT NOT NULL CHECK(family_sha256 ~ '^[0-9a-f]{64}$'),
  policy_id UUID NOT NULL REFERENCES robustness_policy_artifacts(id) ON DELETE RESTRICT, policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')), candidate_count INTEGER NOT NULL CHECK(candidate_count>0),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)), CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB), CHECK(id=economic_deterministic_uuid('statistical-robustness-assessment',schema_name||'@sha256:'||sha256))
);

CREATE TABLE robustness_assessment_candidates (
  assessment_id UUID NOT NULL REFERENCES statistical_robustness_assessments(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT, fold_count INTEGER NOT NULL CHECK(fold_count>=2),
  statistic_count INTEGER NOT NULL CHECK(statistic_count>0), gate_count INTEGER NOT NULL CHECK(gate_count>0), PRIMARY KEY(assessment_id,sequence), UNIQUE(assessment_id,version_id)
);

CREATE TABLE robustness_assessment_folds (
  assessment_id UUID NOT NULL, candidate_sequence INTEGER NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>=0),
  train_start TIMESTAMPTZ NOT NULL CHECK(train_start=date_trunc('microseconds',train_start)), train_end TIMESTAMPTZ NOT NULL CHECK(train_end=date_trunc('microseconds',train_end) AND train_end>train_start),
  test_start TIMESTAMPTZ NOT NULL CHECK(test_start=date_trunc('microseconds',test_start) AND test_start>train_end),
  test_end TIMESTAMPTZ NOT NULL CHECK(test_end=date_trunc('microseconds',test_end) AND test_end>test_start),
  scenario_count INTEGER NOT NULL CHECK(scenario_count>=2), PRIMARY KEY(assessment_id,candidate_sequence,sequence),
  FOREIGN KEY(assessment_id,candidate_sequence) REFERENCES robustness_assessment_candidates(assessment_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE robustness_assessment_scenarios (
  assessment_id UUID NOT NULL, candidate_sequence INTEGER NOT NULL, fold_sequence INTEGER NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>=0),
  kind TEXT NOT NULL CHECK(kind<>'' AND kind=lower(btrim(kind)) AND char_length(kind)<=128), severity TEXT NOT NULL CHECK(severity<>'' AND severity=btrim(severity) AND char_length(severity)<=128),
  report_id UUID NOT NULL REFERENCES trade_portfolio_evaluations(id) ON DELETE RESTRICT, report_sha256 TEXT NOT NULL CHECK(report_sha256 ~ '^[0-9a-f]{64}$'),
  PRIMARY KEY(assessment_id,candidate_sequence,fold_sequence,sequence), UNIQUE(assessment_id,candidate_sequence,fold_sequence,kind),
  FOREIGN KEY(assessment_id,candidate_sequence,fold_sequence) REFERENCES robustness_assessment_folds(assessment_id,candidate_sequence,sequence) ON DELETE RESTRICT,
  CHECK((sequence=0 AND kind='baseline' AND severity='none') OR (sequence>0 AND kind<>'baseline'))
);

CREATE TABLE robustness_statistics (
  assessment_id UUID NOT NULL, candidate_sequence INTEGER NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>=0),
  name TEXT NOT NULL CHECK(name<>'' AND name=btrim(name) AND char_length(name)<=128), state TEXT NOT NULL CHECK(state IN ('available','unavailable')),
  value TEXT NOT NULL DEFAULT '', unit TEXT NOT NULL CHECK(unit<>'' AND unit=btrim(unit) AND char_length(unit)<=128), reason TEXT NOT NULL DEFAULT '' CHECK(reason=btrim(reason) AND char_length(reason)<=128),
  description TEXT NOT NULL CHECK(description<>'' AND description=btrim(description) AND char_length(description)<=256), PRIMARY KEY(assessment_id,candidate_sequence,sequence),
  UNIQUE(assessment_id,candidate_sequence,name), FOREIGN KEY(assessment_id,candidate_sequence) REFERENCES robustness_assessment_candidates(assessment_id,sequence) ON DELETE RESTRICT,
  CHECK((state='available' AND value<>'' AND reason='') OR (state='unavailable' AND value='' AND reason<>''))
);

CREATE TABLE robustness_gates (
  assessment_id UUID NOT NULL, candidate_sequence INTEGER NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>=0),
  name TEXT NOT NULL CHECK(name<>'' AND name=btrim(name) AND char_length(name)<=128), state TEXT NOT NULL CHECK(state IN ('pass','fail')),
  threshold TEXT NOT NULL CHECK(threshold<>'' AND threshold=btrim(threshold) AND char_length(threshold)<=128), observed TEXT NOT NULL CHECK(observed<>'' AND observed=btrim(observed) AND char_length(observed)<=128),
  reason TEXT NOT NULL DEFAULT '' CHECK(reason=btrim(reason) AND char_length(reason)<=128), description TEXT NOT NULL CHECK(description<>'' AND description=btrim(description) AND char_length(description)<=256),
  PRIMARY KEY(assessment_id,candidate_sequence,sequence), UNIQUE(assessment_id,candidate_sequence,name),
  FOREIGN KEY(assessment_id,candidate_sequence) REFERENCES robustness_assessment_candidates(assessment_id,sequence) ON DELETE RESTRICT,
  CHECK((state='pass' AND reason='') OR (state='fail' AND reason<>'')),
  CHECK(name<>'overall_robustness' OR description='evidence_only_not_promotion_or_deployment_authority')
);

CREATE FUNCTION validate_robustness_policy_graph() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'policy_id')::UUID);
  PERFORM 1 FROM robustness_policy_artifacts p WHERE p.id=target AND p.perturbation_count=(SELECT count(*) FROM robustness_policy_perturbations WHERE policy_id=p.id) AND
    (SELECT min(sequence)=0 AND max(sequence)=p.perturbation_count-1 FROM robustness_policy_perturbations WHERE policy_id=p.id) AND
    p.canonical_json=jsonb_build_object('schema',p.schema_name,'version',p.version,'fold_count',p.fold_count,'purge_seconds',p.purge_seconds,
      'embargo_seconds',p.embargo_seconds,'bootstrap_algorithm',p.bootstrap_algorithm,'bootstrap_seed',p.bootstrap_seed,
      'bootstrap_iterations',p.bootstrap_iterations,'confidence_level',p.confidence_level,'family_wise_alpha',p.family_wise_alpha,
      'multiple_testing_correction',p.multiple_testing_correction,'max_largest_positive_share',p.max_largest_positive_share,
      'max_top_decile_positive_share',p.max_top_decile_positive_share,'max_perturbation_degradation',p.max_perturbation_degradation,
      'required_perturbations',(SELECT jsonb_agg(kind ORDER BY sequence) FROM robustness_policy_perturbations WHERE policy_id=p.id),'decimal_scale',p.decimal_scale);
  IF NOT FOUND THEN RAISE EXCEPTION 'robustness policy graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_robustness_family_graph() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'family_id')::UUID);
  PERFORM 1 FROM robustness_search_families f WHERE f.id=target AND f.candidate_count=(SELECT count(*) FROM robustness_search_family_candidates WHERE family_id=f.id) AND
    (SELECT min(sequence)=0 AND max(sequence)=f.candidate_count-1 FROM robustness_search_family_candidates WHERE family_id=f.id) AND
    f.canonical_json=jsonb_build_object('schema',f.schema_name,'name',f.name,'hypothesis_sha256',f.hypothesis_sha256,
      'candidate_version_ids',(SELECT jsonb_agg(version_id::TEXT ORDER BY sequence) FROM robustness_search_family_candidates WHERE family_id=f.id));
  IF NOT FOUND THEN RAISE EXCEPTION 'robustness search family graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_robustness_assessment_graph() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'assessment_id')::UUID);
  PERFORM 1 FROM statistical_robustness_assessments a JOIN robustness_search_families family ON family.id=a.family_id
    JOIN robustness_policy_artifacts policy ON policy.id=a.policy_id WHERE a.id=target AND a.family_sha256=family.sha256 AND a.policy_sha256=policy.sha256 AND
    a.candidate_count=family.candidate_count AND a.candidate_count=(SELECT count(*) FROM robustness_assessment_candidates WHERE assessment_id=a.id) AND
    (SELECT min(sequence)=0 AND max(sequence)=a.candidate_count-1 FROM robustness_assessment_candidates WHERE assessment_id=a.id) AND
    NOT EXISTS(SELECT 1 FROM robustness_assessment_candidates c JOIN robustness_search_family_candidates fc ON fc.family_id=a.family_id AND fc.sequence=c.sequence
      WHERE c.assessment_id=a.id AND (c.version_id<>fc.version_id OR c.fold_count<>policy.fold_count OR c.fold_count<>(SELECT count(*) FROM robustness_assessment_folds f WHERE f.assessment_id=a.id AND f.candidate_sequence=c.sequence) OR
        c.statistic_count<>(SELECT count(*) FROM robustness_statistics s WHERE s.assessment_id=a.id AND s.candidate_sequence=c.sequence) OR
        c.gate_count<>(SELECT count(*) FROM robustness_gates g WHERE g.assessment_id=a.id AND g.candidate_sequence=c.sequence))) AND
    NOT EXISTS(SELECT 1 FROM robustness_assessment_folds f JOIN robustness_assessment_candidates c ON c.assessment_id=f.assessment_id AND c.sequence=f.candidate_sequence
      WHERE f.assessment_id=a.id AND (f.sequence>=c.fold_count OR f.train_end+make_interval(secs=>policy.purge_seconds)>f.test_start OR
        f.scenario_count<>policy.perturbation_count+1 OR f.scenario_count<>(SELECT count(*) FROM robustness_assessment_scenarios s WHERE s.assessment_id=a.id AND s.candidate_sequence=f.candidate_sequence AND s.fold_sequence=f.sequence) OR
        f.sequence>0 AND EXISTS(SELECT 1 FROM robustness_assessment_folds prior WHERE prior.assessment_id=a.id AND prior.candidate_sequence=f.candidate_sequence AND prior.sequence=f.sequence-1 AND
          (prior.test_end>f.test_start OR prior.test_end+make_interval(secs=>policy.embargo_seconds)>f.train_start)))) AND
    NOT EXISTS(SELECT 1 FROM robustness_assessment_scenarios s JOIN robustness_assessment_candidates c ON c.assessment_id=s.assessment_id AND c.sequence=s.candidate_sequence
      JOIN robustness_assessment_folds f ON f.assessment_id=s.assessment_id AND f.candidate_sequence=s.candidate_sequence AND f.sequence=s.fold_sequence
      JOIN trade_portfolio_evaluations report ON report.id=s.report_id JOIN experiment_run_results result ON result.id=report.result_id
      JOIN experiment_programs program ON program.id=result.program_id WHERE s.assessment_id=a.id AND
        (s.report_sha256<>report.sha256 OR report.mode<>a.mode OR report.evaluation_start<>f.test_start OR report.evaluation_end<>f.test_end OR program.version_id<>c.version_id OR
          s.sequence>0 AND NOT EXISTS(SELECT 1 FROM robustness_policy_perturbations p WHERE p.policy_id=a.policy_id AND p.sequence=s.sequence-1 AND p.kind=s.kind))) AND
    a.canonical_json=jsonb_build_object('schema',a.schema_name,'state',a.state,'family_id',a.family_id::TEXT,'family_sha256',a.family_sha256,
      'policy_id',a.policy_id::TEXT,'policy_sha256',a.policy_sha256,'mode',a.mode,'candidates',(
        SELECT jsonb_agg(jsonb_build_object('sequence',c.sequence,'version_id',c.version_id::TEXT,'folds',(
          SELECT jsonb_agg(jsonb_build_object('sequence',f.sequence,'train_start',to_char(f.train_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'train_end',to_char(f.train_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'test_start',to_char(f.test_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'test_end',to_char(f.test_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'baseline',(
              SELECT jsonb_build_object('kind',s.kind,'severity',s.severity,'report_id',s.report_id::TEXT,'report_sha256',s.report_sha256) FROM robustness_assessment_scenarios s
              WHERE s.assessment_id=a.id AND s.candidate_sequence=c.sequence AND s.fold_sequence=f.sequence AND s.sequence=0),
            'perturbations',(SELECT jsonb_agg(jsonb_build_object('kind',s.kind,'severity',s.severity,'report_id',s.report_id::TEXT,'report_sha256',s.report_sha256) ORDER BY s.sequence)
              FROM robustness_assessment_scenarios s WHERE s.assessment_id=a.id AND s.candidate_sequence=c.sequence AND s.fold_sequence=f.sequence AND s.sequence>0)) ORDER BY f.sequence)
          FROM robustness_assessment_folds f WHERE f.assessment_id=a.id AND f.candidate_sequence=c.sequence),
          'statistics',(SELECT jsonb_agg(jsonb_build_object('name',s.name,'state',s.state,'value',s.value,'unit',s.unit,'reason',s.reason,'description',s.description) ORDER BY s.sequence)
            FROM robustness_statistics s WHERE s.assessment_id=a.id AND s.candidate_sequence=c.sequence),
          'gates',(SELECT jsonb_agg(jsonb_build_object('name',g.name,'state',g.state,'threshold',g.threshold,'observed',g.observed,'reason',g.reason,'description',g.description) ORDER BY g.sequence)
            FROM robustness_gates g WHERE g.assessment_id=a.id AND g.candidate_sequence=c.sequence)) ORDER BY c.sequence)
        FROM robustness_assessment_candidates c WHERE c.assessment_id=a.id));
  IF NOT FOUND THEN RAISE EXCEPTION 'robustness assessment graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_robustness_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'robustness evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['robustness_policy_artifacts','robustness_policy_perturbations','robustness_search_families',
  'robustness_search_family_candidates','statistical_robustness_assessments','robustness_assessment_candidates','robustness_assessment_folds',
  'robustness_assessment_scenarios','robustness_statistics','robustness_gates'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_robustness_mutation()','trg_'||name||'_immutable',name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_robustness_policy_graph AFTER INSERT ON robustness_policy_artifacts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_robustness_policy_graph();
CREATE CONSTRAINT TRIGGER trg_robustness_policy_perturbation_graph AFTER INSERT ON robustness_policy_perturbations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_robustness_policy_graph();
CREATE CONSTRAINT TRIGGER trg_robustness_family_graph AFTER INSERT ON robustness_search_families DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_robustness_family_graph();
CREATE CONSTRAINT TRIGGER trg_robustness_family_candidate_graph AFTER INSERT ON robustness_search_family_candidates DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_robustness_family_graph();
CREATE CONSTRAINT TRIGGER trg_robustness_assessment_graph AFTER INSERT ON statistical_robustness_assessments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_robustness_assessment_graph();
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['robustness_assessment_candidates','robustness_assessment_folds','robustness_assessment_scenarios','robustness_statistics','robustness_gates'] LOOP
  EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_robustness_assessment_graph()','trg_'||name||'_graph',name); END LOOP; END $$;
CREATE INDEX idx_robustness_assessments_family ON statistical_robustness_assessments(family_id,created_at,id);
CREATE INDEX idx_robustness_scenarios_report ON robustness_assessment_scenarios(report_id,assessment_id);
