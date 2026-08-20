package dataset

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	manifestCutoff     = time.Date(2026, 8, 20, 16, 0, 0, 0, time.UTC)
	manifestInstrument = uuid.MustParse("11111111-1111-4111-8111-111111111111")
)

func manifestString(value string) *string     { return &value }
func manifestTime(value time.Time) *time.Time { return &value }
func manifestHash(label string) string        { return hashBytes([]byte(label)) }

func validManifestInput() ManifestInput {
	firstAt := manifestCutoff.Add(-2 * time.Hour)
	secondAt := manifestCutoff.Add(-time.Hour)
	return ManifestInput{DecisionCutoff: manifestCutoff, Partitions: []PartitionInput{{
		Kind: KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "quotes/fixture",
		RequestSHA256: manifestHash("request"), MediaType: "application/json", SymbologyVersion: "instrument-v1",
		AdjustmentPolicy: "raw", Timezone: "UTC", Calendar: "XNYS-v1", Revision: "v1",
		License: "test-only", RetentionPolicy: "retain-test-evidence",
		Observations: []ObservationInput{
			{SourceKey: "quote-2", InstrumentID: manifestInstrument, EffectiveAt: secondAt, ObservedAt: secondAt.Add(time.Minute), AvailableAt: secondAt.Add(2 * time.Minute), Revision: "1", ContentSHA256: manifestHash("quote-2"), Bid: manifestString("10.1"), Ask: manifestString("10.2"), Volume: manifestString("2")},
			{SourceKey: "quote-1", InstrumentID: manifestInstrument, EffectiveAt: firstAt, PublishedAt: manifestTime(firstAt.Add(30 * time.Second)), ObservedAt: firstAt.Add(time.Minute), AvailableAt: firstAt.Add(2 * time.Minute), Revision: "1", ContentSHA256: manifestHash("quote-1"), Bid: manifestString("10"), Ask: manifestString("10.1"), Volume: manifestString("1")},
		},
	}}}
}

func TestManifestCanonicalizesOrderDerivesBoundsAndRestores(t *testing.T) {
	t.Parallel()
	input := validManifestInput()
	manifest, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	partitions := manifest.Partitions()
	if manifest.ID() == uuid.Nil || len(partitions) != 1 || partitions[0].RowCount != 2 ||
		partitions[0].Observations[0].SourceKey != "quote-1" || partitions[0].AvailableEnd != formatTime(manifestCutoff.Add(-58*time.Minute)) {
		t.Fatalf("manifest = %s %+v", manifest.ID(), partitions)
	}
	restored, err := ManifestFromCanonical(manifest.ID(), manifest.Digest(), manifest.CanonicalBytes())
	if err != nil || restored.ID() != manifest.ID() {
		t.Fatalf("restore = %+v/%v", restored, err)
	}
	reordered := validManifestInput()
	reordered.Partitions[0].Observations[0], reordered.Partitions[0].Observations[1] = reordered.Partitions[0].Observations[1], reordered.Partitions[0].Observations[0]
	second, err := NewManifest(reordered)
	if err != nil || second.ID() != manifest.ID() || !bytes.Equal(second.CanonicalBytes(), manifest.CanonicalBytes()) {
		t.Fatalf("reordered = %+v/%v", second, err)
	}
	partitions[0].Observations[0].SourceKey = "mutated"
	if manifest.Partitions()[0].Observations[0].SourceKey == "mutated" {
		t.Fatal("getter exposed manifest storage")
	}
}

func TestManifestCorrectionTargetsPriorOriginal(t *testing.T) {
	t.Parallel()
	input := validManifestInput()
	base := input.Partitions[0].Observations[0]
	base.SourceKey = "quote-2-correction"
	base.Revision = "2"
	base.CorrectionOf = "quote-2"
	base.AvailableAt = base.AvailableAt.Add(time.Minute)
	base.ContentSHA256 = manifestHash("quote-2-correction")
	input.Partitions[0].Observations = append(input.Partitions[0].Observations, base)
	if _, err := NewManifest(input); err != nil {
		t.Fatal(err)
	}
	input.Partitions[0].Observations[2].CorrectionOf = "missing"
	if _, err := NewManifest(input); err == nil {
		t.Fatal("missing correction target accepted")
	}
}

