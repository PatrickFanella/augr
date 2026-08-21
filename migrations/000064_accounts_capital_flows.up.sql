-- Explicit economic accounts and append-only capital flows.

CREATE TABLE accounts (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                     TEXT NOT NULL CHECK (btrim(name) <> ''),
    environment              TEXT NOT NULL CHECK (environment IN ('paper_scored', 'paper_stress', 'shadow', 'live')),
    venue                    TEXT NOT NULL CHECK (btrim(venue) <> ''),
    external_account_id      TEXT,
    base_currency            TEXT NOT NULL CHECK (base_currency ~ '^[A-Z]{3}$'),
    storage_namespace        TEXT NOT NULL UNIQUE CHECK (btrim(storage_namespace) <> ''),
    evidence_class           TEXT NOT NULL CHECK (evidence_class IN ('promotion_evidence', 'synthetic_stress', 'non_promotion')),
    starting_capital         NUMERIC(28, 8) NOT NULL CHECK (starting_capital > 0),
    buying_power_multiplier  NUMERIC(20, 8) NOT NULL CHECK (buying_power_multiplier >= 0),
    margin_profile           TEXT NOT NULL CHECK (margin_profile IN ('cash', 'reg_t', 'portfolio', 'stress_unlimited')),
    status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'closed')),
    created_by               TEXT NOT NULL CHECK (btrim(created_by) <> ''),
    creation_metadata        JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(creation_metadata) = 'object'),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (environment = 'paper_scored' AND evidence_class = 'promotion_evidence') OR
        (environment = 'paper_stress' AND evidence_class = 'synthetic_stress') OR
        (environment IN ('shadow', 'live') AND evidence_class = 'non_promotion')
    ),
    CHECK (environment = 'paper_stress' OR buying_power_multiplier > 0),
    CHECK (margin_profile <> 'stress_unlimited' OR environment = 'paper_stress'),
    CHECK (environment NOT IN ('paper_scored', 'paper_stress') OR storage_namespace LIKE environment || '/%')
);

CREATE UNIQUE INDEX idx_accounts_external_identity
    ON accounts (environment, venue, external_account_id)
    WHERE external_account_id IS NOT NULL;

CREATE FUNCTION reject_account_identity_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'account economic identity is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_accounts_immutable_identity
    BEFORE UPDATE OF environment, venue, external_account_id, base_currency,
        storage_namespace, evidence_class, starting_capital,
        buying_power_multiplier, margin_profile, created_by,
        creation_metadata, created_at
    ON accounts
    FOR EACH ROW EXECUTE FUNCTION reject_account_identity_mutation();

CREATE TABLE capital_flows (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id          UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    flow_type           TEXT NOT NULL CHECK (flow_type IN ('deposit', 'withdrawal')),
    amount              NUMERIC(28, 8) NOT NULL CHECK (amount > 0),
    currency            TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    idempotency_key     TEXT NOT NULL CHECK (btrim(idempotency_key) <> ''),
    source              TEXT NOT NULL CHECK (source IN ('account_opening', 'operator', 'venue', 'reconciliation', 'migration')),
    external_reference  TEXT,
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    effective_at        TIMESTAMPTZ NOT NULL,
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, idempotency_key)
);

CREATE INDEX idx_capital_flows_account_effective
    ON capital_flows (account_id, effective_at, id);

CREATE FUNCTION validate_capital_flow_currency() RETURNS TRIGGER AS $$
DECLARE
    account_currency TEXT;
BEGIN
    SELECT base_currency INTO account_currency
    FROM accounts
    WHERE id = NEW.account_id;

    IF account_currency IS NULL THEN
        RAISE EXCEPTION 'capital flow account % does not exist', NEW.account_id;
    END IF;
    IF NEW.currency <> account_currency THEN
        RAISE EXCEPTION 'capital flow currency % does not match account currency %', NEW.currency, account_currency;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_capital_flows_validate_currency
    BEFORE INSERT ON capital_flows
    FOR EACH ROW EXECUTE FUNCTION validate_capital_flow_currency();

CREATE FUNCTION reject_capital_flow_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'capital flows are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_capital_flows_immutable
    BEFORE UPDATE OR DELETE ON capital_flows
    FOR EACH ROW EXECUTE FUNCTION reject_capital_flow_mutation();

-- One explicit default account replaces implicit strategy-derived paper
-- identity. Legacy orders, positions, trades, and aggregates remain unscoped;
-- this migration does not invent account attribution for them.
INSERT INTO accounts (
    id,
    name,
    environment,
    venue,
    base_currency,
    storage_namespace,
    evidence_class,
    starting_capital,
    buying_power_multiplier,
    margin_profile,
    status,
    created_by,
    creation_metadata
) VALUES (
    '00000000-0000-4000-8000-000000000064'::UUID,
    'Default scored paper account',
    'paper_scored',
    'internal',
    'USD',
    'paper_scored/default',
    'promotion_evidence',
    100000.00000000,
    2.00000000,
    'reg_t',
    'active',
    'migration-000064',
    '{"backfill":"explicit_default","legacy_results_attached":false}'::JSONB
);

INSERT INTO capital_flows (
    id,
    account_id,
    flow_type,
    amount,
    currency,
    idempotency_key,
    source,
    metadata,
    effective_at,
    observed_at
) VALUES (
    '00000000-0000-4000-8000-000000000164'::UUID,
    '00000000-0000-4000-8000-000000000064'::UUID,
    'deposit',
    100000.00000000,
    'USD',
    'account-opening:00000000-0000-4000-8000-000000000064',
    'account_opening',
    '{"migration":"000064","legacy_results_attached":false}'::JSONB,
    NOW(),
    NOW()
);
