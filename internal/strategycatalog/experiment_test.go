package strategycatalog

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestExperimentCanonicalIdentityRestoreAndSeedChange(t *testing.T) {
	input := validExperimentInput()
	first, err := NewExperiment(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewExperiment(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() == uuid.Nil || first.ID() != retry.ID() || first.Digest() != retry.Digest() || first.State() != ExperimentDeclared {
		t.Fatal("identical experiment declaration did not converge")
	}
	input.Seed++
	changed, err := NewExperiment(input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() == first.ID() {
		t.Fatal("changed seed reused experiment identity")
	}
	restored, err := ExperimentFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || restored.ID() != first.ID() || restored.ManifestID() != validExperimentInput().ManifestID || restored.Mode() != ExperimentPaperScored {
		t.Fatalf("restore experiment = %+v, %v", restored, err)
	}
}

func TestExperimentRejectsQuarantinedScoredAndInvalidEvidence(t *testing.T) {
	valid := validExperimentInput()
	for name, mutate := range map[string]func(*ExperimentInput){
		"version":     func(value *ExperimentInput) { value.VersionID = uuid.Nil },
		"account":     func(value *ExperimentInput) { value.AccountID = uuid.Nil },
		"binding":     func(value *ExperimentInput) { value.CapitalBindingID = uuid.Nil },
		"manifest":    func(value *ExperimentInput) { value.ManifestID = uuid.Nil },
		"quality":     func(value *ExperimentInput) { value.QualityResultID = uuid.Nil },
		"simulation":  func(value *ExperimentInput) { value.SimulationPolicyVersion = "simulation-policy-v1@sha256:bad" },
		"capital":     func(value *ExperimentInput) { value.CapitalPolicyVersion = "capital-margin-policy-v1@sha256:bad" },
		"mode":        func(value *ExperimentInput) { value.Mode = "live" },
		"quarantined": func(value *ExperimentInput) { value.DatasetQuarantined = true },
		"non UTC": func(value *ExperimentInput) {
			value.EvaluationStart = value.EvaluationStart.In(time.FixedZone("offset", 3600))
		},
		"sub microsecond": func(value *ExperimentInput) { value.EvaluationStart = value.EvaluationStart.Add(time.Nanosecond) },
		"reversed window": func(value *ExperimentInput) { value.EvaluationEnd = value.EvaluationStart.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := NewExperiment(input); err == nil {
				t.Fatal("invalid experiment declaration succeeded")
			}
		})
	}
	stress := valid
	stress.Mode, stress.DatasetQuarantined = ExperimentPaperStress, true
	if _, err := NewExperiment(stress); err != nil {
		t.Fatalf("explicit quarantined stress declaration: %v", err)
	}
}

func TestExperimentRestoreRejectsStateOrPayloadTampering(t *testing.T) {
	experiment, err := NewExperiment(validExperimentInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, tampered := range [][]byte{
		bytes.Replace(experiment.CanonicalBytes(), []byte(`"declared"`), []byte(`"completed"`), 1),
		bytes.Replace(experiment.CanonicalBytes(), []byte(`"seed":42`), []byte(`"seed":43`), 1),
	} {
		if _, err := ExperimentFromCanonical(experiment.ID(), experiment.Digest(), tampered); err == nil {
			t.Fatal("tampered experiment restored")
		}
	}
}

func validExperimentInput() ExperimentInput {
	start := time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC)
	return ExperimentInput{
		VersionID:               uuid.MustParse("30200000-0000-4000-8000-000000000010"),
		AccountID:               uuid.MustParse("30200000-0000-4000-8000-000000000011"),
		CapitalBindingID:        uuid.MustParse("30200000-0000-4000-8000-000000000012"),
		ManifestID:              uuid.MustParse("30200000-0000-4000-8000-000000000013"),
		QualityResultID:         uuid.MustParse("30200000-0000-4000-8000-000000000014"),
		SimulationPolicyVersion: simulation.PolicySchemaV1 + "@sha256:" + strings.Repeat("a", 64),
		CapitalPolicyVersion:    capital.PolicySchemaV1 + "@sha256:" + strings.Repeat("b", 64),
		Mode:                    ExperimentPaperScored, EvaluationStart: start, EvaluationEnd: start.Add(365 * 24 * time.Hour), Seed: 42,
	}
}
