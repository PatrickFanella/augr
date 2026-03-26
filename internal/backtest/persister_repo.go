package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// RepoPersister implements BacktestPersister by marshaling results to JSON and
// delegating storage to a BacktestRunRepository.
type RepoPersister struct {
	repo repository.BacktestRunRepository
}

// NewRepoPersister constructs a RepoPersister backed by the given repository.
func NewRepoPersister(repo repository.BacktestRunRepository) *RepoPersister {
	return &RepoPersister{repo: repo}
}

// PersistRun encodes the OrchestratorResult fields (metrics, trades, equity
// curve) as JSON and creates a BacktestRun record in the repository.
func (p *RepoPersister) PersistRun(ctx context.Context, configID uuid.UUID, triggeredAt time.Time, duration time.Duration, result *OrchestratorResult) error {
	if result == nil {
		return fmt.Errorf("backtest: result is required")
	}

	metricsJSON, err := json.Marshal(result.Metrics)
	if err != nil {
		return fmt.Errorf("backtest: marshal metrics: %w", err)
	}
	tradeLogJSON, err := json.Marshal(result.Trades)
	if err != nil {
		return fmt.Errorf("backtest: marshal trades: %w", err)
	}
	equityCurveJSON, err := json.Marshal(result.EquityCurve)
	if err != nil {
		return fmt.Errorf("backtest: marshal equity curve: %w", err)
	}

	run := &domain.BacktestRun{
		BacktestConfigID:  configID,
		Metrics:           metricsJSON,
		TradeLog:          tradeLogJSON,
		EquityCurve:       equityCurveJSON,
		RunTimestamp:      triggeredAt,
		Duration:          duration,
		PromptVersion:     result.PromptVersion,
		PromptVersionHash: result.PromptVersionHash,
	}

	return p.repo.Create(ctx, run)
}
