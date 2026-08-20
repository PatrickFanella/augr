-- Immutable canonical venue-adapter artifacts and raw provider observations.
-- This migration starts empty and activates no provider, writer, scheduler,
-- credential path, legacy dual write, or live route.

-- This must be the first executable statement. It closes the schema-72 race
-- between the no-venue-order precondition and installation of authorization.
LOCK TABLE execution_orders IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM execution_orders
        WHERE policy_kind = 'venue'
    ) THEN
        RAISE EXCEPTION 'migration 73 cannot attach pre-existing venue orders without canonical adapter policy artifacts';
    END IF;
END;
$$;

-- Return only one of the two byte-for-byte Go artifacts reviewed for OVR-205.
-- JSONB equality validates the complete semantic object; returning the embedded
-- canonical text independently rejects reordered, whitespace-changed,
-- duplicate-key, missing-state, or otherwise rehashed caller bytes.
CREATE FUNCTION venue_adapter_policy_v1_canonical_bytes(policy JSONB) RETURNS BYTEA AS $function$
DECLARE
    alpaca_text CONSTANT TEXT := $alpaca${"schema":"venue-adapter-policy-v1","provider":"alpaca","venue":"alpaca","api_revision":"trading-api-v2","endpoint_families":["/v2/account/activities/FILL","/v2/orders","/v2/orders/{order_id}","/v2/orders:by_client_order_id","activity-sse","trade_updates"],"max_client_order_id_length":128,"retry_lookup":{"dedupe_key":"client_order_id","duplicate_result":"lookup_same_client_id","unresolved_miss":"retain_routed","historical_lookup":false},"authoritative_fill_namespace":"alpaca/account-activities/FILL","contract_metadata":{"required":false,"whole_object":false,"path":null,"values":null},"capabilities":[{"asset_class":"crypto_spot","order_type":"limit","time_in_force":"gtc"},{"asset_class":"crypto_spot","order_type":"limit","time_in_force":"ioc"},{"asset_class":"crypto_spot","order_type":"market","time_in_force":"gtc"},{"asset_class":"crypto_spot","order_type":"market","time_in_force":"ioc"},{"asset_class":"crypto_spot","order_type":"stop_limit","time_in_force":"gtc"},{"asset_class":"equity","order_type":"limit","time_in_force":"day"},{"asset_class":"equity","order_type":"limit","time_in_force":"fok"},{"asset_class":"equity","order_type":"limit","time_in_force":"gtc"},{"asset_class":"equity","order_type":"limit","time_in_force":"ioc"},{"asset_class":"equity","order_type":"market","time_in_force":"day"},{"asset_class":"equity","order_type":"market","time_in_force":"fok"},{"asset_class":"equity","order_type":"market","time_in_force":"gtc"},{"asset_class":"equity","order_type":"market","time_in_force":"ioc"},{"asset_class":"equity","order_type":"stop","time_in_force":"day"},{"asset_class":"equity","order_type":"stop","time_in_force":"gtc"},{"asset_class":"equity","order_type":"stop_limit","time_in_force":"day"},{"asset_class":"equity","order_type":"stop_limit","time_in_force":"gtc"},{"asset_class":"etf","order_type":"limit","time_in_force":"day"},{"asset_class":"etf","order_type":"limit","time_in_force":"fok"},{"asset_class":"etf","order_type":"limit","time_in_force":"gtc"},{"asset_class":"etf","order_type":"limit","time_in_force":"ioc"},{"asset_class":"etf","order_type":"market","time_in_force":"day"},{"asset_class":"etf","order_type":"market","time_in_force":"fok"},{"asset_class":"etf","order_type":"market","time_in_force":"gtc"},{"asset_class":"etf","order_type":"market","time_in_force":"ioc"},{"asset_class":"etf","order_type":"stop","time_in_force":"day"},{"asset_class":"etf","order_type":"stop","time_in_force":"gtc"},{"asset_class":"etf","order_type":"stop_limit","time_in_force":"day"},{"asset_class":"etf","order_type":"stop_limit","time_in_force":"gtc"}],"mappings":[{"namespace":"account_activity","value":"FILL","outcome":"fill"},{"namespace":"account_activity","value":"trade_bust","outcome":"bust"},{"namespace":"account_activity","value":"trade_correct","outcome":"correction"},{"namespace":"order_status","value":"accepted","outcome":"acknowledge"},{"namespace":"order_status","value":"accepted_for_bidding","outcome":"acknowledge"},{"namespace":"order_status","value":"calculated","outcome":"acknowledge"},{"namespace":"order_status","value":"canceled","outcome":"cancelled"},{"namespace":"order_status","value":"done_for_day","outcome":"acknowledge"},{"namespace":"order_status","value":"expired","outcome":"expired"},{"namespace":"order_status","value":"filled","outcome":"fill_notice"},{"namespace":"order_status","value":"held","outcome":"acknowledge"},{"namespace":"order_status","value":"new","outcome":"acknowledge"},{"namespace":"order_status","value":"partially_filled","outcome":"fill_notice"},{"namespace":"order_status","value":"pending_cancel","outcome":"acknowledge"},{"namespace":"order_status","value":"pending_new","outcome":"acknowledge"},{"namespace":"order_status","value":"pending_replace","outcome":"contradiction"},{"namespace":"order_status","value":"rejected","outcome":"rejected"},{"namespace":"order_status","value":"replaced","outcome":"contradiction"},{"namespace":"order_status","value":"stopped","outcome":"acknowledge"},{"namespace":"order_status","value":"suspended","outcome":"acknowledge"},{"namespace":"trade_update","value":"calculated","outcome":"acknowledge"},{"namespace":"trade_update","value":"canceled","outcome":"cancelled"},{"namespace":"trade_update","value":"done_for_day","outcome":"acknowledge"},{"namespace":"trade_update","value":"expired","outcome":"expired"},{"namespace":"trade_update","value":"fill","outcome":"fill_notice"},{"namespace":"trade_update","value":"new","outcome":"acknowledge"},{"namespace":"trade_update","value":"order_cancel_rejected","outcome":"no_change"},{"namespace":"trade_update","value":"order_replace_rejected","outcome":"no_change"},{"namespace":"trade_update","value":"partial_fill","outcome":"fill_notice"},{"namespace":"trade_update","value":"pending_cancel","outcome":"acknowledge"},{"namespace":"trade_update","value":"pending_new","outcome":"acknowledge"},{"namespace":"trade_update","value":"pending_replace","outcome":"contradiction"},{"namespace":"trade_update","value":"rejected","outcome":"rejected"},{"namespace":"trade_update","value":"replaced","outcome":"contradiction"},{"namespace":"trade_update","value":"stopped","outcome":"acknowledge"},{"namespace":"trade_update","value":"suspended","outcome":"acknowledge"}],"fill_identity_fields":["id","order_id","price","qty","side","symbol","transaction_time"],"fee_treatment":"present_exact_per_fill_commission_only"}$alpaca$;
    kalshi_text CONSTANT TEXT := $kalshi${"schema":"venue-adapter-policy-v1","provider":"kalshi","venue":"kalshi","api_revision":"trade-api-v2","endpoint_families":["/historical/fills","/historical/orders","/portfolio/events/orders","/portfolio/events/orders/{order_id}","/portfolio/fills","/portfolio/orders"],"max_client_order_id_length":64,"retry_lookup":{"dedupe_key":"client_order_id","duplicate_result":"conflict_then_current_historical_lookup","unresolved_miss":"retain_routed","historical_lookup":true},"authoritative_fill_namespace":"kalshi/portfolio/fills","contract_metadata":{"required":true,"whole_object":true,"path":["kalshi_v2","outcome"],"values":["no","yes"]},"capabilities":[{"asset_class":"prediction_contract","order_type":"limit","time_in_force":"fok"},{"asset_class":"prediction_contract","order_type":"limit","time_in_force":"gtc"},{"asset_class":"prediction_contract","order_type":"limit","time_in_force":"ioc"}],"mappings":[{"namespace":"fill_record","value":"fill","outcome":"fill"},{"namespace":"order_status","value":"canceled","outcome":"cancelled"},{"namespace":"order_status","value":"executed","outcome":"fill_notice"},{"namespace":"order_status","value":"resting","outcome":"acknowledge"}],"fill_identity_fields":["count_fp","fee_cost","fill_id","no_price_dollars","order_id","side","ticker","trade_id","yes_price_dollars"],"fee_treatment":"optional_exact_fee_cost_usd_attached"}$kalshi$;
