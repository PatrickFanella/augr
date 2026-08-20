LOCK TABLE instruments IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE wheel_v1_policies (
  id UUID PRIMARY KEY,
  schema_name TEXT NOT NULL CHECK(schema_name='quality-filtered-wheel-policy-v1'),
  version TEXT NOT NULL,
  decimal_scale INTEGER NOT NULL CHECK(decimal_scale BETWEEN 6 AND 18),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'version'=version AND (canonical_json->>'decimal_scale')::INTEGER=decimal_scale),
  CHECK(id=economic_deterministic_uuid('quality-filtered-wheel-policy',schema_name||'@sha256:'||sha256))
);

CREATE TABLE wheel_v1_scenarios (
  id UUID PRIMARY KEY,
  schema_name TEXT NOT NULL CHECK(schema_name='quality-filtered-wheel-scenario-v1'),
  state TEXT NOT NULL CHECK(state='declared'),
  policy_id UUID NOT NULL REFERENCES wheel_v1_policies(id) ON DELETE RESTRICT,
  policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  underlying_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
  initial_capital TEXT NOT NULL CHECK(evaluation_decimal_valid(initial_capital) AND initial_capital::NUMERIC>0),
  evaluation_start TIMESTAMPTZ NOT NULL CHECK(evaluation_start=date_trunc('microseconds',evaluation_start)),
  evaluation_end TIMESTAMPTZ NOT NULL CHECK(evaluation_end=date_trunc('microseconds',evaluation_end) AND evaluation_end>evaluation_start),
  mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  event_count INTEGER NOT NULL CHECK(event_count BETWEEN 1 AND 100000),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('quality-filtered-wheel-scenario',schema_name||'@sha256:'||sha256))
);

CREATE TABLE wheel_v1_source_observations (
  scenario_id UUID NOT NULL REFERENCES wheel_v1_scenarios(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence>=0),
  event_kind TEXT NOT NULL CHECK(event_kind IN ('assess_quality','open_put','open_call','mark','close_option','assignment','expiry','dividend')),
  occurred_at TIMESTAMPTZ NOT NULL CHECK(occurred_at=date_trunc('microseconds',occurred_at)),
  evidence_id UUID NOT NULL,
  evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'),
  canonical_event JSONB NOT NULL CHECK(jsonb_typeof(canonical_event)='object'),
  PRIMARY KEY(scenario_id,sequence)
);

CREATE TABLE wheel_v1_reports (
  id UUID PRIMARY KEY,
  schema_name TEXT NOT NULL CHECK(schema_name='quality-filtered-wheel-report-v1'),
  state TEXT NOT NULL CHECK(state='completed'),
  policy_id UUID NOT NULL REFERENCES wheel_v1_policies(id) ON DELETE RESTRICT,
  policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  scenario_id UUID NOT NULL REFERENCES wheel_v1_scenarios(id) ON DELETE RESTRICT,
  scenario_sha256 TEXT NOT NULL CHECK(scenario_sha256 ~ '^[0-9a-f]{64}$'),
  underlying_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
  initial_capital TEXT NOT NULL,
  evaluation_start TIMESTAMPTZ NOT NULL,
  evaluation_end TIMESTAMPTZ NOT NULL,
  transition_count INTEGER NOT NULL CHECK(transition_count BETWEEN 1 AND 100000),
  ending_cash TEXT NOT NULL, ending_shares TEXT NOT NULL, ending_collateral TEXT NOT NULL,
  ending_option_liability TEXT NOT NULL, ending_net_liquidation TEXT NOT NULL,
  premium_income TEXT NOT NULL, dividend_income TEXT NOT NULL, total_fees TEXT NOT NULL,
  capped_upside TEXT NOT NULL, after_cost_total_return TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('quality-filtered-wheel-report',schema_name||'@sha256:'||sha256))
);

CREATE TABLE wheel_v1_transitions (
  report_id UUID NOT NULL REFERENCES wheel_v1_reports(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence>=0),
  event_kind TEXT NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL CHECK(occurred_at=date_trunc('microseconds',occurred_at)),
  action TEXT NOT NULL, reason TEXT NOT NULL,
  selected_instrument_id UUID,
  cash TEXT NOT NULL, shares TEXT NOT NULL, collateral TEXT NOT NULL,
  option_liability TEXT NOT NULL, net_liquidation TEXT NOT NULL,
  canonical_transition JSONB NOT NULL CHECK(jsonb_typeof(canonical_transition)='object'),
  PRIMARY KEY(report_id,sequence),
  CHECK(evaluation_decimal_valid(cash) AND evaluation_decimal_valid(shares) AND evaluation_decimal_valid(collateral) AND evaluation_decimal_valid(option_liability) AND evaluation_decimal_valid(net_liquidation)),
  CHECK(shares::NUMERIC>=0 AND collateral::NUMERIC>=0 AND option_liability::NUMERIC>=0)
);

