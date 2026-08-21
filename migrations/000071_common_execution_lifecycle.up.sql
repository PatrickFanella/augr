-- Immutable common execution lifecycle. This migration starts empty and does
-- not activate a provider, scheduler, simulator, or legacy dual-write path.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM economic_event_normalizations
        WHERE reference_type = 'execution_fill'
    ) THEN
        RAISE EXCEPTION 'migration 71 cannot attach pre-existing execution_fill normalizations';
    END IF;
END;
$$;

CREATE TABLE execution_intents (
    id                         UUID PRIMARY KEY,
    account_id                 UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    environment                TEXT NOT NULL CHECK (environment IN ('paper_scored', 'paper_stress', 'shadow', 'live')),
    instrument_id              UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    idempotency_key            TEXT NOT NULL CHECK (idempotency_key <> '' AND idempotency_key = btrim(idempotency_key) AND char_length(idempotency_key) <= 256),
    desired_quantity_delta     NUMERIC NOT NULL CHECK (
                                   desired_quantity_delta <> 0 AND
                                   abs(desired_quantity_delta) < 100000000000000000000000000 AND
                                   desired_quantity_delta = round(desired_quantity_delta, 12)
                               ),
    decision_quote_snapshot_id UUID NOT NULL REFERENCES quote_snapshots(id) ON DELETE RESTRICT,
    decision_at                TIMESTAMPTZ NOT NULL CHECK (decision_at = date_trunc('microseconds', decision_at)),
    origin_type                TEXT NOT NULL CHECK (origin_type IN (
                                   'strategy_version', 'copy_subscription', 'portfolio_rebalance',
                                   'risk_reduction', 'operator', 'settlement', 'reconciliation'
                               )),
    origin_id                  TEXT NOT NULL CHECK (origin_id <> '' AND origin_id = btrim(origin_id) AND char_length(origin_id) <= 256),
    strategy_version_id        TEXT NOT NULL DEFAULT '' CHECK (strategy_version_id = btrim(strategy_version_id) AND char_length(strategy_version_id) <= 256),
    metadata                   JSONB NOT NULL CHECK (jsonb_typeof(metadata) = 'object'),
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW() CHECK (created_at = date_trunc('microseconds', created_at)),
    UNIQUE (account_id, idempotency_key),
    CHECK (origin_type <> 'strategy_version' OR strategy_version_id = origin_id),
    CHECK (id = economic_deterministic_uuid(
        'execution-intent', account_id::TEXT, idempotency_key
    ))
);

CREATE INDEX idx_execution_intents_account_created
    ON execution_intents (account_id, created_at, id);

CREATE TABLE execution_orders (
    id                      UUID PRIMARY KEY,
    intent_id               UUID NOT NULL REFERENCES execution_intents(id) ON DELETE RESTRICT,
    account_id              UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    instrument_id           UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    idempotency_key         TEXT NOT NULL CHECK (idempotency_key <> '' AND idempotency_key = btrim(idempotency_key) AND char_length(idempotency_key) <= 256),
    client_order_id         TEXT NOT NULL UNIQUE CHECK (client_order_id <> '' AND client_order_id = btrim(client_order_id) AND char_length(client_order_id) <= 256),
    side                    TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    order_type              TEXT NOT NULL CHECK (order_type IN ('market', 'limit', 'stop', 'stop_limit')),
    time_in_force           TEXT NOT NULL CHECK (time_in_force IN ('day', 'gtc', 'ioc', 'fok', 'gtd')),
    quantity                NUMERIC NOT NULL CHECK (
                                quantity > 0 AND quantity < 100000000000000000000000000 AND
                                quantity = round(quantity, 12)
                            ),
    limit_price             NUMERIC CHECK (
                                limit_price IS NULL OR (
                                    limit_price >= 0 AND limit_price < 100000000000000000000000000 AND
                                    limit_price = round(limit_price, 12)
                                )
                            ),
    stop_price              NUMERIC CHECK (
                                stop_price IS NULL OR (
                                    stop_price >= 0 AND stop_price < 100000000000000000000000000 AND
                                    stop_price = round(stop_price, 12)
                                )
                            ),
    venue                   TEXT NOT NULL CHECK (venue <> '' AND venue = lower(btrim(venue)) AND char_length(venue) <= 128),
    venue_contract_id       UUID NOT NULL REFERENCES venue_contracts(id) ON DELETE RESTRICT,
    route_quote_snapshot_id UUID NOT NULL REFERENCES quote_snapshots(id) ON DELETE RESTRICT,
    routed_at               TIMESTAMPTZ NOT NULL CHECK (routed_at = date_trunc('microseconds', routed_at)),
    policy_kind             TEXT NOT NULL CHECK (policy_kind IN ('simulation', 'venue')),
    policy_version          TEXT NOT NULL CHECK (policy_version <> '' AND policy_version = btrim(policy_version) AND char_length(policy_version) <= 256),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW() CHECK (created_at = date_trunc('microseconds', created_at)),
    UNIQUE (intent_id),
    CHECK (client_order_id = id::TEXT),
    CHECK (id = economic_deterministic_uuid(
        'execution-order', intent_id::TEXT, idempotency_key
    )),
    CHECK (
        (order_type = 'market' AND limit_price IS NULL AND stop_price IS NULL) OR
        (order_type = 'limit' AND limit_price IS NOT NULL AND stop_price IS NULL) OR
        (order_type = 'stop' AND limit_price IS NULL AND stop_price IS NOT NULL) OR
        (order_type = 'stop_limit' AND limit_price IS NOT NULL AND stop_price IS NOT NULL)
    )
);

