-- Immutable canonical simulation-policy artifacts. This migration starts with
-- no artifact and does not activate a simulator, scheduler, or execution
-- writer. A schema-71 simulation order cannot be made recoverable by guessing
-- the policy bytes that produced its policy_version.

-- Prevent a schema-71 writer from racing the precondition and trigger
-- installation inside this migration transaction.
LOCK TABLE execution_orders IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM execution_orders
        WHERE policy_kind = 'simulation'
    ) THEN
        RAISE EXCEPTION 'migration 72 cannot attach pre-existing simulation orders without canonical policy artifacts';
    END IF;
END;
$$;

-- Reconstruct the exact Go simulation-policy-v1 encoding while independently
-- validating every fixed-schema field. The final table constraint compares
-- this byte string with the submitted bytes, so semantically equivalent JSON
-- with whitespace, reordered fields, duplicate keys, or non-normalized arrays
-- cannot authorize an execution order that Go would later fail to recover.
CREATE FUNCTION simulation_policy_v1_canonical_bytes(policy JSONB) RETURNS BYTEA AS $$
DECLARE
    canonical_text TEXT := '{"schema":"simulation-policy-v1","assets":[';
    asset_count INTEGER;
    asset_index INTEGER;
    asset JSONB;
    asset_class TEXT;
    previous_asset_class TEXT;
    order_types_text TEXT;
    time_in_force_text TEXT;
    quote_requirements JSONB;
    market_statuses_text TEXT;
    session_statuses_text TEXT;
    max_age_text TEXT;
    max_age BIGINT;
    participation_text TEXT;
    participation NUMERIC;
    latency_text TEXT;
    latency BIGINT;
    calendar_value JSONB;
    calendar_kind TEXT;
    sessions_value JSONB;
    sessions_text TEXT;
    session_count INTEGER;
    session_index INTEGER;
    session_value JSONB;
    session_label TEXT;
    session_open_text TEXT;
    session_close_text TEXT;
    session_open TIMESTAMPTZ;
    session_close TIMESTAMPTZ;
    previous_session_open TIMESTAMPTZ;
    previous_session_close TIMESTAMPTZ;
    previous_session_label TEXT;
    seen_session_labels TEXT[];
    fees_value JSONB;
    per_order_text TEXT;
    per_unit_text TEXT;
    notional_bps_text TEXT;
    fee_scale_text TEXT;
    fee_scale INTEGER;
    array_count INTEGER;
    array_index INTEGER;
    array_item JSONB;
    token TEXT;
    previous_token TEXT;
    field_name TEXT;
