package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestComputeTradeAnalyticsHoldingPeriodsAndExtremes(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trades := []domain.Trade{
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 100, ExecutedAt: base},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideSell, Quantity: 1, Price: 110, ExecutedAt: base.Add(48 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 120, ExecutedAt: base.Add(72 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideSell, Quantity: 1, Price: 100, ExecutedAt: base.Add(96 * time.Hour)},
	}

	a := ComputeTradeAnalytics(trades, base, base.Add(96*time.Hour))

	if a.ClosedTrades != 2 {
		t.Fatalf("ClosedTrades = %d, want 2", a.ClosedTrades)
	}
	if a.HoldingPeriods.Min != 24*time.Hour {
		t.Errorf("HoldingPeriods.Min = %v, want 24h", a.HoldingPeriods.Min)
	}
	if a.HoldingPeriods.Max != 48*time.Hour {
		t.Errorf("HoldingPeriods.Max = %v, want 48h", a.HoldingPeriods.Max)
	}
	if a.HoldingPeriods.Mean != 36*time.Hour {
		t.Errorf("HoldingPeriods.Mean = %v, want 36h", a.HoldingPeriods.Mean)
	}
	if a.HoldingPeriods.Median != 36*time.Hour {
		t.Errorf("HoldingPeriods.Median = %v, want 36h", a.HoldingPeriods.Median)
	}
	if math.Abs(a.TradeFrequencyPerDay-0.4) > 1e-9 {
		t.Errorf("TradeFrequencyPerDay = %f, want 0.4", a.TradeFrequencyPerDay)
	}
	if a.LargestSingleWin != 10 {
		t.Errorf("LargestSingleWin = %f, want 10", a.LargestSingleWin)
	}
	if a.LargestSingleLoss != -20 {
		t.Errorf("LargestSingleLoss = %f, want -20", a.LargestSingleLoss)
	}
}

func TestComputeTradeAnalyticsConsecutiveStreaks(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trades := []domain.Trade{
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 100, ExecutedAt: base},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideSell, Quantity: 1, Price: 110, ExecutedAt: base.Add(24 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 100, ExecutedAt: base.Add(48 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideSell, Quantity: 1, Price: 105, ExecutedAt: base.Add(72 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 100, ExecutedAt: base.Add(96 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideSell, Quantity: 1, Price: 95, ExecutedAt: base.Add(120 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 100, ExecutedAt: base.Add(144 * time.Hour)},
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideSell, Quantity: 1, Price: 90, ExecutedAt: base.Add(168 * time.Hour)},
	}

	a := ComputeTradeAnalytics(trades, base, base.Add(168*time.Hour))
	if a.MaxConsecutiveWins != 2 {
		t.Errorf("MaxConsecutiveWins = %d, want 2", a.MaxConsecutiveWins)
	}
	if a.MaxConsecutiveLosses != 2 {
		t.Errorf("MaxConsecutiveLosses = %d, want 2", a.MaxConsecutiveLosses)
	}
}

func TestComputeTradeAnalyticsNoClosedTrades(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trades := []domain.Trade{
		{ID: uuid.New(), Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 1, Price: 100, ExecutedAt: base},
	}

	a := ComputeTradeAnalytics(trades, base, base.Add(24*time.Hour))
	if a.ClosedTrades != 0 {
		t.Errorf("ClosedTrades = %d, want 0", a.ClosedTrades)
	}
	if a.TradeFrequencyPerDay != 0 {
		t.Errorf("TradeFrequencyPerDay = %f, want 0", a.TradeFrequencyPerDay)
	}
	if a.MaxConsecutiveWins != 0 {
		t.Errorf("MaxConsecutiveWins = %d, want 0", a.MaxConsecutiveWins)
	}
	if a.MaxConsecutiveLosses != 0 {
		t.Errorf("MaxConsecutiveLosses = %d, want 0", a.MaxConsecutiveLosses)
	}
}