BEGIN
    IF policy = alpaca_text::JSONB THEN
        RETURN convert_to(alpaca_text, 'UTF8');
    ELSIF policy = kalshi_text::JSONB THEN
        RETURN convert_to(kalshi_text, 'UTF8');
    END IF;
    RAISE EXCEPTION 'canonical venue adapter policy is not one of the two reviewed v1 artifacts';
END;
$function$ LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog, pg_temp;

CREATE TABLE venue_adapter_policy_artifacts (
    id              UUID PRIMARY KEY,
    schema_name     TEXT NOT NULL CHECK (schema_name = 'venue-adapter-policy-v1'),
    provider        TEXT NOT NULL CHECK (provider IN ('alpaca', 'kalshi')),
    venue           TEXT NOT NULL CHECK (venue IN ('alpaca', 'kalshi') AND venue = provider),
    policy_version  TEXT NOT NULL UNIQUE CHECK (
                        policy_version = btrim(policy_version) AND
                        char_length(policy_version) <= 256
                    ),
    sha256          TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK (octet_length(canonical_bytes) > 0),
    canonical_json  JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', NOW())
                    CHECK (created_at = date_trunc('microseconds', created_at)),
    UNIQUE (policy_version, provider, venue),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (policy_version = schema_name || '@sha256:' || sha256),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json ->> 'schema' = schema_name),
    CHECK (canonical_json ->> 'provider' = provider),
    CHECK (canonical_json ->> 'venue' = venue),
    CHECK (canonical_bytes = venue_adapter_policy_v1_canonical_bytes(canonical_json)),
    CHECK (id = economic_deterministic_uuid(
        'venue-adapter-policy-artifact', policy_version
    ))
);

