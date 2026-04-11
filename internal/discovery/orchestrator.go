package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// DiscoveryConfig holds the full set of parameters for the autonomous discovery pipeline.
type DiscoveryConfig struct {
	Screener     ScreenerConfig
	Generator    GeneratorConfig
	Sweep        SweepConfig // template -- Ticker/Bars filled per candidate
	Scoring      ScoringConfig
	Validation   ValidationConfig
	MaxWinners   int    // max strategies to deploy (default 3)
	DryRun       bool   // don't create strategies, just log
	ScheduleCron string // cron for deployed strategies (default "0 */4 * * *")
}

// DiscoveryDeps bundles external dependencies required by the discovery pipeline.
type DiscoveryDeps struct {
	DataService *data.DataService
	LLMProvider llm.Provider
	Strategies  repository.StrategyRepository
	Logger      *slog.Logger
}

// DeployedStrategy records a strategy that was created in the repository.
type DeployedStrategy struct {
	StrategyID  uuid.UUID               `json:"strategy_id"`
	Ticker      string                  `json:"ticker"`
	Config      rules.RulesEngineConfig `json:"config"`
	InSample    backtest.Metrics        `json:"in_sample"`
	OutOfSample backtest.Metrics        `json:"out_of_sample"`
	Score       float64                 `json:"score"`
}

// DiscoveryResult summarises the pipeline execution.
type DiscoveryResult struct {
	Candidates int                `json:"candidates"`
	Generated  int                `json:"generated"`
	Swept      int                `json:"swept"`
	Validated  int                `json:"validated"`
	Deployed   int                `json:"deployed"`
	Winners    []DeployedStrategy `json:"winners"`
	Duration   time.Duration      `json:"duration"`
	Errors     []string           `json:"errors,omitempty"`
}

