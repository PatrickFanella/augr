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
	DataService      *data.DataService
	LLMProvider      llm.Provider
	Strategies       repository.StrategyRepository
	BacktestConfigs  repository.BacktestConfigRepository
	GeneratorMetrics GeneratorMetrics
	Logger           *slog.Logger
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

type CandidateEvidence struct {
	Ticker     string    `json:"ticker"`
	Close      float64   `json:"close"`
	ADV        float64   `json:"adv"`
	ATR        float64   `json:"atr"`
	Bars       int       `json:"bars"`
	FirstBarAt time.Time `json:"first_bar_at,omitempty"`
	LastBarAt  time.Time `json:"last_bar_at,omitempty"`
}

type SweepEvidence struct {
	Ticker       string                  `json:"ticker"`
	Label        string                  `json:"label"`
	Config       rules.RulesEngineConfig `json:"config"`
	Metrics      backtest.Metrics        `json:"metrics"`
	Score        float64                 `json:"score"`
	HistoryBars  int                     `json:"history_bars"`
	HistoryStart time.Time               `json:"history_start,omitempty"`
	HistoryEnd   time.Time               `json:"history_end,omitempty"`
}

type ValidationEvidence struct {
	Ticker      string                  `json:"ticker"`
	Config      rules.RulesEngineConfig `json:"config"`
	Passed      bool                    `json:"passed"`
	InSample    backtest.Metrics        `json:"in_sample"`
	OutOfSample backtest.Metrics        `json:"out_of_sample"`
	OOSRatio    float64                 `json:"oos_ratio"`
	Reason      string                  `json:"reason,omitempty"`
}

