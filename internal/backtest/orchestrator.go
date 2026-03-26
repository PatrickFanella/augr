package backtest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OrchestratorConfig holds the inputs needed to run a complete backtest
// simulation: strategy identification, the date range, and all simulation
// parameters.
type OrchestratorConfig struct {
	// Strategy identification.
	StrategyID uuid.UUID
	Ticker     string

	// Date range for the simulation.
	StartDate time.Time
	EndDate   time.Time

	// Simulation parameters.
	InitialCash float64
	FillConfig  FillConfig
}

// OrchestratorResult aggregates every output produced by a backtest run:
// trades, per-bar results, final positions, the full equity curve, and
// computed performance metrics.
type OrchestratorResult struct {
	Trades         []domain.Trade
	BarResults     []BarResult
	Positions      []TrackedPosition
	EquityCurve    []EquityPoint
	Metrics        Metrics
	TradeAnalytics TradeAnalytics
}

// Orchestrator coordinates all backtest subsystems (data loading, pipeline
// execution, fill simulation, position tracking, and metric computation) into
// a single runnable unit.
type Orchestrator struct {
	config       OrchestratorConfig
	bars         []domain.OHLCV
	pipeline     *agent.Pipeline
	logger       *slog.Logger
	clockTargets []NowFuncSetter
}

// NewOrchestrator validates the configuration and returns a ready-to-run
// Orchestrator. Historical bars must be supplied directly; the caller is
// responsible for loading them from whatever data source is appropriate.
func NewOrchestrator(
	cfg OrchestratorConfig,
	bars []domain.OHLCV,
	pipeline *agent.Pipeline,
	logger *slog.Logger,
	clockTargets ...NowFuncSetter,
) (*Orchestrator, error) {
	if err := validateOrchestratorConfig(cfg); err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("backtest: at least one OHLCV bar is required")
	}
	if pipeline == nil {
		return nil, fmt.Errorf("backtest: pipeline is required")
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Orchestrator{
		config:       cfg,
		bars:         bars,
		pipeline:     pipeline,
		logger:       logger,
		clockTargets: clockTargets,
	}, nil
}

// Run executes the full backtest simulation. It creates the fill engine,
// broker adapter, position tracker, and runner internally, then delegates
// bar-by-bar execution to the Runner. After the run completes, it gathers
// positions, the equity curve, and computes performance metrics.
func (o *Orchestrator) Run(ctx context.Context) (*OrchestratorResult, error) {
	fillEngine, err := NewFillEngine(o.config.FillConfig)
	if err != nil {
		return nil, fmt.Errorf("backtest: creating fill engine: %w", err)
	}

	broker, err := NewBrokerAdapter(o.config.InitialCash, fillEngine)
	if err != nil {
		return nil, fmt.Errorf("backtest: creating broker adapter: %w", err)
	}

	tracker, err := NewPositionTracker(o.config.InitialCash)
	if err != nil {
		return nil, fmt.Errorf("backtest: creating position tracker: %w", err)
	}

	filtered := filterBars(o.bars, o.config.StartDate, o.config.EndDate)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("backtest: no bars in date range %s to %s",
			o.config.StartDate.Format(time.DateOnly),
			o.config.EndDate.Format(time.DateOnly))
	}

	runnerCfg := RunnerConfig{
		StrategyID: o.config.StrategyID,
		Ticker:     o.config.Ticker,
	}

	runner, err := NewRunner(
		runnerCfg,
		filtered,
		o.pipeline,
		broker,
		tracker,
		o.logger,
		o.clockTargets...,
	)
	if err != nil {
		return nil, fmt.Errorf("backtest: creating runner: %w", err)
	}

	o.logger.Info("backtest: starting orchestrated run",
		slog.String("ticker", o.config.Ticker),
		slog.Time("start", o.config.StartDate),
		slog.Time("end", o.config.EndDate),
		slog.Int("bars", len(filtered)),
		slog.Float64("initial_cash", o.config.InitialCash),
	)

	runResult, err := runner.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("backtest: running simulation: %w", err)
	}

	trades := broker.FilledTrades()
	positions := tracker.Positions()
	equityCurve := runResult.EquityCurve
	metrics := ComputeMetrics(equityCurve)
	tradeAnalytics := ComputeTradeAnalytics(trades, o.config.StartDate, o.config.EndDate)

	o.logger.Info("backtest: orchestrated run complete",
		slog.Int("bars_processed", len(runResult.BarResults)),
		slog.Int("trades", len(trades)),
		slog.Int("open_positions", len(positions)),
		slog.Float64("total_return", metrics.TotalReturn),
		slog.Float64("max_drawdown", metrics.MaxDrawdown),
		slog.Float64("sharpe_ratio", metrics.SharpeRatio),
	)

	return &OrchestratorResult{
		Trades:         trades,
		BarResults:     runResult.BarResults,
		Positions:      positions,
		EquityCurve:    equityCurve,
		Metrics:        metrics,
		TradeAnalytics: tradeAnalytics,
	}, nil
}

// filterBars returns bars whose timestamps fall within [start, end] inclusive.
func filterBars(bars []domain.OHLCV, start, end time.Time) []domain.OHLCV {
	filtered := make([]domain.OHLCV, 0, len(bars))
	for _, bar := range bars {
		if !bar.Timestamp.Before(start) && !bar.Timestamp.After(end) {
			filtered = append(filtered, bar)
		}
	}
	return filtered
}

func validateOrchestratorConfig(cfg OrchestratorConfig) error {
	if cfg.StrategyID == uuid.Nil {
		return fmt.Errorf("backtest: strategy ID is required")
	}
	if strings.TrimSpace(cfg.Ticker) == "" {
		return fmt.Errorf("backtest: ticker is required")
	}
	if cfg.StartDate.IsZero() {
		return fmt.Errorf("backtest: start date is required")
	}
	if cfg.EndDate.IsZero() {
		return fmt.Errorf("backtest: end date is required")
	}
	if cfg.EndDate.Before(cfg.StartDate) {
		return fmt.Errorf("backtest: end date must not be before start date")
	}
	if cfg.InitialCash < 0 {
		return fmt.Errorf("backtest: initial cash must be non-negative")
	}
	return nil
}
