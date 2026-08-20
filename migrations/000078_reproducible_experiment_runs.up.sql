LOCK TABLE research_experiments,strategy_versions,dataset_manifests,dataset_manifest_partitions,
  dataset_manifest_observations,dataset_quality_results,accounts,account_capital_policy_bindings,
  simulation_policy_artifacts,capital_margin_policy_artifacts,execution_intents,execution_orders,
  execution_lifecycle_events,execution_fills IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION experiment_program_identity(version_id_value TEXT,version_sha_value TEXT,compiler_kind_value TEXT,
  compiler_version_value TEXT,source_commit_value TEXT,source_tree_sha_value TEXT,decision_contract_value TEXT,
  adapter_kind_value TEXT,adapter_version_value TEXT,adapter_sha_value TEXT,runner_contract_value TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"experiment-program-v1","version_id":'||dataset_json_string(version_id_value)||
    ',"version_sha256":'||dataset_json_string(version_sha_value)||',"compiler_kind":'||dataset_json_string(compiler_kind_value)||
    ',"compiler_version":'||dataset_json_string(compiler_version_value)||',"source_commit":'||dataset_json_string(source_commit_value)||
    ',"source_tree_sha256":'||dataset_json_string(source_tree_sha_value)||',"decision_contract":'||dataset_json_string(decision_contract_value)||
    ',"adapter_kind":'||dataset_json_string(adapter_kind_value)||',"adapter_version":'||dataset_json_string(adapter_version_value)||
    ',"adapter_sha256":'||dataset_json_string(adapter_sha_value)||',"runner_contract":'||dataset_json_string(runner_contract_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION experiment_plan_intent_identity(instrument_id_value TEXT,contract_id_value TEXT,side_value TEXT,order_type_value TEXT,
  tif_value TEXT,quantity_value TEXT,limit_value TEXT,stop_value TEXT,decision_at_value TEXT,route_at_value TEXT) RETURNS TEXT AS $$
  SELECT '{"instrument_id":'||dataset_json_string(instrument_id_value)||',"venue_contract_id":'||dataset_json_string(contract_id_value)||
    ',"side":'||dataset_json_string(side_value)||',"order_type":'||dataset_json_string(order_type_value)||
    ',"time_in_force":'||dataset_json_string(tif_value)||',"quantity":'||dataset_json_string(quantity_value)||
    ',"limit_price":'||CASE WHEN limit_value IS NULL THEN 'null' ELSE dataset_json_string(limit_value) END||
    ',"stop_price":'||CASE WHEN stop_value IS NULL THEN 'null' ELSE dataset_json_string(stop_value) END||
    ',"decision_at":'||dataset_json_string(decision_at_value)||',"route_at":'||dataset_json_string(route_at_value)||'}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION experiment_plan_step_identity(sequence_value INTEGER,partition_sha_value TEXT,source_key_value TEXT,observation_sha_value TEXT,
  available_at_value TEXT,decision_text TEXT,action_value TEXT,rejection_value TEXT,intent_text TEXT) RETURNS TEXT AS $$
  SELECT '{"sequence":'||sequence_value::TEXT||',"partition_content_sha256":'||dataset_json_string(partition_sha_value)||
    ',"observation_source_key":'||dataset_json_string(source_key_value)||',"observation_content_sha256":'||dataset_json_string(observation_sha_value)||
    ',"available_at":'||dataset_json_string(available_at_value)||',"decision":'||decision_text||
    ',"action":'||dataset_json_string(action_value)||',"rejection_code":'||dataset_json_string(rejection_value)||
    ',"intent":'||COALESCE(intent_text,'null')||'}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION experiment_plan_identity(experiment_id_value TEXT,program_id_value TEXT,account_id_value TEXT,manifest_id_value TEXT,
  manifest_sha_value TEXT,start_value TEXT,end_value TEXT,seed_value BIGINT,mode_value TEXT,steps_text TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"experiment-replay-plan-v1","experiment_id":'||dataset_json_string(experiment_id_value)||
    ',"program_id":'||dataset_json_string(program_id_value)||',"account_id":'||dataset_json_string(account_id_value)||
    ',"manifest_id":'||dataset_json_string(manifest_id_value)||',"manifest_sha256":'||dataset_json_string(manifest_sha_value)||
    ',"evaluation_start":'||dataset_json_string(start_value)||',"evaluation_end":'||dataset_json_string(end_value)||
    ',"seed":'||seed_value::TEXT||',"mode":'||dataset_json_string(mode_value)||',"steps":'||steps_text||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION experiment_attempt_event_identity(attempt_id_value TEXT,sequence_value INTEGER,type_value TEXT,occurred_at_value TEXT,
  result_id_value TEXT,error_code_value TEXT,error_sha_value TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"experiment-attempt-event-v1","attempt_id":'||dataset_json_string(attempt_id_value)||
    ',"sequence":'||sequence_value::TEXT||',"type":'||dataset_json_string(type_value)||
    ',"occurred_at":'||dataset_json_string(occurred_at_value)||',"result_id":'||dataset_json_string(COALESCE(result_id_value,''))||
    ',"error_code":'||dataset_json_string(error_code_value)||',"error_sha256":'||dataset_json_string(error_sha_value)||'}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION experiment_step_outcome_identity(sequence_value INTEGER,action_value TEXT,decision_sha_value TEXT,intent_id_value TEXT,
  order_id_value TEXT,transition_ids_text TEXT,fill_ids_text TEXT,quantity_value TEXT,fee_value TEXT,aggregate_sha_value TEXT,
  outcome_sha_value TEXT) RETURNS TEXT AS $$
  SELECT '{"sequence":'||sequence_value::TEXT||',"action":'||dataset_json_string(action_value)||
    ',"decision_sha256":'||dataset_json_string(decision_sha_value)||',"intent_id":'||dataset_json_string(COALESCE(intent_id_value,''))||
    ',"order_id":'||dataset_json_string(COALESCE(order_id_value,''))||',"transition_ids":'||transition_ids_text||
    ',"fill_ids":'||fill_ids_text||',"filled_quantity":'||dataset_json_string(quantity_value)||
    ',"fee_total":'||dataset_json_string(fee_value)||',"aggregate_sha256":'||dataset_json_string(aggregate_sha_value)||
    ',"outcome_sha256":'||dataset_json_string(outcome_sha_value)||'}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION experiment_metrics_identity(step_count_value INTEGER,noop_count_value INTEGER,rejected_count_value INTEGER,
  intent_count_value INTEGER,order_count_value INTEGER,transition_count_value INTEGER,fill_count_value INTEGER,
  quantity_value TEXT,fee_value TEXT) RETURNS TEXT AS $$
  SELECT '{"step_count":'||step_count_value::TEXT||',"noop_count":'||noop_count_value::TEXT||
    ',"rejected_count":'||rejected_count_value::TEXT||',"intent_count":'||intent_count_value::TEXT||
    ',"order_count":'||order_count_value::TEXT||',"transition_count":'||transition_count_value::TEXT||
    ',"fill_count":'||fill_count_value::TEXT||',"filled_quantity":'||dataset_json_string(quantity_value)||
    ',"fee_total":'||dataset_json_string(fee_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION experiment_result_identity(experiment_id_value TEXT,program_id_value TEXT,plan_id_value TEXT,account_id_value TEXT,
  manifest_id_value TEXT,quality_id_value TEXT,simulation_version_value TEXT,capital_version_value TEXT,mode_value TEXT,
  metrics_text TEXT,outcomes_text TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"experiment-run-result-v1","state":"completed","experiment_id":'||dataset_json_string(experiment_id_value)||
    ',"program_id":'||dataset_json_string(program_id_value)||',"plan_id":'||dataset_json_string(plan_id_value)||
    ',"account_id":'||dataset_json_string(account_id_value)||',"manifest_id":'||dataset_json_string(manifest_id_value)||
    ',"quality_result_id":'||dataset_json_string(quality_id_value)||',"simulation_policy_version":'||dataset_json_string(simulation_version_value)||
    ',"capital_policy_version":'||dataset_json_string(capital_version_value)||',"mode":'||dataset_json_string(mode_value)||
    ',"metrics":'||metrics_text||',"outcomes":'||outcomes_text||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE TABLE experiment_programs (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='experiment-program-v1'),
  version_id UUID NOT NULL REFERENCES strategy_versions(id) ON DELETE RESTRICT,
  version_sha256 TEXT NOT NULL CHECK(version_sha256 ~ '^[0-9a-f]{64}$'),
  compiler_kind TEXT NOT NULL CHECK(compiler_kind<>'' AND compiler_kind=btrim(compiler_kind) AND char_length(compiler_kind)<=128),
  compiler_version TEXT NOT NULL CHECK(compiler_version<>'' AND compiler_version=btrim(compiler_version) AND char_length(compiler_version)<=256),
  source_commit TEXT NOT NULL CHECK(source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'), source_tree_sha256 TEXT NOT NULL CHECK(source_tree_sha256 ~ '^[0-9a-f]{64}$'),
  decision_contract TEXT NOT NULL CHECK(decision_contract<>'' AND decision_contract=btrim(decision_contract) AND char_length(decision_contract)<=256),
  adapter_kind TEXT NOT NULL CHECK(adapter_kind<>'' AND adapter_kind=btrim(adapter_kind) AND char_length(adapter_kind)<=128),
  adapter_version TEXT NOT NULL CHECK(adapter_version<>'' AND adapter_version=btrim(adapter_version) AND char_length(adapter_version)<=256),
  adapter_sha256 TEXT NOT NULL CHECK(adapter_sha256 ~ '^[0-9a-f]{64}$'),
  runner_contract TEXT NOT NULL CHECK(runner_contract='experiment-runner-v1'),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('experiment-program',schema_name||'@sha256:'||sha256)),
  CHECK(convert_from(canonical_bytes,'UTF8')=experiment_program_identity(version_id::TEXT,version_sha256,compiler_kind,compiler_version,
    source_commit,source_tree_sha256,decision_contract,adapter_kind,adapter_version,adapter_sha256,runner_contract))
);

CREATE TABLE experiment_replay_plans (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='experiment-replay-plan-v1'),
  experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  program_id UUID NOT NULL REFERENCES experiment_programs(id) ON DELETE RESTRICT,
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT, manifest_sha256 TEXT NOT NULL CHECK(manifest_sha256 ~ '^[0-9a-f]{64}$'),
  evaluation_start TIMESTAMPTZ NOT NULL CHECK(evaluation_start=date_trunc('microseconds',evaluation_start)),
  evaluation_end TIMESTAMPTZ NOT NULL CHECK(evaluation_end=date_trunc('microseconds',evaluation_end) AND evaluation_end>evaluation_start),
  seed BIGINT NOT NULL, mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')), step_count INTEGER NOT NULL CHECK(step_count>0 AND step_count<=100000),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('experiment-replay-plan',schema_name||'@sha256:'||sha256))
);

