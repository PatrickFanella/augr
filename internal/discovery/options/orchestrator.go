package options

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// OptionsDiscoveryConfig controls the full options discovery pipeline.
type OptionsDiscoveryConfig struct {
	Screener     OptionsScreenerConfig
	Scoring      OptionsScoringConfig
	Generator    discovery.GeneratorConfig
	BacktestCfg  discovery.ScoringConfig // reuse stock scoring thresholds
	Validation   discovery.ValidationConfig
	MaxWinners   int
	DryRun       bool
	ScheduleCron string
}

// OptionsDiscoveryDeps holds dependencies for the options pipeline.
type OptionsDiscoveryDeps struct {
	DataService     *data.DataService
	OptionsProvider data.OptionsDataProvider
	LLMProvider     interface {
		Complete(context.Context, interface{}) (interface{}, error)
	} // unused — use Generator
	Strategies repository.StrategyRepository
	Logger     *slog.Logger
}

// OptionsDeployedStrategy is a winner that was deployed.
type OptionsDeployedStrategy struct {
	StrategyID  uuid.UUID
	Ticker      string
	Config      rules.OptionsRulesConfig
	InSample    backtest.Metrics
	OutOfSample backtest.Metrics
	Score       float64
}

// OptionsDiscoveryResult summarises the pipeline run.
type OptionsDiscoveryResult struct {
	Candidates int
	Scored     int
	Generated  int
	Swept      int
	Validated  int
	Deployed   int
	Winners    []OptionsDeployedStrategy
	Duration   time.Duration
	Errors     []string
}

