LOCK TABLE experiment_run_results,experiment_replay_plans,research_experiments,execution_fills IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION evaluation_decimal_valid(value TEXT) RETURNS BOOLEAN AS $$
  SELECT value ~ '^-?(0|[1-9][0-9]*)(\.[0-9]+)?$' AND char_length(value)<=128 AND
    value::NUMERIC::TEXT=value AND abs(value::NUMERIC)<=1000000000000000000000000000000;
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION evaluation_policy_identity(version_value TEXT,frequency_value TEXT,periods_value INTEGER,return_value TEXT,
  cash_value TEXT,lot_value TEXT,recovery_value TEXT,scale_value INTEGER) RETURNS TEXT AS $$
  SELECT '{"schema":"evaluation-policy-v1","version":'||dataset_json_string(version_value)||
    ',"frequency":'||dataset_json_string(frequency_value)||',"periods_per_year":'||periods_value::TEXT||
    ',"return_kind":'||dataset_json_string(return_value)||',"cash_convention":'||dataset_json_string(cash_value)||
    ',"lot_method":'||dataset_json_string(lot_value)||',"recovery_definition":'||dataset_json_string(recovery_value)||
    ',"decimal_scale":'||scale_value::TEXT||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION evaluation_observation_identity(sequence_value INTEGER,observed_value TEXT,equity_value TEXT,benchmark_value TEXT,
  cash_value TEXT,gross_value TEXT,net_value TEXT,weight_value TEXT,cost_value TEXT,turnover_value TEXT,modeled_value TEXT,
  observed_slippage_value TEXT,evidence_id_value TEXT,evidence_sha_value TEXT) RETURNS TEXT AS $$
  SELECT '{"sequence":'||sequence_value::TEXT||',"observed_at":'||dataset_json_string(observed_value)||
    ',"equity":'||dataset_json_string(equity_value)||',"benchmark_value":'||dataset_json_string(benchmark_value)||
    ',"cash_return":'||dataset_json_string(cash_value)||',"gross_exposure":'||dataset_json_string(gross_value)||
    ',"net_exposure":'||dataset_json_string(net_value)||',"largest_position_weight":'||dataset_json_string(weight_value)||
    ',"cumulative_ownership_cost":'||dataset_json_string(cost_value)||',"cumulative_turnover":'||dataset_json_string(turnover_value)||
    ',"cumulative_modeled_slippage":'||dataset_json_string(modeled_value)||',"cumulative_observed_slippage":'||
      CASE WHEN observed_slippage_value IS NULL THEN 'null' ELSE dataset_json_string(observed_slippage_value) END||
    ',"evidence_id":'||dataset_json_string(evidence_id_value)||',"evidence_sha256":'||dataset_json_string(evidence_sha_value)||'}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION evaluation_trade_identity(sequence_value INTEGER,instrument_value TEXT,side_value TEXT,quantity_value TEXT,
  entry_fill_ids TEXT,exit_fill_ids TEXT,entry_value TEXT,exit_value TEXT,entry_price_value TEXT,exit_price_value TEXT,
  entry_fees_value TEXT,exit_fees_value TEXT,other_cost_value TEXT,gross_pnl_value TEXT,after_pnl_value TEXT) RETURNS TEXT AS $$
  SELECT '{"sequence":'||sequence_value::TEXT||',"instrument_id":'||dataset_json_string(instrument_value)||
    ',"side":'||dataset_json_string(side_value)||',"quantity":'||dataset_json_string(quantity_value)||
    ',"entry_fill_ids":'||entry_fill_ids||',"exit_fill_ids":'||exit_fill_ids||
    ',"entry_at":'||dataset_json_string(entry_value)||',"exit_at":'||dataset_json_string(exit_value)||
    ',"entry_price":'||dataset_json_string(entry_price_value)||',"exit_price":'||dataset_json_string(exit_price_value)||
    ',"entry_fees":'||dataset_json_string(entry_fees_value)||',"exit_fees":'||dataset_json_string(exit_fees_value)||
    ',"other_ownership_cost":'||dataset_json_string(other_cost_value)||',"gross_pnl":'||dataset_json_string(gross_pnl_value)||
    ',"after_cost_pnl":'||dataset_json_string(after_pnl_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION evaluation_metric_identity(section_value TEXT,name_value TEXT,state_value TEXT,value_value TEXT,unit_value TEXT,
  reason_value TEXT,description_value TEXT) RETURNS TEXT AS $$
  SELECT '{"section":'||dataset_json_string(section_value)||',"name":'||dataset_json_string(name_value)||
    ',"state":'||dataset_json_string(state_value)||',"value":'||dataset_json_string(value_value)||
    ',"unit":'||dataset_json_string(unit_value)||',"reason":'||dataset_json_string(reason_value)||
    ',"description":'||dataset_json_string(description_value)||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION trade_portfolio_evaluation_identity(result_id_value TEXT,result_sha_value TEXT,experiment_id_value TEXT,
  program_id_value TEXT,plan_id_value TEXT,account_id_value TEXT,manifest_id_value TEXT,quality_id_value TEXT,mode_value TEXT,
  policy_id_value TEXT,policy_sha_value TEXT,start_value TEXT,end_value TEXT,open_lots_value INTEGER,
  attempted_orders_value TEXT,filled_orders_value TEXT,attempted_quantity_value TEXT,filled_quantity_value TEXT,
  observations_text TEXT,trades_text TEXT,metrics_text TEXT) RETURNS TEXT AS $$
  SELECT '{"schema":"trade-portfolio-evaluation-v1","state":"completed","result_id":'||dataset_json_string(result_id_value)||
    ',"result_sha256":'||dataset_json_string(result_sha_value)||',"experiment_id":'||dataset_json_string(experiment_id_value)||
    ',"program_id":'||dataset_json_string(program_id_value)||',"plan_id":'||dataset_json_string(plan_id_value)||
    ',"account_id":'||dataset_json_string(account_id_value)||',"manifest_id":'||dataset_json_string(manifest_id_value)||
    ',"quality_result_id":'||dataset_json_string(quality_id_value)||',"mode":'||dataset_json_string(mode_value)||
    ',"policy_id":'||dataset_json_string(policy_id_value)||',"policy_sha256":'||dataset_json_string(policy_sha_value)||
    ',"evaluation_start":'||dataset_json_string(start_value)||',"evaluation_end":'||dataset_json_string(end_value)||
    ',"open_lot_count":'||open_lots_value::TEXT||',"execution":{"AttemptedOrders":'||dataset_json_string(attempted_orders_value)||
    ',"FilledOrders":'||dataset_json_string(filled_orders_value)||',"AttemptedQuantity":'||dataset_json_string(attempted_quantity_value)||
    ',"FilledQuantity":'||dataset_json_string(filled_quantity_value)||'},"observations":'||observations_text||
    ',"closed_trades":'||trades_text||',"metrics":'||metrics_text||'}';
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE TABLE evaluation_policy_artifacts (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='evaluation-policy-v1'),
  version TEXT NOT NULL CHECK(version<>'' AND version=btrim(version) AND char_length(version)<=128),
  frequency TEXT NOT NULL CHECK(frequency IN ('minute','daily','weekly','monthly')),
  periods_per_year INTEGER NOT NULL CHECK(periods_per_year>0 AND periods_per_year<=1000000),
  return_kind TEXT NOT NULL CHECK(return_kind='simple'), cash_convention TEXT NOT NULL CHECK(cash_convention='explicit_per_period'),
  lot_method TEXT NOT NULL CHECK(lot_method='fifo'), recovery_definition TEXT NOT NULL CHECK(recovery_definition='first_equity_at_or_above_prior_peak'),
  decimal_scale INTEGER NOT NULL CHECK(decimal_scale BETWEEN 6 AND 18), sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('evaluation-policy',schema_name||'@sha256:'||sha256)),
  CHECK(convert_from(canonical_bytes,'UTF8')=evaluation_policy_identity(version,frequency,periods_per_year,return_kind,cash_convention,lot_method,recovery_definition,decimal_scale))
);

