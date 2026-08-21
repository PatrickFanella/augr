package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/promotion"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type promotionRepositoryFixture struct {
	robustness robustnessRepositoryFixture
	deployment *strategycatalog.Deployment
	policy     *promotion.Policy
	decision   *promotion.Decision
	repo       *PromotionRepo
}

func newPromotionRepositoryFixture(t *testing.T) promotionRepositoryFixture {
	t.Helper()
	robustnessFixture := newRobustnessRepositoryFixture(t)
	ctx := context.Background()
	pool := robustnessFixture.evaluation.experiment.strategy.pool
	if _, err := robustnessFixture.repo.RecordAssessment(ctx, robustnessFixture.assessment); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000081_promotion_retirement_evaluator.up.sql")); err != nil {
		t.Fatal(err)
	}
	strategy := robustnessFixture.evaluation.experiment.strategy
	deployment, err := strategycatalog.NewDeployment(strategycatalog.DeploymentInput{
		VersionID: strategy.version.ID(), AccountID: strategy.account.ID, CapitalBindingID: strategy.binding.ID,
		Budget: "10000", ScheduleCron: "0 14 * * 1-5", Timezone: "America/Chicago", RiskPolicyVersion: "risk-v1", Mode: strategycatalog.ExperimentPaperScored,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.repo.ProposeStrategyDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	policy, err := promotion.NewPolicy(promotion.PolicyInput{Version: "promotion-policy-v1@postgres", RequiredGates: []string{"overall_robustness", "multiple_testing_adjustment"}, FailureAction: promotion.ActionHold})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := promotion.NewDecision(promotion.DecisionInput{Deployment: deployment, Assessment: robustnessFixture.assessment, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPromotionRepo(pool)
	if _, err := repo.RegisterPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	return promotionRepositoryFixture{robustness: robustnessFixture, deployment: deployment, policy: policy, decision: decision, repo: repo}
}

func TestPromotionRepositoryRoundTripConcurrentConvergenceAndRollbackRefusal(t *testing.T) {
	fixture := newPromotionRepositoryFixture(t)
	ctx := context.Background()
	state, head, err := fixture.repo.ProjectDeploymentState(ctx, fixture.deployment.ID())
	if err != nil || state != strategycatalog.DeploymentProposed || head.String() != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("initial projection=%s/%s err=%v", state, head, err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() { defer wait.Done(); _, err := fixture.repo.RecordDecision(ctx, fixture.decision); errs <- err }()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	loaded, err := NewPromotionRepo(fixture.robustness.evaluation.experiment.strategy.pool).GetDecision(ctx, fixture.decision.ID())
	if err != nil || !bytes.Equal(loaded.CanonicalBytes(), fixture.decision.CanonicalBytes()) {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	held, err := promotion.NewDecision(promotion.DecisionInput{Deployment: fixture.deployment, Assessment: fixture.robustness.assessment, Policy: fixture.policy, PriorDecision: fixture.decision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordDecision(ctx, held); err != nil {
		t.Fatal(err)
	}
	state, head, err = fixture.repo.ProjectDeploymentState(ctx, fixture.deployment.ID())
	if err != nil || state != promotion.StateShadow || head != held.ID() {
		t.Fatalf("projection=%s/%s err=%v", state, head, err)
	}
	byDeployment, err := fixture.repo.ListDeploymentDecisions(ctx, fixture.deployment.ID(), 10, 0)
	if err != nil || len(byDeployment) != 2 {
		t.Fatalf("deployment decisions=%d err=%v", len(byDeployment), err)
	}
	for name, list := range map[string]func() ([]*promotion.Decision, error){
		"version": func() ([]*promotion.Decision, error) {
			return fixture.repo.ListVersionDecisions(ctx, fixture.decision.VersionID(), 10, 0)
		},
		"assessment": func() ([]*promotion.Decision, error) {
			return fixture.repo.ListAssessmentDecisions(ctx, fixture.decision.AssessmentID(), 10, 0)
		},
		"family": func() ([]*promotion.Decision, error) {
			return fixture.repo.ListFamilyDecisions(ctx, fixture.decision.FamilyID(), 10, 0)
		},
	} {
		values, err := list()
		if err != nil || len(values) != 2 {
			t.Fatalf("%s decisions=%d err=%v", name, len(values), err)
		}
	}
	pool := fixture.robustness.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `UPDATE promotion_decision_observed_gates SET state=state WHERE decision_id=$1`, fixture.decision.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("mutation error=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000081_promotion_retirement_evaluator.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 81") {
		t.Fatalf("nonempty rollback error=%v", err)
	}
}

func TestPromotionRepositoryRejectsCompetingInitialDecisionFork(t *testing.T) {
	fixture := newPromotionRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RecordDecision(ctx, fixture.decision); err != nil {
		t.Fatal(err)
	}
	alternativePolicy, _ := promotion.NewPolicy(promotion.PolicyInput{Version: "promotion-policy-v1@competing", RequiredGates: []string{"overall_robustness"}, FailureAction: promotion.ActionRetire})
	if _, err := fixture.repo.RegisterPolicy(ctx, alternativePolicy); err != nil {
		t.Fatal(err)
	}
	alternative, err := promotion.NewDecision(promotion.DecisionInput{Deployment: fixture.deployment, Assessment: fixture.robustness.assessment, Policy: alternativePolicy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordDecision(ctx, alternative); err == nil {
		t.Fatal("competing initial decision fork succeeded")
	}
}

func TestPromotionRepositoryApprovedHeldRetiredEvidence(t *testing.T) {
	fixture := newPromotionRepositoryFixture(t)
	ctx := context.Background()
	approved, err := fixture.repo.RecordDecision(ctx, fixture.decision)
	if err != nil {
		t.Fatal(err)
	}
	held, _ := promotion.NewDecision(promotion.DecisionInput{Deployment: fixture.deployment, Assessment: fixture.robustness.assessment, Policy: fixture.policy, PriorDecision: approved})
	if _, err := fixture.repo.RecordDecision(ctx, held); err != nil {
		t.Fatal(err)
	}
	failed := failedRobustnessAssessment(t, fixture.robustness)
	if _, err := fixture.robustness.repo.RecordAssessment(ctx, failed); err != nil {
		t.Fatal(err)
	}
	strategy := fixture.robustness.evaluation.experiment.strategy
	retiredDeployment, err := strategycatalog.NewDeployment(strategycatalog.DeploymentInput{
		VersionID: strategy.version.ID(), AccountID: strategy.account.ID, CapitalBindingID: strategy.binding.ID,
		Budget: "9000", ScheduleCron: "0 14 * * 1-5", Timezone: "America/Chicago", RiskPolicyVersion: "risk-v1", Mode: strategycatalog.ExperimentPaperScored,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.repo.ProposeStrategyDeployment(ctx, retiredDeployment); err != nil {
		t.Fatal(err)
	}
	retirePolicy, _ := promotion.NewPolicy(promotion.PolicyInput{Version: "promotion-policy-v1@retained-retire", RequiredGates: []string{"overall_robustness"}, FailureAction: promotion.ActionRetire})
	if _, err := fixture.repo.RegisterPolicy(ctx, retirePolicy); err != nil {
		t.Fatal(err)
	}
	retired, err := promotion.NewDecision(promotion.DecisionInput{Deployment: retiredDeployment, Assessment: failed, Policy: retirePolicy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.RecordDecision(ctx, retired); err != nil {
		t.Fatal(err)
	}
	if approved.Outcome() != promotion.OutcomeApproved || held.Outcome() != promotion.OutcomeHeld || retired.Outcome() != promotion.OutcomeRetired {
		t.Fatalf("outcomes=%s/%s/%s", approved.Outcome(), held.Outcome(), retired.Outcome())
	}
	t.Logf("VERIFIED_LOCAL approved=%s held=%s retired=%s failed_assessment=%s", approved.ID(), held.ID(), retired.ID(), failed.ID())
}

func failedRobustnessAssessment(t *testing.T, fixture robustnessRepositoryFixture) *robustness.Assessment {
	t.Helper()
	ctx := context.Background()
	start := fixture.evaluation.experiment.plan.EvaluationStart()
	baseline1 := robustnessEvaluationReport(t, fixture.evaluation, start.Add(10*time.Second), []string{"100", "90", "80"}, "failed-b1")
	perturbed1 := robustnessEvaluationReport(t, fixture.evaluation, start.Add(10*time.Second), []string{"100", "89", "78"}, "failed-p1")
	baseline2 := robustnessEvaluationReport(t, fixture.evaluation, start.Add(150*time.Second), []string{"100", "90", "80"}, "failed-b2")
	perturbed2 := robustnessEvaluationReport(t, fixture.evaluation, start.Add(150*time.Second), []string{"100", "89", "78"}, "failed-p2")
	for _, report := range []*evaluation.Report{baseline1, perturbed1, baseline2, perturbed2} {
		if _, err := fixture.evaluation.repo.RecordEvaluation(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	assessment, err := robustness.NewAssessment(robustness.AssessmentInput{Family: fixture.family, Policy: fixture.policy, Mode: strategycatalog.ExperimentPaperScored, Candidates: []robustness.CandidateInput{{
		VersionID: fixture.evaluation.experiment.strategy.version.ID(), Folds: []robustness.FoldInput{
			{TrainStart: start.Add(-10 * time.Second), TrainEnd: start.Add(5 * time.Second), Baseline: baseline1, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_cost", Report: perturbed1}}},
			{TrainStart: start.Add(135 * time.Second), TrainEnd: start.Add(145 * time.Second), Baseline: baseline2, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_cost", Report: perturbed2}}},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}

func TestPromotionRepositoryRollsBackEveryDecisionStage(t *testing.T) {
	for _, stage := range []string{"promotion_decision", "promotion_observed_gate", "promotion_lifecycle_event"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newPromotionRepositoryFixture(t)
			injected := errors.New("stop")
			fixture.repo.afterStage = func(candidate string) error {
				if candidate == stage {
					return injected
				}
				return nil
			}
			if _, err := fixture.repo.RecordDecision(context.Background(), fixture.decision); !errors.Is(err, injected) {
				t.Fatalf("injected %s error=%v", stage, err)
			}
			var count int
			if err := fixture.robustness.evaluation.experiment.strategy.pool.QueryRow(context.Background(), `SELECT count(*) FROM promotion_retirement_decisions`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial decision count=%d err=%v", count, err)
			}
		})
	}
}

func TestPromotionRepositoryRejectsForgedNormalizedGateOnReload(t *testing.T) {
	fixture := newPromotionRepositoryFixture(t)
	ctx := context.Background()
	if _, err := fixture.repo.RecordDecision(ctx, fixture.decision); err != nil {
		t.Fatal(err)
	}
	pool := fixture.robustness.evaluation.experiment.strategy.pool
	if _, err := pool.Exec(ctx, `ALTER TABLE promotion_decision_observed_gates DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE promotion_decision_observed_gates SET observed='forged' WHERE decision_id=$1 AND sequence=0`, fixture.decision.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE promotion_decision_observed_gates ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetDecision(ctx, fixture.decision.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forged normalized gate error=%v", err)
	}
}
