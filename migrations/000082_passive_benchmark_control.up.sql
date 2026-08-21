LOCK TABLE research_experiments,dataset_manifests,instruments,trade_portfolio_evaluations,evaluation_observations IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE passive_benchmark_declarations (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='passive-benchmark-declaration-v1'), state TEXT NOT NULL CHECK(state='declared'),
  experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  experiment_sha256 TEXT NOT NULL CHECK(experiment_sha256 ~ '^[0-9a-f]{64}$'),
  manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
  manifest_sha256 TEXT NOT NULL CHECK(manifest_sha256 ~ '^[0-9a-f]{64}$'),
  benchmark_instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
  benchmark_kind TEXT NOT NULL CHECK(benchmark_kind IN ('buy_and_hold','total_return_index')),
  weighting TEXT NOT NULL CHECK(weighting='single_asset'), distribution_treatment TEXT NOT NULL CHECK(distribution_treatment='reinvested'),
  cash_convention TEXT NOT NULL CHECK(cash_convention='explicit_per_period'), frequency TEXT NOT NULL CHECK(frequency IN ('minute','daily','weekly','monthly')),
  evaluation_start TIMESTAMPTZ NOT NULL CHECK(evaluation_start=date_trunc('microseconds',evaluation_start)),
  evaluation_end TIMESTAMPTZ NOT NULL CHECK(evaluation_end=date_trunc('microseconds',evaluation_end) AND evaluation_end>evaluation_start),
  initial_notional TEXT NOT NULL CHECK(evaluation_decimal_valid(initial_notional) AND initial_notional::NUMERIC>0),
  decimal_scale INTEGER NOT NULL CHECK(decimal_scale BETWEEN 6 AND 18), observation_count INTEGER NOT NULL CHECK(observation_count BETWEEN 2 AND 100000),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('passive-benchmark-declaration',schema_name||'@sha256:'||sha256))
);

CREATE TABLE passive_benchmark_observations (
  declaration_id UUID NOT NULL REFERENCES passive_benchmark_declarations(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK(sequence>=0), observed_at TIMESTAMPTZ NOT NULL CHECK(observed_at=date_trunc('microseconds',observed_at)),
  benchmark_value TEXT NOT NULL CHECK(evaluation_decimal_valid(benchmark_value) AND benchmark_value::NUMERIC>0),
  cash_return TEXT NOT NULL CHECK(evaluation_decimal_valid(cash_return) AND cash_return::NUMERIC>-1),
  evidence_id UUID NOT NULL, evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256 ~ '^[0-9a-f]{64}$'),
  PRIMARY KEY(declaration_id,sequence)
);

CREATE TABLE benchmark_opportunity_cost_reports (
  id UUID PRIMARY KEY, schema_name TEXT NOT NULL CHECK(schema_name='benchmark-opportunity-cost-report-v1'), state TEXT NOT NULL CHECK(state='completed'),
  declaration_id UUID NOT NULL REFERENCES passive_benchmark_declarations(id) ON DELETE RESTRICT,
  declaration_sha256 TEXT NOT NULL CHECK(declaration_sha256 ~ '^[0-9a-f]{64}$'),
  evaluation_id UUID NOT NULL REFERENCES trade_portfolio_evaluations(id) ON DELETE RESTRICT,
  evaluation_sha256 TEXT NOT NULL CHECK(evaluation_sha256 ~ '^[0-9a-f]{64}$'),
  experiment_id UUID NOT NULL REFERENCES research_experiments(id) ON DELETE RESTRICT,
  manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
  benchmark_instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
  strategy_total_return TEXT NOT NULL, benchmark_total_return TEXT NOT NULL, cash_total_return TEXT NOT NULL,
  benchmark_opportunity_cost TEXT NOT NULL, cash_opportunity_cost TEXT NOT NULL,
  strategy_terminal_wealth TEXT NOT NULL, benchmark_terminal_wealth TEXT NOT NULL, cash_terminal_wealth TEXT NOT NULL,
  benchmark_wealth_difference TEXT NOT NULL, cash_wealth_difference TEXT NOT NULL,
  observation_count INTEGER NOT NULL CHECK(observation_count BETWEEN 2 AND 100000),
  sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL CHECK(created_at=date_trunc('microseconds',created_at)),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')), CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(id=economic_deterministic_uuid('benchmark-opportunity-cost-report',schema_name||'@sha256:'||sha256))
);

