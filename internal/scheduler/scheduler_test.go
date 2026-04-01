package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
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
	disabledJobTimeout = time.Duration(0)
	testScheduleSpec   = "@every 1m"
)

type mockStrategyRepo struct {
	mu         sync.Mutex
	strategies []domain.Strategy
	filters    []repository.StrategyFilter
	listErr    error
}

func (m *mockStrategyRepo) Create(context.Context, *domain.Strategy) error { return nil }

func (m *mockStrategyRepo) Get(context.Context, uuid.UUID) (*domain.Strategy, error) { return nil, nil }

func (m *mockStrategyRepo) List(_ context.Context, filter repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.filters = append(m.filters, filter)
	if m.listErr != nil {
		return nil, m.listErr
	}
	if offset >= len(m.strategies) {
		return nil, nil
	}

	end := offset + limit
	if end > len(m.strategies) {
		end = len(m.strategies)
	}

	return append([]domain.Strategy(nil), m.strategies[offset:end]...), nil
}

func (m *mockStrategyRepo) Update(context.Context, *domain.Strategy) error { return nil }

func (m *mockStrategyRepo) Delete(context.Context, uuid.UUID) error { return nil }

func (m *mockStrategyRepo) lastFilter() (repository.StrategyFilter, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.filters) == 0 {
		return repository.StrategyFilter{}, false
	}

	return m.filters[len(m.filters)-1], true
}

type pipelineCall struct {
	strategyID uuid.UUID
	ticker     string
}

type mockPipeline struct {
	mu    sync.Mutex
	calls []pipelineCall
	err   error
	ctxs  []context.Context
}

func (m *mockPipeline) Execute(ctx context.Context, strategyID uuid.UUID, ticker string) (*agent.PipelineState, error) {
	call := pipelineCall{strategyID: strategyID, ticker: ticker}

	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.ctxs = append(m.ctxs, ctx)
	m.mu.Unlock()

	return &agent.PipelineState{}, m.err
}

func (m *mockPipeline) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockPipeline) firstContext() (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ctxs) == 0 {
		return nil, false
	}
	return m.ctxs[0], true
}

type mockBacktestConfigRepo struct {
	mu      sync.Mutex
	configs []domain.BacktestConfig
	filters []repository.BacktestConfigFilter
	listErr error
}

func (m *mockBacktestConfigRepo) Create(context.Context, *domain.BacktestConfig) error { return nil }

func (m *mockBacktestConfigRepo) Get(context.Context, uuid.UUID) (*domain.BacktestConfig, error) {
	return nil, nil
}

func (m *mockBacktestConfigRepo) List(_ context.Context, filter repository.BacktestConfigFilter, limit, offset int) ([]domain.BacktestConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.filters = append(m.filters, filter)
	if m.listErr != nil {
		return nil, m.listErr
	}
	if offset >= len(m.configs) {
		return nil, nil
	}

	end := offset + limit
	if end > len(m.configs) {
		end = len(m.configs)
	}

	return append([]domain.BacktestConfig(nil), m.configs[offset:end]...), nil
}

func (m *mockBacktestConfigRepo) Update(context.Context, *domain.BacktestConfig) error { return nil }

func (m *mockBacktestConfigRepo) Delete(context.Context, uuid.UUID) error { return nil }

type mockBacktestRunRepo struct {
	mu        sync.Mutex
	runs      []*domain.BacktestRun
	createErr error
}

func (m *mockBacktestRunRepo) Create(_ context.Context, run *domain.BacktestRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}

	copied := *run
	if copied.ID == uuid.Nil {
		copied.ID = uuid.New()
	}
	if copied.CreatedAt.IsZero() {
		copied.CreatedAt = time.Now().UTC()
	}
	if copied.UpdatedAt.IsZero() {
		copied.UpdatedAt = copied.CreatedAt
	}
	m.runs = append(m.runs, &copied)
	run.ID = copied.ID
	run.CreatedAt = copied.CreatedAt
	run.UpdatedAt = copied.UpdatedAt
	return nil
}

func (m *mockBacktestRunRepo) Get(context.Context, uuid.UUID) (*domain.BacktestRun, error) {
	return nil, nil
}

