-- Immutable prepared-only multi-session copy target drift evidence.
LOCK TABLE copy_subscriptions, copy_source_observations IN ACCESS SHARE MODE;

CREATE TABLE copy_target_drift_runs (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name='copy-target-drift-session-v1'),
    state TEXT NOT NULL CHECK (state='prepared'),
    subscription_id UUID NOT NULL REFERENCES copy_subscriptions(id),
    origin_type TEXT NOT NULL CHECK (origin_type='copy_subscription'),
    origin_id UUID NOT NULL,
    source_observation_id UUID NOT NULL REFERENCES copy_source_observations(id),
    session_key TEXT NOT NULL CHECK (session_key ~ '^\d{4}-\d{2}-\d{2}/(regular|pre_market|after_hours)$'),
    calculation_version INT NOT NULL CHECK (calculation_version=1),
    maximum_session_turnover NUMERIC(24,2) NOT NULL CHECK (maximum_session_turnover > 0),
    session_budget NUMERIC(24,2) NOT NULL CHECK (session_budget >= 0 AND session_budget <= maximum_session_turnover),
    starting_drift NUMERIC(24,2) NOT NULL CHECK (starting_drift >= 0),
    prepared_turnover NUMERIC(24,2) NOT NULL CHECK (prepared_turnover >= 0 AND prepared_turnover <= session_budget AND prepared_turnover <= starting_drift),
    residual_drift NUMERIC(24,2) NOT NULL CHECK (residual_drift = starting_drift-prepared_turnover),
    converged BOOLEAN NOT NULL CHECK (converged = (residual_drift=0)),
    leg_count INT NOT NULL CHECK (leg_count >= 0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes)>0),
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (origin_id=subscription_id),
    CHECK (convert_from(canonical_bytes,'UTF8')::jsonb=canonical_json),
    CHECK ((starting_drift=0 AND session_budget>=0) OR (starting_drift>0 AND session_budget>0)),
    UNIQUE (subscription_id,source_observation_id,session_key,calculation_version)
);

CREATE TABLE copy_target_drift_legs (
    run_id UUID NOT NULL REFERENCES copy_target_drift_runs(id),
    sequence INT NOT NULL CHECK (sequence >= 0),
    instrument_key TEXT NOT NULL CHECK (instrument_key<>'' AND instrument_key=btrim(instrument_key) AND instrument_key=upper(instrument_key)),
    side TEXT NOT NULL CHECK (side IN ('buy','sell')),
    current_value NUMERIC(24,2) NOT NULL CHECK (current_value >= 0),
    target_value NUMERIC(24,2) NOT NULL CHECK (target_value >= 0),
    starting_drift NUMERIC(24,2) NOT NULL CHECK (starting_drift=abs(target_value-current_value) AND starting_drift>0),
    requested_notional NUMERIC(24,2) NOT NULL CHECK (requested_notional>0 AND requested_notional<=starting_drift),
    projected_value NUMERIC(24,2) NOT NULL CHECK (
        (side='buy' AND target_value>current_value AND projected_value=current_value+requested_notional AND projected_value<=target_value) OR
        (side='sell' AND target_value<current_value AND projected_value=current_value-requested_notional AND projected_value>=target_value)
    ),
    residual_drift NUMERIC(24,2) NOT NULL CHECK (residual_drift=abs(target_value-projected_value)),
    canonical_leg JSONB NOT NULL CHECK (jsonb_typeof(canonical_leg)='object'),
    PRIMARY KEY (run_id,sequence),
    UNIQUE (run_id,instrument_key)
);

CREATE FUNCTION validate_copy_target_drift_run() RETURNS TRIGGER AS $$
DECLARE
    subscription copy_subscriptions%ROWTYPE;
    observation copy_source_observations%ROWTYPE;
    session_date TEXT;
