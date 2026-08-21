-- Stable canonical instrument identities and append-only effective-time
-- reference facts. Legacy symbols are preserved as quarantined evidence; this
-- migration does not cut any ticker-based read path over to these tables.

CREATE TABLE instruments (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_key             TEXT NOT NULL UNIQUE
                                 CHECK (identity_key <> '' AND identity_key = btrim(identity_key)),
    asset_class              TEXT NOT NULL CHECK (asset_class IN (
                                 'unknown', 'equity', 'etf', 'option', 'crypto_spot',
                                 'prediction_contract', 'future'
                             )),
    primary_venue            TEXT NOT NULL
                                 CHECK (primary_venue <> '' AND primary_venue = lower(btrim(primary_venue))),
    currency                 TEXT CHECK (currency ~ '^[A-Z]{3}$'),
    tick_size                NUMERIC(38, 12) CHECK (tick_size > 0),
    lot_size                 NUMERIC(38, 12) CHECK (lot_size > 0),
    multiplier               NUMERIC(38, 12) CHECK (multiplier > 0),
    expiration               TIMESTAMPTZ,
    exercise_style           TEXT CHECK (exercise_style IN ('american', 'european')),
    settlement_method        TEXT CHECK (settlement_method IN ('cash', 'physical', 'crypto', 'binary')),
    underlying_instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    status                   TEXT NOT NULL CHECK (status IN ('active', 'inactive', 'expired', 'quarantined')),
    metadata                 JSONB NOT NULL DEFAULT '{}'::jsonb
                                 CHECK (jsonb_typeof(metadata) = 'object'),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (underlying_instrument_id IS NULL OR underlying_instrument_id <> id),
    CHECK (status <> 'quarantined' OR metadata <> '{}'::jsonb),
    CHECK (status = 'quarantined' OR (
        asset_class <> 'unknown' AND
        currency ~ '^[A-Z]{3}$' AND
        tick_size > 0 AND
        lot_size > 0 AND
        multiplier > 0 AND
        settlement_method IS NOT NULL
    )),
    CHECK (status = 'quarantined' OR asset_class <> 'option' OR (
        expiration IS NOT NULL AND
        exercise_style IS NOT NULL AND
        underlying_instrument_id IS NOT NULL
    ))
);

CREATE INDEX idx_instruments_asset_status
    ON instruments (asset_class, status, id);

CREATE TABLE instrument_alias_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    provider      TEXT NOT NULL
                      CHECK (provider <> '' AND provider = lower(btrim(provider))),
    alias_type    TEXT NOT NULL CHECK (alias_type IN (
                      'ticker', 'occ', 'cusip', 'figi', 'venue_contract', 'slug', 'provider_id'
                  )),
    alias_value   TEXT NOT NULL CHECK (alias_value <> '' AND alias_value = btrim(alias_value)),
    action        TEXT NOT NULL CHECK (action IN ('assigned', 'retired')),
    effective_at  TIMESTAMPTZ NOT NULL,
    source        TEXT NOT NULL CHECK (source <> '' AND source = btrim(source)),
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, alias_type, alias_value, effective_at),
    CHECK (alias_type IN ('slug', 'provider_id') OR alias_value = upper(alias_value))
);

CREATE INDEX idx_instrument_alias_events_resolution
    ON instrument_alias_events (
        provider, alias_type, alias_value, effective_at DESC, created_at DESC, id DESC
    );

CREATE INDEX idx_instrument_alias_events_instrument
    ON instrument_alias_events (instrument_id, effective_at, id);

CREATE TABLE venue_contracts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id     UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    venue             TEXT NOT NULL CHECK (venue <> '' AND venue = lower(btrim(venue))),
    contract_id       TEXT NOT NULL
                          CHECK (contract_id <> '' AND contract_id = upper(btrim(contract_id))),
    currency          TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    tick_size         NUMERIC(38, 12) NOT NULL CHECK (tick_size > 0),
    lot_size          NUMERIC(38, 12) NOT NULL CHECK (lot_size > 0),
    multiplier        NUMERIC(38, 12) NOT NULL CHECK (multiplier > 0),
    settlement_method TEXT NOT NULL CHECK (settlement_method IN ('cash', 'physical', 'crypto', 'binary')),
    valid_from        TIMESTAMPTZ NOT NULL,
    valid_to          TIMESTAMPTZ,
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (venue, contract_id, valid_from),
    CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE INDEX idx_venue_contracts_resolution
    ON venue_contracts (venue, contract_id, valid_from DESC, id DESC);

