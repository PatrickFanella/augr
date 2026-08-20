CREATE TABLE maker_quote_candidates (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK(schema_name='maker-quote-evaluation-v1'),
    state TEXT NOT NULL CHECK(state IN ('qualified','rejected')),
    reason TEXT NOT NULL CHECK((state='qualified' AND reason='') OR (state='rejected' AND reason IN ('invalid_quote','invalid_scenarios','no_fill','inventory_limit','nonpositive_net_capture'))),
    recorder_id UUID NOT NULL REFERENCES prediction_book_fee_recorders(id),
    recorder_sha256 TEXT NOT NULL CHECK(recorder_sha256 ~ '^[0-9a-f]{64}$'),
    candidate_key TEXT NOT NULL CHECK(candidate_key<>''), market_id TEXT NOT NULL CHECK(market_id<>''), outcome_id UUID NOT NULL,
    side TEXT NOT NULL CHECK(side IN ('buy','sell')), decision_at TIMESTAMPTZ NOT NULL,
    quote_book_source_key TEXT NOT NULL, venue TEXT NOT NULL, quote_price NUMERIC NOT NULL CHECK(quote_price>0 AND quote_price<1),
    quote_quantity NUMERIC NOT NULL CHECK(quote_quantity>0), displayed_queue NUMERIC NOT NULL CHECK(displayed_queue>=0),
    prior_queue NUMERIC NOT NULL CHECK(prior_queue>=0), queue_ahead NUMERIC NOT NULL CHECK(queue_ahead=displayed_queue+prior_queue),
    starting_inventory NUMERIC NOT NULL, inventory_limit NUMERIC NOT NULL CHECK(inventory_limit>0),
    hourly_inventory_cost_rate NUMERIC NOT NULL CHECK(hourly_inventory_cost_rate>=0), minimum_expected_net NUMERIC NOT NULL CHECK(minimum_expected_net>=0),
    scenario_count INT NOT NULL CHECK(scenario_count>=0), filled_scenario_count INT NOT NULL CHECK(filled_scenario_count>=0 AND filled_scenario_count<=scenario_count),
    expected_gross_capture NUMERIC NOT NULL, expected_maker_fee NUMERIC NOT NULL CHECK(expected_maker_fee>=0),
    expected_inventory_cost NUMERIC NOT NULL CHECK(expected_inventory_cost>=0), expected_net_capture NUMERIC NOT NULL,
    sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'state'=state AND canonical_json->>'reason'=reason),
    UNIQUE(recorder_id,candidate_key)
);

CREATE TABLE maker_quote_scenarios (
    candidate_id UUID NOT NULL REFERENCES maker_quote_candidates(id), sequence INT NOT NULL CHECK(sequence>=0),
    scenario_key TEXT NOT NULL CHECK(scenario_key<>''), weight NUMERIC NOT NULL CHECK(weight>0), horizon_at TIMESTAMPTZ NOT NULL,
    queue_outflow NUMERIC NOT NULL CHECK(queue_outflow>=0), mark_book_source_key TEXT NOT NULL, mark_price NUMERIC NOT NULL CHECK(mark_price>0 AND mark_price<1),
    filled_quantity NUMERIC NOT NULL CHECK(filled_quantity>=0), residual_quantity NUMERIC NOT NULL CHECK(residual_quantity>=0),
    post_fill_inventory NUMERIC NOT NULL, gross_capture NUMERIC NOT NULL, maker_fee NUMERIC NOT NULL CHECK(maker_fee>=0),
    inventory_cost NUMERIC NOT NULL CHECK(inventory_cost>=0), net_capture NUMERIC NOT NULL CHECK(net_capture=gross_capture-maker_fee-inventory_cost),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(candidate_id,sequence), UNIQUE(candidate_id,scenario_key)
);

CREATE FUNCTION validate_maker_quote_parent() RETURNS TRIGGER AS $$
DECLARE recorder prediction_book_fee_recorders%ROWTYPE;
BEGIN
    SELECT * INTO recorder FROM prediction_book_fee_recorders WHERE id=NEW.recorder_id;
    IF recorder.id IS NULL OR recorder.sha256<>NEW.recorder_sha256 OR
       NEW.candidate_key<>NEW.canonical_json->>'candidate_key' OR NEW.market_id<>NEW.canonical_json->>'market_id' OR
       NEW.outcome_id::TEXT<>NEW.canonical_json->>'outcome_id' OR NEW.side<>NEW.canonical_json->>'side' OR
       NEW.scenario_count<>jsonb_array_length(NEW.canonical_json->'scenarios') OR
       trim_scale(NEW.quote_price)::TEXT<>NEW.canonical_json->>'quote_price' OR
       trim_scale(NEW.queue_ahead)::TEXT<>NEW.canonical_json->>'queue_ahead' OR
       trim_scale(NEW.expected_net_capture)::TEXT<>NEW.canonical_json->>'expected_net_capture' THEN
        RAISE EXCEPTION 'maker quote parent does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER maker_quote_parent_guard BEFORE INSERT ON maker_quote_candidates FOR EACH ROW EXECUTE FUNCTION validate_maker_quote_parent();

CREATE FUNCTION validate_maker_quote_scenario() RETURNS TRIGGER AS $$
DECLARE parent maker_quote_candidates%ROWTYPE; expected JSONB;
BEGIN
    SELECT * INTO parent FROM maker_quote_candidates WHERE id=NEW.candidate_id;
    expected := parent.canonical_json->'scenarios'->NEW.sequence;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN RAISE EXCEPTION 'maker quote scenario does not reconstruct'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER maker_quote_scenario_guard BEFORE INSERT ON maker_quote_scenarios FOR EACH ROW EXECUTE FUNCTION validate_maker_quote_scenario();

CREATE FUNCTION validate_maker_quote_graph() RETURNS TRIGGER AS $$
BEGIN
    IF (SELECT count(*) FROM maker_quote_scenarios WHERE candidate_id=NEW.id)<>NEW.scenario_count THEN
        RAISE EXCEPTION 'maker quote graph does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER maker_quote_graph_guard AFTER INSERT ON maker_quote_candidates DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_maker_quote_graph();

CREATE FUNCTION reject_maker_quote_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'maker quote evidence is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER maker_quote_candidates_append_only BEFORE UPDATE OR DELETE ON maker_quote_candidates FOR EACH ROW EXECUTE FUNCTION reject_maker_quote_mutation();
CREATE TRIGGER maker_quote_scenarios_append_only BEFORE UPDATE OR DELETE ON maker_quote_scenarios FOR EACH ROW EXECUTE FUNCTION reject_maker_quote_mutation();
