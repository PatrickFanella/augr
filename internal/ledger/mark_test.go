package ledger

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewMarkObservationNormalizesCanonicalIdentityAndAllowsZero(t *testing.T) {
	t.Parallel()

	instrumentID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	effectiveAt := time.Date(2026, time.August, 15, 14, 0, 0, 123456000, time.FixedZone("test", -5*60*60))
	observedAt := effectiveAt.Add(2 * time.Minute)
	input := MarkObservationInput{
		InstrumentID:        instrumentID,
		Price:               decimal.Zero,
		PriceCurrency:       " usd ",
		Source:              " Polygon ",
		SourceNamespace:     " consolidated/mark ",
		SourceObservationID: " observation-1 ",
		SourceRevision:      " revision-7 ",
		EffectiveAt:         effectiveAt,
		ObservedAt:          observedAt,
		Metadata:            json.RawMessage(`{"sequence":9007199254740993}`),
	}

	mark, err := NewMarkObservation(input)
	if err != nil {
		t.Fatalf("NewMarkObservation() error = %v", err)
	}
	if mark.ID == uuid.Nil {
		t.Fatal("NewMarkObservation() returned a nil ID")
	}
	if mark.InstrumentID != instrumentID || mark.PriceCurrency != "USD" || mark.Source != "polygon" ||
		mark.SourceNamespace != "consolidated/mark" || mark.SourceObservationID != "observation-1" ||
		mark.SourceRevision != "revision-7" {
		t.Fatalf("NewMarkObservation() identity = %+v", mark)
	}
	if !mark.Price.IsZero() {
		t.Fatalf("NewMarkObservation() price = %s, want zero", mark.Price)
	}
	if mark.EffectiveAt.Location() != time.UTC || mark.ObservedAt.Location() != time.UTC ||
		mark.EffectiveAt.Nanosecond()%1_000 != 0 || mark.ObservedAt.Nanosecond()%1_000 != 0 {
		t.Fatalf("NewMarkObservation() times = %s/%s, want UTC microseconds", mark.EffectiveAt, mark.ObservedAt)
	}
	if string(mark.Metadata) != string(input.Metadata) {
		t.Fatalf("NewMarkObservation() metadata = %s, want byte-preserved %s", mark.Metadata, input.Metadata)
	}

	retry, err := NewMarkObservation(input)
	if err != nil {
		t.Fatalf("NewMarkObservation(retry) error = %v", err)
	}
	if retry.ID != mark.ID {
		t.Fatalf("NewMarkObservation(retry) ID = %s, want %s", retry.ID, mark.ID)
	}
	if !SameMarkObservation(mark, retry) {
		t.Fatal("SameMarkObservation() rejected an identical retry")
	}
}

func TestNewMarkObservationIdentityExcludesRevisionButRetryComparisonDoesNot(t *testing.T) {
	t.Parallel()

	input := validMarkObservationInput()
	first, err := NewMarkObservation(input)
	if err != nil {
		t.Fatalf("NewMarkObservation() error = %v", err)
	}
	input.SourceRevision = "corrected"
	changed, err := NewMarkObservation(input)
	if err != nil {
		t.Fatalf("NewMarkObservation(changed revision) error = %v", err)
	}
	if first.ID != changed.ID {
		t.Fatalf("revision changed identity: %s != %s", first.ID, changed.ID)
	}
	if SameMarkObservation(first, changed) {
		t.Fatal("SameMarkObservation() accepted changed revision evidence")
	}

	input = validMarkObservationInput()
	input.Metadata = json.RawMessage(`{"quality":"corrected"}`)
	changed, err = NewMarkObservation(input)
	if err != nil {
		t.Fatalf("NewMarkObservation(changed metadata) error = %v", err)
	}
	if first.ID != changed.ID || SameMarkObservation(first, changed) {
		t.Fatal("metadata retry semantics did not preserve identity and reject changed evidence")
	}
}

func TestMarkObservationValidateRejectsInvalidCanonicalEvidence(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*MarkObservationInput){
		"missing instrument": func(input *MarkObservationInput) { input.InstrumentID = uuid.Nil },
		"negative price":     func(input *MarkObservationInput) { input.Price = decimal.NewFromInt(-1) },
		"over precision": func(input *MarkObservationInput) {
			input.Price = decimal.RequireFromString("1.0000000000001")
		},
		"over magnitude": func(input *MarkObservationInput) {
			input.Price = decimal.RequireFromString("100000000000000000000000000")
		},
		"bad currency":        func(input *MarkObservationInput) { input.PriceCurrency = "US$" },
		"missing source":      func(input *MarkObservationInput) { input.Source = " " },
		"missing namespace":   func(input *MarkObservationInput) { input.SourceNamespace = " " },
		"missing observation": func(input *MarkObservationInput) { input.SourceObservationID = " " },
		"observation before effective": func(input *MarkObservationInput) {
			input.ObservedAt = input.EffectiveAt.Add(-time.Microsecond)
		},
		"invalid metadata": func(input *MarkObservationInput) { input.Metadata = json.RawMessage(`[]`) },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := validMarkObservationInput()
			mutate(&input)
			if _, err := NewMarkObservation(input); err == nil {
				t.Fatal("NewMarkObservation() unexpectedly succeeded")
			}
		})
	}
}

func TestMarkObservationValidateRejectsForgedIDAndNoncanonicalFields(t *testing.T) {
	t.Parallel()

	mark, err := NewMarkObservation(validMarkObservationInput())
	if err != nil {
		t.Fatalf("NewMarkObservation() error = %v", err)
	}
	for name, mutate := range map[string]func(*MarkObservation){
		"forged ID":        func(value *MarkObservation) { value.ID = uuid.New() },
		"uppercase source": func(value *MarkObservation) { value.Source = "POLYGON" },
		"spaced namespace": func(value *MarkObservation) { value.SourceNamespace = " ns " },
		"non UTC time": func(value *MarkObservation) {
			value.EffectiveAt = value.EffectiveAt.In(time.FixedZone("offset", -5*60*60))
		},
		"non microsecond time": func(value *MarkObservation) {
			value.ObservedAt = value.ObservedAt.Add(time.Nanosecond)
		},
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate := *mark
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() unexpectedly succeeded")
			}
		})
	}
}

func validMarkObservationInput() MarkObservationInput {
	effectiveAt := time.Date(2026, time.August, 15, 14, 0, 0, 0, time.UTC)
	return MarkObservationInput{
		InstrumentID:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Price:               decimal.RequireFromString("123.456789012345"),
		PriceCurrency:       "USD",
		Source:              "polygon",
		SourceNamespace:     "consolidated/mark",
		SourceObservationID: "observation-1",
		SourceRevision:      "revision-1",
		EffectiveAt:         effectiveAt,
		ObservedAt:          effectiveAt.Add(time.Second),
		Metadata:            json.RawMessage(`{"quality":"official"}`),
	}
}
