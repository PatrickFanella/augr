package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

const (
	defaultStrategyPageSize       = 100
	defaultBacktestConfigPageSize = 100
	defaultJobTimeout             = 10 * time.Minute
)

var ErrAlreadyStarted = errors.New("scheduler: already started")

type pipelineExecutor interface {
	Execute(ctx context.Context, strategyID uuid.UUID, ticker string) (*agent.PipelineState, error)
}

type cronEngine interface {
	AddFunc(spec string, cmd func()) (cron.EntryID, error)
	Start()
	Stop() context.Context
}

type backtestRunner interface {
	Run(ctx context.Context, config domain.BacktestConfig) (*backtest.OrchestratorResult, error)
}

type strategyExecutor func(ctx context.Context, strategy domain.Strategy) error

// SchedulerMetrics is implemented by *metrics.Metrics.
type SchedulerMetrics interface {
	RecordSchedulerTick(tickType string)
}

type Option func(*Scheduler)

// WithStrategyExecution routes scheduled strategy triggers through a full
// strategy execution path instead of calling the raw pipeline directly.
func WithStrategyExecution(execute strategyExecutor) Option {
	return func(s *Scheduler) {
		s.strategyExecution = execute
	}
}

// WithMetrics wires scheduler tick emissions into the provided metrics sink.
func WithMetrics(m SchedulerMetrics) Option {
	return func(s *Scheduler) {
		s.metrics = m
	}
}

// WithBacktestScheduling enables cron-triggered backtest runs and persistence.
func WithBacktestScheduling(
	configRepo repository.BacktestConfigRepository,
	persister backtest.BacktestPersister,
	runner backtestRunner,
) Option {
	return func(s *Scheduler) {
		s.backtestConfigRepo = configRepo
		s.backtestPersister = persister
		s.backtestRunner = runner
	}
}

// TickerDiscoveryConfig holds scheduler-level configuration for the ticker
// discovery job.
type TickerDiscoveryConfig struct {
	Cron       string
	MinADV     float64
	MaxTickers int
}

// tickerDiscoveryDeps bundles dependencies required by the scheduled ticker
// discovery job.
type tickerDiscoveryDeps struct {
	universe      *universe.Universe
	polygonClient *polygon.Client
	discoveryDeps discovery.DiscoveryDeps
	config        TickerDiscoveryConfig
}

// WithTickerDiscovery enables scheduled pre-market ticker screening and
// discovery pipeline execution.
func WithTickerDiscovery(u *universe.Universe, pc *polygon.Client, dd discovery.DiscoveryDeps, cfg TickerDiscoveryConfig) Option {
	return func(s *Scheduler) {
		s.tickerDiscovery = &tickerDiscoveryDeps{
			universe:      u,
			polygonClient: pc,
			discoveryDeps: dd,
			config:        cfg,
		}
	}
}

// Scheduler loads active strategies and triggers pipeline runs on cron schedules.
type Scheduler struct {
	mu                 sync.Mutex
	cron               cronEngine
	strategyRepo       repository.StrategyRepository
	pipeline           pipelineExecutor
	strategyExecution  strategyExecutor
	metrics            SchedulerMetrics
	riskEngine         risk.RiskEngine
	backtestConfigRepo repository.BacktestConfigRepository
	backtestPersister  backtest.BacktestPersister
	backtestRunner     backtestRunner
	tickerDiscovery    *tickerDiscoveryDeps
	logger             *slog.Logger
	nowFunc            func() time.Time
	newCron            func() cronEngine
	ctx                context.Context
	cancel             context.CancelFunc
	jobTimeout         time.Duration
	dedup              strategyDedup
	backtestDedup      strategyDedup
	discoveryDedup     strategyDedup
	riskMonitor        *riskMonitor
	strategySem        chan struct{} // limits concurrent strategy executions
}

type strategyScheduleKey struct {
	Ticker     string
	MarketType domain.MarketType
	Schedule   string
}

