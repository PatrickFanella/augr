-- Migration runners must quiesce writers. The lock is deliberately first so
-- the canonical-data guard and restoration observe one stable table state.
LOCK TABLE mark_observations, projection_checkpoints,
    projection_checkpoint_signing_keys,
    projection_checkpoint_signing_key_revocations
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM mark_observations WHERE instrument_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM projection_checkpoints WHERE projection_version IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM projection_checkpoint_signing_keys
    ) OR EXISTS (
        SELECT 1 FROM projection_checkpoint_signing_key_revocations
    ) THEN
        RAISE EXCEPTION 'cannot roll back migration 69 while canonical marks, checkpoints, or signing keys exist';
    END IF;
END;
$$;

DROP FUNCTION IF EXISTS persist_canonical_projection_checkpoint(BYTEA, TEXT, BYTEA);
DROP TRIGGER IF EXISTS trg_projection_checkpoints_canonical ON projection_checkpoints;
DROP FUNCTION IF EXISTS validate_canonical_projection_checkpoint();
DROP INDEX IF EXISTS idx_projection_checkpoints_canonical_latest;
DROP INDEX IF EXISTS idx_projection_checkpoints_canonical_identity;
DROP INDEX IF EXISTS idx_projection_checkpoints_legacy_identity;

ALTER TABLE projection_checkpoints
    DROP CONSTRAINT IF EXISTS projection_checkpoints_canonical_shape_check,
    DROP COLUMN IF EXISTS attestation_hmac,
    DROP COLUMN IF EXISTS attestation_key_id,
    DROP COLUMN IF EXISTS payload_bytes,
    DROP COLUMN IF EXISTS input_checksum,
    DROP COLUMN IF EXISTS position_count,
    DROP COLUMN IF EXISTS match_count,
    DROP COLUMN IF EXISTS lot_count,
    DROP COLUMN IF EXISTS mark_count,
    DROP COLUMN IF EXISTS transaction_count,
    DROP COLUMN IF EXISTS max_mark_age_microseconds,
    DROP COLUMN IF EXISTS mark_namespace,
    DROP COLUMN IF EXISTS mark_source,
    DROP COLUMN IF EXISTS base_currency,
    DROP COLUMN IF EXISTS fifo_method,
    DROP COLUMN IF EXISTS as_of,
    DROP COLUMN IF EXISTS projection_version;

ALTER TABLE projection_checkpoints
    ADD CONSTRAINT projection_checkpoints_account_id_projection_type_through_t_key
    UNIQUE (account_id, projection_type, through_transaction_id);

DROP TRIGGER IF EXISTS trg_projection_checkpoint_signing_key_revocations_immutable
    ON projection_checkpoint_signing_key_revocations;
DROP TABLE IF EXISTS projection_checkpoint_signing_key_revocations;
DROP TRIGGER IF EXISTS trg_projection_checkpoint_signing_keys_immutable
    ON projection_checkpoint_signing_keys;
DROP TABLE IF EXISTS projection_checkpoint_signing_keys;

DROP TRIGGER IF EXISTS trg_mark_observations_canonical ON mark_observations;
DROP FUNCTION IF EXISTS validate_canonical_mark_observation();
DROP INDEX IF EXISTS idx_mark_observations_canonical_latest;
DROP INDEX IF EXISTS idx_mark_observations_canonical_identity;
DROP INDEX IF EXISTS idx_mark_observations_legacy_identity;

ALTER TABLE mark_observations
    DROP CONSTRAINT IF EXISTS mark_observations_canonical_shape_check,
    DROP CONSTRAINT IF EXISTS mark_observations_price_check;

ALTER TABLE mark_observations
    DROP COLUMN IF EXISTS source_revision,
    DROP COLUMN IF EXISTS source_namespace,
    DROP COLUMN IF EXISTS instrument_id;

ALTER TABLE mark_observations
    ALTER COLUMN price TYPE NUMERIC(38, 12) USING price::NUMERIC(38, 12),
    ADD CONSTRAINT mark_observations_price_check CHECK (price > 0);

CREATE UNIQUE INDEX idx_mark_observations_source_identity
    ON mark_observations (
        unit_kind, unit, price_currency, source, effective_at,
        COALESCE(source_observation_id, '')
    );
