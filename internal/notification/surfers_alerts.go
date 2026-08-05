package notification

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	NtfyPriorityHigh = "high"
	NtfyPriorityMax  = "max"
)

type SurfersAlerter struct {
	notifier                Notifier
	logger                  *slog.Logger
	DrawdownDollars         float64
	FillRateDropFraction    float64
	GhostFillsPerHour       int
	ConsecutiveLossTripCRIT bool
	RecorderLagSeconds      float64
}

func (a *SurfersAlerter) OnDrawdown(ctx context.Context, scope string, pnl float64) error {
	return a.send(ctx, SeverityCritical, "drawdown", fmt.Sprintf("%s drawdown %.2f", scope, pnl), NtfyPriorityMax)
}

func (a *SurfersAlerter) OnFillRateDrop(ctx context.Context, strategy string, current, baseline float64) error {
	return a.send(ctx, SeverityWarning, "fill_rate_drop", fmt.Sprintf("%s fill rate %.2f vs baseline %.2f", strategy, current, baseline), NtfyPriorityHigh)
}

func (a *SurfersAlerter) OnGhostFill(ctx context.Context, strategy string, observedPerHour int) error {
	return a.send(ctx, SeverityWarning, "ghost_fill", fmt.Sprintf("%s ghost fills %d/hr", strategy, observedPerHour), NtfyPriorityHigh)
}

func (a *SurfersAlerter) OnConsecutiveLossTrip(ctx context.Context, strategy string, losses int) error {
	return a.send(ctx, SeverityCritical, "consecutive_loss_trip", fmt.Sprintf("%s consecutive losses %d", strategy, losses), NtfyPriorityMax)
}

func (a *SurfersAlerter) OnRecorderLag(ctx context.Context, lagSeconds float64) error {
	return a.send(ctx, SeverityWarning, "recorder_lag", fmt.Sprintf("recorder lag %.1fs", lagSeconds), NtfyPriorityHigh)
}

func (a *SurfersAlerter) send(ctx context.Context, sev Severity, key, body, priority string) error {
	if a == nil || a.notifier == nil {
		return nil
	}
	_ = a.logger
	return a.notifier.Notify(ctx, Alert{Key: key, Title: "Surfers alert", Body: body, Severity: sev, OccurredAt: time.Now().UTC(), Metadata: map[string]string{"ntfy_priority": priority}})
}