// NewScheduler constructs a Scheduler with the supplied dependencies.
func NewScheduler(
	strategyRepo repository.StrategyRepository,
	pipeline pipelineExecutor,
	riskEngine risk.RiskEngine,
	logger *slog.Logger,
	opts ...Option,
) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Scheduler{
		strategyRepo: strategyRepo,
		pipeline:     pipeline,
		riskEngine:   riskEngine,
		logger:       logger,
		nowFunc:      time.Now,
		newCron: func() cronEngine {
			return cron.New()
		},
		jobTimeout: defaultJobTimeout,
		riskMonitor: &riskMonitor{
			riskEngine:   riskEngine,
			pollInterval: defaultPollInterval,
			logger:       logger,
		},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	// Allow at most 2 concurrent strategy executions to avoid overwhelming
	// the LLM (Ollama processes requests serially).
	s.strategySem = make(chan struct{}, 2)

	return s
}

// Start loads all active strategies, registers cron jobs, and starts the scheduler.
func (s *Scheduler) Start() error {
	if s.strategyRepo == nil {
		return fmt.Errorf("scheduler: strategy repository is required")
	}
	if s.pipeline == nil && s.strategyExecution == nil {
		return fmt.Errorf("scheduler: pipeline or strategy execution function is required")
	}
	if s.riskEngine == nil {
		return fmt.Errorf("scheduler: risk engine is required")
	}

	strategies, err := s.loadActiveStrategies(context.Background())
	if err != nil {
		return err
	}
	backtests, err := s.loadScheduledBacktests(context.Background())
	if err != nil {
		return err
	}

	engine := s.newCron()
	if engine == nil {
		return fmt.Errorf("scheduler: cron engine is required")
	}

	registered := 0

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil {
		return ErrAlreadyStarted
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cron = engine
	s.ctx = runCtx
	s.cancel = cancel
	seenActiveStrategySchedules := make(map[strategyScheduleKey]uuid.UUID)

	for _, strategy := range strategies {
		spec := strings.TrimSpace(strategy.ScheduleCron)
		if spec == "" {
			continue
		}

		if strategy.Status == domain.StrategyStatusActive {
			key := strategyScheduleKey{
				Ticker:     strings.ToUpper(strings.TrimSpace(strategy.Ticker)),
				MarketType: strategy.MarketType,
				Schedule:   spec,
			}
			if owner, exists := seenActiveStrategySchedules[key]; exists {
				s.logger.Warn("scheduler: duplicate active strategy schedule detected; skipping duplicate",
					slog.String("strategy_id", strategy.ID.String()),
					slog.String("existing_strategy_id", owner.String()),
					slog.String("ticker", strategy.Ticker),
					slog.String("market_type", strategy.MarketType.String()),
					slog.String("schedule", spec),
				)
				continue
			}
			seenActiveStrategySchedules[key] = strategy.ID
		}

		strategy := strategy
		entryID, err := engine.AddFunc(spec, func() {
			s.runStrategy(strategy)
		})
		if err != nil {
			_, cancel := s.clearStateLocked()
			if cancel != nil {
				cancel()
			}
			return fmt.Errorf("scheduler: register strategy %s schedule %q: %w", strategy.ID, spec, err)
		}

		registered++
		s.logger.Info("scheduler: registered strategy schedule",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
			slog.String("market_type", strategy.MarketType.String()),
			slog.String("schedule", spec),
			slog.Int("entry_id", int(entryID)),
		)
	}

	for _, config := range backtests {
		spec := strings.TrimSpace(config.ScheduleCron)
		if spec == "" {
			continue
		}

		config := config
		entryID, err := engine.AddFunc(spec, func() {
			s.runBacktest(config)
		})
		if err != nil {
			s.logger.Error("scheduler: failed to register backtest schedule, skipping",
				slog.String("backtest_config_id", config.ID.String()),
				slog.String("strategy_id", config.StrategyID.String()),
				slog.String("name", config.Name),
				slog.String("schedule", spec),
				slog.Any("error", err),
			)
			continue
		}

		registered++
		s.logger.Info("scheduler: registered backtest schedule",
			slog.String("backtest_config_id", config.ID.String()),
			slog.String("strategy_id", config.StrategyID.String()),
			slog.String("name", config.Name),
			slog.String("schedule", spec),
			slog.Int("entry_id", int(entryID)),
		)
	}

	if s.tickerDiscovery != nil {
		spec := s.tickerDiscovery.config.Cron
		_, err := engine.AddFunc(spec, func() {
			s.runTickerDiscovery()
		})
		if err != nil {
			s.logger.Error("scheduler: failed to register ticker discovery schedule",
				slog.String("cron", spec),
				slog.Any("error", err),
			)
		} else {
			registered++
			s.logger.Info("scheduler: registered ticker discovery", slog.String("cron", spec))
		}
	}

	engine.Start()

	s.logger.Info("scheduler: started",
		slog.Int("active_strategies", len(strategies)),
		slog.Int("scheduled_backtests", len(backtests)),
		slog.Int("registered_jobs", registered),
	)

	return nil
}

// TriggerStrategy triggers an immediate pipeline run for the given strategy,
// subject to the same concurrency semaphore and dedup guards as cron-triggered
// runs. Execution is asynchronous; this method returns immediately.
func (s *Scheduler) TriggerStrategy(strategy domain.Strategy) {
	go s.runStrategy(strategy)
}

// Stop gracefully stops the cron engine and waits for running jobs to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	engine, cancel := s.clearStateLocked()
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if engine == nil {
		return
	}

	<-engine.Stop().Done()
	s.logger.Info("scheduler: stopped")
}

