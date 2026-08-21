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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel"
	wheelqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type wheelRepositoryFixture struct {
	base     benchmarkFixture
	policy   *wheel.Policy
	scenario *wheel.Scenario
	report   *wheel.Report
	repo     *WheelRepo
}

func newWheelRepositoryFixture(t *testing.T) wheelRepositoryFixture {
	t.Helper()
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	if _, err := base.evaluation.experiment.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.up.sql")); err != nil {
		t.Fatal(err)
	}
	fixture, err := wheelqualification.Build(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(base.evaluation.experiment.strategy.pool)
	if _, err = instruments.CreateInstrument(ctx, fixture.Underlying); err != nil {
		t.Fatal(err)
	}
	if _, err = instruments.CreateInstrument(ctx, fixture.Option); err != nil {
		t.Fatal(err)
	}
	if _, err = instruments.RegisterVenueContract(ctx, fixture.VenueContract); err != nil {
		t.Fatal(err)
	}
	datasets := NewDatasetRepo(base.evaluation.experiment.strategy.pool)
	if _, err = datasets.RecordDatasetManifest(ctx, fixture.Graph.Manifest, wheelqualification.Start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	repo := NewWheelRepo(base.evaluation.experiment.strategy.pool)
	return wheelRepositoryFixture{base: base, policy: fixture.Policy, scenario: fixture.Scenario, report: fixture.Program.Report(), repo: repo}
}

func TestWheelRepositoryRoundTripEightWritersAndRestart(t *testing.T) {
	fixture := newWheelRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RegisterScenario(ctx, fixture.scenario)
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		return
	}
	errs = make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordReport(ctx, fixture.report)
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	restarted := NewWheelRepo(fixture.base.evaluation.experiment.strategy.pool)
	loaded, err := restarted.GetReport(ctx, fixture.report.ID())
	if err != nil || loaded.Digest() != fixture.report.Digest() || !bytes.Equal(loaded.CanonicalBytes(), fixture.report.CanonicalBytes()) {
		t.Fatalf("wheel reload=%v/%v", loaded, err)
	}
	var policies, scenarios, sources, reports, transitions, effects, selected int
	err = fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM wheel_v1_policies),(SELECT count(*) FROM wheel_v1_scenarios),(SELECT count(*) FROM wheel_v1_source_observations),
		(SELECT count(*) FROM wheel_v1_reports),(SELECT count(*) FROM wheel_v1_transitions),(SELECT count(*) FROM wheel_v1_economic_effects),(SELECT count(*) FROM wheel_v1_selected_contracts)`).
		Scan(&policies, &scenarios, &sources, &reports, &transitions, &effects, &selected)
	if err != nil || policies != 1 || scenarios != 1 || sources != 3 || reports != 1 || transitions != 3 || effects != 3 || selected != 2 {
		t.Fatalf("wheel counts=%d/%d/%d/%d/%d/%d/%d err=%v", policies, scenarios, sources, reports, transitions, effects, selected, err)
	}
}

func TestWheelRepositoryAtomicStagesAppendOnlyAndRollbackRefusal(t *testing.T) {
	fixture := newWheelRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	for _, failedStage := range []string{"wheel_scenario", "wheel_source_observation"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RegisterScenario(ctx, fixture.scenario); err == nil {
			t.Fatalf("stage %s accepted", failedStage)
		}
		var count int
		if err := fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM wheel_v1_scenarios`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial rows=%d/%v", failedStage, count, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RegisterScenario(ctx, fixture.scenario); err != nil {
		t.Fatal(err)
	}
	for _, failedStage := range []string{"wheel_report", "wheel_selected_contract", "wheel_economic_effect", "wheel_transition"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RecordReport(ctx, fixture.report); err == nil {
			t.Fatalf("partial report stage %s accepted", failedStage)
		}
		var reportCount int
		if err := fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM wheel_v1_reports`).Scan(&reportCount); err != nil || reportCount != 0 {
			t.Fatalf("partial report stage %s rows=%d/%v", failedStage, reportCount, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RecordReport(ctx, fixture.report); err != nil {
		t.Fatal(err)
	}
	pool := fixture.base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `UPDATE wheel_v1_transitions SET cash='1.000000000000' WHERE report_id=$1 AND sequence=1`, fixture.report.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only error=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("nonempty rollback=%v", err)
	}
}

func TestWheelRepositoryRejectsNormalizedForgeryOnReload(t *testing.T) {
	fixture := newWheelRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterScenario(ctx, fixture.scenario); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordReport(ctx, fixture.report); err != nil {
		t.Fatal(err)
	}
	pool := fixture.base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `ALTER TABLE wheel_v1_economic_effects DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE wheel_v1_economic_effects SET amount='999' WHERE report_id=$1 AND transition_sequence=1 AND effect_sequence=0`, fixture.report.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE wheel_v1_economic_effects ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetReport(ctx, fixture.report.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("normalized forgery reload=%v", err)
	}
}

func TestWheelMigrationEmptyRollbackAndReapply(t *testing.T) {
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	pool := base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.down.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.up.sql")); err != nil {
		t.Fatal(err)
	}
}

func TestWheelRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("WHEEL_V1_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set WHEEL_V1_QUALIFICATION_DB_URL to a dedicated empty schema-83 database")
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
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 83 {
		t.Fatalf("qualification version=%d err=%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM wheel_v1_policies)+(SELECT count(*) FROM wheel_v1_scenarios)+(SELECT count(*) FROM wheel_v1_reports)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("qualification database is not wheel-empty count=%d err=%v", existing, err)
	}
	fixture, err := wheelqualification.BuildLifecycleScenarios()
	if err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	for _, value := range []*instrument.Instrument{fixture.Underlying, fixture.Put, fixture.Call} {
		if _, err = instruments.CreateInstrument(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []*instrument.VenueContract{fixture.PutContract, fixture.CallContract} {
		if _, err = instruments.RegisterVenueContract(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = NewDatasetRepo(pool).RecordDatasetManifest(ctx, fixture.Manifest, fixture.CreatedAt); err != nil {
		t.Fatal(err)
	}
	repo := NewWheelRepo(pool)
	if _, err = repo.RegisterPolicy(ctx, fixture.Policy); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(fixture.Scenarios))
	for name := range fixture.Scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err = repo.RegisterScenario(ctx, fixture.Scenarios[name]); err != nil {
			t.Fatalf("persist scenario %s: %v", name, err)
		}
		if _, err = repo.RecordReport(ctx, fixture.Reports[name]); err != nil {
			t.Fatalf("persist report %s: %v", name, err)
		}
		t.Logf("%s scenario=%s sha=%s report=%s sha=%s", name, fixture.Scenarios[name].ID(), fixture.Scenarios[name].Digest(), fixture.Reports[name].ID(), fixture.Reports[name].Digest())
	}
	var policies, scenarios, sources, reports, transitions, effects, selected int
	err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM wheel_v1_policies),(SELECT count(*) FROM wheel_v1_scenarios),(SELECT count(*) FROM wheel_v1_source_observations),
		(SELECT count(*) FROM wheel_v1_reports),(SELECT count(*) FROM wheel_v1_transitions),(SELECT count(*) FROM wheel_v1_economic_effects),(SELECT count(*) FROM wheel_v1_selected_contracts)`).
		Scan(&policies, &scenarios, &sources, &reports, &transitions, &effects, &selected)
	if err != nil || policies != 1 || scenarios != 5 || sources != 22 || reports != 5 || transitions != 22 || effects != 32 || selected != 14 {
		t.Fatalf("retained counts=%d/%d/%d/%d/%d/%d/%d err=%v", policies, scenarios, sources, reports, transitions, effects, selected, err)
	}
}
