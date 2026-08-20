package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type strategyCatalogFixture struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	repo        *StrategyCatalogRepo
	family      *strategycatalog.Family
	version     *strategycatalog.Version
	manifest    *dataset.Manifest
	clean       *dataset.QualityResult
	quarantined *dataset.QualityResult
	account     *domain.Account
	binding     *capital.Binding
	simulation  string
	capital     string
}

func newStrategyCatalogFixture(t *testing.T) strategyCatalogFixture {
	t.Helper()
	ctx := context.Background()
	pool := newStrategyCatalogMigrationPool(t)
	return newStrategyCatalogFixtureWithPool(t, ctx, pool)
}

func newStrategyCatalogFixtureWithPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool) strategyCatalogFixture {
	t.Helper()
	economic := newEconomicLedgerFixture(t, ctx, pool, "strategy-catalog")

	capitalArtifact := newCapitalPolicyArtifact(t)
	capitalRepo := NewCapitalPolicyRepo(pool)
	if _, err := capitalRepo.RegisterCapitalPolicy(ctx, capitalArtifact); err != nil {
		t.Fatal(err)
	}
	capitalPolicy, err := capital.PolicyFromArtifact(*capitalArtifact)
	if err != nil {
		t.Fatal(err)
	}
	account := newCapitalPolicyAccount(t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000), domain.MarginProfileRegT, decimal.NewFromInt(2), 302)
	if err := NewAccountRepo(pool).Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	binding, err := capital.NewBinding(*account, capitalPolicy, account.StartingCapital, account.MarginProfile, capitalPolicyTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capitalRepo.BindCapitalPolicy(ctx, binding); err != nil {
		t.Fatal(err)
	}

	simulationArtifact := newSimulationPolicyArtifact(t, "1.25")
	if _, err := NewSimulationPolicyRepo(pool).RegisterSimulationPolicy(ctx, simulationArtifact); err != nil {
		t.Fatal(err)
	}

	policy, err := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 20, 20, 0, 0, 123456000, time.UTC)
	policyArtifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest := datasetRepositoryManifest(t, economic.instrument.ID)
	clean := datasetRepositoryQuality(t, policy, manifest, economic.instrument.ID, false)
	quarantined := datasetRepositoryQuality(t, policy, manifest, economic.instrument.ID, true)
	datasetRepo := NewDatasetRepo(pool)
	if _, err := datasetRepo.RegisterDatasetPolicy(ctx, policyArtifact); err != nil {
		t.Fatal(err)
	}
	if _, err := datasetRepo.RecordDatasetManifest(ctx, manifest, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := datasetRepo.RecordDatasetQualityResult(ctx, clean, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := datasetRepo.RecordDatasetQualityResult(ctx, quarantined, createdAt); err != nil {
		t.Fatal(err)
	}

	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{
		Slug: "cross-sectional-momentum", Name: "Cross-sectional momentum",
		Thesis:       "Rank liquid instruments by point-in-time momentum evidence.",
		AssetClasses: []instrument.AssetClass{instrument.AssetClassETF, instrument.AssetClassEquity},
	})
	if err != nil {
		t.Fatal(err)
	}
	version := strategyCatalogVersion(t, family.ID(), json.RawMessage(`{"lookback_sessions":20,"rebalance":"daily"}`), dataset.KindBars, dataset.KindQuotes)
	return strategyCatalogFixture{
		ctx: ctx, pool: pool, repo: NewStrategyCatalogRepo(pool), family: family, version: version,
		manifest: manifest, clean: clean, quarantined: quarantined, account: account, binding: binding,
		simulation: simulationArtifact.Version, capital: capitalArtifact.Version,
	}
}

func TestStrategyCatalogRepoRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("STRATEGY_CATALOG_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set STRATEGY_CATALOG_QUALIFICATION_DB_URL to a dedicated empty schema-77 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var schema string
	var version, existing int
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema != "public" {
		t.Fatalf("qualification schema=%q err=%v", schema, err)
	}
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 77 {
		t.Fatalf("qualification version=%d err=%v", version, err)
	}
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM strategy_families)+(SELECT count(*) FROM strategy_versions)+
		(SELECT count(*) FROM research_experiments)+(SELECT count(*) FROM strategy_deployments)+
		(SELECT count(*) FROM legacy_strategy_family_mappings)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("qualification database is not catalog-empty: count=%d err=%v", existing, err)
	}
	fixture := newStrategyCatalogFixtureWithPool(t, ctx, pool)
	if _, err := fixture.repo.RegisterStrategyFamily(ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterStrategyVersion(ctx, fixture.version); err != nil {
		t.Fatal(err)
	}
	second := strategyCatalogVersion(t, fixture.family.ID(), json.RawMessage(`{"lookback_sessions":60,"rebalance":"daily"}`), dataset.KindBars, dataset.KindQuotes)
	if _, err := fixture.repo.RegisterStrategyVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	scored := fixture.scoredExperiment(t, fixture.version, fixture.clean)
	if _, err := fixture.repo.DeclareResearchExperiment(ctx, scored); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.ProposeStrategyDeployment(ctx, fixture.deployment(t, fixture.version)); err != nil {
		t.Fatal(err)
	}

	capitalArtifact, err := NewCapitalPolicyRepo(pool).GetCapitalPolicyByVersion(ctx, fixture.capital)
	if err != nil {
		t.Fatal(err)
	}
	capitalPolicy, err := capital.PolicyFromArtifact(*capitalArtifact)
	if err != nil {
		t.Fatal(err)
	}
	stressAccount := newCapitalPolicyAccount(t, domain.AccountEnvironmentPaperStress, decimal.NewFromInt(5_000_000), domain.MarginProfileStressUnlimited, decimal.Zero, 303)
	if err := NewAccountRepo(pool).Create(ctx, stressAccount); err != nil {
		t.Fatal(err)
	}
	stressBinding, err := capital.NewBinding(*stressAccount, capitalPolicy, stressAccount.StartingCapital, stressAccount.MarginProfile, capitalPolicyTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCapitalPolicyRepo(pool).BindCapitalPolicy(ctx, stressBinding); err != nil {
		t.Fatal(err)
	}
	stress, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: second.ID(), AccountID: stressAccount.ID, CapitalBindingID: stressBinding.ID,
		ManifestID: fixture.manifest.ID(), QualityResultID: fixture.quarantined.ID(),
		SimulationPolicyVersion: fixture.simulation, CapitalPolicyVersion: fixture.capital,
		Mode: strategycatalog.ExperimentPaperStress, EvaluationStart: strategyCatalogEvaluationStart(),
		EvaluationEnd: strategyCatalogEvaluationStart().Add(time.Hour), Seed: 303, DatasetQuarantined: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.DeclareResearchExperiment(ctx, stress); err != nil {
		t.Fatal(err)
	}

	legacyID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO strategies(
		id,name,description,ticker,market_type,schedule_cron,config,status,skip_next_run,is_paper,is_active
	) VALUES($1,'Retained legacy strategy','OVR-302 retained evidence','AAPL','stock',NULL,'{}'::jsonb,'inactive',false,true,false)`, legacyID); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT strategy_legacy_snapshot_sha($1)`, legacyID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	mapping, err := strategycatalog.NewLegacyMapping(strategycatalog.LegacyMappingInput{
		LegacyStrategyID: legacyID, FamilyID: fixture.family.ID(), LegacySnapshotSHA256: snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.MapLegacyStrategyFamily(ctx, mapping); err != nil {
		t.Fatal(err)
	}

	for _, id := range []uuid.UUID{fixture.version.ID(), second.ID()} {
		if _, err := NewStrategyCatalogRepo(pool).GetStrategyVersion(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	var families, versions, kinds, experiments, deployments, mappings, events int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM strategy_families),(SELECT count(*) FROM strategy_versions),
		(SELECT count(*) FROM strategy_version_dataset_kinds),(SELECT count(*) FROM research_experiments),
		(SELECT count(*) FROM strategy_deployments),(SELECT count(*) FROM legacy_strategy_family_mappings),
		(SELECT count(*) FROM strategy_catalog_lifecycle_events)`).Scan(&families, &versions, &kinds, &experiments, &deployments, &mappings, &events); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained family=%s versions=%s,%s scored=%s stress=%s mapping=%s counts=%d/%d/%d/%d/%d/%d/%d",
		fixture.family.ID(), fixture.version.ID(), second.ID(), scored.ID(), stress.ID(), mapping.ID(),
		families, versions, kinds, experiments, deployments, mappings, events)
}

func TestStrategyCatalogRepoPersistsReloadsRetriesAndConfigVersions(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, fixture.version); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, fixture.version); err != nil {
		t.Fatal(err)
	}
	second := strategyCatalogVersion(t, fixture.family.ID(), json.RawMessage(`{"lookback_sessions":60,"rebalance":"daily"}`), dataset.KindBars, dataset.KindQuotes)
	if _, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, second); err != nil {
		t.Fatal(err)
	}
	if second.ID() == fixture.version.ID() || second.Digest() == fixture.version.Digest() {
		t.Fatal("config edit reused immutable version identity")
	}

	experiment := fixture.scoredExperiment(t, fixture.version, fixture.clean)
	if _, err := fixture.repo.DeclareResearchExperiment(fixture.ctx, experiment); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.DeclareResearchExperiment(fixture.ctx, experiment); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.deployment(t, fixture.version)
	if _, err := fixture.repo.ProposeStrategyDeployment(fixture.ctx, deployment); err != nil {
		t.Fatal(err)
	}

	restarted := NewStrategyCatalogRepo(fixture.pool)
	for name, got := range map[string][]byte{
		"family":         mustFamily(t, fixture.ctx, restarted, fixture.family.ID()).CanonicalBytes(),
		"version":        mustVersion(t, fixture.ctx, restarted, fixture.version.ID()).CanonicalBytes(),
		"second version": mustVersion(t, fixture.ctx, restarted, second.ID()).CanonicalBytes(),
		"experiment":     mustExperiment(t, fixture.ctx, restarted, experiment.ID()).CanonicalBytes(),
		"deployment":     mustDeployment(t, fixture.ctx, restarted, deployment.ID()).CanonicalBytes(),
	} {
		var want []byte
		switch name {
		case "family":
			want = fixture.family.CanonicalBytes()
		case "version":
			want = fixture.version.CanonicalBytes()
		case "second version":
			want = second.CanonicalBytes()
		case "experiment":
			want = experiment.CanonicalBytes()
		case "deployment":
			want = deployment.CanonicalBytes()
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s did not reload exactly", name)
		}
	}
	if deployment.State() != strategycatalog.DeploymentProposed || deployment.ActivationAuthority() != strategycatalog.DeploymentActivationAuthority {
		t.Fatal("deployment escaped inert proposal state")
	}
}

func TestStrategyCatalogRepoConcurrentVersionWritersConvergeCompleteGraph(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, fixture.version)
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
	var versions, kinds, events int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT count(*) FROM strategy_versions WHERE id=$1),
		(SELECT count(*) FROM strategy_version_dataset_kinds WHERE version_id=$1),
		(SELECT count(*) FROM strategy_catalog_lifecycle_events WHERE entity_kind='version' AND entity_id=$1)`, fixture.version.ID()).Scan(&versions, &kinds, &events); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || kinds != 2 || events != 1 {
		t.Fatalf("version graph counts=%d/%d/%d", versions, kinds, events)
	}
}

func TestStrategyCatalogRepoRejectsChangedStableFamilyRetry(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	changed, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{
		Slug: fixture.family.Slug(), Name: "Changed family name", Thesis: fixture.family.Thesis(),
		AssetClasses: fixture.family.AssetClasses(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() != fixture.family.ID() {
		t.Fatal("stable slug did not retain family identity")
	}
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed family retry error=%v", err)
	}
}

func TestStrategyCatalogRepoFailsClosedForExperimentEvidence(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, fixture.version); err != nil {
		t.Fatal(err)
	}
	missingKindVersion := strategyCatalogVersion(t, fixture.family.ID(), json.RawMessage(`{"lookback_sessions":20,"rebalance":"weekly"}`), dataset.KindBars, dataset.KindFilings)
	if _, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, missingKindVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.DeclareResearchExperiment(fixture.ctx, fixture.scoredExperiment(t, missingKindVersion, fixture.clean)); err == nil || !strings.Contains(err.Error(), "not admissible") {
		t.Fatalf("missing-kind experiment error=%v", err)
	}
	quarantineAsStress, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: fixture.version.ID(), AccountID: fixture.account.ID, CapitalBindingID: fixture.binding.ID,
		ManifestID: fixture.manifest.ID(), QualityResultID: fixture.quarantined.ID(),
		SimulationPolicyVersion: fixture.simulation, CapitalPolicyVersion: fixture.capital,
		Mode: strategycatalog.ExperimentPaperStress, EvaluationStart: strategyCatalogEvaluationStart(),
		EvaluationEnd: strategyCatalogEvaluationStart().Add(time.Hour), Seed: 302, DatasetQuarantined: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.DeclareResearchExperiment(fixture.ctx, quarantineAsStress); err == nil || !strings.Contains(err.Error(), "not admissible") {
		t.Fatalf("stress declaration on scored binding error=%v", err)
	}
	var experiments int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM research_experiments`).Scan(&experiments); err != nil || experiments != 0 {
		t.Fatalf("rejected experiment count=%d err=%v", experiments, err)
	}
}

func TestStrategyCatalogRepoInjectedFailuresRollbackGraphs(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("injected stop")
	for _, stage := range []string{"version_kinds", "version_parent", "lifecycle"} {
		fixture.repo.afterStage = func(actual string) error {
			if actual == stage {
				return stop
			}
			return nil
		}
		if _, err := fixture.repo.RegisterStrategyVersion(fixture.ctx, fixture.version); !errors.Is(err, stop) {
			t.Fatalf("stage %s error=%v", stage, err)
		}
		var rows int
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
			(SELECT count(*) FROM strategy_versions WHERE id=$1)+
			(SELECT count(*) FROM strategy_version_dataset_kinds WHERE version_id=$1)+
			(SELECT count(*) FROM strategy_catalog_lifecycle_events WHERE entity_kind='version' AND entity_id=$1)`, fixture.version.ID()).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("stage %s retained rows=%d err=%v", stage, rows, err)
		}
	}
}