CREATE INDEX idx_venue_adapter_policy_artifacts_created
    ON venue_adapter_policy_artifacts (created_at, id);

CREATE FUNCTION reject_venue_adapter_fact_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_venue_adapter_policy_artifacts_immutable
    BEFORE UPDATE OR DELETE ON venue_adapter_policy_artifacts
    FOR EACH ROW EXECUTE FUNCTION reject_venue_adapter_fact_mutation();

CREATE TABLE venue_observations (
    id                   UUID PRIMARY KEY,
    account_id           UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    intent_id            UUID NOT NULL REFERENCES execution_intents(id) ON DELETE RESTRICT,
    order_id             UUID NOT NULL REFERENCES execution_orders(id) ON DELETE RESTRICT,
    binding_id           UUID REFERENCES execution_order_bindings(id) ON DELETE RESTRICT,
    venue_contract_id    UUID NOT NULL REFERENCES venue_contracts(id) ON DELETE RESTRICT,
    provider             TEXT NOT NULL CHECK (provider IN ('alpaca', 'kalshi')),
    venue                TEXT NOT NULL CHECK (venue IN ('alpaca', 'kalshi') AND venue = provider),
    policy_version       TEXT NOT NULL CHECK (
                             policy_version = btrim(policy_version) AND
                             char_length(policy_version) <= 256
                         ),
    kind                 TEXT NOT NULL CHECK (kind IN (
                             'submit_response', 'order_snapshot', 'trade_update', 'fill',
                             'correction', 'bust', 'cancel_response', 'malformed_response'
                         )),
    provider_state       TEXT NOT NULL CHECK (
                             provider_state <> '' AND provider_state = btrim(provider_state) AND
                             char_length(provider_state) <= 128
                         ),
    mapped_outcome       TEXT NOT NULL CHECK (mapped_outcome IN (
                             'acknowledge', 'no_change', 'fill_notice', 'fill',
                             'cancelled', 'expired', 'rejected', 'correction', 'bust',
                             'unknown_state', 'contradiction', 'malformed_observation'
                         )),
    external_order_id    TEXT NOT NULL DEFAULT '' CHECK (
                             external_order_id = btrim(external_order_id) AND
                             char_length(external_order_id) <= 512
                         ),
    client_order_id      TEXT NOT NULL CHECK (
                             client_order_id <> '' AND client_order_id = btrim(client_order_id) AND
                             char_length(client_order_id) <= 256
                         ),
    provider_contract_id TEXT NOT NULL DEFAULT '' CHECK (
                             provider_contract_id = btrim(provider_contract_id) AND
                             char_length(provider_contract_id) <= 512
                         ),
    canonical_outcome    TEXT NOT NULL DEFAULT '' CHECK (canonical_outcome IN ('', 'yes', 'no')),
    provider_book_side   TEXT NOT NULL DEFAULT '' CHECK (provider_book_side IN ('', 'bid', 'ask')),
    provider_action      TEXT NOT NULL DEFAULT '' CHECK (provider_action IN ('', 'buy', 'sell')),
    provider_price       NUMERIC CHECK (
                             provider_price IS NULL OR (
                                 provider_price >= 0 AND
                                 provider_price < 100000000000000000000000000 AND
                                 provider_price = round(provider_price, 12)
                             )
                         ),
    identity_kind        TEXT NOT NULL CHECK (identity_kind IN ('provider', 'local_response')),
    source_namespace     TEXT NOT NULL CHECK (
                             source_namespace <> '' AND source_namespace = btrim(source_namespace) AND
                             char_length(source_namespace) <= 256
                         ),
    source_event_id      TEXT NOT NULL CHECK (
                             source_event_id <> '' AND source_event_id = btrim(source_event_id) AND
                             char_length(source_event_id) <= 512
                         ),
    source_revision      TEXT NOT NULL DEFAULT '' CHECK (
                             source_revision = btrim(source_revision) AND
                             char_length(source_revision) <= 256
                         ),
    source_at            TIMESTAMPTZ NOT NULL CHECK (source_at = date_trunc('microseconds', source_at)),
    received_at          TIMESTAMPTZ NOT NULL CHECK (received_at = date_trunc('microseconds', received_at)),
    raw_bytes            BYTEA NOT NULL CHECK (octet_length(raw_bytes) > 0),
    raw_sha256           TEXT NOT NULL CHECK (raw_sha256 ~ '^[0-9a-f]{64}$'),
    raw_json             JSONB NOT NULL CHECK (jsonb_typeof(raw_json) = 'object'),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', NOW())
                         CHECK (created_at = date_trunc('microseconds', created_at)),
    UNIQUE (account_id, provider, source_namespace, source_event_id),
    FOREIGN KEY (policy_version, provider, venue)
        REFERENCES venue_adapter_policy_artifacts(policy_version, provider, venue) ON DELETE RESTRICT,
    CHECK (source_at <= received_at),
    CHECK ((identity_kind = 'local_response') = (source_event_id LIKE 'local-response/%')),
    CHECK (convert_from(raw_bytes, 'UTF8') IS JSON OBJECT WITH UNIQUE KEYS),
    CHECK (raw_sha256 = encode(digest(raw_bytes, 'sha256'), 'hex')),
    CHECK (raw_json = convert_from(raw_bytes, 'UTF8')::JSONB),
    CHECK (id = economic_deterministic_uuid(
        'venue-observation', account_id::TEXT, provider, source_namespace, source_event_id
    ))
);

