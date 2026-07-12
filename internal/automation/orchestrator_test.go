package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	kalshiexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	predictionexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
	kalshidiscovery "github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func TestJobOrchestratorRunJob_TracksFailureFieldsAndReset(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	shouldFail := true
	orch.Register("job", "test job", schedulerSpecEveryMinute(), func(context.Context) error {
		if shouldFail {
			return errors.New("boom")
		}
		return nil
	})

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob(first) error = %v", err)
	}
	waitForJobRuns(t, orch, "job", 1)

	status := singleJobStatus(t, orch, "job")
	if status.LastResult != "failed" {
		t.Fatalf("LastResult = %q, want failed", status.LastResult)
	}
	if status.LastError != "boom" {
		t.Fatalf("LastError = %q, want boom", status.LastError)
	}
	if status.LastErrorAt == nil {
		t.Fatal("LastErrorAt = nil, want timestamp")
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", status.ConsecutiveFailures)
	}

	shouldFail = false
	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob(second) error = %v", err)
	}
	waitForJobRuns(t, orch, "job", 2)

	status = singleJobStatus(t, orch, "job")
	if status.LastResult != "success" {
		t.Fatalf("LastResult = %q, want success", status.LastResult)
	}
	if status.LastError != "" {
		t.Fatalf("LastError = %q, want empty", status.LastError)
	}
	if status.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0", status.ConsecutiveFailures)
	}
}

func TestJobOrchestratorStatus_IncludesStuckForWhenRunning(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	started := make(chan struct{})
	release := make(chan struct{})
	orch.Register("job", "blocking job", schedulerSpecEveryMinute(), func(context.Context) error {
		close(started)
		<-release
		return nil
	})

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}

	status := singleJobStatus(t, orch, "job")
	if !status.Running {
		t.Fatal("Running = false, want true")
	}
	if status.StuckFor == nil || *status.StuckFor <= 0 {
		t.Fatalf("StuckFor = %v, want > 0", status.StuckFor)
	}

	close(release)
	waitForJobRuns(t, orch, "job", 1)
}

func TestJobOrchestratorRunJob_AutoDisablesAfterThreshold(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("job", "always fails", schedulerSpecEveryMinute(), func(context.Context) error {
		return errors.New("boom")
	})
	orch.SetConsecutiveFailures("job", autoDisableThreshold-1)

	if err := orch.RunJob(context.Background(), "job"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "job", 1)

	status := singleJobStatus(t, orch, "job")
	if status.ConsecutiveFailures != autoDisableThreshold {
		t.Fatalf("ConsecutiveFailures = %d, want %d", status.ConsecutiveFailures, autoDisableThreshold)
	}
	if status.Enabled {
		t.Fatal("Enabled = true, want false after reaching auto-disable threshold")
	}
}

func TestJobOrchestratorWrapAndRun_AutoDisabledJobsAreSkipped(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("job", "always fails", schedulerSpecEveryMinute(), func(context.Context) error {
		return errors.New("boom")
	})
	orch.SetConsecutiveFailures("job", autoDisableThreshold-1)

	job := orch.jobs["job"]
	orch.wrapAndRun(job)

	status := singleJobStatus(t, orch, "job")
	if status.ConsecutiveFailures != autoDisableThreshold {
		t.Fatalf("ConsecutiveFailures = %d, want %d", status.ConsecutiveFailures, autoDisableThreshold)
	}
	if status.Enabled {
		t.Fatal("Enabled = true, want false after reaching auto-disable threshold")
	}
	if status.RunCount != 1 {
		t.Fatalf("RunCount after first run = %d, want 1", status.RunCount)
	}

	orch.wrapAndRun(job)
	status = singleJobStatus(t, orch, "job")
	if status.RunCount != 1 {
		t.Fatalf("RunCount after disabled scheduled invocation = %d, want 1", status.RunCount)
	}
}

type stubAutomationMetrics struct {
	alpacaRuns map[string]int
}

func (m *stubAutomationMetrics) RecordAutomationJobError(string) {}

