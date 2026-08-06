package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	overnightBacktestGeneratePerChunk     = 3
	overnightBacktestChunkTimeout         = 20 * time.Minute
	overnightBacktestGenerateTimeout      = 8 * time.Minute
	overnightBacktestProgressTimeout      = 30 * time.Second
	overnightBacktestProgressReserve      = 30 * time.Second
	overnightBacktestMaxRunAge            = 18 * time.Hour
	overnightBacktestGenerationMaxRetries = 1
)

type overnightBacktestChunker struct {
	deps             OrchestratorDeps
	progress         repository.OvernightBacktestRunRepository
	logger           *slog.Logger
	generatePerChunk int
	generateTimeout  time.Duration
	progressTimeout  time.Duration
}

func newOvernightBacktestChunker(deps OrchestratorDeps, logger *slog.Logger) overnightBacktestChunker {
	if logger == nil {
		logger = slog.Default()
	}
	return overnightBacktestChunker{deps: deps, progress: deps.OvernightBacktestRuns, logger: logger, generatePerChunk: overnightBacktestGeneratePerChunk}
}

func (c overnightBacktestChunker) nextGenerateEnd(start, total int) int {
	if c.generatePerChunk <= 0 {
		c.generatePerChunk = overnightBacktestGeneratePerChunk
	}
	if total <= 0 || start >= total {
		return start
	}
	end := start + c.generatePerChunk
	if end > total {
		end = total
	}
	return end
}

func (c overnightBacktestChunker) advanceAfterGenerate(run *domain.OvernightBacktestRun) {
	if run.CandidateIndex < len(run.Candidates) {
		run.Phase = domain.OvernightBacktestPhaseGenerate
		return
	}
	run.Phase = domain.OvernightBacktestPhaseSweepValidateDeploy
}

func (c overnightBacktestChunker) RunChunk(ctx context.Context) error {
	if c.progress == nil {
		return fmt.Errorf("overnight_backtest: progress repository not configured")
	}
	run, err := c.progress.GetActive(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			run = nil
		} else {
			return err
		}
	}
	if run == nil {
		now := time.Now()
		latest, latestErr := c.progress.ListLatest(ctx, 1)
		if latestErr != nil {
			return latestErr
		}
		if len(latest) > 0 && latest[0].Status == domain.OvernightBacktestStatusCompleted && sameEasternDate(latest[0].StartedAt, now) {
			logger := c.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Info("overnight_backtest: today's run already completed", slog.String("run_id", latest[0].ID.String()))
			return nil
		}
		run = &domain.OvernightBacktestRun{ID: uuid.New(), Status: domain.OvernightBacktestStatusRunning, Phase: domain.OvernightBacktestPhaseScreen, StartedAt: now, UpdatedAt: now}
		if err := c.progress.Create(ctx, run); err != nil {
			return err
		}
	}
	if time.Since(run.StartedAt) > overnightBacktestMaxRunAge {
		run.Status = domain.OvernightBacktestStatusFailed
		now := time.Now()
		run.CompletedAt = &now
		run.UpdatedAt = now
		return c.updateProgress(run)
	}
	chunkCtx, cancel := context.WithTimeout(ctx, overnightBacktestChunkTimeout)
	defer cancel()
	switch run.Phase {
	case domain.OvernightBacktestPhaseScreen:
		return c.runScreen(chunkCtx, run)
	case domain.OvernightBacktestPhaseGenerate:
		return c.runGenerateChunk(chunkCtx, run)
	case domain.OvernightBacktestPhaseSweepValidateDeploy:
		return c.runSweepValidateDeploy(chunkCtx, run)
	default:
		return fmt.Errorf("overnight_backtest: unknown phase %q", run.Phase)
	}
}