CREATE TABLE wheel_v1_economic_effects (
  report_id UUID NOT NULL,
  transition_sequence INTEGER NOT NULL,
  effect_sequence INTEGER NOT NULL CHECK(effect_sequence>=0),
  kind TEXT NOT NULL CHECK(kind IN ('premium','fee','collateral_reserved','buy_to_close','option_liability','put_assignment_purchase','call_assignment_sale','dividend')),
  instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
  quantity TEXT NOT NULL CHECK(evaluation_decimal_valid(quantity)),
  amount TEXT NOT NULL CHECK(evaluation_decimal_valid(amount)),
  evidence_id UUID NOT NULL,
  evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'),
  PRIMARY KEY(report_id,transition_sequence,effect_sequence),
  FOREIGN KEY(report_id,transition_sequence) REFERENCES wheel_v1_transitions(report_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE wheel_v1_selected_contracts (
  report_id UUID NOT NULL,
  transition_sequence INTEGER NOT NULL,
  instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
  PRIMARY KEY(report_id,transition_sequence),
  FOREIGN KEY(report_id,transition_sequence) REFERENCES wheel_v1_transitions(report_id,sequence) ON DELETE RESTRICT
);

CREATE FUNCTION validate_wheel_v1_scenario() RETURNS TRIGGER AS $$
DECLARE target UUID; scenario wheel_v1_scenarios%ROWTYPE; policy wheel_v1_policies%ROWTYPE; reconstructed JSONB;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'scenario_id')::UUID);
  SELECT * INTO scenario FROM wheel_v1_scenarios WHERE id=target;
  SELECT * INTO policy FROM wheel_v1_policies WHERE id=scenario.policy_id;
  SELECT jsonb_build_object('schema',scenario.schema_name,'state',scenario.state,'policy_id',scenario.policy_id::TEXT,'policy_sha256',scenario.policy_sha256,
    'underlying_id',scenario.underlying_id::TEXT,'initial_capital',scenario.initial_capital,
    'evaluation_start',to_char(scenario.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'evaluation_end',to_char(scenario.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'mode',scenario.mode,
    'events',jsonb_agg(source.canonical_event ORDER BY source.sequence)) INTO reconstructed
  FROM wheel_v1_source_observations source WHERE source.scenario_id=target;
  IF scenario.policy_sha256<>policy.sha256 OR scenario.event_count<>(SELECT count(*) FROM wheel_v1_source_observations WHERE scenario_id=target) OR
    NOT EXISTS(SELECT 1 FROM wheel_v1_source_observations WHERE scenario_id=target AND sequence=0 AND occurred_at=scenario.evaluation_start) OR
    NOT EXISTS(SELECT 1 FROM wheel_v1_source_observations WHERE scenario_id=target AND sequence=scenario.event_count-1 AND occurred_at=scenario.evaluation_end) OR
    EXISTS(SELECT 1 FROM wheel_v1_source_observations source WHERE source.scenario_id=target AND
      (source.canonical_event->>'sequence')::INTEGER<>source.sequence OR source.canonical_event->>'kind'<>source.event_kind OR
       (source.canonical_event->>'occurred_at')::TIMESTAMPTZ<>source.occurred_at OR source.canonical_event->>'evidence_id'<>source.evidence_id::TEXT OR
       source.canonical_event->>'evidence_sha256'<>source.evidence_sha256) OR
    EXISTS(SELECT 1 FROM wheel_v1_source_observations current_source JOIN wheel_v1_source_observations prior_source ON prior_source.scenario_id=current_source.scenario_id AND prior_source.sequence=current_source.sequence-1
      WHERE current_source.scenario_id=target AND current_source.sequence>0 AND current_source.occurred_at<=prior_source.occurred_at) OR
    EXISTS(SELECT 1 FROM wheel_v1_source_observations source CROSS JOIN LATERAL jsonb_array_elements(source.canonical_event->'candidates') candidate
      WHERE source.scenario_id=target AND NOT EXISTS(SELECT 1 FROM venue_contracts contract
        WHERE contract.id=(candidate->>'venue_contract_id')::UUID AND contract.instrument_id=(candidate->>'instrument_id')::UUID AND
          source.occurred_at>=contract.valid_from AND (contract.valid_to IS NULL OR source.occurred_at<contract.valid_to))) OR
    EXISTS(SELECT 1 FROM wheel_v1_source_observations source CROSS JOIN LATERAL jsonb_array_elements(source.canonical_event->'candidates') candidate
      WHERE source.scenario_id=target AND NOT EXISTS(SELECT 1 FROM dataset_manifest_observations observation
        WHERE observation.partition_content_sha256=candidate->>'partition_content_sha256' AND observation.source_key=candidate->>'source_key' AND
          observation.content_sha256=candidate->>'evidence_sha256' AND observation.instrument_id=(candidate->>'instrument_id')::UUID AND
          observation.available_at=(candidate->>'available_at')::TIMESTAMPTZ AND observation.available_at<=source.occurred_at)) OR
    reconstructed<>scenario.canonical_json THEN RAISE EXCEPTION 'wheel scenario graph does not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_wheel_v1_report() RETURNS TRIGGER AS $$
DECLARE target UUID; report wheel_v1_reports%ROWTYPE; scenario wheel_v1_scenarios%ROWTYPE; policy wheel_v1_policies%ROWTYPE;
  reconstructed JSONB; last_transition wheel_v1_transitions%ROWTYPE; decimal_pattern TEXT; premium NUMERIC; dividends NUMERIC; fees NUMERIC;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'report_id')::UUID);
  SELECT * INTO report FROM wheel_v1_reports WHERE id=target;
  SELECT * INTO scenario FROM wheel_v1_scenarios WHERE id=report.scenario_id;
  SELECT * INTO policy FROM wheel_v1_policies WHERE id=report.policy_id;
  SELECT jsonb_build_object('schema',report.schema_name,'state',report.state,'policy_id',report.policy_id::TEXT,'policy_sha256',report.policy_sha256,
    'scenario_id',report.scenario_id::TEXT,'scenario_sha256',report.scenario_sha256,'underlying_id',report.underlying_id::TEXT,'initial_capital',report.initial_capital,
    'evaluation_start',to_char(report.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'evaluation_end',to_char(report.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'transitions',jsonb_agg(transition.canonical_transition ORDER BY transition.sequence),
    'ending_cash',report.ending_cash,'ending_shares',report.ending_shares,'ending_collateral',report.ending_collateral,
    'ending_option_liability',report.ending_option_liability,'ending_net_liquidation',report.ending_net_liquidation,
    'premium_income',report.premium_income,'dividend_income',report.dividend_income,'total_fees',report.total_fees,
    'capped_upside',report.capped_upside,'after_cost_total_return',report.after_cost_total_return) INTO reconstructed
  FROM wheel_v1_transitions transition WHERE transition.report_id=target;
  IF report.policy_id<>scenario.policy_id OR report.policy_sha256<>policy.sha256 OR report.scenario_sha256<>scenario.sha256 OR
    report.underlying_id<>scenario.underlying_id OR report.initial_capital<>scenario.initial_capital OR report.evaluation_start<>scenario.evaluation_start OR report.evaluation_end<>scenario.evaluation_end OR
    report.transition_count<>scenario.event_count OR report.transition_count<>(SELECT count(*) FROM wheel_v1_transitions WHERE report_id=target) OR reconstructed<>report.canonical_json OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition JOIN wheel_v1_source_observations source ON source.scenario_id=scenario.id AND source.sequence=transition.sequence
      WHERE transition.report_id=target AND (transition.event_kind<>source.event_kind OR transition.occurred_at<>source.occurred_at)) OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition WHERE transition.report_id=target AND transition.canonical_transition<>
      jsonb_build_object('sequence',transition.sequence,'event_kind',transition.event_kind,'occurred_at',to_char(transition.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        'action',transition.action,'reason',transition.reason,'selected_instrument_id',COALESCE(transition.selected_instrument_id::TEXT,'00000000-0000-0000-0000-000000000000'),
        'cash',transition.cash,'shares',transition.shares,'collateral',transition.collateral,'option_liability',transition.option_liability,'net_liquidation',transition.net_liquidation,
        'effects',COALESCE((SELECT jsonb_agg(jsonb_build_object('kind',effect.kind,'instrument_id',effect.instrument_id::TEXT,'quantity',effect.quantity,'amount',effect.amount,
          'evidence_id',effect.evidence_id::TEXT,'evidence_sha256',effect.evidence_sha256) ORDER BY effect.effect_sequence) FROM wheel_v1_economic_effects effect
          WHERE effect.report_id=target AND effect.transition_sequence=transition.sequence),'[]'::JSONB))) OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition LEFT JOIN wheel_v1_selected_contracts selected ON selected.report_id=transition.report_id AND selected.transition_sequence=transition.sequence
      WHERE transition.report_id=target AND ((transition.selected_instrument_id IS NULL)<>(selected.instrument_id IS NULL) OR transition.selected_instrument_id IS DISTINCT FROM selected.instrument_id))
    THEN RAISE EXCEPTION 'wheel report graph does not reconstruct'; END IF;
  SELECT * INTO last_transition FROM wheel_v1_transitions WHERE report_id=target ORDER BY sequence DESC LIMIT 1;
  decimal_pattern:='^-?[0-9]+\.[0-9]{'||policy.decimal_scale||'}$';
  SELECT COALESCE(sum(amount::NUMERIC) FILTER(WHERE kind='premium'),0),COALESCE(sum(amount::NUMERIC) FILTER(WHERE kind='dividend'),0),COALESCE(-sum(amount::NUMERIC) FILTER(WHERE kind='fee'),0)
    INTO premium,dividends,fees FROM wheel_v1_economic_effects WHERE report_id=target;
  IF report.ending_cash<>last_transition.cash OR report.ending_shares<>last_transition.shares OR report.ending_collateral<>last_transition.collateral OR
    report.ending_option_liability<>last_transition.option_liability OR report.ending_net_liquidation<>last_transition.net_liquidation OR
    report.premium_income::NUMERIC<>premium OR report.dividend_income::NUMERIC<>dividends OR report.total_fees::NUMERIC<>fees OR
    report.after_cost_total_return::NUMERIC<>round(report.ending_net_liquidation::NUMERIC/report.initial_capital::NUMERIC-1,policy.decimal_scale) OR
    report.ending_cash !~ decimal_pattern OR report.ending_shares !~ decimal_pattern OR report.ending_collateral !~ decimal_pattern OR
    report.ending_option_liability !~ decimal_pattern OR report.ending_net_liquidation !~ decimal_pattern OR report.premium_income !~ decimal_pattern OR
    report.dividend_income !~ decimal_pattern OR report.total_fees !~ decimal_pattern OR report.capped_upside !~ decimal_pattern OR report.after_cost_total_return !~ decimal_pattern OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition JOIN wheel_v1_source_observations source ON source.scenario_id=scenario.id AND source.sequence=transition.sequence
      WHERE transition.report_id=target AND transition.net_liquidation::NUMERIC<>round(transition.cash::NUMERIC+transition.shares::NUMERIC*(source.canonical_event->>'underlying_mark')::NUMERIC-transition.option_liability::NUMERIC,policy.decimal_scale))
    THEN RAISE EXCEPTION 'wheel report economics do not reconstruct'; END IF;
  IF EXISTS(SELECT 1 FROM wheel_v1_transitions transition
      LEFT JOIN wheel_v1_transitions prior ON prior.report_id=transition.report_id AND prior.sequence=transition.sequence-1
      WHERE transition.report_id=target AND transition.cash::NUMERIC<>round(CASE WHEN transition.sequence=0 THEN report.initial_capital::NUMERIC ELSE prior.cash::NUMERIC END+
        COALESCE((SELECT sum(effect.amount::NUMERIC) FROM wheel_v1_economic_effects effect WHERE effect.report_id=target AND effect.transition_sequence=transition.sequence AND effect.kind NOT IN ('collateral_reserved','option_liability')),0),policy.decimal_scale)) OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition
      LEFT JOIN wheel_v1_transitions prior ON prior.report_id=transition.report_id AND prior.sequence=transition.sequence-1
      WHERE transition.report_id=target AND transition.shares::NUMERIC<>round(CASE WHEN transition.sequence=0 THEN 0 ELSE prior.shares::NUMERIC END+
        COALESCE((SELECT sum(CASE effect.kind WHEN 'put_assignment_purchase' THEN effect.quantity::NUMERIC WHEN 'call_assignment_sale' THEN -effect.quantity::NUMERIC ELSE 0 END)
          FROM wheel_v1_economic_effects effect WHERE effect.report_id=target AND effect.transition_sequence=transition.sequence),0),policy.decimal_scale)) OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition JOIN wheel_v1_source_observations source ON source.scenario_id=scenario.id AND source.sequence=transition.sequence
      WHERE transition.report_id=target AND transition.action IN ('short_put_opened','covered_call_opened') AND transition.selected_instrument_id IS DISTINCT FROM (
        SELECT (candidate->>'instrument_id')::UUID FROM jsonb_array_elements(source.canonical_event->'candidates') candidate
        WHERE candidate->>'option_type'=CASE transition.action WHEN 'short_put_opened' THEN 'put' ELSE 'call' END AND
          abs((candidate->>'delta')::NUMERIC) BETWEEN (policy.canonical_json->>CASE transition.action WHEN 'short_put_opened' THEN 'put_delta_minimum' ELSE 'call_delta_minimum' END)::NUMERIC AND
            (policy.canonical_json->>CASE transition.action WHEN 'short_put_opened' THEN 'put_delta_maximum' ELSE 'call_delta_maximum' END)::NUMERIC AND
          floor(extract(epoch FROM ((candidate->>'expiry')::TIMESTAMPTZ-transition.occurred_at))/86400) BETWEEN (policy.canonical_json->>'minimum_dte')::INTEGER AND (policy.canonical_json->>'maximum_dte')::INTEGER AND
          (candidate->>'open_interest')::NUMERIC>=(policy.canonical_json->>'minimum_open_interest')::NUMERIC AND (candidate->>'volume')::NUMERIC>=(policy.canonical_json->>'minimum_volume')::NUMERIC AND
          transition.occurred_at-(candidate->>'available_at')::TIMESTAMPTZ<=make_interval(secs=>(policy.canonical_json->>'maximum_market_data_age_seconds')::INTEGER)
        ORDER BY abs(abs((candidate->>'delta')::NUMERIC)-(policy.canonical_json->>CASE transition.action WHEN 'short_put_opened' THEN 'put_delta_target' ELSE 'call_delta_target' END)::NUMERIC),
          floor(extract(epoch FROM ((candidate->>'expiry')::TIMESTAMPTZ-transition.occurred_at))/86400),(candidate->>'strike')::NUMERIC,candidate->>'instrument_id' LIMIT 1)) OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition LEFT JOIN wheel_v1_transitions prior ON prior.report_id=transition.report_id AND prior.sequence=transition.sequence-1
      WHERE transition.report_id=target AND transition.action='covered_call_opened' AND COALESCE(prior.shares::NUMERIC,0)<
        (policy.canonical_json->>'deliverable_quantity')::NUMERIC*(SELECT effect.quantity::NUMERIC FROM wheel_v1_economic_effects effect WHERE effect.report_id=target AND effect.transition_sequence=transition.sequence AND effect.kind='premium' LIMIT 1)) OR
    EXISTS(SELECT 1 FROM wheel_v1_transitions transition JOIN wheel_v1_source_observations source ON source.scenario_id=scenario.id AND source.sequence=transition.sequence
      WHERE transition.report_id=target AND transition.action='short_put_opened' AND transition.collateral::NUMERIC<>
        (policy.canonical_json->>'deliverable_quantity')::NUMERIC*(SELECT (candidate->>'strike')::NUMERIC FROM jsonb_array_elements(source.canonical_event->'candidates') candidate WHERE candidate->>'instrument_id'=transition.selected_instrument_id::TEXT LIMIT 1)*
        (SELECT effect.quantity::NUMERIC FROM wheel_v1_economic_effects effect WHERE effect.report_id=target AND effect.transition_sequence=transition.sequence AND effect.kind='premium' LIMIT 1))
    THEN RAISE EXCEPTION 'wheel report continuity, selection, collateral, or coverage does not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_wheel_v1_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'wheel v1 evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE table_name TEXT; BEGIN FOREACH table_name IN ARRAY ARRAY['wheel_v1_policies','wheel_v1_scenarios','wheel_v1_source_observations','wheel_v1_reports','wheel_v1_transitions','wheel_v1_economic_effects','wheel_v1_selected_contracts'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_wheel_v1_mutation()','trg_'||table_name||'_immutable',table_name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_wheel_v1_scenario_graph AFTER INSERT ON wheel_v1_scenarios DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_wheel_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_wheel_v1_source_graph AFTER INSERT ON wheel_v1_source_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_wheel_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_wheel_v1_report_graph AFTER INSERT ON wheel_v1_reports DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_wheel_v1_report();
CREATE CONSTRAINT TRIGGER trg_wheel_v1_transition_graph AFTER INSERT ON wheel_v1_transitions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_wheel_v1_report();
CREATE CONSTRAINT TRIGGER trg_wheel_v1_effect_graph AFTER INSERT ON wheel_v1_economic_effects DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_wheel_v1_report();
CREATE CONSTRAINT TRIGGER trg_wheel_v1_selection_graph AFTER INSERT ON wheel_v1_selected_contracts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_wheel_v1_report();
CREATE INDEX idx_wheel_v1_scenarios_policy ON wheel_v1_scenarios(policy_id,created_at,id);
CREATE INDEX idx_wheel_v1_reports_scenario ON wheel_v1_reports(scenario_id,created_at,id);
