CREATE TABLE prediction_book_fee_recorders (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK(schema_name='prediction-book-fee-recorder-v1'),
    state TEXT NOT NULL CHECK(state='completed'),
    manifest_id UUID NOT NULL REFERENCES dataset_manifests(id),
    manifest_sha256 TEXT NOT NULL CHECK(manifest_sha256 ~ '^[0-9a-f]{64}$'),
    manifest_cutoff TIMESTAMPTZ NOT NULL,
    book_count INT NOT NULL CHECK(book_count>0),
    level_count INT NOT NULL CHECK(level_count>0),
    fee_count INT NOT NULL CHECK(fee_count>0),
    replay_count INT NOT NULL CHECK(replay_count>0),
    fill_count INT NOT NULL CHECK(fill_count>=0),
    sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK(octet_length(canonical_bytes)>0),
    canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'state'=state),
    CHECK(canonical_json->>'manifest_id'=manifest_id::TEXT AND canonical_json->>'manifest_sha256'=manifest_sha256),
    UNIQUE(manifest_id)
);

CREATE TABLE prediction_recorded_books (
    recorder_id UUID NOT NULL REFERENCES prediction_book_fee_recorders(id), sequence INT NOT NULL CHECK(sequence>=0),
    market_id TEXT NOT NULL CHECK(market_id<>''), outcome_id UUID NOT NULL, venue TEXT NOT NULL CHECK(venue<>''),
    partition_content_sha256 TEXT NOT NULL CHECK(partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    source_key TEXT NOT NULL CHECK(source_key<>''), content_sha256 TEXT NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
    exchange_at TIMESTAMPTZ NOT NULL, available_at TIMESTAMPTZ NOT NULL CHECK(exchange_at<=available_at),
    revision INT NOT NULL CHECK(revision>=0), correction_of TEXT NOT NULL,
    level_count INT NOT NULL CHECK(level_count>=2), canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(recorder_id,sequence), UNIQUE(recorder_id,source_key),
    UNIQUE(recorder_id,outcome_id,exchange_at,revision)
);

CREATE TABLE prediction_recorded_book_levels (
    recorder_id UUID NOT NULL, book_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0),
    side TEXT NOT NULL CHECK(side IN ('bid','ask')), level INT NOT NULL CHECK(level>=0),
    price NUMERIC NOT NULL CHECK(price>0 AND price<1), size NUMERIC NOT NULL CHECK(size>0),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(recorder_id,book_sequence,sequence), UNIQUE(recorder_id,book_sequence,side,level),
    FOREIGN KEY(recorder_id,book_sequence) REFERENCES prediction_recorded_books(recorder_id,sequence)
);

