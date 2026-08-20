package migrations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestDatasetEvidenceMigrationDefinesLockedAppendOnlyGraph(t *testing.T) {
	raw := readMigrationFile(t, "000076_dataset_manifests_quality.up.sql")
	if first := firstExecutableMigrationSQL(raw); !strings.HasPrefix(first, "lock table projection_checkpoints") {
		t.Fatalf("migration 76 first executable SQL = %q", first)
	}
	sql := normalizeSQL(t, raw)
	for _, fragment := range []string{
		"create table dataset_quality_policy_artifacts",
		"create table dataset_manifests",
		"create table dataset_manifest_partitions",
		"create table dataset_manifest_observations",
		"create table dataset_quality_results",
		"create table dataset_quality_checks",
		"create table dataset_quality_findings",
		"create function validate_dataset_manifest_graph",
		"create function validate_dataset_quality_graph",
		"create function reject_dataset_evidence_mutation",
		"deferrable initially deferred",
		"dataset partition canonical graph does not reconstruct",
		"dataset manifest graph does not reconstruct",
		"dataset quality graph does not reconstruct",
		"available_at<=manifest_decision_cutoff",
		"economic_deterministic_uuid('dataset-manifest'",
		"economic_deterministic_uuid('dataset-quality-result'",
		"check_key=dataset_check_key(partition_content_sha256,check_code)",
		"unique(result_id,partition_content_sha256,check_code)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 76 is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"insert into dataset_", "grant insert", "grant update", "grant delete", "current_manifest",
		"current_policy", "create extension", "create schedule", "historical_ohlcv", "submit_order",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("migration 76 contains forbidden activation %q", forbidden)
		}
	}
}