BEGIN
    SELECT * INTO subscription FROM copy_subscriptions WHERE id=NEW.subscription_id;
    SELECT * INTO observation FROM copy_source_observations WHERE id=NEW.source_observation_id;
    session_date := split_part(NEW.session_key,'/',1);
    IF subscription.id IS NULL OR subscription.origin_type<>NEW.origin_type OR subscription.origin_id<>NEW.origin_id OR NOT subscription.is_paper OR
       observation.id IS NULL OR observation.source_id<>subscription.source_id OR
       to_char(to_date(session_date,'YYYY-MM-DD'),'YYYY-MM-DD')<>session_date OR
       NEW.schema_name<>NEW.canonical_json->>'schema' OR NEW.state<>NEW.canonical_json->>'state' OR
       NEW.subscription_id::TEXT<>NEW.canonical_json->>'subscription_id' OR NEW.origin_type<>NEW.canonical_json->>'origin_type' OR
       NEW.origin_id::TEXT<>NEW.canonical_json->>'origin_id' OR NEW.source_observation_id::TEXT<>NEW.canonical_json->>'source_observation_id' OR
       NEW.session_key<>NEW.canonical_json->>'session_key' OR NEW.calculation_version<>(NEW.canonical_json->>'calculation_version')::INT OR
       NEW.maximum_session_turnover<>(NEW.canonical_json->>'maximum_session_turnover')::NUMERIC OR
       NEW.session_budget<>(NEW.canonical_json->>'session_budget')::NUMERIC OR NEW.starting_drift<>(NEW.canonical_json->>'starting_drift')::NUMERIC OR
       NEW.prepared_turnover<>(NEW.canonical_json->>'prepared_turnover')::NUMERIC OR NEW.residual_drift<>(NEW.canonical_json->>'residual_drift')::NUMERIC OR
       NEW.converged<>(NEW.canonical_json->>'converged')::BOOLEAN OR NEW.leg_count<>jsonb_array_length(NEW.canonical_json->'legs') OR
       NEW.sha256<>encode(digest(NEW.canonical_bytes,'sha256'),'hex') THEN
        RAISE EXCEPTION 'copy target drift run does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_target_drift_run_guard
BEFORE INSERT ON copy_target_drift_runs
FOR EACH ROW EXECUTE FUNCTION validate_copy_target_drift_run();

CREATE FUNCTION validate_copy_target_drift_leg() RETURNS TRIGGER AS $$
DECLARE expected JSONB;
BEGIN
    SELECT canonical_json->'legs'->NEW.sequence INTO expected FROM copy_target_drift_runs WHERE id=NEW.run_id;
    IF expected IS NULL OR
       NEW.sequence<>(expected->>'sequence')::INT OR NEW.instrument_key<>expected->>'instrument_key' OR NEW.side<>expected->>'side' OR
       NEW.current_value<>(expected->>'current_value')::NUMERIC OR NEW.target_value<>(expected->>'target_value')::NUMERIC OR
       NEW.starting_drift<>(expected->>'starting_drift')::NUMERIC OR NEW.requested_notional<>(expected->>'requested_notional')::NUMERIC OR
       NEW.projected_value<>(expected->>'projected_value')::NUMERIC OR NEW.residual_drift<>(expected->>'residual_drift')::NUMERIC OR
       NEW.canonical_leg<>expected THEN
        RAISE EXCEPTION 'copy target drift leg does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_target_drift_leg_guard
BEFORE INSERT ON copy_target_drift_legs
FOR EACH ROW EXECUTE FUNCTION validate_copy_target_drift_leg();

CREATE FUNCTION validate_copy_target_drift_graph() RETURNS TRIGGER AS $$
DECLARE actual_count INT; actual_turnover NUMERIC; prior_key TEXT;
BEGIN
    SELECT count(*),coalesce(sum(requested_notional),0),max(instrument_key) FILTER (WHERE sequence<0)
      INTO actual_count,actual_turnover,prior_key FROM copy_target_drift_legs WHERE run_id=NEW.id;
    IF actual_count<>NEW.leg_count OR actual_turnover<>NEW.prepared_turnover OR
       EXISTS (SELECT 1 FROM copy_target_drift_legs leg WHERE leg.run_id=NEW.id AND leg.sequence<>(SELECT count(*) FROM copy_target_drift_legs earlier WHERE earlier.run_id=NEW.id AND earlier.sequence<leg.sequence)) OR
       EXISTS (SELECT 1 FROM copy_target_drift_legs leg JOIN copy_target_drift_legs prior ON prior.run_id=leg.run_id AND prior.sequence=leg.sequence-1 WHERE leg.run_id=NEW.id AND leg.instrument_key<=prior.instrument_key) THEN
        RAISE EXCEPTION 'copy target drift graph does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER copy_target_drift_graph_guard
AFTER INSERT ON copy_target_drift_runs DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION validate_copy_target_drift_graph();

CREATE FUNCTION reject_copy_target_drift_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'copy target drift evidence is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_target_drift_runs_append_only BEFORE UPDATE OR DELETE ON copy_target_drift_runs FOR EACH ROW EXECUTE FUNCTION reject_copy_target_drift_mutation();
CREATE TRIGGER copy_target_drift_legs_append_only BEFORE UPDATE OR DELETE ON copy_target_drift_legs FOR EACH ROW EXECUTE FUNCTION reject_copy_target_drift_mutation();

CREATE INDEX idx_copy_target_drift_origin_session ON copy_target_drift_runs(origin_type,origin_id,session_key);