func (m *mockBacktestRunRepo) List(context.Context, repository.BacktestRunFilter, int, int) ([]domain.BacktestRun, error) {
	return nil, nil
}

func (m *mockBacktestRunRepo) firstRun() (*domain.BacktestRun, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runs) == 0 {
		return nil, false
	}
	copied := *m.runs[0]
	return &copied, true
}

type mockBacktestRunner struct {
	mu     sync.Mutex
	calls  []domain.BacktestConfig
	result *backtest.OrchestratorResult
	err    error
	ctxs   []context.Context
}

func (m *mockBacktestRunner) Run(ctx context.Context, config domain.BacktestConfig) (*backtest.OrchestratorResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, config)
	m.ctxs = append(m.ctxs, ctx)
	return m.result, m.err
}

func (m *mockBacktestRunner) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockRiskEngine struct {
	killSwitchActive bool
	killSwitchErr    error
	blockKillSwitch  bool
	enteredCh        chan struct{}
	enteredOnce      sync.Once
	mu               sync.Mutex
	ctxs             []context.Context
}

func (m *mockRiskEngine) CheckPreTrade(context.Context, *domain.Order, risk.Portfolio) (bool, string, error) {
	return true, "", nil
}

func (m *mockRiskEngine) CheckPositionLimits(context.Context, string, float64, risk.Portfolio) (bool, string, error) {
	return true, "", nil
}

func (m *mockRiskEngine) GetStatus(context.Context) (risk.EngineStatus, error) {
	return risk.EngineStatus{}, nil
}

func (m *mockRiskEngine) TripCircuitBreaker(context.Context, string) error { return nil }

func (m *mockRiskEngine) ResetCircuitBreaker(context.Context) error { return nil }

func (m *mockRiskEngine) IsKillSwitchActive(ctx context.Context) (bool, error) {
	m.mu.Lock()
	m.ctxs = append(m.ctxs, ctx)
	m.mu.Unlock()
	m.enteredOnce.Do(func() {
		if m.enteredCh != nil {
			close(m.enteredCh)
		}
	})
	if m.blockKillSwitch {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return m.killSwitchActive, m.killSwitchErr
}

func (m *mockRiskEngine) ActivateKillSwitch(context.Context, string) error { return nil }

func (m *mockRiskEngine) DeactivateKillSwitch(context.Context) error { return nil }

func (m *mockRiskEngine) UpdateMetrics(context.Context, float64, float64, int) error { return nil }

func (m *mockRiskEngine) firstContext() (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ctxs) == 0 {
		return nil, false
	}
	return m.ctxs[0], true
}

type fakeCronEngine struct {
	mu      sync.Mutex
	jobs    []func()
	started atomic.Bool
	wg      sync.WaitGroup
}

func (f *fakeCronEngine) AddFunc(_ string, cmd func()) (cron.EntryID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, cmd)
	return cron.EntryID(len(f.jobs)), nil
}

func (f *fakeCronEngine) Start() {
	f.started.Store(true)
}

func (f *fakeCronEngine) Stop() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		f.wg.Wait()
		cancel()
	}()
	return ctx
}

func (f *fakeCronEngine) Run(index int) {
	f.mu.Lock()
	job := f.jobs[index]
	f.mu.Unlock()

	f.wg.Add(1)
	defer f.wg.Done()
	job()
}