CREATE TABLE trade_portfolio_evaluations (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='trade-portfolio-evaluation-v1'), state TEXT NOT NULL CHECK(state='completed'),
  result_id UUID NOT NULL REFERENCES experiment_run_results(id) ON DELETE RESTRICT,
  result_sha256 TEXT NOT NULL CHECK(result_sha256 ~ '^[0-9a-f]{64}$'), experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  program_id UUID NOT NULL REFERENCES experiment_programs(id) ON DELETE RESTRICT, plan_id UUID NOT NULL REFERENCES experiment_replay_plans(id) ON DELETE RESTRICT,
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT, manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
  quality_result_id UUID NOT NULL REFERENCES dataset_quality_results(id) ON DELETE RESTRICT,
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  policy_id UUID NOT NULL REFERENCES evaluation_policy_artifacts(id) ON DELETE RESTRICT, policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  evaluation_start TIMESTAMPTZ NOT NULL CHECK(evaluation_start=date_trunc('microseconds',evaluation_start)),
  evaluation_end TIMESTAMPTZ NOT NULL CHECK(evaluation_end=date_trunc('microseconds',evaluation_end) AND evaluation_end>evaluation_start),
  open_lot_count INTEGER NOT NULL CHECK(open_lot_count>=0), attempted_orders TEXT NOT NULL CHECK(attempted_orders ~ '^(0|[1-9][0-9]*)$'),
  filled_orders TEXT NOT NULL CHECK(filled_orders ~ '^(0|[1-9][0-9]*)$' AND filled_orders::NUMERIC<=attempted_orders::NUMERIC),
  attempted_quantity TEXT NOT NULL CHECK(evaluation_decimal_valid(attempted_quantity) AND attempted_quantity::NUMERIC>=0),
  filled_quantity TEXT NOT NULL CHECK(evaluation_decimal_valid(filled_quantity) AND filled_quantity::NUMERIC>=0 AND filled_quantity::NUMERIC<=attempted_quantity::NUMERIC),
  observation_count INTEGER NOT NULL CHECK(observation_count BETWEEN 2 AND 100000), closed_trade_count INTEGER NOT NULL CHECK(closed_trade_count>=0),
  metric_count INTEGER NOT NULL CHECK(metric_count>0), sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('trade-portfolio-evaluation',schema_name||'@sha256:'||sha256))
);

