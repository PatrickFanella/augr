package qualification

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evidencereview"
	"github.com/PatrickFanella/get-rich-quick/internal/promotion"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type Fixture struct {
	Parents         evidencereview.CaseInput
	Case            *evidencereview.Case
	Reviews         []*evidencereview.Review
	Summary         *evidencereview.Summary
	ConflictReviews []*evidencereview.Review
	ConflictSummary *evidencereview.Summary
	HeldParents     evidencereview.CaseInput
	HeldCase        *evidencereview.Case
	HeldReviews     []*evidencereview.Review
	HeldSummary     *evidencereview.Summary
}

func Build() (Fixture, error) {
	research, err := researchqualification.Build()
	if err != nil {
		return Fixture{}, err
	}
	deployment, err := strategycatalog.NewDeployment(strategycatalog.DeploymentInput{VersionID: research.Parents.Version.ID(), AccountID: uuid.MustParse("60300000-0000-4000-8000-000000000001"), CapitalBindingID: uuid.MustParse("60300000-0000-4000-8000-000000000002"), Budget: "10000", ScheduleCron: "0 12 * * 1-5", Timezone: "America/Chicago", RiskPolicyVersion: "risk-policy-v1@sha256:" + strings.Repeat("e", 64), Mode: strategycatalog.ExperimentPaperScored})
	if err != nil {
		return Fixture{}, err
	}
	policy, err := promotion.NewPolicy(promotion.PolicyInput{Version: "promotion-policy-v1@evidence-review", RequiredGates: []string{"overall_robustness"}, FailureAction: promotion.ActionHold})
	if err != nil {
		return Fixture{}, err
	}
	decision, err := promotion.NewDecision(promotion.DecisionInput{Deployment: deployment, Assessment: research.Parents.Assessment, Policy: policy})
	if err != nil {
		return Fixture{}, err
	}
	parents := evidencereview.CaseInput{Hypothesis: research.Hypothesis, Critic: research.ReadyCritic, PromotionPolicy: policy, PromotionDecision: decision, Deployment: deployment, Assessment: research.Parents.Assessment}
	reviewCase, err := evidencereview.NewCase(parents)
	if err != nil {
		return Fixture{}, err
	}
	checks := func(decisionDigest string, unknown bool) []evidencereview.CheckInput {
		values := []evidencereview.CheckInput{}
		for _, name := range []string{"cost_capacity", "policy_decision_consistency", "reproducibility", "safety_boundaries", "source_integrity", "statistical_controls"} {
			state := "pass"
			if unknown && name == "cost_capacity" {
				state = "unknown"
			}
			values = append(values, evidencereview.CheckInput{Name: name, State: state, References: []string{"promotion_decision:sha256:" + decisionDigest}, Explanation: "The exact retained evidence was independently reviewed."})
		}
		return values
	}
	at := time.Date(2026, 8, 20, 19, 0, 0, 0, time.UTC)
	serviceInput := evidencereview.ReviewInput{Case: reviewCase, Reviewer: evidencereview.ReviewerInput{Key: "reviewer_service", Kind: "independent_service", Organization: "Independent Review Service", IdentitySHA256: strings.Repeat("1", 64), SystemPromptSHA256: strings.Repeat("2", 64), DeveloperPromptSHA256: strings.Repeat("3", 64), UserPromptSHA256: strings.Repeat("4", 64)}, ReviewedAt: at, Checks: checks(decision.Digest(), false)}
	service, err := evidencereview.NewReview(serviceInput)
	if err != nil {
		return Fixture{}, err
	}
	humanInput := evidencereview.ReviewInput{Case: reviewCase, Reviewer: evidencereview.ReviewerInput{Key: "reviewer_human", Kind: "human", Organization: "Independent Human Review", IdentitySHA256: strings.Repeat("5", 64)}, ReviewedAt: at.Add(time.Minute), Checks: checks(decision.Digest(), true)}
	human, err := evidencereview.NewReview(humanInput)
	if err != nil {
		return Fixture{}, err
	}
	reviews := []*evidencereview.Review{service, human}
	summary, err := evidencereview.NewSummary(evidencereview.SummaryInput{Case: reviewCase, ReviewHeads: reviews})
	if err != nil {
		return Fixture{}, err
	}
	conflictInput := serviceInput
	conflictInput.Notes = "A materially different retained review."
	conflict, err := evidencereview.NewReview(conflictInput)
	if err != nil {
		return Fixture{}, err
	}
	conflictReviews := []*evidencereview.Review{conflict, human}
	conflictSummary, err := evidencereview.NewSummary(evidencereview.SummaryInput{Case: reviewCase, ReviewHeads: conflictReviews})
	if err != nil {
		return Fixture{}, err
	}
	heldDecision, err := promotion.NewDecision(promotion.DecisionInput{Deployment: deployment, Assessment: research.Parents.Assessment, Policy: policy, PriorDecision: decision})
	if err != nil {
		return Fixture{}, err
	}
	heldParents := evidencereview.CaseInput{Hypothesis: research.Hypothesis, Critic: research.ReadyCritic, PromotionPolicy: policy, PromotionDecision: heldDecision, Deployment: deployment, Assessment: research.Parents.Assessment}
	heldCase, err := evidencereview.NewCase(heldParents)
	if err != nil {
		return Fixture{}, err
	}
	heldServiceInput := serviceInput
	heldServiceInput.Case = heldCase
	heldServiceInput.ReviewedAt = at.Add(2 * time.Minute)
	heldServiceInput.Checks = checks(heldDecision.Digest(), false)
	heldService, err := evidencereview.NewReview(heldServiceInput)
	if err != nil {
		return Fixture{}, err
	}
	heldHumanInput := humanInput
	heldHumanInput.Case = heldCase
	heldHumanInput.ReviewedAt = at.Add(3 * time.Minute)
	heldHumanInput.Checks = checks(heldDecision.Digest(), false)
	heldHuman, err := evidencereview.NewReview(heldHumanInput)
	if err != nil {
		return Fixture{}, err
	}
	heldReviews := []*evidencereview.Review{heldService, heldHuman}
	heldSummary, err := evidencereview.NewSummary(evidencereview.SummaryInput{Case: heldCase, ReviewHeads: heldReviews})
	if err != nil {
		return Fixture{}, err
	}
	return Fixture{parents, reviewCase, reviews, summary, conflictReviews, conflictSummary, heldParents, heldCase, heldReviews, heldSummary}, nil
}