func sameEasternDate(a, b time.Time) bool {
	a = a.In(easternTime)
	b = b.In(easternTime)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func (c overnightBacktestChunker) runScreen(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if c.deps.Universe == nil {
		run.Status = domain.OvernightBacktestStatusCompleted
		run.Phase = domain.OvernightBacktestPhaseDone
		now := time.Now()
		run.CompletedAt = &now
		run.UpdatedAt = now
		return c.updateProgress(run)
	}
	if c.deps.DataService == nil {
		return fmt.Errorf("overnight_backtest: data service not configured")
	}
	watchlist, err := c.deps.Universe.GetWatchlist(ctx, overnightBacktestWatchlistLimit)
	if err != nil {
		return err
	}
	tickers := make([]string, len(watchlist))
	for i, t := range watchlist {
		tickers[i] = t.Ticker
	}
	history, err := c.deps.DataService.DownloadHistoricalOHLCV(ctx, domain.MarketTypeStock, tickers, data.Timeframe1d, time.Now().AddDate(-5, 0, 0), time.Now(), true)
	if err != nil {
		return fmt.Errorf("overnight_backtest: refresh screen inputs: %w", err)
	}
	refreshedTickers := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		if len(history[ticker]) > 0 {
			refreshedTickers = append(refreshedTickers, ticker)
		}
	}
	if len(refreshedTickers) == 0 {
		return fmt.Errorf("overnight_backtest: no refreshed screen inputs")
	}
	screened, err := discovery.Screen(ctx, c.deps.DataService, discovery.ScreenerConfig{Tickers: refreshedTickers, MarketType: domain.MarketTypeStock}, c.logger)
	if err != nil {
		return err
	}
	run.Candidates = discovery.CheckpointCandidatesFromScreenResults(screened)
	run.Summary.Candidates = len(run.Candidates)
	run.CandidateIndex = 0
	run.Phase = domain.OvernightBacktestPhaseGenerate
	run.UpdatedAt = time.Now()
	return c.updateProgress(run)
}

func (c overnightBacktestChunker) runGenerateChunk(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if c.deps.LLMProvider == nil {
		return fmt.Errorf("overnight_backtest: LLM provider not configured")
	}
	start := run.CandidateIndex
	end := c.nextGenerateEnd(start, len(run.Candidates))
	for i := start; i < end; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidate := run.Candidates[i]
		screen := discovery.ScreenResultsFromCheckpointCandidates([]domain.OvernightBacktestCandidate{candidate})[0]
		generateCtx, cancel := c.generationContext(ctx)
		generated, err := discovery.GenerateStrategy(generateCtx, discovery.GeneratorConfig{Provider: c.deps.LLMProvider, MaxRetries: overnightBacktestGenerationMaxRetries, Metrics: c.deps.GeneratorMetrics}, screen, c.logger)
		cancel()
		if err != nil {
			run.Errors = append(run.Errors, err.Error())
		} else {
			cfgJSON, err := encodeOvernightGeneratedConfig(*generated)
			if err != nil {
				run.Errors = append(run.Errors, err.Error())
			} else {
				run.Generated = append(run.Generated, domain.OvernightBacktestGenerated{Ticker: candidate.Ticker, Config: json.RawMessage(cfgJSON)})
				run.Summary.Generated++
			}
		}
		run.CandidateIndex = i + 1
		c.advanceAfterGenerate(run)
		run.UpdatedAt = time.Now()
		if updateErr := c.updateProgress(run); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (c overnightBacktestChunker) runSweepValidateDeploy(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if c.deps.DataService == nil {
		return fmt.Errorf("overnight_backtest: data service not configured")
	}
	if c.deps.StrategyRepo == nil {
		return fmt.Errorf("overnight_backtest: strategy repository not configured")
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	type generated struct {
		ticker string
		config rules.RulesEngineConfig
	}
	generatedConfigs := make([]generated, 0, len(run.Generated))
	for _, gen := range run.Generated {
		rulesCfg, err := decodeOvernightGeneratedConfig(gen.Config)
		if err != nil {
			return err
		}
		generatedConfigs = append(generatedConfigs, generated{ticker: gen.Ticker, config: rulesCfg})
	}
	barsByTicker := make(map[string][]domain.OHLCV, len(generatedConfigs))
	configNameToTicker := make(map[string]string, len(generatedConfigs))
	allBests := make([]discovery.SweepResult, 0, len(generatedConfigs))
	for _, gen := range generatedConfigs {
		if err := ctx.Err(); err != nil {
			return err
		}
		history, err := c.deps.DataService.DownloadHistoricalOHLCV(ctx, domain.MarketTypeStock, []string{gen.ticker}, data.Timeframe1d, time.Now().AddDate(-5, 0, 0), time.Now(), true)
		if err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("history %s: %v", gen.ticker, err))
			continue
		}
		bars := history[gen.ticker]
		if len(bars) < 50 {
			continue
		}
		barsByTicker[gen.ticker] = bars
		configNameToTicker[gen.config.Name] = gen.ticker
		endDate := bars[len(bars)-1].Timestamp
		startDate := endDate.AddDate(-3, 0, 0)
		if startDate.Before(bars[0].Timestamp) {
			startDate = bars[0].Timestamp
		}
		sweepCfg := discovery.SweepConfig{Ticker: gen.ticker, MarketType: domain.MarketTypeStock, Bars: bars, StartDate: startDate, EndDate: endDate, InitialCash: 100000, Variations: 50}
		sweepResults, err := discovery.RunSweep(ctx, gen.config, sweepCfg, discovery.DefaultScoringConfig(), logger)
		if err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("sweep %s: %v", gen.ticker, err))
			continue
		}
		if len(sweepResults) > 0 {
			allBests = append(allBests, sweepResults[0])
		}
	}
	run.Summary.Swept = len(allBests)
	maxWinners := 3
	topScorers := discovery.FilterAndRank(allBests, discovery.DefaultScoringConfig(), maxWinners*2)
	validated := 0
	deployed := 0
	passed := make([]discovery.SweepResult, 0, len(topScorers))
	for _, scorer := range topScorers {
		if err := ctx.Err(); err != nil {
			return err
		}
		ticker := configNameToTicker[scorer.Config.Name]
		bars := barsByTicker[ticker]
		if len(bars) == 0 {
			continue
		}
		val, err := discovery.ValidateOutOfSample(ctx, discovery.ValidationConfig{}, bars, scorer.Config, bars[0].Timestamp, bars[len(bars)-1].Timestamp, 100000, logger)
		if err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("validate %s: %v", ticker, err))
			continue
		}
		if !val.Passed {
			continue
		}
		validated++
		passed = append(passed, scorer)
	}
	if len(passed) > maxWinners {
		passed = passed[:maxWinners]
	}
	for _, scorer := range passed {
		ticker := configNameToTicker[scorer.Config.Name]
		configJSON, err := json.Marshal(map[string]any{"rules_engine": scorer.Config})
		if err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("marshal config %s: %v", ticker, err))
			continue
		}
		strategy := domain.Strategy{ID: uuid.New(), Name: fmt.Sprintf("discovery: %s %s", ticker, scorer.Config.Name), Ticker: ticker, MarketType: domain.MarketTypeStock, IsPaper: true, Status: "active", ScheduleCron: "0 */2 * * *", Config: json.RawMessage(configJSON)}
		if _, _, err := discovery.CreateOrReusePaperStrategy(ctx, c.deps.StrategyRepo, strategy); err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("deploy %s: %v", ticker, err))
			continue
		}
		deployed++
	}
	run.Summary.Validated = validated
	run.Summary.Deployed = deployed
	run.Phase = domain.OvernightBacktestPhaseDone
	run.Status = domain.OvernightBacktestStatusCompleted
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	return c.updateProgress(run)
}

