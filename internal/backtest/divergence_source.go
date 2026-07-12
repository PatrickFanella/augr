package backtest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// RepositoryDivergenceSource compares the latest reproducible backtest with
// persisted paper/live decisions and closed positions for the same strategy.
type RepositoryDivergenceSource struct {
	configs   repository.BacktestConfigRepository
	runs      repository.BacktestRunRepository
	decisions repository.TradeDecisionJournalRepository
	positions repository.PositionRepository
}

func NewRepositoryDivergenceSource(configs repository.BacktestConfigRepository, runs repository.BacktestRunRepository, decisions repository.TradeDecisionJournalRepository, positions repository.PositionRepository) *RepositoryDivergenceSource {
	return &RepositoryDivergenceSource{configs: configs, runs: runs, decisions: decisions, positions: positions}
}

func (s *RepositoryDivergenceSource) DivergenceFor(ctx context.Context, strategyID string) (Divergence, error) {
	id, err := uuid.Parse(strategyID)
	if err != nil {
		return Divergence{}, fmt.Errorf("divergence: invalid strategy id: %w", err)
	}
	if s == nil || s.configs == nil || s.runs == nil || s.decisions == nil || s.positions == nil {
		return Divergence{}, ErrDivergenceNotFound
	}
	configs, err := s.configs.List(ctx, repository.BacktestConfigFilter{StrategyID: &id}, 1, 0)
	if err != nil || len(configs) == 0 {
		return Divergence{}, ErrDivergenceNotFound
	}
	runs, err := s.runs.List(ctx, repository.BacktestRunFilter{BacktestConfigID: &configs[0].ID}, 1, 0)
	if err != nil || len(runs) == 0 {
		return Divergence{}, ErrDivergenceNotFound
	}
	var metrics Metrics
	if err := json.Unmarshal(runs[0].Metrics, &metrics); err != nil {
		return Divergence{}, fmt.Errorf("divergence: decode backtest metrics: %w", err)
	}

	decisions, err := s.decisions.List(ctx, repository.TradeDecisionFilter{StrategyID: &id}, 10000, 0)
	if err != nil {
		return Divergence{}, fmt.Errorf("divergence: list decisions: %w", err)
	}
	positions, err := s.positions.GetByStrategy(ctx, id, repository.PositionFilter{}, 10000, 0)
	if err != nil {
		return Divergence{}, fmt.Errorf("divergence: list positions: %w", err)
	}
	return buildRepositoryDivergence(id, metrics, decisions, positions), nil
}

func buildRepositoryDivergence(id uuid.UUID, metrics Metrics, decisions []domain.TradeDecision, positions []domain.Position) Divergence {
	attempts, fills := len(decisions), 0
	for _, d := range decisions {
		if d.PaperOrderID != nil || d.LiveOrderID != nil {
			fills++
		}
	}
	closed, wins := 0, 0
	for _, p := range positions {
		if p.ClosedAt != nil {
			closed++
			if p.RealizedPnL > 0 {
				wins++
			}
		}
	}
	live := SidedMetrics{Samples: attempts}
	if attempts > 0 {
		live.FillRate = float64(fills) / float64(attempts)
	}
	if closed > 0 {
		live.WinRate = float64(wins) / float64(closed)
	}
	backtest := SidedMetrics{FillRate: metrics.FillRate, WinRate: metrics.WinRate, Samples: metrics.OrderAttempts}
	if backtest.Samples == 0 {
		backtest.Samples = metrics.TotalBars
	}
	return Divergence{StrategyID: id.String(), Backtest: backtest, Live: live}
}

var _ interface {
	DivergenceFor(context.Context, string) (Divergence, error)
} = (*RepositoryDivergenceSource)(nil)
