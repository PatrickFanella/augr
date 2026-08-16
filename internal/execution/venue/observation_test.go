package venue

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestVenueObservationPreservesExactJSONObjectBytesHashAndIdentity(t *testing.T) {
	input := validObservationInput()
	input.RawPayload = json.RawMessage(" {\n  \"price\": \"0.4200\", \"items\": [1, 2]\n} ")
	observation, err := NewObservation(input)
	if err != nil {
		t.Fatalf("NewObservation() error = %v", err)
	}
	wantID := economicid.DeterministicUUID(
		"venue-observation", input.AccountID.String(), string(input.Provider), input.SourceNamespace, input.SourceEventID,
	)
	if observation.ID != wantID {
		t.Fatalf("ID = %s, want %s", observation.ID, wantID)
	}
	if !bytes.Equal(observation.RawPayload, input.RawPayload) || len(observation.PayloadSHA256) != 64 {
		t.Fatalf("raw payload/hash = %q/%q", observation.RawPayload, observation.PayloadSHA256)
	}
	var parsed map[string]any
	if err := json.Unmarshal(observation.Payload, &parsed); err != nil || parsed["price"] != "0.4200" {
		t.Fatalf("parsed payload = %s, error = %v", observation.Payload, err)
	}
	if err := observation.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	observation.RawPayload[0] = '['
	if bytes.Equal(observation.RawPayload, input.RawPayload) {
		t.Fatal("observation unexpectedly aliases caller raw bytes")
	}
}

func TestVenueObservationIdentityExcludesRevisionTimesAndBytesButReplayDoesNot(t *testing.T) {
	input := validObservationInput()
	first, err := NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != retry.ID || !SameObservationPayload(first, retry) {
		t.Fatal("exact retry did not converge")
	}

	mutations := map[string]func(*ObservationInput){
		"revision":       func(value *ObservationInput) { value.SourceRevision = "revision-2" },
		"source time":    func(value *ObservationInput) { value.SourceAt = value.SourceAt.Add(time.Microsecond) },
		"receive time":   func(value *ObservationInput) { value.ReceivedAt = value.ReceivedAt.Add(time.Microsecond) },
		"raw bytes":      func(value *ObservationInput) { value.RawPayload = json.RawMessage(`{"id":"fill-1","price":"0.43"}`) },
		"mapped outcome": func(value *ObservationInput) { value.MappedOutcome = OutcomeContradiction },
		"provider price": func(value *ObservationInput) { value.ProviderPrice = observationDecimalPointer("0.43") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changedInput := input
			mutate(&changedInput)
			changed, err := NewObservation(changedInput)
			if err != nil {
				t.Fatal(err)
			}
			if changed.ID != first.ID {
				t.Fatalf("changed replay identity = %s, want %s", changed.ID, first.ID)
			}
			if SameObservationPayload(first, changed) {
				t.Fatal("changed retry classified as exact replay")
			}
		})
	}

	distinctInput := input
	distinctInput.SourceNamespace = "kalshi/portfolio/order-snapshots"
	distinct, err := NewObservation(distinctInput)
	if err != nil {
		t.Fatal(err)
	}
	if distinct.ID == first.ID {
		t.Fatal("distinct namespace reused observation identity")
	}
}

func TestVenueObservationNormalizesTimesAndLabelsIdentitySource(t *testing.T) {
	input := validObservationInput()
	input.SourceAt = input.SourceAt.In(time.FixedZone("offset", 2*60*60)).Add(999 * time.Nanosecond)
	input.ReceivedAt = input.ReceivedAt.In(time.FixedZone("offset", 2*60*60)).Add(999 * time.Nanosecond)
	input.CreatedAt = input.CreatedAt.In(time.FixedZone("offset", 2*60*60)).Add(999 * time.Nanosecond)
	observation, err := NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]time.Time{
		"source": observation.SourceAt, "receive": observation.ReceivedAt, "created": observation.CreatedAt,
	} {
		if value.Location() != time.UTC || value.Nanosecond()%1_000 != 0 {
			t.Fatalf("%s time = %s", name, value)
		}
	}
	if observation.IdentityKind != SourceIdentityProvider {
		t.Fatalf("IdentityKind = %q", observation.IdentityKind)
	}

	local := validObservationInput()
	local.IdentityKind = SourceIdentityLocalResponse
	local.SourceNamespace = "alpaca/local-responses/order-create"
	local.SourceEventID = "local-response/orders/" + local.OrderID.String() + "/sha256/abc123"
	local.Kind = ObservationMalformedResponse
	local.MappedOutcome = OutcomeMalformedObservation
	if _, err := NewObservation(local); err != nil {
		t.Fatalf("explicit local response identity rejected: %v", err)
	}
	local.SourceEventID = "provider-event-mislabeled"
	if _, err := NewObservation(local); err == nil {
		t.Fatal("unlabelled local response identity unexpectedly accepted")
	}
}

