package lifecycle

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

func TestImmediatePartialFillEstablishesBindingAtomically(t *testing.T) {
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	fillInput := validFillInput(t, routed, fixture, routeInput, "fill-1", "3", "100", "sim-order-1")
	transition, err := RecordFill(routed, fillInput)
	if err != nil {
		t.Fatalf("RecordFill() error = %v", err)
	}
	if transition.Event.Kind != EventFillAcknowledged || transition.Binding == nil || transition.Fill == nil || transition.Normalization == nil {
		t.Fatalf("immediate fill transition = %#v", transition)
	}
	partial, err := ApplyTransition(routed, transition)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	if partial.State != StatePartiallyFilled || partial.Binding == nil || len(partial.Fills) != 1 || !partial.RecoveryEligible() {
		t.Fatalf("partial aggregate state=%s binding=%v fills=%d recovery=%v", partial.State, partial.Binding, len(partial.Fills), partial.RecoveryEligible())
	}
}

func TestImmediateCompleteFillEstablishesBindingAtomically(t *testing.T) {
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	fillInput := validFillInput(t, routed, fixture, routeInput, "fill-1", "8", "100", "sim-order-1")
	transition, err := RecordFill(routed, fillInput)
	if err != nil {
		t.Fatalf("RecordFill() error = %v", err)
	}
	if transition.Event.Kind != EventFillAcknowledged || transition.Event.NextState != StateFilled {
		t.Fatalf("immediate complete event kind=%s next=%s", transition.Event.Kind, transition.Event.NextState)
	}
	filled, err := ApplyTransition(routed, transition)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	if filled.State != StateFilled || filled.RecoveryEligible() {
		t.Fatalf("filled aggregate state=%s recovery=%v", filled.State, filled.RecoveryEligible())
	}
}

func TestFillGraphAppliesMatchingNormalizationAtomically(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	input := validFillInput(t, working, fixture, routeInput, "fill-1", "3", "100", "")
	transition, err := RecordFill(working, input)
	if err != nil {
		t.Fatalf("RecordFill() error = %v", err)
	}
	if transition.Fill.EconomicSourceEventID != transition.Normalization.SourceEvent.ID ||
		transition.Fill.NormalizationID != transition.Normalization.ID ||
		transition.Fill.LedgerTransactionID != transition.Normalization.Transaction.ID {
		t.Fatalf("fill economic links do not match normalization: %#v", transition.Fill)
	}
	if transition.Normalization.ReferenceType != "execution_fill" || transition.Normalization.ReferenceID != transition.Fill.ID.String() {
		t.Fatalf("normalization reference = %s/%s, want execution_fill/%s", transition.Normalization.ReferenceType, transition.Normalization.ReferenceID, transition.Fill.ID)
	}
}

func TestFillAllowsPresentExactZeroPrice(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	input := validFillInput(t, working, fixture, routeInput, "fill-zero", "1", "0", "")
	transition, err := RecordFill(working, input)
	if err != nil {
		t.Fatalf("RecordFill() zero-price error = %v", err)
	}
	if !transition.Fill.Price.IsZero() || transition.Normalization.Price == nil || !transition.Normalization.Price.IsZero() {
		t.Fatalf("zero fill price was not retained exactly: fill=%s normalization=%v", transition.Fill.Price, transition.Normalization.Price)
	}
}

func TestFillRejectsMissingPrice(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	input := validFillInput(t, working, fixture, routeInput, "fill-missing", "1", "100", "")
	input.Normalization.Price = nil
	if _, err := RecordFill(working, input); err == nil {
		t.Fatal("RecordFill() accepted missing normalization price")
	}
}

func TestFillRequiresMatchingAccountInstrumentVenueAndContract(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	input := validFillInput(t, working, fixture, routeInput, "fill-mismatch", "1", "100", "")
	input.Normalization.Venue = "other"
	if _, err := RecordFill(working, input); err == nil {
		t.Fatal("RecordFill() accepted mismatched venue")
	}
}

func TestFillRequiresDeterministicExecutionFillReference(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	input := validFillInput(t, working, fixture, routeInput, "fill-ref", "1", "100", "")
	input.Normalization.ReferenceID = "wrong-fill-id"
	if _, err := RecordFill(working, input); err == nil {
		t.Fatal("RecordFill() accepted mismatched execution-fill reference")
	}
}