// InFlightCount returns the number of in-flight scheduled pipeline runs.
func (s *Scheduler) InFlightCount() int {
	return s.dedup.Count()
}

func (s *Scheduler) loadActiveStrategies(ctx context.Context) ([]domain.Strategy, error) {
	var strategies []domain.Strategy
	for _, status := range []string{domain.StrategyStatusActive, domain.StrategyStatusPaused} {
		filter := repository.StrategyFilter{Status: status}
		for offset := 0; ; offset += defaultStrategyPageSize {
			batch, err := s.strategyRepo.List(ctx, filter, defaultStrategyPageSize, offset)
			if err != nil {
				return nil, fmt.Errorf("scheduler: list %s strategies: %w", status, err)
			}
			strategies = append(strategies, batch...)
			if len(batch) < defaultStrategyPageSize {
				break
			}
		}
	}
	return strategies, nil
}

func (s *Scheduler) loadScheduledBacktests(ctx context.Context) ([]domain.BacktestConfig, error) {
	if s.backtestConfigRepo == nil && s.backtestPersister == nil && s.backtestRunner == nil {
		return nil, nil
	}
	if s.backtestConfigRepo == nil || s.backtestPersister == nil || s.backtestRunner == nil {
		return nil, fmt.Errorf("scheduler: backtest scheduling requires config repository, persister, and runner")
	}

	var configs []domain.BacktestConfig
	for offset := 0; ; offset += defaultBacktestConfigPageSize {
		batch, err := s.backtestConfigRepo.List(ctx, repository.BacktestConfigFilter{}, defaultBacktestConfigPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("scheduler: list backtest configs: %w", err)
		}

		for _, config := range batch {
			if strings.TrimSpace(config.ScheduleCron) != "" {
				configs = append(configs, config)
			}
		}
		if len(batch) < defaultBacktestConfigPageSize {
			break
		}
	}

	return configs, nil
}