// RunDiscovery executes the full autonomous strategy discovery pipeline.
func RunDiscovery(ctx context.Context, cfg DiscoveryConfig, deps DiscoveryDeps) (*DiscoveryResult, error) {
	start := time.Now()
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxWinners == 0 {
		cfg.MaxWinners = 3
	}
	if cfg.ScheduleCron == "" {
		cfg.ScheduleCron = "0 */2 * * 1-5"
	}

	result := &DiscoveryResult{}

	// Step 1: Screen candidates.
	candidates, err := Screen(ctx, deps.DataService, cfg.Screener, logger)
	if err != nil {
		return nil, fmt.Errorf("discovery: screening failed: %w", err)
	}
	result.Candidates = len(candidates)
	logger.Info("discovery: screened candidates",
		slog.Int("candidates", len(candidates)),
		slog.Int("tickers", len(cfg.Screener.Tickers)),
	)

	// Step 2: Generate a strategy config for each candidate.
	type generated struct {
		candidate ScreenResult
		config    rules.RulesEngineConfig
	}
	var generatedConfigs []generated

	generatorCfg := cfg.Generator
	if generatorCfg.Provider == nil {
		generatorCfg.Provider = deps.LLMProvider
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discovery: context cancelled during generation: %w", err)
		}

		rulesConfig, genErr := GenerateStrategy(ctx, generatorCfg, candidate, logger)
		if genErr != nil {
			logger.Warn("discovery: strategy generation failed",
				slog.String("ticker", candidate.Ticker),
				slog.Any("error", genErr),
			)
			result.Errors = append(result.Errors, fmt.Sprintf("generate %s: %v", candidate.Ticker, genErr))
			continue
		}

		generatedConfigs = append(generatedConfigs, generated{
			candidate: candidate,
			config:    *rulesConfig,
		})
	}
	result.Generated = len(generatedConfigs)

	// Step 3: Sweep each generated config and take the best result.
	var allBests []SweepResult
	barsByTicker := make(map[string][]domain.OHLCV)
	for _, gen := range generatedConfigs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discovery: context cancelled during sweep: %w", err)
		}

		sweepCfg := cfg.Sweep
		sweepCfg.Ticker = gen.candidate.Ticker
		sweepCfg.MarketType = cfg.Screener.MarketType

		// Download 5 years of history for backtesting — more data means more
		// trades and more statistically significant results. The first ~1 year
		// serves as indicator warmup (SMA-200 needs 200 bars).
		now := time.Now()
		histFrom := now.AddDate(-5, 0, 0)
		barsMap, dlErr := deps.DataService.DownloadHistoricalOHLCV(
			ctx, cfg.Screener.MarketType,
			[]string{gen.candidate.Ticker},
			data.Timeframe1d, histFrom, now, true,
		)
		if dlErr != nil {
			logger.Warn("discovery: historical download failed",
				slog.String("ticker", gen.candidate.Ticker),
				slog.Any("error", dlErr),
			)
			result.Errors = append(result.Errors, fmt.Sprintf("history %s: %v", gen.candidate.Ticker, dlErr))
			continue
		}
		histBars := barsMap[gen.candidate.Ticker]
		if len(histBars) < 50 {
			logger.Warn("discovery: insufficient historical bars",
				slog.String("ticker", gen.candidate.Ticker),
				slog.Int("bars", len(histBars)),
			)
			continue
		}
		barsByTicker[gen.candidate.Ticker] = histBars
		sweepCfg.Bars = histBars
		sweepCfg.EndDate = histBars[len(histBars)-1].Timestamp
		sweepCfg.StartDate = sweepCfg.EndDate.AddDate(-3, 0, 0)
		if sweepCfg.StartDate.Before(histBars[0].Timestamp) {
			sweepCfg.StartDate = histBars[0].Timestamp
		}

		sweepResults, sweepErr := RunSweep(ctx, gen.config, sweepCfg, cfg.Scoring, logger)
		if sweepErr != nil {
			logger.Warn("discovery: sweep failed",
				slog.String("ticker", gen.candidate.Ticker),
				slog.Any("error", sweepErr),
			)
			result.Errors = append(result.Errors, fmt.Sprintf("sweep %s: %v", gen.candidate.Ticker, sweepErr))
			continue
		}
		if len(sweepResults) > 0 {
			allBests = append(allBests, sweepResults[0])
		}
	}
	result.Swept = len(allBests)

	// Step 4: Filter and rank all best-per-ticker results.
	topScorers := FilterAndRank(allBests, cfg.Scoring, cfg.MaxWinners*2)

	// Step 5: Validate top scorers with walk-forward OOS test.
	type validatedResult struct {
		sweep      SweepResult
		ticker     string
		bars       []domain.OHLCV
		validation *ValidationResult
	}
	var validated []validatedResult

	// barsByTicker already populated during sweep step above.

	// Build a config-name-to-ticker lookup from generated configs.
	tickerByConfigName := make(map[string]string, len(generatedConfigs))
	for _, gen := range generatedConfigs {
		tickerByConfigName[gen.config.Name] = gen.candidate.Ticker
	}

	for _, scorer := range topScorers {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discovery: context cancelled during validation: %w", err)
		}

		ticker := tickerByConfigName[scorer.Config.Name]
		bars := barsByTicker[ticker]
		if len(bars) == 0 {
			continue
		}

		last := bars[len(bars)-1].Timestamp
		valStart := bars[0].Timestamp
		valEnd := last

		valResult, valErr := ValidateOutOfSample(
			ctx, cfg.Validation, bars, scorer.Config,
			valStart, valEnd, cfg.Sweep.InitialCash, logger,
		)
		if valErr != nil {
			logger.Warn("discovery: validation failed",
				slog.String("ticker", ticker),
				slog.Any("error", valErr),
			)
			result.Errors = append(result.Errors, fmt.Sprintf("validate %s: %v", ticker, valErr))
			continue
		}

		if valResult.Passed {
			validated = append(validated, validatedResult{
				sweep:      scorer,
				ticker:     ticker,
				bars:       bars,
				validation: valResult,
			})
		} else {
			logger.Info("discovery: validation did not pass",
				slog.String("ticker", ticker),
				slog.String("reason", valResult.Reason),
			)
		}
	}
	result.Validated = len(validated)

	// Step 6: Deploy top MaxWinners validated results.
	limit := cfg.MaxWinners
	if limit > len(validated) {
		limit = len(validated)
	}

	for _, v := range validated[:limit] {
		configJSON, marshalErr := json.Marshal(map[string]any{
			"rules_engine": v.sweep.Config,
		})
		if marshalErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("marshal config: %v", marshalErr))
			continue
		}

		strategyName := fmt.Sprintf("discovery: %s %s", v.ticker, v.sweep.Config.Name)

		strategy := domain.Strategy{
			ID:           uuid.New(),
			Name:         strategyName,
			Ticker:       v.ticker,
			MarketType:   cfg.Screener.MarketType,
			IsPaper:      true,
			Status:       "active",
			ScheduleCron: cfg.ScheduleCron,
			Config:       json.RawMessage(configJSON),
		}

		if !cfg.DryRun {
			if createErr := deps.Strategies.Create(ctx, &strategy); createErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("deploy %s: %v", strategy.Ticker, createErr))
				continue
			}
		}

		deployed := DeployedStrategy{
			StrategyID:  strategy.ID,
			Ticker:      strategy.Ticker,
			Config:      v.sweep.Config,
			InSample:    v.validation.InSample,
			OutOfSample: v.validation.OutOfSample,
			Score:       v.sweep.Score,
		}
		result.Winners = append(result.Winners, deployed)

		logger.Info("discovery: deployed strategy",
			slog.String("id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
			slog.String("name", strategy.Name),
			slog.Float64("score", v.sweep.Score),
			slog.Bool("dry_run", cfg.DryRun),
		)
	}
	result.Deployed = len(result.Winners)
	result.Duration = time.Since(start)

	logger.Info("discovery: pipeline complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("generated", result.Generated),
		slog.Int("swept", result.Swept),
		slog.Int("validated", result.Validated),
		slog.Int("deployed", result.Deployed),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}
