package evidencereview

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/promotion"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestReviewCaseAndReviewDeterministicRestorePermutation(t *testing.T) {
	caseInput, reviewInput := validInputs(t, false)
	reviewCase, err := NewCase(caseInput)
	if err != nil {
		t.Fatal(err)
	}
	reviewInput.Case = reviewCase
	first, err := NewReview(reviewInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.Disposition() != DispositionSupported {
		t.Fatalf("disposition=%s", first.Disposition())
	}
	permuted := reviewInput
	permuted.Checks = append([]CheckInput(nil), reviewInput.Checks...)
	for l, r := 0, len(permuted.Checks)-1; l < r; l, r = l+1, r-1 {
		permuted.Checks[l], permuted.Checks[r] = permuted.Checks[r], permuted.Checks[l]
	}
	second, err := NewReview(permuted)
	if err != nil || second.ID() != first.ID() || !bytes.Equal(second.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("permutation=%v", err)
	}
	restoredCase, err := CaseFromCanonical(reviewCase.ID(), reviewCase.Digest(), reviewCase.CanonicalBytes(), caseInput)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ReviewFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes(), restoredCase, nil)
	if err != nil || restored.ID() != first.ID() {
		t.Fatalf("restore=%v", err)
	}
	changed := reviewInput
	changed.Notes = "A material reviewed note."
	third, err := NewReview(changed)
	if err != nil || third.ID() == first.ID() {
		t.Fatalf("edit=%v", err)
	}
}

func TestReviewDispositionsAndSupersessionFailClosed(t *testing.T) {
	caseInput, input := validInputs(t, false)
	reviewCase, _ := NewCase(caseInput)
	input.Case = reviewCase
	critical := input
	critical.Checks = append([]CheckInput(nil), input.Checks...)
	for index := range critical.Checks {
		if critical.Checks[index].Name == "safety_boundaries" {
			critical.Checks[index].State = "fail"
			critical.Checks[index].Explanation = "The safety boundary failed independent review."
		}
	}
	rejected, err := NewReview(critical)
	if err != nil || rejected.Disposition() != DispositionRejected {
		t.Fatalf("critical=%v/%v", rejected, err)
	}
	unknown := input
	unknown.Checks = append([]CheckInput(nil), input.Checks...)
	unknown.Checks[0].State = "unknown"
	changes, err := NewReview(unknown)
	if err != nil || changes.Disposition() != DispositionChanges {
		t.Fatalf("unknown=%v/%v", changes, err)
	}
	first, err := NewReview(input)
	if err != nil {
		t.Fatal(err)
	}
	successor := input
	successor.Prior = first
	successor.ReviewedAt = input.ReviewedAt.Add(time.Hour)
	if _, err = NewReview(successor); err != nil {
		t.Fatal(err)
	}
	successor.ReviewedAt = input.ReviewedAt
	if _, err = NewReview(successor); err == nil {
		t.Fatal("stale successor succeeded")
	}
	missing := input
	missing.Checks = missing.Checks[1:]
	if _, err = NewReview(missing); err == nil {
		t.Fatal("missing check succeeded")
	}
	unknownRef := input
	unknownRef.Checks = append([]CheckInput(nil), input.Checks...)
	unknownRef.Checks[0].References = []string{"source:not_retained"}
	if _, err = NewReview(unknownRef); err == nil {
		t.Fatal("unknown reference succeeded")
	}
	unsafe := input
	unsafe.Notes = "deploy now with password=credential"
	if _, err = NewReview(unsafe); err == nil {
		t.Fatal("authority or secret payload succeeded")
	}
}

func TestAIRecommendationCannotOverridePromotionDecision(t *testing.T) {
	readyInput, _ := validInputs(t, false)
	readyCase, err := NewCase(readyInput)
	if err != nil {
		t.Fatal(err)
	}
	rejectInput, _ := validInputs(t, true)
	rejectCase, err := NewCase(rejectInput)
	if err != nil {
		t.Fatal(err)
	}
	if readyCase.CriticRecommendation() != "ready_for_experiment_review" || rejectCase.CriticRecommendation() != "reject" {
		t.Fatalf("recommendations=%s/%s", readyCase.CriticRecommendation(), rejectCase.CriticRecommendation())
	}
	if readyCase.AuthoritativeOutcome() != promotion.OutcomeApproved || rejectCase.AuthoritativeOutcome() != promotion.OutcomeApproved || readyCase.AuthoritativeNextState() != promotion.StateShadow || rejectCase.AuthoritativeNextState() != promotion.StateShadow {
		t.Fatalf("AI recommendation changed authority: %s/%s %s/%s", readyCase.AuthoritativeOutcome(), rejectCase.AuthoritativeOutcome(), readyCase.AuthoritativeNextState(), rejectCase.AuthoritativeNextState())
	}
}

func validInputs(t *testing.T, rejectCritic bool) (CaseInput, ReviewInput) {
	t.Helper()
	fixture, err := researchqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	critic := fixture.ReadyCritic
	if rejectCritic {
		critic = fixture.RejectCritic
	}
	deployment, err := strategycatalog.NewDeployment(strategycatalog.DeploymentInput{VersionID: fixture.Parents.Version.ID(), AccountID: uuid.MustParse("60300000-0000-4000-8000-000000000001"), CapitalBindingID: uuid.MustParse("60300000-0000-4000-8000-000000000002"), Budget: "10000", ScheduleCron: "0 12 * * 1-5", Timezone: "America/Chicago", RiskPolicyVersion: "risk-policy-v1@sha256:" + strings.Repeat("e", 64), Mode: strategycatalog.ExperimentPaperScored})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := promotion.NewPolicy(promotion.PolicyInput{Version: "promotion-policy-v1@evidence-review", RequiredGates: []string{"overall_robustness"}, FailureAction: promotion.ActionHold})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := promotion.NewDecision(promotion.DecisionInput{Deployment: deployment, Assessment: fixture.Parents.Assessment, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	caseInput := CaseInput{fixture.Hypothesis, critic, policy, decision, deployment, fixture.Parents.Assessment}
	refs := []string{"promotion_decision:sha256:" + decision.Digest()}
	checks := []CheckInput{}
	for _, name := range requiredChecks {
		checks = append(checks, CheckInput{Name: name, State: "pass", References: refs, Explanation: "The exact retained evidence passes independent review."})
	}
	review := ReviewInput{Reviewer: ReviewerInput{Key: "reviewer_one", Kind: "independent_service", Organization: "Independent Review Lab", IdentitySHA256: strings.Repeat("1", 64), SystemPromptSHA256: strings.Repeat("2", 64), DeveloperPromptSHA256: strings.Repeat("3", 64), UserPromptSHA256: strings.Repeat("4", 64)}, ReviewedAt: time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC), Checks: checks}
	return caseInput, review
}
