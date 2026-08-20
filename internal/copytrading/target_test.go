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
	now := time.Now().UTC()
	obs := domain.CopySourceObservation{ID: uuid.New()}
	snapshot := domain.CopyPortfolioSnapshot{TotalDisclosedValue: 1_000, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 600}, {CUSIP: "BBB", DisclosedValue: 300}, {CUSIP: "PUT", DisclosedValue: 100, PutCall: "PUT"}}}
	mappings := []domain.CopyInstrumentMapping{{Provider: "sec", IdentifierType: "cusip", IdentifierValue: "AAA", Ticker: "AAPL", InstrumentKey: "AAPL", Confidence: "manual_verified"}}
	prices := map[string]PriceSnapshot{"AAPL": validPriceSnapshot(now, "99.99", "100.01")}

	preview := Build13FTarget(TargetInput{Subscription: sub, Observation: obs, Snapshot: snapshot, Mappings: mappings, Prices: prices, DecisionAt: now})
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
	now := time.Now().UTC()
	preview := Build13FTarget(TargetInput{
		Subscription: sub,
		Observation:  domain.CopySourceObservation{ID: uuid.New()},
		Snapshot:     domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "OTHER", DisclosedValue: 100}}},
		Prices:       map[string]PriceSnapshot{"MSFT": validPriceSnapshot(now, "49.99", "50.01")},
		Positions:    []domain.Position{{StrategyID: &strategyID, Ticker: "MSFT", Quantity: 10, AvgEntry: 40}},
		DecisionAt:   now,
	})
	if len(preview.Intents) != 1 || preview.Intents[0].Side != domain.OrderSideSell {
		t.Fatalf("intents = %+v, want one sell", preview.Intents)
	}
	if preview.Intents[0].AttributedCurrentValue != price*10 || preview.Intents[0].RequestedNotional != price*10 {
		t.Fatalf("sell intent = %+v", preview.Intents[0])
	}
}

func TestBuild13FTargetSkipsMissingPrice(t *testing.T) {
	t.Parallel()
	sub := domain.DefaultCopySubscription()
	sub.ID, sub.LeaderID, sub.SourceID = uuid.New(), uuid.New(), uuid.New()
	preview := Build13FTarget(TargetInput{Subscription: sub, Observation: domain.CopySourceObservation{ID: uuid.New()}, Snapshot: domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 100}}}, Mappings: []domain.CopyInstrumentMapping{{IdentifierValue: "AAA", Ticker: "AAPL", Confidence: "manual_verified"}}})
	if len(preview.Intents) != 1 || preview.Intents[0].Status != "skipped" || preview.Intents[0].PolicyReasons[0] != "missing_executable_quote" {
		t.Fatalf("intent = %+v", preview.Intents)
	}
}

func TestBuild13FTargetEnforcesExactSpreadFreshnessAndSession(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)
	base := domain.DefaultCopySubscription()
	base.ID, base.LeaderID, base.SourceID = uuid.New(), uuid.New(), uuid.New()
	base.MinAvgDollarVolume, base.MinPrice, base.MaxSpreadBPS, base.MaxQuoteAgeSeconds = 0, 0, 100, 60
	observation := domain.CopySourceObservation{ID: uuid.New()}
	snapshot := domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 100}}}
	mappings := []domain.CopyInstrumentMapping{{IdentifierValue: "AAA", Ticker: "AAPL", Confidence: "manual_verified"}}

	for _, test := range []struct {
		name   string
		mutate func(*PriceSnapshot)
		reason string
	}{
		{"threshold equality", func(value *PriceSnapshot) { value.Bid, value.Ask = "99.5", "100.5" }, ""},
		{"above spread", func(value *PriceSnapshot) { value.Bid, value.Ask = "99.49", "100.51" }, "above_max_spread"},
		{"missing bid", func(value *PriceSnapshot) { value.Bid = "" }, "missing_two_sided_quote"},
		{"crossed", func(value *PriceSnapshot) { value.Bid, value.Ask = "101", "100" }, "crossed_quote"},
		{"stale", func(value *PriceSnapshot) { stale := now.Add(-61 * time.Second); value.AvailableAt = &stale }, "stale_quote"},
		{"future", func(value *PriceSnapshot) { future := now.Add(time.Microsecond); value.AvailableAt = &future }, "quote_not_yet_available"},
		{"closed", func(value *PriceSnapshot) { value.MarketStatus = "closed" }, "market_not_open"},
		{"extended", func(value *PriceSnapshot) { value.SessionStatus = "after_hours" }, "session_not_allowed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			quote := validPriceSnapshot(now, "99.99", "100.01")
			test.mutate(&quote)
			preview := Build13FTarget(TargetInput{Subscription: base, Observation: observation, Snapshot: snapshot, Mappings: mappings, Prices: map[string]PriceSnapshot{"AAPL": quote}, DecisionAt: now})
			if len(preview.Intents) != 1 {
				t.Fatalf("intents=%+v", preview.Intents)
			}
			intent := preview.Intents[0]
			if test.reason == "" {
				if intent.PolicyStatus != "approved" || intent.ExecutablePrice == nil || *intent.ExecutablePrice != 100.5 || intent.DecisionSpreadBPS != "100" {
					t.Fatalf("approved=%+v", intent)
				}
			} else if intent.PolicyStatus != "skipped" || !containsReason(intent.PolicyReasons, test.reason) {
				t.Fatalf("reason %s not found in %+v", test.reason, intent)
			}
		})
	}
}

func validPriceSnapshot(now time.Time, bid, ask string) PriceSnapshot {
	available := now.Add(-time.Second)
	return PriceSnapshot{Ticker: "AAPL", QuoteSnapshotID: uuid.New(), Bid: bid, Ask: ask, AvgDollarVolume: 1_000_000, AvailableAt: &available, MarketStatus: "open", SessionStatus: "regular"}
}

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
