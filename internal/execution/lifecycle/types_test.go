package lifecycle

import (
	"bytes"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestOrdinaryLifecycleEventIdentityExcludesSourceRevision(t *testing.T) {
	aggregate, err := Propose(validProposeInput(t))
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	eventInput := nextEventInput(aggregate, "allocation-1")
	eventInput.SourceRevision = "revision-1"
	first, err := Allocate(aggregate, decimal.NewFromInt(8), eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	eventInput.SourceRevision = "revision-2"
	second, err := Allocate(aggregate, decimal.NewFromInt(8), eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() changed revision error = %v", err)
	}
	if first.Event.ID != second.Event.ID {
		t.Fatalf("ordinary event IDs = %s and %s, want equal", first.Event.ID, second.Event.ID)
	}
	if SameEventPayload(&first.Event, &second.Event) {
		t.Fatal("changed ordinary source revision was accepted as identical replay")
	}
}

func TestCorrectionLifecycleEventIdentityUsesOriginalSourceEvent(t *testing.T) {
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	fillInput := validFillInput(t, routed, fixture, routeInput, "fill-identity", "8", "100", "sim-order-identity")
	fillTransition, err := RecordFill(routed, fillInput)
	if err != nil {
		t.Fatalf("RecordFill() error = %v", err)
	}
	filled, err := ApplyTransition(routed, fillTransition)
	if err != nil {
		t.Fatalf("ApplyTransition(fill) error = %v", err)
	}

	correctionInput := nextEventInput(filled, "correction-observation-1")
	correctionInput.Source = fillTransition.Fill.Source
	correctionInput.SourceNamespace = fillTransition.Fill.SourceNamespace
	correctionInput.SourceRevision = "2"
	correctionInput.ObservationClass = ObservationCorrection
	correctionInput.ObservationDiscriminator = "revision:2"
	correctionInput.OriginalFillID = &fillTransition.Fill.ID
	correctionInput.OriginalSourceEventID = fillTransition.Fill.SourceEventID
	correctionInput.Actor = "simulation-venue"
	correctionInput.ReasonCode = "fill_corrected"
	correctionInput.Evidence = []byte(`{"revision":"2","status":"corrected"}`)
	first, err := FailReconciliation(filled, EventFillCorrectionObserved, correctionInput, correctionInput.ReceivedAt)
	if err != nil {
		t.Fatalf("FailReconciliation(first) error = %v", err)
	}

	correctionInput.SourceEventID = "correction-observation-2"
	second, err := FailReconciliation(filled, EventFillCorrectionObserved, correctionInput, correctionInput.ReceivedAt)
	if err != nil {
		t.Fatalf("FailReconciliation(second) error = %v", err)
	}
	if first.Event.ID != second.Event.ID {
		t.Fatalf("correction event IDs = %s and %s, want equal original-fill identity", first.Event.ID, second.Event.ID)
	}
	if SameEventPayload(&first.Event, &second.Event) {
		t.Fatal("different correction observation IDs were accepted as an exact replay")
	}
}

func TestLifecycleEventReplayRequiresExactEvidenceBytes(t *testing.T) {
	aggregate, err := Propose(validProposeInput(t))
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	eventInput := nextEventInput(aggregate, "allocation-1")
	eventInput.Evidence = []byte(`{"allocation":8,"policy":"v1"}`)
	first, err := Allocate(aggregate, decimal.NewFromInt(8), eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	eventInput.Evidence = []byte(`{"policy":"v1","allocation":8}`)
	second, err := Allocate(aggregate, decimal.NewFromInt(8), eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() reordered evidence error = %v", err)
	}
	if first.Event.ID != second.Event.ID {
		t.Fatalf("same source identity produced IDs %s and %s", first.Event.ID, second.Event.ID)
	}
	if bytes.Equal(first.Event.Evidence, second.Event.Evidence) {
		t.Fatal("fixture did not change exact evidence bytes")
	}
	if SameEventPayload(&first.Event, &second.Event) {
		t.Fatal("semantically equal but byte-distinct evidence was accepted as exact replay")
	}
}

func nextEventInput(aggregate *Aggregate, sourceEventID string) EventInput {
	receivedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	return EventInput{
		Source:          "decision",
		SourceNamespace: "allocator-v1",
		SourceEventID:   sourceEventID,
		SourceAt:        receivedAt.Add(-time.Millisecond),
		ReceivedAt:      receivedAt,
		Actor:           "allocator",
		ReasonCode:      "allocated",
		Evidence:        []byte(`{"allocation":8}`),
	}
}
