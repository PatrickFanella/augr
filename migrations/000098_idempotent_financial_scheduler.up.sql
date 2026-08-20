-- OVR-604 additive, inactive financial scheduler occurrence/effect boundary.
-- No cron registration, provider call, or execution route is activated here.

CREATE TABLE financial_job_definitions (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name='financial-job-definition-v1'),
    job_key TEXT NOT NULL UNIQUE CHECK (job_key ~ '^[a-z0-9][a-z0-9_./:-]{0,191}$'),
    mutation_classes TEXT[] NOT NULL CHECK (cardinality(mutation_classes)>0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds',clock_timestamp()),
    CHECK (sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (canonical_json->>'schema'=schema_name AND canonical_json->>'key'=job_key AND canonical_json->'mutations'=to_jsonb(mutation_classes)),
    CHECK (id=economic_deterministic_uuid('financial-job-definition',schema_name||'@sha256:'||sha256))
);

CREATE TABLE financial_job_occurrences (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'financial-job-occurrence-v1'),
    job_key TEXT NOT NULL REFERENCES financial_job_definitions(job_key) ON DELETE RESTRICT,
    schedule_revision TEXT NOT NULL CHECK (schedule_revision = btrim(schedule_revision) AND length(schedule_revision) BETWEEN 1 AND 256),
    trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('scheduled','manual')),
    due_at TIMESTAMPTZ NOT NULL CHECK (due_at = date_trunc('microseconds', due_at)),
    manual_request_id UUID,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds', clock_timestamp()),
    CHECK ((trigger_kind = 'scheduled' AND manual_request_id IS NULL) OR (trigger_kind = 'manual' AND manual_request_id IS NOT NULL)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json->>'schema' = schema_name AND canonical_json->>'job_key' = job_key AND canonical_json->>'schedule_revision' = schedule_revision AND canonical_json->>'trigger' = trigger_kind AND canonical_json->>'due_at' = to_char(due_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK ((trigger_kind = 'scheduled' AND NOT canonical_json ? 'manual_request_id') OR canonical_json->>'manual_request_id' = manual_request_id::TEXT),
    CHECK (id = economic_deterministic_uuid('financial-job-occurrence', schema_name || '@sha256:' || sha256)),
    UNIQUE (job_key, schedule_revision, trigger_kind, due_at, manual_request_id)
);
CREATE UNIQUE INDEX idx_financial_job_scheduled_slot ON financial_job_occurrences(job_key,schedule_revision,due_at) WHERE trigger_kind='scheduled';
CREATE UNIQUE INDEX idx_financial_job_manual_request ON financial_job_occurrences(manual_request_id) WHERE trigger_kind='manual';

CREATE TABLE financial_job_lease_events (
    id UUID PRIMARY KEY,
    occurrence_id UUID NOT NULL REFERENCES financial_job_occurrences(id) ON DELETE RESTRICT,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    event_kind TEXT NOT NULL CHECK (event_kind IN ('acquired','renewed','succeeded','failed')),
    owner_id UUID NOT NULL,
    fence_token BIGINT NOT NULL CHECK (fence_token > 0),
    occurred_at TIMESTAMPTZ NOT NULL CHECK (occurred_at = date_trunc('microseconds', occurred_at)),
    lease_expires_at TIMESTAMPTZ,
    outcome_sha256 TEXT NOT NULL DEFAULT '' CHECK (outcome_sha256 = '' OR outcome_sha256 ~ '^[0-9a-f]{64}$'),
    CHECK ((event_kind IN ('acquired','renewed') AND lease_expires_at > occurred_at AND outcome_sha256 = '') OR (event_kind IN ('succeeded','failed') AND lease_expires_at IS NULL AND outcome_sha256 ~ '^[0-9a-f]{64}$')),
    CHECK (id = economic_deterministic_uuid('financial-job-lease-event', occurrence_id::TEXT, sequence::TEXT, event_kind, owner_id::TEXT, fence_token::TEXT)),
    UNIQUE (occurrence_id, sequence)
);

CREATE TABLE financial_job_effect_claims (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name = 'financial-job-effect-v1'),
    occurrence_id UUID NOT NULL REFERENCES financial_job_occurrences(id) ON DELETE RESTRICT,
    effect_kind TEXT NOT NULL CHECK (effect_kind IN ('execution_intent','execution_order','settlement','ledger','allocation','provider_mutation','supervisor_assessment')),
    business_key TEXT NOT NULL CHECK (business_key ~ '^[a-z0-9][a-z0-9_./:-]{0,191}$'),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    owner_id UUID NOT NULL,
    fence_token BIGINT NOT NULL CHECK (fence_token > 0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json) = 'object'),
    claimed_at TIMESTAMPTZ NOT NULL CHECK (claimed_at = date_trunc('microseconds', claimed_at)),
    CHECK (sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')),
    CHECK (canonical_json = convert_from(canonical_bytes, 'UTF8')::JSONB),
    CHECK (canonical_json->>'schema' = schema_name AND canonical_json->>'occurrence_id' = occurrence_id::TEXT AND canonical_json->>'kind' = effect_kind AND canonical_json->>'business_key' = business_key AND canonical_json->>'payload_sha256' = payload_sha256),
    CHECK (id = economic_deterministic_uuid('financial-job-effect', occurrence_id::TEXT, effect_kind, business_key)),
    UNIQUE (occurrence_id, effect_kind, business_key)
);