CREATE FUNCTION validate_passive_benchmark_declaration() RETURNS TRIGGER AS $$
DECLARE target UUID; declaration passive_benchmark_declarations%ROWTYPE; first_at TIMESTAMPTZ; last_at TIMESTAMPTZ;
BEGIN
  target:=COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'declaration_id')::UUID);
  SELECT * INTO declaration FROM passive_benchmark_declarations WHERE id=target;
  SELECT min(observed_at),max(observed_at) INTO first_at,last_at FROM passive_benchmark_observations WHERE declaration_id=target;
  PERFORM 1 FROM research_experiments experiment JOIN dataset_manifests manifest ON manifest.id=declaration.manifest_id
    WHERE experiment.id=declaration.experiment_id AND declaration.experiment_sha256=experiment.sha256 AND
      declaration.manifest_id=experiment.manifest_id AND declaration.manifest_sha256=manifest.sha256 AND
      declaration.evaluation_start=experiment.evaluation_start AND declaration.evaluation_end=experiment.evaluation_end AND
      declaration.observation_count=(SELECT count(*) FROM passive_benchmark_observations WHERE declaration_id=target) AND
      first_at=declaration.evaluation_start AND last_at=declaration.evaluation_end AND
      (SELECT min(sequence)=0 AND max(sequence)=declaration.observation_count-1 FROM passive_benchmark_observations WHERE declaration_id=target) AND
      NOT EXISTS(SELECT 1 FROM passive_benchmark_observations current_observation
        JOIN passive_benchmark_observations prior_observation ON prior_observation.declaration_id=current_observation.declaration_id AND prior_observation.sequence=current_observation.sequence-1
        WHERE current_observation.declaration_id=target AND current_observation.sequence>0 AND
          CASE declaration.frequency WHEN 'minute' THEN current_observation.observed_at<>prior_observation.observed_at+INTERVAL '1 minute'
            WHEN 'daily' THEN current_observation.observed_at<>prior_observation.observed_at+INTERVAL '1 day'
            WHEN 'weekly' THEN current_observation.observed_at<>prior_observation.observed_at+INTERVAL '7 days'
            WHEN 'monthly' THEN current_observation.observed_at<>date_trunc('month',prior_observation.observed_at)+INTERVAL '1 month'+
              (extract(day FROM prior_observation.observed_at)-1)*INTERVAL '1 day'+(prior_observation.observed_at-date_trunc('day',prior_observation.observed_at)) END) AND
      declaration.canonical_json=jsonb_build_object('schema',declaration.schema_name,'state',declaration.state,
        'experiment_id',declaration.experiment_id::TEXT,'experiment_sha256',declaration.experiment_sha256,
        'manifest_id',declaration.manifest_id::TEXT,'manifest_sha256',declaration.manifest_sha256,
        'benchmark_instrument_id',declaration.benchmark_instrument_id::TEXT,'benchmark_kind',declaration.benchmark_kind,
        'weighting',declaration.weighting,'distribution_treatment',declaration.distribution_treatment,'cash_convention',declaration.cash_convention,
        'frequency',declaration.frequency,'evaluation_start',to_char(declaration.evaluation_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        'evaluation_end',to_char(declaration.evaluation_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        'initial_notional',declaration.initial_notional,'decimal_scale',declaration.decimal_scale,'observations',(
          SELECT jsonb_agg(jsonb_build_object('sequence',observation.sequence,'observed_at',to_char(observation.observed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'value',observation.benchmark_value,'cash_return',observation.cash_return,'evidence_id',observation.evidence_id::TEXT,
            'evidence_sha256',observation.evidence_sha256) ORDER BY observation.sequence)
          FROM passive_benchmark_observations observation WHERE observation.declaration_id=target));
  IF NOT FOUND THEN RAISE EXCEPTION 'passive benchmark declaration graph does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_benchmark_opportunity_cost_report() RETURNS TRIGGER AS $$
DECLARE report benchmark_opportunity_cost_reports%ROWTYPE; declaration passive_benchmark_declarations%ROWTYPE;
  strategy_return NUMERIC; benchmark_return NUMERIC; cash_growth NUMERIC:=1; source RECORD; expected_pattern TEXT;
BEGIN
  SELECT * INTO report FROM benchmark_opportunity_cost_reports WHERE id=NEW.id;
  SELECT * INTO declaration FROM passive_benchmark_declarations WHERE id=report.declaration_id;
  PERFORM 1 FROM trade_portfolio_evaluations evaluation_report
    WHERE evaluation_report.id=report.evaluation_id AND report.declaration_sha256=declaration.sha256 AND
      report.evaluation_sha256=evaluation_report.sha256 AND report.experiment_id=declaration.experiment_id AND
      report.experiment_id=evaluation_report.experiment_id AND report.manifest_id=declaration.manifest_id AND
      report.manifest_id=evaluation_report.manifest_id AND report.benchmark_instrument_id=declaration.benchmark_instrument_id AND
      evaluation_report.evaluation_start=declaration.evaluation_start AND evaluation_report.evaluation_end=declaration.evaluation_end AND
      report.observation_count=declaration.observation_count AND report.observation_count=evaluation_report.observation_count AND
      NOT EXISTS(SELECT 1 FROM passive_benchmark_observations benchmark_observation
        FULL JOIN evaluation_observations evaluation_observation ON evaluation_observation.evaluation_id=report.evaluation_id AND evaluation_observation.sequence=benchmark_observation.sequence
        WHERE benchmark_observation.declaration_id=declaration.id AND (evaluation_observation.sequence IS NULL OR
          benchmark_observation.observed_at<>evaluation_observation.observed_at OR benchmark_observation.benchmark_value<>evaluation_observation.benchmark_value OR
          benchmark_observation.cash_return<>evaluation_observation.cash_return OR benchmark_observation.evidence_id<>evaluation_observation.evidence_id OR
          benchmark_observation.evidence_sha256<>evaluation_observation.evidence_sha256));
  IF NOT FOUND THEN RAISE EXCEPTION 'benchmark opportunity-cost parents or curves do not match'; END IF;
  SELECT last_value(equity) OVER (ORDER BY sequence ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)/first_value(equity) OVER (ORDER BY sequence)-1
    INTO strategy_return FROM (SELECT sequence,equity::NUMERIC equity FROM evaluation_observations WHERE evaluation_id=report.evaluation_id) values ORDER BY sequence LIMIT 1;
  SELECT last_value(benchmark_value) OVER (ORDER BY sequence ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)/first_value(benchmark_value) OVER (ORDER BY sequence)-1
    INTO benchmark_return FROM (SELECT sequence,benchmark_value::NUMERIC benchmark_value FROM passive_benchmark_observations WHERE declaration_id=declaration.id) values ORDER BY sequence LIMIT 1;
  FOR source IN SELECT cash_return::NUMERIC cash_return FROM passive_benchmark_observations WHERE declaration_id=declaration.id AND sequence>0 ORDER BY sequence LOOP
    cash_growth:=cash_growth*(1+source.cash_return);
  END LOOP;
  expected_pattern:='^-?[0-9]+\.[0-9]{'||declaration.decimal_scale||'}$';
  IF report.strategy_total_return !~ expected_pattern OR report.benchmark_total_return !~ expected_pattern OR report.cash_total_return !~ expected_pattern OR
    report.benchmark_opportunity_cost !~ expected_pattern OR report.cash_opportunity_cost !~ expected_pattern OR report.strategy_terminal_wealth !~ expected_pattern OR
    report.benchmark_terminal_wealth !~ expected_pattern OR report.cash_terminal_wealth !~ expected_pattern OR report.benchmark_wealth_difference !~ expected_pattern OR
    report.cash_wealth_difference !~ expected_pattern OR report.strategy_total_return::NUMERIC<>round(strategy_return,declaration.decimal_scale) OR
    report.benchmark_total_return::NUMERIC<>round(benchmark_return,declaration.decimal_scale) OR report.cash_total_return::NUMERIC<>round(cash_growth-1,declaration.decimal_scale) OR
    report.benchmark_opportunity_cost::NUMERIC<>round(benchmark_return-strategy_return,declaration.decimal_scale) OR
    report.cash_opportunity_cost::NUMERIC<>round((cash_growth-1)-strategy_return,declaration.decimal_scale) OR
    report.strategy_terminal_wealth::NUMERIC<>round(declaration.initial_notional::NUMERIC*(1+strategy_return),declaration.decimal_scale) OR
    report.benchmark_terminal_wealth::NUMERIC<>round(declaration.initial_notional::NUMERIC*(1+benchmark_return),declaration.decimal_scale) OR
    report.cash_terminal_wealth::NUMERIC<>round(declaration.initial_notional::NUMERIC*cash_growth,declaration.decimal_scale) OR
    report.benchmark_wealth_difference::NUMERIC<>round(declaration.initial_notional::NUMERIC*(benchmark_return-strategy_return),declaration.decimal_scale) OR
    report.cash_wealth_difference::NUMERIC<>round(declaration.initial_notional::NUMERIC*((cash_growth-1)-strategy_return),declaration.decimal_scale) OR
    report.canonical_json<>jsonb_build_object('schema',report.schema_name,'state',report.state,'declaration_id',report.declaration_id::TEXT,
      'declaration_sha256',report.declaration_sha256,'evaluation_id',report.evaluation_id::TEXT,'evaluation_sha256',report.evaluation_sha256,
      'experiment_id',report.experiment_id::TEXT,'manifest_id',report.manifest_id::TEXT,'benchmark_instrument_id',report.benchmark_instrument_id::TEXT,
      'strategy_total_return',report.strategy_total_return,'benchmark_total_return',report.benchmark_total_return,'cash_total_return',report.cash_total_return,
      'benchmark_opportunity_cost',report.benchmark_opportunity_cost,'cash_opportunity_cost',report.cash_opportunity_cost,
      'strategy_terminal_wealth',report.strategy_terminal_wealth,'benchmark_terminal_wealth',report.benchmark_terminal_wealth,
      'cash_terminal_wealth',report.cash_terminal_wealth,'benchmark_wealth_difference',report.benchmark_wealth_difference,
      'cash_wealth_difference',report.cash_wealth_difference,'observation_count',report.observation_count)
    THEN RAISE EXCEPTION 'benchmark opportunity-cost report does not reconstruct'; END IF; RETURN NULL;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_passive_benchmark_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'passive benchmark evidence is append-only'; END; $$ LANGUAGE plpgsql;
DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['passive_benchmark_declarations','passive_benchmark_observations','benchmark_opportunity_cost_reports'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_passive_benchmark_mutation()','trg_'||name||'_immutable',name); END LOOP; END $$;
CREATE CONSTRAINT TRIGGER trg_passive_benchmark_declaration_graph AFTER INSERT ON passive_benchmark_declarations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_passive_benchmark_declaration();
CREATE CONSTRAINT TRIGGER trg_passive_benchmark_observation_graph AFTER INSERT ON passive_benchmark_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_passive_benchmark_declaration();
CREATE CONSTRAINT TRIGGER trg_benchmark_opportunity_cost_report_graph AFTER INSERT ON benchmark_opportunity_cost_reports DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_benchmark_opportunity_cost_report();
CREATE INDEX idx_passive_benchmark_declarations_experiment ON passive_benchmark_declarations(experiment_id,created_at,id);
CREATE INDEX idx_passive_benchmark_declarations_instrument ON passive_benchmark_declarations(benchmark_instrument_id,created_at,id);
CREATE INDEX idx_benchmark_opportunity_reports_evaluation ON benchmark_opportunity_cost_reports(evaluation_id,created_at,id);
CREATE INDEX idx_benchmark_opportunity_reports_experiment ON benchmark_opportunity_cost_reports(experiment_id,created_at,id);
