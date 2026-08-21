LOCK TABLE strategy_deployments,statistical_robustness_assessments,robustness_assessment_candidates,robustness_gates IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE promotion_policy_artifacts (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='promotion-policy-v1'),
  version TEXT NOT NULL CHECK(version<>'' AND version=btrim(version) AND char_length(version)<=128),
  pass_action TEXT NOT NULL CHECK(pass_action='shadow'), failure_action TEXT NOT NULL CHECK(failure_action IN ('hold','retire')),
  required_gate_count INTEGER NOT NULL CHECK(required_gate_count BETWEEN 1 AND 64),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('promotion-policy',schema_name||'@sha256:'||sha256))
);

CREATE TABLE promotion_policy_required_gates (
  policy_id UUID NOT NULL REFERENCES promotion_policy_artifacts(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence>=0), name TEXT NOT NULL CHECK(name<>'' AND name=lower(btrim(name)) AND char_length(name)<=128),
  PRIMARY KEY(policy_id,sequence), UNIQUE(policy_id,name)
);

CREATE TABLE promotion_retirement_decisions (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='promotion-retirement-decision-v1'),
  deployment_id UUID NOT NULL REFERENCES strategy_deployments(id) ON DELETE RESTRICT,
  deployment_sha256 TEXT NOT NULL CHECK(deployment_sha256 ~ '^[0-9a-f]{64}$'), version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT,
  assessment_id UUID NOT NULL REFERENCES statistical_robustness_assessments(id) ON DELETE RESTRICT,
  assessment_sha256 TEXT NOT NULL CHECK(assessment_sha256 ~ '^[0-9a-f]{64}$'),
  family_id UUID NOT NULL REFERENCES robustness_search_families(id) ON DELETE RESTRICT,
  robustness_policy_id UUID NOT NULL REFERENCES robustness_policy_artifacts(id) ON DELETE RESTRICT,
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  policy_id UUID NOT NULL REFERENCES promotion_policy_artifacts(id) ON DELETE RESTRICT,
  policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  prior_decision_id UUID REFERENCES promotion_retirement_decisions(id) ON DELETE RESTRICT,
  prior_decision_sha256 TEXT NOT NULL DEFAULT '' CHECK(prior_decision_sha256='' OR prior_decision_sha256 ~ '^[0-9a-f]{64}$'),
  candidate_sequence INTEGER NOT NULL CHECK(candidate_sequence>=0), prior_state TEXT NOT NULL CHECK(prior_state IN ('proposed','shadow','retired')),
  next_state TEXT NOT NULL CHECK(next_state IN ('proposed','shadow','retired')), outcome TEXT NOT NULL CHECK(outcome IN ('approved','held','retired')),
  reason TEXT NOT NULL CHECK(reason IN ('all_required_robustness_gates_passed','required_gate_failed_or_transition_not_available','required_robustness_gate_failed')),
  observed_gate_count INTEGER NOT NULL CHECK(observed_gate_count BETWEEN 1 AND 64),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK((prior_decision_id IS NULL AND prior_decision_sha256='') OR (prior_decision_id IS NOT NULL AND prior_decision_sha256<>'')),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('promotion-retirement-decision',schema_name||'@sha256:'||sha256))
);
CREATE UNIQUE INDEX uq_promotion_decision_serial_head ON promotion_retirement_decisions(deployment_id,COALESCE(prior_decision_id,'00000000-0000-0000-0000-000000000000'::UUID));

CREATE TABLE promotion_decision_observed_gates (
  decision_id UUID NOT NULL REFERENCES promotion_retirement_decisions(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence>=0), assessment_id UUID NOT NULL,
  candidate_sequence INTEGER NOT NULL CHECK(candidate_sequence>=0), name TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('pass','fail')), threshold TEXT NOT NULL, observed TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '', description TEXT NOT NULL,
  PRIMARY KEY(decision_id,sequence), UNIQUE(decision_id,name),
  FOREIGN KEY(assessment_id,candidate_sequence,name) REFERENCES robustness_gates(assessment_id,candidate_sequence,name) ON DELETE RESTRICT,
  CHECK((state='pass' AND reason='') OR (state='fail' AND reason<>''))
);