CREATE INDEX idx_venue_observations_order_received
    ON venue_observations (order_id, received_at, id);
CREATE INDEX idx_venue_observations_recovery
    ON venue_observations (account_id, provider, source_namespace, source_event_id);

CREATE TRIGGER trg_venue_observations_immutable
    BEFORE UPDATE OR DELETE ON venue_observations
    FOR EACH ROW EXECUTE FUNCTION reject_venue_adapter_fact_mutation();

CREATE FUNCTION validate_venue_order_policy_artifact() RETURNS TRIGGER AS $$
DECLARE
    artifact_row venue_adapter_policy_artifacts%ROWTYPE;
    contract_row venue_contracts%ROWTYPE;
    instrument_asset_class TEXT;
BEGIN
    IF NEW.policy_kind <> 'venue' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO artifact_row
    FROM venue_adapter_policy_artifacts
    WHERE policy_version = NEW.policy_version;
    IF artifact_row.id IS NULL OR artifact_row.venue <> NEW.venue THEN
        RAISE EXCEPTION 'venue order requires a registered same-venue adapter policy artifact';
    END IF;
    SELECT * INTO contract_row FROM venue_contracts WHERE id = NEW.venue_contract_id;
    SELECT asset_class INTO instrument_asset_class FROM instruments WHERE id = NEW.instrument_id;
    IF contract_row.id IS NULL OR contract_row.venue <> NEW.venue OR
       char_length(NEW.client_order_id) > (artifact_row.canonical_json ->> 'max_client_order_id_length')::INTEGER OR
       NOT EXISTS (
           SELECT 1
           FROM jsonb_array_elements(artifact_row.canonical_json -> 'capabilities') AS capability
           WHERE capability ->> 'asset_class' = instrument_asset_class
             AND capability ->> 'order_type' = NEW.order_type
             AND capability ->> 'time_in_force' = NEW.time_in_force
       ) THEN
        RAISE EXCEPTION 'venue order is not authorized by its adapter policy and dated contract';
    END IF;
    IF artifact_row.provider = 'kalshi' AND contract_row.metadata NOT IN (
        '{"kalshi_v2":{"outcome":"yes"}}'::JSONB,
        '{"kalshi_v2":{"outcome":"no"}}'::JSONB
    ) THEN
        RAISE EXCEPTION 'Kalshi venue order requires exact immutable kalshi_v2 outcome metadata';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_execution_orders_venue_policy
    BEFORE INSERT ON execution_orders
    FOR EACH ROW EXECUTE FUNCTION validate_venue_order_policy_artifact();

