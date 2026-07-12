package backtest

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

func TestSimulationInputHashIsStableAndCoversBarsAndAssumptions(t *testing.T) {
	now := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	cfg := OrchestratorConfig{StrategyID: uuid.New(), Ticker: "AAPL", StartDate: now, EndDate: now.Add(time.Hour), InitialCash: 1000, FillConfig: FillConfig{Slippage: ProportionalSlippage{BasisPoints: 5}}, PromptVersionHash: "prompt"}
	bars := []domain.OHLCV{{Timestamp: now, Open: 100, High: 101, Low: 99, Close: 100, Volume: 10}}
	a, err := simulationInputHash(cfg, bars)
	if err != nil {
		t.Fatal(err)
	}
	b, err := simulationInputHash(cfg, append([]domain.OHLCV(nil), bars...))
	if err != nil || a != b || len(a) != 64 {
		t.Fatalf("stable hash mismatch a=%q b=%q err=%v", a, b, err)
	}
	bars[0].Close = 100.01
	c, _ := simulationInputHash(cfg, bars)
	if c == a {
		t.Fatal("bar mutation did not change input hash")
	}
	cfg.TrailingStopPct = 5
	d, _ := simulationInputHash(cfg, bars)
	if d == c {
		t.Fatal("assumption mutation did not change input hash")
	}
}