CREATE TABLE experiment_replay_plan_steps (
  plan_id UUID NOT NULL REFERENCES experiment_replay_plans(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  partition_content_sha256 TEXT NOT NULL CHECK(partition_content_sha256 ~ '^[0-9a-f]{64}$'),
  observation_source_key TEXT NOT NULL CHECK(observation_source_key<>'' AND observation_source_key=btrim(observation_source_key) AND char_length(observation_source_key)<=512),
  observation_content_sha256 TEXT NOT NULL CHECK(observation_content_sha256 ~ '^[0-9a-f]{64}$'),
  available_at TIMESTAMPTZ NOT NULL CHECK(available_at=date_trunc('microseconds',available_at)),
  decision_bytes BYTEA NOT NULL CHECK(octet_length(decision_bytes)>1 AND octet_length(decision_bytes)<=1048576),
  decision JSONB NOT NULL CHECK(jsonb_typeof(decision)='object'), decision_sha256 TEXT NOT NULL CHECK(decision_sha256 ~ '^[0-9a-f]{64}$'),
  action TEXT NOT NULL CHECK(action IN ('noop','rejected','execute')), rejection_code TEXT NOT NULL DEFAULT '',
  instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT, venue_contract_id UUID REFERENCES venue_contracts(id) ON DELETE RESTRICT,
  side TEXT, order_type TEXT, time_in_force TEXT, quantity NUMERIC(38,12), limit_price NUMERIC(38,12), stop_price NUMERIC(38,12),
  decision_at TIMESTAMPTZ, route_at TIMESTAMPTZ, intent_idempotency_key TEXT, order_idempotency_key TEXT, intent_id UUID, order_id UUID,
  PRIMARY KEY(plan_id,sequence), UNIQUE(plan_id,partition_content_sha256,observation_source_key,observation_content_sha256),
  CHECK(decision=convert_from(decision_bytes,'UTF8')::JSONB), CHECK(convert_from(decision_bytes,'UTF8')=strategy_canonical_json(decision)),
  CHECK(decision_sha256=encode(digest(decision_bytes,'sha256'),'hex')),
  CHECK((action='noop' AND rejection_code='' AND num_nonnulls(instrument_id,venue_contract_id,side,order_type,time_in_force,quantity,limit_price,stop_price,
      decision_at,route_at,intent_idempotency_key,order_idempotency_key,intent_id,order_id)=0) OR
    (action='rejected' AND rejection_code<>'' AND rejection_code=btrim(rejection_code) AND char_length(rejection_code)<=128 AND
      num_nonnulls(instrument_id,venue_contract_id,side,order_type,time_in_force,quantity,limit_price,stop_price,
        decision_at,route_at,intent_idempotency_key,order_idempotency_key,intent_id,order_id)=0) OR
    (action='execute' AND rejection_code='' AND instrument_id IS NOT NULL AND venue_contract_id IS NOT NULL AND side IN ('buy','sell') AND
      order_type IN ('market','limit') AND time_in_force IN ('day','gtc','ioc','fok') AND quantity>0 AND decision_at IS NOT NULL AND route_at IS NOT NULL AND
      decision_at=date_trunc('microseconds',decision_at) AND route_at=date_trunc('microseconds',route_at) AND route_at>=decision_at AND
      ((order_type='market' AND limit_price IS NULL) OR (order_type='limit' AND limit_price>0)) AND stop_price IS NULL AND
      intent_idempotency_key IS NOT NULL AND order_idempotency_key IS NOT NULL AND intent_id IS NOT NULL AND order_id IS NOT NULL))
);

CREATE TABLE experiment_run_attempts (
  id UUID PRIMARY KEY, experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at))
);