func (m *stubAutomationMetrics) RecordAlpacaReconcileRun(result string) {
	if m.alpacaRuns == nil {
		m.alpacaRuns = make(map[string]int)
	}
	m.alpacaRuns[result]++
}
func (m *stubAutomationMetrics) RecordKalshiReconcileRun(result string) {
	m.RecordAlpacaReconcileRun("kalshi_" + result)
}

func TestJobOrchestratorStatus_IncludesLastSummary(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.Register("alpaca_reconcile", "test job", schedulerSpecEveryMinute(), func(context.Context) error { return nil })
	orch.SetLastSummary("alpaca_reconcile", map[string]int{"orders_created": 2, "trades_created": 3})

	status := singleJobStatus(t, orch, "alpaca_reconcile")
	if status.LastSummary == nil {
		t.Fatal("LastSummary = nil, want populated")
	}
	if status.LastSummary["orders_created"] != 2 {
		t.Fatalf("orders_created = %d, want 2", status.LastSummary["orders_created"])
	}
	status.LastSummary["orders_created"] = 99
	statusAgain := singleJobStatus(t, orch, "alpaca_reconcile")
	if statusAgain.LastSummary["orders_created"] != 2 {
		t.Fatalf("mutated summary leaked into orchestrator: %d", statusAgain.LastSummary["orders_created"])
	}
}

func TestJobOrchestratorRegisterAllAddsCurrentDataRefreshBeforeHotScan(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.RegisterAll()

	if _, ok := orch.jobs["current_data_refresh"]; !ok {
		t.Fatal("current_data_refresh job not registered")
	}
	hotScan, ok := orch.jobs["hot_scan"]
	if !ok {
		t.Fatal("hot_scan job not registered")
	}
	if len(hotScan.DependsOn) != 1 || hotScan.DependsOn[0] != "current_data_refresh" {
		t.Fatalf("hot_scan depends_on = %#v, want [current_data_refresh]", hotScan.DependsOn)
	}
}

func TestJobOrchestratorRegisterAllAddsPolymarketReconcile(t *testing.T) {
	t.Parallel()

	reconciler := polymarketexecution.NewReconciler(polymarketexecution.ReconcilerDeps{
		Broker: &polymarketBrokerStub{positions: []domain.Position{{Ticker: "market-one:YES", Side: domain.PositionSideLong, Quantity: 10}}},
		PositionRepo: &polymarketPositionRepoStub{positions: []domain.Position{{
			MarketType: domain.MarketTypePolymarket,
			Ticker:     "market-one",
			Side:       domain.PositionSideLong,
			Quantity:   10,
		}}},
		AuditLogRepo: &polymarketAuditRepoStub{},
		Metrics:      &polymarketReconcilerMetricsStub{},
		Logger:       slog.Default(),
	})
	orch := NewJobOrchestrator(OrchestratorDeps{PolymarketReconciler: reconciler})
	orch.RegisterAll()

	status := singleJobStatus(t, orch, "polymarket_reconcile")
	if status.Schedule == "" {
		t.Fatal("polymarket_reconcile schedule is empty")
	}

	if err := orch.RunJob(context.Background(), "polymarket_reconcile"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "polymarket_reconcile", 1)

	status = singleJobStatus(t, orch, "polymarket_reconcile")
	if status.LastResult != "success" {
		t.Fatalf("LastResult = %q, want success", status.LastResult)
	}
	if status.LastSummary == nil || status.LastSummary["drifts"] != 0 {
		t.Fatalf("LastSummary = %#v, want drifts=0", status.LastSummary)
	}
}

