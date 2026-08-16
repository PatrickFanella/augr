package lifecycle

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

func TestAllocationMustKeepDirectionAndNotExceedDesiredQuantity(t *testing.T) {
	for name, quantity := range map[string]decimal.Decimal{
		"zero":               decimal.Zero,
		"opposite direction": decimal.NewFromInt(-1),
		"larger magnitude":   decimal.NewFromInt(11),
		"overscale":          decimal.RequireFromString("1.0000000000001"),
	} {
		t.Run(name, func(t *testing.T) {
			aggregate, err := Propose(validProposeInput(t))
			if err != nil {
				t.Fatalf("Propose() error = %v", err)
			}
			eventInput := nextEventInput(aggregate, "allocation-"+name)
			if _, err := Allocate(aggregate, quantity, eventInput, eventInput.ReceivedAt); err == nil {
				t.Fatalf("Allocate() accepted %s", quantity)
			}
		})
	}
}

func TestRiskApprovalAndOrderCopyExactAllocatedQuantity(t *testing.T) {
	proposed, err := Propose(validProposeInput(t))
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	allocationInput := nextEventInput(proposed, "allocation-1")
	allocation, err := Allocate(proposed, decimal.NewFromInt(8), allocationInput, allocationInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	allocated, err := ApplyTransition(proposed, allocation)
	if err != nil {
		t.Fatalf("ApplyTransition(allocation) error = %v", err)
	}
	riskInput := nextEventInput(allocated, "risk-approval-1")
	riskInput.Source = "risk"
	riskInput.SourceNamespace = "risk-policy-v1"
	riskInput.Actor = "risk-engine"
	riskInput.ReasonCode = "approved"
	riskInput.Evidence = []byte(`{"approved":true}`)
	approval, err := ApproveRisk(allocated, riskInput, riskInput.ReceivedAt)
	if err != nil {
		t.Fatalf("ApproveRisk() error = %v", err)
	}
	if approval.Event.QuantityDelta == nil || !approval.Event.QuantityDelta.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("risk event quantity = %v, want 8", approval.Event.QuantityDelta)
	}
	approved, err := ApplyTransition(allocated, approval)
	if err != nil {
		t.Fatalf("ApplyTransition(approval) error = %v", err)
	}
	if approved.State != StateRiskApproved || approved.AllocatedQuantity == nil || !approved.AllocatedQuantity.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("approved aggregate state=%s allocation=%v", approved.State, approved.AllocatedQuantity)
	}
}

