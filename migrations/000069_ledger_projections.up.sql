-- Canonical instrument marks and immutable, byte-preserved full-rebuild
-- portfolio checkpoints. Existing schema-65 shell rows remain explicitly
-- legacy and are never selected by the schema-69 repository.

DROP INDEX idx_mark_observations_source_identity;

ALTER TABLE mark_observations
    DROP CONSTRAINT mark_observations_price_check;

ALTER TABLE mark_observations
    ALTER COLUMN price TYPE NUMERIC USING price::NUMERIC;

ALTER TABLE mark_observations
    ADD CONSTRAINT mark_observations_price_check CHECK (
        price >= 0 AND
        price < 100000000000000000000000000 AND
        price = round(price, 12)
    ),
    ADD COLUMN instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    ADD COLUMN source_namespace TEXT,
    ADD COLUMN source_revision TEXT;

ALTER TABLE mark_observations
    ADD CONSTRAINT mark_observations_canonical_shape_check CHECK (
        (
            instrument_id IS NULL AND
            source_namespace IS NULL AND
            source_revision IS NULL
        ) OR (
            instrument_id IS NOT NULL AND
            source_namespace IS NOT NULL AND
            source_revision IS NOT NULL
        )
    );

CREATE UNIQUE INDEX idx_mark_observations_legacy_identity
    ON mark_observations (
        unit_kind, unit, price_currency, source, effective_at,
        COALESCE(source_observation_id, '')
    )
    WHERE instrument_id IS NULL AND source_namespace IS NULL AND source_revision IS NULL;

CREATE UNIQUE INDEX idx_mark_observations_canonical_identity
    ON mark_observations (
        instrument_id, price_currency, source, source_namespace,
        source_observation_id
    )
    WHERE instrument_id IS NOT NULL;

CREATE INDEX idx_mark_observations_canonical_latest
    ON mark_observations (
        instrument_id, price_currency, source, source_namespace,
        effective_at DESC, observed_at DESC, id DESC
    )
    WHERE instrument_id IS NOT NULL;

CREATE FUNCTION validate_canonical_mark_observation() RETURNS TRIGGER AS $$
DECLARE
    instrument_currency TEXT;
    expected_id UUID;
BEGIN
    IF NEW.instrument_id IS NULL OR NEW.source_namespace IS NULL OR NEW.source_revision IS NULL THEN
        RAISE EXCEPTION 'new mark observations must use the schema-69 canonical shape';
    END IF;
    IF NEW.unit_kind <> 'instrument' OR NEW.unit <> NEW.instrument_id::TEXT THEN
        RAISE EXCEPTION 'canonical mark unit must equal its instrument identity';
    END IF;
    IF NEW.source_observation_id IS NULL OR NEW.source_observation_id = '' OR
       NEW.source_observation_id <> btrim(NEW.source_observation_id) THEN
        RAISE EXCEPTION 'canonical mark source observation identity is required and normalized';
    END IF;
    IF NEW.source = '' OR NEW.source <> lower(btrim(NEW.source)) THEN
        RAISE EXCEPTION 'canonical mark source must be lowercase normalized';
    END IF;
    IF NEW.source_namespace = '' OR NEW.source_namespace <> btrim(NEW.source_namespace) OR
       NEW.source_revision <> btrim(NEW.source_revision) THEN
        RAISE EXCEPTION 'canonical mark namespace and revision must be normalized';
    END IF;
    IF NEW.observed_at < NEW.effective_at THEN
        RAISE EXCEPTION 'canonical mark observation cannot precede effective time';
    END IF;

    SELECT currency INTO instrument_currency
    FROM instruments
    WHERE id = NEW.instrument_id;
    IF instrument_currency IS NULL OR NEW.price_currency <> instrument_currency THEN
        RAISE EXCEPTION 'canonical mark currency must equal instrument currency';
    END IF;

    expected_id := economic_deterministic_uuid(
        'canonical-mark-observation',
        NEW.instrument_id::TEXT,
        NEW.price_currency,
        NEW.source,
        NEW.source_namespace,
        NEW.source_observation_id
    );
    IF NEW.id <> expected_id THEN
        RAISE EXCEPTION 'canonical mark ID does not match source identity';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_mark_observations_canonical
    BEFORE INSERT ON mark_observations
    FOR EACH ROW EXECUTE FUNCTION validate_canonical_mark_observation();

