-- Raw-first economic evidence and exact typed ledger normalization. This
-- migration is additive and intentionally performs no legacy backfill.

CREATE FUNCTION economic_deterministic_uuid(domain_name TEXT, VARIADIC components TEXT[])
RETURNS UUID AS $$
DECLARE
    encoded TEXT := domain_name;
    component TEXT;
    hash_hex TEXT;
BEGIN
    IF domain_name IS NULL OR domain_name = '' THEN
        RAISE EXCEPTION 'deterministic UUID domain is required';
    END IF;
    FOREACH component IN ARRAY components LOOP
        IF component IS NULL THEN
            RAISE EXCEPTION 'deterministic UUID components cannot be null';
        END IF;
        encoded := encoded || chr(31) ||
            octet_length(convert_to(component, 'UTF8'))::TEXT || ':' || component;
    END LOOP;
    hash_hex := encode(digest(convert_to(encoded, 'UTF8'), 'sha256'), 'hex');
    RETURN (
        substr(hash_hex, 1, 8) || '-' ||
        substr(hash_hex, 9, 4) || '-' ||
        substr(hash_hex, 13, 4) || '-' ||
        substr(hash_hex, 17, 4) || '-' ||
        substr(hash_hex, 21, 12)
    )::UUID;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE FUNCTION reject_economic_event_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE economic_source_events (
    id                 UUID PRIMARY KEY,
    account_id         UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    source             TEXT NOT NULL CHECK (
                           source <> '' AND source = lower(btrim(source))
                       ),
    source_namespace   TEXT NOT NULL CHECK (
                           source_namespace <> '' AND source_namespace = btrim(source_namespace)
                       ),
    source_event_id    TEXT NOT NULL CHECK (
                           source_event_id <> '' AND source_event_id = btrim(source_event_id)
                       ),
    source_revision    TEXT NOT NULL DEFAULT '' CHECK (source_revision = btrim(source_revision)),
    observed_at        TIMESTAMPTZ NOT NULL,
    raw_payload        BYTEA NOT NULL,
    payload_sha256     TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    payload            JSONB NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, source, source_namespace, source_event_id),
    CHECK (octet_length(raw_payload) > 0),
    CHECK (payload_sha256 = encode(digest(raw_payload, 'sha256'), 'hex')),
    CHECK (payload = convert_from(raw_payload, 'UTF8')::JSONB),
    CHECK (id = economic_deterministic_uuid(
        'economic-source-event',
        account_id::TEXT,
        source,
        source_namespace,
        source_event_id
    ))
);

CREATE INDEX idx_economic_source_events_account_observed
    ON economic_source_events (account_id, observed_at, id);

CREATE TABLE option_contract_terms (
    id                       UUID PRIMARY KEY,
    option_instrument_id     UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    underlying_instrument_id UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    contract_type            TEXT NOT NULL CHECK (contract_type IN ('call', 'put')),
    strike_price             NUMERIC NOT NULL CHECK (
                                 strike_price > 0 AND
                                 strike_price < 100000000000000000000000000 AND
                                 strike_price = round(strike_price, 12)
                             ),
    strike_currency          TEXT NOT NULL CHECK (strike_currency ~ '^[A-Z]{3}$'),
    deliverable_quantity     NUMERIC NOT NULL CHECK (
                                 deliverable_quantity > 0 AND
                                 deliverable_quantity < 100000000000000000000000000 AND
                                 deliverable_quantity = round(deliverable_quantity, 12)
                             ),
    source                   TEXT NOT NULL CHECK (
                                 source <> '' AND source = lower(btrim(source))
                             ),
    source_namespace         TEXT NOT NULL CHECK (
                                 source_namespace <> '' AND source_namespace = btrim(source_namespace)
                             ),
    source_record_id         TEXT NOT NULL CHECK (
                                 source_record_id <> '' AND source_record_id = btrim(source_record_id)
                             ),
    source_revision          TEXT NOT NULL DEFAULT '' CHECK (source_revision = btrim(source_revision)),
    effective_at             TIMESTAMPTZ NOT NULL,
    observed_at              TIMESTAMPTZ NOT NULL,
    raw_payload              BYTEA NOT NULL,
    payload_sha256           TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    payload                  JSONB NOT NULL,
    metadata                 JSONB NOT NULL DEFAULT '{}'::JSONB CHECK (jsonb_typeof(metadata) = 'object'),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (option_instrument_id, effective_at),
    UNIQUE (option_instrument_id, source, source_namespace, source_record_id),
    CHECK (option_instrument_id <> underlying_instrument_id),
    CHECK (observed_at >= effective_at),
    CHECK (octet_length(raw_payload) > 0),
    CHECK (payload_sha256 = encode(digest(raw_payload, 'sha256'), 'hex')),
    CHECK (payload = convert_from(raw_payload, 'UTF8')::JSONB),
    CHECK (id = economic_deterministic_uuid(
        'option-contract-terms',
        option_instrument_id::TEXT,
        source,
        source_namespace,
        source_record_id
    ))
);