func (s *Scheduler) runStrategy(strategy domain.Strategy) {
	if s.metrics != nil {
		s.metrics.RecordSchedulerTick("strategy")
	}

	// Concurrency gate: limit how many strategies run in parallel to avoid
	// overwhelming the LLM backend (Ollama is single-threaded by default).
	s.strategySem <- struct{}{}
	defer func() { <-s.strategySem }()

	// Dedup: skip if this strategy is already running.
	if !s.dedup.TryAcquire(strategy.ID) {
		s.logger.Warn("scheduler: skipping strategy; already in flight",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
		)
		return
	}
	defer s.dedup.Release(strategy.ID)

	// Re-read strategy to get latest status and skip_next_run.
	fetchCtx, fetchCancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer fetchCancel()
	current, err := s.strategyRepo.Get(fetchCtx, strategy.ID)
	if err != nil {
		s.logger.Error("scheduler: failed to fetch strategy",
			slog.String("strategy_id", strategy.ID.String()),
			slog.Any("error", err),
		)
		return
	}

	if current.Status == domain.StrategyStatusPaused {
		s.logger.Info("scheduler: skipping paused strategy",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
		)
		return
	}

	if current.SkipNextRun {
		s.logger.Info("scheduler: skip_next_run is set; resetting and skipping",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
		)
		current.SkipNextRun = false
		if err := s.strategyRepo.Update(fetchCtx, current); err != nil {
			s.logger.Error("scheduler: failed to reset skip_next_run",
				slog.String("strategy_id", strategy.ID.String()),
				slog.Any("error", err),
			)
		}
		return
	}
	now := s.nowFunc()
	ctx, cancel := s.jobContext()
	defer cancel()

	s.logger.Info("scheduler: triggered strategy schedule",
		slog.String("strategy_id", strategy.ID.String()),
		slog.String("ticker", strategy.Ticker),
		slog.String("market_type", strategy.MarketType.String()),
		slog.Time("triggered_at", now.UTC()),
	)

	killSwitchActive, err := s.riskEngine.IsKillSwitchActive(ctx)
	if err != nil {
		s.logger.Error("scheduler: failed to check kill switch",
			slog.String("strategy_id", strategy.ID.String()),
			slog.Any("error", err),
		)
		return
	}
	if killSwitchActive {
		s.logger.Warn("scheduler: skipped strategy because kill switch is active",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
		)
		return
	}

	if !IsMarketOpen(now, strategy.MarketType) {
		s.logger.Info("scheduler: skipped strategy because market is closed",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
			slog.String("market_type", strategy.MarketType.String()),
			slog.Time("checked_at", now.UTC()),
		)
		return
	}

	// Wrap context with kill-switch monitor for mid-pipeline abort.
	monCtx, monCancel := s.riskMonitor.monitorContext(ctx)
	defer monCancel()

	if s.strategyExecution != nil {
		if err := s.strategyExecution(monCtx, *current); err != nil {
			s.logger.Error("scheduler: strategy execution failed",
				slog.String("strategy_id", current.ID.String()),
				slog.String("ticker", current.Ticker),
				slog.Any("error", err),
			)
			return
		}

		s.logger.Info("scheduler: strategy execution completed",
			slog.String("strategy_id", current.ID.String()),
			slog.String("ticker", current.Ticker),
		)
		return
	}

	if _, err := s.pipeline.Execute(monCtx, current.ID, current.Ticker); err != nil {
		s.logger.Error("scheduler: pipeline execution failed",
			slog.String("strategy_id", current.ID.String()),
			slog.String("ticker", current.Ticker),
			slog.Any("error", err),
		)
		return
	}

	s.logger.Info("scheduler: pipeline execution completed",
		slog.String("strategy_id", current.ID.String()),
		slog.String("ticker", current.Ticker),
	)
}

func (s *Scheduler) runBacktest(config domain.BacktestConfig) {
	if s.metrics != nil {
		s.metrics.RecordSchedulerTick("backtest")
	}

	if !s.backtestDedup.TryAcquire(config.ID) {
		s.logger.Warn("scheduler: skipping backtest; already in flight",
			slog.String("backtest_config_id", config.ID.String()),
			slog.String("name", config.Name),
		)
		return
	}
	defer s.backtestDedup.Release(config.ID)

	triggeredAt := s.nowFunc().UTC()
	started := time.Now()
	ctx, cancel := s.jobContext()
	defer cancel()

	s.logger.Info("scheduler: triggered backtest schedule",
		slog.String("backtest_config_id", config.ID.String()),
		slog.String("strategy_id", config.StrategyID.String()),
		slog.String("name", config.Name),
		slog.Time("triggered_at", triggeredAt),
	)

	result, err := s.backtestRunner.Run(ctx, config)
	if err != nil {
		s.logger.Error("scheduler: backtest execution failed",
			slog.String("backtest_config_id", config.ID.String()),
			slog.String("name", config.Name),
			slog.Any("error", err),
		)
		return
	}

	if err := s.backtestPersister.PersistRun(ctx, config.ID, triggeredAt, time.Since(started), result); err != nil {
		s.logger.Error("scheduler: failed to persist backtest run",
			slog.String("backtest_config_id", config.ID.String()),
			slog.String("name", config.Name),
			slog.Any("error", err),
		)
		return
	}

	s.logger.Info("scheduler: backtest execution completed",
		slog.String("backtest_config_id", config.ID.String()),
		slog.String("name", config.Name),
	)
}