-- The database writer identity and the replay attestor are separate
-- capabilities. Operators provision a versioned 32-byte HMAC secret through
-- the migration-owner connection and inject the same secret into the replay
-- worker from an external secret store. Runtime projection roles receive no
-- access to either table.
CREATE TABLE projection_checkpoint_signing_keys (
    key_id          TEXT PRIMARY KEY CHECK (
        key_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
    ),
    signing_secret  BYTEA NOT NULL CHECK (octet_length(signing_secret) = 32),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT NOT NULL CHECK (
        created_by <> '' AND created_by = btrim(created_by)
    )
);

CREATE TRIGGER trg_projection_checkpoint_signing_keys_immutable
    BEFORE UPDATE OR DELETE ON projection_checkpoint_signing_keys
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE projection_checkpoint_signing_key_revocations (
    key_id       TEXT PRIMARY KEY REFERENCES projection_checkpoint_signing_keys(key_id) ON DELETE RESTRICT,
    revoked_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason       TEXT NOT NULL CHECK (reason <> '' AND reason = btrim(reason)),
    revoked_by   TEXT NOT NULL CHECK (revoked_by <> '' AND revoked_by = btrim(revoked_by))
);

CREATE TRIGGER trg_projection_checkpoint_signing_key_revocations_immutable
    BEFORE UPDATE OR DELETE ON projection_checkpoint_signing_key_revocations
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

REVOKE ALL ON projection_checkpoint_signing_keys FROM PUBLIC;
REVOKE ALL ON projection_checkpoint_signing_key_revocations FROM PUBLIC;

ALTER TABLE projection_checkpoints
    DROP CONSTRAINT projection_checkpoints_account_id_projection_type_through_t_key;

ALTER TABLE projection_checkpoints
    ADD COLUMN projection_version TEXT,
    ADD COLUMN as_of TIMESTAMPTZ,
    ADD COLUMN fifo_method TEXT,
    ADD COLUMN base_currency TEXT,
    ADD COLUMN mark_source TEXT,
    ADD COLUMN mark_namespace TEXT,
    ADD COLUMN max_mark_age_microseconds BIGINT,
    ADD COLUMN transaction_count INTEGER,
    ADD COLUMN mark_count INTEGER,
    ADD COLUMN lot_count INTEGER,
    ADD COLUMN match_count INTEGER,
    ADD COLUMN position_count INTEGER,
    ADD COLUMN input_checksum TEXT,
    ADD COLUMN payload_bytes BYTEA,
    ADD COLUMN attestation_key_id TEXT REFERENCES projection_checkpoint_signing_keys(key_id) ON DELETE RESTRICT,
    ADD COLUMN attestation_hmac BYTEA;

ALTER TABLE projection_checkpoints
    ADD CONSTRAINT projection_checkpoints_canonical_shape_check CHECK (
        (
            projection_version IS NULL AND as_of IS NULL AND fifo_method IS NULL AND
            base_currency IS NULL AND mark_source IS NULL AND mark_namespace IS NULL AND
            max_mark_age_microseconds IS NULL AND transaction_count IS NULL AND
            mark_count IS NULL AND lot_count IS NULL AND match_count IS NULL AND
            position_count IS NULL AND input_checksum IS NULL AND payload_bytes IS NULL AND
            attestation_key_id IS NULL AND attestation_hmac IS NULL
        ) OR (
            projection_version IS NOT NULL AND as_of IS NOT NULL AND fifo_method IS NOT NULL AND
            base_currency IS NOT NULL AND mark_source IS NOT NULL AND mark_namespace IS NOT NULL AND
            max_mark_age_microseconds IS NOT NULL AND transaction_count IS NOT NULL AND
            mark_count IS NOT NULL AND lot_count IS NOT NULL AND match_count IS NOT NULL AND
            position_count IS NOT NULL AND input_checksum IS NOT NULL AND payload_bytes IS NOT NULL AND
            attestation_key_id IS NOT NULL AND attestation_hmac IS NOT NULL
        )
    );

CREATE UNIQUE INDEX idx_projection_checkpoints_legacy_identity
    ON projection_checkpoints (account_id, projection_type, through_transaction_id)
    WHERE projection_version IS NULL;

CREATE UNIQUE INDEX idx_projection_checkpoints_canonical_identity
    ON projection_checkpoints (
        account_id, projection_type, projection_version, as_of, input_checksum
    )
    WHERE projection_version IS NOT NULL;

