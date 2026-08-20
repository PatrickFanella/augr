CREATE TABLE generated_strategy_specs (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK(schema_name='typed-generative-strategy-spec-v1'),
    family_id UUID NOT NULL REFERENCES strategy_families(id),
    family_sha256 TEXT NOT NULL CHECK(family_sha256 ~ '^[0-9a-f]{64}$'),
    spec_key TEXT NOT NULL CHECK(spec_key ~ '^[a-z][a-z0-9_]{0,63}$'),
    input_count INT NOT NULL CHECK(input_count>0), instrument_count INT NOT NULL CHECK(instrument_count>0),
    prohibition_count INT NOT NULL CHECK(prohibition_count>=7), property_count INT NOT NULL CHECK(property_count>=5),
    example_count INT NOT NULL CHECK(example_count>0), normalized_row_count INT NOT NULL CHECK(normalized_row_count=input_count+instrument_count+prohibition_count+property_count+example_count),
    sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'family_id'=family_id::TEXT AND canonical_json->>'family_sha256'=family_sha256 AND canonical_json->>'spec_key'=spec_key),
    UNIQUE(family_id,spec_key)
);

CREATE TABLE generated_strategy_spec_rows (
    spec_id UUID NOT NULL REFERENCES generated_strategy_specs(id),
    kind TEXT NOT NULL CHECK(kind IN ('input','instrument','prohibition','property','example')),
    sequence INT NOT NULL CHECK(sequence>=0), canonical_row JSONB NOT NULL,
    PRIMARY KEY(spec_id,kind,sequence)
);

CREATE TABLE generated_strategy_compilation_receipts (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK(schema_name='typed-generative-compilation-receipt-v1'),
    state TEXT NOT NULL CHECK(state='compiled'), family_id UUID NOT NULL REFERENCES strategy_families(id),
    family_sha256 TEXT NOT NULL CHECK(family_sha256 ~ '^[0-9a-f]{64}$'),
    spec_id UUID NOT NULL UNIQUE REFERENCES generated_strategy_specs(id), spec_sha256 TEXT NOT NULL CHECK(spec_sha256 ~ '^[0-9a-f]{64}$'),
    version_id UUID NOT NULL UNIQUE REFERENCES strategy_versions(id), version_sha256 TEXT NOT NULL CHECK(version_sha256 ~ '^[0-9a-f]{64}$'),
    compiler_kind TEXT NOT NULL CHECK(compiler_kind='typed-generative'), compiler_version TEXT NOT NULL CHECK(compiler_version='typed-generative-compiler-v1'),
    source_commit TEXT NOT NULL CHECK(source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'), source_tree_sha256 TEXT NOT NULL CHECK(source_tree_sha256 ~ '^[0-9a-f]{64}$'),
    config_schema TEXT NOT NULL CHECK(config_schema='typed-generative-strategy-config-v1'), decision_contract TEXT NOT NULL CHECK(decision_contract='typed-generative-decision-v1'),
    config_sha256 TEXT NOT NULL CHECK(config_sha256 ~ '^[0-9a-f]{64}$'), sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL, canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'state'=state AND canonical_json->>'spec_id'=spec_id::TEXT AND canonical_json->>'version_id'=version_id::TEXT)
);

CREATE FUNCTION validate_generated_strategy_spec() RETURNS TRIGGER AS $$
DECLARE family_sha TEXT;
BEGIN
    SELECT sha256 INTO family_sha FROM strategy_families WHERE id=NEW.family_id;
    IF family_sha IS NULL OR family_sha<>NEW.family_sha256 OR
       NEW.input_count<>jsonb_array_length(NEW.canonical_json->'inputs') OR
       NEW.instrument_count<>jsonb_array_length(NEW.canonical_json->'universe'->'instruments') OR
       NEW.prohibition_count<>jsonb_array_length(NEW.canonical_json->'prohibited_behaviors') OR
       NEW.property_count<>jsonb_array_length(NEW.canonical_json->'property_tests') OR
       NEW.example_count<>jsonb_array_length(NEW.canonical_json->'example_tests') THEN
        RAISE EXCEPTION 'generated strategy spec does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER generated_strategy_spec_guard BEFORE INSERT ON generated_strategy_specs FOR EACH ROW EXECUTE FUNCTION validate_generated_strategy_spec();

