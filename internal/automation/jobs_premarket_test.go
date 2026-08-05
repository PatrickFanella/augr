package automation

import (
	"context"
	"errors"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type positionReviewStrategyRepo struct{ *kalshiStrategyRepoStub }

func (s *positionReviewStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return len(s.strategies), nil
}

func TestGapScannerSkipsWhenUniverseIsNotConfigured(t *testing.T) {
	orch := NewJobOrchestrator(OrchestratorDeps{})
	if err := orch.gapScanner(context.Background()); err != nil {
		t.Fatalf("gapScanner() error = %v, want nil", err)
	}
}

func TestIsKalshiRateLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "status", err: errors.New("kalshi: request failed (status=429)"), want: true},
		{name: "provider code", err: errors.New(`{"code":"too_many_requests"}`), want: true},
		{name: "other provider error", err: errors.New("kalshi: request failed (status=500)"), want: false},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isKalshiRateLimit(test.err); got != test.want {
				t.Fatalf("isKalshiRateLimit(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

func TestPositionReviewSummarizesActualOpenPositions(t *testing.T) {
	activeID := uuid.New()
	inactiveID := uuid.New()
	price := 101.0
	stop := 95.0
	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo: &positionReviewStrategyRepo{&kalshiStrategyRepoStub{strategies: []domain.Strategy{{
			ID: activeID, Ticker: "AAPL", Status: domain.StrategyStatusActive,
		}}}},
		PositionRepo: &polymarketPositionRepoStub{positions: []domain.Position{
			{ID: uuid.New(), StrategyID: &activeID, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, CurrentPrice: &price, StopLoss: &stop},
			{ID: uuid.New(), StrategyID: &inactiveID, Ticker: "MSFT", Side: domain.PositionSideLong, Quantity: 1},
			{ID: uuid.New(), Ticker: "LEGACY", Side: domain.PositionSideShort, Quantity: 1},
		}},
	})
	orch.Register("position_review", "test", schedulerSpecEveryMinute(), orch.positionReview)

	if err := orch.positionReview(context.Background()); err != nil {
		t.Fatalf("positionReview() error = %v", err)
	}
	got := orch.jobs["position_review"].LastSummary
	want := map[string]int{
		"active_strategies": 1, "open_positions": 3, "strategies_with_positions": 1,
		"unowned_positions": 1, "inactive_strategy_positions": 1,
		"positions_missing_stop_loss": 2, "positions_missing_price": 2,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("summary[%q] = %d, want %d (summary=%v)", key, got[key], value, got)
		}
	}
}
