package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildPipelineRunListQuery_NoFilters(t *testing.T) {
	query, args := buildPipelineRunListQuery(repository.PipelineRunFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}

	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM pipeline_runs")
	assertContains(t, query, "ORDER BY started_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildPipelineRunListQuery_AllFilters(t *testing.T) {
	strategyID := uuid.New()
	tradeDate := time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC)
	startedAfter := tradeDate.Add(-2 * time.Hour)
	startedBefore := tradeDate.Add(2 * time.Hour)

	filter := repository.PipelineRunFilter{
		StrategyID:    &strategyID,
		Ticker:        "AAPL",
		Status:        domain.PipelineStatusRunning,
		TradeDate:     &tradeDate,
		StartedAfter:  &startedAfter,
		StartedBefore: &startedBefore,
	}

	query, args := buildPipelineRunListQuery(filter, 25, 50)

	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "strategy_id = $1")
	assertContains(t, query, "ticker = $2")
	assertContains(t, query, "status = $3")
	assertContains(t, query, "trade_date = $4::date")
	assertContains(t, query, "started_at >= $5")
	assertContains(t, query, "started_at <= $6")
	assertContains(t, query, "LIMIT $7 OFFSET $8")

	if args[0] != strategyID {
		t.Errorf("expected strategy_id arg %s, got %v", strategyID, args[0])
	}

	if args[1] != "AAPL" {
		t.Errorf("expected ticker arg AAPL, got %v", args[1])
	}

	if args[2] != domain.PipelineStatusRunning {
		t.Errorf("expected status arg running, got %v", args[2])
	}
}

func TestBuildPipelineRunListQuery_PartialFilters(t *testing.T) {
	strategyID := uuid.New()
	filter := repository.PipelineRunFilter{
		StrategyID: &strategyID,
		Status:     domain.PipelineStatusFailed,
	}

	query, args := buildPipelineRunListQuery(filter, 10, 0)

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "strategy_id = $1")
	assertNotContains(t, query, "ticker =")
	assertContains(t, query, "status = $2")
	assertNotContains(t, query, "trade_date =")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

