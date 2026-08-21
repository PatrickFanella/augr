-- Canonical append-only quote and depth observations. This migration starts
-- empty: legacy FLOAT/JSON market-data rows lack sufficient canonical identity,
-- exact-decimal provenance, and availability timestamps for automatic upgrade.

CREATE TABLE quote_snapshots (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ingest_sequence       BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    instrument_id         UUID NOT NULL REFERENCES instruments(id) ON DELETE RESTRICT,
    venue_contract_id     UUID REFERENCES venue_contracts(id) ON DELETE RESTRICT,
    provider              TEXT NOT NULL
                                  CHECK (provider <> '' AND provider = lower(btrim(provider))),
    venue                 TEXT NOT NULL
                                  CHECK (venue <> '' AND venue = lower(btrim(venue))),
    source                TEXT CHECK (source IS NULL OR (
                                  source <> '' AND source = btrim(source)
                              )),
    observation_namespace TEXT NOT NULL CHECK (
                                  observation_namespace <> '' AND
                                  observation_namespace = btrim(observation_namespace)
                              ),
    observation_id        TEXT NOT NULL CHECK (
                                  observation_id <> '' AND observation_id = btrim(observation_id)
                              ),
    source_revision       TEXT NOT NULL DEFAULT ''
                                  CHECK (source_revision = btrim(source_revision)),
    source_sequence       BIGINT CHECK (source_sequence >= 0),
    exchange_at           TIMESTAMPTZ,
    received_at           TIMESTAMPTZ NOT NULL,
    available_at          TIMESTAMPTZ,
    bid                   NUMERIC,
    bid_size              NUMERIC,
    ask                   NUMERIC,
    ask_size              NUMERIC,
    last                  NUMERIC,
    mark                  NUMERIC,
    market_status         TEXT CHECK (market_status IS NULL OR (
                                  market_status <> '' AND
                                  market_status = lower(btrim(market_status))
                              )),
    session_status        TEXT CHECK (session_status IS NULL OR (
                                  session_status <> '' AND
                                  session_status = lower(btrim(session_status))
                              )),
    bid_depth_count       INTEGER NOT NULL
                                  CHECK (bid_depth_count BETWEEN 0 AND 1000),
    ask_depth_count       INTEGER NOT NULL
                                  CHECK (ask_depth_count BETWEEN 0 AND 1000),
    metadata              JSONB NOT NULL DEFAULT '{}'::jsonb
                                  CHECK (jsonb_typeof(metadata) = 'object'),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instrument_id, provider, venue, observation_namespace, observation_id, source_revision),
    CHECK (exchange_at IS NULL OR exchange_at <= received_at),
    CHECK (available_at IS NULL OR available_at >= received_at),
    CHECK (bid IS NULL OR ask IS NULL OR bid <= ask),
    CHECK ((bid_size IS NULL OR bid IS NOT NULL) AND
           (ask_size IS NULL OR ask IS NOT NULL)),
    CHECK (bid IS NULL OR (
        bid >= 0 AND bid < 100000000000000000000000000 AND bid = round(bid, 12)
    )),
    CHECK (bid_size IS NULL OR (
        bid_size >= 0 AND bid_size < 100000000000000000000000000 AND
        bid_size = round(bid_size, 12)
    )),
    CHECK (ask IS NULL OR (
        ask >= 0 AND ask < 100000000000000000000000000 AND ask = round(ask, 12)
    )),
    CHECK (ask_size IS NULL OR (
        ask_size >= 0 AND ask_size < 100000000000000000000000000 AND
        ask_size = round(ask_size, 12)
    )),
    CHECK (last IS NULL OR (
        last >= 0 AND last < 100000000000000000000000000 AND last = round(last, 12)
    )),
    CHECK (mark IS NULL OR (
        mark >= 0 AND mark < 100000000000000000000000000 AND mark = round(mark, 12)
    ))
);

CREATE INDEX idx_quote_snapshots_point_in_time
    ON quote_snapshots (
        instrument_id, provider, venue, observation_namespace,
        available_at DESC, source_sequence DESC NULLS LAST, ingest_sequence DESC
    )
    WHERE available_at IS NOT NULL;

CREATE INDEX idx_quote_snapshots_venue_contract
    ON quote_snapshots (venue_contract_id, exchange_at, id)
    WHERE venue_contract_id IS NOT NULL;

