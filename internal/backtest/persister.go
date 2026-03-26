package backtest

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BacktestPersister abstracts the serialization and storage of backtest results.
type BacktestPersister interface {
	// PersistRun encodes the backtest result and stores the resulting run record.
	PersistRun(ctx context.Context, configID uuid.UUID, triggeredAt time.Time, duration time.Duration, result *OrchestratorResult) error
}
