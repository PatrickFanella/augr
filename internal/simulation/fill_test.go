package simulation

import (
	"bytes"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestSimulationFillCreatesExactRawNormalizationLifecycleGraph(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("graph", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transitions) != 1 || result.Transitions[0].Fill == nil || result.Transitions[0].Normalization == nil {
		t.Fatalf("fill graph transition = %#v", result.Transitions)
	}
	transition := result.Transitions[0]
	normalization := transition.Normalization
	if err := normalization.SourceEvent.Validate(); err != nil {
		t.Fatalf("raw source event: %v", err)
	}
	if err := normalization.Validate(); err != nil {
		t.Fatalf("normalization: %v", err)
	}
	if err := normalization.Transaction.Validate(); err != nil {
		t.Fatalf("ledger transaction: %v", err)
	}
	if normalization.ReferenceType != "execution_fill" || normalization.ReferenceID != transition.Fill.ID.String() ||
		transition.Fill.EconomicSourceEventID != normalization.SourceEvent.ID ||
		transition.Fill.NormalizationID != normalization.ID ||
		transition.Fill.LedgerTransactionID != normalization.Transaction.ID ||
		!bytes.Equal(transition.Event.Evidence, normalization.SourceEvent.RawPayload) {
		t.Fatalf("fill graph links = fill:%+v normalization:%+v", transition.Fill, normalization)
	}
}

func TestSimulationFillChargesPerOrderFeeOnce(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("fee-once", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.30"), Size: decimal.NewFromInt(6)},
		},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fills) != 2 || result.Fills[0].Fee == nil || result.Fills[1].Fee == nil {
		t.Fatalf("fees = %#v", result.Fills)
	}
	wantFirst, err := fixture.policy.FillFee(fixture.instrument.AssetClass, decimal.NewFromInt(4), decimal.RequireFromString("10.26"), decimal.NewFromInt(1), true)
	if err != nil {
		t.Fatal(err)
	}
	wantLater, err := fixture.policy.FillFee(fixture.instrument.AssetClass, decimal.NewFromInt(6), decimal.RequireFromString("10.30"), decimal.NewFromInt(1), false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Fills[0].Fee.Equal(*wantFirst) || !result.Fills[1].Fee.Equal(*wantLater) ||
		result.Fills[0].Fee.Sub(*result.Fills[1].Fee).LessThan(decimal.NewFromInt(1)) {
		t.Fatalf("first/later fees = %s/%s, want %s/%s", result.Fills[0].Fee, result.Fills[1].Fee, wantFirst, wantLater)
	}
}

func TestSimulationFillUsesContractMultiplierAndExactCurrency(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.quantity = decimal.NewFromInt(2)
		config.multiplier = decimal.NewFromInt(100)
	})
	snapshot := fixture.snapshot("multiplier", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("0.24"), Size: decimal.NewFromInt(2)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("0.25"), Size: decimal.NewFromInt(2)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	normalization := result.Transitions[0].Normalization
	if normalization.CashCurrency != "USD" || normalization.CostCurrency != "USD" || normalization.CostAmount == nil ||
		normalization.VenueContract == nil || !normalization.VenueContract.Multiplier.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("multiplier/currency normalization = %+v", normalization)
	}
	if result.Fills[0].Fee == nil || !normalization.CostAmount.Equal(*result.Fills[0].Fee) {
		t.Fatalf("normalization/effect fee = %v/%v", normalization.CostAmount, result.Fills[0].Fee)
	}
}

func TestPredictionFillRetainsExactZeroPrice(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.assetClass = instrument.AssetClassPredictionContract
		config.quantity = decimal.NewFromInt(2)
	})
	snapshot := fixture.snapshot("zero-price", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.Zero, Size: decimal.NewFromInt(2)}},
		[]marketdata.DepthLevelInput{{Price: decimal.Zero, Size: decimal.NewFromInt(2)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fills) != 1 || !result.Fills[0].Price.IsZero() ||
		result.Transitions[0].Normalization.Price == nil || !result.Transitions[0].Normalization.Price.IsZero() ||
		!result.Transitions[0].Fill.Price.IsZero() {
		t.Fatalf("zero-price result = %#v", result.Fills)
	}
}

func TestSimulationFillIDsConvergeAndChangedObservationConflicts(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("identity", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	first, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	firstGraph, retryGraph := first.Transitions[0], retry.Transitions[0]
	if firstGraph.Fill.ID != retryGraph.Fill.ID || firstGraph.Normalization.SourceEvent.ID != retryGraph.Normalization.SourceEvent.ID ||
		firstGraph.Normalization.ID != retryGraph.Normalization.ID || firstGraph.Normalization.Transaction.ID != retryGraph.Normalization.Transaction.ID ||
		!ledger.SameEconomicSourceEventPayload(firstGraph.Normalization.SourceEvent, retryGraph.Normalization.SourceEvent) {
		t.Fatal("identical simulation fill did not converge")
	}
	changed := snapshot
	changed.Depth = append([]marketdata.DepthLevel(nil), snapshot.Depth...)
	for index := range changed.Depth {
		if changed.Depth[index].Side == marketdata.DepthSideAsk {
			changed.Depth[index].Size = decimal.NewFromInt(12)
		}
	}
	changedAskSize := decimal.NewFromInt(12)
	changed.AskSize = &changedAskSize
	changedResult, err := fixture.evaluate(fixture.aggregate, changed, *changed.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	changedSource := changedResult.Transitions[0].Normalization.SourceEvent
	if changedSource.ID != firstGraph.Normalization.SourceEvent.ID || ledger.SameEconomicSourceEventPayload(firstGraph.Normalization.SourceEvent, changedSource) {
		t.Fatal("changed observation did not retain identity and expose payload conflict")
	}
}