func TestMultiplePartialFillsReachExactOrderQuantity(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	firstInput := validFillInput(t, working, fixture, routeInput, "fill-1", "3", "100", "")
	first, err := RecordFill(working, firstInput)
	if err != nil {
		t.Fatalf("RecordFill(first) error = %v", err)
	}
	partial, err := ApplyTransition(working, first)
	if err != nil {
		t.Fatalf("ApplyTransition(first) error = %v", err)
	}
	secondInput := validFillInput(t, partial, fixture, routeInput, "fill-2", "2", "100.01", "")
	second, err := RecordFill(partial, secondInput)
	if err != nil {
		t.Fatalf("RecordFill(second) error = %v", err)
	}
	stillPartial, err := ApplyTransition(partial, second)
	if err != nil {
		t.Fatalf("ApplyTransition(second) error = %v", err)
	}
	thirdInput := validFillInput(t, stillPartial, fixture, routeInput, "fill-3", "3", "99.99", "")
	third, err := RecordFill(stillPartial, thirdInput)
	if err != nil {
		t.Fatalf("RecordFill(third) error = %v", err)
	}
	filled, err := ApplyTransition(stillPartial, third)
	if err != nil {
		t.Fatalf("ApplyTransition(third) error = %v", err)
	}
	if filled.State != StateFilled || len(filled.Fills) != 3 || !sumFillQuantity(filled.Fills).Equal(decimal.NewFromInt(8)) {
		t.Fatalf("filled state=%s fills=%d quantity=%s", filled.State, len(filled.Fills), sumFillQuantity(filled.Fills))
	}
}

func TestFillRejectsOverfill(t *testing.T) {
	working, fixture, routeInput := workingAggregate(t)
	input := validFillInput(t, working, fixture, routeInput, "fill-over", "9", "100", "")
	if _, err := RecordFill(working, input); err == nil {
		t.Fatal("RecordFill() accepted overfill")
	}
}

func TestCorrectionAfterFilledAppendsOneTerminalFailure(t *testing.T) {
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	fillInput := validFillInput(t, routed, fixture, routeInput, "fill-1", "8", "100", "sim-order-1")
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
	correctionInput.Evidence = json.RawMessage(`{"execution_id":"fill-1","revision":"2","status":"corrected"}`)
	correction, err := FailReconciliation(filled, EventFillCorrectionObserved, correctionInput, correctionInput.ReceivedAt)
	if err != nil {
		t.Fatalf("FailReconciliation() error = %v", err)
	}
	if correction.Event.ID == fillTransition.Event.ID {
		t.Fatal("correction identity collided with ordinary fill event")
	}
	retry, err := FailReconciliation(filled, EventFillCorrectionObserved, correctionInput, correctionInput.ReceivedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("FailReconciliation() retry error = %v", err)
	}
	if retry.Event.ID != correction.Event.ID || !SameEventPayload(&retry.Event, &correction.Event) {
		t.Fatal("identical correction retry did not converge")
	}
	correctionInput.Evidence = json.RawMessage(`{"execution_id":"fill-1","revision":"2","status":"busted"}`)
	changed, err := FailReconciliation(filled, EventFillCorrectionObserved, correctionInput, correctionInput.ReceivedAt.Add(2*time.Second))
	if err != nil {
		t.Fatalf("FailReconciliation() changed correction error = %v", err)
	}
	if changed.Event.ID != correction.Event.ID || SameEventPayload(&changed.Event, &correction.Event) {
		t.Fatal("changed same-discriminator correction was accepted as identical")
	}
	failed, err := ApplyTransition(filled, correction)
	if err != nil {
		t.Fatalf("ApplyTransition(correction) error = %v", err)
	}
	if failed.State != StateFailedReconciliation || len(failed.Fills) != 1 {
		t.Fatalf("failed state=%s fills=%d", failed.State, len(failed.Fills))
	}
	if _, err := ApplyTransition(failed, correction); err == nil {
		t.Fatal("ApplyTransition() appended after failed reconciliation")
	}
}

func TestCorrectionAndBustRejectForgedCumulativeFillQuantity(t *testing.T) {
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	fillInput := validFillInput(t, routed, fixture, routeInput, "fill-revision", "8", "100", "sim-order-revision")
	fillTransition, err := RecordFill(routed, fillInput)
	if err != nil {
		t.Fatalf("RecordFill() error = %v", err)
	}
	filled, err := ApplyTransition(routed, fillTransition)
	if err != nil {
		t.Fatalf("ApplyTransition(fill) error = %v", err)
	}

	for _, testCase := range []struct {
		name             string
		kind             EventKind
		observationClass ObservationClass
	}{
		{name: "correction", kind: EventFillCorrectionObserved, observationClass: ObservationCorrection},
		{name: "bust", kind: EventFillBustObserved, observationClass: ObservationBust},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			eventInput := nextEventInput(filled, testCase.name+"-observation")
			eventInput.Source = fillTransition.Fill.Source
			eventInput.SourceNamespace = fillTransition.Fill.SourceNamespace
			eventInput.ObservationClass = testCase.observationClass
			eventInput.ObservationDiscriminator = "observation:" + testCase.name
			eventInput.OriginalFillID = &fillTransition.Fill.ID
			eventInput.OriginalSourceEventID = fillTransition.Fill.SourceEventID
			eventInput.Actor = "simulation-reconciler"
			eventInput.ReasonCode = "fill_" + testCase.name
			eventInput.Evidence = json.RawMessage(`{"status":"revised"}`)
			transition, err := FailReconciliation(filled, testCase.kind, eventInput, eventInput.ReceivedAt)
			if err != nil {
				t.Fatalf("FailReconciliation() error = %v", err)
			}
			forgedCumulative := decimal.NewFromInt(8)
			transition.Event.CumulativeFillQuantity = &forgedCumulative
			if _, err := ApplyTransition(filled, transition); err == nil {
				t.Fatal("ApplyTransition() accepted cumulative fill quantity on revision event")
			}
		})
	}
}

