-- OVR-605 additive, inactive daily supervisor evidence boundary.
-- This schema admits or halts named work classes; it grants no execution,
-- settlement, provider, scheduler, risk-control, or brake mutation authority.

CREATE TABLE daily_supervisor_policy_artifacts (
    id UUID PRIMARY KEY,
    policy_version TEXT NOT NULL UNIQUE CHECK (policy_version ~ '^daily-supervisor-policy-v1@sha256:[0-9a-f]{64}$'),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', clock_timestamp()),
    CHECK (sha256 = split_part(policy_version, '@sha256:', 2)),
    CHECK (id = economic_deterministic_uuid('daily-supervisor-policy', policy_version))
);

CREATE TABLE daily_supervisor_assessments (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'autonomous-daily-supervisor-assessment-v1'),
    operating_day DATE NOT NULL,
    timezone TEXT NOT NULL CHECK (timezone <> ''),
    evaluated_at TIMESTAMPTZ NOT NULL CHECK (evaluated_at = date_trunc('microseconds', evaluated_at)),
    policy_version TEXT NOT NULL REFERENCES daily_supervisor_policy_artifacts(policy_version) ON DELETE RESTRICT,
    reconciliation_id UUID NOT NULL REFERENCES venue_reconciliation_runs(id) ON DELETE RESTRICT,
    reconciliation_sha256 TEXT NOT NULL CHECK (reconciliation_sha256 ~ '^[0-9a-f]{64}$'),
    scheduler_occurrence_id UUID NOT NULL REFERENCES financial_job_occurrences(id) ON DELETE RESTRICT,
    scheduler_occurrence_sha256 TEXT NOT NULL CHECK (scheduler_occurrence_sha256 ~ '^[0-9a-f]{64}$'),
    scheduler_effect_id UUID NOT NULL UNIQUE REFERENCES financial_job_effect_claims(id) ON DELETE RESTRICT,
    scheduler_effect_sha256 TEXT NOT NULL CHECK (scheduler_effect_sha256 ~ '^[0-9a-f]{64}$'),
    prior_assessment_id UUID REFERENCES daily_supervisor_assessments(id) ON DELETE RESTRICT,
    prior_assessment_sha256 TEXT NOT NULL DEFAULT '' CHECK (prior_assessment_sha256 = '' OR prior_assessment_sha256 ~ '^[0-9a-f]{64}$'),
    check_count INTEGER NOT NULL CHECK (check_count = 10),
    action_count INTEGER NOT NULL CHECK (action_count = 5),
    attention_count INTEGER NOT NULL CHECK (attention_count BETWEEN 0 AND 10),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', clock_timestamp()),
    CHECK ((prior_assessment_id IS NULL) = (prior_assessment_sha256 = '')),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json->>'schema' = schema_name),
    CHECK (canonical_json->>'operating_day' = operating_day::TEXT),
    CHECK (canonical_json->>'timezone' = timezone),
    CHECK (canonical_json->>'evaluated_at' = to_char(evaluated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (canonical_json->>'policy_version' = policy_version),
    CHECK (canonical_json->>'reconciliation_id' = reconciliation_id::TEXT AND canonical_json->>'reconciliation_sha256' = reconciliation_sha256),
    CHECK (canonical_json->>'scheduler_occurrence_id' = scheduler_occurrence_id::TEXT AND canonical_json->>'scheduler_occurrence_sha256' = scheduler_occurrence_sha256),
    CHECK (canonical_json->>'scheduler_effect_id' = scheduler_effect_id::TEXT AND canonical_json->>'scheduler_effect_sha256' = scheduler_effect_sha256),
    CHECK (COALESCE(canonical_json->>'prior_assessment_id','') = COALESCE(prior_assessment_id::TEXT,'') AND canonical_json->>'prior_assessment_sha256' = prior_assessment_sha256),
    CHECK (id = economic_deterministic_uuid('autonomous-daily-supervisor-assessment', schema_name || '@sha256:' || sha256))
);
CREATE UNIQUE INDEX idx_daily_supervisor_single_successor ON daily_supervisor_assessments(prior_assessment_id) WHERE prior_assessment_id IS NOT NULL;

CREATE TABLE daily_supervisor_checks (
    assessment_id UUID NOT NULL REFERENCES daily_supervisor_assessments(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 10),
    check_name TEXT NOT NULL CHECK (check_name IN ('database','schema','ledger_projection','market_data','risk_brake','reconciliation','exposure_scheduler','exit_worker','settlement_worker','reconciliation_worker')),
    check_state TEXT NOT NULL CHECK (check_state IN ('pass','fail','unknown')),
    evidence_id UUID NOT NULL,
    evidence_sha256 TEXT NOT NULL CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$'),
    observed_at TIMESTAMPTZ NOT NULL CHECK (observed_at = date_trunc('microseconds', observed_at)),
    fresh_through TIMESTAMPTZ NOT NULL CHECK (fresh_through >= observed_at AND fresh_through = date_trunc('microseconds', fresh_through)),
    reason TEXT NOT NULL,
    PRIMARY KEY (assessment_id, sequence),
    UNIQUE (assessment_id, check_name),
    CHECK ((check_state = 'pass' AND reason = '') OR (check_state <> 'pass' AND reason <> ''))
);

CREATE TABLE daily_supervisor_actions (
    assessment_id UUID NOT NULL REFERENCES daily_supervisor_assessments(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 5),
    work_class TEXT NOT NULL CHECK (work_class IN ('new_exposure','protective_exit','settlement','reconciliation','evidence_only')),
    admission TEXT NOT NULL CHECK (admission IN ('eligible','halted')),
    PRIMARY KEY (assessment_id, sequence),
    UNIQUE (assessment_id, work_class)
);

CREATE TABLE daily_supervisor_action_blockers (
    assessment_id UUID NOT NULL,
    action_sequence INTEGER NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    check_name TEXT NOT NULL CHECK (check_name IN ('database','schema','ledger_projection','market_data','risk_brake','reconciliation','exposure_scheduler','exit_worker','settlement_worker','reconciliation_worker')),
    PRIMARY KEY (assessment_id, action_sequence, sequence),
    UNIQUE (assessment_id, action_sequence, check_name),
    FOREIGN KEY (assessment_id, action_sequence) REFERENCES daily_supervisor_actions(assessment_id, sequence) ON DELETE RESTRICT
);

CREATE TABLE daily_supervisor_attention (
    assessment_id UUID NOT NULL REFERENCES daily_supervisor_assessments(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 10),
    check_name TEXT NOT NULL,
    check_state TEXT NOT NULL CHECK (check_state IN ('fail','unknown')),
    reason TEXT NOT NULL CHECK (reason <> ''),
    evidence_id UUID NOT NULL,
    evidence_sha256 TEXT NOT NULL CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (assessment_id, sequence),
    UNIQUE (assessment_id, check_name),
    FOREIGN KEY (assessment_id, check_name) REFERENCES daily_supervisor_checks(assessment_id, check_name) ON DELETE RESTRICT
);

CREATE FUNCTION reject_daily_supervisor_mutation() RETURNS TRIGGER AS $$
BEGIN RAISE EXCEPTION 'daily supervisor evidence is append-only'; END; $$ LANGUAGE plpgsql;

CREATE FUNCTION daily_supervisor_requires(work TEXT, check_name TEXT) RETURNS BOOLEAN AS $$
  SELECT CASE work
    WHEN 'new_exposure' THEN check_name = ANY(ARRAY['database','schema','ledger_projection','market_data','risk_brake','reconciliation','exposure_scheduler'])
    WHEN 'protective_exit' THEN check_name = ANY(ARRAY['database','schema','ledger_projection','risk_brake','exit_worker'])
    WHEN 'settlement' THEN check_name = ANY(ARRAY['database','schema','ledger_projection','risk_brake','settlement_worker'])
    WHEN 'reconciliation' THEN check_name = ANY(ARRAY['database','schema','ledger_projection','reconciliation_worker'])
    WHEN 'evidence_only' THEN check_name = ANY(ARRAY['database','schema'])
    ELSE FALSE
  END;
$$ LANGUAGE SQL IMMUTABLE STRICT;

CREATE FUNCTION validate_daily_supervisor_graph() RETURNS TRIGGER AS $$
DECLARE parent_id UUID; parent daily_supervisor_assessments%ROWTYPE;
BEGIN
    IF TG_TABLE_NAME = 'daily_supervisor_assessments' THEN parent_id := NEW.id;
    ELSE parent_id := NEW.assessment_id;
    END IF;
    SELECT * INTO parent FROM daily_supervisor_assessments WHERE id = parent_id;
    IF parent.id IS NULL THEN RAISE EXCEPTION 'daily supervisor assessment is missing'; END IF;

    PERFORM 1 FROM venue_reconciliation_runs r WHERE r.id = parent.reconciliation_id
      AND r.sha256 = parent.reconciliation_sha256
      AND (((SELECT check_state FROM daily_supervisor_checks WHERE assessment_id=parent.id AND check_name='reconciliation') <> 'pass') OR r.clean);
    IF NOT FOUND THEN RAISE EXCEPTION 'daily supervisor reconciliation evidence does not match'; END IF;
    PERFORM 1 FROM financial_job_occurrences o WHERE o.id=parent.scheduler_occurrence_id AND o.sha256=parent.scheduler_occurrence_sha256 AND o.job_key='daily_supervisor';
    IF NOT FOUND THEN RAISE EXCEPTION 'daily supervisor occurrence evidence does not match'; END IF;
    PERFORM 1 FROM financial_job_effect_claims e WHERE e.id=parent.scheduler_effect_id AND e.sha256=parent.scheduler_effect_sha256 AND e.occurrence_id=parent.scheduler_occurrence_id AND e.effect_kind='supervisor_assessment';
    IF NOT FOUND THEN RAISE EXCEPTION 'daily supervisor effect evidence does not match'; END IF;
    IF parent.prior_assessment_id IS NOT NULL THEN
      PERFORM 1 FROM daily_supervisor_assessments prior WHERE prior.id=parent.prior_assessment_id AND prior.sha256=parent.prior_assessment_sha256 AND prior.operating_day=parent.operating_day AND prior.evaluated_at < parent.evaluated_at;
      IF NOT FOUND THEN RAISE EXCEPTION 'daily supervisor prior assessment does not match'; END IF;
    END IF;

    IF parent.check_count <> (SELECT count(*) FROM daily_supervisor_checks WHERE assessment_id=parent.id)
      OR parent.action_count <> (SELECT count(*) FROM daily_supervisor_actions WHERE assessment_id=parent.id)
      OR parent.attention_count <> (SELECT count(*) FROM daily_supervisor_attention WHERE assessment_id=parent.id)
      OR parent.canonical_json->'checks' <> (SELECT COALESCE(jsonb_agg(jsonb_build_object('name',check_name,'state',check_state,'evidence_id',evidence_id::TEXT,'evidence_sha256',evidence_sha256,'observed_at',to_char(observed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'fresh_through',to_char(fresh_through AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'reason',reason) ORDER BY sequence),'[]'::JSONB) FROM daily_supervisor_checks WHERE assessment_id=parent.id)
      OR parent.canonical_json->'actions' <> (SELECT COALESCE(jsonb_agg(jsonb_build_object('work_class',a.work_class,'admission',a.admission,'blocked_by',COALESCE((SELECT jsonb_agg(b.check_name ORDER BY b.sequence) FROM daily_supervisor_action_blockers b WHERE b.assessment_id=a.assessment_id AND b.action_sequence=a.sequence),'[]'::JSONB)) ORDER BY a.sequence),'[]'::JSONB) FROM daily_supervisor_actions a WHERE a.assessment_id=parent.id)
      OR parent.canonical_json->'attention' <> (SELECT COALESCE(jsonb_agg(jsonb_build_object('check',check_name,'state',check_state,'reason',reason,'evidence_id',evidence_id::TEXT,'evidence_sha256',evidence_sha256) ORDER BY sequence),'[]'::JSONB) FROM daily_supervisor_attention WHERE assessment_id=parent.id)
      OR EXISTS (SELECT 1 FROM daily_supervisor_actions a WHERE a.assessment_id=parent.id AND ((a.admission='eligible') <> NOT EXISTS (SELECT 1 FROM daily_supervisor_action_blockers b WHERE b.assessment_id=a.assessment_id AND b.action_sequence=a.sequence)))
      OR EXISTS (SELECT 1 FROM daily_supervisor_action_blockers b JOIN daily_supervisor_actions a ON (a.assessment_id,a.sequence)=(b.assessment_id,b.action_sequence) JOIN daily_supervisor_checks c ON c.assessment_id=b.assessment_id AND c.check_name=b.check_name WHERE b.assessment_id=parent.id AND (NOT daily_supervisor_requires(a.work_class,b.check_name) OR c.check_state='pass'))
      OR EXISTS (SELECT 1 FROM daily_supervisor_actions a JOIN daily_supervisor_checks c ON c.assessment_id=a.assessment_id AND daily_supervisor_requires(a.work_class,c.check_name) LEFT JOIN daily_supervisor_action_blockers b ON (b.assessment_id,b.action_sequence,b.check_name)=(a.assessment_id,a.sequence,c.check_name) WHERE a.assessment_id=parent.id AND c.check_state<>'pass' AND b.assessment_id IS NULL)
      OR EXISTS (SELECT 1 FROM daily_supervisor_attention attention LEFT JOIN daily_supervisor_checks check_row ON check_row.assessment_id=attention.assessment_id AND check_row.check_name=attention.check_name WHERE attention.assessment_id=parent.id AND (attention.check_state,attention.reason,attention.evidence_id,attention.evidence_sha256) IS DISTINCT FROM (check_row.check_state,check_row.reason,check_row.evidence_id,check_row.evidence_sha256))
      OR EXISTS (SELECT 1 FROM daily_supervisor_checks check_row LEFT JOIN daily_supervisor_attention attention ON attention.assessment_id=check_row.assessment_id AND attention.check_name=check_row.check_name WHERE check_row.assessment_id=parent.id AND check_row.check_state<>'pass' AND attention.assessment_id IS NULL)
    THEN RAISE EXCEPTION 'daily supervisor graph does not reconstruct'; END IF;
    RETURN NULL;
END; $$ LANGUAGE plpgsql;

DO $$ DECLARE table_name TEXT; BEGIN
  FOREACH table_name IN ARRAY ARRAY['daily_supervisor_policy_artifacts','daily_supervisor_assessments','daily_supervisor_checks','daily_supervisor_actions','daily_supervisor_action_blockers','daily_supervisor_attention'] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_daily_supervisor_mutation()', 'trg_'||table_name||'_immutable', table_name);
  END LOOP;
END $$;

DO $$ DECLARE table_name TEXT; BEGIN
  FOREACH table_name IN ARRAY ARRAY['daily_supervisor_assessments','daily_supervisor_checks','daily_supervisor_actions','daily_supervisor_action_blockers','daily_supervisor_attention'] LOOP
    EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_daily_supervisor_graph()', 'trg_'||table_name||'_graph', table_name);
  END LOOP;
END $$;

CREATE INDEX idx_daily_supervisor_assessment_day ON daily_supervisor_assessments(operating_day, evaluated_at DESC, id);
CREATE INDEX idx_daily_supervisor_attention_check ON daily_supervisor_attention(check_name, assessment_id);
