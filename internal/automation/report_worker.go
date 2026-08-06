package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/papervalidation"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

// ReportArtifactWriter persists report artifacts.
type ReportArtifactWriter interface {
	Upsert(context.Context, *pgrepo.ReportArtifact) error
}

type reportWorkerDeps struct {
	StrategyRepo       repository.StrategyRepository
	BacktestConfigRepo repository.BacktestConfigRepository
	BacktestRunRepo    repository.BacktestRunRepository
	ReportArtifactRepo ReportArtifactWriter
}

// ReportWorker owns paper validation report generation and persistence.
type ReportWorker struct {
	deps        reportWorkerDeps
	logger      *slog.Logger
	metrics     ReportWorkerMetrics
	now         func() time.Time
	lastSummary map[string]int
}

// NewReportWorker constructs a report worker with safe defaults.
func NewReportWorker(deps reportWorkerDeps, logger *slog.Logger, metrics ReportWorkerMetrics) *ReportWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReportWorker{
		deps:    deps,
		logger:  logger,
		metrics: metrics,
		now:     time.Now,
	}
}

func (w *ReportWorker) LastSummary() map[string]int {
	return cloneSummary(w.lastSummary)
}

// RunPaperValidationReport generates a report for each active paper strategy.
func (w *ReportWorker) RunPaperValidationReport(ctx context.Context) error {
	if w.deps.StrategyRepo == nil {
		return fmt.Errorf("paper_validation_report: strategy repo not configured")
	}
	if w.now == nil {
		w.now = time.Now
	}

	w.logger.Info("paper_validation_report: starting")

	strategies, err := listAllStrategies(ctx, w.deps.StrategyRepo, repository.StrategyFilter{Status: domain.StrategyStatusActive})
	if err != nil {
		return fmt.Errorf("paper_validation_report: list strategies: %w", err)
	}

	type paperEntry struct {
		ID   uuid.UUID
		Name string
	}
	var paperStrategies []paperEntry
	for _, s := range strategies {
		if s.IsPaper && !eventmarkets.IsEventMarket(s.MarketType) {
			paperStrategies = append(paperStrategies, paperEntry{ID: s.ID, Name: s.Name})
		}
	}
	skipped := len(strategies) - len(paperStrategies)
	if len(paperStrategies) == 0 {
		w.lastSummary = map[string]int{"scanned": len(strategies), "eligible": 0, "skipped": skipped, "succeeded": 0, "failed": 0}
		w.logger.Info("paper_validation_report: no active paper strategies")
		return nil
	}

	w.logger.Info("paper_validation_report: processing", slog.Int("strategies", len(paperStrategies)))

	now := w.now().UTC()
	timeBucket := now.Truncate(24 * time.Hour)
	var succeeded, failed int

	for _, ps := range paperStrategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := w.generateOneReport(ctx, ps.ID, ps.Name, timeBucket, now); err != nil {
			failed++
			if w.metrics != nil {
				w.metrics.RecordReportWorkerError(ps.ID.String())
			}
			w.logger.Warn("paper_validation_report: strategy failed",
				slog.String("strategy", ps.Name),
				slog.Any("error", err),
			)
		} else {
			succeeded++
			if w.metrics != nil {
				w.metrics.RecordReportWorkerSuccess(ps.ID.String())
			}
		}
	}

	w.logger.Info("paper_validation_report: completed",
		slog.Int("succeeded", succeeded),
		slog.Int("failed", failed),
	)
	w.lastSummary = map[string]int{"scanned": len(strategies), "eligible": len(paperStrategies), "skipped": skipped, "succeeded": succeeded, "failed": failed}
	if failed > 0 {
		return fmt.Errorf("paper_validation_report: %d of %d eligible strategies failed", failed, len(paperStrategies))
	}
	return nil
}

