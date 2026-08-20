LOCK TABLE projection_checkpoints, quote_snapshots, instruments IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION dataset_json_string(value TEXT) RETURNS TEXT AS $$
    SELECT replace(replace(replace(replace(replace(
      to_json(value)::TEXT,
      '&', '\u0026'), '<', '\u003c'), '>', '\u003e'), chr(8232), '\u2028'), chr(8233), '\u2029');
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION dataset_json_text_array(value JSONB) RETURNS TEXT AS $$
    SELECT '[' || COALESCE(string_agg(dataset_json_string(item),',' ORDER BY ordinal),'') || ']'
    FROM jsonb_array_elements_text(value) WITH ORDINALITY AS rows(item,ordinal);
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION dataset_check_key(partition_sha TEXT, check_code TEXT) RETURNS TEXT AS $$
    SELECT encode(digest(convert_to(partition_sha,'UTF8') || decode('00','hex') || convert_to(check_code,'UTF8'),'sha256'),'hex');
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION dataset_finding_key(check_key TEXT, finding_code TEXT, evidence JSONB) RETURNS TEXT AS $$
DECLARE payload BYTEA := convert_to(check_key,'UTF8') || decode('00','hex') || convert_to(finding_code,'UTF8') || decode('00','hex'); row_value RECORD; first_value BOOLEAN := TRUE;
BEGIN
  FOR row_value IN SELECT item FROM jsonb_array_elements_text(evidence) WITH ORDINALITY AS rows(item,ordinal) ORDER BY ordinal LOOP
    IF NOT first_value THEN payload := payload || decode('00','hex'); END IF;
    payload := payload || convert_to(row_value.item,'UTF8'); first_value := FALSE;
  END LOOP;
  RETURN encode(digest(payload,'sha256'),'hex');
END;
$$ LANGUAGE plpgsql IMMUTABLE STRICT;

CREATE FUNCTION dataset_check_applies(kind_value TEXT, check_value TEXT) RETURNS BOOLEAN AS $$
    SELECT CASE check_value
      WHEN 'bid_ask' THEN kind_value IN ('depth','prediction_books','quotes')
      WHEN 'content_integrity' THEN TRUE
      WHEN 'corporate_action_reconciliation' THEN kind_value='bars'
      WHEN 'correction_lineage' THEN kind_value IN ('benchmark_membership','corporate_actions','filings','fundamentals','option_chains','option_contracts','prediction_fees','prediction_rules','resolutions')
      WHEN 'instrument_validity' THEN kind_value IN ('bars','benchmark_membership','corporate_actions','depth','fundamentals','option_chains','option_contracts','quotes')
      WHEN 'monotonic_time' THEN TRUE
      WHEN 'nonnegative_depth' THEN kind_value IN ('depth','prediction_books')
      WHEN 'nonnegative_volume' THEN kind_value IN ('bars','prediction_trades')
      WHEN 'no_lookahead' THEN TRUE
      WHEN 'provider_spot_comparison' THEN kind_value IN ('bars','depth','option_chains','prediction_books','prediction_trades','quotes')
      WHEN 'session_coverage' THEN kind_value IN ('bars','depth','option_chains','quotes')
      WHEN 'unique_source_identity' THEN TRUE
      ELSE FALSE END;
$$ LANGUAGE sql IMMUTABLE STRICT;

