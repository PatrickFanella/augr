-- Immutable, read-only venue reconciliation evidence. This migration seeds no
-- policy, grants no writer, selects no current policy, and schedules no work.
LOCK TABLE projection_checkpoints, execution_fills, execution_lifecycle_events,
    venue_observations, economic_event_normalizations, ledger_transactions IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION reject_venue_reconciliation_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION venue_reconciliation_go_json_string(value TEXT) RETURNS TEXT AS $$
    SELECT replace(replace(replace(replace(replace(
        to_json(value)::TEXT,
        '&', '\u0026'), '<', '\u003c'), '>', '\u003e'), chr(8232), '\u2028'), chr(8233), '\u2029');
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION venue_reconciliation_result_identity(
    policy_version TEXT, provider_snapshot_id TEXT, local_snapshot_id TEXT,
    result_key TEXT, result_kind TEXT, result_status TEXT, result_reason TEXT, result_severity TEXT,
    provider_value TEXT, local_value TEXT, delta_value TEXT
) RETURNS TEXT AS $$
    SELECT '{"policy_version":' || venue_reconciliation_go_json_string(policy_version) ||
        ',"provider_snapshot_id":' || venue_reconciliation_go_json_string(provider_snapshot_id) ||
        ',"local_snapshot_id":' || venue_reconciliation_go_json_string(local_snapshot_id) ||
        ',"key":' || venue_reconciliation_go_json_string(result_key) ||
        ',"kind":' || venue_reconciliation_go_json_string(result_kind) ||
        ',"status":' || venue_reconciliation_go_json_string(result_status) ||
        ',"reason":' || venue_reconciliation_go_json_string(result_reason) ||
        ',"severity":' || venue_reconciliation_go_json_string(result_severity) ||
        ',"provider_value":' || COALESCE(venue_reconciliation_go_json_string(provider_value), 'null') ||
        ',"local_value":' || COALESCE(venue_reconciliation_go_json_string(local_value), 'null') ||
        ',"delta":' || COALESCE(venue_reconciliation_go_json_string(delta_value), 'null') || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE TABLE venue_reconciliation_policy_artifacts (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'venue-reconciliation-policy-v1'),
    policy_version TEXT NOT NULL UNIQUE CHECK (policy_version = btrim(policy_version)),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes) > 0),
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (sha256 = '6cfc4ffcaedc9da46e940ae41d613e9cfcc12d48d50212a07355d6e98b0f301d'),
    CHECK (policy_version = schema_name || '@sha256:' || sha256),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK ((canonical_json ->> 'capture_count')::INTEGER = 2),
    CHECK ((canonical_json ->> 'exact_decimals')::BOOLEAN),
    CHECK ((canonical_json ->> 'complete_pagination')::BOOLEAN),
    CHECK ((canonical_json ->> 'complete_fill_coverage')::BOOLEAN),
    CHECK ((canonical_json ->> 'canonical_contracts')::BOOLEAN),
    CHECK (jsonb_array_length(canonical_json -> 'providers') = 2),
    CHECK (id = economic_deterministic_uuid('venue-reconciliation-policy-artifact', policy_version))
);

