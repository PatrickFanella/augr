package backtest

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
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

// EntryReviewFunc is called when the rules engine triggers a buy signal.
// It receives the trading plan and full pipeline state. Returns true to
// confirm (possibly with modifications), false to veto. On confirm, it
// may set plan fields and return the LLM's holding strategy.
type EntryReviewFunc func(ctx context.Context, plan *agent.TradingPlan, state *agent.PipelineState, bar domain.OHLCV, portfolioCash float64) (confirmed bool, holdingStrategy string)

// ExitReviewFunc is called when the rules engine triggers an exit signal on a
// held position. It receives the open position with full journal history.
// Returns true to confirm exit, false to veto (keep holding).
type ExitReviewFunc func(ctx context.Context, pos *rules.OpenPosition, state *agent.PipelineState, bar domain.OHLCV, portfolioCash float64) (confirmed bool, reasoning string)

// Runner orchestrates the backtest loop: it iterates over historical bars,
// advances the simulated clock, feeds each bar to the BrokerAdapter, executes
// the full analysis pipeline, and records equity snapshots.
type Runner struct {
	config      RunnerConfig
	pipeline    *agent.Pipeline
	broker      *BrokerAdapter
	tracker     *PositionTracker
	replay      *ReplayIterator
	entryReview EntryReviewFunc
	exitReview  ExitReviewFunc
	journal     *rules.TradeJournal
	logger      *slog.Logger
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
		journal:  rules.NewTradeJournal(),
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

		// Check hard stops and trailing stops BEFORE the pipeline runs.
		r.checkMechanicalStops(ctx, bar)

		// Execute the full analysis pipeline for this bar.
		state, pipeErr := r.pipeline.Execute(ctx, r.config.StrategyID, r.config.Ticker)

		// If the pipeline produced a buy/sell signal, process it through
		// the journal-aware order flow.
		if pipeErr == nil && state != nil {
			r.processSignal(ctx, state, bar)
		}

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

// SetEntryReview attaches an LLM review for entry signals.
func (r *Runner) SetEntryReview(fn EntryReviewFunc) { r.entryReview = fn }

// SetExitReview attaches an LLM review for exit signals.
func (r *Runner) SetExitReview(fn ExitReviewFunc) { r.exitReview = fn }

// Journal returns the trade journal for inclusion in results.
func (r *Runner) Journal() *rules.TradeJournal { return r.journal }

// checkMechanicalStops checks hard stop-loss, trailing stop, and take-profit
// levels on open positions. These execute immediately without LLM review.
func (r *Runner) checkMechanicalStops(ctx context.Context, bar domain.OHLCV) {
	pos := r.journal.GetOpen(r.config.Ticker)
	if pos == nil {
		return
	}

	// Update trailing stop
	pos.UpdateTrailingStop(bar.Close)

	// Check take-profit
	if pos.IsTakeProfitHit(bar) {
		exitPrice := pos.TakeProfit
		pos.AddEntry(rules.JournalEntry{
			Type: rules.EventTakeProfit, Timestamp: bar.Timestamp,
			Price: exitPrice, Reasoning: "take-profit level hit",
		})
		r.logger.Info("backtest: take-profit hit",
			slog.String("ticker", r.config.Ticker),
			slog.Float64("level", exitPrice),
		)
		r.executeClose(ctx, exitPrice, bar, "take_profit")
		return
	}

	// Check hard stop and trailing stop
	if hit, reason := pos.IsStopHit(bar); hit {
		exitPrice := pos.HardStopLoss
		if pos.TrailingStopLevel > 0 && bar.Low <= pos.TrailingStopLevel {
			exitPrice = pos.TrailingStopLevel
		}
		pos.AddEntry(rules.JournalEntry{
			Type: rules.EventStopHit, Timestamp: bar.Timestamp,
			Price: exitPrice, Reasoning: reason,
		})
		r.logger.Info("backtest: stop hit",
			slog.String("ticker", r.config.Ticker),
			slog.String("reason", reason),
		)
		r.executeClose(ctx, exitPrice, bar, "stop_hit")
	}
}

// processSignal handles the rules engine output through the journal-aware flow.
func (r *Runner) processSignal(ctx context.Context, state *agent.PipelineState, bar domain.OHLCV) {
	plan := state.TradingPlan
	ticker := r.config.Ticker

	if plan.Action == domain.PipelineSignalBuy && !r.journal.IsHolding(ticker) {
		r.processEntry(ctx, state, plan, bar)
	} else if plan.Action == domain.PipelineSignalSell && r.journal.IsHolding(ticker) {
		r.processExit(ctx, state, bar)
	}
}

func (r *Runner) processEntry(ctx context.Context, state *agent.PipelineState, plan agent.TradingPlan, bar domain.OHLCV) {
	bal, _ := r.broker.GetAccountBalance(context.Background())
	holdingStrategy := ""

	// Entry LLM review
	if r.entryReview != nil {
		confirmed, strategy := r.entryReview(ctx, &plan, state, bar, bal.Cash)
		if !confirmed {
			r.logger.Info("backtest: entry vetoed by reviewer",
				slog.String("ticker", r.config.Ticker),
				slog.Time("bar", bar.Timestamp),
			)
			return
		}
		holdingStrategy = strategy
	}

	// Cap at available cash
	quantity := plan.PositionSize
	if plan.EntryPrice > 0 {
		maxShares := bal.Cash / plan.EntryPrice
		if quantity > maxShares {
			quantity = math.Floor(maxShares)
		}
	}
	if quantity <= 0 {
		return
	}

	// Submit order
	order := &domain.Order{
		ID: uuid.New(), Ticker: r.config.Ticker, Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: quantity,
	}
	if plan.EntryPrice > 0 {
		order.LimitPrice = &plan.EntryPrice
	}
	_, err := r.broker.SubmitOrder(ctx, order)
	if err != nil {
		r.logger.Warn("backtest: entry order failed", slog.Any("error", err))
		return
	}
	if order.Status != domain.OrderStatusFilled || order.FilledAvgPrice == nil {
		return
	}

	fillPrice := *order.FilledAvgPrice

	// Apply trade to tracker
	trade := domain.Trade{
		ID: uuid.New(), Ticker: r.config.Ticker, Side: domain.OrderSideBuy,
		Quantity: order.FilledQuantity, Price: fillPrice,
		ExecutedAt: bar.Timestamp, CreatedAt: bar.Timestamp,
	}
	_ = r.tracker.ApplyTrade(trade)

	// Build indicator snapshot for journal
	indSnap := make(map[string]float64)
	if state.Market != nil {
		for _, ind := range state.Market.Indicators {
			indSnap[ind.Name] = ind.Value
		}
	}

	// Open position in journal
	r.journal.OpenNewPosition(rules.OpenPosition{
		Ticker:          r.config.Ticker,
		Side:            domain.PositionSideLong,
		EntryPrice:      fillPrice,
		EntryDate:       bar.Timestamp,
		Quantity:        order.FilledQuantity,
		HardStopLoss:    plan.StopLoss,
		TakeProfit:      plan.TakeProfit,
		HoldingStrategy: holdingStrategy,
		TrailingStopPct: 0, // TODO: configurable
	})

	// Log entry in journal
	r.journal.GetOpen(r.config.Ticker).AddEntry(rules.JournalEntry{
		Type: rules.EventEntry, Timestamp: bar.Timestamp,
		Signal: domain.PipelineSignalBuy, Verdict: "confirm",
		Confidence: plan.Confidence, Reasoning: plan.Rationale,
		Indicators: indSnap, Price: fillPrice,
	})
}

func (r *Runner) processExit(ctx context.Context, state *agent.PipelineState, bar domain.OHLCV) {
	pos := r.journal.GetOpen(r.config.Ticker)
	if pos == nil {
		return
	}

	// Build indicator snapshot
	indSnap := make(map[string]float64)
	if state.Market != nil {
		for _, ind := range state.Market.Indicators {
			indSnap[ind.Name] = ind.Value
		}
	}

	// Exit LLM review
	if r.exitReview != nil {
		bal, _ := r.broker.GetAccountBalance(context.Background())
		confirmed, reasoning := r.exitReview(ctx, pos, state, bar, bal.Cash)
		if !confirmed {
			pos.AddEntry(rules.JournalEntry{
				Type: rules.EventSignalReview, Timestamp: bar.Timestamp,
				Signal: domain.PipelineSignalSell, Verdict: "veto",
				Reasoning: reasoning, Indicators: indSnap, Price: bar.Close,
			})
			r.logger.Info("backtest: exit vetoed by reviewer",
				slog.String("ticker", r.config.Ticker),
				slog.String("reasoning", reasoning),
			)
			return
		}
		pos.AddEntry(rules.JournalEntry{
			Type: rules.EventSignalReview, Timestamp: bar.Timestamp,
			Signal: domain.PipelineSignalSell, Verdict: "confirm",
			Reasoning: reasoning, Indicators: indSnap, Price: bar.Close,
		})
	}

	r.executeClose(ctx, bar.Close, bar, "signal_confirmed")
}

func (r *Runner) executeClose(ctx context.Context, exitPrice float64, bar domain.OHLCV, exitReason string) {
	pos := r.journal.GetOpen(r.config.Ticker)
	if pos == nil {
		return
	}

	order := &domain.Order{
		ID: uuid.New(), Ticker: r.config.Ticker, Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: pos.Quantity,
	}
	order.LimitPrice = &exitPrice

	_, err := r.broker.SubmitOrder(ctx, order)
	if err != nil {
		r.logger.Warn("backtest: exit order failed", slog.Any("error", err))
		return
	}
	if order.Status == domain.OrderStatusFilled && order.FilledAvgPrice != nil {
		trade := domain.Trade{
			ID: uuid.New(), Ticker: r.config.Ticker, Side: domain.OrderSideSell,
			Quantity: order.FilledQuantity, Price: *order.FilledAvgPrice,
			ExecutedAt: bar.Timestamp, CreatedAt: bar.Timestamp,
		}
		_ = r.tracker.ApplyTrade(trade)
	}

	r.journal.ClosePosition(r.config.Ticker, exitPrice, bar.Timestamp, exitReason)
}
