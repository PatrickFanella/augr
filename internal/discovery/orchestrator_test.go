package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestCheckpointCandidateRoundTrip(t *testing.T) {
	screened := []ScreenResult{{
		Ticker: "AAPL",
		Bars: []domain.OHLCV{{
			Timestamp: time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
			Open:      1,
			High:      2,
			Low:       1,
			Close:     2,
			Volume:    100,
		}},
		Indicators: []domain.Indicator{{Name: "rsi_14", Value: 55}},
		Close:      2,
		ADV:        100,
		ATR:        1,
	}}

	checkpoint := CheckpointCandidatesFromScreenResults(screened)
	got := ScreenResultsFromCheckpointCandidates(checkpoint)
	if len(got) != 1 || got[0].Ticker != "AAPL" || got[0].Indicators[0].Name != "rsi_14" {
		t.Fatalf("round trip failed: %#v", got)
	}
}

func TestDiscoveryCanDeployOnlyCompleteResults(t *testing.T) {
	t.Parallel()
	if discoveryCanDeploy(nil) {
		t.Fatal("discoveryCanDeploy(nil) = true")
	}
	if discoveryCanDeploy(&DiscoveryResult{Errors: []string{"generation failed"}}) {
		t.Fatal("discoveryCanDeploy(partial result) = true")
	}
	if !discoveryCanDeploy(&DiscoveryResult{}) {
		t.Fatal("discoveryCanDeploy(complete result) = false")
	}
}

func TestRecordDiscoveryDeploymentOutcomeSeparatesCreateReuseAndDryRun(t *testing.T) {
	result := &DiscoveryResult{}
	recordDiscoveryDeploymentOutcome(result, false, true)
	recordDiscoveryDeploymentOutcome(result, false, false)
	recordDiscoveryDeploymentOutcome(result, true, false)
	if result.Proposed != 3 || result.Created != 1 || result.Reused != 1 || result.Deployed != 1 {
		t.Fatalf("deployment outcome = %+v", result)
	}
}

func TestCreateOrReuseDiscoveryStrategyStagesNewStrategyUntilBacktestConfigExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	strategies := newInMemoryStrategyRepo()
	configs := newDiscoveryBacktestConfigRepo()
	strategy := discoveryTestStrategy()

	created, wasCreated, err := createOrReuseDiscoveryStrategy(ctx, strategy, discoveryTestBars(), 50_000, strategies, configs)
	if err != nil {
		t.Fatalf("createOrReuseDiscoveryStrategy() error = %v", err)
	}
	if !wasCreated || created.Status != domain.StrategyStatusInactive || created.ScheduleCron != "" {
		t.Fatalf("created = %+v, wasCreated = %v", created, wasCreated)
	}
	if len(strategies.createStatuses) != 1 || strategies.createStatuses[0] != domain.StrategyStatusInactive {
		t.Fatalf("create statuses = %v, want [inactive]", strategies.createStatuses)
	}
	if len(strategies.updateStatuses) != 0 {
		t.Fatalf("update statuses = %v, want no activation", strategies.updateStatuses)
	}
	if len(configs.items) != 1 || configs.items[0].StrategyID != created.ID || configs.items[0].Simulation.InitialCapital != 50_000 {
		t.Fatalf("backtest configs = %+v", configs.items)
	}
}

func TestCreateOrReuseDiscoveryStrategyRemovesNewPausedStrategyWhenConfigFails(t *testing.T) {
	t.Parallel()
	strategies := newInMemoryStrategyRepo()
	configs := newDiscoveryBacktestConfigRepo()
	configs.createErr = errors.New("config write failed")

	_, wasCreated, err := createOrReuseDiscoveryStrategy(context.Background(), discoveryTestStrategy(), discoveryTestBars(), 100_000, strategies, configs)
	if err == nil || wasCreated {
		t.Fatalf("error = %v, wasCreated = %v", err, wasCreated)
	}
	if len(strategies.strategies) != 0 {
		t.Fatalf("strategies after compensation = %+v", strategies.strategies)
	}
}