func TestJobOrchestratorRegisterAllCanDisablePolymarketAutomation(t *testing.T) {
	t.Parallel()

	reconciler := polymarketexecution.NewReconciler(polymarketexecution.ReconcilerDeps{
		Broker:       &polymarketBrokerStub{},
		PositionRepo: &polymarketPositionRepoStub{},
		AuditLogRepo: &polymarketAuditRepoStub{},
		Metrics:      &polymarketReconcilerMetricsStub{},
		Logger:       slog.Default(),
	})
	orch := NewJobOrchestrator(OrchestratorDeps{
		PolymarketReconciler:        reconciler,
		DisablePolymarketAutomation: true,
	})
	orch.RegisterAll()

	for _, name := range []string{"polymarket_profiles", "polymarket_reconcile", "polymarket_resolutions", "polymarket_strategy_discovery"} {
		if _, ok := orch.jobs[name]; ok {
			t.Fatalf("job %q registered with DisablePolymarketAutomation=true", name)
		}
	}
}

func TestJobOrchestratorRegisterAllAddsKalshiDiscovery(t *testing.T) {
	t.Parallel()

	origRun := kalshiDiscoveryRun
	defer func() { kalshiDiscoveryRun = origRun }()

	var gotCfg kalshidiscovery.Config
	var gotDeps kalshidiscovery.Deps
	kalshiDiscoveryRun = func(_ context.Context, cfg kalshidiscovery.Config, deps kalshidiscovery.Deps) (*kalshidiscovery.Result, error) {
		gotCfg = cfg
		gotDeps = deps
		return &kalshidiscovery.Result{}, nil
	}

	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo:              &kalshiStrategyRepoStub{},
		KalshiCatalog:             kalshiCatalogStub{},
		KalshiWatchedRepo:         &kalshiWatchedRepoStub{},
		KalshiMarketSnapshotsRepo: &kalshiSnapshotsRepoStub{},
		KalshiDiscoveryRuns:       &kalshiDiscoveryRunsRepoStub{},
	})
	orch.RegisterAll()

	status := singleJobStatus(t, orch, "kalshi_discovery")
	if status.Schedule == "" || !strings.Contains(status.Schedule, "15 * * * *") {
		t.Fatalf("kalshi_discovery schedule = %q, want cron 15 * * * *", status.Schedule)
	}

	if err := orch.RunJob(context.Background(), "kalshi_discovery"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "kalshi_discovery", 1)

	if gotCfg.DryRun {
		t.Fatal("Kalshi discovery cfg.DryRun = true, want false")
	}
	if gotCfg.FetchLimit != 50 || gotCfg.MaxDeployments != 1 || gotCfg.MinConviction != 0.70 {
		t.Fatalf("Kalshi discovery cfg = %#v, want conservative paper settings", gotCfg)
	}
	if gotCfg.Screener.MaxCandidates != 15 || gotCfg.Screener.MinVolume != 1000 || gotCfg.Screener.MinOpenInterest != 500 || gotCfg.Screener.MaxSpreadPct != 12 || gotCfg.Screener.MinDaysToClose != 3 {
		t.Fatalf("Kalshi discovery screener = %#v, want default conservative config", gotCfg.Screener)
	}
	if gotDeps.Catalog == nil || gotDeps.Strategies == nil || gotDeps.Watched == nil || gotDeps.Snapshots == nil || gotDeps.DiscoveryRuns == nil {
		t.Fatalf("Kalshi discovery deps = %#v, want all persistence dependencies wired", gotDeps)
	}
	if gotDeps.Logger == nil {
		t.Fatal("Kalshi discovery logger = nil")
	}
}

func TestJobOrchestratorRegisterAllAddsKalshiSettlement(t *testing.T) {
	t.Parallel()
	orch := NewJobOrchestrator(OrchestratorDeps{
		KalshiCatalog:     kalshiCatalogStub{},
		PredictionSettler: predictionexecution.NewSettler(nil, nil, nil, nil),
	})
	orch.RegisterAll()
	status := singleJobStatus(t, orch, "kalshi_settlement")
	if status.Schedule == "" || status.Schedule == "Manual only" {
		t.Fatalf("kalshi settlement schedule = %q", status.Schedule)
	}
}

func TestJobOrchestratorRegisterAllAddsKalshiReconciliation(t *testing.T) {
	t.Parallel()
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiReconciler: kalshiexecution.NewReconciler(kalshiexecution.ReconcilerDeps{})})
	orch.RegisterAll()
	status := singleJobStatus(t, orch, "kalshi_reconcile")
	if status.Schedule == "" || status.Schedule == "Manual only" {
		t.Fatalf("kalshi reconciliation schedule = %q", status.Schedule)
	}
}

