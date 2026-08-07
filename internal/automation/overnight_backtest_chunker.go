package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
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
		return c.failRun(run, fmt.Errorf("overnight_backtest: stale run %s exceeded %s and was marked failed", run.ID, overnightBacktestMaxRunAge))
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
		return c.failRun(run, fmt.Errorf("overnight_backtest: unknown phase %q", run.Phase))
	}
}

func sameEasternDate(a, b time.Time) bool {
	a = a.In(easternTime)
	b = b.In(easternTime)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func (c overnightBacktestChunker) runScreen(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if c.deps.Universe == nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: universe not configured"))
	}
	if c.deps.DataService == nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: data service not configured"))
	}
	watchlist, err := c.deps.Universe.GetWatchlist(ctx, overnightBacktestWatchlistLimit)
	if err != nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: get watchlist: %w", err))
	}
	tickers := make([]string, len(watchlist))
	for i, t := range watchlist {
		tickers[i] = t.Ticker
	}
	if len(tickers) == 0 {
		return c.failRun(run, fmt.Errorf("overnight_backtest: watchlist empty"))
	}
	history, err := c.deps.DataService.DownloadHistoricalOHLCV(ctx, domain.MarketTypeStock, tickers, data.Timeframe1d, time.Now().AddDate(-5, 0, 0), time.Now(), true)
	if err != nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: refresh screen inputs: %w", err))
	}
	refreshedTickers := make([]string, 0, len(tickers))
	empty := 0
	stale := 0
	now := time.Now()
	for _, ticker := range tickers {
		bars := history[ticker]
		if len(bars) == 0 {
			empty++
			continue
		}
		if !dailyBarFresh(now, bars[len(bars)-1].Timestamp) {
			stale++
			continue
		}
		refreshedTickers = append(refreshedTickers, ticker)
	}
	if empty > 0 || stale > 0 {
		return c.failRun(run, fmt.Errorf("overnight_backtest: incomplete screen inputs: empty=%d stale=%d requested=%d", empty, stale, len(tickers)))
	}
	screened, err := discovery.Screen(ctx, c.deps.DataService, discovery.ScreenerConfig{Tickers: refreshedTickers, MarketType: domain.MarketTypeStock}, c.logger)
	if err != nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: screen: %w", err))
	}
	if err := validateOvernightScreenResults(screened, refreshedTickers, now); err != nil {
		return c.failRun(run, err)
	}
	run.Candidates = discovery.CheckpointCandidatesFromScreenResults(screened)
	run.Summary.Candidates = len(run.Candidates)
	run.CandidateIndex = 0
	run.Phase = domain.OvernightBacktestPhaseGenerate
	run.UpdatedAt = time.Now()
	return c.updateProgress(run)
}

func validateOvernightScreenResults(screened []discovery.ScreenResult, requested []string, now time.Time) error {
	if len(screened) == 0 {
		return fmt.Errorf("overnight_backtest: screen returned no candidates")
	}
	requestedSet := make(map[string]struct{}, len(requested))
	for _, ticker := range requested {
		requestedSet[strings.ToUpper(strings.TrimSpace(ticker))] = struct{}{}
	}
	seen := make(map[string]struct{}, len(screened))
	for _, candidate := range screened {
		ticker := strings.ToUpper(strings.TrimSpace(candidate.Ticker))
		if ticker == "" {
			return fmt.Errorf("overnight_backtest: screen candidate missing ticker")
		}
		if _, ok := requestedSet[ticker]; !ok {
			return fmt.Errorf("overnight_backtest: screen returned unexpected ticker %s", ticker)
		}
		if _, duplicate := seen[ticker]; duplicate {
			return fmt.Errorf("overnight_backtest: screen returned duplicate ticker %s", ticker)
		}
		seen[ticker] = struct{}{}
		if len(candidate.Bars) < 50 {
			return fmt.Errorf("overnight_backtest: screen candidate %s has insufficient bars: %d", ticker, len(candidate.Bars))
		}
		for i := 1; i < len(candidate.Bars); i++ {
			if !candidate.Bars[i].Timestamp.After(candidate.Bars[i-1].Timestamp) {
				return fmt.Errorf("overnight_backtest: screen candidate %s bars are not strictly ordered at index %d", ticker, i)
			}
		}
		latest := candidate.Bars[len(candidate.Bars)-1]
		if !dailyBarFresh(now, latest.Timestamp) {
			return fmt.Errorf("overnight_backtest: screen candidate %s has stale latest bar %s", ticker, latest.Timestamp.UTC().Format(time.RFC3339))
		}
		if !finitePositive(candidate.Close) || !finitePositive(candidate.ADV) || !finitePositive(candidate.ATR) {
			return fmt.Errorf("overnight_backtest: screen candidate %s has invalid close/ADV/ATR", ticker)
		}
		if candidate.Close != latest.Close {
			return fmt.Errorf("overnight_backtest: screen candidate %s close does not match latest bar", ticker)
		}
		if len(candidate.Indicators) < 20 {
			return fmt.Errorf("overnight_backtest: screen candidate %s has insufficient indicators: %d", ticker, len(candidate.Indicators))
		}
		for _, indicator := range candidate.Indicators {
			if strings.TrimSpace(indicator.Name) == "" || math.IsNaN(indicator.Value) || math.IsInf(indicator.Value, 0) || !sameMarketDate(indicator.Timestamp.In(easternTime), latest.Timestamp.In(easternTime)) {
				return fmt.Errorf("overnight_backtest: screen candidate %s has invalid or stale indicator %q", ticker, indicator.Name)
			}
		}
	}
	return nil
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (c overnightBacktestChunker) runGenerateChunk(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if c.deps.LLMProvider == nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: LLM provider not configured"))
	}
	initialErrors := len(run.Errors)
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
	if failures := len(run.Errors) - initialErrors; failures > 0 {
		return c.failRun(run, fmt.Errorf("overnight_backtest: %d candidate generations failed", failures))
	}
	return nil
}

