package lifecycle

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestIntentIdentityUsesAccountAndIdempotencyKey(t *testing.T) {
	input := validProposeInput(t)
	first, err := Propose(input)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}

	input.CreatedAt = input.CreatedAt.Add(time.Second)
	second, err := Propose(input)
	if err != nil {
		t.Fatalf("Propose() retry error = %v", err)
	}

	want := economicid.DeterministicUUID("execution-intent", input.Account.ID.String(), input.IdempotencyKey)
	if first.Intent.ID != want || second.Intent.ID != want {
		t.Fatalf("intent IDs = %s and %s, want %s", first.Intent.ID, second.Intent.ID, want)
	}
	if !SameIntentPayload(&first.Intent, &second.Intent) {
		t.Fatal("created-at-only retry should have the same semantic intent payload")
	}
}

func TestIntentReplayRejectsChangedEconomicPayload(t *testing.T) {
	input := validProposeInput(t)
	first, err := Propose(input)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}

	input.DesiredQuantityDelta = decimal.RequireFromString("9")
	changed, err := Propose(input)
	if err != nil {
		t.Fatalf("Propose() changed payload error = %v", err)
	}
	if first.Intent.ID != changed.Intent.ID {
		t.Fatalf("same identity key produced IDs %s and %s", first.Intent.ID, changed.Intent.ID)
	}
	if SameIntentPayload(&first.Intent, &changed.Intent) {
		t.Fatal("changed desired quantity was accepted as an identical retry")
	}
}

func TestIntentRequiresAccountEnvironmentAndCanonicalInstrument(t *testing.T) {
	input := validProposeInput(t)
	input.Account.Status = domain.AccountStatusPaused
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted a paused account")
	}

	input = validProposeInput(t)
	input.Instrument.Status = instrument.StatusQuarantined
	input.Instrument.Metadata = json.RawMessage(`{"reason":"ambiguous"}`)
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted a quarantined instrument")
	}

	input = validProposeInput(t)
	input.DecisionSnapshot.InstrumentID = uuid.New()
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted a decision snapshot for another instrument")
	}
}

func TestIntentRequiresNonzeroExactDesiredQuantity(t *testing.T) {
	for name, quantity := range map[string]decimal.Decimal{
		"zero":      decimal.Zero,
		"overscale": decimal.RequireFromString("1.0000000000001"),
		"oversize":  decimal.RequireFromString("100000000000000000000000000"),
	} {
		t.Run(name, func(t *testing.T) {
			input := validProposeInput(t)
			input.DesiredQuantityDelta = quantity
			if _, err := Propose(input); err == nil {
				t.Fatalf("Propose() accepted desired quantity %s", quantity)
			}
		})
	}
}

func TestIntentRequiresDecisionAvailableQuote(t *testing.T) {
	input := validProposeInput(t)
	input.DecisionSnapshot.AvailableAt = nil
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted a quote with no decision-availability time")
	}
}

func TestIntentRejectsLookaheadDecisionQuote(t *testing.T) {
	input := validProposeInput(t)
	availableAt := input.DecisionAt.Add(time.Microsecond)
	input.DecisionSnapshot.AvailableAt = &availableAt
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted a quote unavailable at decision time")
	}
}

func TestStrategyOriginRequiresMatchingStrategyVersion(t *testing.T) {
	input := validProposeInput(t)
	input.StrategyVersionID = "strategy-version-2"
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted a mismatched strategy version")
	}
}

func TestIntentRejectsNonObjectMetadata(t *testing.T) {
	input := validProposeInput(t)
	input.Metadata = json.RawMessage(`[]`)
	if _, err := Propose(input); err == nil {
		t.Fatal("Propose() accepted non-object metadata")
	}
}

func validProposeInput(t *testing.T) ProposeInput {
	t.Helper()
	now := time.Date(2026, 8, 15, 15, 4, 5, 123456000, time.UTC)
	account, err := domain.NewAccount(domain.AccountInput{
		Name:                  "Lifecycle scored paper",
		Environment:           domain.AccountEnvironmentPaperScored,
		Venue:                 "simulation",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/lifecycle-test",
		StartingCapital:       decimal.NewFromInt(100000),
		BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile:         domain.MarginProfileCash,
		CreatedBy:             "lifecycle-test",
		CreationMetadata:      json.RawMessage(`{"fixture":true}`),
		CreatedAt:             now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey:      "equity:aapl",
		AssetClass:       instrument.AssetClassEquity,
		PrimaryVenue:     "nasdaq",
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementCash,
		Status:           instrument.StatusActive,
		Metadata:         json.RawMessage(`{"fixture":true}`),
		CreatedAt:        now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("NewInstrument() error = %v", err)
	}
	availableAt := now.Add(-time.Second)
	exchangeAt := availableAt.Add(-2 * time.Millisecond)
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID:         reference.ID,
		Provider:             "fixture",
		Venue:                "nasdaq",
		Source:               "fixture-feed",
		ObservationNamespace: "quotes",
		ObservationID:        "quote-1",
		ExchangeAt:           &exchangeAt,
		ReceivedAt:           exchangeAt.Add(time.Millisecond),
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
	return ProposeInput{
		Account:              *account,
		Instrument:           *reference,
		DecisionSnapshot:     *snapshot,
		IdempotencyKey:       "strategy-version-1:signal-1",
		DesiredQuantityDelta: decimal.NewFromInt(10),
		DecisionAt:           now,
		OriginType:           ledger.ExecutionOriginStrategyVersion,
		OriginID:             "strategy-version-1",
		StrategyVersionID:    "strategy-version-1",
		Metadata:             json.RawMessage(`{"signal":"entry"}`),
		Event: EventInput{
			Source:          "strategy",
			SourceNamespace: "strategy-version-1",
			SourceEventID:   "signal-1",
			SourceAt:        now.Add(-time.Millisecond),
			ReceivedAt:      now,
			Actor:           "strategy-runner",
			ReasonCode:      "signal_proposed",
			Evidence:        json.RawMessage(`{"signal":"entry"}`),
		},
		CreatedAt: now,
	}
}

func decimalPointer(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}