func TestMarshalConfigSnapshot_ValidJSON(t *testing.T) {
	input := json.RawMessage(`{"lookback":20,"threshold":0.5}`)

	got, err := marshalConfigSnapshot(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != `{"lookback":20,"threshold":0.5}` {
		t.Errorf("expected config snapshot pass-through, got %s", got)
	}
}

func TestMarshalConfigSnapshot_NilDefaultsToNull(t *testing.T) {
	got, err := marshalConfigSnapshot(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil config snapshot, got %s", got)
	}
}

func TestMarshalConfigSnapshot_EmptyDefaultsToNull(t *testing.T) {
	got, err := marshalConfigSnapshot(json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil config snapshot, got %s", got)
	}
}

func TestMarshalConfigSnapshot_InvalidJSON(t *testing.T) {
	_, err := marshalConfigSnapshot(json.RawMessage(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestPipelineRunRepoIntegration_CRUDAndFilters(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPipelineRunIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPipelineRunRepo(pool)
	strategyID := uuid.New()

	tradeDate1 := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)
	tradeDate2 := time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC)
	tradeDate3 := time.Date(2027, time.January, 3, 0, 0, 0, 0, time.UTC)
	startedAt1 := time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC)
	startedAt2 := time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)
	startedAt3 := time.Date(2027, time.January, 3, 11, 0, 0, 0, time.UTC)

	run1 := &domain.PipelineRun{
		StrategyID:     strategyID,
		Ticker:         "AAPL",
		TradeDate:      tradeDate1,
		Status:         domain.PipelineStatusRunning,
		Signal:         domain.PipelineSignalBuy,
		StartedAt:      startedAt1,
		ConfigSnapshot: json.RawMessage(`{"window":20}`),
	}
	run2 := &domain.PipelineRun{
		StrategyID: strategyID,
		Ticker:     "AAPL",
		TradeDate:  tradeDate2,
		Status:     domain.PipelineStatusFailed,
		Signal:     domain.PipelineSignalSell,
		StartedAt:  startedAt2,
	}
	run3 := &domain.PipelineRun{
		StrategyID: uuid.New(),
		Ticker:     "MSFT",
		TradeDate:  tradeDate3,
		Status:     domain.PipelineStatusCompleted,
		Signal:     domain.PipelineSignalHold,
		StartedAt:  startedAt3,
	}

	for _, run := range []*domain.PipelineRun{run1, run2, run3} {
		if err := repo.Create(ctx, run); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if run.ID == uuid.Nil {
			t.Fatal("expected Create() to populate ID")
		}
	}

	got, err := repo.Get(ctx, run1.ID, run1.TradeDate)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.StrategyID != run1.StrategyID || got.Signal != run1.Signal || string(got.ConfigSnapshot) != string(run1.ConfigSnapshot) {
		t.Fatalf("Get() returned unexpected run: %+v", got)
	}

	completedAt := startedAt1.Add(30 * time.Minute)
	if err := repo.UpdateStatus(ctx, run1.ID, run1.TradeDate, repository.PipelineRunStatusUpdate{
		Status:       domain.PipelineStatusCompleted,
		CompletedAt:  &completedAt,
		ErrorMessage: "",
	}); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	updated, err := repo.Get(ctx, run1.ID, run1.TradeDate)
	if err != nil {
		t.Fatalf("Get() after update error = %v", err)
	}

	if updated.Status != domain.PipelineStatusCompleted {
		t.Fatalf("expected updated status %q, got %q", domain.PipelineStatusCompleted, updated.Status)
	}

	if updated.CompletedAt == nil || !updated.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected completed_at %v, got %v", completedAt, updated.CompletedAt)
	}

	filteredByStrategy, err := repo.List(ctx, repository.PipelineRunFilter{
		StrategyID: &strategyID,
	}, 10, 0)
	if err != nil {
		t.Fatalf("List() by strategy error = %v", err)
	}

	if len(filteredByStrategy) != 2 {
		t.Fatalf("expected 2 runs for strategy filter, got %d", len(filteredByStrategy))
	}

	filteredByStatus, err := repo.List(ctx, repository.PipelineRunFilter{
		Status: domain.PipelineStatusCompleted,
	}, 10, 0)
	if err != nil {
		t.Fatalf("List() by status error = %v", err)
	}

	if len(filteredByStatus) != 2 {
		t.Fatalf("expected 2 completed runs, got %d", len(filteredByStatus))
	}

	filteredByTradeDate, err := repo.List(ctx, repository.PipelineRunFilter{
		TradeDate: &tradeDate2,
	}, 10, 0)
	if err != nil {
		t.Fatalf("List() by trade date error = %v", err)
	}

	if len(filteredByTradeDate) != 1 || filteredByTradeDate[0].ID != run2.ID {
		t.Fatalf("expected trade date filter to return run2, got %+v", filteredByTradeDate)
	}

	startedAfter := startedAt1.Add(15 * time.Minute)
	startedBefore := startedAt3.Add(-15 * time.Minute)
	filteredByWindow, err := repo.List(ctx, repository.PipelineRunFilter{
		StartedAfter:  &startedAfter,
		StartedBefore: &startedBefore,
	}, 10, 0)
	if err != nil {
		t.Fatalf("List() by started_at window error = %v", err)
	}

	if len(filteredByWindow) != 1 || filteredByWindow[0].ID != run2.ID {
		t.Fatalf("expected time-window filter to return run2, got %+v", filteredByWindow)
	}
}

func TestPipelineRunRepoIntegration_NotFound(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPipelineRunIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPipelineRunRepo(pool)
	missingID := uuid.New()

	missingTradeDate := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)

	_, err := repo.Get(ctx, missingID, missingTradeDate)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected Get() ErrNotFound, got %v", err)
	}

	err = repo.UpdateStatus(ctx, missingID, missingTradeDate, repository.PipelineRunStatusUpdate{
		Status: domain.PipelineStatusFailed,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected UpdateStatus() ErrNotFound, got %v", err)
	}
}

