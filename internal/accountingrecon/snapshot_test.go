package accountingrecon

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewSnapshotCanonicalizesOrderScaleAndIdentity(t *testing.T) {
	t.Parallel()

	input := validSnapshotInput(SourceLegacy)
	input.Metrics = []MetricInput{
		{Kind: MetricEquity, Value: decimal.RequireFromString("101.00"), Provenance: ProvenanceBinaryFloat},
		{Kind: MetricCash, Value: decimal.RequireFromString("99.5000"), Provenance: ProvenanceBinaryFloat},
	}
	input.Positions = []PositionInput{
		{InstrumentID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), Quantity: decimal.RequireFromString("-2.00"), Provenance: ProvenanceBinaryFloat},
		{InstrumentID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Quantity: decimal.RequireFromString("1.5000"), Provenance: ProvenanceBinaryFloat},
	}

	first, err := NewSnapshot(input)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	reordered := input
	reordered.Metrics = []MetricInput{input.Metrics[1], input.Metrics[0]}
	reordered.Positions = []PositionInput{input.Positions[1], input.Positions[0]}
	second, err := NewSnapshot(reordered)
	if err != nil {
		t.Fatalf("NewSnapshot(reordered) error = %v", err)
	}
	if first.ID != second.ID || first.Checksum != second.Checksum || string(first.PayloadBytes) != string(second.PayloadBytes) {
		t.Fatalf("canonical snapshots differ: %s/%s and %s/%s", first.ID, first.Checksum, second.ID, second.Checksum)
	}
	if got, want := first.Metrics[0].Value.String(), "99.5"; got != want {
		t.Fatalf("canonical cash = %q, want %q", got, want)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNewSnapshotDistinguishesMissingFromZero(t *testing.T) {
	t.Parallel()

	zeroInput := validSnapshotInput(SourceLegacy)
	zeroInput.Metrics = []MetricInput{{Kind: MetricFees, Value: decimal.Zero, Provenance: ProvenanceExactDecimal}}
	zero, err := NewSnapshot(zeroInput)
	if err != nil {
		t.Fatalf("NewSnapshot(zero) error = %v", err)
	}

	missingInput := validSnapshotInput(SourceLegacy)
	missingInput.Missing = []MissingFactInput{{FactKey: MetricFactKey(MetricFees), ReasonCode: MissingSourceUnavailable, EvidenceRef: "legacy-paper:fees-not-exposed"}}
	missing, err := NewSnapshot(missingInput)
	if err != nil {
		t.Fatalf("NewSnapshot(missing) error = %v", err)
	}
	if zero.ID == missing.ID || zero.Checksum == missing.Checksum {
		t.Fatal("zero and missing snapshots unexpectedly share identity")
	}
}

func TestDecodeSnapshotAndRunRequireExactCanonicalBytes(t *testing.T) {
	t.Parallel()

	legacy, ledgerSnapshot := completePair(t)
	run, err := Compare(ComparisonInput{Legacy: legacy, Ledger: ledgerSnapshot, Generator: "worker", GeneratedAt: legacy.AsOf.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	decodedSnapshot, err := DecodeSnapshot(legacy.PayloadBytes)
	if err != nil {
		t.Fatalf("DecodeSnapshot() error = %v", err)
	}
	if decodedSnapshot.ID != legacy.ID || decodedSnapshot.Checksum != legacy.Checksum {
		t.Fatalf("decoded snapshot = %s/%s", decodedSnapshot.ID, decodedSnapshot.Checksum)
	}
	decodedRun, err := DecodeRun(run.PayloadBytes)
	if err != nil {
		t.Fatalf("DecodeRun() error = %v", err)
	}
	if decodedRun.ID != run.ID || decodedRun.Checksum != run.Checksum {
		t.Fatalf("decoded run = %s/%s", decodedRun.ID, decodedRun.Checksum)
	}
	if _, err := DecodeSnapshot(append([]byte(" "), legacy.PayloadBytes...)); err == nil {
		t.Fatal("DecodeSnapshot(noncanonical bytes) unexpectedly succeeded")
	}
}

func TestSnapshotAndRunValidationRejectMutableEnvelopeOrResultOverrides(t *testing.T) {
	t.Parallel()

	snapshot, _ := completePair(t)
	snapshot.Version = "forged-version"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("Snapshot.Validate() accepted a mutable version override")
	}

	for name, mutate := range map[string]func(*Run){
		"policy":    func(run *Run) { run.PolicyVersion = "forged-policy" },
		"synthetic": func(run *Run) { run.Synthetic = false },
		"result":    func(run *Run) { run.Results[0].Status = StatusUnexplained },
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			run := equalRunAt(t, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), name == "synthetic")
			run.Results = append([]Result(nil), run.Results...)
			mutate(run)
			if err := run.Validate(); err == nil {
				t.Fatal("Run.Validate() accepted a mutable envelope/result override")
			}
		})
	}
}

func TestNewSnapshotRejectsInvalidBoundaryAndDuplicateFacts(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*SnapshotInput){
		"nil account":       func(input *SnapshotInput) { input.AccountID = uuid.Nil },
		"non utc as of":     func(input *SnapshotInput) { input.AsOf = input.AsOf.In(time.FixedZone("offset", 3600)) },
		"observed before":   func(input *SnapshotInput) { input.ObservedAt = input.AsOf.Add(-time.Second) },
		"bad currency":      func(input *SnapshotInput) { input.Currency = "usd" },
		"blank evidence":    func(input *SnapshotInput) { input.EvidenceID = " " },
		"bad evidence hash": func(input *SnapshotInput) { input.EvidenceChecksum = "abc" },
		"duplicate metric": func(input *SnapshotInput) {
			input.Metrics = []MetricInput{{Kind: MetricCash, Value: decimal.Zero, Provenance: ProvenanceExactDecimal}, {Kind: MetricCash, Value: decimal.NewFromInt(1), Provenance: ProvenanceExactDecimal}}
		},
		"duplicate position": func(input *SnapshotInput) {
			id := uuid.New()
			input.Positions = []PositionInput{{InstrumentID: id, Quantity: decimal.NewFromInt(1), Provenance: ProvenanceExactDecimal}, {InstrumentID: id, Quantity: decimal.Zero, Provenance: ProvenanceExactDecimal}}
		},
		"fact both present and missing": func(input *SnapshotInput) {
			input.Metrics = []MetricInput{{Kind: MetricCash, Value: decimal.Zero, Provenance: ProvenanceExactDecimal}}
			input.Missing = []MissingFactInput{{FactKey: MetricFactKey(MetricCash), ReasonCode: MissingSourceUnavailable, EvidenceRef: "evidence:cash"}}
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := validSnapshotInput(SourceLegacy)
			mutate(&input)
			if _, err := NewSnapshot(input); err == nil {
				t.Fatal("NewSnapshot() unexpectedly succeeded")
			}
		})
	}
}

func validSnapshotInput(source SnapshotSource) SnapshotInput {
	asOf := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	return SnapshotInput{
		Source:                   source,
		AccountID:                uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		AsOf:                     asOf,
		ObservedAt:               asOf.Add(time.Second),
		Currency:                 "USD",
		ProjectionVersion:        "ledger_fifo_v1",
		MarkSource:               "polygon",
		MarkNamespace:            "quotes/scored",
		MaxMarkAge:               5 * time.Minute,
		CaptureFenceID:           "account-fence:fixture",
		CaptureEpoch:             1,
		EvidenceID:               source.String() + ":fixture-1",
		EvidenceChecksum:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PositionCoverageComplete: true,
	}
}
