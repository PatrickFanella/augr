CREATE TABLE complete_set_candidates (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK(schema_name='complete-set-arbitrage-v1'),
    state TEXT NOT NULL CHECK(state IN ('qualified','rejected')),
    reason TEXT NOT NULL CHECK((state='qualified' AND reason='') OR (state='rejected' AND reason IN ('incomplete_set','invalid_replay','insufficient_capital','nonpositive_complete_set_profit','orphan_guard_failure'))),
    recorder_id UUID NOT NULL REFERENCES prediction_book_fee_recorders(id),
    recorder_sha256 TEXT NOT NULL CHECK(recorder_sha256 ~ '^[0-9a-f]{64}$'),
    candidate_key TEXT NOT NULL CHECK(candidate_key<>''), market_id TEXT NOT NULL CHECK(market_id<>''),
    outcome_count INT NOT NULL CHECK(outcome_count BETWEEN 2 AND 12), binding_count INT NOT NULL CHECK(binding_count>=0),
    leg_count INT NOT NULL CHECK(leg_count>=0), scenario_count INT NOT NULL CHECK(scenario_count>=0), scenario_leg_count INT NOT NULL CHECK(scenario_leg_count>=0),
    set_quantity NUMERIC NOT NULL CHECK(set_quantity>0), payout_per_set NUMERIC NOT NULL CHECK(payout_per_set>0),
    available_capital NUMERIC NOT NULL CHECK(available_capital>=0), minimum_profit NUMERIC NOT NULL CHECK(minimum_profit>=0),
    entry_cost NUMERIC NOT NULL CHECK(entry_cost>=0), payout NUMERIC NOT NULL CHECK(payout=set_quantity*payout_per_set),
    after_cost_profit NUMERIC NOT NULL CHECK(after_cost_profit=payout-entry_cost),
    worst_orphan_key TEXT NOT NULL, worst_orphan_loss NUMERIC NOT NULL CHECK(worst_orphan_loss>=0),
    reserved_capital NUMERIC NOT NULL CHECK(reserved_capital=entry_cost+worst_orphan_loss),
    profit_after_orphan_guard NUMERIC NOT NULL CHECK(profit_after_orphan_guard=after_cost_profit-worst_orphan_loss),
    sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'state'=state AND canonical_json->>'reason'=reason),
    UNIQUE(recorder_id,candidate_key)
);

CREATE TABLE complete_set_bindings (
    candidate_id UUID NOT NULL REFERENCES complete_set_candidates(id), sequence INT NOT NULL CHECK(sequence>=0),
    outcome_id UUID NOT NULL, entry_sequence INT NOT NULL CHECK(entry_sequence>=0), unwind_sequence INT NOT NULL CHECK(unwind_sequence>=0 AND unwind_sequence<>entry_sequence),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(candidate_id,sequence), UNIQUE(candidate_id,outcome_id), UNIQUE(candidate_id,entry_sequence), UNIQUE(candidate_id,unwind_sequence)
);

CREATE TABLE complete_set_legs (
    candidate_id UUID NOT NULL REFERENCES complete_set_candidates(id), sequence INT NOT NULL CHECK(sequence>=0),
    outcome_id UUID NOT NULL, entry_sequence INT NOT NULL, unwind_sequence INT NOT NULL,
    entry_cost NUMERIC NOT NULL CHECK(entry_cost>=0), unwind_proceeds NUMERIC NOT NULL CHECK(unwind_proceeds>=0),
    orphan_loss NUMERIC NOT NULL CHECK(orphan_loss=GREATEST(entry_cost-unwind_proceeds,0)),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(candidate_id,sequence), UNIQUE(candidate_id,outcome_id),
    FOREIGN KEY(candidate_id,outcome_id) REFERENCES complete_set_bindings(candidate_id,outcome_id)
);

CREATE TABLE complete_set_orphan_scenarios (
    candidate_id UUID NOT NULL REFERENCES complete_set_candidates(id), sequence INT NOT NULL CHECK(sequence>=0),
    scenario_key TEXT NOT NULL CHECK(scenario_key<>''), entry_cost NUMERIC NOT NULL CHECK(entry_cost>=0),
    unwind_proceeds NUMERIC NOT NULL CHECK(unwind_proceeds>=0), loss NUMERIC NOT NULL CHECK(loss=GREATEST(entry_cost-unwind_proceeds,0)),
    leg_count INT NOT NULL CHECK(leg_count>0), canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(candidate_id,sequence), UNIQUE(candidate_id,scenario_key)
);

CREATE TABLE complete_set_orphan_scenario_legs (
    candidate_id UUID NOT NULL, scenario_sequence INT NOT NULL, sequence INT NOT NULL CHECK(sequence>=0), outcome_id UUID NOT NULL,
    entry_cost NUMERIC NOT NULL CHECK(entry_cost>=0), unwind_proceeds NUMERIC NOT NULL CHECK(unwind_proceeds>=0),
    loss NUMERIC NOT NULL CHECK(loss=GREATEST(entry_cost-unwind_proceeds,0)), canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(candidate_id,scenario_sequence,sequence), UNIQUE(candidate_id,scenario_sequence,outcome_id),
    FOREIGN KEY(candidate_id,scenario_sequence) REFERENCES complete_set_orphan_scenarios(candidate_id,sequence),
    FOREIGN KEY(candidate_id,outcome_id) REFERENCES complete_set_legs(candidate_id,outcome_id)
);