CREATE TABLE quote_depth_levels (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quote_snapshot_id UUID NOT NULL REFERENCES quote_snapshots(id) ON DELETE RESTRICT,
    side              TEXT NOT NULL CHECK (side IN ('bid', 'ask')),
    level_index       INTEGER NOT NULL CHECK (level_index BETWEEN 0 AND 999),
    price             NUMERIC NOT NULL CHECK (
                          price >= 0 AND
                          price < 100000000000000000000000000 AND
                          price = round(price, 12)
                      ),
    size              NUMERIC NOT NULL CHECK (
                          size >= 0 AND
                          size < 100000000000000000000000000 AND
                          size = round(size, 12)
                      ),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (quote_snapshot_id, side, level_index)
);

CREATE INDEX idx_quote_depth_levels_snapshot
    ON quote_depth_levels (quote_snapshot_id, side, level_index);

CREATE FUNCTION reject_quote_snapshot_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_quote_snapshot_venue_contract() RETURNS TRIGGER AS $$
DECLARE
    contract_instrument_id UUID;
    contract_venue TEXT;
    contract_valid_from TIMESTAMPTZ;
    contract_valid_to TIMESTAMPTZ;
    observation_time TIMESTAMPTZ;
BEGIN
    IF NEW.venue_contract_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT instrument_id, venue, valid_from, valid_to
    INTO contract_instrument_id, contract_venue, contract_valid_from, contract_valid_to
    FROM venue_contracts
    WHERE id = NEW.venue_contract_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'quote snapshot venue contract % does not exist', NEW.venue_contract_id;
    END IF;
    IF contract_instrument_id <> NEW.instrument_id THEN
        RAISE EXCEPTION 'quote snapshot venue contract belongs to a different instrument';
    END IF;
    IF contract_venue <> NEW.venue THEN
        RAISE EXCEPTION 'quote snapshot venue contract belongs to a different venue';
    END IF;

    observation_time := COALESCE(NEW.exchange_at, NEW.received_at);
    IF contract_valid_from > observation_time OR
       (contract_valid_to IS NOT NULL AND observation_time >= contract_valid_to) THEN
        RAISE EXCEPTION 'quote snapshot venue contract is not valid at observation time';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION assert_quote_snapshot_depth(target_snapshot_id UUID) RETURNS VOID AS $$
DECLARE
    expected_bid_count INTEGER;
    expected_ask_count INTEGER;
    actual_bid_count BIGINT;
    actual_ask_count BIGINT;
    min_bid_index INTEGER;
    max_bid_index INTEGER;
    min_ask_index INTEGER;
    max_ask_index INTEGER;
    quoted_bid NUMERIC;
    quoted_bid_size NUMERIC;
    quoted_ask NUMERIC;
    quoted_ask_size NUMERIC;
    depth_bid NUMERIC;
    depth_bid_size NUMERIC;
    depth_ask NUMERIC;
    depth_ask_size NUMERIC;
