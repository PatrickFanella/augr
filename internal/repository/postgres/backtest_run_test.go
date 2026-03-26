package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildBacktestRunListQuery_NoFilters(t *testing.T) {
	query, args := buildBacktestRunListQuery(repository.BacktestRunFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}
	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}
	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM backtest_runs")
	assertContains(t, query, "ORDER BY run_timestamp DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildBacktestRunListQuery_AllFilters(t *testing.T) {
	configID := uuid.New()
	runAfter := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	runBefore := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

	query, args := buildBacktestRunListQuery(repository.BacktestRunFilter{
		BacktestConfigID:  &configID,
		PromptVersion:     "prompt-v2",
		PromptVersionHash: "hash-v2",
		RunAfter:          &runAfter,
		RunBefore:         &runBefore,
	}, 25, 50)

	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "backtest_config_id = $1")
	assertContains(t, query, "prompt_version = $2")
	assertContains(t, query, "prompt_version_hash = $3")
	assertContains(t, query, "run_timestamp >= $4")
	assertContains(t, query, "run_timestamp <= $5")
	assertContains(t, query, "LIMIT $6 OFFSET $7")
}

func TestBacktestRunRepoCreate_ValidateError(t *testing.T) {
	repo := NewBacktestRunRepo(nil)

	err := repo.Create(context.Background(), &domain.BacktestRun{
		BacktestConfigID:  uuid.New(),
		Metrics:           []byte(`{"total_return":0.12}`),
		TradeLog:          []byte(`[]`),
		EquityCurve:       []byte(`[]`),
		RunTimestamp:      time.Date(2024, 1, 1, 14, 30, 0, 0, time.UTC),
		PromptVersionHash: "hash-v1",
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "validate backtest run") {
		t.Fatalf("expected wrapped validation error, got %v", err)
	}
}

func TestBacktestRunRepoIntegration_CreateGetList(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	ensureBacktestConfigTable(t, ctx, pool)
	ensureBacktestRunTable(t, ctx, pool)

	configID := createTestBacktestConfig(t, ctx, pool)
	repo := NewBacktestRunRepo(pool)

	first := &domain.BacktestRun{
		BacktestConfigID:  configID,
		Metrics:           []byte(`{"total_return":0.12,"max_drawdown":0.05}`),
		TradeLog:          []byte(`[{"ticker":"AAPL","side":"buy","quantity":10,"price":100,"fee":1}]`),
		EquityCurve:       []byte(`[{"timestamp":"2024-01-02T14:30:00Z","equity":100000},{"timestamp":"2024-01-03T14:30:00Z","equity":112000}]`),
		RunTimestamp:      time.Date(2024, 1, 3, 21, 0, 0, 0, time.UTC),
		Duration:          37 * time.Minute,
		PromptVersion:     "prompt-v1",
		PromptVersionHash: "hash-v1",
	}
	second := &domain.BacktestRun{
		BacktestConfigID:  configID,
		Metrics:           []byte(`{"total_return":0.08,"max_drawdown":0.02}`),
		TradeLog:          []byte(`[]`),
		EquityCurve:       []byte(`[{"timestamp":"2024-02-02T14:30:00Z","equity":100000},{"timestamp":"2024-02-03T14:30:00Z","equity":108000}]`),
		RunTimestamp:      time.Date(2024, 2, 3, 21, 0, 0, 0, time.UTC),
		Duration:          22 * time.Minute,
		PromptVersion:     "prompt-v2",
		PromptVersionHash: "hash-v2",
	}

	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	if first.ID == uuid.Nil || second.ID == uuid.Nil {
		t.Fatal("expected Create() to populate IDs")
	}
	if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatal("expected Create() to populate timestamps")
	}

	got, err := repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	assertBacktestRunEqual(t, got, first)

	listed, err := repo.List(ctx, repository.BacktestRunFilter{
		BacktestConfigID:  &configID,
		PromptVersion:     "prompt-v2",
		PromptVersionHash: "hash-v2",
	}, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed run, got %d", len(listed))
	}
	assertBacktestRunEqual(t, &listed[0], second)

	allRuns, err := repo.List(ctx, repository.BacktestRunFilter{BacktestConfigID: &configID}, 10, 0)
	if err != nil {
		t.Fatalf("List(all) error = %v", err)
	}
	if len(allRuns) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(allRuns))
	}
	if allRuns[0].ID != second.ID {
		t.Fatalf("expected newest run first, got %s want %s", allRuns[0].ID, second.ID)
	}

	_, err = repo.Get(ctx, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing run, got %v", err)
	}
}

func ensureBacktestRunTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `CREATE TABLE backtest_runs (
		id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
		backtest_config_id UUID        NOT NULL REFERENCES backtest_configs (id) ON DELETE CASCADE,
		metrics            JSONB       NOT NULL,
		trade_log          JSONB       NOT NULL,
		equity_curve       JSONB       NOT NULL,
		run_timestamp      TIMESTAMPTZ NOT NULL,
		duration_ns        BIGINT      NOT NULL CHECK (duration_ns >= 0),
		prompt_version     TEXT        NOT NULL,
		prompt_version_hash TEXT       NOT NULL,
		created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		t.Fatalf("failed to create backtest_runs table: %v", err)
	}
}

func createTestBacktestConfig(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	repo := NewBacktestConfigRepo(pool)
	config := &domain.BacktestConfig{
		StrategyID:  createTestPositionStrategy(t, ctx, pool),
		Name:        "Backtest config for run persistence",
		Description: "Used by backtest run repository integration tests",
		StartDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{
			InitialCapital: 100000,
		},
	}
	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("failed to create backtest config fixture: %v", err)
	}
	return config.ID
}

func assertBacktestRunEqual(t *testing.T, got, want *domain.BacktestRun) {
	t.Helper()

	if got.ID != want.ID {
		t.Fatalf("expected ID %s, got %s", want.ID, got.ID)
	}
	if got.BacktestConfigID != want.BacktestConfigID {
		t.Fatalf("expected BacktestConfigID %s, got %s", want.BacktestConfigID, got.BacktestConfigID)
	}
	if !jsonBytesEqual(got.Metrics, want.Metrics) {
		t.Fatalf("expected Metrics %s, got %s", want.Metrics, got.Metrics)
	}
	if !jsonBytesEqual(got.TradeLog, want.TradeLog) {
		t.Fatalf("expected TradeLog %s, got %s", want.TradeLog, got.TradeLog)
	}
	if !jsonBytesEqual(got.EquityCurve, want.EquityCurve) {
		t.Fatalf("expected EquityCurve %s, got %s", want.EquityCurve, got.EquityCurve)
	}
	if !got.RunTimestamp.Equal(want.RunTimestamp) {
		t.Fatalf("expected RunTimestamp %s, got %s", want.RunTimestamp, got.RunTimestamp)
	}
	if got.Duration != want.Duration {
		t.Fatalf("expected Duration %s, got %s", want.Duration, got.Duration)
	}
	if got.PromptVersion != want.PromptVersion {
		t.Fatalf("expected PromptVersion %q, got %q", want.PromptVersion, got.PromptVersion)
	}
	if got.PromptVersionHash != want.PromptVersionHash {
		t.Fatalf("expected PromptVersionHash %q, got %q", want.PromptVersionHash, got.PromptVersionHash)
	}
}
