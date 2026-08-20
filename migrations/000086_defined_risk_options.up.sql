LOCK TABLE instruments,venue_contracts IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE defined_risk_v1_policies (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='defined-risk-options-policy-v1'), version TEXT NOT NULL,
  execution_mode TEXT NOT NULL CHECK(execution_mode IN ('atomic_package','sequential_protective_first')),
  decimal_scale INTEGER NOT NULL CHECK(decimal_scale BETWEEN 6 AND 18), sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'version'=version AND canonical_json->>'execution_mode'=execution_mode AND (canonical_json->>'decimal_scale')::INTEGER=decimal_scale),
  CHECK(id=economic_deterministic_uuid('defined-risk-options-policy',schema_name||'@sha256:'||sha256))
);

CREATE TABLE defined_risk_v1_scenarios (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='defined-risk-options-scenario-v1'), state TEXT NOT NULL CHECK(state='declared'),
  policy_id UUID NOT NULL REFERENCES defined_risk_v1_policies(id) ON DELETE RESTRICT, policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  strategy TEXT NOT NULL CHECK(strategy IN ('bull_call','bear_put','bull_put','bear_call')), initial_capital TEXT NOT NULL CHECK(evaluation_decimal_valid(initial_capital) AND initial_capital::NUMERIC>0),
  requested_contracts INTEGER NOT NULL CHECK(requested_contracts>0), decision_at TIMESTAMPTZ NOT NULL CHECK(decision_at=date_trunc('microseconds',decision_at)),
  expiry_at TIMESTAMPTZ NOT NULL CHECK(expiry_at=date_trunc('microseconds',expiry_at) AND expiry_at>decision_at), mode TEXT NOT NULL CHECK(mode IN ('paper_scored','paper_stress')),
  terminal_underlying TEXT NOT NULL CHECK(evaluation_decimal_valid(terminal_underlying) AND terminal_underlying::NUMERIC>0), terminal_available_at TIMESTAMPTZ NOT NULL CHECK(terminal_available_at=date_trunc('microseconds',terminal_available_at)),
  terminal_evidence_id UUID NOT NULL, terminal_evidence_sha256 TEXT NOT NULL CHECK(terminal_evidence_sha256 ~ '^[0-9a-f]{64}$'), terminal_partition_sha256 TEXT NOT NULL CHECK(terminal_partition_sha256 ~ '^[0-9a-f]{64}$'), terminal_source_key TEXT NOT NULL CHECK(length(terminal_source_key)>0),
  leg_count INTEGER NOT NULL CHECK(leg_count=2), sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('defined-risk-options-scenario',schema_name||'@sha256:'||sha256))
);

CREATE TABLE defined_risk_v1_legs (
  scenario_id UUID NOT NULL REFERENCES defined_risk_v1_scenarios(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence IN (0,1)),
  instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT, venue_contract_id UUID NOT NULL REFERENCES venue_contracts(id) ON DELETE RESTRICT,
  option_type TEXT NOT NULL CHECK(option_type IN ('call','put')), strike TEXT NOT NULL CHECK(evaluation_decimal_valid(strike) AND strike::NUMERIC>0),
  position TEXT NOT NULL CHECK(position IN ('long','short')), canonical_leg JSONB NOT NULL CHECK(jsonb_typeof(canonical_leg)='object'),
  PRIMARY KEY(scenario_id,sequence), UNIQUE(scenario_id,instrument_id), UNIQUE(scenario_id,venue_contract_id)
);

CREATE TABLE defined_risk_v1_observations (
  scenario_id UUID NOT NULL, leg_sequence INTEGER NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('entry','unwind')),
  evidence_id UUID NOT NULL, evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'), available_at TIMESTAMPTZ NOT NULL CHECK(available_at=date_trunc('microseconds',available_at)),
  canonical_quote JSONB NOT NULL CHECK(jsonb_typeof(canonical_quote)='object'), PRIMARY KEY(scenario_id,leg_sequence,kind),
  FOREIGN KEY(scenario_id,leg_sequence) REFERENCES defined_risk_v1_legs(scenario_id,sequence) ON DELETE RESTRICT
);

