package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func (o *JobOrchestrator) registerWeeklyJobs() {
	o.Register("universe_refresh", "Reload universe constituents from Polygon", universeRefreshSpec, o.universeRefresh)
	o.Register("strategy_tournament", "Pit all strategies against each other, prune losers", strategyTournamentSpec, o.strategyTournament)
}

var (
	universeRefreshSpec    = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 12 * * 0", SkipWeekends: false, SkipHolidays: false}
	strategyTournamentSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 14 * * 0", SkipWeekends: false, SkipHolidays: false}
)

// universeRefresh reloads all universe constituents from Polygon.
func (o *JobOrchestrator) universeRefresh(ctx context.Context) error {
	o.logger.Info("universe_refresh: starting")

	if o.deps.Universe == nil {
		o.logger.Info("universe_refresh: skipped — Universe not configured")
		return nil
	}

	count, err := o.deps.Universe.RefreshConstituents(ctx)
	if err != nil {
		return fmt.Errorf("universe_refresh: %w", err)
	}

	o.logger.Info("universe_refresh: completed", slog.Int("tickers_loaded", count))
	return nil
}

// strategyTournament backtests all active strategies over the same
// 1-year period and ranks them by Sharpe ratio.
func (o *JobOrchestrator) strategyTournament(ctx context.Context) error {
	o.logger.Info("strategy_tournament: starting")

	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{Status: "active"}, 100, 0)
	if err != nil {
		return fmt.Errorf("strategy_tournament: list strategies: %w", err)
	}

	now := time.Now()
	histFrom := now.AddDate(-1, 0, 0)

	type ranked struct {
		name   string
		ticker string
		sharpe float64
	}
	var rankings []ranked

	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		rulesConfig, err := extractRulesConfig(strat.Config)
		if err != nil {
			o.logger.Warn("strategy_tournament: bad config",
				slog.String("strategy", strat.Name),
				slog.Any("error", err),
			)
			continue
		}

		barsMap, err := o.deps.DataService.DownloadHistoricalOHLCV(
			ctx, strat.MarketType,
			[]string{strat.Ticker},
			data.Timeframe1d, histFrom, now, true,
		)
		if err != nil {
			o.logger.Warn("strategy_tournament: download failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		bars := barsMap[strat.Ticker]
		if len(bars) < 50 {
			o.logger.Warn("strategy_tournament: insufficient bars",
				slog.String("ticker", strat.Ticker),
				slog.Int("bars", len(bars)),
			)
			continue
		}

		metrics, err := backtestStrategy(ctx, *rulesConfig, strat.Ticker, bars, o.logger)
		if err != nil {
			o.logger.Warn("strategy_tournament: backtest failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		rankings = append(rankings, ranked{
			name:   strat.Name,
			ticker: strat.Ticker,
			sharpe: metrics.SharpeRatio,
		})
	}

	// Sort by Sharpe descending.
	for i := 0; i < len(rankings); i++ {
		for j := i + 1; j < len(rankings); j++ {
			if rankings[j].sharpe > rankings[i].sharpe {
				rankings[i], rankings[j] = rankings[j], rankings[i]
			}
		}
	}

	// Log ranking table.
	for rank, r := range rankings {
		o.logger.Info(fmt.Sprintf("strategy_tournament: #%d %s (%s) sharpe=%.3f",
			rank+1, r.name, r.ticker, r.sharpe),
		)
		if r.sharpe < -0.5 {
			o.logger.Warn("strategy_tournament: consider disabling",
				slog.String("strategy", r.name),
				slog.String("ticker", r.ticker),
				slog.Float64("sharpe", r.sharpe),
			)
		}
	}

	o.logger.Info("strategy_tournament: completed", slog.Int("strategies_ranked", len(rankings)))
	return nil
}

// backtestStrategy runs a single backtest for the given rules config
// and bars, returning the computed metrics.
func backtestStrategy(
	ctx context.Context,
	cfg rules.RulesEngineConfig,
	ticker string,
	bars []domain.OHLCV,
	logger *slog.Logger,
) (*backtest.Metrics, error) {
	startDate := bars[0].Timestamp
	endDate := bars[len(bars)-1].Timestamp
	initialCash := 100_000.0

	pipeline := rules.NewRulesPipeline(
		cfg,
		bars,
		startDate,
		initialCash,
		agent.NoopPersister{},
		nil,
		logger,
	)

	orch, err := backtest.NewOrchestrator(
		backtest.OrchestratorConfig{
			StrategyID:  [16]byte{1},
			Ticker:      ticker,
			StartDate:   startDate,
			EndDate:     endDate,
			InitialCash: initialCash,
			FillConfig: backtest.FillConfig{
				Slippage: backtest.ProportionalSlippage{BasisPoints: 5},
			},
		},
		bars,
		pipeline,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("create orchestrator: %w", err)
	}

	result, err := orch.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("run backtest: %w", err)
	}

	return &result.Metrics, nil
}
