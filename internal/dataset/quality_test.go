package dataset

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
)

func cleanQualityInput(t *testing.T) QualityInput {
	t.Helper()
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewManifest(validManifestInput())
	if err != nil {
		t.Fatal(err)
	}
	partition := manifest.Partitions()[0]
	validTo := manifestCutoff.Add(24 * time.Hour)
	return QualityInput{
		Policy: policy, Manifest: manifest,
		InstrumentWindows:   []InstrumentWindow{{InstrumentID: manifestInstrument, ValidFrom: manifestCutoff.Add(-24 * time.Hour), ValidTo: &validTo, EvidenceSHA256: manifestHash("instrument-window")}},
		Sessions:            []SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{manifestCutoff.Add(-2 * time.Hour), manifestCutoff.Add(-time.Hour)}, EvidenceSHA256: manifestHash("sessions")}},
		ExternalAssessments: []ExternalAssessment{{PartitionContentSHA256: partition.ContentSHA256, Check: CheckProviderSpotCompare, Status: CheckPassed, EvidenceSHA256: manifestHash("spot")}},
	}
}

func TestQualityEvaluatorProducesDeterministicPassingEvidence(t *testing.T) {
	t.Parallel()
	input := cleanQualityInput(t)
	result, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID() == uuid.Nil || result.Quarantined() || len(result.Checks()) == 0 || len(result.Findings()) != 0 {
		t.Fatalf("result = %s quarantined:%v checks:%+v findings:%+v", result.ID(), result.Quarantined(), result.Checks(), result.Findings())
	}
	restored, err := QualityResultFromCanonical(result.ID(), result.Digest(), result.CanonicalBytes())
	if err != nil || restored.ID() != result.ID() {
		t.Fatalf("restore = %+v/%v", restored, err)
	}
	input.InstrumentWindows = append([]InstrumentWindow(nil), input.InstrumentWindows...)
	input.Sessions[0].ExpectedEffectiveAt[0], input.Sessions[0].ExpectedEffectiveAt[1] = input.Sessions[0].ExpectedEffectiveAt[1], input.Sessions[0].ExpectedEffectiveAt[0]
	second, err := Evaluate(input)
	if err != nil || second.ID() != result.ID() || !bytes.Equal(second.CanonicalBytes(), result.CanonicalBytes()) {
		t.Fatalf("reordered = %+v/%v", second, err)
	}
}

func TestQualityEvaluatorQuarantinesMissingRequiredAndFailedChecks(t *testing.T) {
	t.Parallel()
	input := cleanQualityInput(t)
	input.InstrumentWindows = nil
	result, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Quarantined() || len(result.Findings()) != 1 || result.Findings()[0].Code != "instrument_window_not_assessed" {
		t.Fatalf("missing instrument result = %+v", result.Findings())
	}
	input = cleanQualityInput(t)
	input.Sessions[0].ExpectedEffectiveAt = append(input.Sessions[0].ExpectedEffectiveAt, manifestCutoff.Add(-30*time.Minute))
	result, err = Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Quarantined() || len(result.Findings()) != 1 || result.Findings()[0].Code != "missing_session" {
		t.Fatalf("missing session result = %+v", result.Findings())
	}
}

func TestQualityEvaluatorRequiresCorporateActionAssessmentForBars(t *testing.T) {
	t.Parallel()
	input := validManifestInput()
	input.Partitions[0].Kind = KindBars
	input.Partitions[0].Observations[0].Bid, input.Partitions[0].Observations[0].Ask = nil, nil
	input.Partitions[0].Observations[1].Bid, input.Partitions[0].Observations[1].Ask = nil, nil
	manifest, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := NewPolicy(ReviewedPolicyV1Input())
	partition := manifest.Partitions()[0]
	validTo := manifestCutoff.Add(time.Hour)
	quality := QualityInput{Policy: policy, Manifest: manifest,
		InstrumentWindows: []InstrumentWindow{{InstrumentID: manifestInstrument, ValidFrom: manifestCutoff.Add(-24 * time.Hour), ValidTo: &validTo, EvidenceSHA256: manifestHash("window")}},
		Sessions:          []SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{manifestCutoff.Add(-2 * time.Hour), manifestCutoff.Add(-time.Hour)}, EvidenceSHA256: manifestHash("sessions")}},
	}
	result, err := Evaluate(quality)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Quarantined() {
		t.Fatal("unassessed corporate action check passed")
	}
	quality.ExternalAssessments = []ExternalAssessment{{PartitionContentSHA256: partition.ContentSHA256, Check: CheckCorporateActions, Status: CheckPassed, EvidenceSHA256: manifestHash("corporate")}}
	result, err = Evaluate(quality)
	if err != nil || result.Quarantined() {
		t.Fatalf("assessed bars = %+v/%v", result, err)
	}
}

func TestQualityEvaluatorRejectsInvalidInjectedEvidenceAndTamper(t *testing.T) {
	t.Parallel()
	input := cleanQualityInput(t)
	input.Sessions[0].EvidenceSHA256 = "bad"
	if _, err := Evaluate(input); err == nil {
		t.Fatal("invalid session evidence accepted")
	}
	input = cleanQualityInput(t)
	input.ExternalAssessments[0].Status = CheckNotAssessed
	if _, err := Evaluate(input); err == nil {
		t.Fatal("caller-authored not-assessed accepted")
	}
	input = cleanQualityInput(t)
	result, _ := Evaluate(input)
	raw := result.CanonicalBytes()
	raw[len(raw)-2] ^= 1
	if _, err := QualityResultFromCanonical(result.ID(), result.Digest(), raw); err == nil {
		t.Fatal("tampered result accepted")
	}
}
