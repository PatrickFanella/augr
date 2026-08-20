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

	"github.com/PatrickFanella/get-rich-quick/internal/strategy/trend"
	trendqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/trend/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type trendRepositoryFixture struct {
	base     benchmarkFixture
	policy   *trend.Policy
	scenario *trend.Scenario
	report   *trend.Report
	repo     *TrendRepo
}

func newTrendRepositoryFixture(t *testing.T) trendRepositoryFixture {
	t.Helper()
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	pool := base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000084_momentum_quality_baseline.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000085_etf_time_series_trend.up.sql")); err != nil {
		t.Fatal(err)
	}
	fixture, err := trendqualification.Build(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err = instruments.CreateInstrument(ctx, fixture.Base.Instrument); err != nil {
		t.Fatal(err)
	}
	if _, err = instruments.RegisterVenueContract(ctx, fixture.Base.VenueContract); err != nil {
		t.Fatal(err)
	}
	return trendRepositoryFixture{base: base, policy: fixture.Policy, scenario: fixture.Scenario, report: fixture.Program.Report(), repo: NewTrendRepo(pool)}
}

func TestTrendRepositoryRoundTripEightWritersAndRestart(t *testing.T) {
	fixture := newTrendRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
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
	converge(func() error { _, err := fixture.repo.RegisterScenario(ctx, fixture.scenario); return err })
	if t.Failed() {
		return
	}
	converge(func() error { _, err := fixture.repo.RecordReport(ctx, fixture.report); return err })
	loaded, err := NewTrendRepo(fixture.base.evaluation.experiment.strategy.pool).GetReport(ctx, fixture.report.ID())
	if err != nil || loaded.Digest() != fixture.report.Digest() || !bytes.Equal(loaded.CanonicalBytes(), fixture.report.CanonicalBytes()) {
		t.Fatalf("trend reload=%v/%v", loaded, err)
	}
	var policies, scenarios, sources, members, horizons, reports, rebalances, signals, trades, holdings int
	err = fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM trend_v1_policies),(SELECT count(*) FROM trend_v1_scenarios),(SELECT count(*) FROM trend_v1_source_rebalances),(SELECT count(*) FROM trend_v1_universe_members),
		(SELECT count(*) FROM trend_v1_horizon_prices),(SELECT count(*) FROM trend_v1_reports),(SELECT count(*) FROM trend_v1_rebalances),(SELECT count(*) FROM trend_v1_signals),(SELECT count(*) FROM trend_v1_trades),(SELECT count(*) FROM trend_v1_holdings)`).
		Scan(&policies, &scenarios, &sources, &members, &horizons, &reports, &rebalances, &signals, &trades, &holdings)
	if err != nil || policies != 1 || scenarios != 1 || sources != 2 || members != 2 || horizons != 4 || reports != 1 || rebalances != 2 || signals != 2 || trades != 2 {
		t.Fatalf("trend counts=%d/%d/%d/%d/%d/%d/%d/%d/%d/%d err=%v", policies, scenarios, sources, members, horizons, reports, rebalances, signals, trades, holdings, err)
	}
}

func TestTrendRepositoryAtomicStagesAppendOnlyAndRollbackRefusal(t *testing.T) {
	fixture := newTrendRepositoryFixture(t)
	ctx := context.Background()
	pool := fixture.base.evaluation.experiment.strategy.pool
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	for _, failed := range []string{"trend_scenario", "trend_source_rebalance", "trend_universe_member", "trend_horizon_price"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RegisterScenario(ctx, fixture.scenario); err == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM trend_v1_scenarios`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, count, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RegisterScenario(ctx, fixture.scenario); err != nil {
		t.Fatal(err)
	}
	for _, failed := range []string{"trend_report", "trend_signal", "trend_trade", "trend_holding", "trend_rebalance"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RecordReport(ctx, fixture.report); err == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM trend_v1_reports`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, count, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RecordReport(ctx, fixture.report); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE trend_v1_rebalances SET cash='1.000000000000' WHERE report_id=$1 AND sequence=1`, fixture.report.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000085_etf_time_series_trend.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("nonempty rollback=%v", err)
	}
}

func TestTrendRepositoryRejectsNormalizedForgeryOnReload(t *testing.T) {
	fixture := newTrendRepositoryFixture(t)
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
	if _, err := pool.Exec(ctx, `ALTER TABLE trend_v1_trades DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE trend_v1_trades SET canonical_value=jsonb_set(canonical_value,'{cost}','"999"') WHERE report_id=$1 AND rebalance_sequence=0 AND sequence=0`, fixture.report.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE trend_v1_trades ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetReport(ctx, fixture.report.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("normalized forgery=%v", err)
	}
}

func TestTrendMigrationEmptyRollbackAndReapply(t *testing.T) {
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	pool := base.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000083_quality_filtered_wheel_v1.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000084_momentum_quality_baseline.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000085_etf_time_series_trend.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000085_etf_time_series_trend.down.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000085_etf_time_series_trend.up.sql")); err != nil {
		t.Fatal(err)
	}
}

func TestTrendRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("TREND_V1_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set TREND_V1_QUALIFICATION_DB_URL to a dedicated empty schema-85 database")
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
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 85 {
		t.Fatalf("qualification version=%d err=%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM trend_v1_policies)+(SELECT count(*) FROM trend_v1_scenarios)+(SELECT count(*) FROM trend_v1_reports)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("qualification database is not trend-empty count=%d err=%v", existing, err)
	}
	fixture, err := trendqualification.BuildRetainedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err = instruments.CreateInstrument(ctx, fixture.Base.Base.Instrument); err != nil {
		t.Fatal(err)
	}
	if _, err = instruments.RegisterVenueContract(ctx, fixture.Base.Base.VenueContract); err != nil {
		t.Fatal(err)
	}
	repo := NewTrendRepo(pool)
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
	var policies, scenarios, sources, members, horizons, reports, rebalances, signals, trades, holdings int
	err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM trend_v1_policies),(SELECT count(*) FROM trend_v1_scenarios),(SELECT count(*) FROM trend_v1_source_rebalances),(SELECT count(*) FROM trend_v1_universe_members),(SELECT count(*) FROM trend_v1_horizon_prices),(SELECT count(*) FROM trend_v1_reports),(SELECT count(*) FROM trend_v1_rebalances),(SELECT count(*) FROM trend_v1_signals),(SELECT count(*) FROM trend_v1_trades),(SELECT count(*) FROM trend_v1_holdings)`).Scan(&policies, &scenarios, &sources, &members, &horizons, &reports, &rebalances, &signals, &trades, &holdings)
	if err != nil || policies != 1 || scenarios != 5 || sources != 11 || members != 11 || horizons != 22 || reports != 5 || rebalances != 11 || signals != 11 || trades != 9 || holdings != 8 {
		t.Fatalf("retained counts=%d/%d/%d/%d/%d/%d/%d/%d/%d/%d err=%v", policies, scenarios, sources, members, horizons, reports, rebalances, signals, trades, holdings, err)
	}
}