CREATE FUNCTION validate_financial_job_lease_event() RETURNS TRIGGER AS $$
DECLARE previous financial_job_lease_events%ROWTYPE; db_now TIMESTAMPTZ;
BEGIN
    PERFORM 1 FROM financial_job_occurrences WHERE id = NEW.occurrence_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'financial scheduler occurrence is missing'; END IF;
    SELECT * INTO previous FROM financial_job_lease_events WHERE occurrence_id = NEW.occurrence_id ORDER BY sequence DESC LIMIT 1;
    db_now := date_trunc('microseconds', clock_timestamp());
    NEW.occurred_at := db_now;
    IF previous.id IS NULL THEN
        IF NEW.event_kind <> 'acquired' OR NEW.sequence <> 1 OR NEW.fence_token <> 1 THEN RAISE EXCEPTION 'financial scheduler first event must acquire fence one'; END IF;
    ELSIF previous.event_kind IN ('succeeded','failed') THEN
        RAISE EXCEPTION 'financial scheduler occurrence is terminal';
    ELSIF NEW.event_kind = 'acquired' THEN
        IF previous.lease_expires_at > db_now OR NEW.sequence <> previous.sequence + 1 OR NEW.fence_token <> previous.fence_token + 1 THEN RAISE EXCEPTION 'financial scheduler takeover is not authorized'; END IF;
    ELSIF NEW.event_kind = 'renewed' THEN
        IF previous.lease_expires_at <= db_now OR NEW.sequence <> previous.sequence + 1 OR NEW.fence_token <> previous.fence_token OR NEW.owner_id <> previous.owner_id OR NEW.lease_expires_at <= previous.lease_expires_at THEN RAISE EXCEPTION 'financial scheduler renewal is not authorized'; END IF;
    ELSE
        IF previous.lease_expires_at <= db_now OR NEW.sequence <> previous.sequence + 1 OR NEW.fence_token <> previous.fence_token OR NEW.owner_id <> previous.owner_id THEN RAISE EXCEPTION 'financial scheduler completion is not authorized'; END IF;
    END IF;
    RETURN NEW;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_financial_job_effect_claim() RETURNS TRIGGER AS $$
DECLARE current_lease financial_job_lease_events%ROWTYPE; db_now TIMESTAMPTZ;
BEGIN
    PERFORM 1 FROM financial_job_occurrences WHERE id = NEW.occurrence_id FOR UPDATE;
    SELECT * INTO current_lease FROM financial_job_lease_events WHERE occurrence_id = NEW.occurrence_id ORDER BY sequence DESC LIMIT 1;
    db_now := date_trunc('microseconds', clock_timestamp());
    IF current_lease.id IS NULL OR current_lease.event_kind NOT IN ('acquired','renewed') OR current_lease.lease_expires_at <= db_now OR current_lease.owner_id <> NEW.owner_id OR current_lease.fence_token <> NEW.fence_token THEN
        RAISE EXCEPTION 'financial scheduler effect claim is not fenced by the current lease';
    END IF;
    NEW.claimed_at := db_now;
    RETURN NEW;
END; $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_financial_job_lease_event BEFORE INSERT ON financial_job_lease_events FOR EACH ROW EXECUTE FUNCTION validate_financial_job_lease_event();
CREATE TRIGGER trg_financial_job_effect_claim BEFORE INSERT ON financial_job_effect_claims FOR EACH ROW EXECUTE FUNCTION validate_financial_job_effect_claim();

CREATE FUNCTION reject_financial_scheduler_mutation() RETURNS TRIGGER AS $$
BEGIN RAISE EXCEPTION 'financial scheduler evidence is append-only'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER trg_financial_job_occurrences_immutable BEFORE UPDATE OR DELETE ON financial_job_occurrences FOR EACH ROW EXECUTE FUNCTION reject_financial_scheduler_mutation();
CREATE TRIGGER trg_financial_job_definitions_immutable BEFORE UPDATE OR DELETE ON financial_job_definitions FOR EACH ROW EXECUTE FUNCTION reject_financial_scheduler_mutation();
CREATE TRIGGER trg_financial_job_lease_events_immutable BEFORE UPDATE OR DELETE ON financial_job_lease_events FOR EACH ROW EXECUTE FUNCTION reject_financial_scheduler_mutation();
CREATE TRIGGER trg_financial_job_effect_claims_immutable BEFORE UPDATE OR DELETE ON financial_job_effect_claims FOR EACH ROW EXECUTE FUNCTION reject_financial_scheduler_mutation();

CREATE INDEX idx_financial_job_occurrences_due ON financial_job_occurrences(job_key, due_at, id);
CREATE INDEX idx_financial_job_lease_events_latest ON financial_job_lease_events(occurrence_id, sequence DESC);
CREATE INDEX idx_financial_job_effect_claims_occurrence ON financial_job_effect_claims(occurrence_id, effect_kind, id);
