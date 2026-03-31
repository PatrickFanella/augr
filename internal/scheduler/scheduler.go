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
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
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

type Option func(*Scheduler)

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

// Scheduler loads active strategies and triggers pipeline runs on cron schedules.
type Scheduler struct {
	mu                 sync.Mutex
	cron               cronEngine
	strategyRepo       repository.StrategyRepository
	pipeline           pipelineExecutor
	riskEngine         risk.RiskEngine
	backtestConfigRepo repository.BacktestConfigRepository
	backtestPersister  backtest.BacktestPersister
	backtestRunner     backtestRunner
	logger             *slog.Logger
	nowFunc            func() time.Time
	newCron            func() cronEngine
	ctx                context.Context
	cancel             context.CancelFunc
	jobTimeout         time.Duration
	dedup              strategyDedup
	backtestDedup      strategyDedup
	riskMonitor        *riskMonitor
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

	return s
}

// Start loads all active strategies, registers cron jobs, and starts the scheduler.
func (s *Scheduler) Start() error {
	if s.strategyRepo == nil {
		return fmt.Errorf("scheduler: strategy repository is required")
	}
	if s.pipeline == nil {
		return fmt.Errorf("scheduler: pipeline is required")
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

	for _, strategy := range strategies {
		spec := strings.TrimSpace(strategy.ScheduleCron)
		if spec == "" {
			continue
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

	engine.Start()

	s.logger.Info("scheduler: started",
		slog.Int("active_strategies", len(strategies)),
		slog.Int("scheduled_backtests", len(backtests)),
		slog.Int("registered_jobs", registered),
	)

	return nil
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
	active := true
	filter := repository.StrategyFilter{IsActive: &active}

	var strategies []domain.Strategy
	for offset := 0; ; offset += defaultStrategyPageSize {
		batch, err := s.strategyRepo.List(ctx, filter, defaultStrategyPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("scheduler: list active strategies: %w", err)
		}

		strategies = append(strategies, batch...)
		if len(batch) < defaultStrategyPageSize {
			break
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
	// Dedup: skip if this strategy is already running.
	if !s.dedup.TryAcquire(strategy.ID) {
		s.logger.Warn("scheduler: skipping strategy; already in flight",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
		)
		return
	}
	defer s.dedup.Release(strategy.ID)

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

	if _, err := s.pipeline.Execute(monCtx, strategy.ID, strategy.Ticker); err != nil {
		s.logger.Error("scheduler: pipeline execution failed",
			slog.String("strategy_id", strategy.ID.String()),
			slog.String("ticker", strategy.Ticker),
			slog.Any("error", err),
		)
		return
	}

	s.logger.Info("scheduler: pipeline execution completed",
		slog.String("strategy_id", strategy.ID.String()),
		slog.String("ticker", strategy.Ticker),
	)
}

func (s *Scheduler) runBacktest(config domain.BacktestConfig) {
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