func TestManifestMixedKindsCanonicalizePartitionOrder(t *testing.T) {
	t.Parallel()
	input := validManifestInput()
	at := manifestCutoff.Add(-3 * time.Hour)
	input.Partitions = append(input.Partitions, PartitionInput{
		Kind: KindFilings, Provider: "sec", Source: "edgar", Namespace: "sec/13f",
		RequestSHA256: manifestHash("filing-request"), MediaType: "application/xml",
		SymbologyVersion: "cusip-v1", AdjustmentPolicy: "raw", Timezone: "UTC", Calendar: "SEC-v1",
		Revision: "original", License: "public-record", RetentionPolicy: "retain",
		Observations: []ObservationInput{{
			SourceKey: "accession-1", EffectiveAt: at.Add(-45 * 24 * time.Hour), PublishedAt: manifestTime(at),
			ObservedAt: at.Add(time.Minute), AvailableAt: at.Add(2 * time.Minute),
			Revision: "original", ContentSHA256: manifestHash("filing"),
		}},
	})
	first, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Partitions[0], input.Partitions[1] = input.Partitions[1], input.Partitions[0]
	second, err := NewManifest(input)
	if err != nil || first.ID() != second.ID() || len(first.Partitions()) != 2 {
		t.Fatalf("mixed manifest = %s/%s/%v", first.ID(), second.ID(), err)
	}
}

func TestManifestRejectsLookaheadInvalidTimeHashDecimalAndIdentity(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*ManifestInput){
		"cutoff timezone":      func(value *ManifestInput) { value.DecisionCutoff = value.DecisionCutoff.In(time.FixedZone("x", 0)) },
		"no partitions":        func(value *ManifestInput) { value.Partitions = nil },
		"request hash":         func(value *ManifestInput) { value.Partitions[0].RequestSHA256 = "bad" },
		"unknown kind":         func(value *ManifestInput) { value.Partitions[0].Kind = "unknown" },
		"missing availability": func(value *ManifestInput) { value.Partitions[0].Observations[0].AvailableAt = time.Time{} },
		"lookahead": func(value *ManifestInput) {
			value.Partitions[0].Observations[0].AvailableAt = value.DecisionCutoff.Add(time.Microsecond)
		},
		"observed after available": func(value *ManifestInput) {
			value.Partitions[0].Observations[0].ObservedAt = value.Partitions[0].Observations[0].AvailableAt.Add(time.Microsecond)
		},
		"published after observed": func(value *ManifestInput) {
			at := value.Partitions[0].Observations[0].ObservedAt.Add(time.Microsecond)
			value.Partitions[0].Observations[0].PublishedAt = &at
		},
		"duplicate identity": func(value *ManifestInput) {
			value.Partitions[0].Observations[1].SourceKey = value.Partitions[0].Observations[0].SourceKey
			value.Partitions[0].Observations[1].Revision = value.Partitions[0].Observations[0].Revision
		},
		"crossed quote":        func(value *ManifestInput) { value.Partitions[0].Observations[0].Bid = manifestString("11") },
		"negative volume":      func(value *ManifestInput) { value.Partitions[0].Observations[0].Volume = manifestString("-1") },
		"exponent":             func(value *ManifestInput) { value.Partitions[0].Observations[0].Bid = manifestString("1e1") },
		"noncanonical decimal": func(value *ManifestInput) { value.Partitions[0].Observations[0].Bid = manifestString("10.10") },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := validManifestInput()
			mutate(&input)
			if _, err := NewManifest(input); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	manifest, _ := NewManifest(validManifestInput())
	raw := manifest.CanonicalBytes()
	raw = append(raw, []byte(" trailing")...)
	if _, err := ManifestFromCanonical(manifest.ID(), manifest.Digest(), raw); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("tampered restore error = %v", err)
	}
}
