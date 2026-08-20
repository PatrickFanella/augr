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
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestRobustnessRepositoryRoundTripAndConcurrentConvergence(t *testing.T) {
	fixture := newRobustnessRepositoryFixture(t)
	ctx := context.Background()
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordAssessment(ctx, fixture.assessment)
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
	loaded, err := NewRobustnessRepo(fixture.evaluation.experiment.strategy.pool).GetAssessment(ctx, fixture.assessment.ID())
	if err != nil || !bytes.Equal(loaded.CanonicalBytes(), fixture.assessment.CanonicalBytes()) {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	byFamily, err := fixture.repo.ListFamilyAssessments(ctx, fixture.family.ID(), 10, 0)
	byFamily = mustListRobustness(t, byFamily, err)
	byCandidate, err := fixture.repo.ListCandidateAssessments(ctx, fixture.evaluation.experiment.strategy.version.ID(), 10, 0)
	byCandidate = mustListRobustness(t, byCandidate, err)
	byReport, err := fixture.repo.ListReportAssessments(ctx, uuid.MustParse(fixture.assessment.Candidates()[0].Folds[0].Baseline.ReportID), 10, 0)
	byReport = mustListRobustness(t, byReport, err)
	for name, values := range map[string][]*robustness.Assessment{
		"family": byFamily, "candidate": byCandidate, "report": byReport,
	} {
		if len(values) != 1 || values[0].ID() != fixture.assessment.ID() {
			t.Fatalf("%s list=%v", name, values)
		}
	}
	pool := fixture.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `UPDATE robustness_gates SET state=state WHERE assessment_id=$1`, fixture.assessment.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("gate mutation error=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000080_statistical_robustness_assessments.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 80") {
		t.Fatalf("nonempty rollback error=%v", err)
	}
}

func TestRobustnessRepositoryRollsBackEveryAssessmentStage(t *testing.T) {
	stages := []string{"robustness_assessment", "robustness_candidate", "robustness_fold", "robustness_scenario", "robustness_statistic", "robustness_gate"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			fixture := newRobustnessRepositoryFixture(t)
			injected := errors.New("stop")
			fixture.repo.afterStage = func(candidate string) error {
				if candidate == stage {
					return injected
				}
				return nil
			}
			if _, err := fixture.repo.RecordAssessment(context.Background(), fixture.assessment); !errors.Is(err, injected) {
				t.Fatalf("injected %s error=%v", stage, err)
			}
			var count int
			if err := fixture.evaluation.experiment.strategy.pool.QueryRow(context.Background(), `SELECT count(*) FROM statistical_robustness_assessments`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial assessment count=%d err=%v", count, err)
			}
		})
	}
}

func TestRobustnessRepositoryRejectsForgedNormalizedStatistic(t *testing.T) {
	fixture := newRobustnessRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RecordAssessment(ctx, fixture.assessment); err != nil {
		t.Fatal(err)
	}
	pool := fixture.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `ALTER TABLE robustness_statistics DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE robustness_statistics SET value='0.9' WHERE assessment_id=$1 AND sequence=0`, fixture.assessment.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE robustness_statistics ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetAssessment(ctx, fixture.assessment.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forged normalized statistic error=%v", err)
	}
}

type robustnessRepositoryFixture struct {
	evaluation evaluationFixture
	policy     *robustness.Policy
	family     *robustness.Family
	assessment *robustness.Assessment
	repo       *RobustnessRepo
}

func newRobustnessRepositoryFixture(t *testing.T) robustnessRepositoryFixture {
	t.Helper()
	fixture := newEvaluationFixture(t)
	ctx := context.Background()
	if _, err := fixture.experiment.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, "000080_statistical_robustness_assessments.up.sql")); err != nil {
		t.Fatal(err)
	}
	evalRepo := fixture.repo
	if _, err := evalRepo.RegisterPolicy(ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	start := fixture.experiment.plan.EvaluationStart()
	baseline1 := robustnessEvaluationReport(t, fixture, start.Add(10*time.Second), []string{"100", "101", "102"}, "b1")
	perturbed1 := robustnessEvaluationReport(t, fixture, start.Add(10*time.Second), []string{"100", "100.8", "101.6"}, "p1")
	baseline2 := robustnessEvaluationReport(t, fixture, start.Add(150*time.Second), []string{"100", "101", "102"}, "b2")
	perturbed2 := robustnessEvaluationReport(t, fixture, start.Add(150*time.Second), []string{"100", "100.8", "101.6"}, "p2")
	for _, report := range []*evaluation.Report{baseline1, perturbed1, baseline2, perturbed2} {
		if _, err := evalRepo.RecordEvaluation(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := robustness.NewPolicy(robustness.PolicyInput{
		Version: "robustness-policy-v1@postgres", FoldCount: 2, PurgeSeconds: 5, EmbargoSeconds: 5,
		BootstrapAlgorithm: "xorshift64star-iid-percentile-v1", BootstrapSeed: 80, BootstrapIterations: 1000, ConfidenceLevel: "0.95", FamilyWiseAlpha: "0.05",
		MaxLargestPositiveShare: "0.6", MaxTopDecilePositiveShare: "0.6", MaxPerturbationDegradation: "0.005", RequiredPerturbations: []string{"cost_up"}, DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	versionID := fixture.experiment.strategy.version.ID()
	family, err := robustness.NewFamily(robustness.FamilyInput{Name: "postgres robustness family", HypothesisSHA256: strings.Repeat("d", 64), CandidateVersionIDs: []uuid.UUID{versionID}})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := robustness.NewAssessment(robustness.AssessmentInput{
		Family: family, Policy: policy, Mode: strategycatalog.ExperimentPaperScored,
		Candidates: []robustness.CandidateInput{{
			VersionID: versionID,
			Folds: []robustness.FoldInput{
				{TrainStart: start.Add(-10 * time.Second), TrainEnd: start.Add(5 * time.Second), Baseline: baseline1, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_cost", Report: perturbed1}}},
				{TrainStart: start.Add(135 * time.Second), TrainEnd: start.Add(145 * time.Second), Baseline: baseline2, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_cost", Report: perturbed2}}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRobustnessRepo(fixture.experiment.strategy.pool)
	if _, err := repo.RegisterPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RegisterFamily(ctx, family); err != nil {
		t.Fatal(err)
	}
	return robustnessRepositoryFixture{evaluation: fixture, policy: policy, family: family, assessment: assessment, repo: repo}
}

func mustListRobustness(t *testing.T, values []*robustness.Assessment, err error) []*robustness.Assessment {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func robustnessEvaluationReport(t *testing.T, fixture evaluationFixture, start time.Time, equities []string, salt string) *evaluation.Report {
	t.Helper()
	observations := make([]evaluation.ObservationInput, len(equities))
	for i, equity := range equities {
		observations[i] = evaluation.ObservationInput{ObservedAt: start.Add(time.Duration(i) * time.Minute), Equity: equity, BenchmarkValue: "100", CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(salt+string(rune(i)))), EvidenceSHA256: strings.Repeat("e", 64)}
	}
	report, err := evaluation.NewReport(evaluation.ReportInput{Result: fixture.experiment.result, Policy: fixture.policy, EvaluationStart: start, EvaluationEnd: start.Add(time.Duration(len(equities)-1) * time.Minute), Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}, Observations: observations})
	if err != nil {
		t.Fatal(err)
	}
	return report
}
