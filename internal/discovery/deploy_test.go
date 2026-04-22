package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestCreateOrReusePaperStrategyCreatesThenReuses(t *testing.T) {
	t.Parallel()

	repo := newInMemoryStrategyRepo()
	ctx := context.Background()

	strategy := domain.Strategy{
		Name:       "discovery: PBM RSI Momentum Breakout",
		Ticker:     "PBM",
		MarketType: domain.MarketTypeStock,
		IsPaper:    true,
		Status:     domain.StrategyStatusActive,
		Config:     json.RawMessage(`{"rules_engine":{"name":"pbm"}}`),
	}

	created, didCreate, err := CreateOrReusePaperStrategy(ctx, repo, strategy)
	if err != nil {
		t.Fatalf("CreateOrReusePaperStrategy(first) error = %v", err)
	}
	if !didCreate {
		t.Fatal("first call should create strategy")
	}
	if created.ID == uuid.Nil {
		t.Fatal("created strategy id should be set")
	}

	reused, didCreate, err := CreateOrReusePaperStrategy(ctx, repo, strategy)
	if err != nil {
		t.Fatalf("CreateOrReusePaperStrategy(second) error = %v", err)
	}
	if didCreate {
		t.Fatal("second call should reuse existing strategy")
	}
	if reused.ID != created.ID {
		t.Fatalf("reused strategy id = %s, want %s", reused.ID, created.ID)
	}

	got := repo.countByKey(strategy.Ticker, strategy.MarketType, strategy.Name, true)
	if got != 1 {
		t.Fatalf("strategy count for key = %d, want 1", got)
	}
}

func TestCreateOrReusePaperStrategyHandlesUniqueConflictByRequery(t *testing.T) {
	t.Parallel()

	repo := newInMemoryStrategyRepo()
	repo.injectConflictOnce = true
	ctx := context.Background()

	strategy := domain.Strategy{
		Name:       "options: QQQ bull_put_spread",
		Ticker:     "QQQ",
		MarketType: domain.MarketTypeOptions,
		IsPaper:    true,
		Status:     domain.StrategyStatusActive,
		Config:     json.RawMessage(`{"options_rules":{"strategy_type":"bull_put_spread"}}`),
	}

	reused, didCreate, err := CreateOrReusePaperStrategy(ctx, repo, strategy)
	if err != nil {
		t.Fatalf("CreateOrReusePaperStrategy() error = %v", err)
	}
	if didCreate {
		t.Fatal("conflict path should return reused existing strategy")
	}
	if reused.ID == uuid.Nil {
		t.Fatal("reused strategy id should be set")
	}

	got := repo.countByKey(strategy.Ticker, strategy.MarketType, strategy.Name, true)
	if got != 1 {
		t.Fatalf("strategy count for key = %d, want 1", got)
	}
}

type inMemoryStrategyRepo struct {
	strategies         []domain.Strategy
	injectConflictOnce bool
	conflictTriggered  bool
}

func newInMemoryStrategyRepo() *inMemoryStrategyRepo {
	return &inMemoryStrategyRepo{strategies: make([]domain.Strategy, 0)}
}

func (r *inMemoryStrategyRepo) Create(_ context.Context, strategy *domain.Strategy) error {
	if strategy.ID == uuid.Nil {
		strategy.ID = uuid.New()
	}

	if r.injectConflictOnce && !r.conflictTriggered {
		r.conflictTriggered = true

		existing := *strategy
		existing.ID = uuid.New()
		r.strategies = append(r.strategies, existing)
		return errors.New("ERROR: duplicate key value violates unique constraint \"idx_strategies_discovery_unique\" (SQLSTATE 23505)")
	}

	r.strategies = append(r.strategies, *strategy)
	return nil
}

func (r *inMemoryStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	for i := range r.strategies {
		if r.strategies[i].ID == id {
			copy := r.strategies[i]
			return &copy, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *inMemoryStrategyRepo) List(_ context.Context, filter repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error) {
	var filtered []domain.Strategy
	for _, strategy := range r.strategies {
		if filter.Ticker != "" && strategy.Ticker != filter.Ticker {
			continue
		}
		if filter.MarketType != "" && strategy.MarketType != filter.MarketType {
			continue
		}
		if filter.Status != "" && strategy.Status != filter.Status {
			continue
		}
		if filter.IsPaper != nil && strategy.IsPaper != *filter.IsPaper {
			continue
		}
		filtered = append(filtered, strategy)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})

	if offset > len(filtered) {
		return []domain.Strategy{}, nil
	}
	filtered = filtered[offset:]
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (r *inMemoryStrategyRepo) Count(ctx context.Context, filter repository.StrategyFilter) (int, error) {
	listed, err := r.List(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(listed), nil
}

func (r *inMemoryStrategyRepo) Update(_ context.Context, strategy *domain.Strategy) error {
	for i := range r.strategies {
		if r.strategies[i].ID == strategy.ID {
			r.strategies[i] = *strategy
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *inMemoryStrategyRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range r.strategies {
		if r.strategies[i].ID == id {
			r.strategies = append(r.strategies[:i], r.strategies[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *inMemoryStrategyRepo) UpdateThesis(_ context.Context, _ uuid.UUID, _ json.RawMessage) error {
	return nil
}

func (r *inMemoryStrategyRepo) GetThesisRaw(_ context.Context, _ uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

func (r *inMemoryStrategyRepo) countByKey(ticker string, marketType domain.MarketType, name string, isPaper bool) int {
	count := 0
	for _, strategy := range r.strategies {
		if strategy.Ticker == ticker && strategy.MarketType == marketType && strategy.Name == name && strategy.IsPaper == isPaper {
			count++
		}
	}
	return count
}

var _ repository.StrategyRepository = (*inMemoryStrategyRepo)(nil)

func TestIsUniqueViolationHandlesCommonErrors(t *testing.T) {
	t.Parallel()

	if !isUniqueViolation(errors.New("ERROR: duplicate key value violates unique constraint \"foo\"")) {
		t.Fatal("expected duplicate key text to be treated as unique violation")
	}
	if isUniqueViolation(fmt.Errorf("some other error: %w", errors.New("network"))) {
		t.Fatal("unexpected unique violation on unrelated error")
	}
	if !isUniqueViolation(errors.New(strings.ToUpper("unique constraint violation"))) {
		t.Fatal("expected case-insensitive unique detection")
	}
}
