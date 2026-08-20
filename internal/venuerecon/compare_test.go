package venuerecon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

func matchingComparisonInput(t *testing.T) CompareInput {
	t.Helper()
	localInput := validLocalInput(t)
	localInput.Issues = nil
	local, err := NewLocalSnapshot(localInput)
	if err != nil {
		t.Fatal(err)
	}
	providerInput := validCaptureInput(venue.ProviderAlpaca)
	providerInput.AccountID = localAccountID.String()
	providerInput.HorizonStart = localInput.HorizonStart
	providerInput.HorizonEnd = localInput.HorizonEnd
	providerInput.ProviderAsOf = localInput.HorizonEnd
	providerInput.CaptureStart = localInput.HorizonEnd.Add(time.Minute)
	providerInput.CaptureEnd = localInput.HorizonEnd.Add(2 * time.Minute)
	providerInput.Cash = "100.25"
	providerInput.Equity = "122.25"
	providerInput.Positions[0].SourceAt = localInput.HorizonEnd
	providerInput.Fills[0].SourceAt = localInput.Fills[0].SourceAt
	providerInput.Fills[0].Quantity = localInput.Fills[0].Quantity.String()
	providerInput.Fills[0].Price = localInput.Fills[0].Price.String()
	providerInput.Fills[0].Fee = localInput.Fills[0].Fee.String()
	setFixturePages(&providerInput)
	first, err := NormalizeAlpacaCapture(context.Background(), providerInput, fixedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	secondInput := providerInput
	secondInput.CaptureStart = secondInput.CaptureStart.Add(time.Minute)
	secondInput.CaptureEnd = secondInput.CaptureEnd.Add(time.Minute)
	second, err := NormalizeAlpacaCapture(context.Background(), secondInput, fixedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := AdmitStableProviderSnapshot(first, second)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	return CompareInput{Policy: policy, Provider: admission, Local: local, EquityBasisEquivalent: true}
}

func resealComparisonEvidence(input CompareInput) CompareInput {
	for _, capture := range []*ProviderCapture{input.Provider.Snapshot.first, input.Provider.Snapshot.second} {
		capture.bytes, _ = json.Marshal(capture.canonical)
		capture.digest = sha256Hex(capture.bytes)
		capture.id = economicid.DeterministicUUID(providerCaptureDomain, providerCaptureSchemaV1+"@sha256:"+capture.digest)
	}
	input.Provider.Snapshot.second.canonical = input.Provider.Snapshot.first.canonical
	input.Provider.Snapshot.second.bytes = append(json.RawMessage(nil), input.Provider.Snapshot.first.bytes...)
	input.Provider.Snapshot.second.digest = input.Provider.Snapshot.first.digest
	input.Provider.Snapshot.second.id = input.Provider.Snapshot.first.id
	admission, _ := AdmitStableProviderSnapshot(input.Provider.Snapshot.first, input.Provider.Snapshot.second)
	input.Provider = admission
	input.Local.bytes, _ = json.Marshal(input.Local.canonical)
	input.Local.digest = sha256Hex(input.Local.bytes)
	input.Local.id = economicid.DeterministicUUID(localSnapshotDomain, localSnapshotSchemaV1+"@sha256:"+input.Local.digest)
	return input
}

func TestCompareExactCashPositionsAndFillsIsCleanAndDeterministic(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	first, err := Compare(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compare(input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Clean || len(first.Incidents) != 0 || first.ID == uuid.Nil || first.SHA256 == "" || string(first.CanonicalBytes) != string(second.CanonicalBytes) || first.ID != second.ID {
		t.Fatalf("clean deterministic run = %+v", first)
	}
	wantReasons := map[ReasonCode]bool{ReasonCashMatched: false, ReasonPositionMatched: false, ReasonFillMatched: false, ReasonSnapshotMatched: false}
	for _, result := range first.Results {
		if _, ok := wantReasons[result.Reason]; ok {
			wantReasons[result.Reason] = true
		}
	}
	for reason, found := range wantReasons {
		if !found {
			t.Fatalf("missing matched result %s: %+v", reason, first.Results)
		}
	}
}

func TestCompareEmptyPositionAndFillCollectionsCanMatch(t *testing.T) {
	t.Parallel()
	localInput := validLocalInput(t)
	localInput.Fills = nil
	localInput.Issues = nil
	var payload map[string]any
	if err := json.Unmarshal(localInput.Checkpoint.PayloadBytes, &payload); err != nil {
		t.Fatal(err)
	}
	payload["positions"] = []any{}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	localInput.Checkpoint.PayloadBytes = encoded
	localInput.Checkpoint.OutputChecksum = hex.EncodeToString(digest[:])
	localInput.Checkpoint.PositionCount = 0
	local, err := NewLocalSnapshot(localInput)
	if err != nil {
		t.Fatal(err)
	}
	input := matchingComparisonInput(t)
	input.Local = local
	input.Provider.Snapshot.first.canonical.Positions = nil
	input.Provider.Snapshot.first.canonical.Fills = nil
	input = resealComparisonEvidence(input)
	run, err := Compare(input)
	if err != nil || !run.Clean || len(run.Incidents) != 0 {
		t.Fatalf("empty comparison = %+v, %v", run, err)
	}
}

func TestCompareEveryEconomicDifferenceCreatesSeparateIncident(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	capture := input.Provider.Snapshot.first
	capture.canonical.Cash = "100.26"
	capture.canonical.Positions[0].Quantity = "3"
	capture.canonical.Fills[0].Quantity = "3"
	capture.canonical.Fills[0].Price = "11"
	capture.canonical.Fills[0].Fee = "0.5"
	capture.canonical.Fills[0].Side = lifecycle.SideSell
	capture.canonical.Fills[0].ExternalOrderID = "other-order"
	input = resealComparisonEvidence(input)
	run, err := Compare(input)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ReasonCode]bool{
		ReasonCashMismatch: false, ReasonPositionQuantityMismatch: false, ReasonFillQuantityMismatch: false,
		ReasonFillPriceMismatch: false, ReasonFillFeeMismatch: false, ReasonFillSideMismatch: false, ReasonFillOrderMismatch: false,
	}
	for _, incident := range run.Incidents {
		if _, ok := want[incident.Reason]; ok {
			want[incident.Reason] = true
		}
	}
	for reason, found := range want {
		if !found {
			t.Fatalf("missing incident %s: %+v", reason, run.Incidents)
		}
	}
	if run.Clean || len(run.Incidents) != len(want) {
		t.Fatalf("drift run = %+v", run)
	}
}

func TestCompareMissingSidesRemainMissingNotZero(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	input.Provider.Snapshot.first.canonical.Positions = nil
	input.Provider.Snapshot.first.canonical.Fills = nil
	input = resealComparisonEvidence(input)
	run, err := Compare(input)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ReasonCode]bool{ReasonPositionProviderMissing: false, ReasonFillProviderMissing: false}
	for _, result := range run.Results {
		if _, ok := want[result.Reason]; ok {
			want[result.Reason] = true
			if result.ProviderValue != nil || result.LocalValue == nil {
				t.Fatalf("missing provider was collapsed to value: %+v", result)
			}
		}
	}
	for reason, found := range want {
		if !found {
			t.Fatalf("missing result %s", reason)
		}
	}
}

func TestCompareUnstableAndUnavailableAreNeverMatched(t *testing.T) {
	t.Parallel()
	for _, reason := range []ReasonCode{ReasonSnapshotUnstable, ReasonProviderUnavailable} {
		input := matchingComparisonInput(t)
		input.Provider = &SnapshotAdmission{Reason: reason}
		run, err := Compare(input)
		if err != nil || run.Clean || len(run.Results) != 1 || run.Results[0].Status != StatusNotComparable || len(run.Incidents) != 1 {
			t.Fatalf("reason %s run = %+v, %v", reason, run, err)
		}
	}
}

func TestCompareNonEquivalentEquityBasisIsExplicitlyNotComparable(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	input.EquityBasisEquivalent = false
	run, err := Compare(input)
	if err != nil || run.Clean || len(run.Incidents) != 1 || run.Incidents[0].Reason != ReasonEquityBasisNotComparable {
		t.Fatalf("equity basis run = %+v, %v", run, err)
	}
}

func TestCompareRevisionUsesOriginalKeyAndRemainsPending(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	providerFill := &input.Provider.Snapshot.first.canonical.Fills[0]
	providerFill.SourceID = "revision-provider"
	providerFill.OriginalSourceID = "fill-original"
	providerFill.ObservationClass = lifecycle.ObservationCorrection
	providerFill.ObservationDiscriminator = "corr-1"
	localFill := &input.Local.canonical.Fills[0]
	localFill.SourceID = "revision-local"
	localFill.OriginalSourceID = "fill-original"
	localFill.OriginalFillID = uuid.New().String()
	localFill.ObservationClass = lifecycle.ObservationCorrection
	localFill.ObservationDiscriminator = "corr-1"
	input = resealComparisonEvidence(input)
	run, err := Compare(input)
	if err != nil {
		t.Fatal(err)
	}
	var pending *Result
	for index := range run.Results {
		if run.Results[index].Reason == ReasonCorrectionPending {
			pending = &run.Results[index]
		}
	}
	if pending == nil || !strings.Contains(pending.Key, "alpaca:alpaca/account-activities/FILL:revision:fill-original:correction:corr-1") || pending.Status != StatusNotComparable {
		t.Fatalf("pending revision result = %+v", pending)
	}
}

func TestCompareScopeDriftProducesNotComparableIncident(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	input.Provider.Snapshot.first.canonical.AccountID = "foreign-account"
	input = resealComparisonEvidence(input)
	run, err := Compare(input)
	if err != nil || run.Clean || len(run.Incidents) != 1 || run.Incidents[0].Reason != ReasonUnsupportedFact {
		t.Fatalf("scope run = %+v, %v", run, err)
	}
}

func TestCompareChangedEvidenceChangesGraphIdentityAndTamperingFails(t *testing.T) {
	t.Parallel()
	baseline := matchingComparisonInput(t)
	clean, err := Compare(baseline)
	if err != nil {
		t.Fatal(err)
	}
	changed := matchingComparisonInput(t)
	changed.Provider.Snapshot.first.canonical.Cash = "100.26"
	if _, err := Compare(changed); err == nil {
		t.Fatal("mutable provider fields bypassed canonical evidence validation")
	}
	changed = resealComparisonEvidence(changed)
	drift, err := Compare(changed)
	if err != nil {
		t.Fatal(err)
	}
	if drift.ID == clean.ID || drift.Results[0].ID == clean.Results[0].ID || len(drift.Incidents) != 1 {
		t.Fatalf("changed evidence reused graph identity: clean=%s drift=%s", clean.ID, drift.ID)
	}
}

func TestCompareChangedSourceIdentityProducesTwoMissingSideIncidents(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	input.Provider.Snapshot.first.canonical.Fills[0].SourceID = "provider-only-fill"
	input = resealComparisonEvidence(input)
	run, err := Compare(input)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ReasonCode]bool{ReasonFillLocalMissing: false, ReasonFillProviderMissing: false}
	for _, incident := range run.Incidents {
		if _, ok := want[incident.Reason]; ok {
			want[incident.Reason] = true
		}
	}
	if !want[ReasonFillLocalMissing] || !want[ReasonFillProviderMissing] {
		t.Fatalf("source identity union = %+v", run.Incidents)
	}
}

func TestRunnerUsesOnlyReadOnlyCaptureInterfaces(t *testing.T) {
	t.Parallel()
	input := matchingComparisonInput(t)
	reader := &sequenceReader{values: []*ProviderCapture{input.Provider.Snapshot.first, input.Provider.Snapshot.second}}
	localReader := &recordingLocalReader{input: validLocalInput(t)}
	localReader.input.Issues = nil
	runner := NewRunner(input.Policy, reader, NewLocalSource(localReader))
	request := LocalSnapshotRequest{
		AccountID: localReader.input.AccountID, Provider: localReader.input.Provider,
		Namespace: localReader.input.Namespace, HorizonStart: localReader.input.HorizonStart, HorizonEnd: localReader.input.HorizonEnd,
		CheckpointID: localReader.input.Checkpoint.ID,
	}
	run, err := runner.Run(context.Background(), request)
	if err != nil || run == nil || localReader.calls != 1 || reader.index != 2 {
		t.Fatalf("runner = %+v, local reads=%d provider reads=%d err=%v", run, localReader.calls, reader.index, err)
	}
}