CREATE INDEX idx_execution_orders_account_created
    ON execution_orders (account_id, created_at, id);

CREATE TABLE execution_order_bindings (
    id                UUID PRIMARY KEY,
    order_id          UUID NOT NULL UNIQUE REFERENCES execution_orders(id) ON DELETE RESTRICT,
    account_id        UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    venue             TEXT NOT NULL CHECK (venue <> '' AND venue = lower(btrim(venue)) AND char_length(venue) <= 128),
    external_order_id TEXT NOT NULL CHECK (external_order_id <> '' AND external_order_id = btrim(external_order_id) AND char_length(external_order_id) <= 512),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW() CHECK (created_at = date_trunc('microseconds', created_at)),
    UNIQUE (account_id, venue, external_order_id),
    CHECK (id = economic_deterministic_uuid(
        'execution-order-binding', order_id::TEXT
    ))
);

CREATE TABLE execution_fills (
    id                       UUID PRIMARY KEY,
    intent_id                UUID NOT NULL REFERENCES execution_intents(id) ON DELETE RESTRICT,
    order_id                 UUID NOT NULL REFERENCES execution_orders(id) ON DELETE RESTRICT,
    account_id               UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    instrument_id            UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    venue_contract_id        UUID NOT NULL REFERENCES venue_contracts(id) ON DELETE RESTRICT,
    economic_source_event_id UUID NOT NULL UNIQUE REFERENCES economic_source_events(id) ON DELETE RESTRICT,
    normalization_id         UUID NOT NULL UNIQUE REFERENCES economic_event_normalizations(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    ledger_transaction_id    UUID NOT NULL UNIQUE REFERENCES ledger_transactions(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    side                     TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    quantity                 NUMERIC NOT NULL CHECK (
                                 quantity > 0 AND quantity < 100000000000000000000000000 AND
                                 quantity = round(quantity, 12)
                             ),
    price                    NUMERIC NOT NULL CHECK (
                                 price >= 0 AND price < 100000000000000000000000000 AND
                                 price = round(price, 12)
                             ),
    source                   TEXT NOT NULL CHECK (source <> '' AND source = lower(btrim(source)) AND char_length(source) <= 128),
    source_namespace         TEXT NOT NULL CHECK (source_namespace <> '' AND source_namespace = btrim(source_namespace) AND char_length(source_namespace) <= 256),
    source_event_id          TEXT NOT NULL CHECK (source_event_id <> '' AND source_event_id = btrim(source_event_id) AND char_length(source_event_id) <= 512),
    source_revision          TEXT NOT NULL DEFAULT '' CHECK (source_revision = btrim(source_revision) AND char_length(source_revision) <= 256),
    effective_at             TIMESTAMPTZ NOT NULL CHECK (effective_at = date_trunc('microseconds', effective_at)),
    received_at              TIMESTAMPTZ NOT NULL CHECK (received_at = date_trunc('microseconds', received_at)),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW() CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (effective_at <= received_at),
    CHECK (id = economic_deterministic_uuid(
        'execution-fill', order_id::TEXT, economic_source_event_id::TEXT
    ))
);

CREATE INDEX idx_execution_fills_intent_created
    ON execution_fills (intent_id, created_at, id);

CREATE TABLE execution_lifecycle_events (
    id                         UUID PRIMARY KEY,
    ingest_sequence            BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    intent_id                  UUID NOT NULL REFERENCES execution_intents(id) ON DELETE RESTRICT,
    order_id                   UUID REFERENCES execution_orders(id) ON DELETE RESTRICT,
    binding_id                 UUID REFERENCES execution_order_bindings(id) ON DELETE RESTRICT,
    fill_id                    UUID REFERENCES execution_fills(id) ON DELETE RESTRICT,
    kind                       TEXT NOT NULL CHECK (kind IN (
                                   'intent_proposed', 'intent_allocated', 'risk_approved', 'risk_rejected',
                                   'order_routed', 'order_working', 'cancel_requested', 'fill_acknowledged',
                                   'fill_recorded', 'order_cancelled', 'order_expired', 'order_rejected',
                                   'unknown_venue_state', 'contradictory_venue_state',
                                   'fill_correction_observed', 'fill_bust_observed'
                               )),
    observation_class          TEXT NOT NULL CHECK (observation_class IN ('ordinary', 'correction', 'bust')),
    observation_discriminator TEXT NOT NULL DEFAULT '' CHECK (observation_discriminator = btrim(observation_discriminator) AND char_length(observation_discriminator) <= 512),
    prior_state                TEXT NOT NULL CHECK (prior_state IN (
                                   '', 'proposed', 'allocated', 'risk_approved', 'risk_rejected',
                                   'routed', 'working', 'partially_filled', 'filled', 'cancelled',
                                   'expired', 'rejected', 'failed_reconciliation'
                               )),
    next_state                 TEXT NOT NULL CHECK (next_state IN (
                                   'proposed', 'allocated', 'risk_approved', 'risk_rejected',
                                   'routed', 'working', 'partially_filled', 'filled', 'cancelled',
                                   'expired', 'rejected', 'failed_reconciliation'
                               )),
    account_id                 UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    environment                TEXT NOT NULL CHECK (environment IN ('paper_scored', 'paper_stress', 'shadow', 'live')),
    origin_type                TEXT NOT NULL CHECK (origin_type IN (
                                   'strategy_version', 'copy_subscription', 'portfolio_rebalance',
                                   'risk_reduction', 'operator', 'settlement', 'reconciliation'
                               )),
    origin_id                  TEXT NOT NULL CHECK (origin_id <> '' AND origin_id = btrim(origin_id) AND char_length(origin_id) <= 256),
    strategy_version_id        TEXT NOT NULL DEFAULT '' CHECK (strategy_version_id = btrim(strategy_version_id) AND char_length(strategy_version_id) <= 256),
    policy_kind                TEXT NOT NULL DEFAULT '' CHECK (policy_kind IN ('', 'simulation', 'venue')),
    policy_version             TEXT NOT NULL DEFAULT '' CHECK (policy_version = btrim(policy_version) AND char_length(policy_version) <= 256),
    quantity_delta             NUMERIC CHECK (
                                   quantity_delta IS NULL OR (
                                       quantity_delta <> 0 AND abs(quantity_delta) < 100000000000000000000000000 AND
                                       quantity_delta = round(quantity_delta, 12)
                                   )
                               ),
    cumulative_fill_quantity  NUMERIC CHECK (
                                   cumulative_fill_quantity IS NULL OR (
                                       cumulative_fill_quantity > 0 AND cumulative_fill_quantity < 100000000000000000000000000 AND
                                       cumulative_fill_quantity = round(cumulative_fill_quantity, 12)
                                   )
                               ),
    quote_snapshot_id          UUID REFERENCES quote_snapshots(id) ON DELETE RESTRICT,
    source                     TEXT NOT NULL CHECK (source <> '' AND source = lower(btrim(source)) AND char_length(source) <= 128),
    source_namespace           TEXT NOT NULL CHECK (source_namespace <> '' AND source_namespace = btrim(source_namespace) AND char_length(source_namespace) <= 256),
    source_event_id            TEXT NOT NULL CHECK (source_event_id <> '' AND source_event_id = btrim(source_event_id) AND char_length(source_event_id) <= 512),
    source_revision            TEXT NOT NULL DEFAULT '' CHECK (source_revision = btrim(source_revision) AND char_length(source_revision) <= 256),
    source_at                  TIMESTAMPTZ NOT NULL CHECK (source_at = date_trunc('microseconds', source_at)),
    received_at                TIMESTAMPTZ NOT NULL CHECK (received_at = date_trunc('microseconds', received_at)),
    actor                      TEXT NOT NULL CHECK (actor <> '' AND actor = btrim(actor) AND char_length(actor) <= 256),
    reason_code                TEXT NOT NULL CHECK (reason_code <> '' AND reason_code = lower(btrim(reason_code)) AND char_length(reason_code) <= 256),
    reason                     TEXT NOT NULL DEFAULT '' CHECK (reason = btrim(reason) AND char_length(reason) <= 2048),
    evidence_bytes             BYTEA NOT NULL CHECK (octet_length(evidence_bytes) > 0),
    evidence_sha256            TEXT NOT NULL CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$'),
    evidence                   JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    original_fill_id           UUID REFERENCES execution_fills(id) ON DELETE RESTRICT,
    original_source_event_id   TEXT NOT NULL DEFAULT '' CHECK (original_source_event_id = btrim(original_source_event_id) AND char_length(original_source_event_id) <= 512),
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW() CHECK (created_at = date_trunc('microseconds', created_at)),
    CHECK (source_at <= received_at),
	CHECK ((kind IN ('fill_acknowledged', 'fill_recorded')) = (cumulative_fill_quantity IS NOT NULL)),
    CHECK (evidence_sha256 = encode(digest(evidence_bytes, 'sha256'), 'hex')),
    CHECK (evidence = convert_from(evidence_bytes, 'UTF8')::JSONB),
    CHECK ((policy_kind = '') = (policy_version = '')),
    CHECK (
        (observation_class = 'ordinary' AND observation_discriminator = '' AND original_fill_id IS NULL AND original_source_event_id = '') OR
        (observation_class IN ('correction', 'bust') AND observation_discriminator <> '' AND original_fill_id IS NOT NULL AND original_source_event_id <> '')
    ),
    CHECK (
        observation_discriminator NOT LIKE 'revision:%' OR
        (source_revision <> '' AND observation_discriminator = 'revision:' || source_revision)
    ),
    CHECK (id = economic_deterministic_uuid(
        'execution-lifecycle-event', intent_id::TEXT, observation_class, source,
		source_namespace,
		CASE WHEN observation_class = 'ordinary' THEN source_event_id ELSE original_source_event_id END,
		observation_discriminator
    ))
);

CREATE UNIQUE INDEX idx_execution_lifecycle_events_ordinary_source
    ON execution_lifecycle_events (intent_id, source, source_namespace, source_event_id)
    WHERE observation_class = 'ordinary';

CREATE UNIQUE INDEX idx_execution_lifecycle_events_revision_source
    ON execution_lifecycle_events (
        intent_id, observation_class, source, source_namespace,
		original_source_event_id, observation_discriminator
    ) WHERE observation_class IN ('correction', 'bust');

CREATE INDEX idx_execution_lifecycle_events_intent_ingest
    ON execution_lifecycle_events (intent_id, ingest_sequence);

CREATE INDEX idx_execution_lifecycle_recovery
    ON execution_lifecycle_events (account_id, next_state, ingest_sequence DESC, intent_id)
    WHERE next_state IN ('routed', 'working', 'partially_filled');

CREATE FUNCTION reject_execution_lifecycle_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_intents_immutable
    BEFORE UPDATE OR DELETE ON execution_intents
    FOR EACH ROW EXECUTE FUNCTION reject_execution_lifecycle_mutation();
CREATE TRIGGER trg_execution_orders_immutable
    BEFORE UPDATE OR DELETE ON execution_orders
    FOR EACH ROW EXECUTE FUNCTION reject_execution_lifecycle_mutation();
CREATE TRIGGER trg_execution_order_bindings_immutable
    BEFORE UPDATE OR DELETE ON execution_order_bindings
    FOR EACH ROW EXECUTE FUNCTION reject_execution_lifecycle_mutation();
CREATE TRIGGER trg_execution_fills_immutable
    BEFORE UPDATE OR DELETE ON execution_fills
    FOR EACH ROW EXECUTE FUNCTION reject_execution_lifecycle_mutation();
CREATE TRIGGER trg_execution_lifecycle_events_immutable
    BEFORE UPDATE OR DELETE ON execution_lifecycle_events
    FOR EACH ROW EXECUTE FUNCTION reject_execution_lifecycle_mutation();

CREATE FUNCTION validate_execution_intent() RETURNS TRIGGER AS $$
DECLARE
    account_row accounts%ROWTYPE;
    instrument_row instruments%ROWTYPE;
    quote_row quote_snapshots%ROWTYPE;
BEGIN
    SELECT * INTO account_row FROM accounts WHERE id = NEW.account_id;
    SELECT * INTO instrument_row FROM instruments WHERE id = NEW.instrument_id;
    SELECT * INTO quote_row FROM quote_snapshots WHERE id = NEW.decision_quote_snapshot_id;
    IF account_row.id IS NULL OR account_row.status <> 'active' OR account_row.environment <> NEW.environment THEN
        RAISE EXCEPTION 'execution intent account context is invalid';
    END IF;
    IF instrument_row.id IS NULL OR instrument_row.status <> 'active' OR
       (instrument_row.expiration IS NOT NULL AND NEW.decision_at >= instrument_row.expiration) THEN
        RAISE EXCEPTION 'execution intent instrument is not active at decision time';
    END IF;
    IF quote_row.id IS NULL OR quote_row.instrument_id <> NEW.instrument_id OR
       quote_row.available_at IS NULL OR quote_row.available_at > NEW.decision_at THEN
        RAISE EXCEPTION 'execution intent decision quote is unavailable or mismatched';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_intents_validate
    BEFORE INSERT ON execution_intents
    FOR EACH ROW EXECUTE FUNCTION validate_execution_intent();

CREATE FUNCTION validate_execution_order() RETURNS TRIGGER AS $$
DECLARE
    intent_row execution_intents%ROWTYPE;
    contract_row venue_contracts%ROWTYPE;
    quote_row quote_snapshots%ROWTYPE;
    allocated NUMERIC;
    latest_state TEXT;
BEGIN
    SELECT * INTO intent_row FROM execution_intents WHERE id = NEW.intent_id;
    IF intent_row.id IS NULL OR intent_row.account_id <> NEW.account_id OR intent_row.instrument_id <> NEW.instrument_id THEN
        RAISE EXCEPTION 'execution order intent context is invalid';
    END IF;
    SELECT next_state INTO latest_state FROM execution_lifecycle_events
    WHERE intent_id = NEW.intent_id ORDER BY ingest_sequence DESC LIMIT 1;
    SELECT quantity_delta INTO allocated FROM execution_lifecycle_events
    WHERE intent_id = NEW.intent_id AND kind = 'intent_allocated'
    ORDER BY ingest_sequence DESC LIMIT 1;
    IF latest_state IS DISTINCT FROM 'risk_approved' OR allocated IS NULL OR
       NEW.quantity <> abs(allocated) OR
       NEW.side <> (CASE WHEN allocated > 0 THEN 'buy' ELSE 'sell' END) THEN
        RAISE EXCEPTION 'execution order does not copy the approved allocation';
    END IF;
    SELECT * INTO contract_row FROM venue_contracts WHERE id = NEW.venue_contract_id;
    IF contract_row.id IS NULL OR contract_row.instrument_id <> NEW.instrument_id OR contract_row.venue <> NEW.venue OR
       contract_row.valid_from > NEW.routed_at OR
       (contract_row.valid_to IS NOT NULL AND NEW.routed_at >= contract_row.valid_to) OR
       mod(NEW.quantity, contract_row.lot_size) <> 0 OR
       (NEW.limit_price IS NOT NULL AND mod(NEW.limit_price, contract_row.tick_size) <> 0) OR
       (NEW.stop_price IS NOT NULL AND mod(NEW.stop_price, contract_row.tick_size) <> 0) THEN
        RAISE EXCEPTION 'execution order venue contract or mechanics are invalid';
    END IF;
    SELECT * INTO quote_row FROM quote_snapshots WHERE id = NEW.route_quote_snapshot_id;
    IF quote_row.id IS NULL OR quote_row.instrument_id <> NEW.instrument_id OR
       quote_row.venue_contract_id IS DISTINCT FROM NEW.venue_contract_id OR quote_row.venue <> NEW.venue OR
       quote_row.available_at IS NULL OR quote_row.available_at > NEW.routed_at THEN
        RAISE EXCEPTION 'execution order route quote is unavailable or mismatched';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_orders_validate
    BEFORE INSERT ON execution_orders
    FOR EACH ROW EXECUTE FUNCTION validate_execution_order();

CREATE FUNCTION validate_execution_order_binding() RETURNS TRIGGER AS $$
DECLARE
    order_row execution_orders%ROWTYPE;
BEGIN
    SELECT * INTO order_row FROM execution_orders WHERE id = NEW.order_id;
    IF order_row.id IS NULL OR order_row.account_id <> NEW.account_id OR order_row.venue <> NEW.venue THEN
        RAISE EXCEPTION 'execution order binding context is invalid';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_order_bindings_validate
    BEFORE INSERT ON execution_order_bindings
    FOR EACH ROW EXECUTE FUNCTION validate_execution_order_binding();

CREATE FUNCTION validate_execution_fill() RETURNS TRIGGER AS $$
DECLARE
    intent_row execution_intents%ROWTYPE;
    order_row execution_orders%ROWTYPE;
    normalized economic_event_normalizations%ROWTYPE;
    source_row economic_source_events%ROWTYPE;
BEGIN
    SELECT * INTO intent_row FROM execution_intents WHERE id = NEW.intent_id;
    SELECT * INTO order_row FROM execution_orders WHERE id = NEW.order_id;
    SELECT * INTO normalized FROM economic_event_normalizations WHERE id = NEW.normalization_id;
    SELECT * INTO source_row FROM economic_source_events WHERE id = NEW.economic_source_event_id;
    IF intent_row.id IS NULL OR order_row.id IS NULL OR order_row.intent_id <> NEW.intent_id OR
       NEW.account_id <> intent_row.account_id OR NEW.account_id <> order_row.account_id OR
       NEW.instrument_id <> intent_row.instrument_id OR NEW.instrument_id <> order_row.instrument_id OR
       NEW.venue_contract_id <> order_row.venue_contract_id OR NEW.side <> order_row.side THEN
        RAISE EXCEPTION 'execution fill lifecycle context is invalid';
    END IF;
    IF source_row.id IS NULL OR normalized.id IS NULL OR normalized.source_event_id <> source_row.id OR
       normalized.ledger_transaction_id <> NEW.ledger_transaction_id OR
       normalized.reference_type <> 'execution_fill' OR normalized.reference_id <> NEW.id::TEXT OR
       normalized.event_type <> (CASE WHEN NEW.side = 'buy' THEN 'fill.buy' ELSE 'fill.sell' END) OR
       normalized.execution_origin_type <> intent_row.origin_type OR normalized.execution_origin_id <> intent_row.origin_id OR
       normalized.instrument_id IS DISTINCT FROM NEW.instrument_id OR
       normalized.venue_contract_id IS DISTINCT FROM NEW.venue_contract_id OR
       normalized.venue IS DISTINCT FROM order_row.venue OR normalized.quantity IS DISTINCT FROM NEW.quantity OR
       normalized.price IS DISTINCT FROM NEW.price OR normalized.effective_at <> NEW.effective_at THEN
        RAISE EXCEPTION 'execution fill normalization graph is invalid';
    END IF;
    IF source_row.account_id <> NEW.account_id OR source_row.source <> NEW.source OR
       source_row.source_namespace <> NEW.source_namespace OR source_row.source_event_id <> NEW.source_event_id OR
       source_row.source_revision <> NEW.source_revision OR source_row.observed_at <> NEW.received_at THEN
        RAISE EXCEPTION 'execution fill raw source identity is invalid';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_fills_validate
    BEFORE INSERT ON execution_fills
    FOR EACH ROW EXECUTE FUNCTION validate_execution_fill();

CREATE FUNCTION execution_lifecycle_transition_is_valid(
    event_kind TEXT,
    event_class TEXT,
    prior TEXT,
    next TEXT
) RETURNS BOOLEAN AS $$
BEGIN
    IF event_class IN ('correction', 'bust') THEN
        RETURN event_kind IN ('fill_correction_observed', 'fill_bust_observed') AND
               prior <> '' AND prior <> 'failed_reconciliation' AND next = 'failed_reconciliation';
    END IF;
    IF event_class <> 'ordinary' THEN
        RETURN FALSE;
    END IF;
    RETURN CASE event_kind
        WHEN 'intent_proposed' THEN prior = '' AND next = 'proposed'
        WHEN 'intent_allocated' THEN prior = 'proposed' AND next = 'allocated'
        WHEN 'risk_approved' THEN prior = 'allocated' AND next = 'risk_approved'
        WHEN 'risk_rejected' THEN prior = 'allocated' AND next = 'risk_rejected'
        WHEN 'order_routed' THEN prior = 'risk_approved' AND next = 'routed'
        WHEN 'order_working' THEN prior = 'routed' AND next = 'working'
        WHEN 'cancel_requested' THEN prior IN ('working', 'partially_filled') AND next = prior
        WHEN 'fill_acknowledged' THEN prior = 'routed' AND next IN ('partially_filled', 'filled')
        WHEN 'fill_recorded' THEN prior IN ('working', 'partially_filled') AND next IN ('partially_filled', 'filled')
        WHEN 'order_cancelled' THEN prior IN ('routed', 'working', 'partially_filled') AND next = 'cancelled'
        WHEN 'order_expired' THEN prior IN ('routed', 'working', 'partially_filled') AND next = 'expired'
        WHEN 'order_rejected' THEN prior IN ('routed', 'working', 'partially_filled') AND next = 'rejected'
        WHEN 'unknown_venue_state' THEN prior <> '' AND prior <> 'failed_reconciliation' AND next = 'failed_reconciliation'
        WHEN 'contradictory_venue_state' THEN prior <> '' AND prior <> 'failed_reconciliation' AND next = 'failed_reconciliation'
        ELSE FALSE
    END;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE FUNCTION validate_execution_lifecycle_event() RETURNS TRIGGER AS $$
DECLARE
    intent_row execution_intents%ROWTYPE;
    order_row execution_orders%ROWTYPE;
    binding_row execution_order_bindings%ROWTYPE;
    fill_row execution_fills%ROWTYPE;
    previous_state TEXT;
    allocated NUMERIC;
    existing_payload JSONB;
BEGIN
    SELECT * INTO intent_row FROM execution_intents WHERE id = NEW.intent_id FOR UPDATE;
    IF intent_row.id IS NULL OR NEW.account_id <> intent_row.account_id OR NEW.environment <> intent_row.environment OR
       NEW.origin_type <> intent_row.origin_type OR NEW.origin_id <> intent_row.origin_id OR
       NEW.strategy_version_id <> intent_row.strategy_version_id THEN
        RAISE EXCEPTION 'execution lifecycle event context differs from intent';
    END IF;

    -- Check replay only after taking the intent lock. A concurrent identical
    -- writer may have committed while this trigger waited; it must reach the
    -- unique-key conflict as an exact no-op instead of failing stale state.
    SELECT to_jsonb(event_row) INTO existing_payload
    FROM execution_lifecycle_events AS event_row WHERE id = NEW.id;
    IF FOUND THEN
        IF (existing_payload - 'ingest_sequence' - 'created_at') IS DISTINCT FROM
           (to_jsonb(NEW) - 'ingest_sequence' - 'created_at') THEN
            RAISE EXCEPTION 'execution lifecycle event idempotency conflict';
        END IF;
        RETURN NEW;
    END IF;

    SELECT next_state INTO previous_state FROM execution_lifecycle_events
    WHERE intent_id = NEW.intent_id ORDER BY ingest_sequence DESC LIMIT 1;
    previous_state := COALESCE(previous_state, '');
    IF NEW.prior_state <> previous_state OR
       NOT execution_lifecycle_transition_is_valid(NEW.kind, NEW.observation_class, NEW.prior_state, NEW.next_state) THEN
        RAISE EXCEPTION 'execution lifecycle transition is stale or illegal';
    END IF;

    SELECT quantity_delta INTO allocated FROM execution_lifecycle_events
    WHERE intent_id = NEW.intent_id AND kind = 'intent_allocated'
    ORDER BY ingest_sequence DESC LIMIT 1;

    -- Once an order or binding exists, every later event must copy that exact
    -- immutable context. Pre-route reconciliation failures remain valid with
    -- neither reference, but direct SQL cannot erase context after routing.
    SELECT * INTO order_row FROM execution_orders WHERE intent_id = NEW.intent_id;
    IF NEW.kind = 'order_routed' THEN
        IF order_row.id IS NULL OR NEW.order_id IS DISTINCT FROM order_row.id THEN
            RAISE EXCEPTION 'execution routed event requires the canonical order';
        END IF;
    ELSIF order_row.id IS NULL THEN
        IF NEW.order_id IS NOT NULL THEN
            RAISE EXCEPTION 'pre-route execution event cannot reference an order';
        END IF;
    ELSIF NEW.order_id IS DISTINCT FROM order_row.id THEN
        RAISE EXCEPTION 'post-route execution event must reference the canonical order';
    END IF;

    IF order_row.id IS NOT NULL THEN
        SELECT * INTO binding_row FROM execution_order_bindings WHERE order_id = order_row.id;
    END IF;
    IF binding_row.id IS NULL THEN
        IF NEW.binding_id IS NOT NULL AND NEW.kind NOT IN ('order_working', 'fill_acknowledged') THEN
            RAISE EXCEPTION 'execution event cannot introduce a binding outside acknowledgement';
        END IF;
    ELSIF NEW.binding_id IS DISTINCT FROM binding_row.id THEN
        RAISE EXCEPTION 'post-acknowledgement event must reference the canonical binding';
    END IF;

    IF NEW.kind = 'intent_proposed' THEN
        IF NEW.order_id IS NOT NULL OR NEW.binding_id IS NOT NULL OR NEW.fill_id IS NOT NULL OR
           NEW.quantity_delta IS DISTINCT FROM intent_row.desired_quantity_delta OR
           NEW.quote_snapshot_id IS DISTINCT FROM intent_row.decision_quote_snapshot_id OR
           NEW.policy_kind <> '' THEN
            RAISE EXCEPTION 'execution proposal event differs from intent';
        END IF;
    ELSIF NEW.kind = 'intent_allocated' THEN
        IF NEW.order_id IS NOT NULL OR NEW.binding_id IS NOT NULL OR NEW.fill_id IS NOT NULL OR
           NEW.quantity_delta IS NULL OR sign(NEW.quantity_delta) <> sign(intent_row.desired_quantity_delta) OR
           abs(NEW.quantity_delta) > abs(intent_row.desired_quantity_delta) OR
           NEW.quote_snapshot_id IS NOT NULL OR NEW.policy_kind <> '' THEN
            RAISE EXCEPTION 'execution allocation event is invalid';
        END IF;
    ELSIF NEW.kind IN ('risk_approved', 'risk_rejected') THEN
        IF allocated IS NULL OR NEW.quantity_delta IS DISTINCT FROM allocated OR NEW.order_id IS NOT NULL OR
           NEW.binding_id IS NOT NULL OR NEW.fill_id IS NOT NULL OR NEW.quote_snapshot_id IS NOT NULL OR NEW.policy_kind <> '' THEN
            RAISE EXCEPTION 'execution risk event differs from allocation';
        END IF;
    ELSE
        IF allocated IS NOT NULL AND NEW.quantity_delta IS DISTINCT FROM allocated THEN
            RAISE EXCEPTION 'execution event differs from allocated quantity';
        END IF;
        IF NEW.order_id IS NOT NULL THEN
            IF order_row.id IS NULL OR order_row.intent_id <> NEW.intent_id OR
               NEW.policy_kind <> order_row.policy_kind OR NEW.policy_version <> order_row.policy_version THEN
                RAISE EXCEPTION 'execution event order or policy context is invalid';
            END IF;
        ELSIF NEW.policy_kind <> '' THEN
            RAISE EXCEPTION 'execution event without order cannot carry policy';
        END IF;
        IF NEW.kind = 'order_routed' AND (
            order_row.id IS NULL OR NEW.quote_snapshot_id IS DISTINCT FROM order_row.route_quote_snapshot_id OR
            NEW.binding_id IS NOT NULL OR NEW.fill_id IS NOT NULL
        ) THEN
            RAISE EXCEPTION 'execution routed event differs from order';
        ELSIF NEW.kind <> 'order_routed' AND NEW.quote_snapshot_id IS NOT NULL THEN
            RAISE EXCEPTION 'only routed event may carry route quote';
        END IF;
    END IF;

    IF NEW.binding_id IS NOT NULL THEN
        IF binding_row.id IS NULL OR order_row.id IS NULL OR binding_row.order_id <> order_row.id THEN
            RAISE EXCEPTION 'execution event binding context is invalid';
        END IF;
    END IF;
    IF NEW.fill_id IS NOT NULL THEN
        SELECT * INTO fill_row FROM execution_fills WHERE id = NEW.fill_id;
        IF fill_row.id IS NULL OR order_row.id IS NULL OR fill_row.order_id <> order_row.id OR
           NEW.kind NOT IN ('fill_acknowledged', 'fill_recorded') THEN
            RAISE EXCEPTION 'execution event fill context is invalid';
        END IF;
    ELSIF NEW.kind IN ('fill_acknowledged', 'fill_recorded') THEN
        RAISE EXCEPTION 'execution fill event requires a fill';
    END IF;
    IF NEW.kind = 'order_working' AND (NEW.binding_id IS NULL OR NEW.fill_id IS NOT NULL) THEN
        RAISE EXCEPTION 'execution acknowledgement requires exactly one binding';
    END IF;
    IF NEW.kind = 'fill_acknowledged' AND NEW.binding_id IS NULL THEN
        RAISE EXCEPTION 'immediate execution fill requires binding';
    END IF;
    IF NEW.kind = 'fill_recorded' AND NEW.binding_id IS NULL THEN
        RAISE EXCEPTION 'execution fill requires existing binding';
    END IF;
    IF NEW.observation_class = 'correction' AND NEW.kind <> 'fill_correction_observed' OR
       NEW.observation_class = 'bust' AND NEW.kind <> 'fill_bust_observed' OR
       NEW.observation_class = 'ordinary' AND NEW.kind IN ('fill_correction_observed', 'fill_bust_observed') THEN
        RAISE EXCEPTION 'execution revision class differs from event kind';
    END IF;
    IF NEW.observation_class IN ('correction', 'bust') AND NOT EXISTS (
        SELECT 1 FROM execution_fills
        WHERE id = NEW.original_fill_id AND intent_id = NEW.intent_id AND source_event_id = NEW.original_source_event_id
    ) THEN
        RAISE EXCEPTION 'execution revision does not identify an original fill';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_lifecycle_events_validate
    BEFORE INSERT ON execution_lifecycle_events
    FOR EACH ROW EXECUTE FUNCTION validate_execution_lifecycle_event();

CREATE FUNCTION assert_execution_lifecycle_graph(target_intent_id UUID) RETURNS VOID AS $$
DECLARE
    proposed_count BIGINT;
    first_kind TEXT;
BEGIN
    SELECT COUNT(*) FILTER (WHERE kind = 'intent_proposed'),
           (array_agg(kind ORDER BY ingest_sequence))[1]
    INTO proposed_count, first_kind
    FROM execution_lifecycle_events WHERE intent_id = target_intent_id;
    IF proposed_count <> 1 OR first_kind IS DISTINCT FROM 'intent_proposed' THEN
        RAISE EXCEPTION 'execution intent % requires exactly one initial proposal', target_intent_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM execution_orders AS execution_order
        WHERE execution_order.intent_id = target_intent_id AND (
            SELECT COUNT(*) FROM execution_lifecycle_events
            WHERE intent_id = target_intent_id AND order_id = execution_order.id AND kind = 'order_routed'
        ) <> 1
    ) THEN
        RAISE EXCEPTION 'execution intent % order lacks exactly one routed event', target_intent_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM execution_order_bindings AS binding
        JOIN execution_orders AS execution_order ON execution_order.id = binding.order_id
        WHERE execution_order.intent_id = target_intent_id AND (
            SELECT COUNT(*) FROM execution_lifecycle_events
            WHERE intent_id = target_intent_id AND binding_id = binding.id AND kind IN ('order_working', 'fill_acknowledged')
        ) <> 1
    ) THEN
        RAISE EXCEPTION 'execution intent % binding lacks exactly one establishment event', target_intent_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM execution_fills AS fill_row
        WHERE fill_row.intent_id = target_intent_id AND (
            SELECT COUNT(*) FROM execution_lifecycle_events
            WHERE intent_id = target_intent_id AND fill_id = fill_row.id AND kind IN ('fill_recorded', 'fill_acknowledged')
        ) <> 1
    ) THEN
        RAISE EXCEPTION 'execution intent % fill lacks exactly one lifecycle event', target_intent_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM execution_lifecycle_events AS fill_event
        JOIN execution_fills AS fill_row ON fill_row.id = fill_event.fill_id
        JOIN economic_source_events AS source_row ON source_row.id = fill_row.economic_source_event_id
        WHERE fill_event.intent_id = target_intent_id AND fill_event.kind IN ('fill_recorded', 'fill_acknowledged') AND (
            fill_event.source <> source_row.source OR fill_event.source_namespace <> source_row.source_namespace OR
            fill_event.source_event_id <> source_row.source_event_id OR fill_event.source_revision <> source_row.source_revision OR
            fill_event.received_at <> source_row.observed_at OR fill_event.evidence_bytes <> source_row.raw_payload
        )
    ) THEN
        RAISE EXCEPTION 'execution intent % fill event differs from raw evidence', target_intent_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM execution_lifecycle_events AS fill_event
        WHERE fill_event.intent_id = target_intent_id AND fill_event.kind IN ('fill_recorded', 'fill_acknowledged') AND
              fill_event.cumulative_fill_quantity IS DISTINCT FROM (
                  SELECT SUM(fill_row.quantity)
                  FROM execution_lifecycle_events AS prior_fill_event
                  JOIN execution_fills AS fill_row ON fill_row.id = prior_fill_event.fill_id
                  WHERE prior_fill_event.intent_id = target_intent_id AND
                        prior_fill_event.kind IN ('fill_recorded', 'fill_acknowledged') AND
                        prior_fill_event.ingest_sequence <= fill_event.ingest_sequence
              )
    ) THEN
        RAISE EXCEPTION 'execution intent % fill cumulative quantity is invalid', target_intent_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM execution_lifecycle_events AS fill_event
        JOIN execution_orders AS execution_order ON execution_order.intent_id = fill_event.intent_id
        WHERE fill_event.intent_id = target_intent_id AND fill_event.kind IN ('fill_recorded', 'fill_acknowledged') AND (
            fill_event.cumulative_fill_quantity > execution_order.quantity OR
            (fill_event.next_state = 'filled') <> (fill_event.cumulative_fill_quantity = execution_order.quantity)
        )
    ) THEN
        RAISE EXCEPTION 'execution intent % fill state does not match exact order completion', target_intent_id;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION assert_execution_fill_normalization(target_normalization_id UUID) RETURNS VOID AS $$
