package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type datasetRepoFixture struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	repo        *DatasetRepo
	policy      *dataset.Policy
	artifact    *dataset.PolicyArtifact
	manifest    *dataset.Manifest
	clean       *dataset.QualityResult
	quarantined *dataset.QualityResult
	createdAt   time.Time
}

func newDatasetRepoFixture(t *testing.T) datasetRepoFixture {
	t.Helper()
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	for _, migration := range []string{
		"000070_accounting_dual_run.up.sql", "000071_common_execution_lifecycle.up.sql",
		"000072_simulation_policy_artifacts.up.sql", "000073_venue_adapter_observations.up.sql",
		"000074_capital_margin_profiles.up.sql", "000075_venue_reconciliation.up.sql",
		"000076_dataset_manifests_quality.up.sql",
	} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	economic := newEconomicLedgerFixture(t, ctx, pool, "dataset-evidence")
	policy, err := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 20, 22, 0, 0, 123456000, time.UTC)
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest := datasetRepositoryManifest(t, economic.instrument.ID)
	clean := datasetRepositoryQuality(t, policy, manifest, economic.instrument.ID, false)
	quarantined := datasetRepositoryQuality(t, policy, manifest, economic.instrument.ID, true)
	return datasetRepoFixture{
		ctx: ctx, pool: pool, repo: NewDatasetRepo(pool), policy: policy, artifact: artifact,
		manifest: manifest, clean: clean, quarantined: quarantined, createdAt: createdAt,
	}
}

