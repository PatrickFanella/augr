-- OVR-606 additive cost evidence. No ledger, promotion, scheduler, deployment,
-- provider, risk, allocation, settlement, or execution authority is granted.

CREATE FUNCTION full_cost_ledger_evidence_sha256(target UUID) RETURNS TEXT AS $$
  SELECT encode(digest(
    t.id::TEXT || '|' || t.account_id::TEXT || '|' || t.event_type || '|' ||
    to_char(t.effective_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') || '|' ||
    COALESCE((SELECT string_agg(p.id::TEXT || ':' || p.ledger_account || ':' || p.unit_kind || ':' || p.unit || ':' || trim_scale(p.amount)::TEXT, ',' ORDER BY p.id::TEXT) FROM ledger_postings p WHERE p.transaction_id=t.id),'')
  ,'sha256'),'hex') FROM ledger_transactions t WHERE t.id=target;
$$ LANGUAGE SQL STABLE STRICT;

CREATE TABLE full_cost_attribution_reports (
  id UUID PRIMARY KEY,
  schema_name TEXT NOT NULL CHECK(schema_name='full-cost-attribution-report-v1'),
  case_id UUID NOT NULL REFERENCES evidence_review_cases(id) ON DELETE RESTRICT,
  case_sha256 TEXT NOT NULL CHECK(case_sha256~'^[0-9a-f]{64}$'),
  summary_id UUID NOT NULL UNIQUE REFERENCES evidence_review_summaries(id) ON DELETE RESTRICT,
  summary_sha256 TEXT NOT NULL CHECK(summary_sha256~'^[0-9a-f]{64}$'),
  hypothesis_id UUID NOT NULL REFERENCES research_hypotheses(id) ON DELETE RESTRICT,
  hypothesis_sha256 TEXT NOT NULL CHECK(hypothesis_sha256~'^[0-9a-f]{64}$'),
  manifest_id UUID NOT NULL REFERENCES dataset_manifests(id) ON DELETE RESTRICT,
  manifest_sha256 TEXT NOT NULL CHECK(manifest_sha256~'^[0-9a-f]{64}$'),
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  window_start TIMESTAMPTZ NOT NULL CHECK(window_start=date_trunc('microseconds',window_start)),
  window_end TIMESTAMPTZ NOT NULL CHECK(window_end=date_trunc('microseconds',window_end) AND window_end>window_start),
  statement_at TIMESTAMPTZ NOT NULL CHECK(statement_at=date_trunc('microseconds',statement_at) AND statement_at>=window_end),
  currency TEXT NOT NULL CHECK(currency~'^[A-Z]{3}$'),
  line_count INT NOT NULL CHECK(line_count BETWEEN 5 AND 256),
  actual_costs TEXT NOT NULL,
  estimated_costs TEXT NOT NULL,
  actual_rebates TEXT NOT NULL,
  estimated_rebates TEXT NOT NULL,
  known_net_cost TEXT NOT NULL,
  unknown_count INT NOT NULL CHECK(unknown_count BETWEEN 0 AND 256),
  coverage TEXT NOT NULL CHECK(coverage IN('complete_actual','complete_with_estimates','incomplete_unknown')),
  sha256 TEXT NOT NULL CHECK(sha256~'^[0-9a-f]{64}$'),
  canonical_bytes BYTEA NOT NULL,
  canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds',clock_timestamp()),
  CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
  CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
  CHECK(canonical_json->>'schema'=schema_name),
  CHECK(canonical_json->>'case_id'=case_id::TEXT AND canonical_json->>'case_sha256'=case_sha256),
  CHECK(canonical_json->>'summary_id'=summary_id::TEXT AND canonical_json->>'summary_sha256'=summary_sha256),
  CHECK(canonical_json->>'hypothesis_id'=hypothesis_id::TEXT AND canonical_json->>'hypothesis_sha256'=hypothesis_sha256),
  CHECK(canonical_json->>'manifest_id'=manifest_id::TEXT AND canonical_json->>'manifest_sha256'=manifest_sha256),
  CHECK(canonical_json->>'account_id'=account_id::TEXT AND canonical_json->>'currency'=currency),
  CHECK(canonical_json->>'window_start'=to_char(window_start AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
  CHECK(canonical_json->>'window_end'=to_char(window_end AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
  CHECK(canonical_json->>'statement_at'=to_char(statement_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
  CHECK(id=economic_deterministic_uuid('full-cost-attribution-report',schema_name||'@sha256:'||sha256))
);

CREATE TABLE full_cost_attribution_lines (
  report_id UUID NOT NULL REFERENCES full_cost_attribution_reports(id) ON DELETE RESTRICT,
  sequence INT NOT NULL CHECK(sequence BETWEEN 0 AND 255),
  line_key TEXT NOT NULL CHECK(line_key~'^[a-z0-9][a-z0-9_./:-]{0,191}$'),
  category TEXT NOT NULL CHECK(category IN('model','data','fee','rebate','infrastructure')),
  knowledge_status TEXT NOT NULL CHECK(knowledge_status IN('actual','estimated','unknown')),
  amount TEXT NOT NULL,
  evidence_kind TEXT NOT NULL,
  evidence_id UUID,
  evidence_sha256 TEXT NOT NULL,
  method TEXT NOT NULL,
  method_sha256 TEXT NOT NULL,
  explanation TEXT NOT NULL CHECK(explanation<>'' AND explanation=btrim(explanation) AND length(explanation)<=2048),
  canonical_row JSONB NOT NULL CHECK(jsonb_typeof(canonical_row)='object'),
  PRIMARY KEY(report_id,sequence),
  UNIQUE(report_id,line_key),
  CHECK((knowledge_status='unknown' AND amount='' AND evidence_kind='' AND evidence_id IS NULL AND evidence_sha256='' AND method='' AND method_sha256='') OR
        (knowledge_status='actual' AND amount~'^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$' AND evidence_kind~'^[a-z0-9][a-z0-9_./:-]{0,191}$' AND evidence_id IS NOT NULL AND evidence_sha256~'^[0-9a-f]{64}$' AND method='' AND method_sha256='') OR
        (knowledge_status='estimated' AND amount~'^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$' AND evidence_kind~'^[a-z0-9][a-z0-9_./:-]{0,191}$' AND evidence_id IS NOT NULL AND evidence_sha256~'^[0-9a-f]{64}$' AND method~'^[a-z0-9][a-z0-9_./:-]{0,191}$' AND method_sha256~'^[0-9a-f]{64}$'))
);

CREATE FUNCTION reject_full_cost_attribution_mutation() RETURNS TRIGGER AS $$
BEGIN RAISE EXCEPTION 'full cost attribution evidence is append-only'; END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_full_cost_attribution_actual() RETURNS TRIGGER AS $$
DECLARE report full_cost_attribution_reports%ROWTYPE; transaction_row ledger_transactions%ROWTYPE; expected_amount TEXT; hypothesis_row research_hypotheses%ROWTYPE;
BEGIN
  SELECT * INTO report FROM full_cost_attribution_reports WHERE id=NEW.report_id;
  IF NEW.knowledge_status='actual' AND NEW.category='model' THEN
    SELECT * INTO hypothesis_row FROM research_hypotheses WHERE id=report.hypothesis_id;
    IF NEW.evidence_kind<>'research_hypothesis' OR NEW.evidence_id<>hypothesis_row.id OR NEW.evidence_sha256<>hypothesis_row.sha256 OR NEW.amount<>hypothesis_row.canonical_json->'provenance'->>'cost' OR report.currency<>hypothesis_row.canonical_json->'provenance'->>'currency' THEN RAISE EXCEPTION 'actual model cost does not match provenance'; END IF;
  ELSIF NEW.knowledge_status='actual' AND NEW.category IN('fee','rebate') THEN
    SELECT * INTO transaction_row FROM ledger_transactions WHERE id=NEW.evidence_id;
    IF NEW.category='fee' THEN SELECT trim_scale(COALESCE(sum(p.amount),0))::TEXT INTO expected_amount FROM ledger_postings p WHERE p.transaction_id=transaction_row.id AND p.ledger_account='expense:fees' AND p.unit_kind='currency' AND p.unit=report.currency;
    ELSE SELECT trim_scale(-COALESCE(sum(p.amount),0))::TEXT INTO expected_amount FROM ledger_postings p WHERE p.transaction_id=transaction_row.id AND p.ledger_account='income:rebates' AND p.unit_kind='currency' AND p.unit=report.currency; END IF;
    IF transaction_row.id IS NULL OR NEW.evidence_kind<>'ledger_transaction' OR transaction_row.account_id<>report.account_id OR transaction_row.effective_at<report.window_start OR transaction_row.effective_at>=report.window_end OR NEW.evidence_sha256<>full_cost_ledger_evidence_sha256(transaction_row.id) OR transaction_row.event_type<>(CASE WHEN NEW.category='fee' THEN 'cost.fee' ELSE 'cost.rebate' END) OR NEW.amount<>expected_amount THEN RAISE EXCEPTION 'actual fee or rebate does not match ledger postings'; END IF;
  END IF;
  RETURN NEW;
END; $$ LANGUAGE plpgsql;

CREATE FUNCTION validate_full_cost_attribution_graph() RETURNS TRIGGER AS $$
DECLARE target UUID; report full_cost_attribution_reports%ROWTYPE; actual_cost NUMERIC; estimated_cost NUMERIC; actual_rebate NUMERIC; estimated_rebate NUMERIC; unknowns INT; estimates INT;
BEGIN
  IF TG_TABLE_NAME='full_cost_attribution_reports' THEN target:=NEW.id; ELSE target:=NEW.report_id; END IF;
  SELECT * INTO report FROM full_cost_attribution_reports WHERE id=target;
  IF report.id IS NULL THEN RAISE EXCEPTION 'full cost attribution report is missing'; END IF;
  PERFORM 1 FROM evidence_review_cases c JOIN evidence_review_summaries s ON s.case_id=c.id JOIN research_hypotheses h ON h.id=c.hypothesis_id JOIN dataset_manifests m ON m.id=h.manifest_id
    WHERE c.id=report.case_id AND c.sha256=report.case_sha256 AND s.id=report.summary_id AND s.sha256=report.summary_sha256 AND h.id=report.hypothesis_id AND h.sha256=report.hypothesis_sha256 AND m.id=report.manifest_id AND m.sha256=report.manifest_sha256;
  IF NOT FOUND THEN RAISE EXCEPTION 'full cost attribution parent evidence does not match'; END IF;
  IF report.line_count<>(SELECT count(*) FROM full_cost_attribution_lines WHERE report_id=report.id)
    OR EXISTS(SELECT required.category FROM (VALUES('model'),('data'),('fee'),('rebate'),('infrastructure')) required(category) WHERE NOT EXISTS(SELECT 1 FROM full_cost_attribution_lines l WHERE l.report_id=report.id AND l.category=required.category))
    OR report.canonical_json->'lines'<>(SELECT COALESCE(jsonb_agg(canonical_row ORDER BY sequence),'[]'::JSONB) FROM full_cost_attribution_lines WHERE report_id=report.id)
  THEN RAISE EXCEPTION 'full cost attribution lines do not reconstruct'; END IF;
  SELECT COALESCE(sum(amount::NUMERIC) FILTER(WHERE category<>'rebate' AND knowledge_status='actual'),0),COALESCE(sum(amount::NUMERIC) FILTER(WHERE category<>'rebate' AND knowledge_status='estimated'),0),COALESCE(sum(amount::NUMERIC) FILTER(WHERE category='rebate' AND knowledge_status='actual'),0),COALESCE(sum(amount::NUMERIC) FILTER(WHERE category='rebate' AND knowledge_status='estimated'),0),count(*) FILTER(WHERE knowledge_status='unknown'),count(*) FILTER(WHERE knowledge_status='estimated') INTO actual_cost,estimated_cost,actual_rebate,estimated_rebate,unknowns,estimates FROM full_cost_attribution_lines WHERE report_id=report.id;
  IF report.actual_costs<>trim_scale(actual_cost)::TEXT OR report.estimated_costs<>trim_scale(estimated_cost)::TEXT OR report.actual_rebates<>trim_scale(actual_rebate)::TEXT OR report.estimated_rebates<>trim_scale(estimated_rebate)::TEXT OR report.known_net_cost<>trim_scale(actual_cost+estimated_cost-actual_rebate-estimated_rebate)::TEXT OR report.unknown_count<>unknowns OR report.coverage<>(CASE WHEN unknowns>0 THEN 'incomplete_unknown' WHEN estimates>0 THEN 'complete_with_estimates' ELSE 'complete_actual' END) OR report.canonical_json->'totals'<>jsonb_build_object('actual_costs',report.actual_costs,'estimated_costs',report.estimated_costs,'actual_rebates',report.actual_rebates,'estimated_rebates',report.estimated_rebates,'known_net_cost',report.known_net_cost,'unknown_count',report.unknown_count,'coverage',report.coverage) THEN RAISE EXCEPTION 'full cost attribution totals do not reconstruct'; END IF;
  RETURN NULL;
END; $$ LANGUAGE plpgsql;

DO $$ DECLARE table_name TEXT; BEGIN FOREACH table_name IN ARRAY ARRAY['full_cost_attribution_reports','full_cost_attribution_lines'] LOOP EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_full_cost_attribution_mutation()','trg_'||table_name||'_immutable',table_name); END LOOP; END $$;
CREATE TRIGGER trg_full_cost_attribution_actual BEFORE INSERT ON full_cost_attribution_lines FOR EACH ROW EXECUTE FUNCTION validate_full_cost_attribution_actual();
CREATE CONSTRAINT TRIGGER trg_full_cost_attribution_report AFTER INSERT ON full_cost_attribution_reports DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_full_cost_attribution_graph();
CREATE CONSTRAINT TRIGGER trg_full_cost_attribution_line AFTER INSERT ON full_cost_attribution_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_full_cost_attribution_graph();
CREATE INDEX idx_full_cost_attribution_account_window ON full_cost_attribution_reports(account_id,window_start,window_end,id);
