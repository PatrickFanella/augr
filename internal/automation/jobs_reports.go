package automation

import (
	"context"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

const (
	reportTypePaperValidation  = "paper_validation"
	reportTypeWalletIntel      = "wallet_intelligence"
	reportTypeEventCalibration = "event_calibration"
	reportTypeSolverArb        = "solver_arbitrage"
	reportTypeLatencyResearch  = "latency_research"
)

var paperValidationReportSpec = scheduler.ScheduleSpec{
	Type:         scheduler.ScheduleTypeAfterHours,
	Cron:         "0 17 * * 1-5", // 5 PM ET daily, after market close
	SkipWeekends: true,
	SkipHolidays: true,
}

func (o *JobOrchestrator) registerReportJobs() {
	if o.deps.ReportArtifactRepo == nil {
		o.logger.Info("report_jobs: skipped — report artifact repo not configured")
		return
	}
	o.reportWorker = NewReportWorker(reportWorkerDeps{
		StrategyRepo:       o.deps.StrategyRepo,
		BacktestConfigRepo: o.deps.BacktestConfigRepo,
		BacktestRunRepo:    o.deps.BacktestRunRepo,
		ReportArtifactRepo: o.deps.ReportArtifactRepo,
	}, o.logger, o.reportMetrics)
	o.Register(
		"paper_validation_report",
		"Generate paper-trading validation reports for active paper strategies",
		paperValidationReportSpec,
		o.runPaperValidationReport,
	)
}

func (o *JobOrchestrator) runPaperValidationReport(ctx context.Context) error {
	if o.reportWorker == nil {
		return fmt.Errorf("paper_validation_report: worker not configured")
	}
	err := o.reportWorker.RunPaperValidationReport(ctx)
	o.SetLastSummary("paper_validation_report", o.reportWorker.LastSummary())
	return err
}
