-- Immutable capital-tier/margin-policy artifacts and explicit account
-- bindings. This migration seeds no account or policy, changes no runtime
-- route, grants no writer, and activates no risk admission path.

-- Close the account-creation race while the binding table and its independent
-- account-policy validator are installed.
LOCK TABLE accounts IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION capital_margin_policy_v1_canonical_bytes(policy JSONB) RETURNS BYTEA AS $function$
DECLARE
    reviewed_text CONSTANT TEXT := $policy${"schema":"capital-margin-policy-v1","currency":"USD","scale":12,"tiers":["500","5000","25000","100000","1000000","5000000"],"profiles":[{"name":"cash","initial_long":"1","initial_short":"0","maintenance_long":"1","maintenance_short":"0","maximum_gross":"1","cash_reserve":"0","allow_short":false,"unlimited":false},{"name":"portfolio","initial_long":"0.15","initial_short":"0.3","maintenance_long":"0.15","maintenance_short":"0.3","maximum_gross":"6","cash_reserve":"0","allow_short":true,"unlimited":false},{"name":"reg_t","initial_long":"0.5","initial_short":"1.5","maintenance_long":"0.25","maintenance_short":"0.3","maximum_gross":"2","cash_reserve":"0","allow_short":true,"unlimited":false},{"name":"stress_unlimited","initial_long":"0","initial_short":"0","maintenance_long":"0","maintenance_short":"0","maximum_gross":"0","cash_reserve":"0","allow_short":true,"unlimited":true}]}$policy$;
BEGIN
    IF policy <> reviewed_text::JSONB THEN
        RAISE EXCEPTION 'capital margin policy is not the reviewed v1 artifact';
    END IF;
    RETURN convert_to(reviewed_text, 'UTF8');
END;
$function$ LANGUAGE plpgsql IMMUTABLE STRICT;

CREATE TABLE capital_margin_policy_artifacts (
    id              UUID PRIMARY KEY,
    schema_name     TEXT NOT NULL CHECK (schema_name = 'capital-margin-policy-v1'),
    policy_version  TEXT NOT NULL UNIQUE CHECK (
                        policy_version = btrim(policy_version) AND
                        char_length(policy_version) <= 256
                    ),
    sha256          TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes) > 0),
    canonical_json  JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', NOW())
                    CHECK (created_at = date_trunc('microseconds', created_at)),
    UNIQUE (id, policy_version),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (policy_version = schema_name || '@sha256:' || sha256),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK (canonical_bytes = capital_margin_policy_v1_canonical_bytes(canonical_json)),
    CHECK (id = economic_deterministic_uuid(
        'capital-margin-policy-artifact', policy_version
    ))
);

CREATE FUNCTION reject_capital_policy_fact_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_capital_margin_policy_artifacts_immutable
    BEFORE UPDATE OR DELETE ON capital_margin_policy_artifacts
    FOR EACH ROW EXECUTE FUNCTION reject_capital_policy_fact_mutation();

CREATE TABLE account_capital_policy_bindings (
    id                       UUID PRIMARY KEY,
    account_id               UUID NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE RESTRICT,
    policy_artifact_id       UUID NOT NULL,
    policy_version           TEXT NOT NULL,
    tier                     NUMERIC(28, 8) NOT NULL CHECK (tier > 0),
    margin_profile           TEXT NOT NULL CHECK (margin_profile IN ('cash', 'reg_t', 'portfolio', 'stress_unlimited')),
    environment              TEXT NOT NULL CHECK (environment IN ('paper_scored', 'paper_stress')),
    starting_capital         NUMERIC(28, 8) NOT NULL CHECK (starting_capital > 0),
    buying_power_multiplier  NUMERIC(20, 8) NOT NULL CHECK (buying_power_multiplier >= 0),
    evidence_class           TEXT NOT NULL CHECK (evidence_class IN ('promotion_evidence', 'synthetic_stress')),
    storage_namespace        TEXT NOT NULL CHECK (storage_namespace <> '' AND storage_namespace = btrim(storage_namespace)),
    currency                 TEXT NOT NULL CHECK (currency = 'USD'),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', NOW())
                             CHECK (created_at = date_trunc('microseconds', created_at)),
    FOREIGN KEY (policy_artifact_id, policy_version)
        REFERENCES capital_margin_policy_artifacts(id, policy_version) ON DELETE RESTRICT,
    CHECK (tier = starting_capital),
    CHECK (id = economic_deterministic_uuid(
        'capital-policy-binding', account_id::TEXT, policy_version
    ))
);