CREATE FUNCTION validate_venue_observation_semantics() RETURNS TRIGGER AS $$
DECLARE
    order_row execution_orders%ROWTYPE;
    artifact_row venue_adapter_policy_artifacts%ROWTYPE;
    contract_row venue_contracts%ROWTYPE;
    binding_row execution_order_bindings%ROWTYPE;
    mapping_namespace TEXT;
    policy_outcome TEXT;
    metadata_outcome TEXT;
    expected_book_side TEXT;
    expected_provider_price NUMERIC;
BEGIN
    SELECT * INTO order_row FROM execution_orders WHERE id = NEW.order_id;
    SELECT * INTO artifact_row FROM venue_adapter_policy_artifacts WHERE policy_version = NEW.policy_version;
    SELECT * INTO contract_row FROM venue_contracts WHERE id = NEW.venue_contract_id;
    -- Load by canonical order even when the observation was journaled before
    -- the first binding in this transaction.  The deferred check must see a
    -- binding added later and validate the provider external ID against it.
    SELECT * INTO binding_row FROM execution_order_bindings WHERE order_id = NEW.order_id;
    IF order_row.id IS NULL OR artifact_row.id IS NULL OR contract_row.id IS NULL OR
       order_row.intent_id <> NEW.intent_id OR order_row.account_id <> NEW.account_id OR
       order_row.venue_contract_id <> NEW.venue_contract_id OR order_row.venue <> NEW.venue OR
       order_row.policy_kind <> 'venue' OR order_row.policy_version <> NEW.policy_version OR
       artifact_row.provider <> NEW.provider OR artifact_row.venue <> NEW.venue THEN
        RAISE EXCEPTION 'venue observation canonical order context is invalid';
    END IF;
    IF (NEW.binding_id IS NOT NULL AND NEW.binding_id IS DISTINCT FROM binding_row.id) OR
       (binding_row.id IS NOT NULL AND (
           binding_row.order_id <> NEW.order_id OR
           binding_row.account_id <> NEW.account_id OR binding_row.venue <> NEW.venue
       )) THEN
        RAISE EXCEPTION 'venue observation binding context is invalid';
    END IF;

    mapping_namespace := CASE NEW.kind
        WHEN 'trade_update' THEN 'trade_update'
        WHEN 'fill' THEN CASE WHEN NEW.provider = 'alpaca' THEN 'account_activity' ELSE 'fill_record' END
        WHEN 'correction' THEN 'account_activity'
        WHEN 'bust' THEN 'account_activity'
        WHEN 'malformed_response' THEN NULL
        ELSE 'order_status'
    END;
    IF mapping_namespace IS NOT NULL THEN
        SELECT mapping ->> 'outcome' INTO policy_outcome
        FROM jsonb_array_elements(artifact_row.canonical_json -> 'mappings') AS mapping
        WHERE mapping ->> 'namespace' = mapping_namespace
          AND mapping ->> 'value' = NEW.provider_state;
    END IF;

    IF NEW.mapped_outcome = 'malformed_observation' THEN
        IF NEW.kind <> 'malformed_response' OR NEW.identity_kind <> 'local_response' THEN
            RAISE EXCEPTION 'malformed venue observation mapping is invalid';
        END IF;
    ELSIF NEW.mapped_outcome = 'unknown_state' THEN
        IF policy_outcome IS NOT NULL THEN
            RAISE EXCEPTION 'known provider state cannot be labelled unknown';
        END IF;
    ELSIF NEW.mapped_outcome = 'contradiction' THEN
        NULL;
    ELSIF NEW.mapped_outcome = 'no_change' THEN
        IF policy_outcome NOT IN ('acknowledge', 'no_change') THEN
            RAISE EXCEPTION 'venue no-change observation is not policy-compatible';
        END IF;
    ELSIF policy_outcome IS DISTINCT FROM NEW.mapped_outcome THEN
        RAISE EXCEPTION 'venue observation outcome differs from reviewed provider mapping';
    END IF;

    IF NEW.mapped_outcome NOT IN ('contradiction', 'malformed_observation') THEN
        IF NEW.client_order_id <> order_row.client_order_id OR
           NEW.provider_contract_id <> contract_row.contract_id OR
           (binding_row.id IS NOT NULL AND NEW.external_order_id <> binding_row.external_order_id) THEN
            RAISE EXCEPTION 'venue observation provider identity contradicts canonical context';
        END IF;
        IF NEW.mapped_outcome = 'fill' AND
           NEW.source_namespace <> artifact_row.canonical_json ->> 'authoritative_fill_namespace' THEN
            RAISE EXCEPTION 'venue economic fill observation is not from the authoritative feed';
        END IF;
        IF NEW.provider = 'alpaca' THEN
            IF NEW.canonical_outcome <> '' OR NEW.provider_book_side <> '' OR
               (NEW.provider_action <> '' AND NEW.provider_action <> order_row.side) THEN
                RAISE EXCEPTION 'Alpaca observation carries invalid prediction-market semantics';
            END IF;
        ELSE
            metadata_outcome := contract_row.metadata #>> '{kalshi_v2,outcome}';
            expected_book_side := CASE
                WHEN metadata_outcome = 'yes' AND order_row.side = 'buy' THEN 'bid'
                WHEN metadata_outcome = 'yes' AND order_row.side = 'sell' THEN 'ask'
                WHEN metadata_outcome = 'no' AND order_row.side = 'buy' THEN 'ask'
                WHEN metadata_outcome = 'no' AND order_row.side = 'sell' THEN 'bid'
                ELSE NULL
            END;
            IF contract_row.metadata NOT IN (
                   '{"kalshi_v2":{"outcome":"yes"}}'::JSONB,
                   '{"kalshi_v2":{"outcome":"no"}}'::JSONB
               ) OR NEW.canonical_outcome <> metadata_outcome OR
               NEW.provider_book_side <> expected_book_side OR NEW.provider_action <> order_row.side THEN
                RAISE EXCEPTION 'Kalshi observation outcome/action/book side contradicts immutable contract';
            END IF;
            IF NEW.provider_price IS NOT NULL AND order_row.limit_price IS NOT NULL THEN
                expected_provider_price := CASE
                    WHEN metadata_outcome = 'yes' THEN order_row.limit_price
                    ELSE 1 - order_row.limit_price
                END;
                IF NEW.kind <> 'fill' AND NEW.provider_price <> expected_provider_price THEN
                    RAISE EXCEPTION 'Kalshi order observation price contradicts immutable projection';
                END IF;
            END IF;
        END IF;
        IF NEW.mapped_outcome = 'fill' AND NEW.provider_price IS NULL THEN
            RAISE EXCEPTION 'venue fill observation requires exact provider price';
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_venue_observations_semantics
    AFTER INSERT ON venue_observations DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_venue_observation_semantics();

