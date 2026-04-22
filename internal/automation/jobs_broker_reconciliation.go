package automation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var alpacaReconcileSpec = scheduler.ScheduleSpec{
	Type: scheduler.ScheduleTypeCron,
	Cron: "*/5 * * * *",
}

func (o *JobOrchestrator) registerBrokerReconciliationJobs() {
	o.Register(
		"alpaca_reconcile",
		"Reconcile Alpaca broker positions, orders, and fills into local state",
		alpacaReconcileSpec,
		o.alpacaReconcile,
	)
}

func (o *JobOrchestrator) alpacaReconcile(ctx context.Context) error {
	if o.deps.AlpacaReconciler == nil {
		o.logger.Info("alpaca_reconcile: skipped — reconciler not configured")
		if o.metrics != nil {
			o.metrics.RecordAlpacaReconcileRun("skipped")
		}
		return nil
	}

	o.logger.Info("alpaca_reconcile: starting")
	summary, err := o.deps.AlpacaReconciler.Reconcile(ctx)
	if err != nil {
		if o.metrics != nil {
			o.metrics.RecordAlpacaReconcileRun("error")
		}
		return fmt.Errorf("alpaca_reconcile: %w", err)
	}
	o.SetLastSummary("alpaca_reconcile", summary.Map())
	if o.metrics != nil {
		o.metrics.RecordAlpacaReconcileRun("success")
	}

	o.logger.Info("alpaca_reconcile: complete",
		slog.Int("orders_created", summary.OrdersCreated),
		slog.Int("orders_updated", summary.OrdersUpdated),
		slog.Int("positions_created", summary.PositionsCreated),
		slog.Int("positions_updated", summary.PositionsUpdated),
		slog.Int("trades_created", summary.TradesCreated),
	)
	return nil
}
