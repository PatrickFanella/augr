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