CREATE FUNCTION validate_venue_cancel_command(event_row execution_lifecycle_events) RETURNS VOID AS $$
DECLARE
    order_row execution_orders%ROWTYPE;
    artifact_row venue_adapter_policy_artifacts%ROWTYPE;
    binding_row execution_order_bindings%ROWTYPE;
    expected_namespace TEXT;
    expected_path TEXT;
    expected_bytes BYTEA;
BEGIN
    SELECT * INTO order_row FROM execution_orders WHERE id = event_row.order_id;
    SELECT * INTO artifact_row FROM venue_adapter_policy_artifacts WHERE policy_version = event_row.policy_version;
    SELECT * INTO binding_row FROM execution_order_bindings WHERE order_id = order_row.id;
    expected_namespace := 'venue-adapter-policy-v1/' || artifact_row.provider || '/' ||
        event_row.policy_version || '/cancel-request-v1';
    expected_path := CASE artifact_row.provider
        WHEN 'alpaca' THEN '/v2/orders/{external_order_id}'
        WHEN 'kalshi' THEN '/portfolio/events/orders/{external_order_id}'
    END;
    expected_bytes := convert_to(
        '{"schema":"venue-cancel-request-v1","order_id":' || to_json(order_row.id::TEXT)::TEXT ||
        ',"provider":' || to_json(artifact_row.provider)::TEXT ||
        ',"venue":' || to_json(order_row.venue)::TEXT ||
        ',"policy_version":' || to_json(order_row.policy_version)::TEXT ||
        ',"client_order_id":' || to_json(order_row.client_order_id)::TEXT ||
        ',"binding_id":' || to_json(COALESCE(binding_row.id::TEXT, ''))::TEXT ||
        ',"external_order_id":' || to_json(COALESCE(binding_row.external_order_id, ''))::TEXT ||
        ',"method":"DELETE","path_template":' || to_json(expected_path)::TEXT ||
        ',"request_body":"<empty>"}',
        'UTF8'
    );
    IF order_row.id IS NULL OR artifact_row.id IS NULL OR event_row.source <> 'venue_command' OR
       event_row.source_namespace <> expected_namespace OR
       event_row.source_event_id <> order_row.id::TEXT || '/cancel-request-v1' OR
       event_row.source_revision <> '' OR event_row.evidence_bytes <> expected_bytes THEN
        RAISE EXCEPTION 'venue cancellation command identity or canonical evidence is invalid';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_venue_lifecycle_observation() RETURNS TRIGGER AS $$