func (f *fakeCronEngine) jobCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.jobs)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSchedulerStartTriggersPipelineExecution(t *testing.T) {
	strategyID := uuid.New()
	repo := &mockStrategyRepo{
		strategies: []domain.Strategy{
			{
				ID:           strategyID,
				Ticker:       "BTCUSD",
				MarketType:   domain.MarketTypeCrypto,
				ScheduleCron: testScheduleSpec,
				Status:       domain.StrategyStatusActive,
			},
		},
	}
	fakeCron := &fakeCronEngine{}
	pipeline := &mockPipeline{}
	s := NewScheduler(repo, pipeline, &mockRiskEngine{}, testLogger())
	s.newCron = func() cronEngine { return fakeCron }

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	filter, ok := repo.lastFilter()
	if !ok {
		t.Fatal("expected strategy repository List to be called")
	}
	if filter.Status != domain.StrategyStatusActive {
		t.Fatalf("expected active strategy filter, got %+v", filter)
	}
	if !fakeCron.started.Load() {
		t.Fatal("expected cron engine to be started")
	}
	if got := fakeCron.jobCount(); got != 1 {
		t.Fatalf("registered jobs = %d, want 1", got)
	}

	fakeCron.Run(0)

	if got := pipeline.callCount(); got != 1 {
		t.Fatalf("pipeline calls = %d, want 1", got)
	}
	call := pipeline.calls[0]
	if call.strategyID != strategyID {
		t.Fatalf("pipeline strategyID = %s, want %s", call.strategyID, strategyID)
	}
	if call.ticker != "BTCUSD" {
		t.Fatalf("pipeline ticker = %q, want %q", call.ticker, "BTCUSD")
	}
	ctx, ok := pipeline.firstContext()
	if !ok {
		t.Fatal("expected pipeline context to be recorded")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		t.Fatal("expected pipeline context to carry a deadline")
	}
}

func TestSchedulerRunStrategySkipsWhenKillSwitchActive(t *testing.T) {
	pipeline := &mockPipeline{}
	s := NewScheduler(nil, pipeline, &mockRiskEngine{killSwitchActive: true}, testLogger())

	s.runStrategy(domain.Strategy{
		ID:         uuid.New(),
		Ticker:     "BTCUSD",
		MarketType: domain.MarketTypeCrypto,
	})

	if got := pipeline.callCount(); got != 0 {
		t.Fatalf("pipeline calls = %d, want 0", got)
	}
}

func TestSchedulerStartIsIdempotentWhenAlreadyStarted(t *testing.T) {
	repo := &mockStrategyRepo{
		strategies: []domain.Strategy{
			{
				ID:           uuid.New(),
				Ticker:       "BTCUSD",
				MarketType:   domain.MarketTypeCrypto,
				ScheduleCron: testScheduleSpec,
				Status:       domain.StrategyStatusActive,
			},
		},
	}
	s := NewScheduler(repo, &mockPipeline{}, &mockRiskEngine{}, testLogger())
	s.newCron = func() cronEngine { return &fakeCronEngine{} }

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.Start()
		}()
	}
	wg.Wait()
	close(results)
	defer s.Stop()

	var successCount, alreadyStartedCount int
	for err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrAlreadyStarted):
			alreadyStartedCount++
		default:
			t.Fatalf("unexpected Start() error: %v", err)
		}
	}

	if successCount != 1 || alreadyStartedCount != 1 {
		t.Fatalf("successCount=%d alreadyStartedCount=%d, want 1 and 1", successCount, alreadyStartedCount)
	}
}

func TestSchedulerStopCancelsRunningJobs(t *testing.T) {
	repo := &mockStrategyRepo{
		strategies: []domain.Strategy{
			{
				ID:           uuid.New(),
				Ticker:       "BTCUSD",
				MarketType:   domain.MarketTypeCrypto,
				ScheduleCron: testScheduleSpec,
				Status:       domain.StrategyStatusActive,
			},
		},
	}
	fakeCron := &fakeCronEngine{}
	riskEngine := &mockRiskEngine{
		blockKillSwitch: true,
		enteredCh:       make(chan struct{}),
	}
	s := NewScheduler(repo, &mockPipeline{}, riskEngine, testLogger())
	s.newCron = func() cronEngine { return fakeCron }
	s.jobTimeout = disabledJobTimeout

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		fakeCron.Run(0)
		close(done)
	}()

	select {
	case <-riskEngine.enteredCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job to start")
	}
	ctx, ok := riskEngine.firstContext()
	if !ok {
		t.Fatal("expected risk engine context to be recorded")
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Fatal("expected scheduler job timeout value 0 to disable deadlines on the derived context")
	}

	stopDone := make(chan struct{})
	go func() {
		s.Stop()
		close(stopDone)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for running job to stop")
	}

	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Stop() to return")
	}
}

