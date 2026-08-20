package dataset

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReviewedPolicyV1CanonicalIdentityAndRestore(t *testing.T) {
	t.Parallel()
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Digest() != "8b1d8dd9328f060b455cbd096829c01429017223baf37154aeb6059cf64b894c" {
		t.Fatalf("reviewed policy digest = %s", policy.Digest())
	}
	if policy.ID() == uuid.Nil || len(policy.Digest()) != 64 || !strings.HasPrefix(policy.Version(), PolicySchemaV1+"@sha256:") {
		t.Fatalf("policy identity = %s/%s/%s", policy.ID(), policy.Version(), policy.Digest())
	}
	createdAt := time.Date(2026, 8, 20, 12, 0, 0, 123456000, time.UTC)
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID() != policy.ID() || !bytes.Equal(restored.CanonicalBytes(), policy.CanonicalBytes()) {
		t.Fatal("restored policy differs")
	}
	reordered := ReviewedPolicyV1Input()
	for left, right := 0, len(reordered.Kinds)-1; left < right; left, right = left+1, right-1 {
		reordered.Kinds[left], reordered.Kinds[right] = reordered.Kinds[right], reordered.Kinds[left]
	}
	for left, right := 0, len(reordered.Rules)-1; left < right; left, right = left+1, right-1 {
		reordered.Rules[left], reordered.Rules[right] = reordered.Rules[right], reordered.Rules[left]
	}
	second, err := NewPolicy(reordered)
	if err != nil || second.ID() != policy.ID() {
		t.Fatalf("reordered policy = %v/%v", second, err)
	}
}

func TestReviewedPolicyV1RejectsDriftAndTampering(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*PolicyInput){
		"schema":                func(value *PolicyInput) { value.Schema = "dataset-quality-policy-v2" },
		"missing kind":          func(value *PolicyInput) { value.Kinds = value.Kinds[1:] },
		"unknown kind":          func(value *PolicyInput) { value.Kinds[0] = "mystery" },
		"missing rule":          func(value *PolicyInput) { value.Rules = value.Rules[1:] },
		"changed required":      func(value *PolicyInput) { value.Rules[0].Required = !value.Rules[0].Required },
		"changed severity":      func(value *PolicyInput) { value.Rules[0].Severity = SeverityHigh },
		"changed applicability": func(value *PolicyInput) { value.Rules[0].Kinds = append(value.Rules[0].Kinds, KindBars) },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := ReviewedPolicyV1Input()
			mutate(&input)
			if _, err := NewPolicy(input); err == nil {
				t.Fatal("drifted policy accepted")
			}
		})
	}
	policy, _ := NewPolicy(ReviewedPolicyV1Input())
	artifact, _ := policy.NewArtifact(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	artifact.CanonicalBytes[0] = '['
	if _, err := PolicyFromArtifact(*artifact); err == nil {
		t.Fatal("tampered artifact accepted")
	}
	artifact, _ = policy.NewArtifact(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	artifact.CreatedAt = artifact.CreatedAt.In(time.FixedZone("not-utc", 0))
	if err := artifact.Validate(); err == nil {
		t.Fatal("non-UTC artifact accepted")
	}
}