BEGIN
    SELECT bid_depth_count, ask_depth_count, bid, bid_size, ask, ask_size
    INTO expected_bid_count, expected_ask_count,
         quoted_bid, quoted_bid_size, quoted_ask, quoted_ask_size
    FROM quote_snapshots
    WHERE id = target_snapshot_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'quote snapshot % does not exist', target_snapshot_id;
    END IF;

    SELECT
        COUNT(*) FILTER (WHERE side = 'bid'),
        COUNT(*) FILTER (WHERE side = 'ask'),
        MIN(level_index) FILTER (WHERE side = 'bid'),
        MAX(level_index) FILTER (WHERE side = 'bid'),
        MIN(level_index) FILTER (WHERE side = 'ask'),
        MAX(level_index) FILTER (WHERE side = 'ask')
    INTO actual_bid_count, actual_ask_count,
         min_bid_index, max_bid_index, min_ask_index, max_ask_index
    FROM quote_depth_levels
    WHERE quote_snapshot_id = target_snapshot_id;

    IF actual_bid_count <> expected_bid_count OR actual_ask_count <> expected_ask_count THEN
        RAISE EXCEPTION 'quote snapshot % depth count is bid %/% ask %/%',
            target_snapshot_id, actual_bid_count, expected_bid_count,
            actual_ask_count, expected_ask_count;
    END IF;
    IF actual_bid_count > 0 AND
       (min_bid_index <> 0 OR max_bid_index <> actual_bid_count - 1) THEN
        RAISE EXCEPTION 'quote snapshot % bid depth indexes are not contiguous', target_snapshot_id;
    END IF;
    IF actual_ask_count > 0 AND
       (min_ask_index <> 0 OR max_ask_index <> actual_ask_count - 1) THEN
        RAISE EXCEPTION 'quote snapshot % ask depth indexes are not contiguous', target_snapshot_id;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM quote_depth_levels AS current_level
        JOIN quote_depth_levels AS next_level
          ON next_level.quote_snapshot_id = current_level.quote_snapshot_id
         AND next_level.side = current_level.side
         AND next_level.level_index = current_level.level_index + 1
        WHERE current_level.quote_snapshot_id = target_snapshot_id
          AND current_level.side = 'bid'
          AND current_level.price <= next_level.price
    ) THEN
        RAISE EXCEPTION 'quote snapshot % bid depth must strictly descend', target_snapshot_id;
    END IF;
    IF EXISTS (
        SELECT 1
        FROM quote_depth_levels AS current_level
        JOIN quote_depth_levels AS next_level
          ON next_level.quote_snapshot_id = current_level.quote_snapshot_id
         AND next_level.side = current_level.side
         AND next_level.level_index = current_level.level_index + 1
        WHERE current_level.quote_snapshot_id = target_snapshot_id
          AND current_level.side = 'ask'
          AND current_level.price >= next_level.price
    ) THEN
        RAISE EXCEPTION 'quote snapshot % ask depth must strictly ascend', target_snapshot_id;
    END IF;

    SELECT price, size INTO depth_bid, depth_bid_size
    FROM quote_depth_levels
    WHERE quote_snapshot_id = target_snapshot_id AND side = 'bid' AND level_index = 0;
    SELECT price, size INTO depth_ask, depth_ask_size
    FROM quote_depth_levels
    WHERE quote_snapshot_id = target_snapshot_id AND side = 'ask' AND level_index = 0;

    IF actual_bid_count > 0 AND actual_ask_count > 0 AND depth_bid > depth_ask THEN
        RAISE EXCEPTION 'quote snapshot % depth bid exceeds depth ask', target_snapshot_id;
    END IF;
    IF actual_bid_count > 0 AND quoted_bid IS NOT NULL AND depth_bid <> quoted_bid THEN
        RAISE EXCEPTION 'quote snapshot % bid depth top does not match bid', target_snapshot_id;
    END IF;
    IF actual_bid_count > 0 AND quoted_bid_size IS NOT NULL AND depth_bid_size <> quoted_bid_size THEN
        RAISE EXCEPTION 'quote snapshot % bid depth top size does not match bid size', target_snapshot_id;
    END IF;
    IF actual_ask_count > 0 AND quoted_ask IS NOT NULL AND depth_ask <> quoted_ask THEN
        RAISE EXCEPTION 'quote snapshot % ask depth top does not match ask', target_snapshot_id;
    END IF;
    IF actual_ask_count > 0 AND quoted_ask_size IS NOT NULL AND depth_ask_size <> quoted_ask_size THEN
        RAISE EXCEPTION 'quote snapshot % ask depth top size does not match ask size', target_snapshot_id;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_quote_snapshot_depth_row() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_quote_snapshot_depth(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_quote_depth_level_row() RETURNS TRIGGER AS $$
BEGIN
    PERFORM assert_quote_snapshot_depth(NEW.quote_snapshot_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_quote_snapshots_venue_contract
    BEFORE INSERT ON quote_snapshots
    FOR EACH ROW EXECUTE FUNCTION validate_quote_snapshot_venue_contract();

CREATE TRIGGER trg_quote_snapshots_immutable
    BEFORE UPDATE OR DELETE ON quote_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_quote_snapshot_mutation();

CREATE TRIGGER trg_quote_depth_levels_immutable
    BEFORE UPDATE OR DELETE ON quote_depth_levels
    FOR EACH ROW EXECUTE FUNCTION reject_quote_snapshot_mutation();

CREATE CONSTRAINT TRIGGER trg_quote_snapshots_depth_consistent
    AFTER INSERT ON quote_snapshots
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_quote_snapshot_depth_row();

CREATE CONSTRAINT TRIGGER trg_quote_depth_levels_depth_consistent
    AFTER INSERT ON quote_depth_levels
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION validate_quote_depth_level_row();
