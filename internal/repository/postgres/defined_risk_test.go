package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk"
	definedriskqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type definedRiskRepositoryFixture struct {
	base    benchmarkFixture
	fixture *definedriskqualification.Fixture
	repo    *DefinedRiskRepo
}

func newDefinedRiskRepositoryFixture(t *testing.T) definedRiskRepositoryFixture {
	return newDefinedRiskRepositoryFixtureFor(t, definedrisk.ExecutionAtomic, definedrisk.BullCall, "10", "105")
}

func newDefinedRiskRepositoryFixtureFor(t *testing.T, execution definedrisk.ExecutionMode, strategy definedrisk.Strategy, shortDepth, terminal string) definedRiskRepositoryFixture {
	t.Helper()
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	pool := base.evaluation.experiment.strategy.pool
	for _, migration := range []string{"000083_quality_filtered_wheel_v1.up.sql", "000084_momentum_quality_baseline.up.sql", "000085_etf_time_series_trend.up.sql", "000086_defined_risk_options.up.sql"} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatal(err)
		}
	}
	fixture, err := definedriskqualification.Build(strategycatalog.ExperimentPaperScored, execution, strategy, shortDepth, terminal)
	if err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err = instruments.CreateInstrument(ctx, fixture.Underlying); err != nil {
		t.Fatal(err)
	}
	for i := range fixture.Options {
		if _, err = instruments.CreateInstrument(ctx, fixture.Options[i]); err != nil {
			t.Fatal(err)
		}
		if _, err = instruments.RegisterVenueContract(ctx, fixture.Contracts[i]); err != nil {
			t.Fatal(err)
		}
	}
	return definedRiskRepositoryFixture{base, fixture, NewDefinedRiskRepo(pool)}
}

func TestDefinedRiskRepositorySequentialOrphanRoundTrip(t *testing.T) {
	fixture := newDefinedRiskRepositoryFixtureFor(t, definedrisk.ExecutionSequential, definedrisk.BearPut, "0", "95")
	ctx := context.Background()
	if fixture.fixture.Report.Outcome() != "orphan_unwound" {
		t.Fatalf("outcome=%s", fixture.fixture.Report.Outcome())
	}
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.fixture.Policy); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterScenario(ctx, fixture.fixture.Scenario); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordReport(ctx, fixture.fixture.Report); err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.repo.GetReport(ctx, fixture.fixture.Report.ID())
	if err != nil || loaded.Outcome() != "orphan_unwound" || loaded.OrphanLoss() == "0.000000000000" {
		t.Fatalf("orphan reload=%v/%v", loaded, err)
	}
	var observations int
	if err = fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM defined_risk_v1_observations`).Scan(&observations); err != nil || observations != 3 {
		t.Fatalf("observations=%d err=%v", observations, err)
	}
}

func TestDefinedRiskRepositoryRoundTripEightWritersAndRestart(t *testing.T) {
	fixture := newDefinedRiskRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.fixture.Policy); err != nil {
		t.Fatal(err)
	}
	converge := func(write func() error) {
		t.Helper()
		var wait sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wait.Add(1)
			go func() { defer wait.Done(); errs <- write() }()
		}
		wait.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Error(err)
			}
		}
	}
	converge(func() error { _, err := fixture.repo.RegisterScenario(ctx, fixture.fixture.Scenario); return err })
	if t.Failed() {
		return
	}
	converge(func() error { _, err := fixture.repo.RecordReport(ctx, fixture.fixture.Report); return err })
	loaded, err := NewDefinedRiskRepo(fixture.base.evaluation.experiment.strategy.pool).GetReport(ctx, fixture.fixture.Report.ID())
	if err != nil || loaded.Digest() != fixture.fixture.Report.Digest() || !bytes.Equal(loaded.CanonicalBytes(), fixture.fixture.Report.CanonicalBytes()) {
		t.Fatalf("defined-risk reload=%v/%v", loaded, err)
	}
	var policies, scenarios, legs, observations, reports, fills int
	err = fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM defined_risk_v1_policies),(SELECT count(*) FROM defined_risk_v1_scenarios),(SELECT count(*) FROM defined_risk_v1_legs),(SELECT count(*) FROM defined_risk_v1_observations),(SELECT count(*) FROM defined_risk_v1_reports),(SELECT count(*) FROM defined_risk_v1_fills)`).Scan(&policies, &scenarios, &legs, &observations, &reports, &fills)
	if err != nil || policies != 1 || scenarios != 1 || legs != 2 || observations != 2 || reports != 1 || fills != 2 {
		t.Fatalf("defined-risk counts=%d/%d/%d/%d/%d/%d err=%v", policies, scenarios, legs, observations, reports, fills, err)
	}
}

