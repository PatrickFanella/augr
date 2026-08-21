-- Immutable structural evidence for legacy-versus-ledger accounting dual runs.
-- This migration authenticates no source or reviewer: future runtime grants
-- require a separate workload identity and attestation design.

CREATE TABLE accounting_reconciliation_runs (
    id                         UUID PRIMARY KEY,
    account_id                 UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    comparison_version         TEXT NOT NULL CHECK (comparison_version = 'accounting_comparison_v1'),
    policy_version             TEXT NOT NULL CHECK (policy_version = 'legacy_ledger_v1'),
    as_of                      TIMESTAMPTZ NOT NULL,
    generated_at               TIMESTAMPTZ NOT NULL CHECK (generated_at >= as_of),
    generator                  TEXT NOT NULL CHECK (generator <> '' AND generator = btrim(generator) AND char_length(generator) <= 256),
    projection_version         TEXT NOT NULL CHECK (projection_version <> '' AND projection_version = btrim(projection_version) AND char_length(projection_version) <= 128),
    mark_source                TEXT NOT NULL CHECK (mark_source <> '' AND mark_source = lower(mark_source) AND mark_source = btrim(mark_source) AND char_length(mark_source) <= 128),
    mark_namespace             TEXT NOT NULL CHECK (mark_namespace <> '' AND mark_namespace = btrim(mark_namespace) AND char_length(mark_namespace) <= 256),
    max_mark_age_microseconds  BIGINT NOT NULL CHECK (max_mark_age_microseconds > 0),
    capture_fence_id           TEXT NOT NULL CHECK (capture_fence_id <> '' AND capture_fence_id = btrim(capture_fence_id) AND char_length(capture_fence_id) <= 256),
    capture_epoch              BIGINT NOT NULL CHECK (capture_epoch > 0),
    legacy_snapshot_id         UUID NOT NULL,
    legacy_snapshot_checksum   TEXT NOT NULL CHECK (legacy_snapshot_checksum ~ '^[0-9a-f]{64}$'),
    legacy_snapshot_bytes      BYTEA NOT NULL CHECK (octet_length(legacy_snapshot_bytes) > 0),
    ledger_snapshot_id         UUID NOT NULL,
    ledger_snapshot_checksum   TEXT NOT NULL CHECK (ledger_snapshot_checksum ~ '^[0-9a-f]{64}$'),
    ledger_snapshot_bytes      BYTEA NOT NULL CHECK (octet_length(ledger_snapshot_bytes) > 0),
    payload                    JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    payload_bytes              BYTEA NOT NULL CHECK (octet_length(payload_bytes) > 0),
    checksum                   TEXT NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    result_count               INTEGER NOT NULL CHECK (result_count >= 7),
    equal_count                INTEGER NOT NULL CHECK (equal_count >= 0),
    explained_count            INTEGER NOT NULL CHECK (explained_count >= 0),
    unexplained_count          INTEGER NOT NULL CHECK (unexplained_count >= 0),
    not_comparable_count       INTEGER NOT NULL CHECK (not_comparable_count >= 0),
    synthetic                  BOOLEAN NOT NULL,
    attestation_type           TEXT,
    attestation_key_id         TEXT,
    attestation                BYTEA,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, policy_version, as_of, legacy_snapshot_checksum, ledger_snapshot_checksum, checksum),
    CHECK (
        (attestation_type IS NULL AND attestation_key_id IS NULL AND attestation IS NULL) OR
        (attestation_type IS NOT NULL AND attestation_type <> '' AND attestation_type = btrim(attestation_type) AND char_length(attestation_type) <= 256 AND
         attestation_key_id IS NOT NULL AND attestation_key_id <> '' AND attestation_key_id = btrim(attestation_key_id) AND char_length(attestation_key_id) <= 256 AND
         attestation IS NOT NULL AND octet_length(attestation) > 0)
    ),
    CHECK (result_count = equal_count + explained_count + unexplained_count + not_comparable_count)
);

CREATE INDEX idx_accounting_reconciliation_runs_window
    ON accounting_reconciliation_runs (account_id, as_of DESC, id DESC);

