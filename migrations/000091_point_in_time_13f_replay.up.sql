CREATE TABLE copy_13f_replays (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK(schema_name='point-in-time-13f-replay-v1'),
    state TEXT NOT NULL CHECK(state='completed'),
    manifest_id UUID NOT NULL REFERENCES dataset_manifests(id),
    manifest_sha256 TEXT NOT NULL CHECK(manifest_sha256 ~ '^[0-9a-f]{64}$'),
    manifest_cutoff TIMESTAMPTZ NOT NULL,
    selection_cutoff TIMESTAMPTZ NOT NULL CHECK(selection_cutoff<=manifest_cutoff),
    top_n INT NOT NULL CHECK(top_n>0),
    candidate_count INT NOT NULL CHECK(candidate_count>0),
    filing_count INT NOT NULL CHECK(filing_count>=0),
    manager_count INT NOT NULL CHECK(manager_count>0 AND manager_count<=top_n),
    decision_count INT NOT NULL CHECK(decision_count>0),
    step_count INT NOT NULL CHECK(step_count>=0),
    sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL CHECK(octet_length(canonical_bytes)>0),
    canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK(canonical_json->>'schema'=schema_name AND canonical_json->>'state'=state),
    CHECK(canonical_json->>'manifest_id'=manifest_id::TEXT AND canonical_json->>'manifest_sha256'=manifest_sha256),
    CHECK((canonical_json->>'top_n')::INT=top_n),
    UNIQUE(manifest_id,selection_cutoff,top_n)
);