CREATE FUNCTION dataset_observation_identity(
    sequence_value INTEGER, source_key TEXT, instrument_id TEXT, effective_at TEXT,
    published_at TEXT, observed_at TEXT, available_at TEXT, revision TEXT,
    correction_of TEXT, content_sha256 TEXT, bid TEXT, ask TEXT, volume TEXT, depth TEXT
) RETURNS TEXT AS $$
    SELECT '{"sequence":' || sequence_value::TEXT ||
      ',"source_key":' || dataset_json_string(source_key) ||
      ',"instrument_id":' || dataset_json_string(instrument_id) ||
      ',"effective_at":' || dataset_json_string(effective_at) ||
      ',"published_at":' || dataset_json_string(published_at) ||
      ',"observed_at":' || dataset_json_string(observed_at) ||
      ',"available_at":' || dataset_json_string(available_at) ||
      ',"revision":' || dataset_json_string(revision) ||
      ',"correction_of":' || dataset_json_string(correction_of) ||
      ',"content_sha256":' || dataset_json_string(content_sha256) ||
      ',"bid":' || COALESCE(dataset_json_string(bid),'null') ||
      ',"ask":' || COALESCE(dataset_json_string(ask),'null') ||
      ',"volume":' || COALESCE(dataset_json_string(volume),'null') ||
      ',"depth":' || COALESCE(dataset_json_string(depth),'null') || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION dataset_partition_identity(
    sequence_value INTEGER, kind TEXT, provider TEXT, source_name TEXT, namespace TEXT,
    request_sha256 TEXT, content_sha256 TEXT, media_type TEXT, effective_start TEXT,
    effective_end TEXT, observed_start TEXT, observed_end TEXT, available_start TEXT,
    available_end TEXT, symbology_version TEXT, adjustment_policy TEXT, timezone_name TEXT,
    calendar_name TEXT, revision TEXT, supersedes_content_sha256 TEXT, row_count INTEGER,
    license_name TEXT, retention_policy TEXT, observations_json TEXT
) RETURNS TEXT AS $$
    SELECT '{"sequence":' || sequence_value::TEXT ||
      ',"kind":' || dataset_json_string(kind) ||
      ',"provider":' || dataset_json_string(provider) ||
      ',"source":' || dataset_json_string(source_name) ||
      ',"namespace":' || dataset_json_string(namespace) ||
      ',"request_sha256":' || dataset_json_string(request_sha256) ||
      ',"content_sha256":' || dataset_json_string(content_sha256) ||
      ',"media_type":' || dataset_json_string(media_type) ||
      ',"effective_start":' || dataset_json_string(effective_start) ||
      ',"effective_end":' || dataset_json_string(effective_end) ||
      ',"observed_start":' || dataset_json_string(observed_start) ||
      ',"observed_end":' || dataset_json_string(observed_end) ||
      ',"available_start":' || dataset_json_string(available_start) ||
      ',"available_end":' || dataset_json_string(available_end) ||
      ',"symbology_version":' || dataset_json_string(symbology_version) ||
      ',"adjustment_policy":' || dataset_json_string(adjustment_policy) ||
      ',"timezone":' || dataset_json_string(timezone_name) ||
      ',"calendar":' || dataset_json_string(calendar_name) ||
      ',"revision":' || dataset_json_string(revision) ||
      ',"supersedes_content_sha256":' || dataset_json_string(supersedes_content_sha256) ||
      ',"row_count":' || row_count::TEXT ||
      ',"license":' || dataset_json_string(license_name) ||
      ',"retention_policy":' || dataset_json_string(retention_policy) ||
      ',"observations":' || observations_json || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION dataset_manifest_identity(
    decision_cutoff TEXT, partition_count INTEGER, observation_count INTEGER, partitions_json TEXT
) RETURNS TEXT AS $$
    SELECT '{"schema":"dataset-manifest-v1","decision_cutoff":' || dataset_json_string(decision_cutoff) ||
      ',"partition_count":' || partition_count::TEXT || ',"observation_count":' || observation_count::TEXT ||
      ',"partitions":' || partitions_json || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION dataset_check_identity(
    check_key TEXT, partition_sha TEXT, kind TEXT, check_code TEXT, required_value BOOLEAN,
    status_value TEXT, severity_value TEXT, evidence_sha TEXT
) RETURNS TEXT AS $$
    SELECT '{"key":' || dataset_json_string(check_key) ||
      ',"partition_content_sha256":' || dataset_json_string(partition_sha) ||
      ',"kind":' || dataset_json_string(kind) || ',"check":' || dataset_json_string(check_code) ||
      ',"required":' || required_value::TEXT || ',"status":' || dataset_json_string(status_value) ||
      ',"severity":' || dataset_json_string(severity_value) ||
      ',"evidence_sha256":' || dataset_json_string(evidence_sha) || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION dataset_finding_identity(
    finding_key TEXT, partition_sha TEXT, check_code TEXT, finding_code TEXT,
    severity_value TEXT, evidence_json TEXT
) RETURNS TEXT AS $$
    SELECT '{"key":' || dataset_json_string(finding_key) ||
      ',"partition_content_sha256":' || dataset_json_string(partition_sha) ||
      ',"check":' || dataset_json_string(check_code) || ',"code":' || dataset_json_string(finding_code) ||
      ',"severity":' || dataset_json_string(severity_value) || ',"evidence":' || evidence_json || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE FUNCTION dataset_quality_identity(
    policy_version TEXT, manifest_id TEXT, quarantined BOOLEAN, check_count INTEGER,
    finding_count INTEGER, checks_json TEXT, findings_json TEXT
) RETURNS TEXT AS $$
    SELECT '{"schema":"dataset-quality-result-v1","policy_version":' || dataset_json_string(policy_version) ||
      ',"manifest_id":' || dataset_json_string(manifest_id) || ',"quarantined":' || quarantined::TEXT ||
      ',"check_count":' || check_count::TEXT || ',"finding_count":' || finding_count::TEXT ||
      ',"checks":' || checks_json || ',"findings":' || findings_json || '}';