// discoveryDedupKey is a fixed UUID used as the dedup key for the singleton
// ticker discovery job.
var discoveryDedupKey = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func (s *Scheduler) runTickerDiscovery() {
	if s.metrics != nil {
		s.metrics.RecordSchedulerTick("discovery")
	}

	if !s.discoveryDedup.TryAcquire(discoveryDedupKey) {
		s.logger.Warn("scheduler: skipping ticker discovery; already in flight")
		return
	}
	defer s.discoveryDedup.Release(discoveryDedupKey)

	ctx, cancel := s.jobContext()
	defer cancel()

	td := s.tickerDiscovery
	logger := s.logger

	// Step 1: Weekly refresh of universe constituents (runs every Monday).
	weekday := s.nowFunc().Weekday()
	if weekday == time.Monday {
		count, err := td.universe.RefreshConstituents(ctx)
		if err != nil {
			logger.Error("scheduler: ticker discovery refresh failed", slog.Any("error", err))
		} else {
			logger.Info("scheduler: refreshed universe constituents", slog.Int("count", count))
		}
	}

	// Step 2: Run pre-market screen.
	scored, err := td.universe.RunPreMarketScreen(ctx, td.config.MinADV, td.config.MaxTickers)
	if err != nil {
		logger.Error("scheduler: pre-market screen failed", slog.Any("error", err))
		return
	}
	if len(scored) == 0 {
		logger.Info("scheduler: pre-market screen returned no tickers")
		return
	}

	// Step 3: Extract top N ticker symbols.
	maxTickers := td.config.MaxTickers
	if maxTickers <= 0 {
		maxTickers = 30
	}
	if maxTickers > len(scored) {
		maxTickers = len(scored)
	}
	tickers := make([]string, maxTickers)
	for i := 0; i < maxTickers; i++ {
		tickers[i] = scored[i].Ticker
	}

	// Step 4: Build DiscoveryConfig and run.
	cfg := discovery.DiscoveryConfig{
		Screener: discovery.ScreenerConfig{
			Tickers:    tickers,
			MinADV:     td.config.MinADV,
			MinATR:     0.5,
			MarketType: domain.MarketTypeStock,
		},
		Generator: discovery.GeneratorConfig{
			Provider:   td.discoveryDeps.LLMProvider,
			MaxRetries: 3,
		},
		Sweep: discovery.SweepConfig{
			InitialCash: 100000,
			Variations:  20,
		},
		Scoring:    discovery.DefaultScoringConfig(),
		Validation: discovery.ValidationConfig{},
		MaxWinners: 3,
	}

	result, err := discovery.RunDiscovery(ctx, cfg, td.discoveryDeps)
	if err != nil {
		logger.Error("scheduler: ticker discovery pipeline failed", slog.Any("error", err))
		return
	}

	logger.Info("scheduler: ticker discovery complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("deployed", result.Deployed),
		slog.Duration("duration", result.Duration),
	)
}

func (s *Scheduler) clearStateLocked() (cronEngine, context.CancelFunc) {
	engine := s.cron
	cancel := s.cancel
	s.cron = nil
	s.ctx = nil
	s.cancel = nil
	return engine, cancel
}

func (s *Scheduler) jobContext() (context.Context, context.CancelFunc) {
	s.mu.Lock()
	baseCtx := s.ctx
	timeout := s.jobTimeout
	s.mu.Unlock()

	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(baseCtx)
	}

	return context.WithTimeout(baseCtx, timeout)
}