CREATE TABLE copy_13f_replay_candidates (
    replay_id UUID NOT NULL REFERENCES copy_13f_replays(id), sequence INT NOT NULL CHECK(sequence>=0),
    manager_id UUID NOT NULL, partition_content_sha256 TEXT NOT NULL CHECK(partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    source_key TEXT NOT NULL CHECK(source_key<>''), content_sha256 TEXT NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
    available_at TIMESTAMPTZ NOT NULL, eligible BOOLEAN NOT NULL, score NUMERIC NOT NULL,
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(replay_id,sequence), UNIQUE(replay_id,manager_id)
);

CREATE TABLE copy_13f_replay_filings (
    replay_id UUID NOT NULL REFERENCES copy_13f_replays(id), sequence INT NOT NULL CHECK(sequence>=0),
    manager_id UUID NOT NULL, partition_content_sha256 TEXT NOT NULL CHECK(partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    source_key TEXT NOT NULL CHECK(source_key<>''), content_sha256 TEXT NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
    report_period DATE NOT NULL, published_at TIMESTAMPTZ NOT NULL, available_at TIMESTAMPTZ NOT NULL CHECK(published_at<=available_at),
    amendment_number INT NOT NULL CHECK(amendment_number>=0), supersedes_key TEXT NOT NULL,
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(replay_id,sequence), UNIQUE(replay_id,source_key), UNIQUE(replay_id,manager_id,report_period,amendment_number)
);

CREATE TABLE copy_13f_replay_managers (
    replay_id UUID NOT NULL REFERENCES copy_13f_replays(id), sequence INT NOT NULL CHECK(sequence>=0),
    manager_id UUID NOT NULL, rank INT NOT NULL CHECK(rank=sequence), score NUMERIC NOT NULL,
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(replay_id,sequence), UNIQUE(replay_id,manager_id),
    FOREIGN KEY(replay_id,manager_id) REFERENCES copy_13f_replay_candidates(replay_id,manager_id)
);

CREATE TABLE copy_13f_replay_decisions (
    replay_id UUID NOT NULL REFERENCES copy_13f_replays(id), sequence INT NOT NULL CHECK(sequence>=0),
    decision_at TIMESTAMPTZ NOT NULL, manager_id UUID NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('no_filing','selected','unchanged')),
    filing_source_key TEXT NOT NULL, filing_content_sha256 TEXT NOT NULL,
    filing_available_at TIMESTAMPTZ, report_period DATE, amendment_number INT NOT NULL CHECK(amendment_number>=0),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(replay_id,sequence), UNIQUE(replay_id,decision_at,manager_id),
    FOREIGN KEY(replay_id,manager_id) REFERENCES copy_13f_replay_managers(replay_id,manager_id),
    CHECK((status='no_filing' AND filing_source_key='' AND filing_content_sha256='' AND filing_available_at IS NULL AND report_period IS NULL AND amendment_number=0) OR
          (status<>'no_filing' AND filing_source_key<>'' AND filing_content_sha256 ~ '^[0-9a-f]{64}$' AND filing_available_at<=decision_at AND report_period IS NOT NULL))
);

CREATE TABLE copy_13f_replay_steps (
    replay_id UUID NOT NULL REFERENCES copy_13f_replays(id), sequence INT NOT NULL CHECK(sequence>=0),
    decision_sequence INT NOT NULL, partition_content_sha256 TEXT NOT NULL CHECK(partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    observation_source_key TEXT NOT NULL, observation_content_sha256 TEXT NOT NULL CHECK(observation_content_sha256 ~ '^[0-9a-f]{64}$'),
    available_at TIMESTAMPTZ NOT NULL, decision JSONB NOT NULL CHECK(jsonb_typeof(decision)='object'),
    canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
    PRIMARY KEY(replay_id,sequence), UNIQUE(replay_id,decision_sequence),
    FOREIGN KEY(replay_id,decision_sequence) REFERENCES copy_13f_replay_decisions(replay_id,sequence)
);

CREATE FUNCTION validate_copy_13f_replay_parent() RETURNS TRIGGER AS $$
DECLARE manifest dataset_manifests%ROWTYPE;
BEGIN
    SELECT * INTO manifest FROM dataset_manifests WHERE id=NEW.manifest_id;
    IF manifest.id IS NULL OR manifest.sha256<>NEW.manifest_sha256 OR manifest.decision_cutoff<>NEW.manifest_cutoff OR
       NEW.selection_cutoff<>(NEW.canonical_json->>'selection_cutoff')::TIMESTAMPTZ OR
       NEW.manifest_cutoff<>(NEW.canonical_json->>'manifest_cutoff')::TIMESTAMPTZ OR
       NEW.candidate_count<>jsonb_array_length(NEW.canonical_json->'candidate_managers') OR
       NEW.filing_count<>jsonb_array_length(NEW.canonical_json->'filings') OR
       NEW.manager_count<>jsonb_array_length(NEW.canonical_json->'managers') OR
       NEW.decision_count<>jsonb_array_length(NEW.canonical_json->'decisions') OR
       NEW.step_count<>jsonb_array_length(NEW.canonical_json->'steps') THEN
        RAISE EXCEPTION 'copy 13f replay parent does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER copy_13f_replay_parent_guard BEFORE INSERT ON copy_13f_replays FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_parent();

CREATE FUNCTION validate_copy_13f_replay_child() RETURNS TRIGGER AS $$
DECLARE expected JSONB; parent copy_13f_replays%ROWTYPE; array_name TEXT;
BEGIN
    SELECT * INTO parent FROM copy_13f_replays WHERE id=NEW.replay_id;
    array_name := CASE TG_TABLE_NAME
      WHEN 'copy_13f_replay_candidates' THEN 'candidate_managers'
      WHEN 'copy_13f_replay_filings' THEN 'filings'
      WHEN 'copy_13f_replay_managers' THEN 'managers'
      WHEN 'copy_13f_replay_decisions' THEN 'decisions'
      WHEN 'copy_13f_replay_steps' THEN 'steps' END;
    expected := parent.canonical_json->array_name->NEW.sequence;
    IF expected IS NULL OR expected<>NEW.canonical_row THEN
        RAISE EXCEPTION 'copy 13f replay normalized row does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_copy_13f_replay_evidence() RETURNS TRIGGER AS $$
DECLARE parent copy_13f_replays%ROWTYPE;
BEGIN
    SELECT * INTO parent FROM copy_13f_replays WHERE id=NEW.replay_id;
    IF NOT EXISTS (
        SELECT 1 FROM dataset_manifest_observations observation
         WHERE observation.manifest_id=parent.manifest_id
           AND observation.partition_content_sha256=NEW.partition_content_sha256
           AND observation.source_key=NEW.source_key
           AND observation.content_sha256=NEW.content_sha256
           AND observation.available_at=NEW.available_at
    ) THEN
        RAISE EXCEPTION 'copy 13f replay evidence is absent from manifest';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER copy_13f_candidates_guard BEFORE INSERT ON copy_13f_replay_candidates FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_child();
CREATE TRIGGER copy_13f_filings_guard BEFORE INSERT ON copy_13f_replay_filings FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_child();
CREATE TRIGGER copy_13f_managers_guard BEFORE INSERT ON copy_13f_replay_managers FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_child();
CREATE TRIGGER copy_13f_decisions_guard BEFORE INSERT ON copy_13f_replay_decisions FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_child();
CREATE TRIGGER copy_13f_steps_guard BEFORE INSERT ON copy_13f_replay_steps FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_child();
CREATE TRIGGER copy_13f_candidates_evidence_guard BEFORE INSERT ON copy_13f_replay_candidates FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_evidence();
CREATE TRIGGER copy_13f_filings_evidence_guard BEFORE INSERT ON copy_13f_replay_filings FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_evidence();

CREATE FUNCTION validate_copy_13f_replay_graph() RETURNS TRIGGER AS $$
BEGIN
    IF (SELECT count(*) FROM copy_13f_replay_candidates WHERE replay_id=NEW.id)<>NEW.candidate_count OR
       (SELECT count(*) FROM copy_13f_replay_filings WHERE replay_id=NEW.id)<>NEW.filing_count OR
       (SELECT count(*) FROM copy_13f_replay_managers WHERE replay_id=NEW.id)<>NEW.manager_count OR
       (SELECT count(*) FROM copy_13f_replay_decisions WHERE replay_id=NEW.id)<>NEW.decision_count OR
       (SELECT count(*) FROM copy_13f_replay_steps WHERE replay_id=NEW.id)<>NEW.step_count THEN
        RAISE EXCEPTION 'copy 13f replay graph does not reconstruct';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER copy_13f_replay_graph_guard AFTER INSERT ON copy_13f_replays DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_copy_13f_replay_graph();

CREATE FUNCTION reject_copy_13f_replay_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'copy 13f replay evidence is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER copy_13f_replays_append_only BEFORE UPDATE OR DELETE ON copy_13f_replays FOR EACH ROW EXECUTE FUNCTION reject_copy_13f_replay_mutation();
CREATE TRIGGER copy_13f_candidates_append_only BEFORE UPDATE OR DELETE ON copy_13f_replay_candidates FOR EACH ROW EXECUTE FUNCTION reject_copy_13f_replay_mutation();
CREATE TRIGGER copy_13f_filings_append_only BEFORE UPDATE OR DELETE ON copy_13f_replay_filings FOR EACH ROW EXECUTE FUNCTION reject_copy_13f_replay_mutation();
CREATE TRIGGER copy_13f_managers_append_only BEFORE UPDATE OR DELETE ON copy_13f_replay_managers FOR EACH ROW EXECUTE FUNCTION reject_copy_13f_replay_mutation();
CREATE TRIGGER copy_13f_decisions_append_only BEFORE UPDATE OR DELETE ON copy_13f_replay_decisions FOR EACH ROW EXECUTE FUNCTION reject_copy_13f_replay_mutation();
CREATE TRIGGER copy_13f_steps_append_only BEFORE UPDATE OR DELETE ON copy_13f_replay_steps FOR EACH ROW EXECUTE FUNCTION reject_copy_13f_replay_mutation();
