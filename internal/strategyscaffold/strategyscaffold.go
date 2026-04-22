package strategyscaffold

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	discoverypkg "github.com/PatrickFanella/get-rich-quick/internal/discovery"
	optionsdiscovery "github.com/PatrickFanella/get-rich-quick/internal/discovery/options"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	defaultPaperScheduleWeekdays = "0 10 * * 1-5"
	defaultPaperOptionsSchedule  = "0 11,14 * * 1-5"
)

type OptionsBacktestSummary struct {
	Strategy      domain.Strategy
	Metrics       backtest.Metrics
	Trades        []domain.Trade
	EquityCurve   []backtest.EquityPoint
	Validation    *discoverypkg.ValidationResult
	ScaffoldStart time.Time
	ScaffoldEnd   time.Time
}

func StockPaperMovingAverageCrossover(ticker string) (domain.Strategy, error) {
	ticker = normalizeTicker(ticker)
	if ticker == "" {
		return domain.Strategy{}, fmt.Errorf("stock scaffold: ticker is required")
	}

	rulesCfg := rules.RulesEngineConfig{
		Version:     1,
		Name:        "paper-sma-crossover",
		Description: "Paper-trading stock trend scaffold using 20/50 SMA alignment with 200-day trend filter.",
		Entry: rules.ConditionGroup{
			Operator: "AND",
			Conditions: []rules.Condition{
				{Field: "sma_20", Op: "gt", Ref: "sma_50"},
				{Field: "close", Op: "gt", Ref: "sma_200"},
			},
		},
		Exit: rules.ConditionGroup{
			Operator: "OR",
			Conditions: []rules.Condition{
				{Field: "sma_20", Op: "lt", Ref: "sma_50"},
				{Field: "close", Op: "lt", Ref: "sma_200"},
			},
		},
		PositionSizing: rules.SizingConfig{Method: "fixed_fraction", FractionPct: 10},
		StopLoss:       rules.StopLossConfig{Method: "atr_multiple", ATRMultiplier: 2},
		TakeProfit:     rules.TakeProfitConfig{Method: "risk_reward", Ratio: 3},
		Filters:        &rules.FilterConfig{MinVolume: 1_000_000},
	}
	if err := rules.Validate(&rulesCfg); err != nil {
		return domain.Strategy{}, fmt.Errorf("stock scaffold: %w", err)
	}

	config, err := marshalConfig(map[string]any{"rules_engine": rulesCfg})
	if err != nil {
		return domain.Strategy{}, err
	}

	strategy := domain.Strategy{
		ID:           uuid.New(),
		Name:         fmt.Sprintf("paper stock: %s sma trend", ticker),
		Description:  "Rule-based stock paper-trading scaffold for walk-forward backtests and scheduled paper runs.",
		Ticker:       ticker,
		MarketType:   domain.MarketTypeStock,
		ScheduleCron: defaultPaperScheduleWeekdays,
		Config:       config,
		Status:       domain.StrategyStatusActive,
		IsPaper:      true,
	}
	return strategy, strategy.Validate()
}

func OptionsPaperBullPutSpread(ticker string) (domain.Strategy, error) {
	ticker = normalizeTicker(ticker)
	if ticker == "" {
		return domain.Strategy{}, fmt.Errorf("options scaffold: ticker is required")
	}

	optionsCfg := rules.OptionsRulesConfig{
		Version:      1,
		StrategyType: domain.StrategyBullPutSpread,
		Underlying:   ticker,
		Entry: rules.ConditionGroup{
			Operator: "AND",
			Conditions: []rules.Condition{
				{Field: "close", Op: "gt", Ref: "sma_50"},
				{Field: "iv_rank", Op: "gt", Value: float64Ptr(30)},
			},
		},
		Exit: rules.ConditionGroup{
			Operator: "OR",
			Conditions: []rules.Condition{
				{Field: "close", Op: "lt", Ref: "sma_50"},
				{Field: "pnl_pct", Op: "gte", Value: float64Ptr(50)},
			},
		},
		LegSelection: map[string]rules.LegSelector{
			"short_put": {
				OptionType:  domain.OptionTypePut,
				DeltaTarget: 0.25,
				DTEMin:      30,
				DTEMax:      45,
				Side:        domain.OrderSideSell,
				Intent:      domain.PositionIntentSellToOpen,
				Ratio:       1,
			},
			"long_put": {
				OptionType:  domain.OptionTypePut,
				DeltaTarget: 0.10,
				DTEMin:      30,
				DTEMax:      45,
				Side:        domain.OrderSideBuy,
				Intent:      domain.PositionIntentBuyToOpen,
				Ratio:       1,
			},
		},
		PositionSizing: rules.OptionsSizingConfig{Method: "max_risk", MaxRiskUSD: 1000},
		Management:     rules.OptionsManagement{CloseAtProfitPct: 50, CloseAtDTE: 7, StopLossPct: 100},
	}
	if err := rules.ValidateOptions(&optionsCfg); err != nil {
		return domain.Strategy{}, fmt.Errorf("options scaffold: %w", err)
	}

	config, err := marshalConfig(map[string]any{"options_rules": optionsCfg})
	if err != nil {
		return domain.Strategy{}, err
	}

	strategy := domain.Strategy{
		ID:           uuid.New(),
		Name:         fmt.Sprintf("paper options: %s bull put", ticker),
		Description:  "Rule-based options paper-trading scaffold for premium-selling bull put spread validation.",
		Ticker:       ticker,
		MarketType:   domain.MarketTypeOptions,
		ScheduleCron: defaultPaperOptionsSchedule,
		Config:       config,
		Status:       domain.StrategyStatusActive,
		IsPaper:      true,
	}
	return strategy, strategy.Validate()
}