CREATE INDEX idx_account_capital_policy_bindings_policy
    ON account_capital_policy_bindings (policy_version, account_id);

CREATE TRIGGER trg_account_capital_policy_bindings_immutable
    BEFORE UPDATE OR DELETE ON account_capital_policy_bindings
    FOR EACH ROW EXECUTE FUNCTION reject_capital_policy_fact_mutation();

CREATE FUNCTION validate_account_capital_policy_binding() RETURNS TRIGGER AS $$
DECLARE
    account_row accounts%ROWTYPE;
    artifact_row capital_margin_policy_artifacts%ROWTYPE;
    profile_value JSONB;
BEGIN
    SELECT * INTO account_row FROM accounts WHERE id = NEW.account_id;
    SELECT * INTO artifact_row FROM capital_margin_policy_artifacts
        WHERE id = NEW.policy_artifact_id AND policy_version = NEW.policy_version;
    IF account_row.id IS NULL OR artifact_row.id IS NULL THEN
        RAISE EXCEPTION 'capital binding requires an existing account and exact policy artifact';
    END IF;
    IF NEW.environment <> account_row.environment OR
       NEW.starting_capital <> account_row.starting_capital OR
       NEW.buying_power_multiplier <> account_row.buying_power_multiplier OR
       NEW.margin_profile <> account_row.margin_profile OR
       NEW.evidence_class <> account_row.evidence_class OR
       NEW.storage_namespace <> account_row.storage_namespace OR
       NEW.currency <> account_row.base_currency OR
       NEW.tier <> account_row.starting_capital THEN
        RAISE EXCEPTION 'capital binding copied account facts do not match';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements_text(artifact_row.canonical_json -> 'tiers') AS tier_value
        WHERE tier_value::NUMERIC = NEW.tier
    ) THEN
        RAISE EXCEPTION 'capital binding tier is not reviewed';
    END IF;
    SELECT value INTO profile_value
    FROM jsonb_array_elements(artifact_row.canonical_json -> 'profiles') AS profiles(value)
    WHERE value ->> 'name' = NEW.margin_profile;
    IF profile_value IS NULL THEN
        RAISE EXCEPTION 'capital binding profile is not reviewed';
    END IF;
    IF NEW.environment = 'paper_scored' THEN
        IF NEW.evidence_class <> 'promotion_evidence' OR
           NEW.storage_namespace NOT LIKE 'paper_scored/%' OR
           NEW.margin_profile = 'stress_unlimited' OR
           (profile_value ->> 'unlimited')::BOOLEAN OR
           NEW.buying_power_multiplier <> (profile_value ->> 'maximum_gross')::NUMERIC OR
           NEW.buying_power_multiplier <= 0 THEN
            RAISE EXCEPTION 'scored capital binding requires a matching finite promotion profile';
        END IF;
    ELSIF NEW.environment = 'paper_stress' THEN
        IF NEW.evidence_class <> 'synthetic_stress' OR
           NEW.storage_namespace NOT LIKE 'paper_stress/%' OR
           NEW.margin_profile <> 'stress_unlimited' OR
           NOT (profile_value ->> 'unlimited')::BOOLEAN OR
           NEW.buying_power_multiplier <> 0 THEN
            RAISE EXCEPTION 'stress capital binding requires isolated stress-unlimited facts';
        END IF;
    ELSE
        RAISE EXCEPTION 'capital policy v1 supports only scored and stress paper accounts';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_account_capital_policy_bindings_validate
    BEFORE INSERT ON account_capital_policy_bindings
    FOR EACH ROW EXECUTE FUNCTION validate_account_capital_policy_binding();