func TestPipelineRunRepoIntegration_UsesCompositeKey(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPipelineRunIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPipelineRunRepo(pool)
	sharedID := uuid.New()
	tradeDate1 := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)
	tradeDate2 := time.Date(2027, time.January, 3, 0, 0, 0, 0, time.UTC)
	startedAt1 := time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC)
	startedAt2 := time.Date(2027, time.January, 3, 11, 0, 0, 0, time.UTC)

	insertSQL := `INSERT INTO pipeline_runs (
		id, strategy_id, ticker, trade_date, status, signal, started_at, error_message
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	for _, tc := range []struct {
		tradeDate time.Time
		ticker    string
		status    domain.PipelineStatus
		startedAt time.Time
	}{
		{tradeDate: tradeDate1, ticker: "AAPL", status: domain.PipelineStatusRunning, startedAt: startedAt1},
		{tradeDate: tradeDate2, ticker: "MSFT", status: domain.PipelineStatusFailed, startedAt: startedAt2},
	} {
		if _, err := pool.Exec(ctx, insertSQL,
			sharedID,
			uuid.New(),
			tc.ticker,
			tc.tradeDate,
			tc.status,
			"",
			tc.startedAt,
			"",
		); err != nil {
			t.Fatalf("failed to seed duplicate id rows: %v", err)
		}
	}

	got, err := repo.Get(ctx, sharedID, tradeDate2)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.Ticker != "MSFT" || got.TradeDate.Format("2006-01-02") != tradeDate2.Format("2006-01-02") {
		t.Fatalf("expected Get() to return the row for trade date %s, got %+v", tradeDate2.Format("2006-01-02"), got)
	}

	completedAt := startedAt1.Add(time.Hour)
	if err := repo.UpdateStatus(ctx, sharedID, tradeDate1, repository.PipelineRunStatusUpdate{
		Status:       domain.PipelineStatusCompleted,
		CompletedAt:  &completedAt,
		ErrorMessage: "",
	}); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	firstRun, err := repo.Get(ctx, sharedID, tradeDate1)
	if err != nil {
		t.Fatalf("Get() for first run error = %v", err)
	}
	secondRun, err := repo.Get(ctx, sharedID, tradeDate2)
	if err != nil {
		t.Fatalf("Get() for second run error = %v", err)
	}

	if firstRun.Status != domain.PipelineStatusCompleted {
		t.Fatalf("expected first run status completed, got %q", firstRun.Status)
	}
	if secondRun.Status != domain.PipelineStatusFailed {
		t.Fatalf("expected second run status to remain failed, got %q", secondRun.Status)
	}
}

func newPipelineRunIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	schemaName := "integration_pipeline_run_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	ddl := []string{
		`CREATE TYPE pipeline_status AS ENUM ('running', 'completed', 'failed', 'cancelled')`,
		`CREATE TABLE pipeline_runs (
			id              UUID            NOT NULL DEFAULT gen_random_uuid(),
			strategy_id     UUID            NOT NULL,
			ticker          TEXT            NOT NULL,
			trade_date      DATE            NOT NULL,
			status          pipeline_status NOT NULL DEFAULT 'running',
			signal          TEXT            NOT NULL DEFAULT '',
			started_at      TIMESTAMPTZ     NOT NULL,
			completed_at    TIMESTAMPTZ,
			error_message   TEXT            NOT NULL DEFAULT '',
			config_snapshot JSONB,
			PRIMARY KEY (id, trade_date)
		) PARTITION BY RANGE (trade_date)`,
		`CREATE TABLE pipeline_runs_2026_q1 PARTITION OF pipeline_runs
			FOR VALUES FROM ('2026-01-01') TO ('2026-04-01')`,
		`CREATE TABLE pipeline_runs_default PARTITION OF pipeline_runs DEFAULT`,
		`CREATE INDEX idx_pipeline_runs_strategy_id ON pipeline_runs (strategy_id)`,
		`CREATE INDEX idx_pipeline_runs_ticker ON pipeline_runs (ticker)`,
		`CREATE INDEX idx_pipeline_runs_status ON pipeline_runs (status)`,
		`CREATE INDEX idx_pipeline_runs_trade_date ON pipeline_runs (trade_date)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}
