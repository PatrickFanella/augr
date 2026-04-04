package rules

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestTradeJournal_OpenAndClose(t *testing.T) {
	t.Parallel()
	j := NewTradeJournal()

	if j.IsHolding("AAPL") {
		t.Fatal("should not be holding before open")
	}

	j.OpenNewPosition(OpenPosition{
		Ticker:       "AAPL",
		Side:         domain.PositionSideLong,
		EntryPrice:   150,
		EntryDate:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Quantity:     10,
		HardStopLoss: 140,
		TakeProfit:   170,
	})

	if !j.IsHolding("AAPL") {
		t.Fatal("should be holding after open")
	}
	pos := j.GetOpen("AAPL")
	if pos.CostBasis != 1500 {
		t.Errorf("cost basis = %v, want 1500", pos.CostBasis)
	}

	closed := j.ClosePosition("AAPL", 160, time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), "signal_confirmed")
	if closed == nil {
		t.Fatal("close returned nil")
	}
	if closed.RealizedPnL != 100 {
		t.Errorf("realized pnl = %v, want 100 (10 shares * $10)", closed.RealizedPnL)
	}
	if closed.HoldingDaysN != 30 {
		t.Errorf("holding days = %v, want 30", closed.HoldingDaysN)
	}
	if j.IsHolding("AAPL") {
		t.Fatal("should not be holding after close")
	}
	if len(j.Closed) != 1 {
		t.Errorf("closed count = %d, want 1", len(j.Closed))
	}
}

func TestOpenPosition_StopHit(t *testing.T) {
	t.Parallel()
	pos := &OpenPosition{
		Side:              domain.PositionSideLong,
		HardStopLoss:      145,
		TrailingStopPct:   5,
		TrailingStopLevel: 148,
	}

	// Bar doesn't hit either stop
	hit, _ := pos.IsStopHit(domain.OHLCV{Low: 149, High: 155})
	if hit {
		t.Error("should not hit at low 149")
	}

	// Bar hits trailing stop
	hit, reason := pos.IsStopHit(domain.OHLCV{Low: 147, High: 155})
	if !hit {
		t.Error("should hit trailing stop at low 147")
	}
	if reason == "" {
		t.Error("reason should be set")
	}

	// Bar hits hard stop
	hit, _ = pos.IsStopHit(domain.OHLCV{Low: 144, High: 150})
	if !hit {
		t.Error("should hit hard stop at low 144")
	}
}

func TestOpenPosition_TrailingStopUpdate(t *testing.T) {
	t.Parallel()
	pos := &OpenPosition{
		Side:              domain.PositionSideLong,
		TrailingStopPct:   5,
		TrailingStopLevel: 142.5, // 5% below 150
	}

	// Price rises to 160 — trailing stop should advance
	pos.UpdateTrailingStop(160)
	want := 160 * 0.95
	if pos.TrailingStopLevel != want {
		t.Errorf("trailing stop = %v, want %v", pos.TrailingStopLevel, want)
	}

	// Price drops to 155 — trailing stop should NOT decrease
	pos.UpdateTrailingStop(155)
	if pos.TrailingStopLevel != want {
		t.Errorf("trailing stop decreased to %v, should stay at %v", pos.TrailingStopLevel, want)
	}
}

func TestOpenPosition_TakeProfitHit(t *testing.T) {
	t.Parallel()
	pos := &OpenPosition{Side: domain.PositionSideLong, TakeProfit: 170}

	if pos.IsTakeProfitHit(domain.OHLCV{High: 169}) {
		t.Error("should not hit at high 169")
	}
	if !pos.IsTakeProfitHit(domain.OHLCV{High: 171}) {
		t.Error("should hit at high 171")
	}
}

func TestOpenPosition_UnrealizedPnL(t *testing.T) {
	t.Parallel()
	pos := &OpenPosition{Side: domain.PositionSideLong, EntryPrice: 150}
	if pnl := pos.UnrealizedPnL(160); pnl != 10 {
		t.Errorf("long pnl = %v, want 10", pnl)
	}

	short := &OpenPosition{Side: domain.PositionSideShort, EntryPrice: 150}
	if pnl := short.UnrealizedPnL(140); pnl != 10 {
		t.Errorf("short pnl = %v, want 10", pnl)
	}
}

func TestFormatDecisionHistory(t *testing.T) {
	t.Parallel()
	pos := &OpenPosition{
		Ticker:          "AAPL",
		Side:            domain.PositionSideLong,
		EntryPrice:      150,
		EntryDate:       time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Quantity:        10,
		CostBasis:       1500,
		HardStopLoss:    140,
		TakeProfit:      170,
		HoldingStrategy: "Ride momentum until RSI reversal.",
		Journal: []JournalEntry{
			{Type: EventEntry, Timestamp: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), Signal: "buy", Verdict: "confirm", Confidence: 0.85, Price: 150, Reasoning: "Strong entry"},
			{Type: EventHold, Timestamp: time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC), Price: 155, Reasoning: "trailing stop updated"},
		},
	}

	output := FormatDecisionHistory(pos, 160, time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC))
	if output == "" {
		t.Fatal("output should not be empty")
	}
	// Check key sections present
	for _, want := range []string{"CURRENT POSITION", "DECISION HISTORY", "ENTRY", "HOLD", "Holding Strategy", "Ride momentum"} {
		if !contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