CREATE TABLE evaluation_observations (
  evaluation_id UUID NOT NULL REFERENCES trade_portfolio_evaluations(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  observed_at TIMESTAMPTZ NOT NULL CHECK(observed_at=date_trunc('microseconds',observed_at)), equity TEXT NOT NULL CHECK(evaluation_decimal_valid(equity) AND equity::NUMERIC>0),
  benchmark_value TEXT NOT NULL CHECK(evaluation_decimal_valid(benchmark_value) AND benchmark_value::NUMERIC>0),
  cash_return TEXT NOT NULL CHECK(evaluation_decimal_valid(cash_return)), gross_exposure TEXT NOT NULL CHECK(evaluation_decimal_valid(gross_exposure) AND gross_exposure::NUMERIC>=0),
  net_exposure TEXT NOT NULL CHECK(evaluation_decimal_valid(net_exposure)), largest_position_weight TEXT NOT NULL CHECK(evaluation_decimal_valid(largest_position_weight) AND largest_position_weight::NUMERIC BETWEEN 0 AND 1),
  cumulative_ownership_cost TEXT NOT NULL CHECK(evaluation_decimal_valid(cumulative_ownership_cost) AND cumulative_ownership_cost::NUMERIC>=0),
  cumulative_turnover TEXT NOT NULL CHECK(evaluation_decimal_valid(cumulative_turnover) AND cumulative_turnover::NUMERIC>=0),
  cumulative_modeled_slippage TEXT NOT NULL CHECK(evaluation_decimal_valid(cumulative_modeled_slippage) AND cumulative_modeled_slippage::NUMERIC>=0),
  cumulative_observed_slippage TEXT CHECK(cumulative_observed_slippage IS NULL OR evaluation_decimal_valid(cumulative_observed_slippage) AND cumulative_observed_slippage::NUMERIC>=0),
  evidence_id UUID NOT NULL, evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'), PRIMARY KEY(evaluation_id,sequence)
);

CREATE TABLE evaluation_closed_trades (
  evaluation_id UUID NOT NULL REFERENCES trade_portfolio_evaluations(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT, side TEXT NOT NULL CHECK(side IN ('long','short')),
  quantity TEXT NOT NULL CHECK(evaluation_decimal_valid(quantity) AND quantity::NUMERIC>0), entry_fill_count INTEGER NOT NULL CHECK(entry_fill_count>0),
  exit_fill_count INTEGER NOT NULL CHECK(exit_fill_count>0), entry_at TIMESTAMPTZ NOT NULL CHECK(entry_at=date_trunc('microseconds',entry_at)),
  exit_at TIMESTAMPTZ NOT NULL CHECK(exit_at=date_trunc('microseconds',exit_at) AND exit_at>=entry_at),
  entry_price TEXT NOT NULL CHECK(evaluation_decimal_valid(entry_price) AND entry_price::NUMERIC>0),
  exit_price TEXT NOT NULL CHECK(evaluation_decimal_valid(exit_price) AND exit_price::NUMERIC>0),
  entry_fees TEXT NOT NULL CHECK(evaluation_decimal_valid(entry_fees) AND entry_fees::NUMERIC>=0), exit_fees TEXT NOT NULL CHECK(evaluation_decimal_valid(exit_fees) AND exit_fees::NUMERIC>=0),
  other_ownership_cost TEXT NOT NULL CHECK(evaluation_decimal_valid(other_ownership_cost) AND other_ownership_cost::NUMERIC>=0),
  gross_pnl TEXT NOT NULL CHECK(evaluation_decimal_valid(gross_pnl)), after_cost_pnl TEXT NOT NULL CHECK(evaluation_decimal_valid(after_cost_pnl)),
  PRIMARY KEY(evaluation_id,sequence), CHECK(after_cost_pnl::NUMERIC=gross_pnl::NUMERIC-entry_fees::NUMERIC-exit_fees::NUMERIC-other_ownership_cost::NUMERIC)
);

CREATE TABLE evaluation_trade_fill_ids (
  evaluation_id UUID NOT NULL, trade_sequence INTEGER NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('entry','exit')),
  sequence INTEGER NOT NULL CHECK(sequence>=0), fill_id UUID NOT NULL REFERENCES execution_fills(id) ON DELETE RESTRICT,
  PRIMARY KEY(evaluation_id,trade_sequence,kind,sequence), UNIQUE(evaluation_id,fill_id),
  FOREIGN KEY(evaluation_id,trade_sequence) REFERENCES evaluation_closed_trades(evaluation_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE evaluation_metrics (
  evaluation_id UUID NOT NULL REFERENCES trade_portfolio_evaluations(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  section TEXT NOT NULL CHECK(section IN ('portfolio','trade','execution','cost','exposure','sample','curve_diagnostics')),
  name TEXT NOT NULL CHECK(name<>'' AND name=btrim(name) AND char_length(name)<=128), state TEXT NOT NULL CHECK(state IN ('available','unavailable','positive_infinity')),
  value TEXT NOT NULL DEFAULT '', unit TEXT NOT NULL CHECK(unit<>'' AND unit=btrim(unit) AND char_length(unit)<=128),
  reason TEXT NOT NULL DEFAULT '' CHECK(reason=btrim(reason) AND char_length(reason)<=128),
  description TEXT NOT NULL CHECK(description<>'' AND description=btrim(description) AND char_length(description)<=256),
  PRIMARY KEY(evaluation_id,sequence), UNIQUE(evaluation_id,section,name),
  CHECK((state='available' AND value<>'' AND reason='') OR (state='unavailable' AND value='' AND reason<>'') OR (state='positive_infinity' AND value='' AND reason='')),
  CHECK(section<>'trade' OR name<>'win_rate' OR description='closed_trade_after_cost_win_rate_not_bar_return_rate'),
  CHECK(section<>'curve_diagnostics' OR name<>'bar_positive_return_rate' OR description='descriptor_only_not_trade_win_rate')
);

CREATE FUNCTION validate_trade_portfolio_evaluation_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; observations_text TEXT; trades_text TEXT; metrics_text TEXT;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'evaluation_id')::UUID);
  SELECT '['||COALESCE(string_agg(evaluation_observation_identity(o.sequence,to_char(o.observed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    o.equity,o.benchmark_value,o.cash_return,o.gross_exposure,o.net_exposure,o.largest_position_weight,o.cumulative_ownership_cost,
    o.cumulative_turnover,o.cumulative_modeled_slippage,o.cumulative_observed_slippage,o.evidence_id::TEXT,o.evidence_sha256),',' ORDER BY o.sequence),'')||']'
    INTO observations_text FROM evaluation_observations o WHERE o.evaluation_id=target;
  SELECT '['||COALESCE(string_agg(evaluation_trade_identity(t.sequence,t.instrument_id::TEXT,t.side,t.quantity,
    '['||COALESCE((SELECT string_agg(dataset_json_string(f.fill_id::TEXT),',' ORDER BY f.sequence) FROM evaluation_trade_fill_ids f WHERE f.evaluation_id=t.evaluation_id AND f.trade_sequence=t.sequence AND f.kind='entry'),'')||']',
    '['||COALESCE((SELECT string_agg(dataset_json_string(f.fill_id::TEXT),',' ORDER BY f.sequence) FROM evaluation_trade_fill_ids f WHERE f.evaluation_id=t.evaluation_id AND f.trade_sequence=t.sequence AND f.kind='exit'),'')||']',
    to_char(t.entry_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(t.exit_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    t.entry_price,t.exit_price,t.entry_fees,t.exit_fees,t.other_ownership_cost,t.gross_pnl,t.after_cost_pnl),',' ORDER BY t.sequence),'')||']'
    INTO trades_text FROM evaluation_closed_trades t WHERE t.evaluation_id=target;
  SELECT '['||COALESCE(string_agg(evaluation_metric_identity(m.section,m.name,m.state,m.value,m.unit,m.reason,m.description),',' ORDER BY m.sequence),'')||']'
    INTO metrics_text FROM evaluation_metrics m WHERE m.evaluation_id=target;
  PERFORM 1 FROM trade_portfolio_evaluations e JOIN experiment_run_results r ON r.id=e.result_id
    JOIN experiment_replay_plans p ON p.id=e.plan_id JOIN evaluation_policy_artifacts policy ON policy.id=e.policy_id
    WHERE e.id=target AND e.result_sha256=r.sha256 AND e.experiment_id=r.experiment_id AND e.program_id=r.program_id AND e.plan_id=r.plan_id AND
      e.account_id=r.account_id AND e.manifest_id=r.manifest_id AND e.quality_result_id=r.quality_result_id AND e.mode=r.mode AND
      e.policy_sha256=policy.sha256 AND e.evaluation_start>=p.evaluation_start AND e.evaluation_end<=p.evaluation_end AND
      e.observation_count=(SELECT count(*) FROM evaluation_observations WHERE evaluation_id=e.id) AND
      (SELECT min(sequence)=0 AND max(sequence)=e.observation_count-1 FROM evaluation_observations WHERE evaluation_id=e.id) AND
      e.closed_trade_count=(SELECT count(*) FROM evaluation_closed_trades WHERE evaluation_id=e.id) AND
      (e.closed_trade_count=0 OR (SELECT min(sequence)=0 AND max(sequence)=e.closed_trade_count-1 FROM evaluation_closed_trades WHERE evaluation_id=e.id)) AND
      e.metric_count=(SELECT count(*) FROM evaluation_metrics WHERE evaluation_id=e.id) AND
      (SELECT min(sequence)=0 AND max(sequence)=e.metric_count-1 FROM evaluation_metrics WHERE evaluation_id=e.id) AND
      (SELECT observed_at=e.evaluation_start FROM evaluation_observations WHERE evaluation_id=e.id AND sequence=0) AND
      (SELECT observed_at=e.evaluation_end FROM evaluation_observations WHERE evaluation_id=e.id AND sequence=e.observation_count-1) AND
      NOT EXISTS(SELECT 1 FROM evaluation_observations current JOIN evaluation_observations prior ON prior.evaluation_id=current.evaluation_id AND prior.sequence=current.sequence-1
        WHERE current.evaluation_id=e.id AND (current.observed_at<=prior.observed_at OR current.cumulative_ownership_cost::NUMERIC<prior.cumulative_ownership_cost::NUMERIC OR
          current.cumulative_turnover::NUMERIC<prior.cumulative_turnover::NUMERIC OR current.cumulative_modeled_slippage::NUMERIC<prior.cumulative_modeled_slippage::NUMERIC OR
          current.cumulative_observed_slippage IS NOT NULL AND prior.cumulative_observed_slippage IS NOT NULL AND current.cumulative_observed_slippage::NUMERIC<prior.cumulative_observed_slippage::NUMERIC OR
          policy.frequency='minute' AND current.observed_at<>prior.observed_at+INTERVAL '1 minute' OR
          policy.frequency='daily' AND current.observed_at<>prior.observed_at+INTERVAL '1 day' OR
          policy.frequency='weekly' AND current.observed_at<>prior.observed_at+INTERVAL '7 days' OR
          policy.frequency='monthly' AND current.observed_at<>prior.observed_at+INTERVAL '1 month')) AND
      NOT EXISTS(SELECT 1 FROM evaluation_closed_trades t WHERE t.evaluation_id=e.id AND
        (t.entry_at<e.evaluation_start OR t.exit_at>e.evaluation_end OR t.entry_fill_count<>(SELECT count(*) FROM evaluation_trade_fill_ids f WHERE f.evaluation_id=e.id AND f.trade_sequence=t.sequence AND f.kind='entry') OR
          t.exit_fill_count<>(SELECT count(*) FROM evaluation_trade_fill_ids f WHERE f.evaluation_id=e.id AND f.trade_sequence=t.sequence AND f.kind='exit') OR
          EXISTS(SELECT 1 FROM evaluation_trade_fill_ids source JOIN execution_fills fill ON fill.id=source.fill_id
            WHERE source.evaluation_id=e.id AND source.trade_sequence=t.sequence AND (fill.account_id<>e.account_id OR fill.instrument_id<>t.instrument_id)))) AND
      e.canonical_bytes=convert_to(trade_portfolio_evaluation_identity(e.result_id::TEXT,e.result_sha256,e.experiment_id::TEXT,e.program_id::TEXT,
        e.plan_id::TEXT,e.account_id::TEXT,e.manifest_id::TEXT,e.quality_result_id::TEXT,e.mode,e.policy_id::TEXT,e.policy_sha256,
        to_char(e.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(e.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        e.open_lot_count,e.attempted_orders,e.filled_orders,e.attempted_quantity,e.filled_quantity,observations_text,trades_text,metrics_text),'UTF8');
  IF NOT FOUND THEN RAISE EXCEPTION 'trade portfolio evaluation graph does not reconstruct'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION reject_trade_portfolio_evaluation_mutation() RETURNS TRIGGER AS $$
BEGIN RAISE EXCEPTION 'trade portfolio evaluation evidence is append-only'; END;
$$ LANGUAGE plpgsql;

DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['evaluation_policy_artifacts','trade_portfolio_evaluations','evaluation_observations',
  'evaluation_closed_trades','evaluation_trade_fill_ids','evaluation_metrics'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_trade_portfolio_evaluation_mutation()',
    'trg_'||name||'_immutable',name); END LOOP; END $$;

CREATE CONSTRAINT TRIGGER trg_trade_portfolio_evaluation_graph AFTER INSERT ON trade_portfolio_evaluations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trade_portfolio_evaluation_graph();
CREATE CONSTRAINT TRIGGER trg_evaluation_observation_graph AFTER INSERT ON evaluation_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trade_portfolio_evaluation_graph();
CREATE CONSTRAINT TRIGGER trg_evaluation_closed_trade_graph AFTER INSERT ON evaluation_closed_trades DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trade_portfolio_evaluation_graph();
CREATE CONSTRAINT TRIGGER trg_evaluation_trade_fill_graph AFTER INSERT ON evaluation_trade_fill_ids DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trade_portfolio_evaluation_graph();
CREATE CONSTRAINT TRIGGER trg_evaluation_metric_graph AFTER INSERT ON evaluation_metrics DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trade_portfolio_evaluation_graph();
CREATE INDEX idx_trade_portfolio_evaluations_result ON trade_portfolio_evaluations(result_id,created_at,id);
CREATE INDEX idx_trade_portfolio_evaluations_experiment ON trade_portfolio_evaluations(experiment_id,created_at,id);