DECLARE
    expected_outcomes TEXT[];
BEGIN
    IF NEW.policy_kind <> 'venue' OR NEW.kind = 'order_routed' THEN
        RETURN NULL;
    END IF;
    IF NEW.kind = 'cancel_requested' THEN
        PERFORM validate_venue_cancel_command(NEW);
        RETURN NULL;
    END IF;
    expected_outcomes := CASE NEW.kind
        WHEN 'order_working' THEN ARRAY['acknowledge']::TEXT[]
        WHEN 'fill_acknowledged' THEN ARRAY['fill']::TEXT[]
        WHEN 'fill_recorded' THEN ARRAY['fill']::TEXT[]
        WHEN 'order_cancelled' THEN ARRAY['cancelled']::TEXT[]
        WHEN 'order_expired' THEN ARRAY['expired']::TEXT[]
        WHEN 'order_rejected' THEN ARRAY['rejected']::TEXT[]
        WHEN 'unknown_venue_state' THEN ARRAY['unknown_state']::TEXT[]
        WHEN 'contradictory_venue_state' THEN ARRAY['contradiction', 'malformed_observation']::TEXT[]
        WHEN 'fill_correction_observed' THEN ARRAY['correction']::TEXT[]
        WHEN 'fill_bust_observed' THEN ARRAY['bust']::TEXT[]
        ELSE ARRAY[]::TEXT[]
    END;
    IF cardinality(expected_outcomes) = 0 OR NOT EXISTS (
        SELECT 1 FROM venue_observations AS observation
        WHERE observation.account_id = NEW.account_id
          AND observation.intent_id = NEW.intent_id
          AND observation.order_id = NEW.order_id
          AND observation.provider = NEW.source
          AND observation.policy_version = NEW.policy_version
          AND observation.source_namespace = NEW.source_namespace
          AND observation.source_event_id = NEW.source_event_id
          AND observation.source_revision = NEW.source_revision
          AND observation.source_at = NEW.source_at
          AND observation.received_at = NEW.received_at
          AND observation.raw_bytes = NEW.evidence_bytes
          AND observation.raw_sha256 = NEW.evidence_sha256
          AND observation.raw_json = NEW.evidence
          AND observation.mapped_outcome = ANY(expected_outcomes)
    ) THEN
        RAISE EXCEPTION 'provider-driven venue lifecycle event requires exact prior raw observation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_execution_lifecycle_venue_observation
    AFTER INSERT ON execution_lifecycle_events DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_venue_lifecycle_observation();