CREATE TABLE venue_provider_snapshots (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'venue-provider-stable-snapshot-v1'),
    provider TEXT NOT NULL CHECK (provider IN ('alpaca', 'kalshi')),
    account_external_id TEXT NOT NULL CHECK (account_external_id <> '' AND account_external_id = btrim(account_external_id)),
    namespace TEXT NOT NULL CHECK (namespace <> '' AND namespace = btrim(namespace)),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    state_sha256 TEXT NOT NULL CHECK (state_sha256 ~ '^[0-9a-f]{64}$'),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    state_bytes BYTEA NOT NULL,
    state_json JSONB NOT NULL CHECK (jsonb_typeof(state_json) = 'object'),
    first_capture_id UUID NOT NULL,
    second_capture_id UUID NOT NULL,
    first_capture_start TIMESTAMPTZ NOT NULL,
    first_capture_end TIMESTAMPTZ NOT NULL CHECK (first_capture_end >= first_capture_start),
    second_capture_start TIMESTAMPTZ NOT NULL,
    second_capture_end TIMESTAMPTZ NOT NULL CHECK (second_capture_end >= second_capture_start),
    page_count INTEGER NOT NULL CHECK (page_count > 0),
    position_count INTEGER NOT NULL CHECK (position_count >= 0),
    fill_count INTEGER NOT NULL CHECK (fill_count >= 0),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (state_sha256 = encode(digest(state_bytes, 'sha256'), 'hex')),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (state_json = convert_from(state_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK (canonical_json ->> 'provider_state_sha256' = state_sha256),
    CHECK (canonical_json ->> 'first_capture_id' = first_capture_id::TEXT),
    CHECK (canonical_json ->> 'second_capture_id' = second_capture_id::TEXT),
    CHECK (canonical_json ->> 'first_capture_start' = to_char(first_capture_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (canonical_json ->> 'first_capture_end' = to_char(first_capture_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (canonical_json ->> 'second_capture_start' = to_char(second_capture_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (canonical_json ->> 'second_capture_end' = to_char(second_capture_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (first_capture_id = economic_deterministic_uuid('venue-reconciliation-provider-capture', 'venue-provider-capture-v1@sha256:' || state_sha256)),
    CHECK (second_capture_id = first_capture_id),
    CHECK (state_json ->> 'provider' = provider),
    CHECK (state_json ->> 'account_id' = account_external_id),
    CHECK (state_json ->> 'namespace' = namespace),
    CHECK (state_json ->> 'currency' = currency),
    CHECK (id = economic_deterministic_uuid('venue-reconciliation-stable-snapshot', schema_name || '@sha256:' || sha256))
);

CREATE TABLE venue_provider_snapshot_pages (
    snapshot_id UUID NOT NULL REFERENCES venue_provider_snapshots(id) ON DELETE RESTRICT,
    account_external_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    cursor TEXT NOT NULL,
    next_cursor TEXT NOT NULL,
    terminal BOOLEAN NOT NULL,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    raw_bytes BYTEA NOT NULL,
    PRIMARY KEY (snapshot_id, sequence),
    UNIQUE (snapshot_id, cursor),
    CHECK (sha256 = encode(digest(raw_bytes, 'sha256'), 'hex'))
);

CREATE TABLE venue_provider_snapshot_positions (
    snapshot_id UUID NOT NULL REFERENCES venue_provider_snapshots(id) ON DELETE RESTRICT,
    account_external_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    instrument_id UUID NOT NULL,
    venue_contract_id UUID NOT NULL,
    contract_id TEXT NOT NULL CHECK (contract_id <> ''),
    quantity NUMERIC(38,12) NOT NULL,
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    source_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (snapshot_id, instrument_id),
    UNIQUE (snapshot_id, venue_contract_id)
);

CREATE TABLE venue_provider_snapshot_fills (
    snapshot_id UUID NOT NULL REFERENCES venue_provider_snapshots(id) ON DELETE RESTRICT,
    account_external_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    comparison_key TEXT NOT NULL CHECK (comparison_key <> ''),
    source_id TEXT NOT NULL CHECK (source_id <> ''),
    original_source_id TEXT NOT NULL,
    observation_class TEXT NOT NULL CHECK (observation_class IN ('ordinary','correction','bust')),
    observation_discriminator TEXT NOT NULL,
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    PRIMARY KEY (snapshot_id, sequence),
    UNIQUE (snapshot_id, comparison_key),
    UNIQUE (snapshot_id, source_id)
);

CREATE TABLE venue_local_snapshots (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'venue-local-snapshot-v1'),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    checkpoint_id UUID NOT NULL REFERENCES projection_checkpoints(id) ON DELETE RESTRICT,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    transaction_count INTEGER NOT NULL CHECK (transaction_count > 0),
    position_count INTEGER NOT NULL CHECK (position_count >= 0),
    fill_count INTEGER NOT NULL CHECK (fill_count >= 0),
    issue_count INTEGER NOT NULL CHECK (issue_count >= 0),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK (canonical_json ->> 'account_id' = account_id::TEXT),
    CHECK (canonical_json ->> 'provider' = provider),
    CHECK (canonical_json ->> 'namespace' = namespace),
    CHECK (canonical_json ->> 'horizon_start' = to_char(horizon_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (canonical_json ->> 'horizon_end' = to_char(horizon_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK (canonical_json ->> 'checkpoint_id' = checkpoint_id::TEXT),
    CHECK (id = economic_deterministic_uuid('venue-reconciliation-local-snapshot', schema_name || '@sha256:' || sha256))
);

CREATE TABLE venue_local_snapshot_transactions (
    snapshot_id UUID NOT NULL REFERENCES venue_local_snapshots(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    transaction_id UUID NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
    PRIMARY KEY (snapshot_id, transaction_id)
);
CREATE TABLE venue_local_snapshot_positions (
    snapshot_id UUID NOT NULL REFERENCES venue_local_snapshots(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    instrument_id UUID NOT NULL,
    quantity NUMERIC(38,12) NOT NULL CHECK (quantity <> 0),
    PRIMARY KEY (snapshot_id, instrument_id)
);
CREATE TABLE venue_local_snapshot_fills (
    snapshot_id UUID NOT NULL REFERENCES venue_local_snapshots(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    comparison_key TEXT NOT NULL CHECK (comparison_key <> ''),
    fill_id UUID NOT NULL REFERENCES execution_fills(id) ON DELETE RESTRICT,
    ledger_transaction_id UUID NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    PRIMARY KEY (snapshot_id, sequence),
    UNIQUE (snapshot_id, comparison_key),
    CHECK (fill_id <> '00000000-0000-0000-0000-000000000000'::UUID)
);
CREATE TABLE venue_local_snapshot_issues (
    snapshot_id UUID NOT NULL REFERENCES venue_local_snapshots(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('alpaca','kalshi')),
    namespace TEXT NOT NULL CHECK (namespace <> ''),
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL CHECK (horizon_end > horizon_start),
    issue_key TEXT NOT NULL CHECK (issue_key <> ''),
    reason TEXT NOT NULL CHECK (reason IN ('local_fill_incomplete','local_fill_after_frontier')),
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    PRIMARY KEY (snapshot_id, issue_key)
);

CREATE TABLE venue_reconciliation_runs (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'venue-reconciliation-run-v1'),
    policy_version TEXT NOT NULL REFERENCES venue_reconciliation_policy_artifacts(policy_version) ON DELETE RESTRICT,
    provider_snapshot_id UUID REFERENCES venue_provider_snapshots(id) ON DELETE RESTRICT,
    local_snapshot_id UUID NOT NULL REFERENCES venue_local_snapshots(id) ON DELETE RESTRICT,
    clean BOOLEAN NOT NULL,
    result_count INTEGER NOT NULL CHECK (result_count > 0),
    incident_count INTEGER NOT NULL CHECK (incident_count >= 0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (clean = (incident_count = 0)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK (canonical_json ->> 'policy_version' = policy_version),
    CHECK (canonical_json ->> 'local_snapshot_id' = local_snapshot_id::TEXT),
    CHECK (COALESCE(canonical_json ->> 'provider_snapshot_id','') = COALESCE(provider_snapshot_id::TEXT,'')),
    CHECK (id = economic_deterministic_uuid('venue-reconciliation-run', schema_name || '@sha256:' || sha256))
);

CREATE TABLE venue_reconciliation_results (
    run_id UUID NOT NULL REFERENCES venue_reconciliation_runs(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL,
    provider_snapshot_id UUID,
    local_snapshot_id UUID NOT NULL,
    id UUID NOT NULL,
    result_key TEXT NOT NULL CHECK (result_key <> ''),
    kind TEXT NOT NULL CHECK (kind IN ('cash','fill','position','snapshot')),
    status TEXT NOT NULL CHECK (status IN ('matched','drift','not_comparable')),
    reason TEXT NOT NULL CHECK (reason <> ''),
    severity TEXT NOT NULL CHECK (severity IN ('none','high','critical')),
    provider_value TEXT,
    local_value TEXT,
    delta TEXT,
    PRIMARY KEY (run_id, id),
    UNIQUE (run_id, result_key, reason),
    CHECK ((status = 'matched') = (severity = 'none'))
);
CREATE TABLE venue_reconciliation_incidents (
    run_id UUID NOT NULL,
    policy_version TEXT NOT NULL,
    provider_snapshot_id UUID,
    local_snapshot_id UUID NOT NULL,
    id UUID NOT NULL,
    result_id UUID NOT NULL,
    incident_key TEXT NOT NULL CHECK (incident_key <> ''),
    reason TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('high','critical')),
    PRIMARY KEY (run_id, id),
    UNIQUE (run_id, result_id),
    FOREIGN KEY (run_id, result_id) REFERENCES venue_reconciliation_results(run_id, id) ON DELETE RESTRICT,
    CHECK (id = economic_deterministic_uuid('venue-reconciliation-incident', result_id::TEXT))
);

CREATE FUNCTION validate_venue_reconciliation_graph() RETURNS TRIGGER AS $$
DECLARE parent_id UUID;
BEGIN
    IF TG_TABLE_NAME = 'venue_provider_snapshots' THEN parent_id := NEW.id;
    ELSIF TG_TABLE_NAME IN ('venue_provider_snapshot_pages','venue_provider_snapshot_positions','venue_provider_snapshot_fills') THEN parent_id := NEW.snapshot_id;
    ELSIF TG_TABLE_NAME = 'venue_local_snapshots' THEN parent_id := NEW.id;
    ELSIF TG_TABLE_NAME IN ('venue_local_snapshot_transactions','venue_local_snapshot_positions','venue_local_snapshot_fills','venue_local_snapshot_issues') THEN parent_id := NEW.snapshot_id;
    ELSIF TG_TABLE_NAME = 'venue_reconciliation_runs' THEN parent_id := NEW.id;
    ELSE parent_id := NEW.run_id;
    END IF;

    IF TG_TABLE_NAME IN ('venue_provider_snapshots','venue_provider_snapshot_pages','venue_provider_snapshot_positions','venue_provider_snapshot_fills') THEN
        PERFORM 1 FROM venue_provider_snapshots s WHERE s.id = parent_id AND
            s.page_count = (SELECT count(*) FROM venue_provider_snapshot_pages WHERE snapshot_id=s.id) AND
            s.position_count = (SELECT count(*) FROM venue_provider_snapshot_positions WHERE snapshot_id=s.id) AND
            s.fill_count = (SELECT count(*) FROM venue_provider_snapshot_fills WHERE snapshot_id=s.id) AND
            NOT EXISTS (SELECT 1 FROM venue_provider_snapshot_pages child WHERE child.snapshot_id=s.id AND
                (child.account_external_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_external_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            NOT EXISTS (SELECT 1 FROM venue_provider_snapshot_positions child WHERE child.snapshot_id=s.id AND
                (child.account_external_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_external_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            NOT EXISTS (SELECT 1 FROM venue_provider_snapshot_fills child WHERE child.snapshot_id=s.id AND
                (child.account_external_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_external_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            s.state_json -> 'pages' = (SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'sequence', sequence, 'cursor', cursor, 'next_cursor', next_cursor, 'terminal', terminal,
                'sha256', sha256, 'raw', convert_from(raw_bytes,'UTF8')::JSONB) ORDER BY sequence), '[]'::JSONB)
                FROM venue_provider_snapshot_pages WHERE snapshot_id=s.id) AND
            s.state_json -> 'positions' = (SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'instrument_id', instrument_id::TEXT, 'venue_contract_id', venue_contract_id::TEXT,
                'contract_id', contract_id, 'quantity', trim_scale(quantity)::TEXT, 'currency', currency,
                'source_at', to_char(source_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
                ORDER BY instrument_id::TEXT), '[]'::JSONB) FROM venue_provider_snapshot_positions WHERE snapshot_id=s.id) AND
            s.state_json -> 'fills' = (SELECT COALESCE(jsonb_agg(evidence ORDER BY sequence), '[]'::JSONB)
                FROM venue_provider_snapshot_fills WHERE snapshot_id=s.id);
    ELSIF TG_TABLE_NAME IN ('venue_local_snapshots','venue_local_snapshot_transactions','venue_local_snapshot_positions','venue_local_snapshot_fills','venue_local_snapshot_issues') THEN
        PERFORM 1 FROM venue_local_snapshots s WHERE s.id = parent_id AND
            s.transaction_count = (SELECT count(*) FROM venue_local_snapshot_transactions WHERE snapshot_id=s.id) AND
            s.position_count = (SELECT count(*) FROM venue_local_snapshot_positions WHERE snapshot_id=s.id) AND
            s.fill_count = (SELECT count(*) FROM venue_local_snapshot_fills WHERE snapshot_id=s.id) AND
            s.issue_count = (SELECT count(*) FROM venue_local_snapshot_issues WHERE snapshot_id=s.id) AND
            NOT EXISTS (SELECT 1 FROM venue_local_snapshot_transactions child WHERE child.snapshot_id=s.id AND
                (child.account_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            NOT EXISTS (SELECT 1 FROM venue_local_snapshot_positions child WHERE child.snapshot_id=s.id AND
                (child.account_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            NOT EXISTS (SELECT 1 FROM venue_local_snapshot_fills child WHERE child.snapshot_id=s.id AND
                (child.account_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            NOT EXISTS (SELECT 1 FROM venue_local_snapshot_issues child WHERE child.snapshot_id=s.id AND
                (child.account_id,child.provider,child.namespace,child.horizon_start,child.horizon_end) IS DISTINCT FROM
                (s.account_id,s.provider,s.namespace,s.horizon_start,s.horizon_end)) AND
            s.canonical_json -> 'transaction_ids' = (SELECT COALESCE(jsonb_agg(transaction_id::TEXT ORDER BY transaction_id::TEXT), '[]'::JSONB)
                FROM venue_local_snapshot_transactions WHERE snapshot_id=s.id) AND
            s.canonical_json -> 'positions' = (SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'instrument_id', instrument_id::TEXT, 'quantity', trim_scale(quantity)::TEXT) ORDER BY instrument_id::TEXT), '[]'::JSONB)
                FROM venue_local_snapshot_positions WHERE snapshot_id=s.id) AND
            s.canonical_json -> 'fills' = (SELECT COALESCE(jsonb_agg(evidence ORDER BY sequence), '[]'::JSONB)
                FROM venue_local_snapshot_fills WHERE snapshot_id=s.id) AND
            s.canonical_json -> 'issues' = (SELECT COALESCE(jsonb_agg(evidence ORDER BY reason, evidence->>'source_id', evidence->>'ledger_transaction_id'), '[]'::JSONB)
                FROM venue_local_snapshot_issues WHERE snapshot_id=s.id);
    ELSE
        PERFORM 1 FROM venue_reconciliation_runs r WHERE r.id = parent_id AND
            r.result_count = (SELECT count(*) FROM venue_reconciliation_results WHERE run_id=r.id) AND
            r.incident_count = (SELECT count(*) FROM venue_reconciliation_incidents WHERE run_id=r.id) AND
            r.clean = NOT EXISTS (SELECT 1 FROM venue_reconciliation_incidents WHERE run_id=r.id) AND
            NOT EXISTS (SELECT 1 FROM venue_reconciliation_results child WHERE child.run_id=r.id AND
                (child.policy_version,child.provider_snapshot_id,child.local_snapshot_id) IS DISTINCT FROM
                (r.policy_version,r.provider_snapshot_id,r.local_snapshot_id)) AND
            NOT EXISTS (SELECT 1 FROM venue_reconciliation_incidents child WHERE child.run_id=r.id AND
                (child.policy_version,child.provider_snapshot_id,child.local_snapshot_id) IS DISTINCT FROM
                (r.policy_version,r.provider_snapshot_id,r.local_snapshot_id)) AND
            NOT EXISTS (SELECT 1 FROM venue_reconciliation_results result WHERE result.run_id=r.id AND
                result.id <> economic_deterministic_uuid('venue-reconciliation-result', venue_reconciliation_result_identity(
                    r.policy_version, COALESCE(r.provider_snapshot_id::TEXT,''), r.local_snapshot_id::TEXT,
                    result.result_key, result.kind, result.status, result.reason, result.severity,
                    result.provider_value, result.local_value, result.delta))) AND
            NOT EXISTS (SELECT 1 FROM venue_reconciliation_results result
                LEFT JOIN venue_reconciliation_incidents incident ON incident.run_id=result.run_id AND incident.result_id=result.id
                WHERE result.run_id=r.id AND (
                    (result.status='matched' AND incident.id IS NOT NULL) OR
                    (result.status<>'matched' AND (incident.id IS NULL OR incident.incident_key<>result.result_key OR
                        incident.reason<>result.reason OR incident.severity<>result.severity)))) AND
            r.canonical_json -> 'results' = (SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'id', id::TEXT, 'key', result_key, 'kind', kind, 'status', status, 'reason', reason,
                'severity', severity, 'provider_value', provider_value, 'local_value', local_value, 'delta', delta)
                ORDER BY result_key, reason), '[]'::JSONB) FROM venue_reconciliation_results WHERE run_id=r.id) AND
            r.canonical_json -> 'incidents' = (SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'id', id::TEXT, 'result_id', result_id::TEXT, 'key', incident_key, 'reason', reason, 'severity', severity)
                ORDER BY incident_key, reason), '[]'::JSONB) FROM venue_reconciliation_incidents WHERE run_id=r.id);
    END IF;
    IF NOT FOUND THEN RAISE EXCEPTION 'venue reconciliation graph counts do not reconstruct'; END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DO $$ DECLARE table_name TEXT; BEGIN
    FOREACH table_name IN ARRAY ARRAY[
      'venue_reconciliation_policy_artifacts','venue_provider_snapshots','venue_provider_snapshot_pages',
      'venue_provider_snapshot_positions','venue_provider_snapshot_fills','venue_local_snapshots',
      'venue_local_snapshot_transactions','venue_local_snapshot_positions','venue_local_snapshot_fills',
      'venue_local_snapshot_issues','venue_reconciliation_runs','venue_reconciliation_results','venue_reconciliation_incidents'
    ] LOOP
      EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_venue_reconciliation_mutation()', 'trg_'||table_name||'_immutable', table_name);
    END LOOP;
END $$;

CREATE CONSTRAINT TRIGGER trg_provider_reconciliation_graph
AFTER INSERT ON venue_provider_snapshots DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_provider_page_reconciliation_graph
AFTER INSERT ON venue_provider_snapshot_pages DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_provider_position_reconciliation_graph
AFTER INSERT ON venue_provider_snapshot_positions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_provider_fill_reconciliation_graph
AFTER INSERT ON venue_provider_snapshot_fills DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_local_reconciliation_graph
AFTER INSERT ON venue_local_snapshots DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_local_transaction_reconciliation_graph
AFTER INSERT ON venue_local_snapshot_transactions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_local_position_reconciliation_graph
AFTER INSERT ON venue_local_snapshot_positions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_local_fill_reconciliation_graph
AFTER INSERT ON venue_local_snapshot_fills DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_local_issue_reconciliation_graph
AFTER INSERT ON venue_local_snapshot_issues DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_run_reconciliation_graph
AFTER INSERT ON venue_reconciliation_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_result_reconciliation_graph
AFTER INSERT ON venue_reconciliation_results DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
CREATE CONSTRAINT TRIGGER trg_incident_reconciliation_graph
AFTER INSERT ON venue_reconciliation_incidents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_venue_reconciliation_graph();
