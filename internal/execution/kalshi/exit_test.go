package kalshi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestEvaluateExitDeterministicTriggers(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	strategyID := uuid.New()
	strategy := domain.Strategy{
		ID:         strategyID,
		Ticker:     "KX-TEST",
		MarketType: domain.MarketTypeKalshi,
		Config:     json.RawMessage(`{"discovery_meta":{"template":"microstructure","direction":"YES","confidence":0.72,"auto_exits_enabled":true}}`),
	}
	base := Snapshot{
		Ticker: "KX-TEST", Title: "Test", Status: "active",
		BestBidYes: 0.50, BestAskYes: 0.51, BestBidNo: 0.49, BestAskNo: 0.50,
		Volume: 2000, OpenInterest: 2000, CloseTime: now.Add(24 * time.Hour), FetchedAt: now,
	}
	positions := []domain.Position{
		{StrategyID: &strategyID, MarketType: domain.MarketTypeKalshi, Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 40, AvgEntry: 0.40},
		{StrategyID: &strategyID, MarketType: domain.MarketTypeKalshi, Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 60, AvgEntry: 0.40},
	}

	decision, ok := EvaluateExit(strategy, base, positions, now)
	if !ok || decision.Signal != domain.PipelineSignalSell || decision.PositionSize != 100 || decision.EntryPrice != 0.50 {
		t.Fatalf("take-profit decision = %+v, ok=%v", decision, ok)
	}

	stop := base
	stop.BestBidYes = 0.30
	stop.BestAskYes = 0.31
	decision, ok = EvaluateExit(strategy, stop, positions, now)
	if !ok || decision.RealizedPnLPct > -0.249999 || decision.RealizedPnLPct < -0.250001 {
		t.Fatalf("stop-loss decision = %+v, ok=%v", decision, ok)
	}

	nearClose := base
	nearClose.BestBidYes = 0.40
	nearClose.BestAskYes = 0.41
	nearClose.CloseTime = now.Add(20 * time.Minute)
	decision, ok = EvaluateExit(strategy, nearClose, positions, now)
	if !ok || decision.GateResults[len(decision.GateResults)-1] != "close_time_guard" {
		t.Fatalf("close-time decision = %+v, ok=%v", decision, ok)
	}
}

func TestEvaluateExitHoldsWithoutOwnedPositionOrTrigger(t *testing.T) {
	now := time.Now().UTC()
	strategy := domain.Strategy{
		ID: uuid.New(), Ticker: "KX-TEST", MarketType: domain.MarketTypeKalshi,
		Config: json.RawMessage(`{"discovery_meta":{"template":"microstructure","direction":"YES","confidence":0.72,"auto_exits_enabled":true}}`),
	}
	snapshot := Snapshot{Ticker: "KX-TEST", Title: "Test", Status: "active", BestBidYes: 0.40, BestAskYes: 0.41, BestBidNo: 0.59, BestAskNo: 0.60, Volume: 2000, CloseTime: now.Add(time.Hour)}
	if decision, ok := EvaluateExit(strategy, snapshot, nil, now); ok {
		t.Fatalf("unexpected exit without position: %+v", decision)
	}
	positions := []domain.Position{{Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 10, AvgEntry: 0.40}}
	if decision, ok := EvaluateExit(strategy, snapshot, positions, now); ok {
		t.Fatalf("unexpected exit without trigger: %+v", decision)
	}
}

func TestEvaluateExitRequiresStrategyOptIn(t *testing.T) {
	now := time.Now().UTC()
	strategy := domain.Strategy{
		ID: uuid.New(), Ticker: "KX-TEST", MarketType: domain.MarketTypeKalshi,
		Config: json.RawMessage(`{"discovery_meta":{"template":"microstructure","direction":"YES","confidence":0.72}}`),
	}
	snapshot := Snapshot{Ticker: "KX-TEST", Title: "Test", Status: "active", BestBidYes: 0.90, BestAskYes: 0.91, BestBidNo: 0.09, BestAskNo: 0.10, Volume: 2000, CloseTime: now.Add(time.Hour)}
	positions := []domain.Position{{Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 10, AvgEntry: 0.40}}
	if decision, ok := EvaluateExit(strategy, snapshot, positions, now); ok {
		t.Fatalf("unexpected exit without strategy opt-in: %+v", decision)
	}
}