// RunOptionsDiscovery executes the full options discovery pipeline:
// Screen → Score → Generate → Sweep → Validate → Deploy.
func RunOptionsDiscovery(ctx context.Context, cfg OptionsDiscoveryConfig, deps OptionsDiscoveryDeps) (*OptionsDiscoveryResult, error) {
	start := time.Now()
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	result := &OptionsDiscoveryResult{}

	if cfg.MaxWinners <= 0 {
		cfg.MaxWinners = 3
	}
	if cfg.BacktestCfg.MinSharpe == 0 {
		cfg.BacktestCfg = discovery.DefaultScoringConfig()
	}

	// Stage 1: Screen.
	logger.Info("options/discovery: screening candidates")
	candidates, err := ScreenOptions(ctx, deps.DataService, deps.OptionsProvider, cfg.Screener, logger)
	if err != nil {
		return nil, fmt.Errorf("options/discovery: screen: %w", err)
	}
	result.Candidates = len(candidates)
	if len(candidates) == 0 {
		logger.Info("options/discovery: no candidates passed screening")
		result.Duration = time.Since(start)
		return result, nil
	}

	// Stage 2: Score.
	logger.Info("options/discovery: scoring candidates", slog.Int("candidates", len(candidates)))
	scored := ScoreOptionsCandidates(candidates, cfg.Scoring)
	result.Scored = len(scored)

	// Take top 2x max winners for generation.
	limit := cfg.MaxWinners * 2
	if limit > len(scored) {
		limit = len(scored)
	}
	scored = scored[:limit]

	// Stage 3: Generate + Sweep + Validate per candidate.
	type sweepWinner struct {
		ticker    string
		config    rules.OptionsRulesConfig
		metrics   backtest.Metrics
		score     float64
		bars      []domain.OHLCV
		inSample  backtest.Metrics
		oosSample backtest.Metrics
		validated bool
	}
	var winners []sweepWinner

	now := time.Now()
	histFrom := now.AddDate(-3, 0, 0) // 3 years of history

	for _, candidate := range scored {
		if ctx.Err() != nil {
			break
		}

		// Generate.
		optConfig, genErr := GenerateOptionsStrategy(ctx, cfg.Generator, candidate, logger)
		if genErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("generate %s: %v", candidate.Ticker, genErr))
			continue
		}
		result.Generated++

		// Download history for backtesting.
		barsMap, dlErr := deps.DataService.DownloadHistoricalOHLCV(
			ctx, domain.MarketTypeStock,
			[]string{candidate.Ticker},
			data.Timeframe1d, histFrom, now, true,
		)
		if dlErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("download %s: %v", candidate.Ticker, dlErr))
			continue
		}
		bars := barsMap[candidate.Ticker]
		if len(bars) < 100 {
			result.Errors = append(result.Errors, fmt.Sprintf("insufficient bars for %s: %d", candidate.Ticker, len(bars)))
			continue
		}

		// Sweep.
		sweepStart := now.AddDate(-2, 0, 0)
		sweepEnd := now.AddDate(0, -3, 0) // leave 3 months for validation
		sweepCfg := OptionsSweepConfig{
			Ticker:      candidate.Ticker,
			Bars:        bars,
			StartDate:   sweepStart,
			EndDate:     sweepEnd,
			InitialCash: 100_000,
			Variations:  20,
		}

		sweepResults, sweepErr := RunOptionsSweep(ctx, *optConfig, sweepCfg, cfg.BacktestCfg, logger)
		if sweepErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("sweep %s: %v", candidate.Ticker, sweepErr))
			continue
		}
		result.Swept++

		// Take best result.
		if len(sweepResults) == 0 || math.IsInf(sweepResults[0].Score, -1) {
			continue
		}
		best := sweepResults[0]

		// Validate.
		valStart := sweepEnd
		valEnd := now
		valResult, valErr := ValidateOptionsOutOfSample(ctx, cfg.Validation, bars, best.Config, valStart, valEnd, 100_000, logger)
		if valErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("validate %s: %v", candidate.Ticker, valErr))
			continue
		}

		if !valResult.Passed {
			logger.Info("options/discovery: validation failed",
				slog.String("ticker", candidate.Ticker),
				slog.String("reason", valResult.Reason),
			)
			continue
		}
		result.Validated++

		winners = append(winners, sweepWinner{
			ticker:    candidate.Ticker,
			config:    best.Config,
			metrics:   best.Metrics,
			score:     best.Score,
			bars:      bars,
			inSample:  valResult.InSample,
			oosSample: valResult.OutOfSample,
			validated: true,
		})
	}

	// Stage 6: Deploy top winners.
	deployed := 0
	for _, w := range winners {
		if deployed >= cfg.MaxWinners {
			break
		}

		configJSON, err := json.Marshal(map[string]any{
			"options_rules": w.config,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("marshal %s: %v", w.ticker, err))
			continue
		}

		cron := cfg.ScheduleCron
		if cron == "" {
			cron = "0 */2 * * 1-5" // every 2 hours, weekdays
		}

		strategy := domain.Strategy{
			ID:           uuid.New(),
			Name:         fmt.Sprintf("options: %s %s", w.ticker, w.config.StrategyType),
			Ticker:       w.ticker,
			MarketType:   domain.MarketTypeOptions,
			IsPaper:      true,
			Status:       domain.StrategyStatusActive,
			ScheduleCron: cron,
			Config:       json.RawMessage(configJSON),
		}

		if !cfg.DryRun {
			if createErr := deps.Strategies.Create(ctx, &strategy); createErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("deploy %s: %v", w.ticker, createErr))
				continue
			}
		}

		result.Winners = append(result.Winners, OptionsDeployedStrategy{
			StrategyID:  strategy.ID,
			Ticker:      w.ticker,
			Config:      w.config,
			InSample:    w.inSample,
			OutOfSample: w.oosSample,
			Score:       w.score,
		})
		deployed++

		logger.Info("options/discovery: strategy deployed",
			slog.String("id", strategy.ID.String()),
			slog.String("ticker", w.ticker),
			slog.String("type", string(w.config.StrategyType)),
			slog.Float64("score", w.score),
		)
	}
	result.Deployed = deployed

	result.Duration = time.Since(start)
	logger.Info("options/discovery: complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("scored", result.Scored),
		slog.Int("generated", result.Generated),
		slog.Int("swept", result.Swept),
		slog.Int("validated", result.Validated),
		slog.Int("deployed", result.Deployed),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}
