package instrument

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewOptionContractTermsCreatesCanonicalCallAndPut(t *testing.T) {
	for _, contractType := range []OptionContractType{OptionContractCall, OptionContractPut} {
		t.Run(string(contractType), func(t *testing.T) {
			input := validOptionContractTermsInput()
			input.ContractType = contractType
			terms, err := NewOptionContractTerms(input)
			if err != nil {
				t.Fatalf("NewOptionContractTerms() error = %v", err)
			}
			if terms.ID == uuid.Nil || terms.ContractType != contractType ||
				terms.StrikePrice.String() != "125.5" || terms.DeliverableQuantity.String() != "100" {
				t.Fatalf("unexpected terms: %+v", terms)
			}
			if err := terms.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestOptionContractTermsIdentityExcludesRevisionButReplayDoesNot(t *testing.T) {
	input := validOptionContractTermsInput()
	first, err := NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != retry.ID || !SameOptionContractTermsPayload(first, retry) {
		t.Fatalf("identical retry did not converge: %#v %#v", first, retry)
	}

	revisedInput := input
	revisedInput.SourceRevision = "revision-2"
	revised, err := NewOptionContractTerms(revisedInput)
	if err != nil {
		t.Fatal(err)
	}
	if revised.ID != first.ID {
		t.Fatalf("revision changed terms identity: %s != %s", revised.ID, first.ID)
	}
	if SameOptionContractTermsPayload(first, revised) {
		t.Fatal("revision-changing retry was treated as identical")
	}
}

func TestOptionContractTermsPreservesWireEvidence(t *testing.T) {
	input := validOptionContractTermsInput()
	input.RawPayload = json.RawMessage(" {\n \"strike\": \"125.500\"\n} ")
	terms, err := NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(terms.RawPayload, input.RawPayload) || len(terms.PayloadSHA256) != 64 {
		t.Fatalf("wire evidence changed: payload=%q hash=%q", terms.RawPayload, terms.PayloadSHA256)
	}

	tampered := *terms
	tampered.RawPayload = json.RawMessage(`{"strike":"126"}`)
	if err := tampered.Validate(); err == nil {
		t.Fatal("Validate() accepted a mismatched raw-payload hash")
	}
}

func TestOptionContractTermsNormalizesProvenanceAndTimestamps(t *testing.T) {
	input := validOptionContractTermsInput()
	input.StrikeCurrency = " usd "
	input.Source = " OCC-Feed "
	input.SourceNamespace = " option/terms "
	input.SourceRecordID = " record-1 "
	input.SourceRevision = " revision-a "
	input.EffectiveAt = input.EffectiveAt.Add(999 * time.Nanosecond)
	input.ObservedAt = input.ObservedAt.Add(999 * time.Nanosecond)
	terms, err := NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	if terms.StrikeCurrency != "USD" || terms.Source != "occ-feed" ||
		terms.SourceNamespace != "option/terms" || terms.SourceRecordID != "record-1" ||
		terms.SourceRevision != "revision-a" {
		t.Fatalf("unexpected normalized provenance: %+v", terms)
	}
	if terms.EffectiveAt.Nanosecond()%1_000 != 0 || terms.ObservedAt.Nanosecond()%1_000 != 0 {
		t.Fatalf("timestamps are not at PostgreSQL precision: %s/%s", terms.EffectiveAt, terms.ObservedAt)
	}
}

func TestOptionContractTermsRejectsInvalidShapeAndProvenance(t *testing.T) {
	for name, mutate := range map[string]func(*OptionContractTermsInput){
		"option ID":       func(input *OptionContractTermsInput) { input.OptionInstrumentID = uuid.Nil },
		"underlying ID":   func(input *OptionContractTermsInput) { input.UnderlyingInstrumentID = uuid.Nil },
		"self underlying": func(input *OptionContractTermsInput) { input.UnderlyingInstrumentID = input.OptionInstrumentID },
		"contract type":   func(input *OptionContractTermsInput) { input.ContractType = "straddle" },
		"zero strike":     func(input *OptionContractTermsInput) { input.StrikePrice = decimal.Zero },
		"strike precision": func(input *OptionContractTermsInput) {
			input.StrikePrice = decimal.RequireFromString("1.0000000000001")
		},
		"currency":         func(input *OptionContractTermsInput) { input.StrikeCurrency = "USDX" },
		"zero deliverable": func(input *OptionContractTermsInput) { input.DeliverableQuantity = decimal.Zero },
		"source":           func(input *OptionContractTermsInput) { input.Source = "" },
		"namespace":        func(input *OptionContractTermsInput) { input.SourceNamespace = "" },
		"source record":    func(input *OptionContractTermsInput) { input.SourceRecordID = "" },
		"effective time":   func(input *OptionContractTermsInput) { input.EffectiveAt = time.Time{} },
		"observed time":    func(input *OptionContractTermsInput) { input.ObservedAt = time.Time{} },
		"lookahead":        func(input *OptionContractTermsInput) { input.ObservedAt = input.EffectiveAt.Add(-time.Second) },
		"raw payload":      func(input *OptionContractTermsInput) { input.RawPayload = json.RawMessage(`{"broken"`) },
		"metadata":         func(input *OptionContractTermsInput) { input.Metadata = json.RawMessage(`[]`) },
	} {
		t.Run(name, func(t *testing.T) {
			input := validOptionContractTermsInput()
			mutate(&input)
			if _, err := NewOptionContractTerms(input); err == nil {
				t.Fatal("NewOptionContractTerms() unexpectedly succeeded")
			}
		})
	}
}

func validOptionContractTermsInput() OptionContractTermsInput {
	return OptionContractTermsInput{
		OptionInstrumentID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		UnderlyingInstrumentID: uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		ContractType:           OptionContractCall,
		StrikePrice:            decimal.RequireFromString("125.50"),
		StrikeCurrency:         "USD",
		DeliverableQuantity:    decimal.NewFromInt(100),
		Source:                 "occ-feed",
		SourceNamespace:        "option/terms",
		SourceRecordID:         "terms-123",
		SourceRevision:         "revision-1",
		EffectiveAt:            time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		ObservedAt:             time.Date(2026, time.August, 1, 0, 0, 1, 0, time.UTC),
		RawPayload:             json.RawMessage(`{"id":"terms-123","strike":"125.50"}`),
		Metadata:               json.RawMessage(`{"provenance":"provider"}`),
		CreatedAt:              time.Date(2026, time.August, 1, 0, 0, 2, 0, time.UTC),
	}
}
