package ledger

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEconomicSourceEventIdentityExcludesRevision(t *testing.T) {
	base := validEconomicSourceEventInput()
	first, err := NewEconomicSourceEvent(base)
	if err != nil {
		t.Fatalf("NewEconomicSourceEvent() error = %v", err)
	}

	revisedInput := base
	revisedInput.SourceRevision = "revision-2"
	revised, err := NewEconomicSourceEvent(revisedInput)
	if err != nil {
		t.Fatalf("NewEconomicSourceEvent(revision) error = %v", err)
	}
	if first.ID != revised.ID {
		t.Fatalf("revision changed durable identity: %s != %s", first.ID, revised.ID)
	}
	if SameEconomicSourceEventPayload(first, revised) {
		t.Fatal("revision-changing retry was classified as an identical replay")
	}
}

func TestEconomicSourceEventPreservesWireJSONAndHash(t *testing.T) {
	input := validEconomicSourceEventInput()
	input.RawPayload = json.RawMessage(" {\n  \"price\": 1.2300, \"items\": [1, 2]\n} ")
	event, err := NewEconomicSourceEvent(input)
	if err != nil {
		t.Fatalf("NewEconomicSourceEvent() error = %v", err)
	}
	if !bytes.Equal(event.RawPayload, input.RawPayload) {
		t.Fatalf("raw payload changed:\n got %q\nwant %q", event.RawPayload, input.RawPayload)
	}
	if len(event.PayloadSHA256) != 64 {
		t.Fatalf("payload hash length = %d, want 64", len(event.PayloadSHA256))
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tampered := *event
	tampered.RawPayload = json.RawMessage(`{"price":1.24}`)
	if err := tampered.Validate(); err == nil {
		t.Fatal("Validate() accepted raw bytes that do not match the stored hash")
	}
}

func TestEconomicSourceEventAcceptsAnyValidJSONType(t *testing.T) {
	for name, payload := range map[string]json.RawMessage{
		"object":  json.RawMessage(`{"fill":"f-1"}`),
		"array":   json.RawMessage(`[1,2,3]`),
		"string":  json.RawMessage(`"provider-event"`),
		"number":  json.RawMessage(`1.2300`),
		"boolean": json.RawMessage(`true`),
		"null":    json.RawMessage(`null`),
	} {
		t.Run(name, func(t *testing.T) {
			input := validEconomicSourceEventInput()
			input.RawPayload = payload
			if _, err := NewEconomicSourceEvent(input); err != nil {
				t.Fatalf("NewEconomicSourceEvent() error = %v", err)
			}
		})
	}
}

func TestEconomicSourceEventRejectsInvalidOrMissingEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*EconomicSourceEventInput){
		"account":          func(input *EconomicSourceEventInput) { input.AccountID = uuid.Nil },
		"source":           func(input *EconomicSourceEventInput) { input.Source = " " },
		"namespace":        func(input *EconomicSourceEventInput) { input.SourceNamespace = "" },
		"source event":     func(input *EconomicSourceEventInput) { input.SourceEventID = "" },
		"observed time":    func(input *EconomicSourceEventInput) { input.ObservedAt = time.Time{} },
		"raw payload":      func(input *EconomicSourceEventInput) { input.RawPayload = nil },
		"invalid raw JSON": func(input *EconomicSourceEventInput) { input.RawPayload = json.RawMessage(`{"broken"`) },
	} {
		t.Run(name, func(t *testing.T) {
			input := validEconomicSourceEventInput()
			mutate(&input)
			if _, err := NewEconomicSourceEvent(input); err == nil {
				t.Fatal("NewEconomicSourceEvent() unexpectedly succeeded")
			}
		})
	}
}

func TestEconomicSourceEventNormalizesIdentityAndTimestamps(t *testing.T) {
	input := validEconomicSourceEventInput()
	input.Source = " Provider-A "
	input.SourceNamespace = " fills/us "
	input.SourceEventID = " event-1 "
	input.SourceRevision = " rev-a "
	input.ObservedAt = input.ObservedAt.Add(999 * time.Nanosecond)
	event, err := NewEconomicSourceEvent(input)
	if err != nil {
		t.Fatalf("NewEconomicSourceEvent() error = %v", err)
	}
	if event.Source != "provider-a" || event.SourceNamespace != "fills/us" ||
		event.SourceEventID != "event-1" || event.SourceRevision != "rev-a" {
		t.Fatalf("unexpected normalized identity: %+v", event)
	}
	if event.ObservedAt.Nanosecond()%1_000 != 0 || event.CreatedAt.Nanosecond()%1_000 != 0 {
		t.Fatalf("timestamps are not at PostgreSQL precision: %s/%s", event.ObservedAt, event.CreatedAt)
	}
}

func TestEconomicSourceEventNamespaceChangesIdentityAndRetryMatchesExactly(t *testing.T) {
	input := validEconomicSourceEventInput()
	first, err := NewEconomicSourceEvent(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewEconomicSourceEvent(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != retry.ID || !SameEconomicSourceEventPayload(first, retry) {
		t.Fatalf("identical retry did not converge: %#v %#v", first, retry)
	}

	distinctInput := input
	distinctInput.SourceNamespace = "settlements/us"
	distinct, err := NewEconomicSourceEvent(distinctInput)
	if err != nil {
		t.Fatal(err)
	}
	if distinct.ID == first.ID {
		t.Fatal("distinct source namespace reused source-event UUID")
	}
}

func validEconomicSourceEventInput() EconomicSourceEventInput {
	return EconomicSourceEventInput{
		AccountID:       uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Source:          "provider-a",
		SourceNamespace: "fills/us",
		SourceEventID:   "fill-123",
		SourceRevision:  "revision-1",
		ObservedAt:      time.Date(2026, time.August, 15, 15, 0, 1, 0, time.UTC),
		RawPayload:      json.RawMessage(`{"id":"fill-123","price":"12.34"}`),
		CreatedAt:       time.Date(2026, time.August, 15, 15, 0, 2, 0, time.UTC),
	}
}