func TestLifecycleRejectsStaleExpectedState(t *testing.T) {
	proposed, err := Propose(validProposeInput(t))
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	firstInput := nextEventInput(proposed, "allocation-1")
	first, err := Allocate(proposed, decimal.NewFromInt(8), firstInput, firstInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	allocated, err := ApplyTransition(proposed, first)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	staleInput := nextEventInput(proposed, "allocation-2")
	stale, err := Allocate(proposed, decimal.NewFromInt(7), staleInput, staleInput.ReceivedAt)
	if err != nil {
		t.Fatalf("stale Allocate() construction error = %v", err)
	}
	if _, err := ApplyTransition(allocated, stale); err == nil {
		t.Fatal("ApplyTransition() accepted stale prior state")
	}
}

func TestCancelRequestRetainsWorkingState(t *testing.T) {
	working, _, _ := workingAggregate(t)
	eventInput := nextEventInput(working, "cancel-request-1")
	eventInput.Source = "operator"
	eventInput.SourceNamespace = "operator-console"
	eventInput.Actor = "operator"
	eventInput.ReasonCode = "cancel_requested"
	eventInput.Evidence = json.RawMessage(`{"requested":true}`)
	request, err := RequestCancel(working, eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("RequestCancel() error = %v", err)
	}
	after, err := ApplyTransition(working, request)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	if after.State != StateWorking || len(after.Events) != len(working.Events)+1 {
		t.Fatalf("cancel request state=%s events=%d", after.State, len(after.Events))
	}
}

func TestLifecycleRejectsOrdinaryTransitionAfterTerminalState(t *testing.T) {
	routed, _ := routedAggregate(t)
	rejectedInput := nextEventInput(routed, "reject-1")
	rejectedInput.Source = "simulation"
	rejectedInput.SourceNamespace = "simulation-policy-v1"
	rejectedInput.Actor = "simulation-venue"
	rejectedInput.ReasonCode = "order_rejected"
	rejectedInput.Evidence = json.RawMessage(`{"status":"rejected"}`)
	rejection, err := ObserveOrderTerminal(routed, EventOrderRejected, rejectedInput, rejectedInput.ReceivedAt)
	if err != nil {
		t.Fatalf("ObserveOrderTerminal() error = %v", err)
	}
	rejected, err := ApplyTransition(routed, rejection)
	if err != nil {
		t.Fatalf("ApplyTransition(rejection) error = %v", err)
	}
	if rejected.State != StateRejected || rejected.RecoveryEligible() {
		t.Fatalf("rejected state=%s recovery=%v", rejected.State, rejected.RecoveryEligible())
	}
	if _, err := ObserveOrderTerminal(rejected, EventOrderCancelled, rejectedInput, rejectedInput.ReceivedAt); err == nil {
		t.Fatal("ObserveOrderTerminal() accepted an ordinary event after terminal state")
	}
}

func TestLifecycleRejectsEventContextMismatch(t *testing.T) {
	proposed, err := Propose(validProposeInput(t))
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	eventInput := nextEventInput(proposed, "allocation-1")
	allocation, err := Allocate(proposed, decimal.NewFromInt(8), eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	allocation.Event.OriginID = "other-origin"
	if _, err := ApplyTransition(proposed, allocation); err == nil {
		t.Fatal("ApplyTransition() accepted mismatched event context")
	}
}

func TestLifecycleRejectsExtraneousChildFacts(t *testing.T) {
	proposed, err := Propose(validProposeInput(t))
	if err != nil {
		t.Fatal(err)
	}
	allocationInput := nextEventInput(proposed, "allocation-child")
	allocation, err := Allocate(proposed, decimal.NewFromInt(8), allocationInput, allocationInput.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	allocation.Order = &Order{}
	if _, err := ApplyTransition(proposed, allocation); err == nil {
		t.Fatal("ApplyTransition() accepted an order child on allocation")
	}
	allocation.Order = nil
	forgedCumulative := decimal.NewFromInt(1)
	allocation.Event.CumulativeFillQuantity = &forgedCumulative
	if _, err := ApplyTransition(proposed, allocation); err == nil {
		t.Fatal("ApplyTransition() accepted cumulative fill quantity on allocation")
	}

	working, _, _ := workingAggregate(t)
	failureInput := nextEventInput(working, "unknown-child")
	failureInput.Source = "simulation"
	failureInput.SourceNamespace = "simulation-policy-v1"
	failureInput.Actor = "reconciler"
	failureInput.ReasonCode = "unknown_venue_state"
	failureInput.Evidence = json.RawMessage(`{"status":"mystery"}`)
	failure, err := FailReconciliation(working, EventUnknownVenueState, failureInput, failureInput.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	failure.Fill = &Fill{}
	if _, err := ApplyTransition(working, failure); err == nil {
		t.Fatal("ApplyTransition() accepted a fill child on reconciliation failure")
	}
}

func TestUnknownVenueStateFailsClosed(t *testing.T) {
	working, _, _ := workingAggregate(t)
	eventInput := nextEventInput(working, "unknown-1")
	eventInput.Source = "simulation"
	eventInput.SourceNamespace = "simulation-policy-v1"
	eventInput.Actor = "reconciler"
	eventInput.ReasonCode = "unknown_venue_state"
	eventInput.Evidence = json.RawMessage(`{"status":"mystery"}`)
	failure, err := FailReconciliation(working, EventUnknownVenueState, eventInput, eventInput.ReceivedAt)
	if err != nil {
		t.Fatalf("FailReconciliation() error = %v", err)
	}
	failed, err := ApplyTransition(working, failure)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	if failed.State != StateFailedReconciliation || failed.RecoveryEligible() {
		t.Fatalf("failed state=%s recovery=%v", failed.State, failed.RecoveryEligible())
	}
}

func TestReplayLifecycleReconstructsOrderAndBinding(t *testing.T) {
	fixture := validProposeInput(t)
	proposed, err := Propose(fixture)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	transitions := []Transition{{Event: proposed.Events[0]}}
	allocationInput := nextEventInput(proposed, "allocation-1")
	allocation, err := Allocate(proposed, decimal.NewFromInt(8), allocationInput, allocationInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	transitions = append(transitions, *allocation)
	allocated, err := ApplyTransition(proposed, allocation)
	if err != nil {
		t.Fatalf("ApplyTransition(allocation) error = %v", err)
	}
	riskInput := nextEventInput(allocated, "risk-1")
	riskInput.Source = "risk"
	riskInput.SourceNamespace = "risk-v1"
	riskInput.Actor = "risk-engine"
	riskInput.ReasonCode = "approved"
	riskInput.Evidence = json.RawMessage(`{"approved":true}`)
	approval, err := ApproveRisk(allocated, riskInput, riskInput.ReceivedAt)
	if err != nil {
		t.Fatalf("ApproveRisk() error = %v", err)
	}
	transitions = append(transitions, *approval)
	approved, err := ApplyTransition(allocated, approval)
	if err != nil {
		t.Fatalf("ApplyTransition(approval) error = %v", err)
	}
	route, err := Route(approved, validRouteInput(t, approved, fixture))
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	transitions = append(transitions, *route)
	routed, err := ApplyTransition(approved, route)
	if err != nil {
		t.Fatalf("ApplyTransition(route) error = %v", err)
	}
	ackInput := nextEventInput(routed, "ack-1")
	ackInput.Source = "simulation"
	ackInput.SourceNamespace = "simulation-v1"
	ackInput.Actor = "simulation-venue"
	ackInput.ReasonCode = "acknowledged"
	ackInput.Evidence = json.RawMessage(`{"external_order_id":"sim-order-1"}`)
	ack, err := Acknowledge(routed, "sim-order-1", ackInput, ackInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	transitions = append(transitions, *ack)

	replayed, err := Replay(fixture.Account.ID, proposed.Intent, transitions)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if replayed.State != StateWorking || replayed.Order == nil || replayed.Binding == nil || len(replayed.Events) != len(transitions) {
		t.Fatalf("replayed state=%s order=%v binding=%v events=%d", replayed.State, replayed.Order, replayed.Binding, len(replayed.Events))
	}

	transitions = append(transitions, transitions[len(transitions)-1])
	if _, err := Replay(fixture.Account.ID, proposed.Intent, transitions); err == nil {
		t.Fatal("Replay() accepted duplicate event identity")
	}
}
