package lifecycle

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestRouteOrderCopiesExactAllocatedQuantityAndStableIdentity(t *testing.T) {
	aggregate, fixture := riskApprovedAggregate(t, decimal.NewFromInt(8))
	routeInput := validRouteInput(t, aggregate, fixture)
	transition, err := Route(aggregate, routeInput)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if transition.Order == nil {
		t.Fatal("Route() returned no order")
	}
	if transition.Order.Side != SideBuy || !transition.Order.Quantity.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("order side=%s quantity=%s, want buy 8", transition.Order.Side, transition.Order.Quantity)
	}
	if transition.Order.ClientOrderID != transition.Order.ID.String() {
		t.Fatalf("client order ID = %q, want %s", transition.Order.ClientOrderID, transition.Order.ID)
	}

	retry, err := Route(aggregate, routeInput)
	if err != nil {
		t.Fatalf("Route() retry error = %v", err)
	}
	if retry.Order.ID != transition.Order.ID || !SameOrderPayload(retry.Order, transition.Order) {
		t.Fatal("identical route retry did not converge on the same order")
	}
}

func TestRouteOrderRequiresExactLotAndPriceTick(t *testing.T) {
	t.Run("lot", func(t *testing.T) {
		aggregate, fixture := riskApprovedAggregate(t, decimal.RequireFromString("8.5"))
		routeInput := validRouteInput(t, aggregate, fixture)
		if _, err := Route(aggregate, routeInput); err == nil {
			t.Fatal("Route() accepted quantity off venue lot")
		}
	})

	t.Run("tick", func(t *testing.T) {
		aggregate, fixture := riskApprovedAggregate(t, decimal.NewFromInt(8))
		routeInput := validRouteInput(t, aggregate, fixture)
		routeInput.LimitPrice = decimalPointer("100.005")
		if _, err := Route(aggregate, routeInput); err == nil {
			t.Fatal("Route() accepted limit price off venue tick")
		}
	})
}

func TestRouteOrderRejectsLookaheadAndMismatchedAssessment(t *testing.T) {
	aggregate, fixture := riskApprovedAggregate(t, decimal.NewFromInt(8))
	routeInput := validRouteInput(t, aggregate, fixture)
	availableAt := routeInput.RoutedAt.Add(time.Microsecond)
	routeInput.RouteSnapshot.AvailableAt = &availableAt
	if _, err := Route(aggregate, routeInput); err == nil {
		t.Fatal("Route() accepted a quote unavailable at route time")
	}
}

func TestRouteOrderEnforcesPriceFieldCombinations(t *testing.T) {
	tests := []struct {
		name       string
		orderType  OrderType
		limitPrice *decimal.Decimal
		stopPrice  *decimal.Decimal
		wantError  bool
	}{
		{name: "market", orderType: OrderMarket},
		{name: "market with limit", orderType: OrderMarket, limitPrice: decimalPointer("100"), wantError: true},
		{name: "limit", orderType: OrderLimit, limitPrice: decimalPointer("100")},
		{name: "limit missing price", orderType: OrderLimit, wantError: true},
		{name: "stop", orderType: OrderStop, stopPrice: decimalPointer("99")},
		{name: "stop limit", orderType: OrderStopLimit, limitPrice: decimalPointer("100"), stopPrice: decimalPointer("99")},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			aggregate, fixture := riskApprovedAggregate(t, decimal.NewFromInt(8))
			input := validRouteInput(t, aggregate, fixture)
			input.OrderType = testCase.orderType
			input.LimitPrice = testCase.limitPrice
			input.StopPrice = testCase.stopPrice
			_, err := Route(aggregate, input)
			if (err != nil) != testCase.wantError {
				t.Fatalf("Route() error = %v, wantError=%v", err, testCase.wantError)
			}
		})
	}
}

func TestAcknowledgementCreatesImmutableBindingAndRecoveryState(t *testing.T) {
	routed, _ := routedAggregate(t)
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
	if ack.Binding == nil || ack.Binding.ExternalOrderID != "sim-order-1" {
		t.Fatalf("binding = %#v", ack.Binding)
	}
	working, err := ApplyTransition(routed, ack)
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v", err)
	}
	if working.State != StateWorking || !working.RecoveryEligible() {
		t.Fatalf("working state=%s recovery=%v", working.State, working.RecoveryEligible())
	}
	if routed.RecoveryEligible() != true {
		t.Fatal("routed lifecycle must be recovery eligible")
	}
}