func StockPaperBacktestConfig(strategy domain.Strategy, startDate, endDate time.Time, initialCapital float64) (domain.BacktestConfig, error) {
	if strategy.MarketType != domain.MarketTypeStock {
		return domain.BacktestConfig{}, fmt.Errorf("stock scaffold: strategy market_type must be %q", domain.MarketTypeStock)
	}
	if !endDate.After(startDate) {
		return domain.BacktestConfig{}, fmt.Errorf("stock scaffold: end_date must be after start_date")
	}
	if initialCapital <= 0 {
		return domain.BacktestConfig{}, fmt.Errorf("stock scaffold: initial_capital must be > 0")
	}
	cfg := domain.BacktestConfig{
		ID:          uuid.New(),
		StrategyID:  strategy.ID,
		Name:        fmt.Sprintf("paper backtest: %s stock trend", strategy.Ticker),
		Description: "Reusable backtest config for the stock paper-trading scaffold.",
		StartDate:   startDate,
		EndDate:     endDate,
		Simulation:  domain.BacktestSimulationParameters{InitialCapital: initialCapital, MaxVolumePct: 0.1},
	}
	return cfg, cfg.Validate()
}

func RunOptionsPaperBacktest(ctx context.Context, ticker string, bars []domain.OHLCV, startDate, endDate time.Time, initialCash float64, logger *slog.Logger) (*OptionsBacktestSummary, error) {
	strategy, err := OptionsPaperBullPutSpread(ticker)
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(strategy.Config, &payload); err != nil {
		return nil, fmt.Errorf("options scaffold: parse strategy config: %w", err)
	}
	optCfg, err := rules.ParseOptions(payload["options_rules"])
	if err != nil {
		return nil, fmt.Errorf("options scaffold: parse options_rules: %w", err)
	}
	if optCfg == nil {
		return nil, fmt.Errorf("options scaffold: options_rules config missing")
	}

	return runOptionsPaperBacktest(ctx, strategy, *optCfg, bars, startDate, endDate, initialCash, logger)
}

func RunOptionsPaperBacktestWithConfig(ctx context.Context, optionsCfg rules.OptionsRulesConfig, bars []domain.OHLCV, startDate, endDate time.Time, initialCash float64, logger *slog.Logger) (*OptionsBacktestSummary, error) {
	if err := rules.ValidateOptions(&optionsCfg); err != nil {
		return nil, fmt.Errorf("options scaffold: %w", err)
	}
	strategy, err := OptionsPaperBullPutSpread(optionsCfg.Underlying)
	if err != nil {
		return nil, err
	}
	config, err := marshalConfig(map[string]any{"options_rules": optionsCfg})
	if err != nil {
		return nil, err
	}
	strategy.Config = config
	strategy.Ticker = normalizeTicker(optionsCfg.Underlying)
	strategy.Name = fmt.Sprintf("paper options: %s %s", strategy.Ticker, strings.ReplaceAll(string(optionsCfg.StrategyType), "_", " "))
	return runOptionsPaperBacktest(ctx, strategy, optionsCfg, bars, startDate, endDate, initialCash, logger)
}

func runOptionsPaperBacktest(ctx context.Context, strategy domain.Strategy, optionsCfg rules.OptionsRulesConfig, bars []domain.OHLCV, startDate, endDate time.Time, initialCash float64, logger *slog.Logger) (*OptionsBacktestSummary, error) {
	if len(bars) == 0 {
		return nil, fmt.Errorf("options scaffold: bars are required")
	}
	if !endDate.After(startDate) {
		return nil, fmt.Errorf("options scaffold: end_date must be after start_date")
	}
	if initialCash <= 0 {
		return nil, fmt.Errorf("options scaffold: initial_cash must be > 0")
	}
	if logger == nil {
		logger = slog.Default()
	}

	sweepCfg := optionsdiscovery.OptionsSweepConfig{
		Ticker:      strategy.Ticker,
		Bars:        bars,
		StartDate:   startDate,
		EndDate:     endDate,
		InitialCash: initialCash,
		// Use an explicit positive value so paper backtests do not trigger
		// RunOptionsSweep's Variations <= 0 fallback behavior.
		Variations: 1,
	}
	results, err := optionsdiscovery.RunOptionsSweep(ctx, optionsCfg, sweepCfg, discoverypkg.DefaultScoringConfig(), logger)
	if err != nil {
		return nil, fmt.Errorf("options scaffold: run sweep: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("options scaffold: sweep returned no results")
	}

	validation, err := optionsdiscovery.ValidateOptionsOutOfSample(ctx, discoverypkg.ValidationConfig{CalibrationMonths: 6, TestMonths: 3, MinOOSRatio: 0.5}, bars, results[0].Config, startDate, endDate, initialCash, logger)
	if err != nil {
		return nil, fmt.Errorf("options scaffold: validate out of sample: %w", err)
	}

	return &OptionsBacktestSummary{
		Strategy:      strategy,
		Metrics:       results[0].Metrics,
		Trades:        append([]domain.Trade(nil), results[0].Trades...),
		EquityCurve:   append([]backtest.EquityPoint(nil), results[0].EquityCurve...),
		Validation:    validation,
		ScaffoldStart: startDate,
		ScaffoldEnd:   endDate,
	}, nil
}

func normalizeTicker(ticker string) string {
	return strings.ToUpper(strings.TrimSpace(ticker))
}

func marshalConfig(v any) (domain.StrategyConfig, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("strategy scaffold: marshal config: %w", err)
	}
	return domain.StrategyConfig(payload), nil
}

func float64Ptr(v float64) *float64 {
	return &v
}
