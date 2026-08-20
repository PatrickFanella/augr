LOCK TABLE instruments,venue_contracts IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE trend_v1_policies (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='etf-time-series-trend-policy-v1'),
  version TEXT NOT NULL, decimal_scale INTEGER NOT NULL CHECK(decimal_scale BETWEEN 6 AND 18),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'version'=version AND (canonical_json->>'decimal_scale')::INTEGER=decimal_scale),
  CHECK(id=economic_deterministic_uuid('etf-time-series-trend-policy',schema_name||'@sha256:'||sha256))
);

CREATE TABLE trend_v1_scenarios (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='etf-time-series-trend-scenario-v1'), state TEXT NOT NULL CHECK(state='declared'),
  policy_id UUID NOT NULL REFERENCES trend_v1_policies(id) ON DELETE RESTRICT, policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  initial_capital TEXT NOT NULL CHECK(evaluation_decimal_valid(initial_capital) AND initial_capital::NUMERIC>0),
  evaluation_start TIMESTAMPTZ NOT NULL CHECK(evaluation_start=date_trunc('microseconds',evaluation_start)),
  evaluation_end TIMESTAMPTZ NOT NULL CHECK(evaluation_end=date_trunc('microseconds',evaluation_end) AND evaluation_end>evaluation_start),
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')), rebalance_count INTEGER NOT NULL CHECK(rebalance_count BETWEEN 2 AND 10000),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('etf-time-series-trend-scenario',schema_name||'@sha256:'||sha256))
);