CREATE TABLE corporate_actions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id           UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    successor_instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    action_type             TEXT NOT NULL CHECK (action_type IN (
                                'symbol_change', 'split', 'reverse_split', 'merger',
                                'spinoff', 'delisting', 'cash_dividend', 'futures_roll'
                            )),
    effective_at            TIMESTAMPTZ NOT NULL,
    ratio_numerator         NUMERIC(38, 12) CHECK (ratio_numerator > 0),
    ratio_denominator       NUMERIC(38, 12) CHECK (ratio_denominator > 0),
    cash_amount             NUMERIC(38, 12) CHECK (cash_amount > 0),
    cash_currency           TEXT CHECK (cash_currency ~ '^[A-Z]{3}$'),
    source                  TEXT NOT NULL CHECK (source <> '' AND source = btrim(source)),
    source_event_id         TEXT NOT NULL CHECK (source_event_id <> '' AND source_event_id = btrim(source_event_id)),
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source, source_event_id),
    CHECK (successor_instrument_id IS NULL OR successor_instrument_id <> instrument_id),
    CHECK ((ratio_numerator IS NULL) = (ratio_denominator IS NULL)),
    CHECK (action_type NOT IN ('split', 'reverse_split') OR ratio_numerator IS NOT NULL),
    CHECK (action_type NOT IN ('merger', 'spinoff', 'futures_roll') OR successor_instrument_id IS NOT NULL),
    CHECK ((cash_amount IS NULL) = (cash_currency IS NULL)),
    CHECK (action_type <> 'cash_dividend' OR cash_amount IS NOT NULL)
);

CREATE INDEX idx_corporate_actions_instrument_effective
    ON corporate_actions (instrument_id, effective_at, id);

CREATE TABLE instrument_identity_quarantine (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id     UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    finding_code      TEXT NOT NULL CHECK (finding_code <> '' AND finding_code = btrim(finding_code)),
    source            TEXT NOT NULL CHECK (source <> '' AND source = btrim(source)),
    source_record_key TEXT NOT NULL
                          CHECK (source_record_key <> '' AND source_record_key = btrim(source_record_key)),
    details           JSONB NOT NULL CHECK (jsonb_typeof(details) = 'object'),
    observed_at       TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (finding_code, source, source_record_key)
);

CREATE INDEX idx_instrument_identity_quarantine_instrument
    ON instrument_identity_quarantine (instrument_id, observed_at, id);

CREATE FUNCTION reject_instrument_reference_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_instrument_alias_event_transition() RETURNS TRIGGER AS $$
DECLARE
    latest_action TEXT;
    latest_instrument_id UUID;
    latest_effective_at TIMESTAMPTZ;
    has_latest BOOLEAN;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(
        NEW.provider || E'\x1f' || NEW.alias_type || E'\x1f' || NEW.alias_value,
        0
    ));

    -- Let the unique key and repository payload comparison handle exact-time
    -- retries. This does not authorize an effective-time rebind.
    IF EXISTS (
        SELECT 1
        FROM instrument_alias_events
        WHERE provider = NEW.provider
          AND alias_type = NEW.alias_type
          AND alias_value = NEW.alias_value
          AND effective_at = NEW.effective_at
    ) THEN
        RETURN NEW;
    END IF;

    SELECT action, instrument_id, effective_at
    INTO latest_action, latest_instrument_id, latest_effective_at
    FROM instrument_alias_events
    WHERE provider = NEW.provider
      AND alias_type = NEW.alias_type
      AND alias_value = NEW.alias_value
    ORDER BY effective_at DESC, created_at DESC, id DESC
    LIMIT 1;
    has_latest := FOUND;

    IF has_latest AND NEW.effective_at <= latest_effective_at THEN
        RAISE EXCEPTION 'alias events must be appended in strictly increasing effective-time order';
    END IF;

    IF NEW.action = 'assigned' THEN
        IF has_latest AND latest_action = 'assigned' AND latest_instrument_id <> NEW.instrument_id THEN
            RAISE EXCEPTION 'alias is already assigned; retire it before rebinding';
        END IF;
    ELSIF NOT has_latest OR latest_action <> 'assigned' OR latest_instrument_id <> NEW.instrument_id THEN
        RAISE EXCEPTION 'alias retirement must follow an assignment to the same instrument';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_venue_contract_window() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.venue || E'\x1f' || NEW.contract_id, 0));
    IF EXISTS (
        SELECT 1
        FROM venue_contracts
        WHERE venue = NEW.venue
          AND contract_id = NEW.contract_id
          AND valid_from = NEW.valid_from
    ) THEN
        RETURN NEW;
    END IF;
    IF EXISTS (
        SELECT 1
        FROM venue_contracts
        WHERE venue = NEW.venue
          AND contract_id = NEW.contract_id
          AND tstzrange(valid_from, valid_to, '[)') && tstzrange(NEW.valid_from, NEW.valid_to, '[)')
    ) THEN
        RAISE EXCEPTION 'venue contract validity window overlaps an existing contract';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_instruments_immutable
    BEFORE UPDATE OR DELETE ON instruments
    FOR EACH ROW EXECUTE FUNCTION reject_instrument_reference_mutation();