CREATE TABLE experiment_run_results (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='experiment-run-result-v1'), state TEXT NOT NULL CHECK(state='completed'),
  experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  program_id UUID NOT NULL REFERENCES experiment_programs(id) ON DELETE RESTRICT, plan_id UUID NOT NULL REFERENCES experiment_replay_plans(id) ON DELETE RESTRICT,
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT, manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
  quality_result_id UUID NOT NULL REFERENCES dataset_quality_results(id) ON DELETE RESTRICT,
  simulation_policy_version TEXT NOT NULL REFERENCES simulation_policy_artifacts(policy_version) ON DELETE RESTRICT,
  capital_policy_version TEXT NOT NULL REFERENCES capital_margin_policy_artifacts(policy_version) ON DELETE RESTRICT,
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  step_count INTEGER NOT NULL CHECK(step_count>0), noop_count INTEGER NOT NULL CHECK(noop_count>=0), rejected_count INTEGER NOT NULL CHECK(rejected_count>=0),
  intent_count INTEGER NOT NULL CHECK(intent_count>=0), order_count INTEGER NOT NULL CHECK(order_count>=0),
  transition_count INTEGER NOT NULL CHECK(transition_count>=0), fill_count INTEGER NOT NULL CHECK(fill_count>=0),
  filled_quantity NUMERIC(38,12) NOT NULL CHECK(filled_quantity>=0), fee_total NUMERIC(38,12) NOT NULL CHECK(fee_total>=0),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('experiment-run-result',schema_name||'@sha256:'||sha256)), UNIQUE(experiment_id,id)
);