CREATE TABLE deployment_promotion_lifecycle_events (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='deployment-promotion-lifecycle-event-v1'),
  deployment_id UUID NOT NULL REFERENCES strategy_deployments(id) ON DELETE RESTRICT,
  decision_id UUID NOT NULL UNIQUE REFERENCES promotion_retirement_decisions(id) ON DELETE RESTRICT,
  prior_state TEXT NOT NULL CHECK(prior_state IN ('proposed','shadow','retired')),
  next_state TEXT NOT NULL CHECK(next_state IN ('proposed','shadow','retired')),
  outcome TEXT NOT NULL CHECK(outcome IN ('approved','held','retired')),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('deployment-promotion-lifecycle-event',schema_name||'@sha256:'||sha256))
);

CREATE FUNCTION validate_promotion_policy_graph() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'policy_id')::UUID);
  PERFORM 1 FROM promotion_policy_artifacts p WHERE p.id=target AND
    p.required_gate_count=(SELECT count(*) FROM promotion_policy_required_gates g WHERE g.policy_id=p.id) AND
    (SELECT min(sequence)=0 AND max(sequence)=p.required_gate_count-1 FROM promotion_policy_required_gates g WHERE g.policy_id=p.id) AND
    EXISTS(SELECT 1 FROM promotion_policy_required_gates g WHERE g.policy_id=p.id AND g.name='overall_robustness') AND
    p.canonical_json=jsonb_build_object('schema',p.schema_name,'version',p.version,'required_gates',(
      SELECT jsonb_agg(g.name ORDER BY g.sequence) FROM promotion_policy_required_gates g WHERE g.policy_id=p.id),
      'pass_action',p.pass_action,'failure_action',p.failure_action);
  IF NOT FOUND THEN RAISE EXCEPTION 'promotion policy graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_promotion_decision_graph() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'decision_id')::UUID);
  PERFORM 1 FROM promotion_retirement_decisions d
    JOIN strategy_deployments deployment ON deployment.id=d.deployment_id
    JOIN statistical_robustness_assessments assessment ON assessment.id=d.assessment_id
    JOIN robustness_assessment_candidates candidate ON candidate.assessment_id=d.assessment_id AND candidate.sequence=d.candidate_sequence
    JOIN promotion_policy_artifacts policy ON policy.id=d.policy_id
    LEFT JOIN promotion_retirement_decisions prior ON prior.id=d.prior_decision_id
    WHERE d.id=target AND d.deployment_sha256=deployment.sha256 AND deployment.state='proposed' AND d.version_id=deployment.version_id AND
      d.assessment_sha256=assessment.sha256 AND d.family_id=assessment.family_id AND d.robustness_policy_id=assessment.policy_id AND
      d.mode=deployment.mode AND d.mode=assessment.mode AND candidate.version_id=d.version_id AND d.policy_sha256=policy.sha256 AND
      ((d.prior_decision_id IS NULL AND d.prior_state=deployment.state) OR
       (d.prior_decision_id IS NOT NULL AND prior.deployment_id=d.deployment_id AND d.prior_decision_sha256=prior.sha256 AND d.prior_state=prior.next_state)) AND
      d.observed_gate_count=policy.required_gate_count AND d.observed_gate_count=(SELECT count(*) FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id) AND
      (SELECT min(sequence)=0 AND max(sequence)=d.observed_gate_count-1 FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id) AND
      NOT EXISTS(SELECT 1 FROM promotion_decision_observed_gates observed_gate
        JOIN promotion_policy_required_gates required_gate ON required_gate.policy_id=d.policy_id AND required_gate.sequence=observed_gate.sequence
        JOIN robustness_gates source_gate ON source_gate.assessment_id=d.assessment_id AND source_gate.candidate_sequence=d.candidate_sequence AND source_gate.name=observed_gate.name
        WHERE observed_gate.decision_id=d.id AND (observed_gate.assessment_id<>d.assessment_id OR observed_gate.candidate_sequence<>d.candidate_sequence OR
          observed_gate.name<>required_gate.name OR observed_gate.state<>source_gate.state OR observed_gate.threshold<>source_gate.threshold OR
          observed_gate.observed<>source_gate.observed OR observed_gate.reason<>source_gate.reason OR observed_gate.description<>source_gate.description)) AND
      ((d.prior_state='proposed' AND NOT EXISTS(SELECT 1 FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id AND g.state<>'pass') AND
          d.outcome='approved' AND d.next_state='shadow' AND d.reason='all_required_robustness_gates_passed') OR
       (EXISTS(SELECT 1 FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id AND g.state<>'pass') AND policy.failure_action='retire' AND
          d.outcome='retired' AND d.next_state='retired' AND d.reason='required_robustness_gate_failed') OR
       ((d.prior_state<>'proposed' OR EXISTS(SELECT 1 FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id AND g.state<>'pass')) AND
          (policy.failure_action='hold' OR NOT EXISTS(SELECT 1 FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id AND g.state<>'pass')) AND
          d.outcome='held' AND d.next_state=d.prior_state AND d.reason='required_gate_failed_or_transition_not_available')) AND
      d.canonical_json=jsonb_build_object('schema',d.schema_name,'deployment_id',d.deployment_id::TEXT,'deployment_sha256',d.deployment_sha256,
        'version_id',d.version_id::TEXT,'assessment_id',d.assessment_id::TEXT,'assessment_sha256',d.assessment_sha256,'family_id',d.family_id::TEXT,
        'robustness_policy_id',d.robustness_policy_id::TEXT,'mode',d.mode,'policy_id',d.policy_id::TEXT,'policy_sha256',d.policy_sha256,
        'prior_decision_id',COALESCE(d.prior_decision_id::TEXT,''),'prior_decision_sha256',d.prior_decision_sha256,'prior_state',d.prior_state,
        'next_state',d.next_state,'outcome',d.outcome,'reason',d.reason,'observed_gates',(
          SELECT jsonb_agg(jsonb_build_object('name',g.name,'state',g.state,'threshold',g.threshold,'observed',g.observed,'reason',g.reason,'description',g.description) ORDER BY g.sequence)
          FROM promotion_decision_observed_gates g WHERE g.decision_id=d.id));
  IF NOT FOUND THEN RAISE EXCEPTION 'promotion decision graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_deployment_promotion_event() RETURNS TRIGGER AS $$
