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

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type evaluationFixture struct {
	experiment experimentMigrationFixture
	policy     *evaluation.Policy
	report     *evaluation.Report
	repo       *EvaluationRepo
}

func newEvaluationFixture(t *testing.T) evaluationFixture {
	t.Helper()
	fixture := newExperimentRunMigrationFixture(t)
	ctx := fixture.strategy.ctx
	if _, err := fixture.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000079_trade_portfolio_evaluations.up.sql")); err != nil {
		t.Fatal(err)
	}
	if err := insertExperimentProgramPlan(ctx, fixture.strategy.pool, fixture.program, fixture.plan, fixture.start); err != nil {
		t.Fatal(err)
	}
	attemptID := uuid.New()
	started, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{AttemptID: attemptID, Sequence: 0, Type: experimentrun.AttemptStarted, OccurredAt: fixture.start})
	if err := insertExperimentAttemptStart(ctx, fixture.strategy.pool, fixture.plan.ExperimentID(), started, fixture.start); err != nil {
		t.Fatal(err)
	}
	completed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptCompleted,
		OccurredAt: fixture.start.Add(time.Second), ResultID: fixture.result.ID(),
	})
	if err := insertExperimentNoopResult(ctx, fixture.strategy.pool, fixture.result, fixture.plan, completed, fixture.start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{
		Version: "evaluation-policy-v1@repository", Frequency: "minute", PeriodsPerYear: 98280,
		ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo",
		RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := "0.2"
	report, err := evaluation.NewReport(evaluation.ReportInput{
		Result: fixture.result, Policy: policy, EvaluationStart: fixture.plan.EvaluationStart(),
		EvaluationEnd: fixture.plan.EvaluationEnd(), OpenLotCount: 2,
		Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"},
		Observations: []evaluation.ObservationInput{
			{ObservedAt: fixture.plan.EvaluationStart(), Equity: "100", BenchmarkValue: "100", CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", CumulativeObservedSlippage: textPointer("0"), EvidenceID: uuid.MustParse("30410000-0000-4000-8000-000000000001"), EvidenceSHA256: strings.Repeat("1", 64)},
			{ObservedAt: fixture.plan.EvaluationStart().Add(time.Minute), Equity: "101", BenchmarkValue: "100.5", CashReturn: "0.000001", GrossExposure: "20", NetExposure: "20", LargestPositionWeight: "0.2", CumulativeOwnershipCost: "0.1", CumulativeTurnover: "0.1", CumulativeModeledSlippage: "0.1", CumulativeObservedSlippage: textPointer("0.1"), EvidenceID: uuid.MustParse("30410000-0000-4000-8000-000000000002"), EvidenceSHA256: strings.Repeat("2", 64)},
			{ObservedAt: fixture.plan.EvaluationEnd(), Equity: "100.5", BenchmarkValue: "100.25", CashReturn: "0.000001", GrossExposure: "10", NetExposure: "10", LargestPositionWeight: "0.1", CumulativeOwnershipCost: "0.2", CumulativeTurnover: "0.2", CumulativeModeledSlippage: "0.2", CumulativeObservedSlippage: &observed, EvidenceID: uuid.MustParse("30410000-0000-4000-8000-000000000003"), EvidenceSHA256: strings.Repeat("3", 64)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return evaluationFixture{experiment: fixture, policy: policy, report: report, repo: NewEvaluationRepo(fixture.strategy.pool)}
}

func TestEvaluationRepositoryRoundTripListsAndConverges(t *testing.T) {
	fixture := newEvaluationFixture(t)
	ctx := fixture.experiment.strategy.ctx
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordEvaluation(ctx, fixture.report)
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
	loaded, err := NewEvaluationRepo(fixture.experiment.strategy.pool).GetEvaluation(ctx, fixture.report.ID())
	if err != nil || loaded.Digest() != fixture.report.Digest() || !bytes.Equal(loaded.CanonicalBytes(), fixture.report.CanonicalBytes()) {
		t.Fatalf("restarted load=%v err=%v", loaded, err)
	}
	byResult, err := fixture.repo.ListResultEvaluations(ctx, fixture.report.ResultID(), 10, 0)
	if err != nil || len(byResult) != 1 || byResult[0].ID() != fixture.report.ID() {
		t.Fatalf("result list=%v err=%v", byResult, err)
	}
	byExperiment, err := fixture.repo.ListExperimentEvaluations(ctx, fixture.report.ExperimentID(), 10, 0)
	if err != nil || len(byExperiment) != 1 || byExperiment[0].ID() != fixture.report.ID() {
		t.Fatalf("experiment list=%v err=%v", byExperiment, err)
	}
	var reports, observations, metrics int
	if err := fixture.experiment.strategy.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM trade_portfolio_evaluations),
		(SELECT count(*) FROM evaluation_observations),(SELECT count(*) FROM evaluation_metrics)`).Scan(&reports, &observations, &metrics); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || observations != 3 || metrics != len(fixture.report.Metrics()) {
		t.Fatalf("graph counts=%d/%d/%d", reports, observations, metrics)
	}
}

func TestEvaluationRepositoryRollsBackEveryInjectedStage(t *testing.T) {
	for _, stage := range []string{"evaluation_parent", "evaluation_observation", "evaluation_metric"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newEvaluationFixture(t)
			if _, err := fixture.repo.RegisterPolicy(fixture.experiment.strategy.ctx, fixture.policy); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("stop")
			fixture.repo.afterStage = func(candidate string) error {
				if candidate == stage {
					return injected
				}
				return nil
			}
			if _, err := fixture.repo.RecordEvaluation(fixture.experiment.strategy.ctx, fixture.report); !errors.Is(err, injected) {
				t.Fatalf("injected error=%v", err)
			}
			var count int
			if err := fixture.experiment.strategy.pool.QueryRow(fixture.experiment.strategy.ctx, `SELECT count(*) FROM trade_portfolio_evaluations`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial parent count=%d err=%v", count, err)
			}
		})
	}
}

func TestEvaluationMigrationRejectsMutationIncompleteGraphAndNonemptyRollback(t *testing.T) {
	fixture := newEvaluationFixture(t)
	ctx := fixture.experiment.strategy.ctx
	if _, err := fixture.repo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordEvaluation(ctx, fixture.report); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.experiment.strategy.pool.Exec(ctx, `UPDATE evaluation_metrics SET value=value WHERE evaluation_id=$1`, fixture.report.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("metric mutation error=%v", err)
	}
	if _, err := fixture.experiment.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000079_trade_portfolio_evaluations.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 79") {
		t.Fatalf("nonempty rollback error=%v", err)
	}

	incomplete := newEvaluationFixture(t)
	if _, err := incomplete.repo.RegisterPolicy(incomplete.experiment.strategy.ctx, incomplete.policy); err != nil {
		t.Fatal(err)
	}
	tx, err := incomplete.experiment.strategy.pool.Begin(incomplete.experiment.strategy.ctx)
	if err != nil {
		t.Fatal(err)
	}
	execution := incomplete.report.Execution()
	_, err = tx.Exec(incomplete.experiment.strategy.ctx, `INSERT INTO trade_portfolio_evaluations(
		id,schema_name,state,result_id,result_sha256,experiment_id,program_id,plan_id,account_id,manifest_id,quality_result_id,mode,
		policy_id,policy_sha256,evaluation_start,evaluation_end,open_lot_count,attempted_orders,filled_orders,attempted_quantity,
		filled_quantity,observation_count,closed_trade_count,metric_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'trade-portfolio-evaluation-v1','completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,convert_from($24,'UTF8')::jsonb,$25)`,
		incomplete.report.ID(), incomplete.report.ResultID(), incomplete.report.ResultDigest(), incomplete.report.ExperimentID(), incomplete.report.ProgramID(),
		incomplete.report.PlanID(), incomplete.report.AccountID(), incomplete.report.ManifestID(), incomplete.report.QualityResultID(), incomplete.report.Mode(),
		incomplete.report.PolicyID(), incomplete.report.PolicyDigest(), incomplete.report.EvaluationStart(), incomplete.report.EvaluationEnd(), incomplete.report.OpenLotCount(),
		execution.AttemptedOrders, execution.FilledOrders, execution.AttemptedQuantity, execution.FilledQuantity, len(incomplete.report.Observations()),
		len(incomplete.report.ClosedTrades()), len(incomplete.report.Metrics()), incomplete.report.Digest(), incomplete.report.CanonicalBytes(), databaseNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(incomplete.experiment.strategy.ctx); err == nil || !strings.Contains(err.Error(), "graph does not reconstruct") {
		t.Fatalf("incomplete graph commit error=%v", err)
	}
}

func TestEvaluationRepositoryRejectsMissingPolicyAndResult(t *testing.T) {
	fixture := newEvaluationFixture(t)
	if _, err := fixture.repo.RecordEvaluation(fixture.experiment.strategy.ctx, fixture.report); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("missing policy error=%v", err)
	}
}

func TestEvaluationGoldenTradeEvidencePersistsRelationally(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	if _, err := pool.Exec(context.Background(), repositoryMigrationSQL(t, "000079_trade_portfolio_evaluations.up.sql")); err != nil {
		t.Fatal(err)
	}
	fixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperScored)
	runner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
	result := runGoldenExperiment(t, runner, fixture, uuid.MustParse("30420000-0000-4000-8000-000000000001"), 0)
	fillIDs := result.Outcomes()[0].FillIDs
	if len(fillIDs) != 2 {
		t.Fatalf("golden result fills=%d", len(fillIDs))
	}
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{
		Version: "evaluation-policy-v1@golden", Frequency: "minute", PeriodsPerYear: 98280,
		ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo",
		RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := evaluation.NewReport(evaluation.ReportInput{
		Result: result, Policy: policy, EvaluationStart: qualification.Start, EvaluationEnd: qualification.End,
		Execution: evaluation.ExecutionInput{AttemptedOrders: "1", FilledOrders: "1", AttemptedQuantity: "10", FilledQuantity: "10"},
		Observations: []evaluation.ObservationInput{
			{ObservedAt: qualification.Start, Equity: "25000", BenchmarkValue: "100", CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", CumulativeObservedSlippage: textPointer("0"), EvidenceID: fixture.QuoteSnapshot.ID, EvidenceSHA256: strings.Repeat("7", 64)},
			{ObservedAt: qualification.RouteAt, Equity: "25000.04", BenchmarkValue: "100.01", CashReturn: "0.000001", GrossExposure: "102.05", NetExposure: "102.05", LargestPositionWeight: "0.004082", CumulativeOwnershipCost: "0.5", CumulativeTurnover: "0.004082", CumulativeModeledSlippage: "0.5", CumulativeObservedSlippage: textPointer("0.5"), EvidenceID: fillIDs[0], EvidenceSHA256: strings.Repeat("8", 64)},
			{ObservedAt: qualification.End, Equity: "24999.04", BenchmarkValue: "100.02", CashReturn: "0.000001", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "1.01", CumulativeTurnover: "0.008164", CumulativeModeledSlippage: "1.01", CumulativeObservedSlippage: textPointer("1.01"), EvidenceID: fillIDs[1], EvidenceSHA256: strings.Repeat("9", 64)},
		}, ClosedTrades: []evaluation.ClosedTradeInput{{
			InstrumentID: fixture.Instrument.ID, Side: "long", Quantity: "5", EntryFillIDs: fillIDs[:1],
			ExitFillIDs: fillIDs[1:], EntryAt: qualification.RouteAt, ExitAt: qualification.RouteAt, EntryPrice: "10.2", ExitPrice: "10.21",
			EntryFees: "0.5", ExitFees: "0.51", OtherOwnershipCost: "0", GrossPnL: "0.05", AfterCostPnL: "-0.96",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewEvaluationRepo(pool)
	if _, err := repo.RegisterPolicy(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.RecordEvaluation(context.Background(), report)
	if err != nil {
		t.Fatal(err)
	}
	if metric := evaluationMetric(loaded, "trade", "win_rate"); metric.Value != "0" || metric.Description != "closed_trade_after_cost_win_rate_not_bar_return_rate" {
		t.Fatalf("relational trade metric=%+v", metric)
	}
	if metric := evaluationMetric(loaded, "curve_diagnostics", "bar_positive_return_rate"); metric.Description != "descriptor_only_not_trade_win_rate" {
		t.Fatalf("relational curve metric=%+v", metric)
	}
	var trades, fills int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM evaluation_closed_trades WHERE evaluation_id=$1),
		(SELECT count(*) FROM evaluation_trade_fill_ids WHERE evaluation_id=$1)`, report.ID()).Scan(&trades, &fills); err != nil || trades != 1 || fills != 2 {
		t.Fatalf("relational trade/fill counts=%d/%d err=%v", trades, fills, err)
	}
}

func evaluationMetric(report *evaluation.Report, section, name string) evaluation.Metric {
	for _, metric := range report.Metrics() {
		if metric.Section == section && metric.Name == name {
			return metric
		}
	}
	return evaluation.Metric{}
}

func textPointer(value string) *string { return &value }