func (c overnightBacktestChunker) runSweepValidateDeploy(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if c.deps.DataService == nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: data service not configured"))
	}
	if c.deps.StrategyRepo == nil {
		return c.failRun(run, fmt.Errorf("overnight_backtest: strategy repository not configured"))
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	initialErrors := len(run.Errors)
	type generated struct {
		ticker string
		config rules.RulesEngineConfig
	}
	generatedConfigs := make([]generated, 0, len(run.Generated))
	for _, gen := range run.Generated {
		rulesCfg, err := decodeOvernightGeneratedConfig(gen.Config)
		if err != nil {
			return c.failRun(run, err)
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
			run.Errors = append(run.Errors, fmt.Sprintf("history %s: insufficient bars %d", gen.ticker, len(bars)))
			continue
		}
		if !dailyBarFresh(time.Now(), bars[len(bars)-1].Timestamp) {
			run.Errors = append(run.Errors, fmt.Sprintf("history %s: stale latest bar %s", gen.ticker, bars[len(bars)-1].Timestamp.UTC().Format(time.RFC3339)))
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
	created := 0
	reused := 0
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
	if failures := len(run.Errors) - initialErrors; failures > 0 {
		return c.failRun(run, fmt.Errorf("overnight_backtest: %d sweep or validation inputs failed", failures))
	}
	for _, scorer := range passed {
		ticker := configNameToTicker[scorer.Config.Name]
		configJSON, err := json.Marshal(map[string]any{"rules_engine": scorer.Config})
		if err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("marshal config %s: %v", ticker, err))
			continue
		}
		strategy := domain.Strategy{ID: uuid.New(), Name: fmt.Sprintf("discovery: %s %s", ticker, scorer.Config.Name), Ticker: ticker, MarketType: domain.MarketTypeStock, IsPaper: true, Status: "active", ScheduleCron: "0 */2 * * *", Config: json.RawMessage(configJSON)}
		_, wasCreated, err := discovery.CreateOrReusePaperStrategy(ctx, c.deps.StrategyRepo, strategy)
		if err != nil {
			run.Errors = append(run.Errors, fmt.Sprintf("deploy %s: %v", ticker, err))
			continue
		}
		if wasCreated {
			created++
		} else {
			reused++
		}
	}
	run.Summary.Validated = validated
	run.Summary.Deployed = created
	run.Summary.Created = created
	run.Summary.Reused = reused
	if failures := len(run.Errors) - initialErrors; failures > 0 {
		return c.failRun(run, fmt.Errorf("overnight_backtest: %d deployment steps failed", failures))
	}
	run.Phase = domain.OvernightBacktestPhaseDone
	run.Status = domain.OvernightBacktestStatusCompleted
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	return c.updateProgress(run)
}

func (c overnightBacktestChunker) failRun(run *domain.OvernightBacktestRun, cause error) error {
	if run == nil {
		return cause
	}
	run.Errors = append(run.Errors, cause.Error())
	run.Status = domain.OvernightBacktestStatusFailed
	run.Phase = domain.OvernightBacktestPhaseDone
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	if err := c.updateProgress(run); err != nil {
		return fmt.Errorf("%v; persist failed run: %w", cause, err)
	}
	return cause
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
