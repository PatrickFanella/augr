package backtest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// NowFuncSetter is implemented by components that support simulated-clock
// injection (e.g. DataService, OrderManager, RiskEngineImpl).
type NowFuncSetter interface {
	SetNowFunc(func() time.Time)
}

// RunnerConfig holds the parameters for a single backtest run.
type RunnerConfig struct {
	StrategyID uuid.UUID
	Ticker     string
}

// BarResult captures the outcome of processing a single bar through the
// analysis pipeline.
type BarResult struct {
	Bar   domain.OHLCV
	State *agent.PipelineState
	Err   error
}

// RunResult captures the full backtest outcome including per-bar results, the
// aggregated equity curve, and drawdown overlay data derived from that curve.
type RunResult struct {
	BarResults        []BarResult
	EquityCurve       []EquityPoint
	EquityCurveReport EquityCurveReport
}

// Runner orchestrates the backtest loop: it iterates over historical bars,
// advances the simulated clock, feeds each bar to the BrokerAdapter, executes
// the full analysis pipeline, and records equity snapshots.
type Runner struct {
	config   RunnerConfig
	pipeline *agent.Pipeline
	broker   *BrokerAdapter
	tracker  *PositionTracker
	replay   *ReplayIterator
	logger   *slog.Logger
}

// NewRunner constructs a Runner from the given configuration, historical bars,
// and pre-wired pipeline components. The simulated clock derived from the
// ReplayIterator is injected into the pipeline and every supplied
// NowFuncSetter target so that all stages observe the same simulated time.
func NewRunner(
	cfg RunnerConfig,
	bars []domain.OHLCV,
	pipeline *agent.Pipeline,
	broker *BrokerAdapter,
	tracker *PositionTracker,
	logger *slog.Logger,
	clockTargets ...NowFuncSetter,
) (*Runner, error) {
	if pipeline == nil {
		return nil, fmt.Errorf("backtest: pipeline is required")
	}
	if broker == nil {
		return nil, fmt.Errorf("backtest: broker adapter is required")
	}
	if tracker == nil {
		return nil, fmt.Errorf("backtest: position tracker is required")
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("backtest: at least one OHLCV bar is required")
	}

	replay, err := NewReplayIterator(bars)
	if err != nil {
		return nil, fmt.Errorf("backtest: creating replay iterator: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	// Wire the simulated clock to all components.
	clock := replay.Clock()
	nowFunc := clock.Now
	pipeline.SetNowFunc(nowFunc)
	for _, target := range clockTargets {
		if target != nil {
			target.SetNowFunc(nowFunc)
		}
	}

	return &Runner{
		config:   cfg,
		pipeline: pipeline,
		broker:   broker,
		tracker:  tracker,
		replay:   replay,
		logger:   logger,
	}, nil
}

// Run executes the backtest: for each bar it advances the simulated clock, sets
// the market bar on the BrokerAdapter (triggering resting-order processing),
// runs the full analysis pipeline, and records an equity snapshot. Pipeline
// errors are captured per-bar and do not halt the run; context cancellation or
// infrastructure errors do.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	result := &RunResult{}

	for {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("backtest: context cancelled: %w", err)
		}

		if !r.replay.Next() {
			break
		}

		bar, err := r.replay.Current()
		if err != nil {
			return result, fmt.Errorf("backtest: reading current bar: %w", err)
		}

		r.logger.Debug("backtest: processing bar",
			slog.String("ticker", r.config.Ticker),
			slog.Time("timestamp", bar.Timestamp),
			slog.Float64("close", bar.Close),
			slog.Int("remaining", r.replay.Remaining()),
		)

		// Feed the latest bar to the broker so resting orders are evaluated
		// and mark-to-market equity is recalculated.
		if err := r.broker.SetMarketBar(r.config.Ticker, bar); err != nil {
			return result, fmt.Errorf("backtest: setting market bar: %w", err)
		}

		// Execute the full analysis pipeline for this bar.
		state, pipeErr := r.pipeline.Execute(ctx, r.config.StrategyID, r.config.Ticker)

		// Update position tracker marks to the latest bar close before
		// recording an equity snapshot so unrealized P&L is accurate.
		if err := r.tracker.UpdateMarketPrice(r.config.Ticker, bar.Close); err != nil {
			return result, fmt.Errorf("backtest: updating position tracker: %w", err)
		}

		// Record an equity snapshot after the pipeline (and any resulting
		// order processing) has completed.
		r.tracker.RecordEquity(bar.Timestamp)

		result.BarResults = append(result.BarResults, BarResult{
			Bar:   bar,
			State: state,
			Err:   pipeErr,
		})
	}

	result.EquityCurve = r.tracker.EquityCurve()
	result.EquityCurveReport = GenerateEquityCurveReport(result.EquityCurve)
	return result, nil
}