CREATE TABLE accounting_reconciliation_results (
    id              UUID PRIMARY KEY,
    run_id          UUID NOT NULL REFERENCES accounting_reconciliation_runs(id) ON DELETE RESTRICT,
    fact_key        TEXT NOT NULL CHECK (fact_key <> '' AND fact_key = btrim(fact_key) AND char_length(fact_key) <= 256),
    legacy_value    NUMERIC,
    ledger_value    NUMERIC,
    delta           NUMERIC,
    status          TEXT NOT NULL CHECK (status IN ('equal', 'explained', 'unexplained', 'not_comparable')),
    reason_code     TEXT NOT NULL CHECK (reason_code <> '' AND reason_code = btrim(reason_code) AND char_length(reason_code) <= 256),
    explanation     JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, fact_key),
    CHECK (explanation IS NULL OR jsonb_typeof(explanation) = 'object'),
    CHECK (legacy_value IS NULL OR (scale(legacy_value) <= 18 AND length(split_part(abs(legacy_value)::TEXT, '.', 1)) <= 26)),
    CHECK (ledger_value IS NULL OR (scale(ledger_value) <= 18 AND length(split_part(abs(ledger_value)::TEXT, '.', 1)) <= 26)),
    CHECK (delta IS NULL OR (scale(delta) <= 18 AND length(split_part(abs(delta)::TEXT, '.', 1)) <= 26))
);

CREATE INDEX idx_accounting_reconciliation_results_status
    ON accounting_reconciliation_results (run_id, status, fact_key);

CREATE FUNCTION reject_accounting_reconciliation_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_accounting_reconciliation_runs_immutable
    BEFORE UPDATE OR DELETE ON accounting_reconciliation_runs
    FOR EACH ROW EXECUTE FUNCTION reject_accounting_reconciliation_mutation();

CREATE TRIGGER trg_accounting_reconciliation_results_immutable
    BEFORE UPDATE OR DELETE ON accounting_reconciliation_results
    FOR EACH ROW EXECUTE FUNCTION reject_accounting_reconciliation_mutation();

CREATE FUNCTION validate_accounting_reconciliation_run() RETURNS TRIGGER AS $$
DECLARE
    decoded_payload JSONB;
    legacy_payload JSONB;
    ledger_payload JSONB;
    expected_legacy_id UUID;
    expected_ledger_id UUID;
    expected_id UUID;