CREATE TABLE experiment_run_step_outcomes (
  result_id UUID NOT NULL REFERENCES experiment_run_results(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  action TEXT NOT NULL CHECK(action IN ('noop','rejected','execute')), decision_sha256 TEXT NOT NULL CHECK(decision_sha256 ~ '^[0-9a-f]{64}$'),
  intent_id UUID REFERENCES execution_intents(id) ON DELETE RESTRICT, order_id UUID REFERENCES execution_orders(id) ON DELETE RESTRICT,
  transition_count INTEGER NOT NULL CHECK(transition_count>=0), fill_count INTEGER NOT NULL CHECK(fill_count>=0),
  filled_quantity NUMERIC(38,12) NOT NULL CHECK(filled_quantity>=0), fee_total NUMERIC(38,12) NOT NULL CHECK(fee_total>=0),
  aggregate_sha256 TEXT NOT NULL, outcome_sha256 TEXT NOT NULL,
  PRIMARY KEY(result_id,sequence),
  CHECK((action IN ('noop','rejected') AND intent_id IS NULL AND order_id IS NULL AND transition_count=0 AND fill_count=0 AND
    filled_quantity=0 AND fee_total=0 AND aggregate_sha256='' AND outcome_sha256='') OR
    (action='execute' AND intent_id IS NOT NULL AND order_id IS NOT NULL AND aggregate_sha256 ~ '^[0-9a-f]{64}$' AND outcome_sha256 ~ '^[0-9a-f]{64}$'))
);

CREATE TABLE experiment_run_transition_ids (
  result_id UUID NOT NULL, step_sequence INTEGER NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>=0), transition_id UUID NOT NULL REFERENCES execution_lifecycle_events(id) ON DELETE RESTRICT,
  PRIMARY KEY(result_id,step_sequence,sequence), UNIQUE(result_id,step_sequence,transition_id),
  FOREIGN KEY(result_id,step_sequence) REFERENCES experiment_run_step_outcomes(result_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE experiment_run_fill_ids (
  result_id UUID NOT NULL, step_sequence INTEGER NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>=0), fill_id UUID NOT NULL REFERENCES execution_fills(id) ON DELETE RESTRICT,
  PRIMARY KEY(result_id,step_sequence,sequence), UNIQUE(result_id,step_sequence,fill_id),
  FOREIGN KEY(result_id,step_sequence) REFERENCES experiment_run_step_outcomes(result_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE experiment_run_attempt_events (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='experiment-attempt-event-v1'),
  attempt_id UUID NOT NULL REFERENCES experiment_run_attempts(id) ON DELETE RESTRICT, experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence IN (0,1)), type TEXT NOT NULL CHECK(type IN ('started','completed','failed')),
  occurred_at TIMESTAMPTZ NOT NULL CHECK(occurred_at=date_trunc('microseconds',occurred_at)),
  result_id UUID, error_code TEXT NOT NULL DEFAULT '', error_sha256 TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  UNIQUE(attempt_id,sequence),
  FOREIGN KEY(experiment_id,result_id) REFERENCES experiment_run_results(experiment_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK((type='started' AND sequence=0 AND result_id IS NULL AND error_code='' AND error_sha256='') OR
    (type='completed' AND sequence=1 AND result_id IS NOT NULL AND error_code='' AND error_sha256='') OR
    (type='failed' AND sequence=1 AND result_id IS NULL AND error_code<>'' AND error_code=btrim(error_code) AND char_length(error_code)<=128 AND error_sha256 ~ '^[0-9a-f]{64}$')),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('experiment-attempt-event',attempt_id::TEXT,sequence::TEXT,type,sha256)),
  CHECK(convert_from(canonical_bytes,'UTF8')=experiment_attempt_event_identity(attempt_id::TEXT,sequence,type,
    to_char(occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),result_id::TEXT,error_code,error_sha256))
);

CREATE FUNCTION validate_experiment_program() RETURNS TRIGGER AS $$
BEGIN
  PERFORM 1 FROM experiment_programs p JOIN strategy_versions v ON v.id=p.version_id WHERE p.id=NEW.id AND
    p.version_sha256=v.sha256 AND p.compiler_kind=v.compiler_kind AND p.compiler_version=v.compiler_version AND p.source_commit=v.source_commit AND
    p.source_tree_sha256=v.source_tree_sha256 AND p.decision_contract=v.decision_contract;
  IF NOT FOUND THEN RAISE EXCEPTION 'experiment program does not match strategy version'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_experiment_plan_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; steps_text TEXT;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'plan_id')::UUID);
  SELECT '['||COALESCE(string_agg(experiment_plan_step_identity(s.sequence,s.partition_content_sha256,s.observation_source_key,
    s.observation_content_sha256,to_char(s.available_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    convert_from(s.decision_bytes,'UTF8'),s.action,s.rejection_code,
    CASE WHEN s.action='execute' THEN experiment_plan_intent_identity(s.instrument_id::TEXT,s.venue_contract_id::TEXT,s.side,s.order_type,s.time_in_force,
      trim_scale(s.quantity)::TEXT,CASE WHEN s.limit_price IS NULL THEN NULL ELSE trim_scale(s.limit_price)::TEXT END,
      CASE WHEN s.stop_price IS NULL THEN NULL ELSE trim_scale(s.stop_price)::TEXT END,
      to_char(s.decision_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(s.route_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) END),',' ORDER BY s.sequence),'')||']'
    INTO steps_text FROM experiment_replay_plan_steps s WHERE s.plan_id=target;
  PERFORM 1 FROM experiment_replay_plans p JOIN research_experiments e ON e.id=p.experiment_id
    JOIN experiment_programs program ON program.id=p.program_id AND program.version_id=e.version_id
    JOIN dataset_manifests m ON m.id=p.manifest_id
    WHERE p.id=target AND p.account_id=e.account_id AND p.manifest_id=e.manifest_id AND p.manifest_sha256=m.sha256 AND
      p.evaluation_start=e.evaluation_start AND p.evaluation_end=e.evaluation_end AND p.seed=e.seed AND p.mode=e.mode AND
      p.step_count=(SELECT count(*) FROM experiment_replay_plan_steps WHERE plan_id=p.id) AND
      (SELECT min(sequence)=0 AND max(sequence)=p.step_count-1 FROM experiment_replay_plan_steps WHERE plan_id=p.id) AND
      NOT EXISTS(SELECT 1 FROM experiment_replay_plan_steps s WHERE s.plan_id=p.id AND (s.available_at<p.evaluation_start OR s.available_at>p.evaluation_end OR
        NOT EXISTS(SELECT 1 FROM dataset_manifest_partitions part JOIN dataset_manifest_observations observation
          ON observation.manifest_id=part.manifest_id AND observation.partition_sequence=part.sequence
          WHERE part.manifest_id=p.manifest_id AND part.content_sha256=s.partition_content_sha256 AND observation.source_key=s.observation_source_key AND
            observation.content_sha256=s.observation_content_sha256 AND observation.available_at=s.available_at) OR
        s.action='execute' AND (s.decision_at<p.evaluation_start OR s.route_at>p.evaluation_end OR
          s.intent_idempotency_key<>'experiment/'||p.id::TEXT||'/step/'||s.sequence::TEXT||'/decision/'||s.decision_sha256 OR
          s.order_idempotency_key<>'experiment/'||p.id::TEXT||'/step/'||s.sequence::TEXT||'/order' OR
          s.intent_id<>economic_deterministic_uuid('execution-intent',p.account_id::TEXT,s.intent_idempotency_key) OR
          s.order_id<>economic_deterministic_uuid('execution-order',s.intent_id::TEXT,s.order_idempotency_key)))) AND
      p.canonical_bytes=convert_to(experiment_plan_identity(p.experiment_id::TEXT,p.program_id::TEXT,p.account_id::TEXT,p.manifest_id::TEXT,p.manifest_sha256,
        to_char(p.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(p.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        p.seed,p.mode,steps_text),'UTF8');
  IF NOT FOUND THEN RAISE EXCEPTION 'experiment replay plan graph does not reconstruct'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_experiment_result_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; outcomes_text TEXT; metrics_text TEXT;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'result_id')::UUID);
  SELECT '['||COALESCE(string_agg(experiment_step_outcome_identity(o.sequence,o.action,o.decision_sha256,o.intent_id::TEXT,o.order_id::TEXT,
    '['||COALESCE((SELECT string_agg(dataset_json_string(t.transition_id::TEXT),',' ORDER BY t.sequence) FROM experiment_run_transition_ids t WHERE t.result_id=o.result_id AND t.step_sequence=o.sequence),'')||']',
    '['||COALESCE((SELECT string_agg(dataset_json_string(f.fill_id::TEXT),',' ORDER BY f.sequence) FROM experiment_run_fill_ids f WHERE f.result_id=o.result_id AND f.step_sequence=o.sequence),'')||']',
    trim_scale(o.filled_quantity)::TEXT,trim_scale(o.fee_total)::TEXT,o.aggregate_sha256,o.outcome_sha256),',' ORDER BY o.sequence),'')||']'
    INTO outcomes_text FROM experiment_run_step_outcomes o WHERE o.result_id=target;
  SELECT experiment_metrics_identity(r.step_count,r.noop_count,r.rejected_count,r.intent_count,r.order_count,r.transition_count,r.fill_count,
    trim_scale(r.filled_quantity)::TEXT,trim_scale(r.fee_total)::TEXT) INTO metrics_text FROM experiment_run_results r WHERE r.id=target;
  PERFORM 1 FROM experiment_run_results r JOIN research_experiments e ON e.id=r.experiment_id
    JOIN experiment_replay_plans p ON p.id=r.plan_id AND p.experiment_id=e.id AND p.program_id=r.program_id
    WHERE r.id=target AND r.account_id=e.account_id AND r.manifest_id=e.manifest_id AND r.quality_result_id=e.quality_result_id AND
      r.simulation_policy_version=e.simulation_policy_version AND r.capital_policy_version=e.capital_policy_version AND r.mode=e.mode AND
      r.step_count=p.step_count AND r.step_count=(SELECT count(*) FROM experiment_run_step_outcomes WHERE result_id=r.id) AND
      (SELECT min(sequence)=0 AND max(sequence)=r.step_count-1 FROM experiment_run_step_outcomes WHERE result_id=r.id) AND
      r.noop_count=(SELECT count(*) FROM experiment_run_step_outcomes WHERE result_id=r.id AND action='noop') AND
      r.rejected_count=(SELECT count(*) FROM experiment_run_step_outcomes WHERE result_id=r.id AND action='rejected') AND
      r.intent_count=(SELECT count(*) FROM experiment_run_step_outcomes WHERE result_id=r.id AND action='execute') AND r.order_count=r.intent_count AND
      r.transition_count=(SELECT COALESCE(sum(transition_count),0) FROM experiment_run_step_outcomes WHERE result_id=r.id) AND
      r.fill_count=(SELECT COALESCE(sum(fill_count),0) FROM experiment_run_step_outcomes WHERE result_id=r.id) AND
      r.filled_quantity=(SELECT COALESCE(sum(filled_quantity),0) FROM experiment_run_step_outcomes WHERE result_id=r.id) AND
      r.fee_total=(SELECT COALESCE(sum(fee_total),0) FROM experiment_run_step_outcomes WHERE result_id=r.id) AND
      NOT EXISTS(SELECT 1 FROM experiment_run_step_outcomes o JOIN experiment_replay_plan_steps s ON s.plan_id=p.id AND s.sequence=o.sequence
        WHERE o.result_id=r.id AND (o.action<>s.action OR o.decision_sha256<>s.decision_sha256 OR o.transition_count<>(SELECT count(*) FROM experiment_run_transition_ids t WHERE t.result_id=r.id AND t.step_sequence=o.sequence) OR
          o.fill_count<>(SELECT count(*) FROM experiment_run_fill_ids f WHERE f.result_id=r.id AND f.step_sequence=o.sequence) OR
          o.action='execute' AND (o.intent_id<>s.intent_id OR o.order_id<>s.order_id OR
            EXISTS(SELECT 1 FROM experiment_run_transition_ids t JOIN execution_lifecycle_events event ON event.id=t.transition_id WHERE t.result_id=r.id AND t.step_sequence=o.sequence AND event.intent_id<>o.intent_id) OR
            EXISTS(SELECT 1 FROM experiment_run_fill_ids f JOIN execution_fills fill ON fill.id=f.fill_id WHERE f.result_id=r.id AND f.step_sequence=o.sequence AND fill.intent_id<>o.intent_id)))) AND
      r.canonical_bytes=convert_to(experiment_result_identity(r.experiment_id::TEXT,r.program_id::TEXT,r.plan_id::TEXT,r.account_id::TEXT,r.manifest_id::TEXT,
        r.quality_result_id::TEXT,r.simulation_policy_version,r.capital_policy_version,r.mode,metrics_text,outcomes_text),'UTF8');
  IF NOT FOUND THEN RAISE EXCEPTION 'experiment result graph does not reconstruct'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_experiment_attempt_event() RETURNS TRIGGER AS $$
BEGIN
  PERFORM 1 FROM experiment_run_attempt_events event JOIN experiment_run_attempts attempt ON attempt.id=event.attempt_id AND attempt.experiment_id=event.experiment_id
    WHERE event.id=NEW.id AND ((event.sequence=0 AND NOT EXISTS(SELECT 1 FROM experiment_run_attempt_events prior WHERE prior.attempt_id=event.attempt_id AND prior.sequence<>0)) OR
      (event.sequence=1 AND EXISTS(SELECT 1 FROM experiment_run_attempt_events prior WHERE prior.attempt_id=event.attempt_id AND prior.sequence=0 AND prior.occurred_at<=event.occurred_at))) AND
      (event.type<>'completed' OR EXISTS(SELECT 1 FROM experiment_run_results result WHERE result.id=event.result_id AND result.experiment_id=event.experiment_id));
  IF NOT FOUND THEN RAISE EXCEPTION 'experiment attempt event graph is invalid'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION reject_experiment_run_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'experiment run evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['experiment_programs','experiment_replay_plans','experiment_replay_plan_steps',
  'experiment_run_attempts','experiment_run_attempt_events','experiment_run_results','experiment_run_step_outcomes',
  'experiment_run_transition_ids','experiment_run_fill_ids'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_experiment_run_mutation()','trg_'||name||'_immutable',name); END LOOP; END $$;

CREATE CONSTRAINT TRIGGER trg_experiment_program_graph AFTER INSERT ON experiment_programs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_program();
CREATE CONSTRAINT TRIGGER trg_experiment_plan_graph AFTER INSERT ON experiment_replay_plans DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_plan_graph();
CREATE CONSTRAINT TRIGGER trg_experiment_plan_step_graph AFTER INSERT ON experiment_replay_plan_steps DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_plan_graph();
CREATE CONSTRAINT TRIGGER trg_experiment_result_graph AFTER INSERT ON experiment_run_results DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_result_graph();
CREATE CONSTRAINT TRIGGER trg_experiment_result_step_graph AFTER INSERT ON experiment_run_step_outcomes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_result_graph();
CREATE CONSTRAINT TRIGGER trg_experiment_result_transition_graph AFTER INSERT ON experiment_run_transition_ids DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_result_graph();
CREATE CONSTRAINT TRIGGER trg_experiment_result_fill_graph AFTER INSERT ON experiment_run_fill_ids DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_result_graph();
CREATE CONSTRAINT TRIGGER trg_experiment_attempt_event_graph AFTER INSERT ON experiment_run_attempt_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_experiment_attempt_event();

CREATE INDEX idx_experiment_programs_version ON experiment_programs(version_id,created_at,id);
CREATE INDEX idx_experiment_plans_experiment ON experiment_replay_plans(experiment_id,created_at,id);
CREATE INDEX idx_experiment_attempts_experiment ON experiment_run_attempts(experiment_id,created_at,id);
CREATE INDEX idx_experiment_attempt_events_experiment ON experiment_run_attempt_events(experiment_id,occurred_at,id);
CREATE INDEX idx_experiment_results_experiment ON experiment_run_results(experiment_id,created_at,id);