CREATE INDEX idx_option_contract_terms_effective
    ON option_contract_terms (option_instrument_id, effective_at DESC, id DESC);

CREATE TABLE economic_event_normalizations (
    id                      UUID PRIMARY KEY,
    source_event_id         UUID NOT NULL UNIQUE
                                REFERENCES economic_source_events(id) ON DELETE RESTRICT,
    event_type              TEXT NOT NULL CHECK (event_type IN (
                                'fill.buy', 'fill.sell', 'cost.fee', 'cost.rebate',
                                'settlement.option_cash', 'settlement.option_expiration',
                                'settlement.option_exercise', 'settlement.option_assignment',
                                'settlement.prediction_payout'
                            )),
    normalizer_version      TEXT NOT NULL CHECK (
                                normalizer_version <> '' AND normalizer_version = btrim(normalizer_version)
                            ),
    execution_origin_type   TEXT NOT NULL CHECK (execution_origin_type IN (
                                'strategy_version', 'copy_subscription', 'portfolio_rebalance',
                                'risk_reduction', 'operator', 'settlement', 'reconciliation'
                            )),
    execution_origin_id     TEXT NOT NULL CHECK (
                                execution_origin_id <> '' AND execution_origin_id = btrim(execution_origin_id)
                            ),
    reference_type          TEXT NOT NULL CHECK (
                                reference_type <> '' AND reference_type = btrim(reference_type)
                            ),
    reference_id            TEXT NOT NULL CHECK (
                                reference_id <> '' AND reference_id = btrim(reference_id)
                            ),
    venue                   TEXT CHECK (venue IS NULL OR (
                                venue <> '' AND venue = lower(btrim(venue))
                            )),
    instrument_id           UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    secondary_instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    venue_contract_id       UUID REFERENCES venue_contracts(id) ON DELETE RESTRICT,
    option_terms_id         UUID REFERENCES option_contract_terms(id) ON DELETE RESTRICT,
    effective_at            TIMESTAMPTZ NOT NULL,
    cash_currency           TEXT NOT NULL CHECK (cash_currency ~ '^[A-Z]{3}$'),
    quantity                NUMERIC,
    price                   NUMERIC,
    cost_kind               TEXT CHECK (cost_kind IN ('fee', 'rebate')),
    cost_currency           TEXT CHECK (cost_currency ~ '^[A-Z]{3}$'),
    cost_amount             NUMERIC,
    position_quantity       NUMERIC,
    settlement_price        NUMERIC,
    ledger_transaction_id   UUID NOT NULL UNIQUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (ledger_transaction_id) REFERENCES ledger_transactions(id)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CHECK (secondary_instrument_id IS NULL OR secondary_instrument_id <> instrument_id),
    CHECK (id = economic_deterministic_uuid(
        'economic-normalization', source_event_id::TEXT, normalizer_version
    )),
    CHECK (ledger_transaction_id = economic_deterministic_uuid(
        'economic-ledger-transaction', source_event_id::TEXT, normalizer_version
    )),
    CHECK (quantity IS NULL OR (
        quantity > 0 AND quantity < 100000000000000000000000000 AND quantity = round(quantity, 12)
    )),
    CHECK (price IS NULL OR (
        price >= 0 AND price < 100000000000000000000000000 AND price = round(price, 12)
    )),
    CHECK (cost_amount IS NULL OR (
        cost_amount > 0 AND cost_amount < 100000000000000000000000000 AND cost_amount = round(cost_amount, 12)
    )),
    CHECK (position_quantity IS NULL OR (
        position_quantity <> 0 AND abs(position_quantity) < 100000000000000000000000000 AND
        position_quantity = round(position_quantity, 12)
    )),
    CHECK (settlement_price IS NULL OR (
        settlement_price >= 0 AND settlement_price < 100000000000000000000000000 AND
        settlement_price = round(settlement_price, 12)
    )),
    CHECK (
        (
            event_type IN ('fill.buy', 'fill.sell') AND
            venue IS NOT NULL AND instrument_id IS NOT NULL AND venue_contract_id IS NOT NULL AND
            secondary_instrument_id IS NULL AND option_terms_id IS NULL AND
            quantity IS NOT NULL AND price IS NOT NULL AND
            position_quantity IS NULL AND settlement_price IS NULL AND
            ((cost_kind IS NULL AND cost_currency IS NULL AND cost_amount IS NULL) OR
             (cost_kind IS NOT NULL AND cost_currency IS NOT NULL AND cost_amount IS NOT NULL))
        ) OR (
            event_type IN ('cost.fee', 'cost.rebate') AND
            venue IS NULL AND instrument_id IS NULL AND secondary_instrument_id IS NULL AND
            venue_contract_id IS NULL AND option_terms_id IS NULL AND
            quantity IS NULL AND price IS NULL AND position_quantity IS NULL AND settlement_price IS NULL AND
            cost_kind IS NOT NULL AND cost_currency IS NOT NULL AND cost_amount IS NOT NULL AND
            ((event_type = 'cost.fee' AND cost_kind = 'fee') OR
             (event_type = 'cost.rebate' AND cost_kind = 'rebate'))
        ) OR (
            event_type IN (
                'settlement.option_cash', 'settlement.option_expiration',
                'settlement.prediction_payout'
            ) AND
            venue IS NOT NULL AND instrument_id IS NOT NULL AND venue_contract_id IS NOT NULL AND
            secondary_instrument_id IS NULL AND option_terms_id IS NULL AND
            quantity IS NULL AND price IS NULL AND
            cost_kind IS NULL AND cost_currency IS NULL AND cost_amount IS NULL AND
            position_quantity IS NOT NULL AND settlement_price IS NOT NULL
        ) OR (
            event_type IN ('settlement.option_exercise', 'settlement.option_assignment') AND
            venue IS NOT NULL AND instrument_id IS NOT NULL AND secondary_instrument_id IS NOT NULL AND
            venue_contract_id IS NOT NULL AND option_terms_id IS NOT NULL AND
            quantity IS NULL AND price IS NULL AND
            cost_kind IS NULL AND cost_currency IS NULL AND cost_amount IS NULL AND
            position_quantity IS NOT NULL AND settlement_price IS NULL
        )
    )
);