BEGIN
    BEGIN
        decoded_payload := convert_from(NEW.payload_bytes, 'UTF8')::JSONB;
        legacy_payload := convert_from(NEW.legacy_snapshot_bytes, 'UTF8')::JSONB;
        ledger_payload := convert_from(NEW.ledger_snapshot_bytes, 'UTF8')::JSONB;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'accounting reconciliation bytes must contain valid UTF-8 JSON';
    END;

    IF decoded_payload IS DISTINCT FROM NEW.payload OR
       decoded_payload->'legacy_snapshot' IS DISTINCT FROM legacy_payload OR
       decoded_payload->'ledger_snapshot' IS DISTINCT FROM ledger_payload THEN
        RAISE EXCEPTION 'accounting reconciliation byte and JSON evidence differ';
    END IF;
    IF encode(digest(NEW.legacy_snapshot_bytes, 'sha256'), 'hex') <> NEW.legacy_snapshot_checksum OR
       encode(digest(NEW.ledger_snapshot_bytes, 'sha256'), 'hex') <> NEW.ledger_snapshot_checksum OR
       encode(digest(NEW.payload_bytes, 'sha256'), 'hex') <> NEW.checksum THEN
        RAISE EXCEPTION 'accounting reconciliation SHA-256 evidence differs';
    END IF;

    IF legacy_payload->>'version' IS DISTINCT FROM 'accounting_snapshot_v1' OR legacy_payload->>'source' IS DISTINCT FROM 'legacy_compatibility' OR
       ledger_payload->>'version' IS DISTINCT FROM 'accounting_snapshot_v1' OR ledger_payload->>'source' IS DISTINCT FROM 'immutable_ledger' THEN
        RAISE EXCEPTION 'accounting reconciliation source snapshot roles are invalid';
    END IF;
    IF legacy_payload->>'account_id' IS DISTINCT FROM NEW.account_id::TEXT OR ledger_payload->>'account_id' IS DISTINCT FROM NEW.account_id::TEXT OR
       legacy_payload->>'as_of' IS DISTINCT FROM to_char(NEW.as_of AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') OR
       ledger_payload->>'as_of' IS DISTINCT FROM to_char(NEW.as_of AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') OR
       legacy_payload->>'currency' IS DISTINCT FROM ledger_payload->>'currency' OR
       legacy_payload->>'projection_version' IS DISTINCT FROM NEW.projection_version OR ledger_payload->>'projection_version' IS DISTINCT FROM NEW.projection_version OR
       legacy_payload->>'mark_source' IS DISTINCT FROM NEW.mark_source OR ledger_payload->>'mark_source' IS DISTINCT FROM NEW.mark_source OR
       legacy_payload->>'mark_namespace' IS DISTINCT FROM NEW.mark_namespace OR ledger_payload->>'mark_namespace' IS DISTINCT FROM NEW.mark_namespace OR
       (legacy_payload->>'max_mark_age_microseconds')::BIGINT IS DISTINCT FROM NEW.max_mark_age_microseconds OR
       (ledger_payload->>'max_mark_age_microseconds')::BIGINT IS DISTINCT FROM NEW.max_mark_age_microseconds OR
       legacy_payload->>'capture_fence_id' IS DISTINCT FROM NEW.capture_fence_id OR ledger_payload->>'capture_fence_id' IS DISTINCT FROM NEW.capture_fence_id OR
       (legacy_payload->>'capture_epoch')::BIGINT IS DISTINCT FROM NEW.capture_epoch OR (ledger_payload->>'capture_epoch')::BIGINT IS DISTINCT FROM NEW.capture_epoch OR
       legacy_payload->>'observed_at' IS NULL OR ledger_payload->>'observed_at' IS NULL OR
       legacy_payload->>'observed_at' IS DISTINCT FROM to_char((legacy_payload->>'observed_at')::TIMESTAMPTZ AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') OR
       ledger_payload->>'observed_at' IS DISTINCT FROM to_char((ledger_payload->>'observed_at')::TIMESTAMPTZ AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') OR
       (legacy_payload->>'observed_at')::TIMESTAMPTZ < NEW.as_of OR (ledger_payload->>'observed_at')::TIMESTAMPTZ < NEW.as_of OR
       (legacy_payload->>'observed_at')::TIMESTAMPTZ > NEW.generated_at OR (ledger_payload->>'observed_at')::TIMESTAMPTZ > NEW.generated_at OR
       ((legacy_payload->>'synthetic')::BOOLEAN OR (ledger_payload->>'synthetic')::BOOLEAN) IS DISTINCT FROM NEW.synthetic THEN
        RAISE EXCEPTION 'accounting reconciliation source boundary differs';
    END IF;

    IF decoded_payload->>'version' IS DISTINCT FROM NEW.comparison_version OR decoded_payload->>'policy_version' IS DISTINCT FROM NEW.policy_version OR
       decoded_payload->>'account_id' IS DISTINCT FROM NEW.account_id::TEXT OR
       decoded_payload->>'as_of' IS DISTINCT FROM to_char(NEW.as_of AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') OR
       decoded_payload->>'generated_at' IS DISTINCT FROM to_char(NEW.generated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') OR
       decoded_payload->>'generator' IS DISTINCT FROM NEW.generator OR decoded_payload->>'projection_version' IS DISTINCT FROM NEW.projection_version OR
       decoded_payload->>'mark_source' IS DISTINCT FROM NEW.mark_source OR decoded_payload->>'mark_namespace' IS DISTINCT FROM NEW.mark_namespace OR
       (decoded_payload->>'max_mark_age_microseconds')::BIGINT IS DISTINCT FROM NEW.max_mark_age_microseconds OR
       decoded_payload->>'capture_fence_id' IS DISTINCT FROM NEW.capture_fence_id OR (decoded_payload->>'capture_epoch')::BIGINT IS DISTINCT FROM NEW.capture_epoch OR
       (decoded_payload->>'synthetic')::BOOLEAN IS DISTINCT FROM NEW.synthetic OR
       (decoded_payload->>'equal_count')::INTEGER IS DISTINCT FROM NEW.equal_count OR
       (decoded_payload->>'explained_count')::INTEGER IS DISTINCT FROM NEW.explained_count OR
       (decoded_payload->>'unexplained_count')::INTEGER IS DISTINCT FROM NEW.unexplained_count OR
       (decoded_payload->>'not_comparable_count')::INTEGER IS DISTINCT FROM NEW.not_comparable_count OR
       jsonb_typeof(decoded_payload->'results') IS DISTINCT FROM 'array' OR jsonb_array_length(decoded_payload->'results') IS DISTINCT FROM NEW.result_count THEN
        RAISE EXCEPTION 'accounting reconciliation payload header or counts differ';
    END IF;

    expected_legacy_id := economic_deterministic_uuid(
        'accounting-reconciliation-snapshot',
        legacy_payload->>'version', legacy_payload->>'source', NEW.account_id::TEXT, NEW.legacy_snapshot_checksum
    );
    expected_ledger_id := economic_deterministic_uuid(
        'accounting-reconciliation-snapshot',
        ledger_payload->>'version', ledger_payload->>'source', NEW.account_id::TEXT, NEW.ledger_snapshot_checksum
    );
    IF NEW.legacy_snapshot_id <> expected_legacy_id OR NEW.ledger_snapshot_id <> expected_ledger_id THEN
        RAISE EXCEPTION 'accounting reconciliation snapshot IDs differ from canonical evidence';
    END IF;
    expected_id := economic_deterministic_uuid(
        'accounting-reconciliation-run',
        NEW.comparison_version, NEW.policy_version, NEW.account_id::TEXT,
        NEW.legacy_snapshot_checksum, NEW.ledger_snapshot_checksum, NEW.checksum
    );
    IF NEW.id <> expected_id THEN
        RAISE EXCEPTION 'accounting reconciliation run ID differs from canonical evidence';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_accounting_reconciliation_runs_validate
    BEFORE INSERT ON accounting_reconciliation_runs
    FOR EACH ROW EXECUTE FUNCTION validate_accounting_reconciliation_run();

CREATE FUNCTION validate_accounting_reconciliation_result() RETURNS TRIGGER AS $$
DECLARE
    parent accounting_reconciliation_runs%ROWTYPE;
    encoded JSONB;
    explanation_identity TEXT := '';
    expected_id UUID;
BEGIN
    SELECT * INTO parent FROM accounting_reconciliation_runs WHERE id = NEW.run_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'accounting reconciliation parent run does not exist';
    END IF;
    SELECT item INTO encoded
    FROM jsonb_array_elements(parent.payload->'results') AS item
    WHERE item->>'fact_key' = NEW.fact_key;
    IF encoded IS NULL THEN
        RAISE EXCEPTION 'accounting reconciliation result is absent from parent bytes';
    END IF;

    IF encoded->>'id' IS DISTINCT FROM NEW.id::TEXT OR encoded->>'status' IS DISTINCT FROM NEW.status OR encoded->>'reason_code' IS DISTINCT FROM NEW.reason_code OR
       encoded->>'legacy_value' IS DISTINCT FROM (CASE WHEN NEW.legacy_value IS NULL THEN NULL ELSE NEW.legacy_value::TEXT END) OR
       encoded->>'ledger_value' IS DISTINCT FROM (CASE WHEN NEW.ledger_value IS NULL THEN NULL ELSE NEW.ledger_value::TEXT END) OR
       encoded->>'delta' IS DISTINCT FROM (CASE WHEN NEW.delta IS NULL THEN NULL ELSE NEW.delta::TEXT END) OR
       encoded->'explanation' IS DISTINCT FROM COALESCE(NEW.explanation, 'null'::JSONB) THEN
        RAISE EXCEPTION 'accounting reconciliation result differs from parent bytes';
    END IF;

    IF NEW.legacy_value IS NULL OR NEW.ledger_value IS NULL THEN
        IF NEW.status <> 'not_comparable' OR NEW.delta IS NOT NULL OR NEW.explanation IS NOT NULL THEN
            RAISE EXCEPTION 'missing accounting values must be not comparable';
        END IF;
    ELSE
        IF NEW.delta IS DISTINCT FROM NEW.ledger_value - NEW.legacy_value THEN
            RAISE EXCEPTION 'accounting reconciliation delta is not exact';
        END IF;
        IF NEW.delta = 0 AND (NEW.status <> 'equal' OR NEW.reason_code <> 'exact_match' OR NEW.explanation IS NOT NULL) THEN
            RAISE EXCEPTION 'zero accounting delta must be an unexplained-free exact match';
        ELSIF NEW.delta <> 0 AND NEW.explanation IS NULL AND (NEW.status <> 'unexplained' OR NEW.reason_code <> 'exact_delta_unexplained') THEN
            RAISE EXCEPTION 'unclassified accounting delta must be unexplained';
        ELSIF NEW.delta <> 0 AND NEW.explanation IS NOT NULL AND (NEW.status <> 'explained' OR NEW.reason_code IS DISTINCT FROM NEW.explanation->>'code') THEN
            RAISE EXCEPTION 'classified accounting delta must be explained';
        END IF;
    END IF;

    IF NEW.explanation IS NOT NULL THEN
        IF NEW.explanation->>'fact_key' IS DISTINCT FROM NEW.fact_key OR
           COALESCE(NEW.explanation->>'code', '') NOT IN (
               'legacy_binary_float_representation', 'legacy_mark_source_timing',
               'legacy_option_multiplier_semantics',
               'source_correction_evidence'
           ) OR
           btrim(COALESCE(NEW.explanation->>'rationale', '')) = '' OR
           btrim(COALESCE(NEW.explanation->>'evidence_ref', '')) = '' OR
           COALESCE(NEW.explanation->>'evidence_checksum', '') !~ '^[0-9a-f]{64}$' OR
           btrim(COALESCE(NEW.explanation->>'generator', '')) = '' OR
           btrim(COALESCE(NEW.explanation->>'reviewer', '')) = '' OR
           NEW.explanation->>'generator' IS DISTINCT FROM parent.generator OR
           NEW.explanation->>'generator' = NEW.explanation->>'reviewer' OR
           COALESCE(NEW.explanation->>'reviewed_at', '') !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$' THEN
            RAISE EXCEPTION 'accounting reconciliation explanation is structurally invalid';
        END IF;
        IF (NEW.explanation->>'reviewed_at')::TIMESTAMPTZ < GREATEST(
               (parent.payload->'legacy_snapshot'->>'observed_at')::TIMESTAMPTZ,
               (parent.payload->'ledger_snapshot'->>'observed_at')::TIMESTAMPTZ
           ) OR (NEW.explanation->>'reviewed_at')::TIMESTAMPTZ > parent.generated_at THEN
            RAISE EXCEPTION 'accounting reconciliation explanation review time is outside the evidence window';
        END IF;
        explanation_identity := concat_ws(chr(31),
            NEW.explanation->>'fact_key', NEW.explanation->>'code',
            NEW.explanation->>'rationale', NEW.explanation->>'evidence_ref',
            NEW.explanation->>'evidence_checksum', NEW.explanation->>'generator',
            NEW.explanation->>'reviewer', NEW.explanation->>'reviewed_at'
        );
    END IF;

    expected_id := economic_deterministic_uuid(
        'accounting-reconciliation-result',
        parent.legacy_snapshot_id::TEXT, parent.ledger_snapshot_id::TEXT,
        NEW.fact_key, NEW.status,
        COALESCE(NEW.legacy_value::TEXT, 'missing'),
        COALESCE(NEW.ledger_value::TEXT, 'missing'),
        COALESCE(NEW.delta::TEXT, 'missing'), explanation_identity
    );
    IF NEW.id <> expected_id THEN
        RAISE EXCEPTION 'accounting reconciliation result ID differs from canonical evidence';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_accounting_reconciliation_results_validate
    BEFORE INSERT ON accounting_reconciliation_results
    FOR EACH ROW EXECUTE FUNCTION validate_accounting_reconciliation_result();

CREATE FUNCTION assert_accounting_reconciliation_run_complete(target_run_id UUID) RETURNS VOID AS $$
DECLARE
    expected accounting_reconciliation_runs%ROWTYPE;
    actual_count BIGINT;
    actual_equal BIGINT;
    actual_explained BIGINT;
    actual_unexplained BIGINT;
    actual_not_comparable BIGINT;
BEGIN
    SELECT * INTO expected FROM accounting_reconciliation_runs WHERE id = target_run_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    SELECT COUNT(*), COUNT(*) FILTER (WHERE status='equal'), COUNT(*) FILTER (WHERE status='explained'),
           COUNT(*) FILTER (WHERE status='unexplained'), COUNT(*) FILTER (WHERE status='not_comparable')
    INTO actual_count, actual_equal, actual_explained, actual_unexplained, actual_not_comparable
    FROM accounting_reconciliation_results WHERE run_id = target_run_id;
    IF actual_count <> expected.result_count OR actual_equal <> expected.equal_count OR
       actual_explained <> expected.explained_count OR actual_unexplained <> expected.unexplained_count OR
       actual_not_comparable <> expected.not_comparable_count THEN
        RAISE EXCEPTION 'accounting reconciliation result set is incomplete';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_accounting_reconciliation_parent_complete() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_accounting_reconciliation_run_complete(COALESCE(NEW.id, OLD.id));
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_accounting_reconciliation_result_set_complete() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_accounting_reconciliation_run_complete(COALESCE(NEW.run_id, OLD.run_id));
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_accounting_reconciliation_run_complete
    AFTER INSERT OR UPDATE ON accounting_reconciliation_runs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_accounting_reconciliation_parent_complete();

CREATE CONSTRAINT TRIGGER trg_accounting_reconciliation_results_complete
    AFTER INSERT OR UPDATE OR DELETE ON accounting_reconciliation_results
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_accounting_reconciliation_result_set_complete();