$$ LANGUAGE sql IMMUTABLE;

CREATE TABLE dataset_quality_policy_artifacts (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name='dataset-quality-policy-v1'),
    policy_version TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at=date_trunc('microseconds',created_at)),
    CHECK (sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK (sha256='8b1d8dd9328f060b455cbd096829c01429017223baf37154aeb6059cf64b894c'),
    CHECK (policy_version=schema_name || '@sha256:' || sha256),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (canonical_json->>'schema'=schema_name),
    CHECK (jsonb_array_length(canonical_json->'kinds')=15),
    CHECK (jsonb_array_length(canonical_json->'rules')=12),
    CHECK (id=economic_deterministic_uuid('dataset-quality-policy-artifact',policy_version))
);

CREATE TABLE dataset_manifests (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name='dataset-manifest-v1'),
    decision_cutoff TIMESTAMPTZ NOT NULL CHECK (decision_cutoff=date_trunc('microseconds',decision_cutoff)),
    partition_count INTEGER NOT NULL CHECK (partition_count>0),
    observation_count INTEGER NOT NULL CHECK (observation_count>0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at=date_trunc('microseconds',created_at)),
    CHECK (sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (canonical_json->>'schema'=schema_name),
    CHECK (canonical_json->>'decision_cutoff'=to_char(decision_cutoff AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
    CHECK ((canonical_json->>'partition_count')::INTEGER=partition_count),
    CHECK ((canonical_json->>'observation_count')::INTEGER=observation_count),
    CHECK (id=economic_deterministic_uuid('dataset-manifest',schema_name || '@sha256:' || sha256))
);

CREATE TABLE dataset_manifest_partitions (
    manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
    manifest_decision_cutoff TIMESTAMPTZ NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence>=0),
    kind TEXT NOT NULL CHECK (kind IN ('bars','benchmark_membership','corporate_actions','depth','external_object','filings','fundamentals','option_chains','option_contracts','prediction_books','prediction_fees','prediction_rules','prediction_trades','quotes','resolutions')),
    provider TEXT NOT NULL CHECK (provider<>'' AND provider=btrim(provider)),
    source_name TEXT NOT NULL CHECK (source_name<>'' AND source_name=btrim(source_name)),
    namespace TEXT NOT NULL CHECK (namespace<>'' AND namespace=btrim(namespace)),
    request_sha256 TEXT NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    content_sha256 TEXT NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    media_type TEXT NOT NULL CHECK (media_type<>'' AND media_type=btrim(media_type)),
    effective_start TIMESTAMPTZ NOT NULL,
    effective_end TIMESTAMPTZ NOT NULL CHECK (effective_end>=effective_start),
    observed_start TIMESTAMPTZ NOT NULL,
    observed_end TIMESTAMPTZ NOT NULL CHECK (observed_end>=observed_start),
    available_start TIMESTAMPTZ NOT NULL,
    available_end TIMESTAMPTZ NOT NULL CHECK (available_end>=available_start),
    symbology_version TEXT NOT NULL CHECK (symbology_version<>''),
    adjustment_policy TEXT NOT NULL CHECK (adjustment_policy<>''),
    timezone_name TEXT NOT NULL CHECK (timezone_name<>''),
    calendar_name TEXT NOT NULL CHECK (calendar_name<>''),
    revision TEXT NOT NULL,
    supersedes_content_sha256 TEXT NOT NULL CHECK (supersedes_content_sha256='' OR supersedes_content_sha256 ~ '^[0-9a-f]{64}$'),
    row_count INTEGER NOT NULL CHECK (row_count>0),
    license_name TEXT NOT NULL CHECK (license_name<>''),
    retention_policy TEXT NOT NULL CHECK (retention_policy<>''),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    PRIMARY KEY(manifest_id,sequence),
    UNIQUE(manifest_id,content_sha256),
    CHECK (manifest_decision_cutoff=date_trunc('microseconds',manifest_decision_cutoff)),
    CHECK (effective_start=date_trunc('microseconds',effective_start) AND effective_end=date_trunc('microseconds',effective_end)),
    CHECK (observed_start=date_trunc('microseconds',observed_start) AND observed_end=date_trunc('microseconds',observed_end)),
    CHECK (available_start=date_trunc('microseconds',available_start) AND available_end=date_trunc('microseconds',available_end)),
    CHECK (available_end<=manifest_decision_cutoff),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB)
);