CREATE TABLE defined_risk_v1_reports (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='defined-risk-options-report-v1'), state TEXT NOT NULL CHECK(state='completed'),
  policy_id UUID NOT NULL REFERENCES defined_risk_v1_policies(id) ON DELETE RESTRICT, policy_sha256 TEXT NOT NULL CHECK(policy_sha256 ~ '^[0-9a-f]{64}$'),
  scenario_id UUID NOT NULL REFERENCES defined_risk_v1_scenarios(id) ON DELETE RESTRICT, scenario_sha256 TEXT NOT NULL CHECK(scenario_sha256 ~ '^[0-9a-f]{64}$'),
  strategy TEXT NOT NULL CHECK(strategy IN ('bull_call','bear_put','bull_put','bear_call')), execution_mode TEXT NOT NULL CHECK(execution_mode IN ('atomic_package','sequential_protective_first')),
  outcome TEXT NOT NULL CHECK(outcome IN ('rejected','settled','orphan_unwound')), reason TEXT NOT NULL, contracts INTEGER NOT NULL CHECK(contracts>=0), fill_count INTEGER NOT NULL CHECK(fill_count BETWEEN 0 AND 2),
  width TEXT NOT NULL, net_premium_per_contract TEXT NOT NULL, maximum_loss_per_contract TEXT NOT NULL, maximum_reward_per_contract TEXT NOT NULL, orphan_reserve_per_contract TEXT NOT NULL,
  reserved_capital TEXT NOT NULL, entry_fees TEXT NOT NULL, unwind_fees TEXT NOT NULL, orphan_loss TEXT NOT NULL, expiration_payoff TEXT NOT NULL, ending_cash TEXT NOT NULL, after_cost_total_return TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('defined-risk-options-report',schema_name||'@sha256:'||sha256)),
  CHECK(evaluation_decimal_valid(width) AND evaluation_decimal_valid(net_premium_per_contract) AND evaluation_decimal_valid(maximum_loss_per_contract) AND evaluation_decimal_valid(maximum_reward_per_contract) AND evaluation_decimal_valid(orphan_reserve_per_contract) AND evaluation_decimal_valid(reserved_capital) AND evaluation_decimal_valid(entry_fees) AND evaluation_decimal_valid(unwind_fees) AND evaluation_decimal_valid(orphan_loss) AND evaluation_decimal_valid(expiration_payoff) AND evaluation_decimal_valid(ending_cash) AND evaluation_decimal_valid(after_cost_total_return)),
  CHECK(width::NUMERIC>0 AND maximum_loss_per_contract::NUMERIC>0 AND maximum_reward_per_contract::NUMERIC>=0 AND orphan_reserve_per_contract::NUMERIC>=0 AND reserved_capital::NUMERIC>=0 AND entry_fees::NUMERIC>=0 AND unwind_fees::NUMERIC>=0 AND orphan_loss::NUMERIC>=0 AND ending_cash::NUMERIC>0)
);

CREATE TABLE defined_risk_v1_fills (
  report_id UUID NOT NULL REFERENCES defined_risk_v1_reports(id) ON DELETE RESTRICT, sequence INTEGER NOT NULL CHECK(sequence IN (0,1)),
  instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT, action TEXT NOT NULL CHECK(action IN ('open','unwind')),
  quantity INTEGER NOT NULL CHECK(quantity>0), price TEXT NOT NULL CHECK(evaluation_decimal_valid(price) AND price::NUMERIC>0), fee TEXT NOT NULL CHECK(evaluation_decimal_valid(fee) AND fee::NUMERIC>=0),
  evidence_id UUID NOT NULL, evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'), canonical_fill JSONB NOT NULL CHECK(jsonb_typeof(canonical_fill)='object'), PRIMARY KEY(report_id,sequence)
);

CREATE FUNCTION validate_defined_risk_v1_scenario() RETURNS TRIGGER AS $$
DECLARE target UUID; scenario defined_risk_v1_scenarios%ROWTYPE; policy defined_risk_v1_policies%ROWTYPE;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'scenario_id')::UUID);
  SELECT * INTO scenario FROM defined_risk_v1_scenarios WHERE id=target; SELECT * INTO policy FROM defined_risk_v1_policies WHERE id=scenario.policy_id;
  IF scenario.policy_sha256<>policy.sha256 OR scenario.leg_count<>(SELECT count(*) FROM defined_risk_v1_legs WHERE scenario_id=target) OR
    scenario.canonical_json->'legs'<>COALESCE((SELECT jsonb_agg(leg.canonical_leg ORDER BY leg.sequence) FROM defined_risk_v1_legs leg WHERE leg.scenario_id=target),'[]'::JSONB) OR
    scenario.canonical_json->>'strategy'<>scenario.strategy OR scenario.canonical_json->>'initial_capital'<>scenario.initial_capital OR (scenario.canonical_json->>'requested_contracts')::INTEGER<>scenario.requested_contracts OR scenario.canonical_json->>'mode'<>scenario.mode OR scenario.canonical_json->>'terminal_underlying'<>scenario.terminal_underlying OR scenario.canonical_json->>'terminal_evidence_id'<>scenario.terminal_evidence_id::TEXT OR scenario.canonical_json->>'terminal_evidence_sha256'<>scenario.terminal_evidence_sha256 OR scenario.canonical_json->>'terminal_partition_content_sha256'<>scenario.terminal_partition_sha256 OR scenario.canonical_json->>'terminal_source_key'<>scenario.terminal_source_key OR
    EXISTS(SELECT 1 FROM defined_risk_v1_legs leg WHERE leg.scenario_id=target AND (leg.canonical_leg->'entry'<>COALESCE((SELECT observation.canonical_quote FROM defined_risk_v1_observations observation WHERE observation.scenario_id=target AND observation.leg_sequence=leg.sequence AND observation.kind='entry'),'null'::JSONB) OR leg.canonical_leg->'unwind'<>COALESCE((SELECT observation.canonical_quote FROM defined_risk_v1_observations observation WHERE observation.scenario_id=target AND observation.leg_sequence=leg.sequence AND observation.kind='unwind'),'null'::JSONB))) OR
    (policy.execution_mode='atomic_package' AND (SELECT count(*) FROM defined_risk_v1_observations WHERE scenario_id=target)<>2) OR (policy.execution_mode='sequential_protective_first' AND (SELECT count(*) FROM defined_risk_v1_observations WHERE scenario_id=target)<>3)
    THEN RAISE EXCEPTION 'defined-risk scenario graph does not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_defined_risk_v1_report() RETURNS TRIGGER AS $$