CREATE INDEX idx_projection_checkpoints_canonical_latest
    ON projection_checkpoints (
        account_id, projection_type, projection_version, as_of DESC, id DESC
    )
    WHERE projection_version IS NOT NULL;

CREATE FUNCTION validate_canonical_projection_checkpoint() RETURNS TRIGGER AS $$
DECLARE
    expected_account_currency TEXT;
    expected_transaction_count INTEGER;
    expected_through_transaction_id UUID;
    expected_id UUID;
    expected_as_of_text TEXT;
    decoded_payload JSONB;
    signing_secret BYTEA;
    expected_attestation_hmac BYTEA;
BEGIN
    IF NEW.projection_version IS NULL OR NEW.as_of IS NULL OR NEW.fifo_method IS NULL OR
       NEW.base_currency IS NULL OR NEW.mark_source IS NULL OR NEW.mark_namespace IS NULL OR
       NEW.max_mark_age_microseconds IS NULL OR NEW.transaction_count IS NULL OR
       NEW.mark_count IS NULL OR NEW.lot_count IS NULL OR NEW.match_count IS NULL OR
       NEW.position_count IS NULL OR NEW.input_checksum IS NULL OR NEW.payload_bytes IS NULL OR
       NEW.attestation_key_id IS NULL OR NEW.attestation_hmac IS NULL THEN
        RAISE EXCEPTION 'new projection checkpoints must use the schema-69 canonical shape';
    END IF;
    IF NEW.projection_type <> 'portfolio' OR NEW.projection_version <> 'ledger_fifo_v1' OR
       NEW.fifo_method <> 'fifo' THEN
        RAISE EXCEPTION 'unsupported canonical projection contract';
    END IF;
    IF NEW.mark_source = '' OR NEW.mark_source <> lower(btrim(NEW.mark_source)) OR
       NEW.mark_namespace = '' OR NEW.mark_namespace <> btrim(NEW.mark_namespace) THEN
        RAISE EXCEPTION 'canonical checkpoint mark policy is not normalized';
    END IF;
    IF NEW.max_mark_age_microseconds <= 0 OR NEW.transaction_count <= 0 OR
       NEW.mark_count < 0 OR NEW.lot_count < 0 OR NEW.match_count < 0 OR NEW.position_count < 0 THEN
        RAISE EXCEPTION 'canonical checkpoint counts or mark age are invalid';
    END IF;
    IF NEW.input_checksum !~ '^[0-9a-f]{64}$' OR octet_length(NEW.payload_bytes) = 0 THEN
        RAISE EXCEPTION 'canonical checkpoint input checksum or payload bytes are invalid';
    END IF;
    IF NEW.attestation_key_id !~ '^[a-z0-9][a-z0-9._-]{0,127}$' OR
       octet_length(NEW.attestation_hmac) <> 32 THEN
        RAISE EXCEPTION 'canonical checkpoint attestation shape is invalid';
    END IF;

    SELECT signing_key.signing_secret INTO signing_secret
    FROM projection_checkpoint_signing_keys AS signing_key
    WHERE signing_key.key_id = NEW.attestation_key_id
      AND NOT EXISTS (
          SELECT 1
          FROM projection_checkpoint_signing_key_revocations AS revocation
          WHERE revocation.key_id = signing_key.key_id
      );
    IF signing_secret IS NULL THEN
        RAISE EXCEPTION 'canonical checkpoint attestation key is unknown or revoked';
    END IF;
    expected_attestation_hmac := hmac(
        convert_to('augr-projection-checkpoint-hmac-v1', 'UTF8') ||
        decode('00', 'hex') ||
        convert_to(NEW.attestation_key_id, 'UTF8') ||
        decode('00', 'hex') ||
        NEW.payload_bytes,
        signing_secret,
        'sha256'
    );
    IF NEW.attestation_hmac IS DISTINCT FROM expected_attestation_hmac THEN
        RAISE EXCEPTION 'canonical checkpoint attestation HMAC does not match exact payload bytes';
    END IF;

    SELECT base_currency INTO expected_account_currency
    FROM accounts
    WHERE id = NEW.account_id;
    IF expected_account_currency IS NULL OR NEW.base_currency <> expected_account_currency THEN
        RAISE EXCEPTION 'canonical checkpoint currency must equal account currency';
    END IF;

    BEGIN
        decoded_payload := convert_from(NEW.payload_bytes, 'UTF8')::JSONB;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'canonical checkpoint payload bytes must contain UTF-8 JSON';
    END;
    IF NEW.payload <> decoded_payload THEN
        RAISE EXCEPTION 'canonical checkpoint JSONB does not match payload bytes';
    END IF;
    IF NEW.checksum <> encode(digest(NEW.payload_bytes, 'sha256'), 'hex') THEN
        RAISE EXCEPTION 'canonical checkpoint checksum does not match payload bytes';
    END IF;

    SELECT COUNT(*)::INTEGER INTO expected_transaction_count
    FROM ledger_transactions
    WHERE account_id = NEW.account_id
      AND effective_at <= NEW.as_of
      AND observed_at <= NEW.as_of;
    IF expected_transaction_count = 0 THEN
        RAISE EXCEPTION 'canonical checkpoint requires at least one eligible ledger transaction';
    END IF;
    SELECT id INTO expected_through_transaction_id
    FROM ledger_transactions
    WHERE account_id = NEW.account_id
      AND effective_at <= NEW.as_of
      AND observed_at <= NEW.as_of
    ORDER BY effective_at DESC, observed_at DESC, id DESC
    LIMIT 1;
    IF NEW.transaction_count <> expected_transaction_count OR
       NEW.through_transaction_id <> expected_through_transaction_id THEN
        RAISE EXCEPTION 'canonical checkpoint ledger boundary does not match its bitemporal snapshot';
    END IF;

    expected_as_of_text := to_char(
        NEW.as_of AT TIME ZONE 'UTC',
        'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
    );
    expected_id := economic_deterministic_uuid(
        'portfolio-projection-checkpoint',
        NEW.account_id::TEXT,
        NEW.projection_type,
        NEW.projection_version,
        expected_as_of_text,
        NEW.input_checksum
    );
    IF NEW.id <> expected_id THEN
        RAISE EXCEPTION 'canonical checkpoint ID does not match input identity';
    END IF;

    IF NEW.payload->>'checkpoint_id' IS DISTINCT FROM NEW.id::TEXT OR
       NEW.payload->>'projection_type' IS DISTINCT FROM NEW.projection_type OR
       NEW.payload->>'version' IS DISTINCT FROM NEW.projection_version OR
       NEW.payload->>'fifo' IS DISTINCT FROM NEW.fifo_method OR
       NEW.payload->>'account_id' IS DISTINCT FROM NEW.account_id::TEXT OR
       NEW.payload->>'base_currency' IS DISTINCT FROM NEW.base_currency OR
       NEW.payload->>'as_of' IS DISTINCT FROM expected_as_of_text OR
       NEW.payload->>'mark_source' IS DISTINCT FROM NEW.mark_source OR
       NEW.payload->>'mark_namespace' IS DISTINCT FROM NEW.mark_namespace OR
       (NEW.payload->>'max_mark_age_microseconds')::BIGINT IS DISTINCT FROM NEW.max_mark_age_microseconds OR
       NEW.payload->>'through_transaction_id' IS DISTINCT FROM NEW.through_transaction_id::TEXT OR
       (NEW.payload->>'transaction_count')::INTEGER IS DISTINCT FROM NEW.transaction_count OR
       NEW.payload->>'input_checksum' IS DISTINCT FROM NEW.input_checksum THEN
        RAISE EXCEPTION 'canonical checkpoint payload header does not match relational evidence';
    END IF;
    IF jsonb_typeof(NEW.payload->'marks') IS DISTINCT FROM 'array' OR
       jsonb_typeof(NEW.payload->'lots') IS DISTINCT FROM 'array' OR
       jsonb_typeof(NEW.payload->'matches') IS DISTINCT FROM 'array' OR
       jsonb_typeof(NEW.payload->'positions') IS DISTINCT FROM 'array' OR
       jsonb_typeof(NEW.payload->'totals') IS DISTINCT FROM 'object' OR
       jsonb_array_length(NEW.payload->'marks') IS DISTINCT FROM NEW.mark_count OR
       jsonb_array_length(NEW.payload->'lots') IS DISTINCT FROM NEW.lot_count OR
       jsonb_array_length(NEW.payload->'matches') IS DISTINCT FROM NEW.match_count OR
       jsonb_array_length(NEW.payload->'positions') IS DISTINCT FROM NEW.position_count THEN
        RAISE EXCEPTION 'canonical checkpoint payload counts do not match relational evidence';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_projection_checkpoints_canonical
    BEFORE INSERT ON projection_checkpoints
    FOR EACH ROW EXECUTE FUNCTION validate_canonical_projection_checkpoint();