CREATE INDEX idx_economic_normalizations_instrument_effective
    ON economic_event_normalizations (instrument_id, effective_at, id)
    WHERE instrument_id IS NOT NULL;

CREATE FUNCTION assert_economic_posting(
    target_transaction_id UUID,
    target_source_event_id UUID,
    target_normalizer_version TEXT,
    expected_key TEXT,
    expected_account TEXT,
    expected_unit_kind TEXT,
    expected_unit TEXT,
    expected_amount NUMERIC
) RETURNS VOID AS $$
DECLARE
    actual_id UUID;
    actual_account TEXT;
    actual_unit_kind TEXT;
    actual_unit TEXT;
    actual_amount NUMERIC;
    actual_metadata JSONB;
    expected_id UUID;
BEGIN
    SELECT id, ledger_account, unit_kind, unit, amount, metadata
    INTO actual_id, actual_account, actual_unit_kind, actual_unit, actual_amount, actual_metadata
    FROM ledger_postings
    WHERE transaction_id = target_transaction_id AND idempotency_key = expected_key;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'economic ledger transaction % is missing posting %',
            target_transaction_id, expected_key;
    END IF;

    expected_id := economic_deterministic_uuid(
        'economic-ledger-posting',
        target_source_event_id::TEXT,
        target_normalizer_version,
        expected_key
    );
    IF actual_id <> expected_id OR
       actual_account <> expected_account OR
       actual_unit_kind <> expected_unit_kind OR
       actual_unit <> expected_unit OR
       actual_amount <> expected_amount OR
       actual_metadata <> '{}'::JSONB THEN
        RAISE EXCEPTION 'economic ledger posting % does not match normalized facts', expected_key;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION assert_economic_normalization(target_normalization_id UUID) RETURNS VOID AS $$
