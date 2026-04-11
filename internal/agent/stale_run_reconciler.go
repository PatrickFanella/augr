package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

const staleRunUpdateTimeout = 10 * time.Second

// StaleRunMetrics captures the single stale-run metric emitted by the reconciler.
type StaleRunMetrics interface {
	RecordStaleRunReconciled()
}

// StaleRunReconcilerConfig defines the stale-run watchdog cadence and clock source.
type StaleRunReconcilerConfig struct {
	TTL      time.Duration
	Interval time.Duration
	Clock    func() time.Time
}

// StaleRunReconciler marks abandoned running pipeline runs as failed.
type StaleRunReconciler struct {
	runs     repository.PipelineRunRepository
	auditLog repository.AuditLogRepository
	registry *RunContextRegistry
	metrics  StaleRunMetrics
	logger   *slog.Logger
	ttl      time.Duration
	interval time.Duration
	clock    func() time.Time
}

// NewStaleRunReconciler constructs a stale-run watchdog.
func NewStaleRunReconciler(
	runs repository.PipelineRunRepository,
	auditLog repository.AuditLogRepository,
	registry *RunContextRegistry,
	metrics StaleRunMetrics,
	logger *slog.Logger,
	cfg StaleRunReconcilerConfig,
) *StaleRunReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	return &StaleRunReconciler{
		runs:     runs,
		auditLog: auditLog,
		registry: registry,
		metrics:  metrics,
		logger:   logger,
		ttl:      cfg.TTL,
		interval: interval,
		clock:    clock,
	}
}

// Start begins the periodic stale-run sweep until ctx is cancelled.
func (r *StaleRunReconciler) Start(ctx context.Context) {
	if r == nil || r.runs == nil || r.ttl <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			if _, err := r.Reconcile(ctx); err != nil {
				r.logger.Warn("stale run reconciler sweep failed", slog.Any("error", err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Reconcile performs one stale-run sweep and returns the number of repaired runs.
func (r *StaleRunReconciler) Reconcile(ctx context.Context) (int, error) {
	if r == nil || r.runs == nil || r.ttl <= 0 {
		return 0, nil
	}
	now := r.clock().UTC()
	cutoff := now.Add(-r.ttl)
	runs, err := r.runs.List(ctx, repository.PipelineRunFilter{
		Status:        domain.PipelineStatusRunning,
		StartedBefore: &cutoff,
	}, 500, 0)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, run := range runs {
		completedAt := now
		updateCtx, cancel := context.WithTimeout(context.Background(), staleRunUpdateTimeout)
		err := r.runs.UpdateStatus(updateCtx, run.ID, run.TradeDate, repository.PipelineRunStatusUpdate{
			Status:       domain.PipelineStatusFailed,
			CompletedAt:  &completedAt,
			ErrorMessage: "stale run: exceeded TTL",
		})
		cancel()
		if err != nil {
			r.logger.Warn("stale run reconciler failed to update run status",
				slog.String("run_id", run.ID.String()),
				slog.Any("error", err),
			)
			continue
		}

		cancelled := r.registry != nil && r.registry.Cancel(run.ID)
		r.writeAuditLog(run, now, cancelled)
		if r.metrics != nil {
			r.metrics.RecordStaleRunReconciled()
		}
		reconciled++
	}
	return reconciled, nil
}

func (r *StaleRunReconciler) writeAuditLog(run domain.PipelineRun, now time.Time, cancelled bool) {
	if r.auditLog == nil {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"reason":            "stale run: exceeded TTL",
		"started_at":        run.StartedAt.UTC(),
		"reconciled_at":     now,
		"stale_for":         now.Sub(run.StartedAt).String(),
		"context_cancelled": cancelled,
	})
	if err != nil {
		r.logger.Warn("stale run reconciler failed to marshal audit details", slog.Any("error", err))
		return
	}
	entityID := run.ID
	entry := &domain.AuditLogEntry{
		ID:         uuid.New(),
		EventType:  "pipeline_run.stale_reconciled",
		EntityType: "pipeline_run",
		EntityID:   &entityID,
		Actor:      "system",
		Details:    raw,
		CreatedAt:  now,
	}
	if err := r.auditLog.Create(context.Background(), entry); err != nil {
		r.logger.Warn("stale run reconciler audit log write failed",
			slog.String("run_id", run.ID.String()),
			slog.Any("error", err),
		)
	}
}