func TestVenueObservationRejectsInvalidShapeAndVocabulary(t *testing.T) {
	mutations := map[string]func(*ObservationInput){
		"account":                func(value *ObservationInput) { value.AccountID = uuid.Nil },
		"intent":                 func(value *ObservationInput) { value.IntentID = uuid.Nil },
		"order":                  func(value *ObservationInput) { value.OrderID = uuid.Nil },
		"venue contract":         func(value *ObservationInput) { value.VenueContractID = uuid.Nil },
		"unknown provider":       func(value *ObservationInput) { value.Provider = Provider("later") },
		"wrong venue":            func(value *ObservationInput) { value.Venue = "other" },
		"policy":                 func(value *ObservationInput) { value.PolicyVersion = "venue-adapter-policy-v1@latest" },
		"kind":                   func(value *ObservationInput) { value.Kind = ObservationKind("later") },
		"outcome":                func(value *ObservationInput) { value.MappedOutcome = MappedOutcome("later") },
		"client order":           func(value *ObservationInput) { value.ClientOrderID = "" },
		"source kind":            func(value *ObservationInput) { value.IdentityKind = SourceIdentityKind("later") },
		"namespace":              func(value *ObservationInput) { value.SourceNamespace = "" },
		"event":                  func(value *ObservationInput) { value.SourceEventID = "" },
		"source time":            func(value *ObservationInput) { value.SourceAt = time.Time{} },
		"received before source": func(value *ObservationInput) { value.ReceivedAt = value.SourceAt.Add(-time.Microsecond) },
		"missing raw":            func(value *ObservationInput) { value.RawPayload = nil },
		"invalid json":           func(value *ObservationInput) { value.RawPayload = json.RawMessage(`{"broken"`) },
		"array json":             func(value *ObservationInput) { value.RawPayload = json.RawMessage(`[1,2]`) },
		"duplicate top-level key": func(value *ObservationInput) {
			value.RawPayload = json.RawMessage(`{"id":"one","id":"two"}`)
		},
		"duplicate nested key": func(value *ObservationInput) {
			value.RawPayload = json.RawMessage(`{"order":{"id":"one","id":"two"}}`)
		},
		"kalshi outcome":   func(value *ObservationInput) { value.CanonicalOutcome = "maybe" },
		"kalshi book side": func(value *ObservationInput) { value.ProviderBookSide = "yes" },
		"kalshi action":    func(value *ObservationInput) { value.ProviderAction = "hold" },
		"negative provider price": func(value *ObservationInput) {
			value.ProviderPrice = observationDecimalPointer("-0.01")
		},
		"inexact provider price": func(value *ObservationInput) {
			value.ProviderPrice = observationDecimalPointer("0.1234567890123")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			input := validObservationInput()
			mutate(&input)
			if _, err := NewObservation(input); err == nil {
				t.Fatal("NewObservation() unexpectedly succeeded")
			}
		})
	}
}

func TestVenueObservationValidateDetectsTamperingAndClonesOptionalBinding(t *testing.T) {
	input := validObservationInput()
	bindingID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	input.BindingID = &bindingID
	providerPrice := decimal.RequireFromString("0.42")
	input.ProviderPrice = &providerPrice
	observation, err := NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	bindingID = uuid.Nil
	providerPrice = decimal.RequireFromString("0.99")
	if observation.BindingID == nil || *observation.BindingID == uuid.Nil {
		t.Fatal("observation aliases caller binding ID")
	}
	if observation.ProviderPrice == nil || !observation.ProviderPrice.Equal(decimal.RequireFromString("0.42")) {
		t.Fatal("observation aliases caller provider price")
	}

	tests := map[string]func(*Observation){
		"id":                  func(value *Observation) { value.ID = uuid.New() },
		"hash":                func(value *Observation) { value.PayloadSHA256 = stringsOf("0", 64) },
		"payload":             func(value *Observation) { value.Payload = json.RawMessage(`{"id":"changed"}`) },
		"raw":                 func(value *Observation) { value.RawPayload = json.RawMessage(`{"id":"changed"}`) },
		"revision whitespace": func(value *Observation) { value.SourceRevision = " revision-1 " },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := cloneObservation(*observation)
			mutate(&changed)
			if err := changed.Validate(); err == nil {
				t.Fatal("Validate() unexpectedly accepted tampering")
			}
		})
	}
}

func validObservationInput() ObservationInput {
	return ObservationInput{
		AccountID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		IntentID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		OrderID:            uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		VenueContractID:    uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Provider:           ProviderKalshi,
		Venue:              "kalshi",
		PolicyVersion:      PolicySchemaV1 + "@sha256:" + stringsOf("a", 64),
		Kind:               ObservationFill,
		ProviderState:      "fill",
		MappedOutcome:      OutcomeFill,
		ExternalOrderID:    "kalshi-order-1",
		ClientOrderID:      "33333333-3333-3333-3333-333333333333",
		ProviderContractID: "KX-EXAMPLE",
		CanonicalOutcome:   "no",
		ProviderBookSide:   "ask",
		ProviderAction:     "buy",
		ProviderPrice:      observationDecimalPointer("0.42"),
		IdentityKind:       SourceIdentityProvider,
		SourceNamespace:    "kalshi/portfolio/fills",
		SourceEventID:      "fill-1",
		SourceRevision:     "revision-1",
		SourceAt:           time.Date(2026, time.August, 15, 15, 0, 1, 0, time.UTC),
		ReceivedAt:         time.Date(2026, time.August, 15, 15, 0, 2, 0, time.UTC),
		RawPayload:         json.RawMessage(`{"id":"fill-1","price":"0.42"}`),
		CreatedAt:          time.Date(2026, time.August, 15, 15, 0, 3, 0, time.UTC),
	}
}

func observationDecimalPointer(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}

func stringsOf(value string, count int) string {
	var result []byte
	for range count {
		result = append(result, value...)
	}
	return string(result)
}
