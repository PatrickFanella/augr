package rules

import (
	"context"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func testSpread() *domain.OptionSpread {
	expiry := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	return &domain.OptionSpread{
		StrategyType: domain.StrategyBullPutSpread,
		Underlying:   "AAPL",
		Legs: []domain.SpreadLeg{
			{
				Contract: domain.OptionContract{
					OCCSymbol:  "AAPL260515P00140000",
					Underlying: "AAPL",
					OptionType: domain.OptionTypePut,
					Strike:     140,
					Expiry:     expiry,
					Multiplier: 100,
				},
				Side:           domain.OrderSideSell,
				PositionIntent: domain.PositionIntentSellToOpen,
				Ratio:          1,
				Quantity:       1,
			},
			{
				Contract: domain.OptionContract{
					OCCSymbol:  "AAPL260515P00135000",
					Underlying: "AAPL",
					OptionType: domain.OptionTypePut,
					Strike:     135,
					Expiry:     expiry,
					Multiplier: 100,
				},
				Side:           domain.OrderSideBuy,
				PositionIntent: domain.PositionIntentBuyToOpen,
				Ratio:          1,
				Quantity:       1,
			},
		},
		MaxRisk:   250,
		MaxReward: 150,
	}
}

func testChain() []domain.OptionSnapshot {
	expiry := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	return []domain.OptionSnapshot{
		{
			Contract: domain.OptionContract{
				OCCSymbol: "AAPL260515P00140000", Underlying: "AAPL",
				OptionType: domain.OptionTypePut, Strike: 140, Expiry: expiry, Multiplier: 100,
			},
			Greeks: domain.OptionGreeks{Delta: -0.25, Gamma: 0.03, Theta: -0.05, Vega: 0.12, IV: 0.32},
			Bid:    2.10, Ask: 2.30, Mid: 2.20, Volume: 1200, OpenInterest: 8500,
		},
		{
			Contract: domain.OptionContract{
				OCCSymbol: "AAPL260515P00135000", Underlying: "AAPL",
				OptionType: domain.OptionTypePut, Strike: 135, Expiry: expiry, Multiplier: 100,
			},
			Greeks: domain.OptionGreeks{Delta: -0.15, Gamma: 0.02, Theta: -0.03, Vega: 0.08, IV: 0.34},
			Bid:    0.80, Ask: 0.95, Mid: 0.875, Volume: 900, OpenInterest: 6200,
		},
	}
}

func testOptionsState() *agent.PipelineState {
	return &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars: []domain.OHLCV{
				{Close: 148, Open: 147, High: 149, Low: 146, Volume: 90000},
				{Close: 150, Open: 148, High: 152, Low: 147, Volume: 100000},
			},
			Indicators: []domain.Indicator{
				{Name: "rsi_14", Value: 55},
				{Name: "sma_200", Value: 145},
				{Name: "atr_14", Value: 3.5},
			},
		},
	}
}

func TestOptionsSignalReviewer_EntryConfirm(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"confirm","confidence":0.82,"holding_strategy":"Close at 50% max profit or 21 DTE, whichever comes first. Roll down if delta exceeds -0.30.","reasoning":"IV rank is elevated at 32% supporting premium selling. Strikes are 1.5 ATR from current price with adequate OI."}`,
	}
	reviewer := NewOptionsSignalReviewer(provider, "test-model", nil)

	ok, strategy := reviewer.ReviewSpreadEntry(
		context.Background(), testSpread(), testChain(), testOptionsState(), 150.0, 50000,
	)
	if !ok {
		t.Fatal("expected confirm to return true")
	}
	if strategy == "" {
		t.Fatal("expected non-empty holding strategy")
	}
	if strategy != "Close at 50% max profit or 21 DTE, whichever comes first. Roll down if delta exceeds -0.30." {
		t.Errorf("holding_strategy = %q, want expected value", strategy)
	}
}

func TestOptionsSignalReviewer_EntryVeto(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"veto","confidence":0.25,"holding_strategy":"","reasoning":"IV is too low at 18% for a premium-selling strategy. Wait for IV expansion above 30th percentile before entering bull put spreads."}`,
	}
	reviewer := NewOptionsSignalReviewer(provider, "test-model", nil)

	ok, strategy := reviewer.ReviewSpreadEntry(
		context.Background(), testSpread(), testChain(), testOptionsState(), 150.0, 50000,
	)
	if ok {
		t.Fatal("expected veto to return false")
	}
	if strategy != "" {
		t.Errorf("expected empty strategy on veto, got %q", strategy)
	}
}

