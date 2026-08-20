package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/benchmark"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
)

type benchmarkFixture struct {
	evaluation  evaluationFixture
	declaration *benchmark.Declaration
	report      *benchmark.Report
	repo        *BenchmarkRepo
}

func newBenchmarkFixture(t *testing.T) benchmarkFixture {
	t.Helper()
	fixture := newEvaluationFixture(t)
	ctx := fixture.experiment.strategy.ctx
	if _, err := fixture.experiment.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000082_passive_benchmark_control.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	experiment, err := fixture.experiment.strategy.repo.GetResearchExperiment(ctx, fixture.experiment.plan.ExperimentID())
	if err != nil {
		t.Fatal(err)
	}
	start, end := experiment.EvaluationStart(), experiment.EvaluationEnd()
	observations := make([]evaluation.ObservationInput, 6)
	benchmarkObservations := make([]benchmark.ObservationInput, 6)
	for i := range observations {
		at := start.Add(time.Duration(i) * time.Minute)
		value := []string{"100", "100.1", "100.2", "100.3", "100.4", "100.5"}[i]
		equity := []string{"100", "100.2", "100.1", "100.4", "100.3", "100.6"}[i]
		cash := "0.000001"
		if i == 0 {
			cash = "0"
		}
		evidenceID := uuid.NewSHA1(uuid.NameSpaceURL, []byte{byte(82), byte(i)})
		evidenceSHA := strings.Repeat(string(rune('1'+i)), 64)
		observations[i] = evaluation.ObservationInput{ObservedAt: at, Equity: equity, BenchmarkValue: value, CashReturn: cash, GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: evidenceID, EvidenceSHA256: evidenceSHA}
		benchmarkObservations[i] = benchmark.ObservationInput{ObservedAt: at, Value: value, CashReturn: cash, EvidenceID: evidenceID, EvidenceSHA256: evidenceSHA}
	}
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{Version: "evaluation-policy-v1@benchmark-repository", Frequency: "minute", PeriodsPerYear: 98280, ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12})
	if err != nil {
		t.Fatal(err)
	}
	evaluationReport, err := evaluation.NewReport(evaluation.ReportInput{Result: fixture.experiment.result, Policy: policy, EvaluationStart: start, EvaluationEnd: end, Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}, Observations: observations})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.repo.RegisterPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.repo.RecordEvaluation(ctx, evaluationReport); err != nil {
		t.Fatal(err)
	}
	instrumentID := uuid.MustParse(fixture.experiment.strategy.manifest.Partitions()[0].Observations[0].InstrumentID)
	declaration, err := benchmark.NewDeclaration(benchmark.DeclarationInput{Experiment: experiment, Manifest: fixture.experiment.strategy.manifest, BenchmarkInstrumentID: instrumentID, BenchmarkKind: "total_return_index", Weighting: "single_asset", DistributionTreatment: "reinvested", CashConvention: "explicit_per_period", Frequency: "minute", InitialNotional: "25000", DecimalScale: 12, Observations: benchmarkObservations})
	if err != nil {
		t.Fatal(err)
	}
	report, err := benchmark.NewReport(declaration, evaluationReport)
	if err != nil {
		t.Fatal(err)
	}
	return benchmarkFixture{evaluation: fixture, declaration: declaration, report: report, repo: NewBenchmarkRepo(fixture.experiment.strategy.pool)}
}

func TestBenchmarkRepositoryRoundTripListsAndEightWriters(t *testing.T) {
	fixture := newBenchmarkFixture(t)
	ctx := context.Background()
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RegisterDeclaration(ctx, fixture.declaration)
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
	service, _ := benchmark.NewService(fixture.repo)
	got, err := service.Evaluate(ctx, fixture.declaration, fixture.report.EvaluationID())
	if err != nil || got.ID() != fixture.report.ID() {
		t.Fatalf("evaluate=%v/%v", got, err)
	}
	restarted := NewBenchmarkRepo(fixture.evaluation.experiment.strategy.pool)
	loaded, err := restarted.GetReport(ctx, fixture.report.ID())
	if err != nil || loaded.Digest() != fixture.report.Digest() || !bytes.Equal(loaded.CanonicalBytes(), fixture.report.CanonicalBytes()) {
		t.Fatalf("reload=%v/%v", loaded, err)
	}
	declarations, err := restarted.ListExperimentDeclarations(ctx, fixture.declaration.ExperimentID(), 10, 0)
	if err != nil || len(declarations) != 1 {
		t.Fatalf("declarations=%d/%v", len(declarations), err)
	}
	reports, err := restarted.ListEvaluationReports(ctx, fixture.report.EvaluationID(), 10, 0)
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%d/%v", len(reports), err)
	}
	var declarationCount, observationCount, reportCount int
	err = fixture.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM passive_benchmark_declarations),(SELECT count(*) FROM passive_benchmark_observations),(SELECT count(*) FROM benchmark_opportunity_cost_reports)`).Scan(&declarationCount, &observationCount, &reportCount)
	if err != nil || declarationCount != 1 || observationCount != 6 || reportCount != 1 {
		t.Fatalf("counts=%d/%d/%d err=%v", declarationCount, observationCount, reportCount, err)
	}
}

func TestBenchmarkRepositoryRollbackForgeryAndAppendOnly(t *testing.T) {
	fixture := newBenchmarkFixture(t)
	ctx := context.Background()
	repo := fixture.repo
	for _, failedStage := range []string{"benchmark_declaration", "benchmark_observation"} {
		repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := repo.RegisterDeclaration(ctx, fixture.declaration); err == nil {
			t.Fatalf("%s interruption accepted", failedStage)
		}
		var count int
		if err := fixture.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM passive_benchmark_declarations`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s partial rows=%d/%v", failedStage, count, err)
		}
	}
	repo.afterStage = nil
	if _, err := repo.RegisterDeclaration(ctx, fixture.declaration); err != nil {
		t.Fatal(err)
	}
	repo.afterStage = func(stage string) error {
		if stage == "benchmark_report" {
			return errors.New("injected")
		}
		return nil
	}
	if _, err := repo.RecordReport(ctx, fixture.report); err == nil {
		t.Fatal("report interruption accepted")
	}
	var reportCount int
	if err := fixture.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM benchmark_opportunity_cost_reports`).Scan(&reportCount); err != nil || reportCount != 0 {
		t.Fatalf("partial report rows=%d/%v", reportCount, err)
	}
	repo.afterStage = nil
	if _, err := repo.RecordReport(ctx, fixture.report); err != nil {
		t.Fatal(err)
	}
	pool := fixture.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `UPDATE passive_benchmark_observations SET benchmark_value='999' WHERE declaration_id=$1 AND sequence=1`, fixture.declaration.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("mutation error=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000082_passive_benchmark_control.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("nonempty rollback=%v", err)
	}
}

func TestBenchmarkRepositoryRejectsNormalizedForgery(t *testing.T) {
	fixture := newBenchmarkFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RegisterDeclaration(ctx, fixture.declaration); err != nil {
		t.Fatal(err)
	}
	pool := fixture.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `ALTER TABLE passive_benchmark_observations DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE passive_benchmark_observations SET evidence_sha256=$1 WHERE declaration_id=$2 AND sequence=1`, strings.Repeat("f", 64), fixture.declaration.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE passive_benchmark_observations ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetDeclaration(ctx, fixture.declaration.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery reload=%v", err)
	}
}