CREATE TRIGGER trg_instrument_alias_events_transition
    BEFORE INSERT ON instrument_alias_events
    FOR EACH ROW EXECUTE FUNCTION validate_instrument_alias_event_transition();

CREATE TRIGGER trg_instrument_alias_events_immutable
    BEFORE UPDATE OR DELETE ON instrument_alias_events
    FOR EACH ROW EXECUTE FUNCTION reject_instrument_reference_mutation();

CREATE TRIGGER trg_venue_contracts_window
    BEFORE INSERT ON venue_contracts
    FOR EACH ROW EXECUTE FUNCTION validate_venue_contract_window();

CREATE TRIGGER trg_venue_contracts_immutable
    BEFORE UPDATE OR DELETE ON venue_contracts
    FOR EACH ROW EXECUTE FUNCTION reject_instrument_reference_mutation();

CREATE TRIGGER trg_corporate_actions_immutable
    BEFORE UPDATE OR DELETE ON corporate_actions
    FOR EACH ROW EXECUTE FUNCTION reject_instrument_reference_mutation();

CREATE TRIGGER trg_instrument_identity_quarantine_immutable
    BEFORE UPDATE OR DELETE ON instrument_identity_quarantine
    FOR EACH ROW EXECUTE FUNCTION reject_instrument_reference_mutation();

-- Build one deterministic evidence set over every legacy table that already
-- carries a typed symbol. Unknown contract mechanics remain NULL.
WITH legacy_symbols(raw_symbol, market_type, observed_at, source_table) AS (
    SELECT ticker, market_type::TEXT, created_at, 'strategies' FROM strategies
    UNION ALL
    SELECT ticker, market_type::TEXT, created_at, 'orders' FROM orders
    UNION ALL
    SELECT instrument_key, market_type::TEXT, created_at, 'trade_decisions' FROM trade_decisions
    UNION ALL
    SELECT ticker, 'stock', created_at, 'universe_tickers' FROM universe_tickers
    UNION ALL
    SELECT occ_symbol, 'option_contract', fetched_at, 'option_contracts' FROM option_contracts
    UNION ALL
    SELECT ticker, 'kalshi', added_at, 'kalshi_watched_markets' FROM kalshi_watched_markets
    UNION ALL
    SELECT slug, 'polymarket', added_at, 'polymarket_watched_markets' FROM polymarket_watched_markets
    UNION ALL
    SELECT ticker, 'stock', valid_from, 'copy_instrument_mappings' FROM copy_instrument_mappings
), normalized AS (
    SELECT
        market_type,
        CASE
            WHEN market_type = 'polymarket' THEN btrim(raw_symbol)
            ELSE upper(btrim(raw_symbol))
        END AS symbol,
        MIN(observed_at) AS first_seen_at,
        jsonb_agg(DISTINCT source_table ORDER BY source_table) AS sources
    FROM legacy_symbols
    WHERE raw_symbol IS NOT NULL AND btrim(raw_symbol) <> ''
    GROUP BY
        market_type,
        CASE
            WHEN market_type = 'polymarket' THEN btrim(raw_symbol)
            ELSE upper(btrim(raw_symbol))
        END
)
INSERT INTO instruments (
    id, identity_key, asset_class, primary_venue, status, metadata, created_at
)
SELECT
    md5('legacy-instrument:' || market_type || ':' || symbol)::UUID,
    'legacy:' || market_type || ':' || symbol,
    CASE market_type
        WHEN 'stock' THEN 'equity'
        WHEN 'crypto' THEN 'crypto_spot'
        WHEN 'options' THEN 'option'
        WHEN 'option_contract' THEN 'option'
        WHEN 'kalshi' THEN 'prediction_contract'
        WHEN 'polymarket' THEN 'prediction_contract'
        ELSE 'unknown'
    END,
    CASE market_type
        WHEN 'kalshi' THEN 'kalshi'
        WHEN 'polymarket' THEN 'polymarket'
        ELSE 'legacy_unknown'
    END,
    'quarantined',
    jsonb_build_object(
        'backfill', 'migration_000066',
        'legacy_market_type', market_type,
        'legacy_symbol', symbol,
        'sources', sources
    ),
    first_seen_at
