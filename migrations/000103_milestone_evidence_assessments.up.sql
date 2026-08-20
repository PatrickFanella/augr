-- Append-only OVR-702 through OVR-705 assessment evidence. This schema grants
-- no provider, scheduler, deployment, account, risk, order, or execution authority.
CREATE TABLE milestone_evidence_assessments(
 id UUID PRIMARY KEY,
 schema_name TEXT NOT NULL CHECK(schema_name='milestone-7-evidence-assessment-v1'),
 campaign TEXT NOT NULL CHECK(campaign IN ('shadow_30_day','scored_paper_60_90_day','portfolio_paper','architecture_readiness')),
 outcome TEXT NOT NULL CHECK(outcome IN ('qualified','rejected','held','ready','not_ready','blocked')),
 blocker_count INT NOT NULL CHECK(blocker_count>=0),
 parent_count INT NOT NULL CHECK(parent_count>=0),
 sha256 TEXT NOT NULL CHECK(sha256~'^[0-9a-f]{64}$'),
 canonical_bytes BYTEA NOT NULL,
 canonical_json JSONB NOT NULL CHECK(jsonb_typeof(canonical_json)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT date_trunc('microseconds',clock_timestamp()),
 CHECK(sha256=encode(digest(canonical_bytes,'sha256'),'hex')),
 CHECK(canonical_json=convert_from(canonical_bytes,'UTF8')::JSONB),
 CHECK(id=economic_deterministic_uuid('milestone-7-evidence',schema_name||'@sha256:'||sha256))
);

CREATE TABLE milestone_evidence_blockers(
 assessment_id UUID NOT NULL REFERENCES milestone_evidence_assessments(id),
 sequence INT NOT NULL CHECK(sequence>=0),
 blocker TEXT NOT NULL CHECK(blocker<>''),
 PRIMARY KEY(assessment_id,sequence),
 UNIQUE(assessment_id,blocker)
);

CREATE TABLE milestone_evidence_parents(
 assessment_id UUID NOT NULL REFERENCES milestone_evidence_assessments(id),
 sequence INT NOT NULL CHECK(sequence>=0),
 kind TEXT NOT NULL CHECK(kind~'^[a-z0-9_][a-z0-9_.-]{0,127}$'),
 evidence_id UUID NOT NULL,
 evidence_sha256 TEXT NOT NULL CHECK(evidence_sha256~'^[0-9a-f]{64}$'),
 PRIMARY KEY(assessment_id,sequence),
 UNIQUE(assessment_id,kind,evidence_id)
);

CREATE FUNCTION reject_milestone_evidence_mutation() RETURNS TRIGGER AS $$
BEGIN RAISE EXCEPTION 'milestone evidence is append-only';END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION validate_milestone_evidence_graph() RETURNS TRIGGER AS $$
DECLARE target UUID;a milestone_evidence_assessments%ROWTYPE;
BEGIN
 IF TG_TABLE_NAME='milestone_evidence_assessments' THEN target:=NEW.id;ELSE target:=NEW.assessment_id;END IF;
 SELECT * INTO a FROM milestone_evidence_assessments WHERE id=target;
 IF a.canonical_json->>'schema'<>a.schema_name OR a.canonical_json->>'campaign'<>a.campaign OR a.canonical_json->>'outcome'<>a.outcome
  OR a.blocker_count<>(SELECT count(*) FROM milestone_evidence_blockers WHERE assessment_id=a.id)
  OR a.parent_count<>(SELECT count(*) FROM milestone_evidence_parents WHERE assessment_id=a.id)
  OR a.canonical_json->'blockers'<>COALESCE((SELECT jsonb_agg(to_jsonb(x.blocker) ORDER BY x.sequence) FROM milestone_evidence_blockers x WHERE x.assessment_id=a.id),'[]'::jsonb)
  OR a.canonical_json->'parents'<>COALESCE((SELECT jsonb_agg(jsonb_build_object('kind',x.kind,'id',x.evidence_id::TEXT,'sha256',x.evidence_sha256) ORDER BY x.sequence) FROM milestone_evidence_parents x WHERE x.assessment_id=a.id),'[]'::jsonb)
  OR EXISTS(
   SELECT 1 FROM milestone_evidence_parents x
   WHERE x.assessment_id=a.id AND x.kind IN ('shadow_30_day','scored_paper_60_90_day','portfolio_paper','architecture_readiness')
   AND NOT EXISTS(SELECT 1 FROM milestone_evidence_assessments p WHERE p.id=x.evidence_id AND p.campaign=x.kind AND p.sha256=x.evidence_sha256)
  )
 THEN RAISE EXCEPTION 'milestone evidence graph does not reconstruct';END IF;
 RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DO $$DECLARE n TEXT;BEGIN
 FOREACH n IN ARRAY ARRAY['milestone_evidence_assessments','milestone_evidence_blockers','milestone_evidence_parents'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION reject_milestone_evidence_mutation()','trg_'||n||'_immutable',n);
 END LOOP;
END$$;

CREATE CONSTRAINT TRIGGER trg_milestone_evidence_assessment AFTER INSERT ON milestone_evidence_assessments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_milestone_evidence_graph();
CREATE CONSTRAINT TRIGGER trg_milestone_evidence_blocker AFTER INSERT ON milestone_evidence_blockers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_milestone_evidence_graph();
CREATE CONSTRAINT TRIGGER trg_milestone_evidence_parent AFTER INSERT ON milestone_evidence_parents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_milestone_evidence_graph();
CREATE INDEX idx_milestone_evidence_campaign ON milestone_evidence_assessments(campaign,created_at,id);