CREATE FUNCTION validate_complete_set_parent() RETURNS TRIGGER AS $$
DECLARE recorder prediction_book_fee_recorders%ROWTYPE;
BEGIN
    SELECT * INTO recorder FROM prediction_book_fee_recorders WHERE id=NEW.recorder_id;
    IF recorder.id IS NULL OR recorder.sha256<>NEW.recorder_sha256 OR
       NEW.candidate_key<>NEW.canonical_json->>'candidate_key' OR NEW.market_id<>NEW.canonical_json->>'market_id' OR
       NEW.outcome_count<>jsonb_array_length(NEW.canonical_json->'outcomes') OR NEW.binding_count<>jsonb_array_length(NEW.canonical_json->'bindings') OR
       NEW.leg_count<>jsonb_array_length(NEW.canonical_json->'legs') OR NEW.scenario_count<>jsonb_array_length(NEW.canonical_json->'scenarios') OR
       trim_scale(NEW.set_quantity)::TEXT<>NEW.canonical_json->>'set_quantity' OR trim_scale(NEW.payout)::TEXT<>NEW.canonical_json->>'payout' OR
       trim_scale(NEW.entry_cost)::TEXT<>NEW.canonical_json->>'entry_cost' OR trim_scale(NEW.worst_orphan_loss)::TEXT<>NEW.canonical_json->>'worst_orphan_loss' THEN
        RAISE EXCEPTION 'complete set parent does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER complete_set_parent_guard BEFORE INSERT ON complete_set_candidates FOR EACH ROW EXECUTE FUNCTION validate_complete_set_parent();

CREATE FUNCTION validate_complete_set_row() RETURNS TRIGGER AS $$
DECLARE parent complete_set_candidates%ROWTYPE; expected JSONB; array_name TEXT;
BEGIN
    SELECT * INTO parent FROM complete_set_candidates WHERE id=NEW.candidate_id;
    array_name := CASE TG_TABLE_NAME WHEN 'complete_set_bindings' THEN 'bindings' WHEN 'complete_set_legs' THEN 'legs' ELSE 'scenarios' END;
    expected := parent.canonical_json->array_name->NEW.sequence;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN RAISE EXCEPTION 'complete set normalized row does not reconstruct'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER complete_set_bindings_guard BEFORE INSERT ON complete_set_bindings FOR EACH ROW EXECUTE FUNCTION validate_complete_set_row();
CREATE TRIGGER complete_set_legs_guard BEFORE INSERT ON complete_set_legs FOR EACH ROW EXECUTE FUNCTION validate_complete_set_row();
CREATE TRIGGER complete_set_scenarios_guard BEFORE INSERT ON complete_set_orphan_scenarios FOR EACH ROW EXECUTE FUNCTION validate_complete_set_row();

CREATE FUNCTION validate_complete_set_scenario_leg() RETURNS TRIGGER AS $$
DECLARE parent complete_set_candidates%ROWTYPE; expected JSONB;
BEGIN
    SELECT * INTO parent FROM complete_set_candidates WHERE id=NEW.candidate_id;
    expected := parent.canonical_json->'scenarios'->NEW.scenario_sequence->'legs'->NEW.sequence;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN RAISE EXCEPTION 'complete set scenario leg does not reconstruct'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER complete_set_scenario_legs_guard BEFORE INSERT ON complete_set_orphan_scenario_legs FOR EACH ROW EXECUTE FUNCTION validate_complete_set_scenario_leg();

CREATE FUNCTION validate_complete_set_graph() RETURNS TRIGGER AS $$
BEGIN
    IF (SELECT count(*) FROM complete_set_bindings WHERE candidate_id=NEW.id)<>NEW.binding_count OR
       (SELECT count(*) FROM complete_set_legs WHERE candidate_id=NEW.id)<>NEW.leg_count OR
       (SELECT count(*) FROM complete_set_orphan_scenarios WHERE candidate_id=NEW.id)<>NEW.scenario_count OR
       (SELECT count(*) FROM complete_set_orphan_scenario_legs WHERE candidate_id=NEW.id)<>NEW.scenario_leg_count THEN
        RAISE EXCEPTION 'complete set graph does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER complete_set_graph_guard AFTER INSERT ON complete_set_candidates DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_complete_set_graph();

CREATE FUNCTION reject_complete_set_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'complete set evidence is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER complete_set_candidates_append_only BEFORE UPDATE OR DELETE ON complete_set_candidates FOR EACH ROW EXECUTE FUNCTION reject_complete_set_mutation();
CREATE TRIGGER complete_set_bindings_append_only BEFORE UPDATE OR DELETE ON complete_set_bindings FOR EACH ROW EXECUTE FUNCTION reject_complete_set_mutation();
CREATE TRIGGER complete_set_legs_append_only BEFORE UPDATE OR DELETE ON complete_set_legs FOR EACH ROW EXECUTE FUNCTION reject_complete_set_mutation();
CREATE TRIGGER complete_set_scenarios_append_only BEFORE UPDATE OR DELETE ON complete_set_orphan_scenarios FOR EACH ROW EXECUTE FUNCTION reject_complete_set_mutation();
CREATE TRIGGER complete_set_scenario_legs_append_only BEFORE UPDATE OR DELETE ON complete_set_orphan_scenario_legs FOR EACH ROW EXECUTE FUNCTION reject_complete_set_mutation();