FROM normalized
ON CONFLICT (identity_key) DO NOTHING;

INSERT INTO instrument_alias_events (
    id, instrument_id, provider, alias_type, alias_value, action,
    effective_at, source, metadata, created_at
)
SELECT
    md5('legacy-alias:' || identity_key)::UUID,
    id,
    CASE metadata->>'legacy_market_type'
        WHEN 'stock' THEN 'legacy_augr_stock'
        ELSE 'legacy_augr_' || (metadata->>'legacy_market_type')
    END,
    CASE metadata->>'legacy_market_type'
        WHEN 'polymarket' THEN 'slug'
        WHEN 'option_contract' THEN 'occ'
        ELSE 'ticker'
    END,
    metadata->>'legacy_symbol',
    'assigned',
    created_at,
    'migration_000066',
    jsonb_build_object('backfill', 'migration_000066'),
    created_at
FROM instruments
WHERE metadata->>'backfill' = 'migration_000066'
ORDER BY identity_key;

INSERT INTO instrument_identity_quarantine (
    id, instrument_id, finding_code, source, source_record_key,
    details, observed_at, created_at
)
SELECT
    md5('legacy-quarantine:' || identity_key)::UUID,
    id,
    'migration_000066_incomplete_reference_terms',
    'migration_000066',
    identity_key,
    jsonb_build_object(
        'reason', 'legacy symbol has no verified currency, tick size, lot size, multiplier, or settlement method',
        'sources', metadata->'sources'
    ),
    created_at,
    created_at
FROM instruments
WHERE metadata->>'backfill' = 'migration_000066'
ORDER BY identity_key;

-- Only current mappings that were explicitly verified are promoted to alias
-- evidence. Ended, ambiguous, and stale mappings remain quarantine findings.
INSERT INTO instrument_alias_events (
    id, instrument_id, provider, alias_type, alias_value, action,
    effective_at, source, metadata, created_at
)
SELECT
    md5('copy-mapping-alias:' || mapping.id::TEXT)::UUID,
    canonical.id,
    lower(btrim(mapping.provider)),
    CASE lower(btrim(mapping.identifier_type))
        WHEN 'cusip' THEN 'cusip'
        WHEN 'figi' THEN 'figi'
        ELSE 'provider_id'
    END,
    CASE lower(btrim(mapping.identifier_type))
        WHEN 'cusip' THEN upper(btrim(mapping.identifier_value))
        WHEN 'figi' THEN upper(btrim(mapping.identifier_value))
        ELSE btrim(mapping.identifier_value)
    END,
    'assigned',
    mapping.valid_from,
    'copy_instrument_mappings',
    jsonb_build_object(
        'mapping_id', mapping.id,
        'confidence', mapping.confidence,
        'mapping_method', mapping.mapping_method
    ),
    mapping.created_at
FROM copy_instrument_mappings AS mapping
JOIN instruments AS canonical
  ON canonical.identity_key = 'legacy:stock:' || upper(btrim(mapping.ticker))
WHERE mapping.confidence IN ('manual_verified', 'provider_verified')
  AND mapping.valid_to IS NULL
  AND btrim(mapping.provider) <> ''
  AND btrim(mapping.identifier_value) <> ''
ORDER BY lower(btrim(mapping.provider)), mapping.identifier_type, mapping.identifier_value, mapping.valid_from;

INSERT INTO instrument_identity_quarantine (
    id, instrument_id, finding_code, source, source_record_key,
    details, observed_at, created_at
)
SELECT
    md5('copy-mapping-quarantine:' || mapping.id::TEXT)::UUID,
    canonical.id,
    CASE
        WHEN mapping.confidence IN ('ambiguous', 'stale')
            THEN 'migration_000066_copy_mapping_' || mapping.confidence
        ELSE 'migration_000066_expired_verified_mapping'
    END,
    'copy_instrument_mappings',
    mapping.id::TEXT,
    jsonb_build_object(
        'provider', mapping.provider,
        'identifier_type', mapping.identifier_type,
        'identifier_value', mapping.identifier_value,
        'confidence', mapping.confidence,
        'valid_from', mapping.valid_from,
        'valid_to', mapping.valid_to
    ),
    mapping.created_at,
    mapping.created_at
FROM copy_instrument_mappings AS mapping
JOIN instruments AS canonical
  ON canonical.identity_key = 'legacy:stock:' || upper(btrim(mapping.ticker))
WHERE mapping.confidence IN ('ambiguous', 'stale')
   OR (mapping.confidence IN ('manual_verified', 'provider_verified') AND mapping.valid_to IS NOT NULL)
ORDER BY mapping.id;
