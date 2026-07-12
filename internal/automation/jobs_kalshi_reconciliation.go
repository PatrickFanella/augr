package automation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var kalshiReconcileSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "5,35 * * * *"}

func (o *JobOrchestrator) registerKalshiReconciliationJob() {
	if o.deps.KalshiReconciler == nil {
		return
	}
	o.Register("kalshi_reconcile", "Audit Kalshi broker positions against side-qualified local positions", kalshiReconcileSpec, o.kalshiReconcile)
}

func (o *JobOrchestrator) kalshiReconcile(ctx context.Context) error {
	result, err := o.deps.KalshiReconciler.Check(ctx)
	if err != nil {
		return fmt.Errorf("kalshi_reconcile: %w", err)
	}
	o.SetLastSummary("kalshi_reconcile", map[string]int{"broker_positions": result.BrokerPositions, "local_positions": result.LocalPositions, "matched_positions": result.MatchedPositions, "drifts": result.DriftCount})
	o.logger.Info("kalshi_reconcile: complete", slog.Int("broker_positions", result.BrokerPositions), slog.Int("local_positions", result.LocalPositions), slog.Int("drifts", result.DriftCount))
	return nil
}