func (c overnightBacktestChunker) updateProgress(run *domain.OvernightBacktestRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.progressTimeoutOrDefault())
	defer cancel()
	return c.progress.Update(ctx, run)
}

func (c overnightBacktestChunker) generationContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := c.generateTimeoutOrDefault()
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline) - overnightBacktestProgressReserve
		if remaining <= 0 {
			remaining = time.Second
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(parent, timeout)
}

func (c overnightBacktestChunker) generateTimeoutOrDefault() time.Duration {
	if c.generateTimeout > 0 {
		return c.generateTimeout
	}
	return overnightBacktestGenerateTimeout
}

func (c overnightBacktestChunker) progressTimeoutOrDefault() time.Duration {
	if c.progressTimeout > 0 {
		return c.progressTimeout
	}
	return overnightBacktestProgressTimeout
}

func encodeOvernightGeneratedConfig(cfg rules.RulesEngineConfig) (json.RawMessage, error) {
	cfgJSON, err := json.Marshal(map[string]any{"rules_engine": cfg})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(cfgJSON), nil
}

func decodeOvernightGeneratedConfig(raw json.RawMessage) (rules.RulesEngineConfig, error) {
	var wrapped struct {
		RulesEngine rules.RulesEngineConfig `json:"rules_engine"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return rules.RulesEngineConfig{}, err
	}
	if wrapped.RulesEngine.Name == "" {
		return rules.RulesEngineConfig{}, fmt.Errorf("overnight_backtest: generated config missing rules_engine")
	}
	return wrapped.RulesEngine, nil
}