func TestDatasetEvidenceSQLJSONEscapingMatchesGo(t *testing.T) {
	ctx, pool := newDatasetMigrationPool(t)
	value := "source:<A&B>\u2028line\u2029"
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT dataset_json_string($1)`, value).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("SQL JSON string differs from Go\nSQL: %s\n Go: %s", got, want)
	}
}

func TestDatasetEvidenceMigrationReconstructsManifestAndRejectsMutation(t *testing.T) {
	ctx, pool := newDatasetMigrationPool(t)
	manifest := migrationDatasetManifest(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	insertDatasetManifest(t, ctx, tx, manifest, time.Date(2026, 8, 20, 21, 0, 0, 123456000, time.UTC))
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit valid manifest graph: %v", err)
	}

	var storedDigest string
	if err := pool.QueryRow(ctx, `SELECT sha256 FROM dataset_manifests WHERE id=$1`, manifest.ID()).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	if storedDigest != manifest.Digest() {
		t.Fatalf("stored manifest digest=%s want=%s", storedDigest, manifest.Digest())
	}
	if _, err := pool.Exec(ctx, `UPDATE dataset_manifests SET created_at=created_at WHERE id=$1`, manifest.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("manifest mutation error = %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000076_dataset_manifests_quality.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 76") {
		t.Fatalf("nonempty rollback error = %v", err)
	}
}

func TestDatasetEvidenceMigrationRejectsForgedManifestGraph(t *testing.T) {
	ctx, pool := newDatasetMigrationPool(t)
	manifest := migrationDatasetManifest(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	insertDatasetManifest(t, ctx, tx, manifest, time.Date(2026, 8, 20, 21, 5, 0, 123456000, time.UTC))
	if _, err := tx.Exec(ctx, `UPDATE dataset_manifest_partitions SET content_sha256=$1 WHERE manifest_id=$2`, strings.Repeat("f", 64), manifest.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("child mutation error = %v", err)
	}
	_ = tx.Rollback(ctx)

	forged, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	insertDatasetManifest(t, ctx, forged, manifest, time.Date(2026, 8, 20, 21, 6, 0, 123456000, time.UTC))
	if _, err := forged.Exec(ctx, `INSERT INTO dataset_manifest_observations(
		manifest_id,manifest_decision_cutoff,partition_sequence,partition_content_sha256,sequence,source_key,instrument_id,
		effective_at,published_at,observed_at,available_at,revision,correction_of,content_sha256,bid,ask,volume,depth,canonical_bytes,canonical_json
	) SELECT manifest_id,manifest_decision_cutoff,partition_sequence,partition_content_sha256,99,source_key||'-forged',instrument_id,
		effective_at,published_at,observed_at,available_at,revision,correction_of,content_sha256,bid,ask,volume,depth,canonical_bytes,canonical_json
	FROM dataset_manifest_observations WHERE manifest_id=$1 LIMIT 1`, manifest.ID()); err == nil {
		if err := forged.Commit(ctx); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
			t.Fatalf("forged graph commit error = %v", err)
		}
	} else {
		_ = forged.Rollback(ctx)
	}
}

func TestDatasetEvidenceMigrationCyclesEmptyAndRefusesPolicyRollback(t *testing.T) {
	ctx, pool := newDatasetMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000076_dataset_manifests_quality.down.sql")); err != nil {
		t.Fatalf("empty rollback migration 76: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000076_dataset_manifests_quality.up.sql")); err != nil {
		t.Fatalf("reapply migration 76: %v", err)
	}
	policy, err := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(time.Date(2026, 8, 20, 21, 10, 0, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO dataset_quality_policy_artifacts(
		id,schema_name,policy_version,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, artifact.ID, artifact.Schema,
		artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt); err != nil {
		t.Fatalf("insert reviewed policy: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000076_dataset_manifests_quality.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 76") {
		t.Fatalf("nonempty policy rollback error = %v", err)
	}
}

func TestDatasetEvidenceMigrationRejectsMissingApplicableQualityCheck(t *testing.T) {
	ctx, pool := newDatasetMigrationPool(t)
	policy, err := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 20, 21, 8, 0, 123456000, time.UTC)
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest := migrationDatasetManifest(t)
	quality, err := dataset.Evaluate(dataset.QualityInput{Policy: policy, Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	checks := quality.Checks()
	omit := -1
	for index, check := range checks {
		if check.Status == dataset.CheckPassed {
			omit = index
			break
		}
	}
	if omit < 0 {
		t.Fatal("quality fixture has no passed check to omit")
	}
	checks = append(checks[:omit:omit], checks[omit+1:]...)
	forgedCanonical := struct {
		Schema        string                `json:"schema"`
		PolicyVersion string                `json:"policy_version"`
		ManifestID    string                `json:"manifest_id"`
		Quarantined   bool                  `json:"quarantined"`
		CheckCount    int                   `json:"check_count"`
		FindingCount  int                   `json:"finding_count"`
		Checks        []dataset.CheckResult `json:"checks"`
		Findings      []dataset.Finding     `json:"findings"`
	}{dataset.QualityResultSchemaV1, quality.PolicyVersion(), manifest.ID().String(), quality.Quarantined(), len(checks), len(quality.Findings()), checks, quality.Findings()}
	forgedBytes, err := json.Marshal(forgedCanonical)
	if err != nil {
		t.Fatal(err)
	}
	digestBytes := sha256.Sum256(forgedBytes)
	digest := hex.EncodeToString(digestBytes[:])
	resultID := economicid.DeterministicUUID("dataset-quality-result", dataset.QualityResultSchemaV1+"@sha256:"+digest)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_quality_policy_artifacts(
		id,schema_name,policy_version,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, artifact.ID, artifact.Schema,
		artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
	insertDatasetManifest(t, ctx, tx, manifest, createdAt)
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_quality_results(
		id,schema_name,policy_version,manifest_id,quarantined,check_count,finding_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,convert_from($9,'UTF8')::JSONB,$10)`, resultID, dataset.QualityResultSchemaV1,
		quality.PolicyVersion(), manifest.ID(), quality.Quarantined(), len(checks), len(quality.Findings()), digest, forgedBytes, createdAt); err != nil {
		t.Fatal(err)
	}
	for sequence, check := range checks {
		checkBytes, _ := json.Marshal(check)
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_quality_checks(
			result_id,policy_version,manifest_id,sequence,check_key,partition_content_sha256,kind,check_code,required,status,severity,evidence_sha256,canonical_bytes,canonical_json
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,convert_from($13,'UTF8')::JSONB)`, resultID, quality.PolicyVersion(), manifest.ID(),
			sequence, check.Key, check.PartitionContentSHA256, check.Kind, check.Check, check.Required, check.Status, check.Severity, check.EvidenceSHA256, checkBytes); err != nil {
			t.Fatal(err)
		}
	}
	for sequence, finding := range quality.Findings() {
		findingBytes, _ := json.Marshal(finding)
		evidenceBytes, _ := json.Marshal(finding.Evidence)
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_quality_findings(
			result_id,policy_version,manifest_id,sequence,finding_key,partition_content_sha256,check_code,finding_code,severity,evidence,canonical_bytes,canonical_json
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,convert_from($11,'UTF8')::JSONB)`, resultID, quality.PolicyVersion(), manifest.ID(), sequence,
			finding.Key, finding.PartitionContentSHA256, finding.Check, finding.Code, finding.Severity, string(evidenceBytes), findingBytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err == nil || !strings.Contains(err.Error(), "quality graph does not reconstruct") {
		t.Fatalf("missing applicable check commit error = %v", err)
	}
}

func TestDatasetEvidenceMigrationDefinesEmptyOnlyRollback(t *testing.T) {
	sql := normalizeSQL(t, readMigrationFile(t, "000076_dataset_manifests_quality.down.sql"))
	for _, fragment := range []string{
		"in access exclusive mode", "cannot roll back migration 76 while dataset evidence exists",
		"drop table dataset_quality_findings", "drop table dataset_quality_policy_artifacts",
		"drop function validate_dataset_quality_graph()", "drop function validate_dataset_manifest_graph()",
		"drop function reject_dataset_evidence_mutation()",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 76 rollback is missing %q", fragment)
		}
	}
	for _, preserved := range []string{"historical_ohlcv", "instruments", "quote_snapshots", "venue_reconciliation_runs"} {
		if strings.Contains(sql, "drop table "+preserved) {
			t.Errorf("migration 76 rollback must preserve %s", preserved)
		}
	}
}

func newDatasetMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	for _, migration := range []string{
		"000074_capital_margin_profiles.up.sql",
		"000075_venue_reconciliation.up.sql",
		"000076_dataset_manifests_quality.up.sql",
	} {
		if _, err := pool.Exec(ctx, readMigrationFile(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return ctx, pool
}

func migrationDatasetManifest(t *testing.T) *dataset.Manifest {
	t.Helper()
	cutoff := time.Date(2026, 8, 20, 20, 0, 0, 123456000, time.UTC)
	published := cutoff.Add(-5 * time.Minute)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: cutoff, Partitions: []dataset.PartitionInput{{
		Kind: dataset.KindCorporateActions, Provider: "provider<&>", Source: "fixture", Namespace: "actions",
		RequestSHA256: strings.Repeat("1", 64), MediaType: "application/json", SymbologyVersion: "v1",
		AdjustmentPolicy: "raw", Timezone: "UTC", Calendar: "continuous", Revision: "r1",
		License: "test-only", RetentionPolicy: "retain-test", Observations: []dataset.ObservationInput{
			{SourceKey: "original", EffectiveAt: cutoff.Add(-time.Hour), PublishedAt: &published, ObservedAt: cutoff.Add(-4 * time.Minute), AvailableAt: cutoff.Add(-3 * time.Minute), Revision: "r1", ContentSHA256: strings.Repeat("2", 64)},
			{SourceKey: "correction", EffectiveAt: cutoff.Add(-time.Hour), PublishedAt: &published, ObservedAt: cutoff.Add(-2 * time.Minute), AvailableAt: cutoff.Add(-time.Minute), Revision: "r2", CorrectionOf: "original", ContentSHA256: strings.Repeat("3", 64)},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func insertDatasetManifest(t *testing.T, ctx context.Context, tx pgx.Tx, manifest *dataset.Manifest, createdAt time.Time) {
	t.Helper()
	partitions := manifest.Partitions()
	observationCount := 0
	for _, partition := range partitions {
		observationCount += len(partition.Observations)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_manifests(
		id,schema_name,decision_cutoff,partition_count,observation_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::JSONB,$8)`, manifest.ID(), dataset.ManifestSchemaV1,
		manifest.DecisionCutoff(), len(partitions), observationCount, manifest.Digest(), []byte(manifest.CanonicalBytes()), createdAt); err != nil {
		t.Fatal(err)
	}
	for _, partition := range partitions {
		partitionBytes, err := json.Marshal(partition)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_manifest_partitions(
			manifest_id,manifest_decision_cutoff,sequence,kind,provider,source_name,namespace,request_sha256,content_sha256,media_type,
			effective_start,effective_end,observed_start,observed_end,available_start,available_end,symbology_version,adjustment_policy,
			timezone_name,calendar_name,revision,supersedes_content_sha256,row_count,license_name,retention_policy,canonical_bytes,canonical_json
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,convert_from($26,'UTF8')::JSONB)`,
			manifest.ID(), manifest.DecisionCutoff(), partition.Sequence, partition.Kind, partition.Provider, partition.Source,
			partition.Namespace, partition.RequestSHA256, partition.ContentSHA256, partition.MediaType, partition.EffectiveStart,
			partition.EffectiveEnd, partition.ObservedStart, partition.ObservedEnd, partition.AvailableStart, partition.AvailableEnd,
			partition.SymbologyVersion, partition.AdjustmentPolicy, partition.Timezone, partition.Calendar, partition.Revision,
			partition.SupersedesContentSHA256, partition.RowCount, partition.License, partition.RetentionPolicy, partitionBytes); err != nil {
			t.Fatal(err)
		}
		for _, observation := range partition.Observations {
			observationBytes, err := json.Marshal(observation)
			if err != nil {
				t.Fatal(err)
			}
			var instrumentID any
			if observation.InstrumentID != "" {
				instrumentID = observation.InstrumentID
			}
			var publishedAt any
			if observation.PublishedAt != "" {
				publishedAt = observation.PublishedAt
			}
			if _, err := tx.Exec(ctx, `INSERT INTO dataset_manifest_observations(
				manifest_id,manifest_decision_cutoff,partition_sequence,partition_content_sha256,sequence,source_key,instrument_id,
				effective_at,published_at,observed_at,available_at,revision,correction_of,content_sha256,bid,ask,volume,depth,canonical_bytes,canonical_json
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,convert_from($19,'UTF8')::JSONB)`,
				manifest.ID(), manifest.DecisionCutoff(), partition.Sequence, partition.ContentSHA256, observation.Sequence,
				observation.SourceKey, instrumentID, observation.EffectiveAt, publishedAt, observation.ObservedAt,
				observation.AvailableAt, observation.Revision, observation.CorrectionOf, observation.ContentSHA256,
				observation.Bid, observation.Ask, observation.Volume, observation.Depth, observationBytes); err != nil {
				t.Fatal(err)
			}
		}
	}
}