// DiscoveryResult summarises the pipeline execution.
type DiscoveryResult struct {
	Candidates         int                  `json:"candidates"`
	Generated          int                  `json:"generated"`
	Swept              int                  `json:"swept"`
	Validated          int                  `json:"validated"`
	Deployed           int                  `json:"deployed"`
	Proposed           int                  `json:"proposed"`
	Created            int                  `json:"created"`
	Reused             int                  `json:"reused"`
	CandidateEvidence  []CandidateEvidence  `json:"candidate_evidence,omitempty"`
	GenerationEvidence []GenerationEvidence `json:"generation_evidence,omitempty"`
	SweepEvidence      []SweepEvidence      `json:"sweep_evidence,omitempty"`
	ValidationEvidence []ValidationEvidence `json:"validation_evidence,omitempty"`
	Winners            []DeployedStrategy   `json:"winners"`
	Duration           time.Duration        `json:"duration"`
	Errors             []string             `json:"errors,omitempty"`
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
		cfg.ScheduleCron = "0 */2 * * *"
	}
	if !cfg.DryRun {
		if deps.Strategies == nil {
			return nil, fmt.Errorf("discovery: strategy repository is required for deployment")
		}
		if deps.BacktestConfigs == nil {
			return nil, fmt.Errorf("discovery: backtest config repository is required for deployment")
		}
	}

	result := &DiscoveryResult{}

	// Step 1: Screen candidates.
	candidates, err := Screen(ctx, deps.DataService, cfg.Screener, logger)
	if err != nil {
		return nil, fmt.Errorf("discovery: screening failed: %w", err)
	}
	result.Candidates = len(candidates)
	for _, candidate := range candidates {
		evidence := CandidateEvidence{Ticker: candidate.Ticker, Close: candidate.Close, ADV: candidate.ADV, ATR: candidate.ATR, Bars: len(candidate.Bars)}
		if len(candidate.Bars) > 0 {
			evidence.FirstBarAt = candidate.Bars[0].Timestamp
			evidence.LastBarAt = candidate.Bars[len(candidate.Bars)-1].Timestamp
		}
		result.CandidateEvidence = append(result.CandidateEvidence, evidence)
	}
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
	if generatorCfg.Metrics == nil {
		generatorCfg.Metrics = deps.GeneratorMetrics
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discovery: context cancelled during generation: %w", err)
		}

		rulesConfig, generationEvidence, genErr := GenerateStrategyWithEvidence(ctx, generatorCfg, candidate, logger)
		if generationEvidence != nil {
			result.GenerationEvidence = append(result.GenerationEvidence, *generationEvidence)
		}
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
			result.Errors = append(result.Errors, fmt.Sprintf("history %s: insufficient bars (%d)", gen.candidate.Ticker, len(histBars)))
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
			best := sweepResults[0]
			allBests = append(allBests, best)
			result.SweepEvidence = append(result.SweepEvidence, SweepEvidence{
				Ticker: gen.candidate.Ticker, Label: best.Label, Config: best.Config,
				Metrics: best.Metrics, Score: best.Score, HistoryBars: len(histBars),
				HistoryStart: histBars[0].Timestamp, HistoryEnd: histBars[len(histBars)-1].Timestamp,
			})
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("sweep %s: no successful variants", gen.candidate.Ticker))
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
		if ticker == "" || len(bars) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("validate %s: missing ticker or history", scorer.Config.Name))
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
		result.ValidationEvidence = append(result.ValidationEvidence, ValidationEvidence{
			Ticker: ticker, Config: scorer.Config, Passed: valResult.Passed,
			InSample: valResult.InSample, OutOfSample: valResult.OutOfSample,
			OOSRatio: valResult.OOSRatio, Reason: valResult.Reason,
		})

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
	if !discoveryCanDeploy(result) {
		result.Duration = time.Since(start)
		logger.Warn("discovery: deployment skipped after incomplete pipeline",
			slog.Int("errors", len(result.Errors)),
			slog.Int("validated", result.Validated),
			slog.Duration("duration", result.Duration),
		)
		return result, nil
	}

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

		wasCreated := false
		if !cfg.DryRun {
			initialCash := cfg.Sweep.InitialCash
			if initialCash == 0 {
				initialCash = 100_000
			}
			createdStrategy, created, createErr := createOrReuseDiscoveryStrategy(
				ctx, strategy, v.bars, initialCash, deps.Strategies, deps.BacktestConfigs,
			)
			if createErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("deploy %s: %v", strategy.Ticker, createErr))
				continue
			}
			strategy = createdStrategy
			if !created {
				logger.Info("discovery: strategy already exists, reusing",
					slog.String("id", strategy.ID.String()),
					slog.String("ticker", strategy.Ticker),
					slog.String("name", strategy.Name),
				)
			} else {
				wasCreated = true
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
		recordDiscoveryDeploymentOutcome(result, cfg.DryRun, wasCreated)

		logger.Info("discovery: winner selected",
			slog.String("id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
			slog.String("name", strategy.Name),
			slog.Float64("score", v.sweep.Score),
			slog.Bool("dry_run", cfg.DryRun),
		)
	}
	result.Duration = time.Since(start)

	logger.Info("discovery: pipeline complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("generated", result.Generated),
		slog.Int("swept", result.Swept),
		slog.Int("validated", result.Validated),
		slog.Int("deployed", result.Deployed),
		slog.Int("proposed", result.Proposed),
		slog.Int("created", result.Created),
		slog.Int("reused", result.Reused),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}

// createOrReuseDiscoveryStrategy makes the backtest definition a durable
// research precondition. New candidates remain inactive and unscheduled after
// their config is stored. A reused active legacy strategy is paused only when a
// missing config must be repaired; discovery never reactivates it.
func createOrReuseDiscoveryStrategy(
	ctx context.Context,
	strategy domain.Strategy,
	bars []domain.OHLCV,
	initialCash float64,
	strategies repository.StrategyRepository,
	configs repository.BacktestConfigRepository,
) (domain.Strategy, bool, error) {
	if strategies == nil {
		return domain.Strategy{}, false, fmt.Errorf("strategy repository is required")
	}
	if configs == nil {
		return domain.Strategy{}, false, fmt.Errorf("backtest config repository is required")
	}
	if len(bars) < 2 {
		return domain.Strategy{}, false, fmt.Errorf("at least two historical bars are required")
	}

	persisted, created, err := CreateOrReusePaperStrategy(ctx, strategies, strategy)
	if err != nil {
		return domain.Strategy{}, false, err
	}

	existingConfigs, err := configs.List(ctx, repository.BacktestConfigFilter{StrategyID: &persisted.ID}, 1, 0)
	if err != nil {
		return persisted, created, fmt.Errorf("list backtest configs: %w", err)
	}
	if len(existingConfigs) > 0 {
		return persisted, created, nil
	}

	if persisted.Status == domain.StrategyStatusActive {
		persisted.Status = domain.StrategyStatusPaused
		if err := strategies.Update(ctx, &persisted); err != nil {
			return persisted, created, fmt.Errorf("pause strategy before backtest config repair: %w", err)
		}
	}

	btCfg := domain.BacktestConfig{
		ID:         uuid.New(),
		StrategyID: persisted.ID,
		Name:       persisted.Name + " (discovery)",
		StartDate:  bars[0].Timestamp,
		EndDate:    bars[len(bars)-1].Timestamp,
		Simulation: domain.BacktestSimulationParameters{InitialCapital: initialCash},
	}
	if err := configs.Create(ctx, &btCfg); err != nil {
		if created {
			if cleanupErr := strategies.Delete(ctx, persisted.ID); cleanupErr != nil {
				return persisted, true, fmt.Errorf("create backtest config: %w; remove paused strategy: %v", err, cleanupErr)
			}
			return domain.Strategy{}, false, fmt.Errorf("create backtest config: %w", err)
		}
		return persisted, false, fmt.Errorf("create backtest config; strategy remains fail-closed: %w", err)
	}

	return persisted, created, nil
}

func recordDiscoveryDeploymentOutcome(result *DiscoveryResult, dryRun, created bool) {
	if result == nil {
		return
	}
	result.Proposed++
	if dryRun {
		return
	}
	if created {
		result.Created++
		result.Deployed++
		return
	}
	result.Reused++
}

func discoveryCanDeploy(result *DiscoveryResult) bool {
	return result != nil && len(result.Errors) == 0
}

// CheckpointCandidatesFromScreenResults converts screened candidates into the
// domain checkpoint shape used by resumable overnight backtest runs.
func CheckpointCandidatesFromScreenResults(in []ScreenResult) []domain.OvernightBacktestCandidate {
	out := make([]domain.OvernightBacktestCandidate, 0, len(in))
	for _, c := range in {
		out = append(out, domain.OvernightBacktestCandidate{
			Ticker:     c.Ticker,
			Bars:       c.Bars,
			Indicators: c.Indicators,
			Close:      c.Close,
			ADV:        c.ADV,
			ATR:        c.ATR,
		})
	}
	return out
}

// ScreenResultsFromCheckpointCandidates converts persisted checkpoint
// candidates back into discovery screen results for later pipeline phases.
func ScreenResultsFromCheckpointCandidates(in []domain.OvernightBacktestCandidate) []ScreenResult {
	out := make([]ScreenResult, 0, len(in))
	for _, c := range in {
		out = append(out, ScreenResult{
			Ticker:     c.Ticker,
			Bars:       c.Bars,
			Indicators: c.Indicators,
			Close:      c.Close,
			ADV:        c.ADV,
			ATR:        c.ATR,
		})
	}
	return out
}
