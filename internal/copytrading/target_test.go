package copytrading

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

func TestBuild13FTargetPreservesUnmappedWeightAsCashAndCapsTurnover(t *testing.T) {
	t.Parallel()
	sub := domain.DefaultCopySubscription()
	sub.ID = uuid.New()
	sub.LeaderID = uuid.New()
	sub.SourceID = uuid.New()
	sub.CapitalBudget = 10_000
	sub.CashBufferPct = 0.10
	sub.MaxPositionWeight = 0.40
	sub.MaxTurnoverPct = 0.25
	sub.MinAvgDollarVolume = 1
	obs := domain.CopySourceObservation{ID: uuid.New()}
	snapshot := domain.CopyPortfolioSnapshot{TotalDisclosedValue: 1_000, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 600}, {CUSIP: "BBB", DisclosedValue: 300}, {CUSIP: "PUT", DisclosedValue: 100, PutCall: "PUT"}}}
	mappings := []domain.CopyInstrumentMapping{{Provider: "sec", IdentifierType: "cusip", IdentifierValue: "AAA", Ticker: "AAPL", InstrumentKey: "AAPL", Confidence: "manual_verified"}}
	prices := map[string]PriceSnapshot{"AAPL": {Ticker: "AAPL", Price: 100, AvgDollarVolume: 1_000_000, ObservedAt: time.Now()}}

	preview := Build13FTarget(TargetInput{Subscription: sub, Observation: obs, Snapshot: snapshot, Mappings: mappings, Prices: prices})
	if len(preview.Intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1", len(preview.Intents))
	}
	if got := preview.Intents[0].TargetValue; got != 4000 { // 60% * 9000, capped at 40% of capital
		t.Fatalf("TargetValue = %v, want 4000", got)
	}
	if got := preview.Intents[0].RequestedNotional; got != 2500 {
		t.Fatalf("RequestedNotional = %v, want turnover cap 2500", got)
	}
	if preview.Summary.UnmappedWeight != 0.3 || preview.Summary.ExcludedWeight != 0.1 {
		t.Fatalf("summary weights = %+v", preview.Summary)
	}
	if preview.Summary.TargetCashValue != 6000 {
		t.Fatalf("TargetCashValue = %v, want 6000", preview.Summary.TargetCashValue)
	}
}

func TestBuild13FTargetSellsOnlyAttributedPosition(t *testing.T) {
	t.Parallel()
	sub := domain.DefaultCopySubscription()
	sub.ID, sub.LeaderID, sub.SourceID = uuid.New(), uuid.New(), uuid.New()
	sub.MinAvgDollarVolume = 0
	strategyID := uuid.New()
	price := 50.0
	preview := Build13FTarget(TargetInput{
		Subscription: sub,
		Observation:  domain.CopySourceObservation{ID: uuid.New()},
		Snapshot:     domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "OTHER", DisclosedValue: 100}}},
		Prices:       map[string]PriceSnapshot{"MSFT": {Ticker: "MSFT", Price: price}},
		Positions:    []domain.Position{{StrategyID: &strategyID, Ticker: "MSFT", Quantity: 10, AvgEntry: 40}},
	})
	if len(preview.Intents) != 1 || preview.Intents[0].Side != domain.OrderSideSell {
		t.Fatalf("intents = %+v, want one sell", preview.Intents)
	}
	if preview.Intents[0].AttributedCurrentValue != 500 || preview.Intents[0].RequestedNotional != 500 {
		t.Fatalf("sell intent = %+v", preview.Intents[0])
	}
}

func TestBuild13FTargetSkipsMissingPrice(t *testing.T) {
	t.Parallel()
	sub := domain.DefaultCopySubscription()
	sub.ID, sub.LeaderID, sub.SourceID = uuid.New(), uuid.New(), uuid.New()
	preview := Build13FTarget(TargetInput{Subscription: sub, Observation: domain.CopySourceObservation{ID: uuid.New()}, Snapshot: domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 100}}}, Mappings: []domain.CopyInstrumentMapping{{IdentifierValue: "AAA", Ticker: "AAPL", Confidence: "manual_verified"}}})
	if len(preview.Intents) != 1 || preview.Intents[0].Status != "skipped" || preview.Intents[0].PolicyReasons[0] != "missing_current_price" {
		t.Fatalf("intent = %+v", preview.Intents)
	}
}