func TestCreateOrReuseDiscoveryStrategyRepairsReusedActiveStrategyFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	strategies := newInMemoryStrategyRepo()
	existing := discoveryTestStrategy()
	if err := strategies.Create(ctx, &existing); err != nil {
		t.Fatal(err)
	}
	strategies.createStatuses = nil
	configs := newDiscoveryBacktestConfigRepo()

	reused, wasCreated, err := createOrReuseDiscoveryStrategy(ctx, discoveryTestStrategy(), discoveryTestBars(), 100_000, strategies, configs)
	if err != nil {
		t.Fatalf("createOrReuseDiscoveryStrategy() error = %v", err)
	}
	if wasCreated || reused.ID != existing.ID || reused.Status != domain.StrategyStatusPaused {
		t.Fatalf("reused = %+v, wasCreated = %v", reused, wasCreated)
	}
	if len(strategies.updateStatuses) != 1 || strategies.updateStatuses[0] != domain.StrategyStatusPaused {
		t.Fatalf("update statuses = %v, want [paused]", strategies.updateStatuses)
	}
	if len(configs.items) != 1 || configs.items[0].StrategyID != existing.ID {
		t.Fatalf("backtest configs = %+v", configs.items)
	}
}

func TestCreateOrReuseDiscoveryStrategyLeavesLegacyStrategyPausedWhenRepairFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	strategies := newInMemoryStrategyRepo()
	existing := discoveryTestStrategy()
	if err := strategies.Create(ctx, &existing); err != nil {
		t.Fatal(err)
	}
	strategies.createStatuses = nil
	configs := newDiscoveryBacktestConfigRepo()
	configs.createErr = errors.New("config write failed")

	reused, wasCreated, err := createOrReuseDiscoveryStrategy(ctx, discoveryTestStrategy(), discoveryTestBars(), 100_000, strategies, configs)
	if err == nil || wasCreated {
		t.Fatalf("error = %v, wasCreated = %v", err, wasCreated)
	}
	if reused.ID != existing.ID || reused.Status != domain.StrategyStatusPaused {
		t.Fatalf("reused = %+v", reused)
	}
	persisted, getErr := strategies.Get(ctx, existing.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if persisted.Status != domain.StrategyStatusPaused {
		t.Fatalf("persisted status = %q, want paused", persisted.Status)
	}
	if len(strategies.updateStatuses) != 1 || strategies.updateStatuses[0] != domain.StrategyStatusPaused {
		t.Fatalf("update statuses = %v, want [paused]", strategies.updateStatuses)
	}
}

func discoveryTestStrategy() domain.Strategy {
	return domain.Strategy{
		ID:           uuid.New(),
		Name:         "discovery: TEST momentum",
		Ticker:       "TEST",
		MarketType:   domain.MarketTypeStock,
		IsPaper:      true,
		Status:       domain.StrategyStatusActive,
		ScheduleCron: "0 */2 * * *",
	}
}

func discoveryTestBars() []domain.OHLCV {
	return []domain.OHLCV{
		{Timestamp: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Close: 10},
		{Timestamp: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Close: 11},
	}
}

type discoveryBacktestConfigRepo struct {
	items     []domain.BacktestConfig
	createErr error
}

func newDiscoveryBacktestConfigRepo() *discoveryBacktestConfigRepo {
	return &discoveryBacktestConfigRepo{items: make([]domain.BacktestConfig, 0)}
}

func (r *discoveryBacktestConfigRepo) Create(_ context.Context, config *domain.BacktestConfig) error {
	if r.createErr != nil {
		return r.createErr
	}
	if config.ID == uuid.Nil {
		config.ID = uuid.New()
	}
	r.items = append(r.items, *config)
	return nil
}

func (r *discoveryBacktestConfigRepo) Get(_ context.Context, id uuid.UUID) (*domain.BacktestConfig, error) {
	for i := range r.items {
		if r.items[i].ID == id {
			item := r.items[i]
			return &item, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *discoveryBacktestConfigRepo) List(_ context.Context, filter repository.BacktestConfigFilter, limit, offset int) ([]domain.BacktestConfig, error) {
	items := make([]domain.BacktestConfig, 0)
	for _, item := range r.items {
		if filter.StrategyID != nil && item.StrategyID != *filter.StrategyID {
			continue
		}
		items = append(items, item)
	}
	if offset >= len(items) {
		return []domain.BacktestConfig{}, nil
	}
	items = items[offset:]
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *discoveryBacktestConfigRepo) Count(ctx context.Context, filter repository.BacktestConfigFilter) (int, error) {
	items, err := r.List(ctx, filter, 0, 0)
	return len(items), err
}

func (r *discoveryBacktestConfigRepo) Update(_ context.Context, config *domain.BacktestConfig) error {
	for i := range r.items {
		if r.items[i].ID == config.ID {
			r.items[i] = *config
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *discoveryBacktestConfigRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range r.items {
		if r.items[i].ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
}

var _ repository.BacktestConfigRepository = (*discoveryBacktestConfigRepo)(nil)