-- A canonical checkpoint is an attestation produced by the pure replay engine,
-- not an arbitrary runtime row. This function is the only write capability a
-- dedicated projection-writer role receives. It derives every relational
-- envelope field from the exact canonical bytes; the trigger then verifies the
-- separate replay-worker HMAC and database-visible bitemporal boundary.
-- Deployment must grant EXECUTE only to a non-owner role with no direct
-- checkpoint DML, signing-key read access, or schema-creation rights.
CREATE FUNCTION persist_canonical_projection_checkpoint(
    checkpoint_payload_bytes BYTEA,
    checkpoint_attestation_key_id TEXT,
    checkpoint_attestation_hmac BYTEA
)
RETURNS TABLE (persisted_id UUID) AS $$
DECLARE
    decoded_payload JSONB;
BEGIN
    BEGIN
        decoded_payload := convert_from(checkpoint_payload_bytes, 'UTF8')::JSONB;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'canonical checkpoint payload bytes must contain UTF-8 JSON';
    END;

    RETURN QUERY
    INSERT INTO projection_checkpoints (
        id, account_id, projection_type, through_transaction_id, payload, checksum,
        projection_version, as_of, fifo_method, base_currency,
        mark_source, mark_namespace, max_mark_age_microseconds,
        transaction_count, mark_count, lot_count, match_count, position_count,
        input_checksum, payload_bytes, attestation_key_id, attestation_hmac
    ) VALUES (
        (decoded_payload->>'checkpoint_id')::UUID,
        (decoded_payload->>'account_id')::UUID,
        decoded_payload->>'projection_type',
        (decoded_payload->>'through_transaction_id')::UUID,
        decoded_payload,
        encode(digest(checkpoint_payload_bytes, 'sha256'), 'hex'),
        decoded_payload->>'version',
        (decoded_payload->>'as_of')::TIMESTAMPTZ,
        decoded_payload->>'fifo',
        decoded_payload->>'base_currency',
        decoded_payload->>'mark_source',
        decoded_payload->>'mark_namespace',
        (decoded_payload->>'max_mark_age_microseconds')::BIGINT,
        (decoded_payload->>'transaction_count')::INTEGER,
        jsonb_array_length(decoded_payload->'marks'),
        jsonb_array_length(decoded_payload->'lots'),
        jsonb_array_length(decoded_payload->'matches'),
        jsonb_array_length(decoded_payload->'positions'),
        decoded_payload->>'input_checksum',
        checkpoint_payload_bytes,
        checkpoint_attestation_key_id,
        checkpoint_attestation_hmac
    )
    ON CONFLICT (
        account_id, projection_type, projection_version, as_of, input_checksum
    ) WHERE projection_version IS NOT NULL DO NOTHING
    RETURNING projection_checkpoints.id;
END;
$$ LANGUAGE plpgsql
SECURITY DEFINER;

-- Pin the definer function to catalog, the migration target schema, the
-- pgcrypto extension schema, and pg_temp last. The migration target is dynamic
-- so isolated-schema rehearsals exercise the same object binding as public.
DO $$
DECLARE
    target_schema TEXT := current_schema();
    pgcrypto_schema TEXT;
BEGIN
    SELECT namespace.nspname INTO pgcrypto_schema
    FROM pg_extension AS extension
    JOIN pg_namespace AS namespace ON namespace.oid = extension.extnamespace
    WHERE extension.extname = 'pgcrypto';
    IF pgcrypto_schema IS NULL THEN
        RAISE EXCEPTION 'pgcrypto extension schema is required for canonical checkpoints';
    END IF;
    EXECUTE format(
        'ALTER FUNCTION %I.persist_canonical_projection_checkpoint(BYTEA, TEXT, BYTEA) '
        'SET search_path TO pg_catalog, %I, %I, pg_temp',
        target_schema,
        target_schema,
        pgcrypto_schema
    );
END;
$$;

REVOKE ALL ON FUNCTION persist_canonical_projection_checkpoint(BYTEA, TEXT, BYTEA) FROM PUBLIC;