CREATE TABLE dataset_manifest_observations (
    manifest_id UUID NOT NULL,
    manifest_decision_cutoff TIMESTAMPTZ NOT NULL,
    partition_sequence INTEGER NOT NULL,
    partition_content_sha256 TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence>=0),
    source_key TEXT NOT NULL CHECK (source_key<>'' AND source_key=btrim(source_key)),
    instrument_id UUID REFERENCES instruments(id) ON DELETE RESTRICT,
    effective_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    observed_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    revision TEXT NOT NULL,
    correction_of TEXT NOT NULL,
    content_sha256 TEXT NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    bid NUMERIC(38,12), ask NUMERIC(38,12), volume NUMERIC(38,12), depth NUMERIC(38,12),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    PRIMARY KEY(manifest_id,partition_sequence,sequence),
    UNIQUE(manifest_id,partition_sequence,source_key,revision),
    FOREIGN KEY(manifest_id,partition_sequence) REFERENCES dataset_manifest_partitions(manifest_id,sequence) ON DELETE RESTRICT,
    CHECK (manifest_decision_cutoff=date_trunc('microseconds',manifest_decision_cutoff)),
    CHECK (effective_at=date_trunc('microseconds',effective_at) AND observed_at=date_trunc('microseconds',observed_at) AND available_at=date_trunc('microseconds',available_at)),
    CHECK (published_at IS NULL OR published_at=date_trunc('microseconds',published_at)),
    CHECK (published_at IS NULL OR published_at<=observed_at),
    CHECK (observed_at<=available_at AND available_at<=manifest_decision_cutoff),
    CHECK ((bid IS NULL)=(ask IS NULL) AND (bid IS NULL OR bid>=0 AND ask>=bid)),
    CHECK (volume IS NULL OR volume>=0), CHECK (depth IS NULL OR depth>=0),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (convert_from(canonical_bytes,'UTF8')=dataset_observation_identity(sequence,source_key,COALESCE(instrument_id::TEXT,''),
      to_char(effective_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      CASE WHEN published_at IS NULL THEN '' ELSE to_char(published_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END,
      to_char(observed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(available_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      revision,correction_of,content_sha256,CASE WHEN bid IS NULL THEN NULL ELSE trim_scale(bid)::TEXT END,
      CASE WHEN ask IS NULL THEN NULL ELSE trim_scale(ask)::TEXT END,CASE WHEN volume IS NULL THEN NULL ELSE trim_scale(volume)::TEXT END,
      CASE WHEN depth IS NULL THEN NULL ELSE trim_scale(depth)::TEXT END))
);

CREATE TABLE dataset_quality_results (
    id UUID PRIMARY KEY,
    schema_name TEXT NOT NULL CHECK (schema_name='dataset-quality-result-v1'),
    policy_version TEXT NOT NULL REFERENCES dataset_quality_policy_artifacts(policy_version) ON DELETE RESTRICT,
    manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
    quarantined BOOLEAN NOT NULL,
    check_count INTEGER NOT NULL CHECK (check_count>0),
    finding_count INTEGER NOT NULL CHECK (finding_count>=0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    created_at TIMESTAMPTZ NOT NULL CHECK (created_at=date_trunc('microseconds',created_at)),
    CHECK (sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (canonical_json->>'schema'=schema_name AND canonical_json->>'policy_version'=policy_version AND canonical_json->>'manifest_id'=manifest_id::TEXT),
    CHECK ((canonical_json->>'quarantined')::BOOLEAN=quarantined),
    CHECK (id=economic_deterministic_uuid('dataset-quality-result',schema_name || '@sha256:' || sha256))
);

CREATE TABLE dataset_quality_checks (
    result_id UUID NOT NULL REFERENCES dataset_quality_results(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL,
    manifest_id UUID NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence>=0),
    check_key TEXT NOT NULL,
    partition_content_sha256 TEXT NOT NULL CHECK (partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    kind TEXT NOT NULL,
    check_code TEXT NOT NULL,
    required BOOLEAN NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('passed','failed','not_assessed')),
    severity TEXT NOT NULL CHECK (severity IN ('high','critical')),
    evidence_sha256 TEXT NOT NULL CHECK (evidence_sha256='' OR evidence_sha256 ~ '^[0-9a-f]{64}$'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    PRIMARY KEY(result_id,sequence), UNIQUE(result_id,check_key),
    CHECK (kind IN ('bars','benchmark_membership','corporate_actions','depth','external_object','filings','fundamentals','option_chains','option_contracts','prediction_books','prediction_fees','prediction_rules','prediction_trades','quotes','resolutions')),
    CHECK (check_code IN ('bid_ask','content_integrity','corporate_action_reconciliation','correction_lineage','instrument_validity','monotonic_time','nonnegative_depth','nonnegative_volume','no_lookahead','provider_spot_comparison','session_coverage','unique_source_identity')),
    CHECK (dataset_check_applies(kind,check_code)),
    CHECK (required=(check_code<>'provider_spot_comparison')),
    CHECK (severity=CASE WHEN check_code IN ('bid_ask','content_integrity','instrument_validity','nonnegative_depth','nonnegative_volume','no_lookahead','unique_source_identity') THEN 'critical' ELSE 'high' END),
    CHECK (check_key=dataset_check_key(partition_content_sha256,check_code)),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (convert_from(canonical_bytes,'UTF8')=dataset_check_identity(check_key,partition_content_sha256,kind,check_code,required,status,severity,evidence_sha256))
);

CREATE TABLE dataset_quality_findings (
    result_id UUID NOT NULL REFERENCES dataset_quality_results(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL,
    manifest_id UUID NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence>=0),
    finding_key TEXT NOT NULL CHECK (finding_key ~ '^[0-9a-f]{64}$'),
    partition_content_sha256 TEXT NOT NULL CHECK (partition_content_sha256 ~ '^[0-9a-f]{64}$'),
    check_code TEXT NOT NULL,
    finding_code TEXT NOT NULL CHECK (finding_code<>''),
    severity TEXT NOT NULL CHECK (severity IN ('high','critical')),
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence)='array'),
    canonical_bytes BYTEA NOT NULL,
    canonical_json JSONB NOT NULL CHECK (jsonb_typeof(canonical_json)='object'),
    PRIMARY KEY(result_id,sequence), UNIQUE(result_id,finding_key), UNIQUE(result_id,partition_content_sha256,check_code),
    CHECK (canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
    CHECK (finding_key=dataset_finding_key(dataset_check_key(partition_content_sha256,check_code),finding_code,evidence)),
    CHECK (convert_from(canonical_bytes,'UTF8')=dataset_finding_identity(finding_key,partition_content_sha256,check_code,finding_code,severity,dataset_json_text_array(evidence)))
);

CREATE FUNCTION dataset_partition_content_digest(target_manifest UUID, target_partition INTEGER) RETURNS TEXT AS $$
DECLARE payload BYTEA := convert_to('dataset-partition-observations-v1','UTF8') || decode('00','hex'); row_value RECORD;
BEGIN
  FOR row_value IN SELECT canonical_bytes FROM dataset_manifest_observations WHERE manifest_id=target_manifest AND partition_sequence=target_partition ORDER BY sequence LOOP
    payload := payload || int8send(octet_length(row_value.canonical_bytes)::BIGINT) || row_value.canonical_bytes;
  END LOOP;
  RETURN encode(digest(payload,'sha256'),'hex');
END;
$$ LANGUAGE plpgsql STABLE;

CREATE FUNCTION validate_dataset_manifest_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; partition_row RECORD; observations_text TEXT; partitions_text TEXT; expected BYTEA;
BEGIN
  target := COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'manifest_id')::UUID);
  FOR partition_row IN SELECT * FROM dataset_manifest_partitions WHERE manifest_id=target ORDER BY sequence LOOP
    SELECT '[' || COALESCE(string_agg(convert_from(canonical_bytes,'UTF8'),',' ORDER BY sequence),'') || ']'
      INTO observations_text FROM dataset_manifest_observations WHERE manifest_id=target AND partition_sequence=partition_row.sequence;
    expected := convert_to(dataset_partition_identity(partition_row.sequence,partition_row.kind,partition_row.provider,partition_row.source_name,
      partition_row.namespace,partition_row.request_sha256,partition_row.content_sha256,partition_row.media_type,
      to_char(partition_row.effective_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(partition_row.effective_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      to_char(partition_row.observed_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(partition_row.observed_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      to_char(partition_row.available_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),to_char(partition_row.available_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      partition_row.symbology_version,partition_row.adjustment_policy,partition_row.timezone_name,partition_row.calendar_name,partition_row.revision,
      partition_row.supersedes_content_sha256,partition_row.row_count,partition_row.license_name,partition_row.retention_policy,observations_text),'UTF8');
    IF expected<>partition_row.canonical_bytes OR partition_row.content_sha256<>dataset_partition_content_digest(target,partition_row.sequence) THEN
      RAISE EXCEPTION 'dataset partition canonical graph does not reconstruct';
    END IF;
  END LOOP;
  SELECT '[' || COALESCE(string_agg(convert_from(canonical_bytes,'UTF8'),',' ORDER BY sequence),'') || ']'
    INTO partitions_text FROM dataset_manifest_partitions WHERE manifest_id=target;
  PERFORM 1 FROM dataset_manifests m WHERE m.id=target AND
    m.partition_count=(SELECT count(*) FROM dataset_manifest_partitions WHERE manifest_id=m.id) AND
    m.observation_count=(SELECT count(*) FROM dataset_manifest_observations WHERE manifest_id=m.id) AND
    (SELECT COALESCE(min(sequence),0)=0 AND COALESCE(max(sequence),-1)=m.partition_count-1 FROM dataset_manifest_partitions WHERE manifest_id=m.id) AND
    NOT EXISTS (SELECT 1 FROM dataset_manifest_partitions p WHERE p.manifest_id=m.id AND p.manifest_decision_cutoff<>m.decision_cutoff) AND
    NOT EXISTS (SELECT 1 FROM dataset_manifest_observations o JOIN dataset_manifest_partitions p ON p.manifest_id=o.manifest_id AND p.sequence=o.partition_sequence
      WHERE o.manifest_id=m.id AND (o.manifest_decision_cutoff<>m.decision_cutoff OR o.partition_content_sha256<>p.content_sha256)) AND
    NOT EXISTS (SELECT 1 FROM dataset_manifest_partitions p WHERE p.manifest_id=m.id AND (p.row_count<>(SELECT count(*) FROM dataset_manifest_observations o WHERE o.manifest_id=m.id AND o.partition_sequence=p.sequence) OR
      (SELECT COALESCE(min(sequence),0)<>0 OR COALESCE(max(sequence),-1)<>p.row_count-1 OR min(effective_at)<>p.effective_start OR max(effective_at)<>p.effective_end OR
        min(observed_at)<>p.observed_start OR max(observed_at)<>p.observed_end OR min(available_at)<>p.available_start OR max(available_at)<>p.available_end
       FROM dataset_manifest_observations o WHERE o.manifest_id=m.id AND o.partition_sequence=p.sequence))) AND
    NOT EXISTS (SELECT 1 FROM dataset_manifest_observations correction WHERE correction.manifest_id=m.id AND correction.correction_of<>'' AND NOT EXISTS (
      SELECT 1 FROM dataset_manifest_observations original WHERE original.manifest_id=correction.manifest_id AND original.partition_sequence=correction.partition_sequence AND
      original.source_key=correction.correction_of AND original.correction_of='' AND original.available_at<correction.available_at)) AND
    m.canonical_bytes=convert_to(dataset_manifest_identity(to_char(m.decision_cutoff AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),m.partition_count,m.observation_count,partitions_text),'UTF8');
  IF NOT FOUND THEN RAISE EXCEPTION 'dataset manifest graph does not reconstruct'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_dataset_quality_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; checks_text TEXT; findings_text TEXT;
BEGIN
  target := COALESCE((to_jsonb(NEW)->>'id')::UUID,(to_jsonb(NEW)->>'result_id')::UUID);
  SELECT '[' || COALESCE(string_agg(convert_from(canonical_bytes,'UTF8'),',' ORDER BY sequence),'') || ']' INTO checks_text FROM dataset_quality_checks WHERE result_id=target;
  SELECT '[' || COALESCE(string_agg(convert_from(canonical_bytes,'UTF8'),',' ORDER BY sequence),'') || ']' INTO findings_text FROM dataset_quality_findings WHERE result_id=target;
  PERFORM 1 FROM dataset_quality_results r WHERE r.id=target AND
    r.check_count=(SELECT count(*) FROM dataset_quality_checks WHERE result_id=r.id) AND r.finding_count=(SELECT count(*) FROM dataset_quality_findings WHERE result_id=r.id) AND
    (SELECT COALESCE(min(sequence),0)=0 AND COALESCE(max(sequence),-1)=r.check_count-1 FROM dataset_quality_checks WHERE result_id=r.id) AND
    (r.finding_count=0 OR (SELECT min(sequence)=0 AND max(sequence)=r.finding_count-1 FROM dataset_quality_findings WHERE result_id=r.id)) AND
    NOT EXISTS (SELECT 1 FROM dataset_quality_checks c WHERE c.result_id=r.id AND (c.policy_version,c.manifest_id) IS DISTINCT FROM (r.policy_version,r.manifest_id)) AND
    NOT EXISTS (SELECT 1 FROM dataset_quality_checks c WHERE c.result_id=r.id AND NOT EXISTS (
      SELECT 1 FROM dataset_manifest_partitions p WHERE p.manifest_id=r.manifest_id AND p.content_sha256=c.partition_content_sha256 AND p.kind=c.kind)) AND
    NOT EXISTS (SELECT 1 FROM dataset_quality_findings f WHERE f.result_id=r.id AND (f.policy_version,f.manifest_id) IS DISTINCT FROM (r.policy_version,r.manifest_id)) AND
    r.quarantined=EXISTS(SELECT 1 FROM dataset_quality_checks c WHERE c.result_id=r.id AND (c.status='failed' OR c.required AND c.status='not_assessed')) AND
    NOT EXISTS (SELECT 1 FROM dataset_quality_checks c WHERE c.result_id=r.id AND c.status<>'passed' AND NOT EXISTS (
      SELECT 1 FROM dataset_quality_findings f WHERE f.result_id=c.result_id AND f.partition_content_sha256=c.partition_content_sha256 AND f.check_code=c.check_code)) AND
    NOT EXISTS (SELECT 1 FROM dataset_quality_findings f WHERE f.result_id=r.id AND NOT EXISTS (
      SELECT 1 FROM dataset_quality_checks c WHERE c.result_id=f.result_id AND c.partition_content_sha256=f.partition_content_sha256 AND c.check_code=f.check_code AND c.status<>'passed' AND c.severity=f.severity)) AND
    r.canonical_bytes=convert_to(dataset_quality_identity(r.policy_version,r.manifest_id::TEXT,r.quarantined,r.check_count,r.finding_count,checks_text,findings_text),'UTF8');
  IF NOT FOUND THEN RAISE EXCEPTION 'dataset quality graph does not reconstruct'; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION reject_dataset_evidence_mutation() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'dataset evidence is append-only'; END; $$ LANGUAGE plpgsql;

DO $$ DECLARE name TEXT; BEGIN FOREACH name IN ARRAY ARRAY['dataset_quality_policy_artifacts','dataset_manifests','dataset_manifest_partitions','dataset_manifest_observations','dataset_quality_results','dataset_quality_checks','dataset_quality_findings'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_dataset_evidence_mutation()','trg_'||name||'_immutable',name);
END LOOP; END $$;

CREATE CONSTRAINT TRIGGER trg_dataset_manifest_graph AFTER INSERT ON dataset_manifests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_dataset_manifest_graph();
CREATE CONSTRAINT TRIGGER trg_dataset_partition_graph AFTER INSERT ON dataset_manifest_partitions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_dataset_manifest_graph();
CREATE CONSTRAINT TRIGGER trg_dataset_observation_graph AFTER INSERT ON dataset_manifest_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_dataset_manifest_graph();
CREATE CONSTRAINT TRIGGER trg_dataset_quality_graph AFTER INSERT ON dataset_quality_results DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_dataset_quality_graph();
CREATE CONSTRAINT TRIGGER trg_dataset_check_graph AFTER INSERT ON dataset_quality_checks DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_dataset_quality_graph();
CREATE CONSTRAINT TRIGGER trg_dataset_finding_graph AFTER INSERT ON dataset_quality_findings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_dataset_quality_graph();

CREATE INDEX idx_dataset_manifest_cutoff ON dataset_manifests(decision_cutoff,id);
CREATE INDEX idx_dataset_partition_kind ON dataset_manifest_partitions(kind,provider,namespace,manifest_id);
CREATE INDEX idx_dataset_observation_instrument_time ON dataset_manifest_observations(instrument_id,effective_at,available_at) WHERE instrument_id IS NOT NULL;
CREATE INDEX idx_dataset_quality_manifest ON dataset_quality_results(manifest_id,policy_version,id);