DECLARE
    normalized economic_event_normalizations%ROWTYPE;
    source_event economic_source_events%ROWTYPE;
    account_currency TEXT;
    transaction_row ledger_transactions%ROWTYPE;
    primary_instrument instruments%ROWTYPE;
    secondary_instrument instruments%ROWTYPE;
    contract venue_contracts%ROWTYPE;
    terms option_contract_terms%ROWTYPE;
    latest_terms_id UUID;
    expected_transaction_id UUID;
    expected_metadata JSONB;
    expected_posting_count INTEGER := 0;
    actual_posting_count INTEGER;
    inventory_account TEXT;
    direction NUMERIC;
    inventory_amount NUMERIC;
    gross_cash NUMERIC;
    payout NUMERIC;
    option_close NUMERIC;
    delivered NUMERIC;
    strike_cash NUMERIC;
BEGIN
    SELECT * INTO normalized
    FROM economic_event_normalizations
    WHERE id = target_normalization_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'economic normalization % does not exist', target_normalization_id;
    END IF;

    SELECT * INTO source_event FROM economic_source_events WHERE id = normalized.source_event_id;
    SELECT base_currency INTO account_currency FROM accounts WHERE id = source_event.account_id;
    IF account_currency IS NULL OR normalized.cash_currency <> account_currency THEN
        RAISE EXCEPTION 'economic normalization cash currency does not match account base currency';
    END IF;
    IF normalized.effective_at > source_event.observed_at THEN
        RAISE EXCEPTION 'economic normalization effective time follows source observation';
    END IF;
    IF normalized.id <> economic_deterministic_uuid(
        'economic-normalization', source_event.id::TEXT, normalized.normalizer_version
    ) THEN
        RAISE EXCEPTION 'economic normalization ID is not deterministic';
    END IF;

    expected_transaction_id := economic_deterministic_uuid(
        'economic-ledger-transaction', source_event.id::TEXT, normalized.normalizer_version
    );
    IF normalized.ledger_transaction_id <> expected_transaction_id THEN
        RAISE EXCEPTION 'economic normalization ledger transaction ID is not deterministic';
    END IF;
    SELECT * INTO transaction_row FROM ledger_transactions WHERE id = expected_transaction_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'economic normalization % has no ledger transaction', normalized.id;
    END IF;
    expected_metadata := jsonb_build_object(
        'economic_normalization_id', normalized.id::TEXT,
        'execution_origin_id', normalized.execution_origin_id,
        'execution_origin_type', normalized.execution_origin_type,
        'normalizer_version', normalized.normalizer_version,
        'raw_payload_sha256', source_event.payload_sha256,
        'source_event_id', source_event.id::TEXT
    );
    IF transaction_row.account_id <> source_event.account_id OR
       transaction_row.event_type <> normalized.event_type OR
       transaction_row.idempotency_key <> 'economic-source-event:' || source_event.id::TEXT OR
       transaction_row.origin_type <> 'economic_source_event' OR
       transaction_row.origin_id <> source_event.id::TEXT OR
       transaction_row.reference_type IS DISTINCT FROM normalized.reference_type OR
       transaction_row.reference_id IS DISTINCT FROM normalized.reference_id OR
       transaction_row.effective_at <> normalized.effective_at OR
       transaction_row.observed_at <> source_event.observed_at OR
       transaction_row.metadata <> expected_metadata THEN
        RAISE EXCEPTION 'economic normalization ledger header does not match source and normalized facts';
    END IF;

    IF normalized.instrument_id IS NOT NULL THEN
        SELECT * INTO primary_instrument FROM instruments WHERE id = normalized.instrument_id;
        SELECT * INTO contract FROM venue_contracts WHERE id = normalized.venue_contract_id;
        IF primary_instrument.id IS NULL OR contract.id IS NULL OR
           contract.instrument_id <> primary_instrument.id OR contract.venue <> normalized.venue THEN
            RAISE EXCEPTION 'economic normalization canonical instrument/venue contract mismatch';
        END IF;
        IF primary_instrument.currency <> normalized.cash_currency OR contract.currency <> normalized.cash_currency THEN
            RAISE EXCEPTION 'economic normalization instrument/contract currency mismatch';
        END IF;
        inventory_account := CASE primary_instrument.asset_class
            WHEN 'equity' THEN 'asset:security_inventory'
            WHEN 'etf' THEN 'asset:security_inventory'
            WHEN 'future' THEN 'asset:security_inventory'
            WHEN 'crypto_spot' THEN 'asset:crypto_inventory'
            WHEN 'option' THEN 'asset:option_inventory'
            WHEN 'prediction_contract' THEN 'asset:event_contract_inventory'
            ELSE NULL
        END;
        IF inventory_account IS NULL THEN
            RAISE EXCEPTION 'economic normalization instrument asset class has no inventory account';
        END IF;
    END IF;

    IF normalized.event_type IN ('fill.buy', 'fill.sell') THEN
        IF primary_instrument.status <> 'active' THEN
            RAISE EXCEPTION 'fill instrument must be active';
        END IF;
        IF normalized.effective_at < contract.valid_from OR
           (contract.valid_to IS NOT NULL AND normalized.effective_at >= contract.valid_to) THEN
            RAISE EXCEPTION 'fill venue contract is not effective at fill time';
        END IF;
        IF mod(normalized.quantity, contract.lot_size) <> 0 OR
           mod(normalized.price, contract.tick_size) <> 0 THEN
            RAISE EXCEPTION 'fill quantity/price violates exact venue lot/tick mechanics';
        END IF;
        IF primary_instrument.asset_class = 'prediction_contract' AND
           (normalized.price < 0 OR normalized.price > 1) THEN
            RAISE EXCEPTION 'prediction fill price must be between zero and one';
        END IF;
        direction := CASE normalized.event_type WHEN 'fill.buy' THEN 1 ELSE -1 END;
        inventory_amount := normalized.quantity * direction;
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'inventory', inventory_account, 'instrument', primary_instrument.id::TEXT, inventory_amount
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'clearing-inventory', 'clearing:execution', 'instrument', primary_instrument.id::TEXT, -inventory_amount
        );
        expected_posting_count := expected_posting_count + 2;

        gross_cash := normalized.price * normalized.quantity * contract.multiplier;
        IF gross_cash < 0 OR gross_cash >= 100000000000000000000000000 OR
           gross_cash <> round(gross_cash, 12) THEN
            RAISE EXCEPTION 'fill gross cash exceeds exact ledger numeric contract';
        END IF;
        IF gross_cash <> 0 THEN
            gross_cash := -gross_cash * direction;
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'gross-cash', 'asset:cash', 'currency', normalized.cash_currency, gross_cash
            );
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'clearing-gross-cash', 'clearing:execution', 'currency', normalized.cash_currency, -gross_cash
            );
            expected_posting_count := expected_posting_count + 2;
        END IF;
    ELSIF normalized.event_type IN ('cost.fee', 'cost.rebate') THEN
        IF normalized.cost_currency <> normalized.cash_currency THEN
            RAISE EXCEPTION 'standalone cost currency must match account base currency';
        END IF;
    ELSIF normalized.event_type IN (
        'settlement.option_cash', 'settlement.option_expiration', 'settlement.prediction_payout'
    ) THEN
        IF primary_instrument.status = 'quarantined' OR contract.valid_from > normalized.effective_at OR
           primary_instrument.multiplier <> contract.multiplier THEN
            RAISE EXCEPTION 'settlement canonical mechanics are incomplete or not yet effective';
        END IF;
        IF mod(abs(normalized.position_quantity), contract.lot_size) <> 0 OR
           mod(normalized.settlement_price, contract.tick_size) <> 0 THEN
            RAISE EXCEPTION 'settlement quantity/price violates exact venue lot/tick mechanics';
        END IF;
        IF normalized.event_type = 'settlement.option_cash' AND
           (primary_instrument.asset_class <> 'option' OR
            primary_instrument.settlement_method <> 'cash' OR contract.settlement_method <> 'cash') THEN
            RAISE EXCEPTION 'cash option settlement requires cash option mechanics';
        ELSIF normalized.event_type = 'settlement.option_expiration' AND
              (primary_instrument.asset_class <> 'option' OR normalized.settlement_price <> 0 OR
               primary_instrument.settlement_method <> contract.settlement_method) THEN
            RAISE EXCEPTION 'option expiration requires matching option mechanics and zero price';
        ELSIF normalized.event_type = 'settlement.prediction_payout' AND
              (primary_instrument.asset_class <> 'prediction_contract' OR
               primary_instrument.settlement_method <> 'binary' OR contract.settlement_method <> 'binary' OR
               normalized.settlement_price NOT IN (0, 1)) THEN
            RAISE EXCEPTION 'prediction payout requires binary mechanics and zero-or-one resolution';
        END IF;

        inventory_amount := -normalized.position_quantity;
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'inventory-settlement', inventory_account, 'instrument', primary_instrument.id::TEXT, inventory_amount
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'clearing-inventory-settlement', 'clearing:settlement', 'instrument', primary_instrument.id::TEXT, -inventory_amount
        );
        expected_posting_count := expected_posting_count + 2;

        payout := normalized.position_quantity * normalized.settlement_price * contract.multiplier;
        IF abs(payout) >= 100000000000000000000000000 OR payout <> round(payout, 12) THEN
            RAISE EXCEPTION 'settlement payout exceeds exact ledger numeric contract';
        END IF;
        IF payout <> 0 THEN
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'settlement-cash', 'asset:cash', 'currency', normalized.cash_currency, payout
            );
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'clearing-settlement-cash', 'clearing:settlement', 'currency', normalized.cash_currency, -payout
            );
            expected_posting_count := expected_posting_count + 2;
        END IF;
    ELSE
        PERFORM pg_advisory_xact_lock(hashtextextended(
            'economic-option-terms:' || primary_instrument.id::TEXT, 0
        ));
        SELECT * INTO secondary_instrument FROM instruments WHERE id = normalized.secondary_instrument_id;
        SELECT * INTO terms FROM option_contract_terms WHERE id = normalized.option_terms_id;
        IF secondary_instrument.id IS NULL OR terms.id IS NULL OR
           primary_instrument.asset_class <> 'option' OR
           primary_instrument.status = 'quarantined' OR secondary_instrument.status = 'quarantined' OR
           primary_instrument.underlying_instrument_id <> secondary_instrument.id OR
           terms.option_instrument_id <> primary_instrument.id OR
           terms.underlying_instrument_id <> secondary_instrument.id OR
           primary_instrument.settlement_method <> 'physical' OR contract.settlement_method <> 'physical' OR
           contract.valid_from > normalized.effective_at THEN
            RAISE EXCEPTION 'physical option canonical references or mechanics do not match';
        END IF;
        IF contract.multiplier <> terms.deliverable_quantity OR
           terms.strike_currency <> normalized.cash_currency THEN
            RAISE EXCEPTION 'physical option multiplier, deliverable, or strike currency mismatch';
        END IF;
        IF terms.effective_at > normalized.effective_at OR terms.observed_at > source_event.observed_at THEN
            RAISE EXCEPTION 'physical option terms are future or not yet observed';
        END IF;
        SELECT id INTO latest_terms_id
        FROM option_contract_terms
        WHERE option_instrument_id = primary_instrument.id
          AND effective_at <= normalized.effective_at
          AND observed_at <= source_event.observed_at
        ORDER BY effective_at DESC, id DESC
        LIMIT 1;
        IF latest_terms_id IS DISTINCT FROM terms.id THEN
            RAISE EXCEPTION 'physical option normalization did not select latest eligible terms';
        END IF;
        IF mod(abs(normalized.position_quantity), contract.lot_size) <> 0 OR
           (normalized.event_type = 'settlement.option_exercise' AND normalized.position_quantity <= 0) OR
           (normalized.event_type = 'settlement.option_assignment' AND normalized.position_quantity >= 0) THEN
            RAISE EXCEPTION 'physical option position/action violates exact mechanics';
        END IF;
        option_close := -normalized.position_quantity;
        delivered := normalized.position_quantity * terms.deliverable_quantity *
            CASE terms.contract_type WHEN 'call' THEN 1 ELSE -1 END;
        IF abs(delivered) >= 100000000000000000000000000 OR delivered <> round(delivered, 12) OR
           mod(abs(delivered), secondary_instrument.lot_size) <> 0 THEN
            RAISE EXCEPTION 'physical option delivery violates exact underlying mechanics';
        END IF;
        strike_cash := -delivered * terms.strike_price;
        IF abs(strike_cash) >= 100000000000000000000000000 OR strike_cash <> round(strike_cash, 12) THEN
            RAISE EXCEPTION 'physical option strike cash exceeds exact ledger numeric contract';
        END IF;
        inventory_account := CASE secondary_instrument.asset_class
            WHEN 'equity' THEN 'asset:security_inventory'
            WHEN 'etf' THEN 'asset:security_inventory'
            WHEN 'future' THEN 'asset:security_inventory'
            WHEN 'crypto_spot' THEN 'asset:crypto_inventory'
            WHEN 'option' THEN 'asset:option_inventory'
            WHEN 'prediction_contract' THEN 'asset:event_contract_inventory'
            ELSE NULL
        END;
        IF inventory_account IS NULL THEN
            RAISE EXCEPTION 'physical option underlying has no inventory account';
        END IF;
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'option-close', 'asset:option_inventory', 'instrument', primary_instrument.id::TEXT, option_close
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'clearing-option-close', 'clearing:settlement', 'instrument', primary_instrument.id::TEXT, -option_close
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'underlying-delivery', inventory_account, 'instrument', secondary_instrument.id::TEXT, delivered
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'clearing-underlying-delivery', 'clearing:settlement', 'instrument', secondary_instrument.id::TEXT, -delivered
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'strike-cash', 'asset:cash', 'currency', normalized.cash_currency, strike_cash
        );
        PERFORM assert_economic_posting(
            expected_transaction_id, source_event.id, normalized.normalizer_version,
            'clearing-strike-cash', 'clearing:settlement', 'currency', normalized.cash_currency, -strike_cash
        );
        expected_posting_count := expected_posting_count + 6;
    END IF;

    IF normalized.cost_amount IS NOT NULL THEN
        IF normalized.cost_currency <> normalized.cash_currency OR
           (normalized.venue_contract_id IS NOT NULL AND normalized.cost_currency <> contract.currency) THEN
            RAISE EXCEPTION 'economic cost currency must match account and venue currency';
        END IF;
        IF normalized.cost_kind = 'fee' THEN
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'fee-expense', 'expense:fees', 'currency', normalized.cash_currency, normalized.cost_amount
            );
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'fee-cash', 'asset:cash', 'currency', normalized.cash_currency, -normalized.cost_amount
            );
        ELSE
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'rebate-cash', 'asset:cash', 'currency', normalized.cash_currency, normalized.cost_amount
            );
            PERFORM assert_economic_posting(
                expected_transaction_id, source_event.id, normalized.normalizer_version,
                'rebate-income', 'income:rebates', 'currency', normalized.cash_currency, -normalized.cost_amount
            );
        END IF;
        expected_posting_count := expected_posting_count + 2;
    END IF;

    SELECT count(*) INTO actual_posting_count
    FROM ledger_postings WHERE transaction_id = expected_transaction_id;
    IF actual_posting_count <> expected_posting_count OR
       transaction_row.posting_count <> expected_posting_count THEN
        RAISE EXCEPTION 'economic normalization ledger posting count is %/% expected %',
            actual_posting_count, transaction_row.posting_count, expected_posting_count;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_economic_normalization_row() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_economic_normalization(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_option_contract_terms_insert() RETURNS TRIGGER AS $$
DECLARE
    option_row instruments%ROWTYPE;
    underlying_row instruments%ROWTYPE;
    latest_effective_at TIMESTAMPTZ;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(
        'economic-option-terms:' || NEW.option_instrument_id::TEXT, 0
    ));

    IF EXISTS (
        SELECT 1 FROM option_contract_terms
        WHERE id = NEW.id OR (
            option_instrument_id = NEW.option_instrument_id AND
            source = NEW.source AND source_namespace = NEW.source_namespace AND
            source_record_id = NEW.source_record_id
        )
    ) THEN
        RETURN NEW;
    END IF;

    SELECT * INTO option_row FROM instruments WHERE id = NEW.option_instrument_id;
    SELECT * INTO underlying_row FROM instruments WHERE id = NEW.underlying_instrument_id;
    IF option_row.id IS NULL OR underlying_row.id IS NULL OR
       option_row.asset_class <> 'option' OR option_row.status = 'quarantined' OR
       underlying_row.status = 'quarantined' OR
       option_row.underlying_instrument_id <> underlying_row.id OR
       option_row.settlement_method <> 'physical' OR
       option_row.currency <> NEW.strike_currency THEN
        RAISE EXCEPTION 'option contract terms do not match immutable canonical option mechanics';
    END IF;

    SELECT max(effective_at) INTO latest_effective_at
    FROM option_contract_terms WHERE option_instrument_id = NEW.option_instrument_id;
    IF latest_effective_at IS NOT NULL AND NEW.effective_at <= latest_effective_at THEN
        RAISE EXCEPTION 'option contract terms must append in strictly increasing effective-time order';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM economic_event_normalizations AS normalized
        JOIN economic_source_events AS source_event ON source_event.id = normalized.source_event_id
        JOIN option_contract_terms AS selected_terms ON selected_terms.id = normalized.option_terms_id
        WHERE normalized.event_type IN (
                'settlement.option_exercise', 'settlement.option_assignment'
              )
          AND normalized.instrument_id = NEW.option_instrument_id
          AND NEW.effective_at > selected_terms.effective_at
          AND NEW.effective_at <= normalized.effective_at
          AND NEW.observed_at <= source_event.observed_at
    ) THEN
        RAISE EXCEPTION 'option contract terms would retroactively supersede an immutable normalization';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_economic_source_events_immutable
    BEFORE UPDATE OR DELETE ON economic_source_events
    FOR EACH ROW EXECUTE FUNCTION reject_economic_event_mutation();

CREATE TRIGGER trg_option_contract_terms_validate
    BEFORE INSERT ON option_contract_terms
    FOR EACH ROW EXECUTE FUNCTION validate_option_contract_terms_insert();

CREATE TRIGGER trg_option_contract_terms_immutable
    BEFORE UPDATE OR DELETE ON option_contract_terms
    FOR EACH ROW EXECUTE FUNCTION reject_economic_event_mutation();

CREATE TRIGGER trg_economic_normalizations_immutable
    BEFORE UPDATE OR DELETE ON economic_event_normalizations
    FOR EACH ROW EXECUTE FUNCTION reject_economic_event_mutation();

CREATE CONSTRAINT TRIGGER trg_economic_normalizations_semantic
    AFTER INSERT ON economic_event_normalizations
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_economic_normalization_row();