func TestStrategyCatalogRepoMapsLegacySnapshotOnlyAsUnvalidated(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.RegisterStrategyFamily(fixture.ctx, fixture.family); err != nil {
		t.Fatal(err)
	}
	legacy := &domain.Strategy{ID: uuid.New()}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO strategies(
		id,name,description,ticker,market_type,schedule_cron,config,status,skip_next_run,is_paper,is_active
	) VALUES($1,'Legacy strategy','Mutable legacy evidence','AAPL','stock','0 14 * * 1-5',$2::jsonb,'inactive',false,true,false)`,
		legacy.ID, `{"lookback":20}`); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT strategy_legacy_snapshot_sha($1)`, legacy.ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	mapping, err := strategycatalog.NewLegacyMapping(strategycatalog.LegacyMappingInput{
		LegacyStrategyID: legacy.ID, FamilyID: fixture.family.ID(), LegacySnapshotSHA256: snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.repo.MapLegacyStrategyFamily(fixture.ctx, mapping)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStrategyCatalogRepo(fixture.pool).GetLegacyStrategyFamilyMapping(fixture.ctx, mapping.ID())
	if err != nil {
		t.Fatal(err)
	}
	if stored.State() != strategycatalog.LegacyUnvalidated || loaded.State() != strategycatalog.LegacyUnvalidated || !bytes.Equal(loaded.CanonicalBytes(), mapping.CanonicalBytes()) {
		t.Fatal("legacy mapping did not remain exact and unvalidated")
	}
	var versions, experiments int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT (SELECT count(*) FROM strategy_versions),(SELECT count(*) FROM research_experiments)`).Scan(&versions, &experiments); err != nil {
		t.Fatal(err)
	}
	if versions != 0 || experiments != 0 {
		t.Fatalf("legacy mapping implicitly created versions/experiments=%d/%d", versions, experiments)
	}
}

func strategyCatalogVersion(t *testing.T, familyID uuid.UUID, config json.RawMessage, kinds ...dataset.Kind) *strategycatalog.Version {
	t.Helper()
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{
		FamilyID: familyID, CompilerKind: "go-plugin", CompilerVersion: "go1.25.0",
		SourceCommit: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64),
		ConfigSchema: "cross-sectional-momentum-config-v1", Config: config,
		DecisionContract: "rank-and-target-v1", RequiredDatasetKinds: kinds,
	})
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func (fixture strategyCatalogFixture) scoredExperiment(t *testing.T, version *strategycatalog.Version, quality *dataset.QualityResult) *strategycatalog.Experiment {
	t.Helper()
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: version.ID(), AccountID: fixture.account.ID, CapitalBindingID: fixture.binding.ID,
		ManifestID: fixture.manifest.ID(), QualityResultID: quality.ID(), SimulationPolicyVersion: fixture.simulation,
		CapitalPolicyVersion: fixture.capital, Mode: strategycatalog.ExperimentPaperScored,
		EvaluationStart: strategyCatalogEvaluationStart(), EvaluationEnd: strategyCatalogEvaluationStart().Add(time.Hour), Seed: 302,
		DatasetQuarantined: quality.Quarantined(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return experiment
}

func (fixture strategyCatalogFixture) deployment(t *testing.T, version *strategycatalog.Version) *strategycatalog.Deployment {
	t.Helper()
	deployment, err := strategycatalog.NewDeployment(strategycatalog.DeploymentInput{
		VersionID: version.ID(), AccountID: fixture.account.ID, CapitalBindingID: fixture.binding.ID,
		Budget: "25000", ScheduleCron: "0 14 * * 1-5", Timezone: "America/New_York",
		RiskPolicyVersion: "common-risk-policy-v1@sha256:" + strings.Repeat("c", 64), Mode: strategycatalog.ExperimentPaperScored,
	})
	if err != nil {
		t.Fatal(err)
	}
	return deployment
}

func strategyCatalogEvaluationStart() time.Time {
	return time.Date(2026, 8, 20, 19, 0, 0, 123456000, time.UTC)
}

func mustFamily(t *testing.T, ctx context.Context, repo *StrategyCatalogRepo, id uuid.UUID) *strategycatalog.Family {
	t.Helper()
	value, err := repo.GetStrategyFamily(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustVersion(t *testing.T, ctx context.Context, repo *StrategyCatalogRepo, id uuid.UUID) *strategycatalog.Version {
	t.Helper()
	value, err := repo.GetStrategyVersion(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustExperiment(t *testing.T, ctx context.Context, repo *StrategyCatalogRepo, id uuid.UUID) *strategycatalog.Experiment {
	t.Helper()
	value, err := repo.GetResearchExperiment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustDeployment(t *testing.T, ctx context.Context, repo *StrategyCatalogRepo, id uuid.UUID) *strategycatalog.Deployment {
	t.Helper()
	value, err := repo.GetStrategyDeployment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStrategyCatalogRepoMissingReadsReturnNotFound(t *testing.T) {
	fixture := newStrategyCatalogFixture(t)
	if _, err := fixture.repo.GetStrategyFamily(fixture.ctx, uuid.New()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing family error=%v", err)
	}
}