CREATE FUNCTION validate_generated_strategy_spec_row() RETURNS TRIGGER AS $$
DECLARE parent generated_strategy_specs%ROWTYPE; expected JSONB;
BEGIN
    SELECT * INTO parent FROM generated_strategy_specs WHERE id=NEW.spec_id;
    expected := CASE NEW.kind
        WHEN 'input' THEN parent.canonical_json->'inputs'->NEW.sequence
        WHEN 'instrument' THEN parent.canonical_json->'universe'->'instruments'->NEW.sequence
        WHEN 'prohibition' THEN parent.canonical_json->'prohibited_behaviors'->NEW.sequence
        WHEN 'property' THEN parent.canonical_json->'property_tests'->NEW.sequence
        WHEN 'example' THEN parent.canonical_json->'example_tests'->NEW.sequence END;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN RAISE EXCEPTION 'generated strategy normalized row does not reconstruct'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER generated_strategy_spec_row_guard BEFORE INSERT ON generated_strategy_spec_rows FOR EACH ROW EXECUTE FUNCTION validate_generated_strategy_spec_row();

CREATE FUNCTION validate_generated_strategy_spec_graph() RETURNS TRIGGER AS $$
BEGIN
    IF (SELECT count(*) FROM generated_strategy_spec_rows WHERE spec_id=NEW.id)<>NEW.normalized_row_count THEN
        RAISE EXCEPTION 'generated strategy spec graph does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER generated_strategy_spec_graph_guard AFTER INSERT ON generated_strategy_specs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_generated_strategy_spec_graph();

CREATE FUNCTION validate_generated_strategy_receipt() RETURNS TRIGGER AS $$
DECLARE spec generated_strategy_specs%ROWTYPE; version strategy_versions%ROWTYPE;
BEGIN
    SELECT * INTO spec FROM generated_strategy_specs WHERE id=NEW.spec_id;
    SELECT * INTO version FROM strategy_versions WHERE id=NEW.version_id;
    IF spec.id IS NULL OR version.id IS NULL OR spec.family_id<>NEW.family_id OR spec.family_sha256<>NEW.family_sha256 OR spec.sha256<>NEW.spec_sha256 OR
       version.family_id<>NEW.family_id OR version.sha256<>NEW.version_sha256 OR version.compiler_kind<>NEW.compiler_kind OR version.compiler_version<>NEW.compiler_version OR
       version.source_commit<>NEW.source_commit OR version.source_tree_sha256<>NEW.source_tree_sha256 OR version.config_schema<>NEW.config_schema OR version.decision_contract<>NEW.decision_contract OR
       version.config->>'spec_id'<>NEW.spec_id::TEXT OR version.config->>'spec_sha256'<>NEW.spec_sha256 OR encode(digest(version.config_bytes,'sha256'),'hex')<>NEW.config_sha256 THEN
        RAISE EXCEPTION 'generated strategy receipt does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER generated_strategy_receipt_guard BEFORE INSERT ON generated_strategy_compilation_receipts FOR EACH ROW EXECUTE FUNCTION validate_generated_strategy_receipt();

CREATE FUNCTION reject_generated_strategy_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'generated strategy evidence is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER generated_strategy_specs_append_only BEFORE UPDATE OR DELETE ON generated_strategy_specs FOR EACH ROW EXECUTE FUNCTION reject_generated_strategy_mutation();
CREATE TRIGGER generated_strategy_rows_append_only BEFORE UPDATE OR DELETE ON generated_strategy_spec_rows FOR EACH ROW EXECUTE FUNCTION reject_generated_strategy_mutation();
CREATE TRIGGER generated_strategy_receipts_append_only BEFORE UPDATE OR DELETE ON generated_strategy_compilation_receipts FOR EACH ROW EXECUTE FUNCTION reject_generated_strategy_mutation();
