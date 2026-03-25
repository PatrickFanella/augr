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
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

const defaultStrategyPageSize = 100
const defaultJobTimeout = 10 * time.Minute

var ErrAlreadyStarted = errors.New("scheduler: already started")

type pipelineExecutor interface {
	Execute(ctx context.Context, strategyID uuid.UUID, ticker string) (*agent.PipelineState, error)
}

type cronEngine interface {
	AddFunc(spec string, cmd func()) (cron.EntryID, error)
	Start()
	Stop() context.Context
}

// Scheduler loads active strategies and triggers pipeline runs on cron schedules.
type Scheduler struct {
	mu           sync.Mutex
	cron         cronEngine
	strategyRepo repository.StrategyRepository
	pipeline     pipelineExecutor
	riskEngine   risk.RiskEngine
	logger       *slog.Logger
	nowFunc      func() time.Time
	newCron      func() cronEngine
	ctx          context.Context
	cancel       context.CancelFunc
	jobTimeout   time.Duration
}

// NewScheduler constructs a Scheduler with the supplied dependencies.
func NewScheduler(
	strategyRepo repository.StrategyRepository,
	pipeline pipelineExecutor,
	riskEngine risk.RiskEngine,
	logger *slog.Logger,
) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Scheduler{
		strategyRepo: strategyRepo,
		pipeline:     pipeline,
		riskEngine:   riskEngine,
		logger:       logger,
		nowFunc:      time.Now,
		newCron: func() cronEngine {
			return cron.New()
		},
		jobTimeout: defaultJobTimeout,
	}
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

	engine.Start()

	s.logger.Info("scheduler: started",
		slog.Int("active_strategies", len(strategies)),
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

func (s *Scheduler) runStrategy(strategy domain.Strategy) {
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

	if _, err := s.pipeline.Execute(ctx, strategy.ID, strategy.Ticker); err != nil {
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
