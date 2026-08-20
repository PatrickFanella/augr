package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	fixture.repo.afterStage = func(stage string) error {
		if stage == "wheel_transition" {
			return errors.New("injected")
		}
		return nil
	}
	if _, err := fixture.repo.RecordReport(ctx, fixture.report); err == nil {
		t.Fatal("partial report accepted")
	}
	var reportCount int
	if err := fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM wheel_v1_reports`).Scan(&reportCount); err != nil || reportCount != 0 {
		t.Fatalf("partial report rows=%d/%v", reportCount, err)
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