CREATE FUNCTION validate_venue_execution_fill_observation() RETURNS TRIGGER AS $$
DECLARE
    order_row execution_orders%ROWTYPE;
    source_row economic_source_events%ROWTYPE;
    observation_row venue_observations%ROWTYPE;
BEGIN
    SELECT * INTO order_row FROM execution_orders WHERE id = NEW.order_id;
    IF order_row.policy_kind <> 'venue' THEN
        RETURN NULL;
    END IF;
    SELECT * INTO source_row FROM economic_source_events WHERE id = NEW.economic_source_event_id;
    SELECT * INTO observation_row
    FROM venue_observations
    WHERE account_id = NEW.account_id
      AND order_id = NEW.order_id
      AND provider = NEW.source
      AND policy_version = order_row.policy_version
      AND source_namespace = NEW.source_namespace
      AND source_event_id = NEW.source_event_id;
    IF observation_row.id IS NULL OR observation_row.mapped_outcome <> 'fill' OR
       observation_row.source_revision <> NEW.source_revision OR
       observation_row.source_at <> NEW.effective_at OR observation_row.received_at <> NEW.received_at OR
       source_row.source_revision <> observation_row.source_revision OR
       source_row.observed_at <> observation_row.received_at OR
       source_row.raw_payload <> observation_row.raw_bytes OR
       source_row.payload_sha256 <> observation_row.raw_sha256 OR
       (observation_row.provider = 'alpaca' AND observation_row.provider_price <> NEW.price) OR
       (observation_row.provider = 'kalshi' AND observation_row.provider_price <>
           CASE observation_row.canonical_outcome WHEN 'yes' THEN NEW.price ELSE 1 - NEW.price END) THEN
        RAISE EXCEPTION 'venue execution fill requires exact raw venue and economic evidence';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_execution_fills_venue_observation
    AFTER INSERT ON execution_fills DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_venue_execution_fill_observation();