func riskApprovedAggregate(t *testing.T, allocatedQuantity decimal.Decimal) (*Aggregate, ProposeInput) {
	t.Helper()
	fixture := validProposeInput(t)
	if allocatedQuantity.IsNegative() {
		fixture.DesiredQuantityDelta = decimal.NewFromInt(-10)
	} else if allocatedQuantity.GreaterThan(decimal.NewFromInt(10)) {
		fixture.DesiredQuantityDelta = allocatedQuantity
	}
	proposed, err := Propose(fixture)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	allocationInput := nextEventInput(proposed, "allocation-1")
	allocation, err := Allocate(proposed, allocatedQuantity, allocationInput, allocationInput.ReceivedAt)
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	allocated, err := ApplyTransition(proposed, allocation)
	if err != nil {
		t.Fatalf("ApplyTransition(allocation) error = %v", err)
	}
	riskInput := nextEventInput(allocated, "risk-1")
	riskInput.Source = "risk"
	riskInput.SourceNamespace = "risk-policy-v1"
	riskInput.Actor = "risk-engine"
	riskInput.ReasonCode = "approved"
	riskInput.Evidence = json.RawMessage(`{"approved":true}`)
	approval, err := ApproveRisk(allocated, riskInput, riskInput.ReceivedAt)
	if err != nil {
		t.Fatalf("ApproveRisk() error = %v", err)
	}
	approved, err := ApplyTransition(allocated, approval)
	if err != nil {
		t.Fatalf("ApplyTransition(approval) error = %v", err)
	}
	return approved, fixture
}

func validRouteInput(t *testing.T, aggregate *Aggregate, fixture ProposeInput) RouteInput {
	t.Helper()
	routedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID:     fixture.Instrument.ID,
		Venue:            "nasdaq",
		ContractID:       "AAPL",
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementCash,
		ValidFrom:        routedAt.Add(-24 * time.Hour),
		Metadata:         json.RawMessage(`{"fixture":true}`),
		CreatedAt:        routedAt.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("NewVenueContract() error = %v", err)
	}
	exchangeAt := routedAt.Add(-10 * time.Millisecond)
	receivedAt := exchangeAt.Add(time.Millisecond)
	availableAt := receivedAt.Add(time.Millisecond)
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID:         fixture.Instrument.ID,
		VenueContractID:      &contract.ID,
		Provider:             "fixture",
		Venue:                "nasdaq",
		Source:               "fixture-feed",
		ObservationNamespace: "routes",
		ObservationID:        "route-quote-1",
		ExchangeAt:           &exchangeAt,
		ReceivedAt:           receivedAt,
		AvailableAt:          &availableAt,
		Bid:                  decimalPointer("99.99"),
		Ask:                  decimalPointer("100.01"),
		MarketStatus:         "open",
		SessionStatus:        "regular",
		Metadata:             json.RawMessage(`{"fixture":true}`),
		CreatedAt:            availableAt,
	})
	if err != nil {
		t.Fatalf("NewQuoteSnapshot() error = %v", err)
	}
	eventInput := nextEventInput(aggregate, "route-1")
	eventInput.Source = "router"
	eventInput.SourceNamespace = "router-v1"
	eventInput.Actor = "order-router"
	eventInput.ReasonCode = "order_routed"
	eventInput.Evidence = json.RawMessage(`{"policy":"simulation-v1"}`)
	return RouteInput{
		OrderIdempotencyKey: "order-1",
		Instrument:          fixture.Instrument,
		VenueContract:       *contract,
		RouteSnapshot:       *snapshot,
		QuoteRequirements: marketdata.QuoteRequirements{
			RequireSource:          true,
			RequireBid:             true,
			RequireAsk:             true,
			RequireMarketStatus:    true,
			RequireSessionStatus:   true,
			AllowedMarketStatuses:  []string{"open"},
			AllowedSessionStatuses: []string{"regular"},
			MaxAge:                 time.Second,
		},
		OrderType:     OrderLimit,
		TimeInForce:   TimeInForceDay,
		LimitPrice:    decimalPointer("100"),
		PolicyKind:    PolicySimulation,
		PolicyVersion: "simulation-v1",
		Event:         eventInput,
		RoutedAt:      routedAt,
		CreatedAt:     routedAt,
	}
}

func routedAggregate(t *testing.T) (*Aggregate, ProposeInput) {
	t.Helper()
	routed, fixture, _ := routedAggregateWithRoute(t)
	return routed, fixture
}

func routedAggregateWithRoute(t *testing.T) (*Aggregate, ProposeInput, RouteInput) {
	t.Helper()
	approved, fixture := riskApprovedAggregate(t, decimal.NewFromInt(8))
	routeInput := validRouteInput(t, approved, fixture)
	transition, err := Route(approved, routeInput)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	routed, err := ApplyTransition(approved, transition)
	if err != nil {
		t.Fatalf("ApplyTransition(route) error = %v", err)
	}
	return routed, fixture, routeInput
}