func TestStrategyResweepSkipsKalshiBeforeOHLCVDownload(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{
		StrategyRepo: &kalshiStrategyRepoStub{strategies: []domain.Strategy{
			{
				ID:         uuid.New(),
				Name:       "auto: kalshi KXMENWORLDCUP-26-US",
				Ticker:     "KXMENWORLDCUP-26-US",
				MarketType: domain.MarketTypeKalshi,
				Status:     domain.StrategyStatusActive,
				IsPaper:    true,
			},
		}},
	})

	if err := orch.strategyResweep(context.Background()); err != nil {
		t.Fatalf("strategyResweep() error = %v", err)
	}
}

func TestSupportsOHLCVResweep(t *testing.T) {
	t.Parallel()

	for _, marketType := range []domain.MarketType{domain.MarketTypeStock, domain.MarketTypeCrypto} {
		if !eventmarkets.SupportsOHLCVResweep(marketType) {
			t.Fatalf("SupportsOHLCVResweep(%q) = false, want true", marketType)
		}
	}
	for _, marketType := range []domain.MarketType{domain.MarketTypeKalshi, domain.MarketTypePolymarket, domain.MarketTypeOptions} {
		if eventmarkets.SupportsOHLCVResweep(marketType) {
			t.Fatalf("SupportsOHLCVResweep(%q) = true, want false", marketType)
		}
	}
}

func TestJobOrchestratorAlpacaReconcileRecordsMetricsAndSummary(t *testing.T) {
	t.Parallel()

	metrics := &stubAutomationMetrics{}
	orch := NewJobOrchestrator(OrchestratorDeps{Logger: slog.Default()})
	orch.WithJobMetrics(metrics)
	orch.Register("alpaca_reconcile", "test job", schedulerSpecEveryMinute(), func(context.Context) error {
		orch.SetLastSummary("alpaca_reconcile", map[string]int{"orders_created": 1})
		metrics.RecordAlpacaReconcileRun("success")
		return nil
	})

	if err := orch.RunJob(context.Background(), "alpaca_reconcile"); err != nil {
		t.Fatalf("RunJob() error = %v", err)
	}
	waitForJobRuns(t, orch, "alpaca_reconcile", 1)

	status := singleJobStatus(t, orch, "alpaca_reconcile")
	if status.LastSummary == nil || status.LastSummary["orders_created"] != 1 {
		t.Fatalf("LastSummary = %#v, want orders_created=1", status.LastSummary)
	}
	if metrics.alpacaRuns["success"] != 1 {
		t.Fatalf("alpaca success runs = %d, want 1", metrics.alpacaRuns["success"])
	}
}

func waitForJobRuns(t *testing.T, orch *JobOrchestrator, jobName string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := singleJobStatus(t, orch, jobName)
		if status.RunCount >= want && !status.Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach run_count=%d", jobName, want)
}

func singleJobStatus(t *testing.T, orch *JobOrchestrator, jobName string) JobStatus {
	t.Helper()
	for _, status := range orch.Status() {
		if status.Name == jobName {
			return status
		}
	}
	t.Fatalf("job status %q not found", jobName)
	return JobStatus{}
}

func schedulerSpecEveryMinute() scheduler.ScheduleSpec {
	return scheduler.ScheduleSpec{Cron: "* * * * *", Type: scheduler.ScheduleTypeCron}
}

type kalshiCatalogStub struct{}

func (kalshiCatalogStub) ListMarkets(context.Context, kalshidiscovery.ListOptions) ([]kalshidiscovery.MarketCandidate, string, error) {
	return nil, "", nil
}

type kalshiStrategyRepoStub struct {
	strategies []domain.Strategy
}