CREATE TABLE prediction_recorded_fee_policies (
    recorder_id UUID NOT NULL REFERENCES prediction_book_fee_recorders(id), sequence INT NOT NULL CHECK(sequence>=0),
    instrument_id UUID NOT NULL, venue TEXT NOT NULL CHECK(venue<>''), role TEXT NOT NULL CHECK(role IN ('maker','taker')),
    partition_content_sha256 TEXT NOT NULL CHECK(partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    source_key TEXT NOT NULL CHECK(source_key<>''), content_sha256 TEXT NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
    available_at TIMESTAMPTZ NOT NULL, effective_from TIMESTAMPTZ NOT NULL, effective_to TIMESTAMPTZ,
    formula TEXT NOT NULL CHECK(formula IN ('notional_bps','contract_curve')), rate NUMERIC NOT NULL CHECK(rate>=0),
    scale INT NOT NULL CHECK(scale BETWEEN 0 AND 12), rounding TEXT NOT NULL CHECK(rounding IN ('half_up','ceiling')),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(recorder_id,sequence), UNIQUE(recorder_id,source_key),
    CHECK(effective_to IS NULL OR effective_from<effective_to)
);

CREATE TABLE prediction_recorded_replays (
    recorder_id UUID NOT NULL REFERENCES prediction_book_fee_recorders(id), sequence INT NOT NULL CHECK(sequence>=0),
    decision_at TIMESTAMPTZ NOT NULL, market_id TEXT NOT NULL, outcome_id UUID NOT NULL,
    side TEXT NOT NULL CHECK(side IN ('buy','sell')), role TEXT NOT NULL CHECK(role IN ('maker','taker')),
    quantity NUMERIC NOT NULL CHECK(quantity>0), limit_price NUMERIC NOT NULL CHECK(limit_price>0 AND limit_price<1),
    status TEXT NOT NULL CHECK(status IN ('no_book','no_fee_policy','limit_blocked','partial','filled')),
    book_source_key TEXT NOT NULL, fee_source_key TEXT NOT NULL,
    filled_quantity NUMERIC NOT NULL CHECK(filled_quantity>=0), residual_quantity NUMERIC NOT NULL CHECK(residual_quantity>=0),
    weighted_price NUMERIC NOT NULL CHECK(weighted_price>=0), gross_cash NUMERIC NOT NULL CHECK(gross_cash>=0),
    fee NUMERIC NOT NULL CHECK(fee>=0), net_cash NUMERIC NOT NULL, fill_count INT NOT NULL CHECK(fill_count>=0),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(recorder_id,sequence),
    CHECK(filled_quantity+residual_quantity=quantity),
    CHECK((filled_quantity=0 AND weighted_price=0 AND gross_cash=0 AND fee=0 AND net_cash=0 AND fill_count=0) OR
          (filled_quantity>0 AND weighted_price=gross_cash/filled_quantity AND fill_count>0)),
    CHECK((side='buy' AND net_cash=gross_cash+fee) OR (side='sell' AND net_cash=gross_cash-fee))
);

CREATE TABLE prediction_recorded_fills (
    recorder_id UUID NOT NULL, replay_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0),
    book_level INT NOT NULL CHECK(book_level>=0), price NUMERIC NOT NULL CHECK(price>0 AND price<1),
    quantity NUMERIC NOT NULL CHECK(quantity>0), gross NUMERIC NOT NULL CHECK(gross=price*quantity),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(recorder_id,replay_sequence,sequence),
    FOREIGN KEY(recorder_id,replay_sequence) REFERENCES prediction_recorded_replays(recorder_id,sequence)
);

CREATE FUNCTION validate_prediction_recorder_parent() RETURNS TRIGGER AS $$
DECLARE manifest dataset_manifests%ROWTYPE;
BEGIN
    SELECT * INTO manifest FROM dataset_manifests WHERE id=NEW.manifest_id;
    IF manifest.id IS NULL OR manifest.sha256<>NEW.manifest_sha256 OR manifest.decision_cutoff<>NEW.manifest_cutoff OR
       NEW.manifest_cutoff<>(NEW.canonical_json->>'manifest_cutoff')::TIMESTAMPTZ OR
       NEW.book_count<>jsonb_array_length(NEW.canonical_json->'books') OR
       NEW.fee_count<>jsonb_array_length(NEW.canonical_json->'fees') OR
       NEW.replay_count<>jsonb_array_length(NEW.canonical_json->'replays') THEN
        RAISE EXCEPTION 'prediction recorder parent does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER prediction_recorder_parent_guard BEFORE INSERT ON prediction_book_fee_recorders FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_parent();

CREATE FUNCTION validate_prediction_recorder_evidence() RETURNS TRIGGER AS $$
DECLARE parent prediction_book_fee_recorders%ROWTYPE; expected_kind TEXT; expected_instrument UUID;
BEGIN
    SELECT * INTO parent FROM prediction_book_fee_recorders WHERE id=NEW.recorder_id;
    expected_kind := CASE TG_TABLE_NAME WHEN 'prediction_recorded_books' THEN 'prediction_books' ELSE 'prediction_fees' END;
    expected_instrument := COALESCE(
        (to_jsonb(NEW)->>'outcome_id')::UUID,
        (to_jsonb(NEW)->>'instrument_id')::UUID
    );
    IF NOT EXISTS (
        SELECT 1 FROM dataset_manifest_observations observation
        JOIN dataset_manifest_partitions partition ON partition.manifest_id=observation.manifest_id AND partition.sequence=observation.partition_sequence
         WHERE observation.manifest_id=parent.manifest_id AND partition.kind=expected_kind
           AND observation.partition_content_sha256=NEW.partition_content_sha256
           AND observation.source_key=NEW.source_key AND observation.content_sha256=NEW.content_sha256
           AND observation.available_at=NEW.available_at AND observation.instrument_id=expected_instrument
    ) THEN RAISE EXCEPTION 'prediction recorder evidence is absent from manifest'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER prediction_books_evidence_guard BEFORE INSERT ON prediction_recorded_books FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_evidence();