func TestSchedulerRunStrategySkipsOutsideStockMarketHours(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation(America/New_York): %v", err)
	}

	pipeline := &mockPipeline{}
	s := NewScheduler(nil, pipeline, &mockRiskEngine{}, testLogger())
	s.nowFunc = func() time.Time {
		return time.Date(2024, time.January, 6, 10, 0, 0, 0, loc)
	}

	s.runStrategy(domain.Strategy{
		ID:         uuid.New(),
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
	})

	if got := pipeline.callCount(); got != 0 {
		t.Fatalf("pipeline calls = %d, want 0", got)
	}
}

func TestSchedulerStartTriggersScheduledBacktestAndPersistsRun(t *testing.T) {
	configID := uuid.New()
	triggeredAt := time.Date(2026, time.March, 25, 2, 0, 0, 0, time.UTC)

	backtestRepo := &mockBacktestConfigRepo{
		configs: []domain.BacktestConfig{
			{
				ID:           configID,
				StrategyID:   uuid.New(),
				Name:         "Nightly benchmark",
				ScheduleCron: "@daily",
				StartDate:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndDate:      time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
				Simulation: domain.BacktestSimulationParameters{
					InitialCapital: 100000,
				},
			},
		},
	}
	runRepo := &mockBacktestRunRepo{}
	persister := backtest.NewRepoPersister(runRepo)
	runner := &mockBacktestRunner{
		result: &backtest.OrchestratorResult{
			Metrics: backtest.Metrics{
				TotalReturn: 0.12,
				SharpeRatio: 1.8,
			},
			Trades: []domain.Trade{
				{Ticker: "BTCUSD"},
			},
			EquityCurve: []backtest.EquityPoint{
				{Timestamp: triggeredAt, Equity: 100000},
			},
			PromptVersion:     "prompt-v1",
			PromptVersionHash: "hash-v1",
		},
	}
	fakeCron := &fakeCronEngine{}
	s := NewScheduler(
		&mockStrategyRepo{},
		&mockPipeline{},
		&mockRiskEngine{},
		testLogger(),
		WithBacktestScheduling(backtestRepo, persister, runner),
	)
	s.newCron = func() cronEngine { return fakeCron }
	s.nowFunc = func() time.Time { return triggeredAt }

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	if got := fakeCron.jobCount(); got != 1 {
		t.Fatalf("registered jobs = %d, want 1", got)
	}

	fakeCron.Run(0)

	if got := runner.callCount(); got != 1 {
		t.Fatalf("backtest runner calls = %d, want 1", got)
	}

	run, ok := runRepo.firstRun()
	if !ok {
		t.Fatal("expected persisted backtest run")
	}
	if run.BacktestConfigID != configID {
		t.Fatalf("run BacktestConfigID = %s, want %s", run.BacktestConfigID, configID)
	}
	if !run.RunTimestamp.Equal(triggeredAt) {
		t.Fatalf("run RunTimestamp = %s, want %s", run.RunTimestamp, triggeredAt)
	}
	if run.Duration < 0 {
		t.Fatalf("run Duration = %s, want non-negative", run.Duration)
	}
	if run.PromptVersion != "prompt-v1" {
		t.Fatalf("run PromptVersion = %q, want %q", run.PromptVersion, "prompt-v1")
	}
	if run.PromptVersionHash != "hash-v1" {
		t.Fatalf("run PromptVersionHash = %q, want %q", run.PromptVersionHash, "hash-v1")
	}

	var metrics backtest.Metrics
	if err := json.Unmarshal(run.Metrics, &metrics); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}
	if metrics.TotalReturn != 0.12 {
		t.Fatalf("metrics TotalReturn = %v, want %v", metrics.TotalReturn, 0.12)
	}

	var trades []domain.Trade
	if err := json.Unmarshal(run.TradeLog, &trades); err != nil {
		t.Fatalf("unmarshal trade log: %v", err)
	}
	if len(trades) != 1 || trades[0].Ticker != "BTCUSD" {
		t.Fatalf("trade log = %+v, want one BTCUSD trade", trades)
	}

	var curve []backtest.EquityPoint
	if err := json.Unmarshal(run.EquityCurve, &curve); err != nil {
		t.Fatalf("unmarshal equity curve: %v", err)
	}
	if len(curve) != 1 || !curve[0].Timestamp.Equal(triggeredAt) {
		t.Fatalf("equity curve = %+v, want timestamp %s", curve, triggeredAt)
	}
}