DECLARE
    normalized economic_event_normalizations%ROWTYPE;
    fill_count BIGINT;
BEGIN
    SELECT * INTO normalized FROM economic_event_normalizations WHERE id = target_normalization_id;
    IF NOT FOUND OR normalized.reference_type <> 'execution_fill' THEN
        RETURN;
    END IF;
    IF normalized.reference_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RAISE EXCEPTION 'execution_fill normalization reference is not a canonical UUID';
    END IF;
    SELECT COUNT(*) INTO fill_count FROM execution_fills
    WHERE id = normalized.reference_id::UUID AND normalization_id = normalized.id AND
          economic_source_event_id = normalized.source_event_id AND
          ledger_transaction_id = normalized.ledger_transaction_id;
    IF fill_count <> 1 THEN
        RAISE EXCEPTION 'execution_fill normalization % requires exactly one lifecycle fill', normalized.id;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_execution_intent_complete() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_execution_lifecycle_graph(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_execution_child_complete() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_execution_lifecycle_graph(NEW.intent_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_execution_binding_complete() RETURNS TRIGGER AS $$
DECLARE
    target_intent_id UUID;
BEGIN
    SELECT intent_id INTO target_intent_id FROM execution_orders WHERE id = NEW.order_id;
    PERFORM assert_execution_lifecycle_graph(target_intent_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_execution_normalization_complete() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_execution_fill_normalization(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_execution_intents_complete
    AFTER INSERT ON execution_intents DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_execution_intent_complete();
CREATE CONSTRAINT TRIGGER trg_execution_orders_complete
    AFTER INSERT ON execution_orders DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_execution_child_complete();
CREATE CONSTRAINT TRIGGER trg_execution_order_bindings_complete
    AFTER INSERT ON execution_order_bindings DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_execution_binding_complete();
CREATE CONSTRAINT TRIGGER trg_execution_fills_complete
    AFTER INSERT ON execution_fills DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_execution_child_complete();
CREATE CONSTRAINT TRIGGER trg_execution_lifecycle_events_complete
    AFTER INSERT ON execution_lifecycle_events DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_execution_child_complete();
CREATE CONSTRAINT TRIGGER trg_economic_normalizations_execution_fill_complete
    AFTER INSERT ON economic_event_normalizations DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_execution_normalization_complete();