DECLARE target UUID; report defined_risk_v1_reports%ROWTYPE; scenario defined_risk_v1_scenarios%ROWTYPE; policy defined_risk_v1_policies%ROWTYPE;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'report_id')::UUID);
  SELECT * INTO report FROM defined_risk_v1_reports WHERE id=target; SELECT * INTO scenario FROM defined_risk_v1_scenarios WHERE id=report.scenario_id; SELECT * INTO policy FROM defined_risk_v1_policies WHERE id=report.policy_id;
  IF report.policy_id<>scenario.policy_id OR report.policy_sha256<>policy.sha256 OR report.scenario_sha256<>scenario.sha256 OR report.strategy<>scenario.strategy OR report.execution_mode<>policy.execution_mode OR report.fill_count<>(SELECT count(*) FROM defined_risk_v1_fills WHERE report_id=target) OR
    report.canonical_json->'fills'<>COALESCE((SELECT jsonb_agg(fill.canonical_fill ORDER BY fill.sequence) FROM defined_risk_v1_fills fill WHERE fill.report_id=target),'[]'::JSONB) OR
    report.canonical_json->>'outcome'<>report.outcome OR report.canonical_json->>'reason'<>report.reason OR (report.canonical_json->>'contracts')::INTEGER<>report.contracts OR report.canonical_json->>'reserved_capital'<>report.reserved_capital OR report.canonical_json->>'orphan_loss'<>report.orphan_loss OR report.canonical_json->>'ending_cash'<>report.ending_cash OR
    (report.outcome='rejected' AND (report.contracts<>0 OR report.fill_count<>0 OR report.reserved_capital::NUMERIC<>0)) OR (report.outcome IN ('settled','orphan_unwound') AND (report.contracts=0 OR report.fill_count<>2 OR report.reserved_capital::NUMERIC<=0))
    THEN RAISE EXCEPTION 'defined-risk report graph does not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_defined_risk_v1_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'defined-risk v1 evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE table_name TEXT; BEGIN FOREACH table_name IN ARRAY ARRAY['defined_risk_v1_policies','defined_risk_v1_scenarios','defined_risk_v1_legs','defined_risk_v1_observations','defined_risk_v1_reports','defined_risk_v1_fills'] LOOP EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_defined_risk_v1_mutation()','trg_'||table_name||'_immutable',table_name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_defined_risk_v1_scenario_graph AFTER INSERT ON defined_risk_v1_scenarios DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_defined_risk_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_defined_risk_v1_leg_graph AFTER INSERT ON defined_risk_v1_legs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_defined_risk_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_defined_risk_v1_observation_graph AFTER INSERT ON defined_risk_v1_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_defined_risk_v1_scenario();
CREATE CONSTRAINT TRIGGER trg_defined_risk_v1_report_graph AFTER INSERT ON defined_risk_v1_reports DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_defined_risk_v1_report();
CREATE CONSTRAINT TRIGGER trg_defined_risk_v1_fill_graph AFTER INSERT ON defined_risk_v1_fills DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_defined_risk_v1_report();
CREATE INDEX idx_defined_risk_v1_scenarios_policy ON defined_risk_v1_scenarios(policy_id,created_at,id);
CREATE INDEX idx_defined_risk_v1_reports_scenario ON defined_risk_v1_reports(scenario_id,created_at,id);
