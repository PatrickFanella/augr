-- Append-only double-entry economic ledger. Signed amounts use debit-positive,
-- credit-negative convention and must balance independently for every unit.

CREATE TABLE ledger_transactions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id        UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    event_type        TEXT NOT NULL CHECK (event_type <> '' AND event_type = btrim(event_type)),
    idempotency_key   TEXT NOT NULL CHECK (idempotency_key <> '' AND idempotency_key = btrim(idempotency_key)),
    origin_type       TEXT NOT NULL CHECK (origin_type <> '' AND origin_type = btrim(origin_type)),
    origin_id         TEXT NOT NULL CHECK (origin_id <> '' AND origin_id = btrim(origin_id)),
    reference_type    TEXT,
    reference_id      TEXT,
    effective_at      TIMESTAMPTZ NOT NULL,
    observed_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    posting_count     INTEGER NOT NULL CHECK (posting_count >= 2),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, idempotency_key),
    UNIQUE (account_id, origin_type, origin_id),
    CHECK ((reference_type IS NULL) = (reference_id IS NULL)),
    CHECK (reference_type IS NULL OR (
        reference_type <> '' AND reference_type = btrim(reference_type) AND
        reference_id <> '' AND reference_id = btrim(reference_id)
    ))
);

CREATE INDEX idx_ledger_transactions_account_effective
    ON ledger_transactions (account_id, effective_at, id);

CREATE TABLE ledger_postings (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id    UUID NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
    idempotency_key   TEXT NOT NULL CHECK (idempotency_key <> '' AND idempotency_key = btrim(idempotency_key)),
    ledger_account    TEXT NOT NULL CHECK (ledger_account <> '' AND ledger_account = btrim(ledger_account)),
    unit_kind         TEXT NOT NULL CHECK (unit_kind IN ('currency', 'instrument')),
    unit              TEXT NOT NULL CHECK (unit <> '' AND unit = btrim(unit)),
    amount            NUMERIC(38, 12) NOT NULL CHECK (amount <> 0),
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_id, idempotency_key),
    CHECK (unit_kind <> 'currency' OR unit ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_ledger_postings_transaction
    ON ledger_postings (transaction_id, id);

CREATE INDEX idx_ledger_postings_account_unit
    ON ledger_postings (ledger_account, unit_kind, unit, transaction_id);

CREATE FUNCTION reject_ledger_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_ledger_transactions_immutable
    BEFORE UPDATE OR DELETE ON ledger_transactions
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TRIGGER trg_ledger_postings_immutable
    BEFORE UPDATE OR DELETE ON ledger_postings
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION assert_ledger_transaction_balanced(target_transaction_id UUID) RETURNS VOID AS $$
DECLARE
    posting_count BIGINT;
    expected_posting_count INTEGER;
    imbalanced_kind TEXT;
    imbalanced_unit TEXT;
    imbalanced_amount NUMERIC(38, 12);
BEGIN
    SELECT ledger_transactions.posting_count INTO expected_posting_count
    FROM ledger_transactions
    WHERE id = target_transaction_id;

    IF expected_posting_count IS NULL THEN
        RAISE EXCEPTION 'ledger transaction % does not exist', target_transaction_id;
    END IF;

    SELECT COUNT(*) INTO posting_count
    FROM ledger_postings
    WHERE transaction_id = target_transaction_id;

    IF posting_count <> expected_posting_count THEN
        RAISE EXCEPTION 'ledger transaction % has % postings, expected %',
            target_transaction_id, posting_count, expected_posting_count;
    END IF;

    SELECT unit_kind, unit, SUM(amount)
    INTO imbalanced_kind, imbalanced_unit, imbalanced_amount
    FROM ledger_postings
    WHERE transaction_id = target_transaction_id
    GROUP BY unit_kind, unit
    HAVING SUM(amount) <> 0
    ORDER BY unit_kind, unit
    LIMIT 1;

    IF FOUND THEN
        RAISE EXCEPTION 'ledger transaction % is unbalanced for % % by %',
            target_transaction_id, imbalanced_kind, imbalanced_unit, imbalanced_amount;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_ledger_transaction_row_balance() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_ledger_transaction_balanced(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_ledger_posting_balance() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_ledger_transaction_balanced(CASE WHEN TG_OP = 'DELETE' THEN OLD.transaction_id ELSE NEW.transaction_id END);
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_ledger_transactions_balanced
    AFTER INSERT OR UPDATE ON ledger_transactions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_ledger_transaction_row_balance();

CREATE CONSTRAINT TRIGGER trg_ledger_postings_balanced
    AFTER INSERT OR UPDATE OR DELETE ON ledger_postings
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_ledger_posting_balance();

CREATE TABLE mark_observations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    unit_kind             TEXT NOT NULL CHECK (unit_kind IN ('currency', 'instrument')),
    unit                  TEXT NOT NULL CHECK (unit <> '' AND unit = btrim(unit)),
    price                 NUMERIC(38, 12) NOT NULL CHECK (price > 0),
    price_currency        TEXT NOT NULL CHECK (price_currency ~ '^[A-Z]{3}$'),
    source                TEXT NOT NULL CHECK (source <> '' AND source = btrim(source)),
    source_observation_id TEXT CHECK (source_observation_id IS NULL OR (
                              source_observation_id <> '' AND source_observation_id = btrim(source_observation_id)
                          )),
    effective_at          TIMESTAMPTZ NOT NULL,
    observed_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata              JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (unit_kind <> 'currency' OR unit ~ '^[A-Z]{3}$')
);

CREATE UNIQUE INDEX idx_mark_observations_source_identity
    ON mark_observations (unit_kind, unit, price_currency, source, effective_at, COALESCE(source_observation_id, ''));

CREATE INDEX idx_mark_observations_unit_effective
    ON mark_observations (unit_kind, unit, effective_at DESC, id DESC);

CREATE TRIGGER trg_mark_observations_immutable
    BEFORE UPDATE OR DELETE ON mark_observations
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE projection_checkpoints (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id               UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    projection_type          TEXT NOT NULL CHECK (projection_type <> '' AND projection_type = btrim(projection_type)),
    through_transaction_id   UUID NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
    payload                  JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    checksum                 TEXT NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, projection_type, through_transaction_id)
);

