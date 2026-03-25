package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

const defaultPollInterval = 30 * time.Second

// riskMonitor polls the risk engine's kill switch and cancels the given context
// when the kill switch becomes active.
type riskMonitor struct {
	riskEngine   risk.RiskEngine
	pollInterval time.Duration
	logger       *slog.Logger
}

// monitorContext wraps the parent context with kill-switch polling. It returns
// a derived context that will be cancelled if the kill switch activates, and a
// cancel function that must be called to stop the monitor goroutine.
func (m *riskMonitor) monitorContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	interval := m.pollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				active, err := m.riskEngine.IsKillSwitchActive(ctx)
				if err != nil {
					m.logger.Warn("scheduler: failed to poll kill switch",
						slog.Any("error", err),
					)
					continue
				}
				if active {
					m.logger.Warn("scheduler: kill switch activated during pipeline execution; cancelling")
					cancel()
					return
				}
			}
		}
	}()

	return ctx, cancel
}