func TestDatasetRepoPersistsReloadsRetriesAndRestartRecomputes(t *testing.T) {
	fixture := newDatasetRepoFixture(t)
	registered, err := fixture.repo.RegisterDatasetPolicy(fixture.ctx, fixture.artifact)
	if err != nil || registered.Version != fixture.artifact.Version {
		t.Fatalf("register policy = %+v, %v", registered, err)
	}
	storedManifest, err := fixture.repo.RecordDatasetManifest(fixture.ctx, fixture.manifest, fixture.createdAt)
	if err != nil || storedManifest.ID() != fixture.manifest.ID() || storedManifest.Digest() != fixture.manifest.Digest() {
		t.Fatalf("record manifest = %+v, %v", storedManifest, err)
	}
	storedClean, err := fixture.repo.RecordDatasetQualityResult(fixture.ctx, fixture.clean, fixture.createdAt)
	if err != nil || storedClean.ID() != fixture.clean.ID() || storedClean.Quarantined() {
		t.Fatalf("record clean result = %+v, %v", storedClean, err)
	}
	storedQuarantined, err := fixture.repo.RecordDatasetQualityResult(fixture.ctx, fixture.quarantined, fixture.createdAt)
	if err != nil || storedQuarantined.ID() != fixture.quarantined.ID() || !storedQuarantined.Quarantined() {
		t.Fatalf("record quarantined result = %+v, %v", storedQuarantined, err)
	}
	if _, err := fixture.repo.RegisterDatasetPolicy(fixture.ctx, fixture.artifact); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordDatasetManifest(fixture.ctx, fixture.manifest, fixture.createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordDatasetQualityResult(fixture.ctx, fixture.clean, fixture.createdAt); err != nil {
		t.Fatal(err)
	}

	restarted := NewDatasetRepo(fixture.pool)
	loadedManifest, err := restarted.GetDatasetManifest(fixture.ctx, fixture.manifest.ID())
	if err != nil || string(loadedManifest.CanonicalBytes()) != string(fixture.manifest.CanonicalBytes()) {
		t.Fatalf("restart manifest = %+v, %v", loadedManifest, err)
	}
	recomputed := datasetRepositoryQuality(t, fixture.policy, loadedManifest, datasetManifestInstrumentID(t, loadedManifest), false)
	if recomputed.ID() != fixture.clean.ID() || recomputed.Digest() != fixture.clean.Digest() {
		t.Fatalf("restart recomputation = %s/%s want %s/%s", recomputed.ID(), recomputed.Digest(), fixture.clean.ID(), fixture.clean.Digest())
	}
	loadedQuality, err := restarted.GetDatasetQualityResult(fixture.ctx, recomputed.ID())
	if err != nil || string(loadedQuality.CanonicalBytes()) != string(recomputed.CanonicalBytes()) {
		t.Fatalf("restart quality = %+v, %v", loadedQuality, err)
	}
}

func TestDatasetRepoConcurrentWritersConvergeCompleteGraphs(t *testing.T) {
	fixture := newDatasetRepoFixture(t)
	if _, err := fixture.repo.RegisterDatasetPolicy(fixture.ctx, fixture.artifact); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordDatasetManifest(fixture.ctx, fixture.manifest, fixture.createdAt)
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error(err)
		}
	}
	errorsFound = make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordDatasetQualityResult(fixture.ctx, fixture.clean, fixture.createdAt)
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error(err)
		}
	}
	partitions := fixture.manifest.Partitions()
	checks, findings := fixture.clean.Checks(), fixture.clean.Findings()
	var manifests, partitionRows, observationRows, qualityRows, checkRows, findingRows int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT count(*) FROM dataset_manifests WHERE id=$1),
		(SELECT count(*) FROM dataset_manifest_partitions WHERE manifest_id=$1),
		(SELECT count(*) FROM dataset_manifest_observations WHERE manifest_id=$1),
		(SELECT count(*) FROM dataset_quality_results WHERE id=$2),
		(SELECT count(*) FROM dataset_quality_checks WHERE result_id=$2),
		(SELECT count(*) FROM dataset_quality_findings WHERE result_id=$2)`, fixture.manifest.ID(), fixture.clean.ID()).Scan(
		&manifests, &partitionRows, &observationRows, &qualityRows, &checkRows, &findingRows); err != nil {
		t.Fatal(err)
	}
	wantObservations := 0
	for _, partition := range partitions {
		wantObservations += len(partition.Observations)
	}
	if manifests != 1 || partitionRows != len(partitions) || observationRows != wantObservations || qualityRows != 1 || checkRows != len(checks) || findingRows != len(findings) {
		t.Fatalf("graph counts=%d/%d/%d/%d/%d/%d", manifests, partitionRows, observationRows, qualityRows, checkRows, findingRows)
	}
}

func TestDatasetRepoInjectedStageFailuresRollbackEveryGraph(t *testing.T) {
	fixture := newDatasetRepoFixture(t)
	if _, err := fixture.repo.RegisterDatasetPolicy(fixture.ctx, fixture.artifact); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("injected stop")
	for _, stage := range []string{"manifest_parent", "manifest_partitions", "manifest_observations"} {
		fixture.repo.afterStage = func(actual string) error {
			if actual == stage {
				return stop
			}
			return nil
		}
		if _, err := fixture.repo.RecordDatasetManifest(fixture.ctx, fixture.manifest, fixture.createdAt); !errors.Is(err, stop) {
			t.Fatalf("stage %s error=%v", stage, err)
		}
		assertDatasetGraphCount(t, fixture, "dataset_manifests", fixture.manifest.ID(), 0)
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RecordDatasetManifest(fixture.ctx, fixture.manifest, fixture.createdAt); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"quality_parent", "quality_checks", "quality_findings"} {
		fixture.repo.afterStage = func(actual string) error {
			if actual == stage {
				return stop
			}
			return nil
		}
		if _, err := fixture.repo.RecordDatasetQualityResult(fixture.ctx, fixture.clean, fixture.createdAt); !errors.Is(err, stop) {
			t.Fatalf("stage %s error=%v", stage, err)
		}
		assertDatasetGraphCount(t, fixture, "dataset_quality_results", fixture.clean.ID(), 0)
	}
}

func TestDatasetRepoRejectsChangedPolicyRetry(t *testing.T) {
	fixture := newDatasetRepoFixture(t)
	if _, err := fixture.repo.RegisterDatasetPolicy(fixture.ctx, fixture.artifact); err != nil {
		t.Fatal(err)
	}
	changed := *fixture.artifact
	changed.CreatedAt = changed.CreatedAt.Add(time.Microsecond)
	if _, err := fixture.repo.RegisterDatasetPolicy(fixture.ctx, &changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed policy retry error=%v", err)
	}
}

func datasetRepositoryManifest(t *testing.T, instrumentID uuid.UUID) *dataset.Manifest {
	t.Helper()
	cutoff := time.Date(2026, 8, 20, 21, 0, 0, 123456000, time.UTC)
	published := cutoff.Add(-10 * time.Minute)
	decimalValue := func(value string) *string { return &value }
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: cutoff, Partitions: []dataset.PartitionInput{
		{
			Kind: dataset.KindBars, Provider: "golden-provider", Source: "bars-file", Namespace: "bars/AAPL/1m",
			RequestSHA256: strings.Repeat("1", 64), MediaType: "application/x-parquet", SymbologyVersion: "figi-v1",
			AdjustmentPolicy: "raw", Timezone: "America/New_York", Calendar: "XNYS", Revision: "r1",
			License: "test-only", RetentionPolicy: "retain-golden", Observations: []dataset.ObservationInput{
				{SourceKey: "bar-1", InstrumentID: instrumentID, EffectiveAt: cutoff.Add(-2 * time.Hour), PublishedAt: &published, ObservedAt: cutoff.Add(-9 * time.Minute), AvailableAt: cutoff.Add(-8 * time.Minute), Revision: "r1", ContentSHA256: strings.Repeat("2", 64), Volume: decimalValue("100")},
				{SourceKey: "bar-1-correction", InstrumentID: instrumentID, EffectiveAt: cutoff.Add(-2 * time.Hour), PublishedAt: &published, ObservedAt: cutoff.Add(-7 * time.Minute), AvailableAt: cutoff.Add(-6 * time.Minute), Revision: "r2", CorrectionOf: "bar-1", ContentSHA256: strings.Repeat("3", 64), Volume: decimalValue("101")},
			},
		},
		{
			Kind: dataset.KindQuotes, Provider: "golden-provider", Source: "quotes-api", Namespace: "quotes/AAPL",
			RequestSHA256: strings.Repeat("4", 64), MediaType: "application/json", SymbologyVersion: "figi-v1",
			AdjustmentPolicy: "not_applicable", Timezone: "UTC", Calendar: "XNYS", Revision: "r1",
			License: "test-only", RetentionPolicy: "retain-golden", Observations: []dataset.ObservationInput{
				{SourceKey: "quote-1", InstrumentID: instrumentID, EffectiveAt: cutoff.Add(-time.Hour), ObservedAt: cutoff.Add(-5 * time.Minute), AvailableAt: cutoff.Add(-4 * time.Minute), Revision: "r1", ContentSHA256: strings.Repeat("5", 64), Bid: decimalValue("10.25"), Ask: decimalValue("10.27")},
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func datasetRepositoryQuality(t *testing.T, policy *dataset.Policy, manifest *dataset.Manifest, instrumentID uuid.UUID, failCorporateAction bool) *dataset.QualityResult {
	t.Helper()
	windowStart := time.Date(2026, 8, 1, 0, 0, 0, 123456000, time.UTC)
	input := dataset.QualityInput{
		Policy: policy, Manifest: manifest,
		InstrumentWindows: []dataset.InstrumentWindow{{InstrumentID: instrumentID, ValidFrom: windowStart, EvidenceSHA256: strings.Repeat("6", 64)}},
	}
	for _, partition := range manifest.Partitions() {
		effective := make([]time.Time, 0, len(partition.Observations))
		seen := map[string]struct{}{}
		for _, observation := range partition.Observations {
			if _, ok := seen[observation.EffectiveAt]; ok {
				continue
			}
			seen[observation.EffectiveAt] = struct{}{}
			value, err := time.Parse("2006-01-02T15:04:05.000000Z", observation.EffectiveAt)
			if err != nil {
				t.Fatal(err)
			}
			effective = append(effective, value)
		}
		if partition.Kind == dataset.KindBars || partition.Kind == dataset.KindQuotes {
			input.Sessions = append(input.Sessions, dataset.SessionEvidence{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: effective, EvidenceSHA256: strings.Repeat("7", 64)})
			input.ExternalAssessments = append(input.ExternalAssessments, dataset.ExternalAssessment{PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckProviderSpotCompare, Status: dataset.CheckPassed, EvidenceSHA256: strings.Repeat("8", 64)})
		}
		if partition.Kind == dataset.KindBars {
			status := dataset.CheckPassed
			if failCorporateAction {
				status = dataset.CheckFailed
			}
			input.ExternalAssessments = append(input.ExternalAssessments, dataset.ExternalAssessment{PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckCorporateActions, Status: status, EvidenceSHA256: strings.Repeat("9", 64)})
		}
	}
	result, err := dataset.Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Quarantined() != failCorporateAction {
		t.Fatalf("quality quarantined=%t want=%t", result.Quarantined(), failCorporateAction)
	}
	return result
}

func datasetManifestInstrumentID(t *testing.T, manifest *dataset.Manifest) uuid.UUID {
	t.Helper()
	for _, partition := range manifest.Partitions() {
		for _, observation := range partition.Observations {
			if observation.InstrumentID != "" {
				id, err := uuid.Parse(observation.InstrumentID)
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
		}
	}
	t.Fatal("manifest has no instrument")
	return uuid.Nil
}

func assertDatasetGraphCount(t *testing.T, fixture datasetRepoFixture, table string, id uuid.UUID, want int) {
	t.Helper()
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM `+table+` WHERE id=$1`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s count=%d want=%d", table, count, want)
	}
}