CREATE INDEX idx_projection_checkpoints_latest
    ON projection_checkpoints (account_id, projection_type, created_at DESC, id DESC);

CREATE TRIGGER trg_projection_checkpoints_immutable
    BEFORE UPDATE OR DELETE ON projection_checkpoints
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Backfill every defensible capital-flow fact. Existing orders, trades, and
-- positions remain legacy compatibility projections and are not attributed.
INSERT INTO ledger_transactions (
    id,
    account_id,
    event_type,
    idempotency_key,
    origin_type,
    origin_id,
    reference_type,
    reference_id,
    effective_at,
    observed_at,
    metadata,
    posting_count,
    created_at
)
SELECT
    md5('ledger-transaction:' || flow.id::text)::UUID,
    flow.account_id,
    CASE flow.flow_type
        WHEN 'deposit' THEN 'capital_flow.deposit'
        WHEN 'withdrawal' THEN 'capital_flow.withdrawal'
    END,
    'capital-flow:' || flow.id::text,
    'capital_flow',
    flow.id::text,
    'capital_flow',
    flow.id::text,
    flow.effective_at,
    flow.observed_at,
    jsonb_build_object('normalizer', 'capital_flow_v1', 'source', flow.source),
    2,
    flow.created_at
FROM capital_flows AS flow;

INSERT INTO ledger_postings (
    id,
    transaction_id,
    idempotency_key,
    ledger_account,
    unit_kind,
    unit,
    amount,
    metadata,
    created_at
)
SELECT
    md5('ledger-posting:' || line.posting_key || ':' || flow.id::text)::UUID,
    md5('ledger-transaction:' || flow.id::text)::UUID,
    line.posting_key,
    line.ledger_account,
    'currency',
    flow.currency,
    CASE
        WHEN line.posting_key = 'cash' AND flow.flow_type = 'deposit' THEN flow.amount
        WHEN line.posting_key = 'cash' AND flow.flow_type = 'withdrawal' THEN -flow.amount
        WHEN line.posting_key = 'contributed-capital' AND flow.flow_type = 'deposit' THEN -flow.amount
        ELSE flow.amount
    END,
    '{}'::JSONB,
    flow.created_at
FROM capital_flows AS flow
CROSS JOIN (VALUES
    ('cash', 'asset:cash'),
    ('contributed-capital', 'equity:contributed_capital')
) AS line(posting_key, ledger_account);

CREATE FUNCTION post_capital_flow_to_ledger() RETURNS TRIGGER AS $$
DECLARE
    transaction_id UUID := md5('ledger-transaction:' || NEW.id::text)::UUID;
BEGIN
    INSERT INTO ledger_transactions (
        id,
        account_id,
        event_type,
        idempotency_key,
        origin_type,
        origin_id,
        reference_type,
        reference_id,
        effective_at,
        observed_at,
        metadata,
        posting_count,
        created_at
    ) VALUES (
        transaction_id,
        NEW.account_id,
        CASE NEW.flow_type
            WHEN 'deposit' THEN 'capital_flow.deposit'
            WHEN 'withdrawal' THEN 'capital_flow.withdrawal'
        END,
        'capital-flow:' || NEW.id::text,
        'capital_flow',
        NEW.id::text,
        'capital_flow',
        NEW.id::text,
        NEW.effective_at,
        NEW.observed_at,
        jsonb_build_object('normalizer', 'capital_flow_v1', 'source', NEW.source),
        2,
        NEW.created_at
    );

    INSERT INTO ledger_postings (
        id,
        transaction_id,
        idempotency_key,
        ledger_account,
        unit_kind,
        unit,
        amount,
        metadata,
        created_at
    ) VALUES
        (
            md5('ledger-posting:cash:' || NEW.id::text)::UUID,
            transaction_id,
            'cash',
            'asset:cash',
            'currency',
            NEW.currency,
            CASE NEW.flow_type WHEN 'deposit' THEN NEW.amount ELSE -NEW.amount END,
            '{}'::JSONB,
            NEW.created_at
        ),
        (
            md5('ledger-posting:contributed-capital:' || NEW.id::text)::UUID,
            transaction_id,
            'contributed-capital',
            'equity:contributed_capital',
            'currency',
            NEW.currency,
            CASE NEW.flow_type WHEN 'deposit' THEN -NEW.amount ELSE NEW.amount END,
            '{}'::JSONB,
            NEW.created_at
        );

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_capital_flows_to_ledger
    AFTER INSERT ON capital_flows
    FOR EACH ROW EXECUTE FUNCTION post_capital_flow_to_ledger();
