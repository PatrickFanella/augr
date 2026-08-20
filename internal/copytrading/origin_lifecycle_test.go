package copytrading

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestBuildOriginProposalUsesSubscriptionWithoutStrategyVersion(t *testing.T) {
	t.Parallel()
	input := originProposalFixture(t)
	first, err := BuildOriginProposal(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildOriginProposal(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Intent.ID != second.Intent.ID || first.Intent.OriginType != ledger.ExecutionOriginCopySubscription || first.Intent.OriginID != input.Subscription.ID.String() || first.Intent.StrategyVersionID != "" {
		t.Fatalf("proposal=%+v replay=%+v", first.Intent, second.Intent)
	}

	for name, mutate := range map[string]func(*OriginProposalInput){
		"subscription origin": func(value *OriginProposalInput) { value.Subscription.OriginID = uuid.New() },
		"intent origin":       func(value *OriginProposalInput) { value.Intent.OriginID = uuid.New() },
		"intent parent":       func(value *OriginProposalInput) { value.Intent.SubscriptionID = uuid.New() },
		"nonpaper":            func(value *OriginProposalInput) { value.Subscription.IsPaper = false },
		"unapproved":          func(value *OriginProposalInput) { value.Intent.PolicyStatus = "rejected" },
		"wrong side":          func(value *OriginProposalInput) { value.QuantityDelta = value.QuantityDelta.Neg() },
	} {
		value := input
		mutate(&value)
		if _, err := BuildOriginProposal(value); err == nil {
			t.Fatalf("accepted %s forgery", name)
		}
	}
}

func originProposalFixture(t *testing.T) OriginProposalInput {
	t.Helper()
	now := time.Date(2026, 8, 20, 18, 0, 0, 123456000, time.UTC)
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "Copy paper", Environment: domain.AccountEnvironmentPaperScored,
		Venue: "simulation", BaseCurrency: "USD", StorageNamespace: "paper_scored/copy",
		StartingCapital: decimal.NewFromInt(10000), BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile: domain.MarginProfileCash, CreatedBy: "copy-origin-test",
		CreationMetadata: json.RawMessage(`{"fixture":true}`), CreatedAt: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "equity:aapl", AssetClass: instrument.AssetClassEquity,
		PrimaryVenue: "nasdaq", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementCash, Status: instrument.StatusActive,
		Metadata: json.RawMessage(`{"fixture":true}`), CreatedAt: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	availableAt := now.Add(-time.Second)
	exchangeAt := availableAt.Add(-time.Millisecond)
	bid, ask := decimal.RequireFromString("99.99"), decimal.RequireFromString("100.01")
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: reference.ID, Provider: "fixture", Venue: "nasdaq", Source: "fixture-feed",
		ObservationNamespace: "copy", ObservationID: "quote-1", ExchangeAt: &exchangeAt,
		ReceivedAt: exchangeAt.Add(time.Microsecond), AvailableAt: &availableAt, Bid: &bid, Ask: &ask,
		MarketStatus: "open", SessionStatus: "regular", Metadata: json.RawMessage(`{"fixture":true}`), CreatedAt: availableAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID := uuid.New()
	subscription := domain.DefaultCopySubscription()
	subscription.ID, subscription.OriginType, subscription.OriginID = subscriptionID, "copy_subscription", subscriptionID
	subscription.LeaderID, subscription.SourceID = uuid.New(), uuid.New()
	subscription.Status = domain.CopySubscriptionPaperActive
	intent := domain.CopyTradeIntent{
		ID: uuid.New(), SubscriptionID: subscriptionID, OriginType: "copy_subscription", OriginID: subscriptionID,
		SourceObservationID: uuid.New(), InstrumentKey: "AAPL", Ticker: "AAPL", Side: domain.OrderSideBuy,
		RequestedNotional: 1000, CalculationVersion: 1, PolicyStatus: "approved",
	}
	return OriginProposalInput{
		Subscription: subscription, Intent: intent, Account: *account, Instrument: *reference,
		DecisionSnapshot: *snapshot, QuantityDelta: decimal.NewFromInt(10), DecisionAt: now, CreatedAt: now,
	}
}
