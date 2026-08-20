package researchworkflow

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestCriticDeterministicRecommendationRestoreAndPermutation(t *testing.T) {
	input := validCriticInput(t)
	first, err := NewCritic(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Recommendation() != "ready_for_experiment_review" {
		t.Fatalf("recommendation=%s", first.Recommendation())
	}
	permuted := input
	permuted.Findings = []FindingInput{input.Findings[1], input.Findings[0]}
	permuted.Findings[0].References = []string{input.Findings[1].References[1], input.Findings[1].References[0]}
	permuted.Checks = append([]CheckInput(nil), input.Checks...)
	for left, right := 0, len(permuted.Checks)-1; left < right; left, right = left+1, right-1 {
		permuted.Checks[left], permuted.Checks[right] = permuted.Checks[right], permuted.Checks[left]
	}
	second, err := NewCritic(permuted)
	if err != nil || second.ID() != first.ID() || !bytes.Equal(second.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("permutation diverged: %v", err)
	}
	restored, err := CriticFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes(), input.Hypothesis)
	if err != nil || restored.Recommendation() != first.Recommendation() {
		t.Fatalf("restore failed: %v", err)
	}
	changed := input
	changed.Findings = append([]FindingInput(nil), input.Findings...)
	changed.Findings[0].Explanation = "The independently reviewed evidence changed materially."
	third, err := NewCritic(changed)
	if err != nil || third.ID() == first.ID() {
		t.Fatalf("semantic edit retained identity: %v", err)
	}
}

func TestCriticFailsClosedOnHighCriticalUnknownAndMissingReferences(t *testing.T) {
	input := validCriticInput(t)
	input.Findings = append(input.Findings, FindingInput{"open_high", "leakage", "high", "open", []string{"test:leakage_gate"}, "An unresolved high-severity leakage issue remains."})
	critic, err := NewCritic(input)
	if err != nil || critic.Recommendation() != "reject" {
		t.Fatalf("high finding recommendation=%v err=%v", critic, err)
	}

	input = validCriticInput(t)
	input.Checks[0].State = "unknown"
	critic, err = NewCritic(input)
	if err != nil || critic.Recommendation() != "revise" {
		t.Fatalf("unknown check recommendation=%v err=%v", critic, err)
	}

	input = validCriticInput(t)
	input.Findings[0].References = []string{"source:not_retained"}
	if _, err := NewCritic(input); err == nil {
		t.Fatal("unknown reference succeeded")
	}

	input = validCriticInput(t)
	input.Checks = input.Checks[1:]
	if _, err := NewCritic(input); err == nil {
		t.Fatal("incomplete checks succeeded")
	}

	input = validCriticInput(t)
	input.Provenance = ProvenanceInput{input.Hypothesis.canonical.Provenance.Provider, input.Hypothesis.canonical.Provenance.Model, input.Hypothesis.canonical.Provenance.SystemPromptSHA256, input.Hypothesis.canonical.Provenance.DeveloperPromptSHA256, input.Hypothesis.canonical.Provenance.UserPromptSHA256, 1, 1, "USD", "0"}
	if _, err := NewCritic(input); err == nil {
		t.Fatal("author provenance reused as independent review")
	}
}

func TestCriticHasNoLifecycleExecutionOrTradingAuthority(t *testing.T) {
	typeOf := reflect.TypeOf(&Critic{})
	for index := 0; index < typeOf.NumMethod(); index++ {
		method := strings.ToLower(typeOf.Method(index).Name)
		for _, forbidden := range []string{"approve", "deploy", "experiment", "intent", "order", "promote", "reserve", "retire", "schedule"} {
			if strings.Contains(method, forbidden) {
				t.Fatalf("forbidden authority exposed: %s", method)
			}
		}
	}
}

func validCriticInput(t *testing.T) CriticInput {
	t.Helper()
	hypothesis, err := NewHypothesis(validHypothesisInput(t))
	if err != nil {
		t.Fatal(err)
	}
	checks := make([]CheckInput, 0, len(requiredCriticChecks))
	for _, name := range requiredCriticChecks {
		reference := "hypothesis:sha256:" + hypothesis.Digest()
		switch name {
		case "source_coverage":
			reference = "source:paper_a"
		case "leakage":
			reference = "test:leakage_gate"
		case "cost_capacity":
			reference = "test:cost_gate"
		case "test_completeness":
			reference = "test:refutation_gate"
		case "multiple_testing":
			reference = "assessment:sha256:" + hypothesis.canonical.Parents.AssessmentSHA256
		case "reproducibility":
			reference = "version:sha256:" + hypothesis.canonical.Parents.VersionSHA256
		}
		checks = append(checks, CheckInput{name, "pass", []string{reference}, "Independent review found complete retained evidence for this check."})
	}
	return CriticInput{Hypothesis: hypothesis, ReviewKey: "independent_review_v1", Provenance: ProvenanceInput{"anthropic", "critic-model", strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), 1500, 600, "USD", "0.3"}, Findings: []FindingInput{{"license_note", "source_coverage", "low", "resolved", []string{"source:paper_a"}, "The retained license was independently confirmed."}, {"reproduction_note", "reproducibility", "medium", "resolved", []string{"hypothesis:sha256:" + hypothesis.Digest(), "version:sha256:" + hypothesis.canonical.Parents.VersionSHA256}, "The canonical reconstruction was independently confirmed."}}, Checks: checks}
}