func TestOptionsSignalReviewer_ExitConfirm(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"confirm","confidence":0.90,"reasoning":"Position has reached 65% of max profit with 30 DTE remaining. Theta decay diminishing, close to lock in gains."}`,
	}
	reviewer := NewOptionsSignalReviewer(provider, "test-model", nil)

	pos := &OpenPosition{
		Ticker:          "AAPL",
		Side:            domain.PositionSideLong,
		EntryPrice:      1.50,
		EntryDate:       time.Now().AddDate(0, 0, -14),
		Quantity:        1,
		HoldingStrategy: "Close at 50% max profit or 21 DTE",
		Journal: []JournalEntry{
			{Type: EventEntry, Price: 1.50, Reasoning: "Bull put spread opened"},
		},
	}

	ok, reasoning := reviewer.ReviewSpreadExit(
		context.Background(), testSpread(), pos, testChain(), testOptionsState(), 150.0, 50000,
	)
	if !ok {
		t.Fatal("expected exit confirm to return true")
	}
	if reasoning == "" {
		t.Fatal("expected non-empty reasoning")
	}
}

func TestOptionsSignalReviewer_ExitVeto(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{
		response: `{"verdict":"veto","confidence":0.75,"reasoning":"Only at 20% max profit with 40 DTE remaining. Theta is still working in our favor and underlying is well above short strike. Continue holding."}`,
	}
	reviewer := NewOptionsSignalReviewer(provider, "test-model", nil)

	pos := &OpenPosition{
		Ticker:          "AAPL",
		Side:            domain.PositionSideLong,
		EntryPrice:      1.50,
		EntryDate:       time.Now().AddDate(0, 0, -5),
		Quantity:        1,
		HoldingStrategy: "Close at 50% max profit or 21 DTE",
	}

	ok, reasoning := reviewer.ReviewSpreadExit(
		context.Background(), testSpread(), pos, testChain(), testOptionsState(), 150.0, 50000,
	)
	if ok {
		t.Fatal("expected exit veto to return false")
	}
	if reasoning == "" {
		t.Fatal("expected non-empty reasoning on veto")
	}
}

func TestOptionsSignalReviewer_LLMErrorConfirmsByDefault(t *testing.T) {
	t.Parallel()
	provider := &mockLLMProvider{err: context.DeadlineExceeded}
	reviewer := NewOptionsSignalReviewer(provider, "test-model", nil)

	// Entry should confirm by default on error
	ok, _ := reviewer.ReviewSpreadEntry(
		context.Background(), testSpread(), testChain(), testOptionsState(), 150.0, 50000,
	)
	if !ok {
		t.Fatal("expected LLM error to confirm entry by default")
	}

	// Exit should confirm by default on error
	pos := &OpenPosition{
		Ticker: "AAPL", EntryPrice: 1.50, Quantity: 1,
		EntryDate: time.Now().AddDate(0, 0, -7),
	}
	ok, reason := reviewer.ReviewSpreadExit(
		context.Background(), testSpread(), pos, testChain(), testOptionsState(), 150.0, 50000,
	)
	if !ok {
		t.Fatal("expected LLM error to confirm exit by default")
	}
	if reason == "" {
		t.Fatal("expected fallback reason on LLM error")
	}
}