func TestCorrectionRevisionDiscriminatorMustMatchRevision(t *testing.T) {
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	fillInput := validFillInput(t, routed, fixture, routeInput, "fill-1", "8", "100", "sim-order-1")
	fillTransition, err := RecordFill(routed, fillInput)
	if err != nil {
		t.Fatalf("RecordFill() error = %v", err)
	}
	filled, err := ApplyTransition(routed, fillTransition)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	eventInput := nextEventInput(filled, "fill-1")
	eventInput.Source = fillTransition.Fill.Source
	eventInput.SourceNamespace = fillTransition.Fill.SourceNamespace
	eventInput.SourceRevision = "2"
	eventInput.ObservationClass = ObservationCorrection
	eventInput.ObservationDiscriminator = "revision:3"
	eventInput.OriginalFillID = &fillTransition.Fill.ID
	eventInput.OriginalSourceEventID = fillTransition.Fill.SourceEventID
	eventInput.Actor = "simulation-venue"
	eventInput.ReasonCode = "fill_corrected"
	eventInput.Evidence = json.RawMessage(`{"revision":"2"}`)
	if _, err := FailReconciliation(filled, EventFillCorrectionObserved, eventInput, eventInput.ReceivedAt); err == nil {
		t.Fatal("FailReconciliation() accepted a revision discriminator that does not match source revision")
	}
}

func validFillInput(
	t *testing.T,
	aggregate *Aggregate,
	fixture ProposeInput,
	routeInput RouteInput,
	sourceEventID, quantity, price, externalOrderID string,
) FillInput {
	t.Helper()
	receivedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	raw := json.RawMessage(`{"execution_id":"` + sourceEventID + `","quantity":"` + quantity + `","price":"` + price + `"}`)
	sourceEvent, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID:       fixture.Account.ID,
		Source:          "simulation",
		SourceNamespace: "simulation-policy-v1",
		SourceEventID:   sourceEventID,
		ObservedAt:      receivedAt,
		RawPayload:      raw,
		CreatedAt:       receivedAt,
	})
	if err != nil {
		t.Fatalf("NewEconomicSourceEvent() error = %v", err)
	}
	fillID := FillID(aggregate.Order.ID, sourceEvent.ID)
	side := ledger.FillSideBuy
	if aggregate.Order.Side == SideSell {
		side = ledger.FillSideSell
	}
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent:         sourceEvent,
			Account:             &fixture.Account,
			NormalizerVersion:   "execution-lifecycle-v1",
			ExecutionOriginType: fixture.OriginType,
			ExecutionOriginID:   fixture.OriginID,
			ReferenceType:       "execution_fill",
			ReferenceID:         fillID.String(),
			EffectiveAt:         receivedAt.Add(-time.Millisecond),
		},
		Instrument:    fixture.Instrument,
		VenueContract: routeInput.VenueContract,
		Side:          side,
		Quantity:      decimal.RequireFromString(quantity),
		Price:         decimal.RequireFromString(price),
	})
	if err != nil {
		t.Fatalf("NewFillEconomicNormalization() error = %v", err)
	}
	eventInput := EventInput{
		Source:          sourceEvent.Source,
		SourceNamespace: sourceEvent.SourceNamespace,
		SourceEventID:   sourceEvent.SourceEventID,
		SourceRevision:  sourceEvent.SourceRevision,
		SourceAt:        receivedAt.Add(-time.Millisecond),
		ReceivedAt:      receivedAt,
		Actor:           "simulation-venue",
		ReasonCode:      "fill_reported",
		Evidence:        raw,
	}
	return FillInput{
		Normalization:   normalization,
		ExternalOrderID: externalOrderID,
		Event:           eventInput,
		CreatedAt:       receivedAt,
	}
}

func workingAggregate(t *testing.T) (*Aggregate, ProposeInput, RouteInput) {
	t.Helper()
	routed, fixture, routeInput := routedAggregateWithRoute(t)
	eventInput := nextEventInput(routed, "ack-1")
	eventInput.Source = "simulation"
	eventInput.SourceNamespace = "simulation-policy-v1"
	eventInput.Actor = "simulation-venue"
	eventInput.ReasonCode = "order_acknowledged"
	eventInput.Evidence = json.RawMessage(`{"external_order_id":"sim-order-1"}`)
	ack, err := Acknowledge(routed, "sim-order-1", eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	working, err := ApplyTransition(routed, ack)
	if err != nil {
		t.Fatalf("ApplyTransition(ack) error = %v", err)
	}
	return working, fixture, routeInput
}
