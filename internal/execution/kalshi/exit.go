package kalshi

import (
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
)

const (
	defaultTakeProfitPct   = 0.25
	defaultStopLossPct     = 0.20
	defaultExitBeforeClose = 30 * time.Minute
)

// EvaluateExit returns a deterministic full-position exit when an open Kalshi
// position reaches its profit target, stop loss, or close-time guard.
func EvaluateExit(strategy domain.Strategy, snapshot Snapshot, positions []domain.Position, now time.Time) (NativeDecision, bool) {
	meta, ok := parseDiscoveryMeta(strategy.Config)
	if !ok || !meta.AutoExitsEnabled {
		return NativeDecision{}, false
	}
	side := prediction.NormalizeOutcomeSide(meta.Direction)
	if side == "" || normalizeStatus(snapshot.Status) != "active" {
		return NativeDecision{}, false
	}
	exitPrice, ok := snapshot.ExitPriceForSide(side)
	if !ok || exitPrice <= 0 || exitPrice > 1 {
		return NativeDecision{}, false
	}

	positionTicker := strings.ToUpper(strings.TrimSpace(snapshot.Ticker)) + ":" + side
	var quantity, cost float64
	for _, position := range positions {
		if position.ClosedAt == nil && position.Quantity > 0 && strings.EqualFold(strings.TrimSpace(position.Ticker), positionTicker) {
			quantity += position.Quantity
			cost += position.Quantity * position.AvgEntry
		}
	}
	if quantity <= 0 || cost <= 0 {
		return NativeDecision{}, false
	}
	avgEntry := cost / quantity
	takeProfitPct := positiveOr(meta.TakeProfitPct, defaultTakeProfitPct)
	stopLossPct := positiveOr(meta.StopLossPct, defaultStopLossPct)
	exitBeforeClose := defaultExitBeforeClose
	if meta.ExitBeforeCloseMinutes > 0 {
		exitBeforeClose = time.Duration(meta.ExitBeforeCloseMinutes) * time.Minute
	}

	reason := ""
	switch {
	case !snapshot.CloseTime.IsZero() && !snapshot.CloseTime.After(now.Add(exitBeforeClose)):
		reason = "close_time_guard"
	case exitPrice >= avgEntry*(1+takeProfitPct):
		reason = "take_profit"
	case exitPrice <= avgEntry*(1-stopLossPct):
		reason = "stop_loss"
	default:
		return NativeDecision{}, false
	}

	return NativeDecision{
		Signal:         domain.PipelineSignalSell,
		Action:         "exit",
		Side:           side,
		EntryType:      defaultEntryType,
		EntryPrice:     exitPrice,
		Confidence:     meta.confidence(),
		TimeHorizon:    normalizedTimeHorizon(meta.TimeHorizon),
		Reason:         fmt.Sprintf("kalshi deterministic exit: %s", reason),
		Rationale:      fmt.Sprintf("kalshi deterministic exit: %s", reason),
		Template:       normalizeTemplate(meta.Template),
		GateResults:    []string{"owned_position", "executable_bid", reason},
		PositionSize:   quantity,
		AverageEntry:   avgEntry,
		RealizedPnLPct: (exitPrice - avgEntry) / avgEntry,
	}, true
}

func positiveOr(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}