BEGIN
    IF jsonb_typeof(policy) IS DISTINCT FROM 'object' OR
       ARRAY(
           SELECT key_name
           FROM jsonb_object_keys(policy) AS keys(key_name)
           ORDER BY key_name COLLATE "C"
       ) <> ARRAY['assets', 'schema']::TEXT[] THEN
        RAISE EXCEPTION 'canonical simulation policy must contain exactly schema and assets';
    END IF;
    IF jsonb_typeof(policy -> 'schema') IS DISTINCT FROM 'string' OR
       policy ->> 'schema' <> 'simulation-policy-v1' THEN
        RAISE EXCEPTION 'canonical simulation policy schema is invalid';
    END IF;
    IF jsonb_typeof(policy -> 'assets') IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'canonical simulation policy assets must be an array';
    END IF;
    asset_count := jsonb_array_length(policy -> 'assets');
    IF asset_count = 0 THEN
        RAISE EXCEPTION 'canonical simulation policy requires at least one asset';
    END IF;

    FOR asset_index IN 0..asset_count - 1 LOOP
        asset := policy -> 'assets' -> asset_index;
        IF jsonb_typeof(asset) IS DISTINCT FROM 'object' OR
           ARRAY(
               SELECT key_name
               FROM jsonb_object_keys(asset) AS keys(key_name)
               ORDER BY key_name COLLATE "C"
           ) <> ARRAY[
               'asset_class', 'calendar', 'fees',
               'fixed_latency_nanoseconds', 'max_depth_participation',
               'order_types', 'quote_requirements', 'time_in_force'
           ]::TEXT[] THEN
            RAISE EXCEPTION 'canonical simulation policy asset % has an invalid fixed shape', asset_index;
        END IF;

        IF jsonb_typeof(asset -> 'asset_class') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'canonical simulation policy asset % class must be a string', asset_index;
        END IF;
        asset_class := asset ->> 'asset_class';
        IF asset_class <> ALL(ARRAY[
            'crypto_spot', 'equity', 'etf', 'option', 'prediction_contract'
        ]::TEXT[]) THEN
            RAISE EXCEPTION 'canonical simulation policy asset class % is unsupported', asset_class;
        END IF;
        IF previous_asset_class IS NOT NULL AND
           asset_class COLLATE "C" <= previous_asset_class COLLATE "C" THEN
            RAISE EXCEPTION 'canonical simulation policy asset classes must be unique and sorted';
        END IF;
        previous_asset_class := asset_class;

        IF jsonb_typeof(asset -> 'order_types') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'canonical simulation policy order types must be an array';
        END IF;
        array_count := jsonb_array_length(asset -> 'order_types');
        IF array_count = 0 THEN
            RAISE EXCEPTION 'canonical simulation policy requires at least one order type';
        END IF;
        order_types_text := '[';
        previous_token := NULL;
        FOR array_index IN 0..array_count - 1 LOOP
            array_item := asset -> 'order_types' -> array_index;
            IF jsonb_typeof(array_item) IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'canonical simulation policy order type must be a string';
            END IF;
            token := array_item #>> '{}';
            IF token <> ALL(ARRAY['limit', 'market']::TEXT[]) THEN
                RAISE EXCEPTION 'canonical simulation policy order type % is unsupported', token;
            END IF;
            IF previous_token IS NOT NULL AND
               token COLLATE "C" <= previous_token COLLATE "C" THEN
                RAISE EXCEPTION 'canonical simulation policy order types must be unique and sorted';
            END IF;
            order_types_text := order_types_text ||
                CASE WHEN array_index > 0 THEN ',' ELSE '' END || '"' || token || '"';
            previous_token := token;
        END LOOP;
        order_types_text := order_types_text || ']';

        IF jsonb_typeof(asset -> 'time_in_force') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'canonical simulation policy time in force must be an array';
        END IF;
        array_count := jsonb_array_length(asset -> 'time_in_force');
        IF array_count = 0 THEN
            RAISE EXCEPTION 'canonical simulation policy requires at least one time in force';
        END IF;
        time_in_force_text := '[';
        previous_token := NULL;
        FOR array_index IN 0..array_count - 1 LOOP
            array_item := asset -> 'time_in_force' -> array_index;
            IF jsonb_typeof(array_item) IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'canonical simulation policy time in force must be a string';
            END IF;
            token := array_item #>> '{}';
            IF token <> ALL(ARRAY['day', 'fok', 'gtc', 'ioc']::TEXT[]) THEN
                RAISE EXCEPTION 'canonical simulation policy time in force % is unsupported', token;
            END IF;
            IF previous_token IS NOT NULL AND
               token COLLATE "C" <= previous_token COLLATE "C" THEN
                RAISE EXCEPTION 'canonical simulation policy time in force values must be unique and sorted';
            END IF;
            time_in_force_text := time_in_force_text ||
                CASE WHEN array_index > 0 THEN ',' ELSE '' END || '"' || token || '"';
            previous_token := token;
        END LOOP;
        time_in_force_text := time_in_force_text || ']';

        quote_requirements := asset -> 'quote_requirements';
        IF jsonb_typeof(quote_requirements) IS DISTINCT FROM 'object' OR
           ARRAY(
               SELECT key_name
               FROM jsonb_object_keys(quote_requirements) AS keys(key_name)
               ORDER BY key_name COLLATE "C"
           ) <> ARRAY[
               'allowed_market_statuses', 'allowed_session_statuses',
               'max_age_nanoseconds', 'require_ask', 'require_ask_depth',
               'require_bid', 'require_bid_depth', 'require_market_status',
               'require_session_status', 'require_source',
               'require_venue_contract'
           ]::TEXT[] THEN
            RAISE EXCEPTION 'canonical simulation policy quote requirements have an invalid fixed shape';
        END IF;
        FOREACH field_name IN ARRAY ARRAY[
            'require_source', 'require_venue_contract', 'require_bid',
            'require_ask', 'require_bid_depth', 'require_ask_depth',
            'require_market_status', 'require_session_status'
        ]::TEXT[] LOOP
            IF quote_requirements -> field_name IS DISTINCT FROM 'true'::JSONB THEN
                RAISE EXCEPTION 'canonical simulation policy quote requirement % must be true', field_name;
            END IF;
        END LOOP;

        IF jsonb_typeof(quote_requirements -> 'allowed_market_statuses') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'canonical simulation policy market statuses must be an array';
        END IF;
        array_count := jsonb_array_length(quote_requirements -> 'allowed_market_statuses');
        IF array_count = 0 THEN
            RAISE EXCEPTION 'canonical simulation policy requires an allowed market status';
        END IF;
        market_statuses_text := '[';
        previous_token := NULL;
        FOR array_index IN 0..array_count - 1 LOOP
            array_item := quote_requirements -> 'allowed_market_statuses' -> array_index;
            IF jsonb_typeof(array_item) IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'canonical simulation policy market status must be a string';
            END IF;
            token := array_item #>> '{}';
            IF token !~ '^[a-z0-9][a-z0-9_.:-]{0,63}$' THEN
                RAISE EXCEPTION 'canonical simulation policy market status % is not a canonical token', token;
            END IF;
            IF previous_token IS NOT NULL AND
               token COLLATE "C" <= previous_token COLLATE "C" THEN
                RAISE EXCEPTION 'canonical simulation policy market statuses must be unique and sorted';
            END IF;
            market_statuses_text := market_statuses_text ||
                CASE WHEN array_index > 0 THEN ',' ELSE '' END || '"' || token || '"';
            previous_token := token;
        END LOOP;
        market_statuses_text := market_statuses_text || ']';

        IF jsonb_typeof(quote_requirements -> 'allowed_session_statuses') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'canonical simulation policy session statuses must be an array';
        END IF;
        array_count := jsonb_array_length(quote_requirements -> 'allowed_session_statuses');
        IF array_count = 0 THEN
            RAISE EXCEPTION 'canonical simulation policy requires an allowed session status';
        END IF;
        session_statuses_text := '[';
        previous_token := NULL;
        FOR array_index IN 0..array_count - 1 LOOP
            array_item := quote_requirements -> 'allowed_session_statuses' -> array_index;
            IF jsonb_typeof(array_item) IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'canonical simulation policy session status must be a string';
            END IF;
            token := array_item #>> '{}';
            IF token !~ '^[a-z0-9][a-z0-9_.:-]{0,63}$' THEN
                RAISE EXCEPTION 'canonical simulation policy session status % is not a canonical token', token;
            END IF;
            IF previous_token IS NOT NULL AND
               token COLLATE "C" <= previous_token COLLATE "C" THEN
                RAISE EXCEPTION 'canonical simulation policy session statuses must be unique and sorted';
            END IF;
            session_statuses_text := session_statuses_text ||
                CASE WHEN array_index > 0 THEN ',' ELSE '' END || '"' || token || '"';
            previous_token := token;
        END LOOP;
        session_statuses_text := session_statuses_text || ']';

        IF jsonb_typeof(quote_requirements -> 'max_age_nanoseconds') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'canonical simulation policy maximum age must be an integer';
        END IF;
        max_age_text := quote_requirements ->> 'max_age_nanoseconds';
        IF max_age_text !~ '^[1-9][0-9]*$' THEN
            RAISE EXCEPTION 'canonical simulation policy maximum age must be a positive canonical integer';
        END IF;
        BEGIN
            max_age := max_age_text::BIGINT;
        EXCEPTION WHEN OTHERS THEN
            RAISE EXCEPTION 'canonical simulation policy maximum age exceeds int64';
        END;
        IF max_age <= 0 THEN
            RAISE EXCEPTION 'canonical simulation policy maximum age must be positive';
        END IF;

        IF jsonb_typeof(asset -> 'max_depth_participation') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'canonical simulation policy depth participation must be a string';
        END IF;
        participation_text := asset ->> 'max_depth_participation';
        IF participation_text !~ '^(0|[1-9][0-9]{0,25}|(0|[1-9][0-9]{0,25})[.][0-9]{0,11}[1-9])$' THEN
            RAISE EXCEPTION 'canonical simulation policy depth participation is not a canonical decimal';
        END IF;
        BEGIN
            participation := participation_text::NUMERIC;
        EXCEPTION WHEN OTHERS THEN
            RAISE EXCEPTION 'canonical simulation policy depth participation is invalid';
        END;
        IF participation <= 0 OR participation > 1 THEN
            RAISE EXCEPTION 'canonical simulation policy depth participation must be in (0,1]';
        END IF;

        IF jsonb_typeof(asset -> 'fixed_latency_nanoseconds') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'canonical simulation policy latency must be an integer';
        END IF;
        latency_text := asset ->> 'fixed_latency_nanoseconds';
        IF latency_text !~ '^(0|[1-9][0-9]*)$' THEN
            RAISE EXCEPTION 'canonical simulation policy latency must be a nonnegative canonical integer';
        END IF;
        BEGIN
            latency := latency_text::BIGINT;
        EXCEPTION WHEN OTHERS THEN
            RAISE EXCEPTION 'canonical simulation policy latency exceeds int64';
        END;
        IF latency < 0 THEN
            RAISE EXCEPTION 'canonical simulation policy latency cannot be negative';
        END IF;

        calendar_value := asset -> 'calendar';
        IF jsonb_typeof(calendar_value) IS DISTINCT FROM 'object' OR
           ARRAY(
               SELECT key_name
               FROM jsonb_object_keys(calendar_value) AS keys(key_name)
               ORDER BY key_name COLLATE "C"
           ) <> ARRAY['kind', 'sessions']::TEXT[] THEN
            RAISE EXCEPTION 'canonical simulation policy calendar has an invalid fixed shape';
        END IF;
        IF jsonb_typeof(calendar_value -> 'kind') IS DISTINCT FROM 'string' OR
           jsonb_typeof(calendar_value -> 'sessions') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'canonical simulation policy calendar kind/sessions are invalid';
        END IF;
        calendar_kind := calendar_value ->> 'kind';
        sessions_value := calendar_value -> 'sessions';
        session_count := jsonb_array_length(sessions_value);
        sessions_text := '[';
        previous_session_open := NULL;
        previous_session_close := NULL;
        previous_session_label := NULL;
        seen_session_labels := ARRAY[]::TEXT[];

        IF calendar_kind = 'continuous_24x7' THEN
            IF session_count <> 0 THEN
                RAISE EXCEPTION 'canonical simulation policy continuous calendar cannot contain sessions';
            END IF;
            IF asset -> 'time_in_force' @> '["day"]'::JSONB THEN
                RAISE EXCEPTION 'canonical simulation policy continuous calendar cannot support DAY';
            END IF;
        ELSIF calendar_kind = 'explicit_sessions' THEN
            IF session_count = 0 THEN
                RAISE EXCEPTION 'canonical simulation policy explicit calendar requires a session';
            END IF;
            FOR session_index IN 0..session_count - 1 LOOP
                session_value := sessions_value -> session_index;
                IF jsonb_typeof(session_value) IS DISTINCT FROM 'object' OR
                   ARRAY(
                       SELECT key_name
                       FROM jsonb_object_keys(session_value) AS keys(key_name)
                       ORDER BY key_name COLLATE "C"
                   ) <> ARRAY['close_at', 'label', 'open_at']::TEXT[] THEN
                    RAISE EXCEPTION 'canonical simulation policy session % has an invalid fixed shape', session_index;
                END IF;
                IF jsonb_typeof(session_value -> 'label') IS DISTINCT FROM 'string' OR
                   jsonb_typeof(session_value -> 'open_at') IS DISTINCT FROM 'string' OR
                   jsonb_typeof(session_value -> 'close_at') IS DISTINCT FROM 'string' THEN
                    RAISE EXCEPTION 'canonical simulation policy session fields must be strings';
                END IF;
                session_label := session_value ->> 'label';
                session_open_text := session_value ->> 'open_at';
                session_close_text := session_value ->> 'close_at';
                IF session_label !~ '^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$' THEN
                    RAISE EXCEPTION 'canonical simulation policy session label is not a canonical token';
                END IF;
                IF session_label = ANY(seen_session_labels) THEN
                    RAISE EXCEPTION 'canonical simulation policy session labels must be unique';
                END IF;
                seen_session_labels := array_append(seen_session_labels, session_label);
                IF session_open_text !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[.][0-9]{6}Z$' OR
                   session_close_text !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[.][0-9]{6}Z$' THEN
                    RAISE EXCEPTION 'canonical simulation policy session timestamps must use UTC microseconds';
                END IF;
                BEGIN
                    session_open := session_open_text::TIMESTAMPTZ;
                    session_close := session_close_text::TIMESTAMPTZ;
                EXCEPTION WHEN OTHERS THEN
                    RAISE EXCEPTION 'canonical simulation policy session timestamp is invalid';
                END;
                IF to_char(session_open AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') <> session_open_text OR
                   to_char(session_close AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') <> session_close_text THEN
                    RAISE EXCEPTION 'canonical simulation policy session timestamp is not canonical';
                END IF;
                IF session_close <= session_open THEN
                    RAISE EXCEPTION 'canonical simulation policy session close must follow open';
                END IF;
                IF previous_session_open IS NOT NULL AND (
                    session_open < previous_session_open OR
                    (session_open = previous_session_open AND
                     session_label COLLATE "C" <= previous_session_label COLLATE "C")
                ) THEN
                    RAISE EXCEPTION 'canonical simulation policy sessions must be sorted';
                END IF;
                IF previous_session_close IS NOT NULL AND session_open < previous_session_close THEN
                    RAISE EXCEPTION 'canonical simulation policy sessions cannot overlap';
                END IF;
                sessions_text := sessions_text ||
                    CASE WHEN session_index > 0 THEN ',' ELSE '' END ||
                    '{"label":"' || session_label || '","open_at":"' || session_open_text ||
                    '","close_at":"' || session_close_text || '"}';
                previous_session_open := session_open;
                previous_session_close := session_close;
                previous_session_label := session_label;
            END LOOP;
        ELSE
            RAISE EXCEPTION 'canonical simulation policy calendar kind % is invalid', calendar_kind;
        END IF;
        sessions_text := sessions_text || ']';

        fees_value := asset -> 'fees';
        IF jsonb_typeof(fees_value) IS DISTINCT FROM 'object' OR
           ARRAY(
               SELECT key_name
               FROM jsonb_object_keys(fees_value) AS keys(key_name)
               ORDER BY key_name COLLATE "C"
           ) <> ARRAY['notional_bps', 'per_order', 'per_unit', 'scale']::TEXT[] THEN
            RAISE EXCEPTION 'canonical simulation policy fees have an invalid fixed shape';
        END IF;
        IF jsonb_typeof(fees_value -> 'per_order') IS DISTINCT FROM 'string' OR
           jsonb_typeof(fees_value -> 'per_unit') IS DISTINCT FROM 'string' OR
           jsonb_typeof(fees_value -> 'notional_bps') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'canonical simulation policy fee values must be strings';
        END IF;
        per_order_text := fees_value ->> 'per_order';
        per_unit_text := fees_value ->> 'per_unit';
        notional_bps_text := fees_value ->> 'notional_bps';
        FOREACH token IN ARRAY ARRAY[per_order_text, per_unit_text, notional_bps_text] LOOP
            IF token !~ '^(0|[1-9][0-9]{0,25}|(0|[1-9][0-9]{0,25})[.][0-9]{0,11}[1-9])$' THEN
                RAISE EXCEPTION 'canonical simulation policy fee is not a canonical nonnegative decimal';
            END IF;
            BEGIN
                participation := token::NUMERIC;
            EXCEPTION WHEN OTHERS THEN
                RAISE EXCEPTION 'canonical simulation policy fee is invalid';
            END;
            IF participation < 0 OR participation >= 100000000000000000000000000::NUMERIC THEN
                RAISE EXCEPTION 'canonical simulation policy fee exceeds exact decimal magnitude';
            END IF;
        END LOOP;
        IF jsonb_typeof(fees_value -> 'scale') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'canonical simulation policy fee scale must be an integer';
        END IF;
        fee_scale_text := fees_value ->> 'scale';
        IF fee_scale_text !~ '^(0|[1-9][0-9]*)$' THEN
            RAISE EXCEPTION 'canonical simulation policy fee scale must be a canonical integer';
        END IF;
        BEGIN
            fee_scale := fee_scale_text::INTEGER;
        EXCEPTION WHEN OTHERS THEN
            RAISE EXCEPTION 'canonical simulation policy fee scale is invalid';
        END;
        IF fee_scale < 0 OR fee_scale > 12 THEN
            RAISE EXCEPTION 'canonical simulation policy fee scale must be between 0 and 12';
        END IF;

        canonical_text := canonical_text ||
            CASE WHEN asset_index > 0 THEN ',' ELSE '' END ||
            '{"asset_class":"' || asset_class || '","order_types":' || order_types_text ||
            ',"time_in_force":' || time_in_force_text ||
            ',"quote_requirements":{"require_source":true,"require_venue_contract":true' ||
            ',"require_bid":true,"require_ask":true,"require_bid_depth":true' ||
            ',"require_ask_depth":true,"require_market_status":true' ||
            ',"require_session_status":true,"allowed_market_statuses":' || market_statuses_text ||
            ',"allowed_session_statuses":' || session_statuses_text ||
            ',"max_age_nanoseconds":' || max_age_text || '}' ||
            ',"max_depth_participation":"' || participation_text || '"' ||
            ',"fixed_latency_nanoseconds":' || latency_text ||
            ',"calendar":{"kind":"' || calendar_kind || '","sessions":' || sessions_text || '}' ||
            ',"fees":{"per_order":"' || per_order_text || '","per_unit":"' || per_unit_text ||
            '","notional_bps":"' || notional_bps_text || '","scale":' || fee_scale_text || '}}';
    END LOOP;

    RETURN convert_to(canonical_text || ']}', 'UTF8');
END;
$$ LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog, pg_temp;

CREATE TABLE simulation_policy_artifacts (
    id              UUID PRIMARY KEY,
    schema_name     TEXT NOT NULL CHECK (
                        schema_name = btrim(schema_name) AND
                        schema_name ~ '^[a-z][a-z0-9-]{0,127}$'
                    ),
    policy_version  TEXT NOT NULL UNIQUE CHECK (
                        policy_version = btrim(policy_version) AND
                        char_length(policy_version) <= 256
                    ),
    sha256          TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes) > 0),
    canonical_json  JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', NOW())
                    CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (policy_version = schema_name || '@sha256:' || sha256),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK (canonical_bytes = simulation_policy_v1_canonical_bytes(canonical_json)),
    CHECK (id = economic_deterministic_uuid(
        'simulation-policy-artifact', policy_version
    ))
);

CREATE INDEX idx_simulation_policy_artifacts_created
    ON simulation_policy_artifacts (created_at, id);

CREATE FUNCTION reject_simulation_policy_artifact_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'simulation policy artifacts are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_simulation_policy_artifacts_immutable
    BEFORE UPDATE OR DELETE ON simulation_policy_artifacts
    FOR EACH ROW EXECUTE FUNCTION reject_simulation_policy_artifact_mutation();

CREATE FUNCTION validate_simulation_order_policy_artifact() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.policy_kind = 'simulation' AND NOT EXISTS (
        SELECT 1 FROM simulation_policy_artifacts
        WHERE policy_version = NEW.policy_version
    ) THEN
        RAISE EXCEPTION 'simulation order requires a registered simulation policy artifact for version %', NEW.policy_version;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_orders_simulation_policy
    BEFORE INSERT ON execution_orders
    FOR EACH ROW EXECUTE FUNCTION validate_simulation_order_policy_artifact();