func (w *ReportWorker) generateOneReport(
	ctx context.Context,
	strategyID uuid.UUID,
	strategyName string,
	timeBucket, now time.Time,
) error {
	start := time.Now()

	strategy, err := w.deps.StrategyRepo.Get(ctx, strategyID)
	if err != nil {
		return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("get strategy: %w", err))
	}
	paperStart := strategy.CreatedAt

	if w.deps.BacktestConfigRepo == nil || w.deps.BacktestRunRepo == nil {
		return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("backtest repos not configured"))
	}

	configs, err := w.deps.BacktestConfigRepo.List(ctx, repository.BacktestConfigFilter{StrategyID: &strategyID}, 1, 0)
	if err != nil {
		return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("list backtest configs: %w", err))
	}
	if len(configs) == 0 {
		return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("no backtest configs found for strategy %s", strategyName))
	}

	configID := configs[0].ID
	runs, err := w.deps.BacktestRunRepo.List(ctx, repository.BacktestRunFilter{BacktestConfigID: &configID}, 1, 0)
	if err != nil {
		return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("list backtest runs: %w", err))
	}

	var btMetrics backtest.Metrics
	var analytics backtest.TradeAnalytics
	if len(runs) > 0 {
		latestRun := runs[0]
		if err := json.Unmarshal(latestRun.Metrics, &btMetrics); err != nil {
			return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("unmarshal metrics: %w", err))
		}
		if len(latestRun.TradeLog) > 0 {
			var trades []domain.Trade
			if err := json.Unmarshal(latestRun.TradeLog, &trades); err != nil {
				return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("unmarshal trade log: %w", err))
			}
			analytics = backtest.ComputeTradeAnalytics(trades, btMetrics.StartTime, btMetrics.EndTime)
		} else {
			analytics = backtest.ComputeTradeAnalytics(nil, btMetrics.StartTime, btMetrics.EndTime)
		}
	} else {
		w.logger.Info("paper_validation_report: no backtest runs yet, generating pending report",
			slog.String("strategy", strategyName),
		)
	}

	report := papervalidation.GenerateReport(btMetrics, analytics, papervalidation.DefaultThresholds(), paperStart, now)

	reportJSON, err := json.Marshal(report)
	if err != nil {
		return w.persistErrorArtifact(ctx, strategyID, timeBucket, fmt.Errorf("marshal report: %w", err))
	}

	latencyMs := int(time.Since(start).Milliseconds())
	completed := time.Now().UTC()
	artifact := &pgrepo.ReportArtifact{
		StrategyID:  strategyID,
		ReportType:  reportTypePaperValidation,
		TimeBucket:  timeBucket,
		Status:      "completed",
		ReportJSON:  reportJSON,
		LatencyMs:   latencyMs,
		CompletedAt: &completed,
	}
	if w.deps.ReportArtifactRepo == nil {
		return fmt.Errorf("persist report: report artifact repo not configured")
	}
	if err := w.deps.ReportArtifactRepo.Upsert(ctx, artifact); err != nil {
		return fmt.Errorf("persist report: %w", err)
	}

	w.logger.Info("paper_validation_report: generated",
		slog.String("strategy", strategyName),
		slog.String("decision", report.Decision),
		slog.Int("latency_ms", latencyMs),
	)
	return nil
}

func (w *ReportWorker) persistErrorArtifact(
	ctx context.Context,
	strategyID uuid.UUID,
	timeBucket time.Time,
	origErr error,
) error {
	if w.deps.ReportArtifactRepo == nil {
		w.logger.Error("paper_validation_report: cannot persist error artifact (repo nil)", slog.Any("original_error", origErr))
		return origErr
	}
	completed := time.Now().UTC()
	artifact := &pgrepo.ReportArtifact{
		StrategyID:   strategyID,
		ReportType:   reportTypePaperValidation,
		TimeBucket:   timeBucket,
		Status:       "error",
		ErrorMessage: origErr.Error(),
		CompletedAt:  &completed,
	}
	if err := w.deps.ReportArtifactRepo.Upsert(ctx, artifact); err != nil {
		w.logger.Error("paper_validation_report: failed to persist error artifact",
			slog.Any("original_error", origErr),
			slog.Any("persist_error", err),
		)
	}
	return origErr
}