CREATE TRIGGER prediction_fees_evidence_guard BEFORE INSERT ON prediction_recorded_fee_policies FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_evidence();

CREATE FUNCTION validate_prediction_recorder_row() RETURNS TRIGGER AS $$
DECLARE parent prediction_book_fee_recorders%ROWTYPE; expected JSONB; array_name TEXT;
BEGIN
    SELECT * INTO parent FROM prediction_book_fee_recorders WHERE id=NEW.recorder_id;
    array_name := CASE TG_TABLE_NAME WHEN 'prediction_recorded_books' THEN 'books' WHEN 'prediction_recorded_fee_policies' THEN 'fees' ELSE 'replays' END;
    expected := parent.canonical_json->array_name->NEW.sequence;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN RAISE EXCEPTION 'prediction recorder normalized row does not reconstruct'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER prediction_books_row_guard BEFORE INSERT ON prediction_recorded_books FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_row();
CREATE TRIGGER prediction_fees_row_guard BEFORE INSERT ON prediction_recorded_fee_policies FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_row();
CREATE TRIGGER prediction_replays_row_guard BEFORE INSERT ON prediction_recorded_replays FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_row();

CREATE FUNCTION validate_prediction_recorder_nested_row() RETURNS TRIGGER AS $$
DECLARE parent prediction_book_fee_recorders%ROWTYPE; expected JSONB;
BEGIN
    SELECT * INTO parent FROM prediction_book_fee_recorders WHERE id=NEW.recorder_id;
    IF TG_TABLE_NAME='prediction_recorded_book_levels' THEN expected := parent.canonical_json->'books'->NEW.book_sequence->'levels'->NEW.sequence;
    ELSE expected := parent.canonical_json->'replays'->NEW.replay_sequence->'fills'->NEW.sequence; END IF;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN RAISE EXCEPTION 'prediction recorder nested row does not reconstruct'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER prediction_levels_row_guard BEFORE INSERT ON prediction_recorded_book_levels FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_nested_row();
CREATE TRIGGER prediction_fills_row_guard BEFORE INSERT ON prediction_recorded_fills FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_nested_row();

CREATE FUNCTION validate_prediction_recorder_graph() RETURNS TRIGGER AS $$
BEGIN
    IF (SELECT count(*) FROM prediction_recorded_books WHERE recorder_id=NEW.id)<>NEW.book_count OR
       (SELECT count(*) FROM prediction_recorded_book_levels WHERE recorder_id=NEW.id)<>NEW.level_count OR
       (SELECT count(*) FROM prediction_recorded_fee_policies WHERE recorder_id=NEW.id)<>NEW.fee_count OR
       (SELECT count(*) FROM prediction_recorded_replays WHERE recorder_id=NEW.id)<>NEW.replay_count OR
       (SELECT count(*) FROM prediction_recorded_fills WHERE recorder_id=NEW.id)<>NEW.fill_count THEN
        RAISE EXCEPTION 'prediction recorder graph does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER prediction_recorder_graph_guard AFTER INSERT ON prediction_book_fee_recorders DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_prediction_recorder_graph();

CREATE FUNCTION reject_prediction_recorder_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'prediction recorder evidence is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER prediction_recorders_append_only BEFORE UPDATE OR DELETE ON prediction_book_fee_recorders FOR EACH ROW EXECUTE FUNCTION reject_prediction_recorder_mutation();
CREATE TRIGGER prediction_books_append_only BEFORE UPDATE OR DELETE ON prediction_recorded_books FOR EACH ROW EXECUTE FUNCTION reject_prediction_recorder_mutation();
CREATE TRIGGER prediction_levels_append_only BEFORE UPDATE OR DELETE ON prediction_recorded_book_levels FOR EACH ROW EXECUTE FUNCTION reject_prediction_recorder_mutation();
CREATE TRIGGER prediction_fees_append_only BEFORE UPDATE OR DELETE ON prediction_recorded_fee_policies FOR EACH ROW EXECUTE FUNCTION reject_prediction_recorder_mutation();
CREATE TRIGGER prediction_replays_append_only BEFORE UPDATE OR DELETE ON prediction_recorded_replays FOR EACH ROW EXECUTE FUNCTION reject_prediction_recorder_mutation();
CREATE TRIGGER prediction_fills_append_only BEFORE UPDATE OR DELETE ON prediction_recorded_fills FOR EACH ROW EXECUTE FUNCTION reject_prediction_recorder_mutation();