CREATE TABLE trend_v1_source_rebalances (
  scenario_id UUID NOT NULL REFERENCES trend_v1_scenarios(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  occurred_at TIMESTAMPTZ NOT NULL CHECK(occurred_at=date_trunc('microseconds',occurred_at)), member_count INTEGER NOT NULL CHECK(member_count>0),
  canonical_rebalance JSONB NOT NULL CHECK(jsonb_typeof(canonical_rebalance)='object'), PRIMARY KEY(scenario_id,sequence)
);

CREATE TABLE trend_v1_universe_members (
  scenario_id UUID NOT NULL, rebalance_sequence INTEGER NOT NULL, member_sequence INTEGER NOT NULL CHECK(member_sequence>=0),
  instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT, venue_contract_id UUID NOT NULL REFERENCES venue_contracts(id) ON DELETE RESTRICT,
  evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'), canonical_member JSONB NOT NULL CHECK(jsonb_typeof(canonical_member)='object'),
  PRIMARY KEY(scenario_id,rebalance_sequence,member_sequence), UNIQUE(scenario_id,rebalance_sequence,instrument_id),
  FOREIGN KEY(scenario_id,rebalance_sequence) REFERENCES trend_v1_source_rebalances(scenario_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE trend_v1_horizon_prices (
  scenario_id UUID NOT NULL, rebalance_sequence INTEGER NOT NULL, member_sequence INTEGER NOT NULL, horizon_sequence INTEGER NOT NULL CHECK(horizon_sequence>=0),
  price TEXT NOT NULL CHECK(evaluation_decimal_valid(price) AND price::NUMERIC>0), PRIMARY KEY(scenario_id,rebalance_sequence,member_sequence,horizon_sequence),
  FOREIGN KEY(scenario_id,rebalance_sequence,member_sequence) REFERENCES trend_v1_universe_members(scenario_id,rebalance_sequence,member_sequence) ON DELETE RESTRICT
);

CREATE TABLE trend_v1_reports (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='etf-time-series-trend-report-v1'), state TEXT NOT NULL CHECK(state='completed'),
  policy_id UUID NOT NULL REFERENCES trend_v1_policies(id) ON DELETE RESTRICT, policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  scenario_id UUID NOT NULL REFERENCES trend_v1_scenarios(id) ON DELETE RESTRICT, scenario_sha256 TEXT NOT NULL CHECK(scenario_sha256 ~ '^[0-9a-f]{64}$'),
  initial_capital TEXT NOT NULL, evaluation_start TIMESTAMPTZ NOT NULL, evaluation_end TIMESTAMPTZ NOT NULL,
  rebalance_count INTEGER NOT NULL CHECK(rebalance_count>=2),
  ending_cash TEXT NOT NULL, ending_equity TEXT NOT NULL, cumulative_turnover TEXT NOT NULL, total_cost TEXT NOT NULL, after_cost_total_return TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('etf-time-series-trend-report',schema_name||'@sha256:'||sha256)),
  CHECK(evaluation_decimal_valid(initial_capital) AND evaluation_decimal_valid(ending_cash) AND evaluation_decimal_valid(ending_equity) AND evaluation_decimal_valid(cumulative_turnover) AND evaluation_decimal_valid(total_cost) AND evaluation_decimal_valid(after_cost_total_return)),
  CHECK(ending_cash::NUMERIC>=0 AND ending_equity::NUMERIC>0 AND cumulative_turnover::NUMERIC>=0 AND total_cost::NUMERIC>=0)
);

CREATE TABLE trend_v1_rebalances (
  report_id UUID NOT NULL REFERENCES trend_v1_reports(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence>=0),
  occurred_at TIMESTAMPTZ NOT NULL CHECK(occurred_at=date_trunc('microseconds',occurred_at)),
  desired_turnover TEXT NOT NULL, applied_turnover TEXT NOT NULL, turnover_scale TEXT NOT NULL, remaining_target_drift TEXT NOT NULL,
  cost TEXT NOT NULL, cash TEXT NOT NULL, equity TEXT NOT NULL, gross_target_weight TEXT NOT NULL, signal_count INTEGER NOT NULL CHECK(signal_count>0), trade_count INTEGER NOT NULL CHECK(trade_count>=0), holding_count INTEGER NOT NULL CHECK(holding_count>=0),
  canonical_rebalance JSONB NOT NULL CHECK(jsonb_typeof(canonical_rebalance)='object'), PRIMARY KEY(report_id,sequence),
  CHECK(evaluation_decimal_valid(desired_turnover) AND evaluation_decimal_valid(applied_turnover) AND evaluation_decimal_valid(turnover_scale) AND evaluation_decimal_valid(remaining_target_drift) AND evaluation_decimal_valid(cost) AND evaluation_decimal_valid(cash) AND evaluation_decimal_valid(equity) AND evaluation_decimal_valid(gross_target_weight)),
  CHECK(desired_turnover::NUMERIC>=0 AND applied_turnover::NUMERIC>=0 AND applied_turnover::NUMERIC<=desired_turnover::NUMERIC AND turnover_scale::NUMERIC>0 AND turnover_scale::NUMERIC<=1 AND remaining_target_drift::NUMERIC>=0 AND cost::NUMERIC>=0 AND cash::NUMERIC>=0 AND equity::NUMERIC>0 AND gross_target_weight::NUMERIC>=0 AND gross_target_weight::NUMERIC<=1)
);

CREATE TABLE trend_v1_signals (report_id UUID NOT NULL,rebalance_sequence INTEGER NOT NULL,sequence INTEGER NOT NULL CHECK(sequence>=0),canonical_value JSONB NOT NULL CHECK(jsonb_typeof(canonical_value)='object'),PRIMARY KEY(report_id,rebalance_sequence,sequence),FOREIGN KEY(report_id,rebalance_sequence) REFERENCES trend_v1_rebalances(report_id,sequence) ON DELETE RESTRICT);
CREATE TABLE trend_v1_trades (report_id UUID NOT NULL,rebalance_sequence INTEGER NOT NULL,sequence INTEGER NOT NULL CHECK(sequence>=0),canonical_value JSONB NOT NULL CHECK(jsonb_typeof(canonical_value)='object'),PRIMARY KEY(report_id,rebalance_sequence,sequence),FOREIGN KEY(report_id,rebalance_sequence) REFERENCES trend_v1_rebalances(report_id,sequence) ON DELETE RESTRICT);
CREATE TABLE trend_v1_holdings (report_id UUID NOT NULL,rebalance_sequence INTEGER NOT NULL,sequence INTEGER NOT NULL CHECK(sequence>=0),canonical_value JSONB NOT NULL CHECK(jsonb_typeof(canonical_value)='object'),PRIMARY KEY(report_id,rebalance_sequence,sequence),FOREIGN KEY(report_id,rebalance_sequence) REFERENCES trend_v1_rebalances(report_id,sequence) ON DELETE RESTRICT);

CREATE FUNCTION validate_trend_v1_scenario() RETURNS TRIGGER AS $$
DECLARE target UUID; scenario trend_v1_scenarios%ROWTYPE; policy trend_v1_policies%ROWTYPE; reconstructed JSONB;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'scenario_id')::UUID);
  SELECT * INTO scenario FROM trend_v1_scenarios WHERE id=target; SELECT * INTO policy FROM trend_v1_policies WHERE id=scenario.policy_id;
  SELECT jsonb_build_object('schema',scenario.schema_name,'state',scenario.state,'policy_id',scenario.policy_id::TEXT,'policy_sha256',scenario.policy_sha256,'initial_capital',scenario.initial_capital,
    'evaluation_start',to_char(scenario.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'evaluation_end',to_char(scenario.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'mode',scenario.mode,
    'rebalances',COALESCE((SELECT jsonb_agg(source.canonical_rebalance ORDER BY source.sequence) FROM trend_v1_source_rebalances source WHERE source.scenario_id=target),'[]'::JSONB)) INTO reconstructed;
  IF scenario.policy_sha256<>policy.sha256 OR scenario.rebalance_count<>(SELECT count(*) FROM trend_v1_source_rebalances WHERE scenario_id=target) OR reconstructed<>scenario.canonical_json OR
    NOT EXISTS(SELECT 1 FROM trend_v1_source_rebalances WHERE scenario_id=target AND sequence=0 AND occurred_at=scenario.evaluation_start) OR NOT EXISTS(SELECT 1 FROM trend_v1_source_rebalances WHERE scenario_id=target AND sequence=scenario.rebalance_count-1 AND occurred_at=scenario.evaluation_end) OR
    EXISTS(SELECT 1 FROM trend_v1_source_rebalances source WHERE source.scenario_id=target AND (source.member_count<>(SELECT count(*) FROM trend_v1_universe_members member WHERE member.scenario_id=target AND member.rebalance_sequence=source.sequence) OR source.canonical_rebalance->'members'<>COALESCE((SELECT jsonb_agg(member.canonical_member ORDER BY member.member_sequence) FROM trend_v1_universe_members member WHERE member.scenario_id=target AND member.rebalance_sequence=source.sequence),'[]'::JSONB))) OR
    EXISTS(SELECT 1 FROM trend_v1_universe_members member WHERE member.scenario_id=target AND member.canonical_member->'horizon_prices'<>COALESCE((SELECT jsonb_agg(to_jsonb(price.price) ORDER BY price.horizon_sequence) FROM trend_v1_horizon_prices price WHERE price.scenario_id=member.scenario_id AND price.rebalance_sequence=member.rebalance_sequence AND price.member_sequence=member.member_sequence),'[]'::JSONB)) OR
    EXISTS(SELECT 1 FROM trend_v1_source_rebalances current_source LEFT JOIN trend_v1_source_rebalances prior ON prior.scenario_id=current_source.scenario_id AND prior.sequence=current_source.sequence-1 WHERE current_source.scenario_id=target AND current_source.sequence>0 AND (prior.sequence IS NULL OR current_source.occurred_at<=prior.occurred_at))
    THEN RAISE EXCEPTION 'trend scenario graph does not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_trend_v1_report() RETURNS TRIGGER AS $$
DECLARE target UUID; report trend_v1_reports%ROWTYPE; scenario trend_v1_scenarios%ROWTYPE; policy trend_v1_policies%ROWTYPE; reconstructed JSONB;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'report_id')::UUID);
  SELECT * INTO report FROM trend_v1_reports WHERE id=target; SELECT * INTO scenario FROM trend_v1_scenarios WHERE id=report.scenario_id; SELECT * INTO policy FROM trend_v1_policies WHERE id=report.policy_id;
  SELECT jsonb_build_object('schema',report.schema_name,'state',report.state,'policy_id',report.policy_id::TEXT,'policy_sha256',report.policy_sha256,'scenario_id',report.scenario_id::TEXT,'scenario_sha256',report.scenario_sha256,'initial_capital',report.initial_capital,
    'evaluation_start',to_char(report.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'evaluation_end',to_char(report.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'rebalances',COALESCE((SELECT jsonb_agg(rebalance.canonical_rebalance ORDER BY rebalance.sequence) FROM trend_v1_rebalances rebalance WHERE rebalance.report_id=target),'[]'::JSONB),
    'ending_cash',report.ending_cash,'ending_equity',report.ending_equity,'cumulative_turnover',report.cumulative_turnover,'total_cost',report.total_cost,'after_cost_total_return',report.after_cost_total_return) INTO reconstructed;
  IF report.policy_id<>scenario.policy_id OR report.policy_sha256<>policy.sha256 OR report.scenario_sha256<>scenario.sha256 OR report.initial_capital<>scenario.initial_capital OR report.evaluation_start<>scenario.evaluation_start OR report.evaluation_end<>scenario.evaluation_end OR
    report.rebalance_count<>(SELECT count(*) FROM trend_v1_rebalances WHERE report_id=target) OR reconstructed<>report.canonical_json OR
    EXISTS(SELECT 1 FROM trend_v1_rebalances rebalance JOIN trend_v1_source_rebalances source ON source.scenario_id=scenario.id AND source.sequence=rebalance.sequence WHERE rebalance.report_id=target AND rebalance.occurred_at<>source.occurred_at) OR
    EXISTS(SELECT 1 FROM trend_v1_rebalances rebalance WHERE rebalance.report_id=target AND (rebalance.applied_turnover::NUMERIC>(policy.canonical_json->>'maximum_rebalance_turnover')::NUMERIC OR rebalance.gross_target_weight::NUMERIC>(policy.canonical_json->>'maximum_gross_weight')::NUMERIC OR rebalance.signal_count<>(SELECT count(*) FROM trend_v1_signals value WHERE value.report_id=target AND value.rebalance_sequence=rebalance.sequence) OR rebalance.trade_count<>(SELECT count(*) FROM trend_v1_trades value WHERE value.report_id=target AND value.rebalance_sequence=rebalance.sequence) OR rebalance.holding_count<>(SELECT count(*) FROM trend_v1_holdings value WHERE value.report_id=target AND value.rebalance_sequence=rebalance.sequence) OR rebalance.canonical_rebalance->'signals'<>COALESCE((SELECT jsonb_agg(value.canonical_value ORDER BY value.sequence) FROM trend_v1_signals value WHERE value.report_id=target AND value.rebalance_sequence=rebalance.sequence),'[]'::JSONB) OR rebalance.canonical_rebalance->'trades'<>COALESCE((SELECT jsonb_agg(value.canonical_value ORDER BY value.sequence) FROM trend_v1_trades value WHERE value.report_id=target AND value.rebalance_sequence=rebalance.sequence),'[]'::JSONB) OR rebalance.canonical_rebalance->'holdings'<>COALESCE((SELECT jsonb_agg(value.canonical_value ORDER BY value.sequence) FROM trend_v1_holdings value WHERE value.report_id=target AND value.rebalance_sequence=rebalance.sequence),'[]'::JSONB))) OR
    EXISTS(SELECT 1 FROM trend_v1_signals value WHERE value.report_id=target AND (value.canonical_value->>'target_weight')::NUMERIC>(policy.canonical_json->>'maximum_instrument_weight')::NUMERIC)
    THEN RAISE EXCEPTION 'trend report graph does not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_trend_v1_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'trend v1 evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE table_name TEXT; BEGIN FOREACH table_name IN ARRAY ARRAY['trend_v1_policies','trend_v1_scenarios','trend_v1_source_rebalances','trend_v1_universe_members','trend_v1_horizon_prices','trend_v1_reports','trend_v1_rebalances','trend_v1_signals','trend_v1_trades','trend_v1_holdings'] LOOP EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_trend_v1_mutation()','trg_'||table_name||'_immutable',table_name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_trend_v1_scenario_graph AFTER INSERT ON trend_v1_scenarios DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_trend_v1_source_graph AFTER INSERT ON trend_v1_source_rebalances DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_trend_v1_member_graph AFTER INSERT ON trend_v1_universe_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_trend_v1_horizon_graph AFTER INSERT ON trend_v1_horizon_prices DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_trend_v1_report_graph AFTER INSERT ON trend_v1_reports DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_report();
CREATE CONSTRAINT TRIGGER trg_trend_v1_rebalance_graph AFTER INSERT ON trend_v1_rebalances DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_report();
CREATE CONSTRAINT TRIGGER trg_trend_v1_signal_graph AFTER INSERT ON trend_v1_signals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_report();
CREATE CONSTRAINT TRIGGER trg_trend_v1_trade_graph AFTER INSERT ON trend_v1_trades DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_report();
CREATE CONSTRAINT TRIGGER trg_trend_v1_holding_graph AFTER INSERT ON trend_v1_holdings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_trend_v1_report();
CREATE INDEX idx_trend_v1_scenarios_policy ON trend_v1_scenarios(policy_id,created_at,id);
CREATE INDEX idx_trend_v1_reports_scenario ON trend_v1_reports(scenario_id,created_at,id);
