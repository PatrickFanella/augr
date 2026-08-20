package evidencereview

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSummaryDeterministicPermutationRestoreAndAuthority(t *testing.T) {
	caseInput, firstInput := validInputs(t, false)
	reviewCase, err := NewCase(caseInput)
	if err != nil {
		t.Fatal(err)
	}
	firstInput.Case = reviewCase
	first, err := NewReview(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := firstInput
	secondInput.Reviewer = ReviewerInput{Key: "reviewer_two", Kind: "human", Organization: "Independent Human Review", IdentitySHA256: strings.Repeat("5", 64)}
	secondInput.ReviewedAt = firstInput.ReviewedAt.Add(time.Minute)
	second, err := NewReview(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := NewSummary(SummaryInput{reviewCase, []*Review{second, first}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Consensus() != DispositionSupported || summary.EscalationRequired() {
		t.Fatalf("summary=%s/%t", summary.Consensus(), summary.EscalationRequired())
	}
	retry, err := NewSummary(SummaryInput{reviewCase, []*Review{first, second}})
	if err != nil || retry.ID() != summary.ID() || !bytes.Equal(retry.CanonicalBytes(), summary.CanonicalBytes()) {
		t.Fatalf("permutation=%v", err)
	}
	restored, err := SummaryFromCanonical(summary.ID(), summary.Digest(), summary.CanonicalBytes(), SummaryInput{reviewCase, []*Review{first, second}})
	if err != nil || restored.ID() != summary.ID() {
		t.Fatalf("restore=%v", err)
	}
	for _, value := range []any{&Case{}, &Review{}, &Summary{}} {
		typ := reflect.TypeOf(value)
		for index := 0; index < typ.NumMethod(); index++ {
			method := strings.ToLower(typ.Method(index).Name)
			for _, forbidden := range []string{"approve", "deploy", "intent", "order", "promote", "retire", "schedule", "statechange"} {
				if strings.Contains(method, forbidden) {
					t.Fatalf("forbidden authority=%s", method)
				}
			}
		}
	}
}

func TestSummaryDisagreementEscalatesWithoutChangingPromotion(t *testing.T) {
	caseInput, firstInput := validInputs(t, false)
	reviewCase, _ := NewCase(caseInput)
	firstInput.Case = reviewCase
	first, _ := NewReview(firstInput)
	secondInput := firstInput
	secondInput.Reviewer = ReviewerInput{Key: "reviewer_two", Kind: "human", Organization: "Independent Human Review", IdentitySHA256: strings.Repeat("6", 64)}
	secondInput.ReviewedAt = firstInput.ReviewedAt.Add(time.Minute)
	secondInput.Checks = append([]CheckInput(nil), firstInput.Checks...)
	for index := range secondInput.Checks {
		if secondInput.Checks[index].Name == "cost_capacity" {
			secondInput.Checks[index].State = "unknown"
		}
	}
	second, err := NewReview(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := NewSummary(SummaryInput{reviewCase, []*Review{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Consensus() != "disagreement" || !summary.EscalationRequired() {
		t.Fatalf("summary=%s/%t", summary.Consensus(), summary.EscalationRequired())
	}
	if summary.AuthoritativeOutcome() != reviewCase.AuthoritativeOutcome() || summary.AuthoritativeNextState() != reviewCase.AuthoritativeNextState() {
		t.Fatal("summary changed promotion authority")
	}
	if _, err = NewSummary(SummaryInput{reviewCase, []*Review{first, first}}); err == nil {
		t.Fatal("duplicate reviewer head succeeded")
	}
	if _, err = NewSummary(SummaryInput{reviewCase, []*Review{first}}); err == nil {
		t.Fatal("single review summary succeeded")
	}
}