BEGIN
  PERFORM 1 FROM deployment_promotion_lifecycle_events event JOIN promotion_retirement_decisions decision ON decision.id=event.decision_id
    WHERE event.id=NEW.id AND event.deployment_id=decision.deployment_id AND event.prior_state=decision.prior_state AND
      event.next_state=decision.next_state AND event.outcome=decision.outcome AND
      event.canonical_json=jsonb_build_object('schema',event.schema_name,'deployment_id',event.deployment_id::TEXT,
        'decision_id',event.decision_id::TEXT,'prior_state',event.prior_state,'next_state',event.next_state,'outcome',event.outcome);
  IF NOT FOUND THEN RAISE EXCEPTION 'deployment promotion lifecycle event does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_promotion_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'promotion evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['promotion_policy_artifacts','promotion_policy_required_gates','promotion_retirement_decisions','promotion_decision_observed_gates','deployment_promotion_lifecycle_events'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_promotion_mutation()','trg_'||name||'_immutable',name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_promotion_policy_graph AFTER INSERT ON promotion_policy_artifacts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_promotion_policy_graph();
CREATE CONSTRAINT TRIGGER trg_promotion_policy_gate_graph AFTER INSERT ON promotion_policy_required_gates DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_promotion_policy_graph();
CREATE CONSTRAINT TRIGGER trg_promotion_decision_graph AFTER INSERT ON promotion_retirement_decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_promotion_decision_graph();
CREATE CONSTRAINT TRIGGER trg_promotion_observed_gate_graph AFTER INSERT ON promotion_decision_observed_gates DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_promotion_decision_graph();
CREATE CONSTRAINT TRIGGER trg_deployment_promotion_event_graph AFTER INSERT ON deployment_promotion_lifecycle_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_deployment_promotion_event();
CREATE INDEX idx_promotion_decisions_deployment ON promotion_retirement_decisions(deployment_id,created_at,id);
CREATE INDEX idx_promotion_decisions_version ON promotion_retirement_decisions(version_id,created_at,id);
CREATE INDEX idx_promotion_decisions_assessment ON promotion_retirement_decisions(assessment_id,created_at,id);
CREATE INDEX idx_promotion_decisions_family ON promotion_retirement_decisions(family_id,created_at,id);