func TestDefinedRiskRepositoryAtomicStagesAppendOnlyAndRollbackRefusal(t *testing.T) {
	fixture := newDefinedRiskRepositoryFixture(t)
	ctx := context.Background()
	pool := fixture.base.evaluation.experiment.strategy.pool
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.fixture.Policy); err != nil {
		t.Fatal(err)
	}
	for _, failed := range []string{"defined_risk_scenario", "defined_risk_leg", "defined_risk_observation"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RegisterScenario(ctx, fixture.fixture.Scenario); err == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM defined_risk_v1_scenarios`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, count, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RegisterScenario(ctx, fixture.fixture.Scenario); err != nil {
		t.Fatal(err)
	}
	for _, failed := range []string{"defined_risk_report", "defined_risk_fill"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RecordReport(ctx, fixture.fixture.Report); err == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM defined_risk_v1_reports`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, count, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RecordReport(ctx, fixture.fixture.Report); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE defined_risk_v1_fills SET price='999' WHERE report_id=$1 AND sequence=0`, fixture.fixture.Report.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000086_defined_risk_options.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("nonempty rollback=%v", err)
	}
}

func TestDefinedRiskRepositoryRejectsNormalizedForgeryOnReload(t *testing.T) {
	fixture := newDefinedRiskRepositoryFixture(t)
	ctx := context.Background()
	for _, write := range []func() error{func() error { _, err := fixture.repo.RegisterPolicy(ctx, fixture.fixture.Policy); return err }, func() error { _, err := fixture.repo.RegisterScenario(ctx, fixture.fixture.Scenario); return err }, func() error { _, err := fixture.repo.RecordReport(ctx, fixture.fixture.Report); return err }} {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
	pool := fixture.base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `ALTER TABLE defined_risk_v1_fills DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE defined_risk_v1_fills SET canonical_fill=jsonb_set(canonical_fill,'{price}','"999"') WHERE report_id=$1 AND sequence=0`, fixture.fixture.Report.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE defined_risk_v1_fills ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetReport(ctx, fixture.fixture.Report.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("normalized forgery=%v", err)
	}
}

func TestDefinedRiskMigrationEmptyRollbackAndReapply(t *testing.T) {
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	pool := base.evaluation.experiment.strategy.pool
	for _, migration := range []string{"000083_quality_filtered_wheel_v1.up.sql", "000084_momentum_quality_baseline.up.sql", "000085_etf_time_series_trend.up.sql", "000086_defined_risk_options.up.sql", "000086_defined_risk_options.down.sql", "000086_defined_risk_options.up.sql"} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatalf("%s: %v", migration, err)
		}
	}
}

func TestDefinedRiskRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("DEFINED_RISK_V1_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set DEFINED_RISK_V1_QUALIFICATION_DB_URL to a dedicated empty schema-86 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var schema string
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema != "public" {
		t.Fatalf("qualification schema=%q err=%v", schema, err)
	}
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 86 {
		t.Fatalf("qualification version=%d err=%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM defined_risk_v1_policies)+(SELECT count(*) FROM defined_risk_v1_scenarios)+(SELECT count(*) FROM defined_risk_v1_reports)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("qualification database is not defined-risk-empty count=%d err=%v", existing, err)
	}
	values, err := definedriskqualification.BuildRetainedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	instruments := NewInstrumentRepo(pool)
	repo := NewDefinedRiskRepo(pool)
	for _, name := range names {
		fixture := values[name]
		if _, err = instruments.CreateInstrument(ctx, fixture.Underlying); err != nil {
			t.Fatalf("%s underlying: %v", name, err)
		}
		for i := range fixture.Options {
			if _, err = instruments.CreateInstrument(ctx, fixture.Options[i]); err != nil {
				t.Fatalf("%s option: %v", name, err)
			}
			if _, err = instruments.RegisterVenueContract(ctx, fixture.Contracts[i]); err != nil {
				t.Fatalf("%s contract: %v", name, err)
			}
		}
		if _, err = repo.RegisterPolicy(ctx, fixture.Policy); err != nil {
			t.Fatalf("%s policy: %v", name, err)
		}
		if _, err = repo.RegisterScenario(ctx, fixture.Scenario); err != nil {
			t.Fatalf("%s scenario: %v", name, err)
		}
		if _, err = repo.RecordReport(ctx, fixture.Report); err != nil {
			t.Fatalf("%s report: %v", name, err)
		}
		t.Logf("%s scenario=%s sha=%s report=%s sha=%s outcome=%s", name, fixture.Scenario.ID(), fixture.Scenario.Digest(), fixture.Report.ID(), fixture.Report.Digest(), fixture.Report.Outcome())
	}
	var policies, scenarios, legs, observations, reports, fills int
	err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM defined_risk_v1_policies),(SELECT count(*) FROM defined_risk_v1_scenarios),(SELECT count(*) FROM defined_risk_v1_legs),(SELECT count(*) FROM defined_risk_v1_observations),(SELECT count(*) FROM defined_risk_v1_reports),(SELECT count(*) FROM defined_risk_v1_fills)`).Scan(&policies, &scenarios, &legs, &observations, &reports, &fills)
	if err != nil || policies != 2 || scenarios != 7 || legs != 14 || observations != 16 || reports != 7 || fills != 12 {
		t.Fatalf("retained counts=%d/%d/%d/%d/%d/%d err=%v", policies, scenarios, legs, observations, reports, fills, err)
	}
}