func (s *kalshiStrategyRepoStub) Create(context.Context, *domain.Strategy) error { return nil }
func (s *kalshiStrategyRepoStub) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}
func (s *kalshiStrategyRepoStub) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return append([]domain.Strategy(nil), s.strategies...), nil
}
func (s *kalshiStrategyRepoStub) Count(context.Context, repository.StrategyFilter) (int, error) {
	return 0, nil
}
func (s *kalshiStrategyRepoStub) Update(context.Context, *domain.Strategy) error { return nil }
func (s *kalshiStrategyRepoStub) Delete(context.Context, uuid.UUID) error        { return nil }
func (s *kalshiStrategyRepoStub) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}
func (s *kalshiStrategyRepoStub) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type kalshiWatchedRepoStub struct{}

func (s *kalshiWatchedRepoStub) Upsert(context.Context, *domain.KalshiWatchedMarket) error {
	return nil
}
func (s *kalshiWatchedRepoStub) SetEnabled(context.Context, string, bool) error { return nil }
func (s *kalshiWatchedRepoStub) ListEnabled(context.Context) ([]domain.KalshiWatchedMarket, error) {
	return nil, nil
}

type kalshiSnapshotsRepoStub struct{}

func (s *kalshiSnapshotsRepoStub) Create(context.Context, *domain.KalshiMarketSnapshot) error {
	return nil
}
func (s *kalshiSnapshotsRepoStub) ListLatestByTicker(context.Context, string, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}
func (s *kalshiSnapshotsRepoStub) ListRecent(context.Context, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

type kalshiDiscoveryRunsRepoStub struct{}

func (s *kalshiDiscoveryRunsRepoStub) Create(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}
func (s *kalshiDiscoveryRunsRepoStub) GetActive(context.Context) (*domain.KalshiDiscoveryRun, error) {
	return nil, repository.ErrNotFound
}
func (s *kalshiDiscoveryRunsRepoStub) Finish(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}
func (s *kalshiDiscoveryRunsRepoStub) ListLatest(context.Context, int) ([]domain.KalshiDiscoveryRun, error) {
	return nil, nil
}

type polymarketBrokerStub struct {
	positions []domain.Position
}

func (s *polymarketBrokerStub) SubmitOrder(context.Context, *domain.Order) (string, error) {
	return "", nil
}

func (s *polymarketBrokerStub) CancelOrder(context.Context, string) error { return nil }

func (s *polymarketBrokerStub) GetOrderStatus(context.Context, string) (domain.OrderStatus, error) {
	return "", nil
}

func (s *polymarketBrokerStub) GetPositions(context.Context) ([]domain.Position, error) {
	return append([]domain.Position(nil), s.positions...), nil
}

func (s *polymarketBrokerStub) GetAccountBalance(context.Context) (execution.Balance, error) {
	return execution.Balance{}, nil
}

type polymarketPositionRepoStub struct {
	positions []domain.Position
}

func (s *polymarketPositionRepoStub) Create(context.Context, *domain.Position) error { return nil }
func (s *polymarketPositionRepoStub) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}
func (s *polymarketPositionRepoStub) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (s *polymarketPositionRepoStub) Count(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}
func (s *polymarketPositionRepoStub) Update(context.Context, *domain.Position) error { return nil }
func (s *polymarketPositionRepoStub) Delete(context.Context, uuid.UUID) error        { return nil }
func (s *polymarketPositionRepoStub) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return append([]domain.Position(nil), s.positions...), nil
}
func (s *polymarketPositionRepoStub) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return len(s.positions), nil
}
func (s *polymarketPositionRepoStub) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

type polymarketAuditRepoStub struct{}

func (s *polymarketAuditRepoStub) Create(context.Context, *domain.AuditLogEntry) error { return nil }
func (s *polymarketAuditRepoStub) Query(context.Context, repository.AuditLogFilter, int, int) ([]domain.AuditLogEntry, error) {
	return nil, nil
}
func (s *polymarketAuditRepoStub) Count(context.Context, repository.AuditLogFilter) (int, error) {
	return 0, nil
}

type polymarketReconcilerMetricsStub struct{}

func (s *polymarketReconcilerMetricsStub) IncDrift(string) {}
