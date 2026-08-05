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

func TestBuildKalshiSnapshotListQueryDefaults(t *testing.T) {
	query, args := buildKalshiSnapshotListQuery("WHERE ticker = $1", "BTC-1", 0)
	if len(args) != 2 || args[0] != "BTC-1" || args[1] != 20 {
		t.Fatalf("args = %#v, want ticker and default limit", args)
	}
	assertContains(t, query, "FROM kalshi_market_snapshots")
	assertContains(t, query, "WHERE ticker = $1")
	assertContains(t, query, "ORDER BY captured_at DESC, id DESC")
	assertContains(t, query, "LIMIT $2")
}

func TestKalshiRepositoriesIntegration_CRUD(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newKalshiIntegrationPool(t, ctx)
	defer cleanup()

	watchedRepo := NewKalshiWatchedMarketsRepo(pool)
	snapshotRepo := NewKalshiMarketSnapshotsRepo(pool)
	runRepo := NewKalshiDiscoveryRunRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)
	closeTime := now.Add(24 * time.Hour)
	market := &domain.KalshiWatchedMarket{
		Ticker:      "KX-TEST",
		EventTicker: "EVT-TEST",
		Title:       "Will test pass?",
		Category:    "testing",
		Status:      "open",
		CloseTime:   &closeTime,
	}
	if err := watchedRepo.Upsert(ctx, market); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if !market.Enabled {
		t.Fatalf("Upsert() Enabled = false, want default enabled")
	}
	if market.AddedAt.IsZero() || market.UpdatedAt.IsZero() {
		t.Fatalf("upserted timestamps should be populated: %+v", market)
	}
	markets, err := watchedRepo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled() error = %v", err)
	}
	if len(markets) != 1 || markets[0].Ticker != market.Ticker {
		t.Fatalf("ListEnabled() = %#v, want one watched market", markets)
	}
	market.Enabled = false
	if err := watchedRepo.Upsert(ctx, market); err != nil {
		t.Fatalf("Upsert() preserve enabled error = %v", err)
	}
	markets, err = watchedRepo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled() after preserve error = %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("ListEnabled() after preserve = %#v, want still enabled", markets)
	}
	if err := watchedRepo.SetEnabled(ctx, market.Ticker, false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	markets, err = watchedRepo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled() after disable error = %v", err)
	}
	if len(markets) != 0 {
		t.Fatalf("ListEnabled() after disable = %#v, want empty", markets)
	}

	snap1 := &domain.KalshiMarketSnapshot{Ticker: market.Ticker, Title: market.Title, Status: market.Status, YesBid: 0.51, YesAsk: 0.55, NoBid: 0.45, NoAsk: 0.49, Volume: 1234, OpenInterest: 5678, CloseTime: &closeTime, Raw: json.RawMessage(`{"seq":1}`)}
	snap2 := &domain.KalshiMarketSnapshot{Ticker: market.Ticker, Title: market.Title, Status: market.Status, YesBid: 0.61, YesAsk: 0.65, NoBid: 0.35, NoAsk: 0.39, Volume: 2234, OpenInterest: 6678, CloseTime: &closeTime, Raw: json.RawMessage(`{"seq":2}`)}
	if err := snapshotRepo.Create(ctx, snap1); err != nil {
		t.Fatalf("Create() snap1 error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := snapshotRepo.Create(ctx, snap2); err != nil {
		t.Fatalf("Create() snap2 error = %v", err)
	}
	latest, err := snapshotRepo.ListLatestByTicker(ctx, market.Ticker, 10)
	if err != nil {
		t.Fatalf("ListLatestByTicker() error = %v", err)
	}
	if len(latest) != 2 || !jsonBytesEqual(latest[0].Raw, snap2.Raw) || !jsonBytesEqual(latest[1].Raw, snap1.Raw) {
		t.Fatalf("ListLatestByTicker() = %#v, want newest-first snapshots", latest)
	}
	recent, err := snapshotRepo.ListRecent(ctx, 1)
	if err != nil {
		t.Fatalf("ListRecent() error = %v", err)
	}
	if len(recent) != 1 || recent[0].ID != snap2.ID {
		t.Fatalf("ListRecent() = %#v, want latest snapshot", recent)
	}

	run := &domain.KalshiDiscoveryRun{
		Status: domain.KalshiDiscoveryStatusRunning,
		Result: domain.KalshiDiscoveryResult{
			Fetched:  3,
			Screened: 2,
			Proposed: 1,
			Deployed: 0,
			Errors:   []string{"initial warning"},
			Summary:  json.RawMessage(`{"stage":"start"}`),
		},
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() run error = %v", err)
	}
	active, err := runRepo.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive() error = %v", err)
	}
	if active.ID != run.ID || active.Result.Fetched != 3 {
		t.Fatalf("GetActive() = %#v, want created run", active)
	}
	finishedAt := now.Add(5 * time.Minute)
	run.Status = domain.KalshiDiscoveryStatusCompleted
	run.Result.Deployed = 1
	run.Result.Errors = []string{"done"}
	run.Result.Summary = json.RawMessage(`{"stage":"finish"}`)
	run.FinishedAt = &finishedAt
	if err := runRepo.Finish(ctx, run); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if _, err := runRepo.GetActive(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetActive() after finish error = %v, want ErrNotFound", err)
	}
	latestRuns, err := runRepo.ListLatest(ctx, 5)
	if err != nil {
		t.Fatalf("ListLatest() error = %v", err)
	}
	if len(latestRuns) != 1 || latestRuns[0].Status != domain.KalshiDiscoveryStatusCompleted || latestRuns[0].Result.Deployed != 1 {
		t.Fatalf("ListLatest() = %#v, want finished run", latestRuns)
	}
}

func newKalshiIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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
	schemaName := "integration_kalshi_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+pqQuoteIdent(schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}
	ddl := `CREATE TABLE kalshi_watched_markets (
		ticker TEXT PRIMARY KEY,
		event_ticker TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		close_time TIMESTAMPTZ,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to apply watched markets DDL: %v", err)
	}
	ddl = `CREATE TABLE kalshi_market_snapshots (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		provider TEXT NOT NULL DEFAULT 'kalshi',
		environment TEXT NOT NULL DEFAULT 'unknown',
		source_url TEXT NOT NULL DEFAULT '',
		ticker TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		yes_bid DOUBLE PRECISION NOT NULL DEFAULT 0,
		yes_ask DOUBLE PRECISION NOT NULL DEFAULT 0,
		no_bid DOUBLE PRECISION NOT NULL DEFAULT 0,
		no_ask DOUBLE PRECISION NOT NULL DEFAULT 0,
		volume DOUBLE PRECISION NOT NULL DEFAULT 0,
		open_interest DOUBLE PRECISION NOT NULL DEFAULT 0,
		close_time TIMESTAMPTZ,
		raw JSONB NOT NULL DEFAULT '{}'::jsonb,
		captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to apply snapshots DDL: %v", err)
	}
	ddl = `CREATE TABLE kalshi_discovery_runs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		status TEXT NOT NULL DEFAULT 'running',
		fetched INTEGER NOT NULL DEFAULT 0,
		screened INTEGER NOT NULL DEFAULT 0,
		proposed INTEGER NOT NULL DEFAULT 0,
		deployed INTEGER NOT NULL DEFAULT 0,
		errors JSONB NOT NULL DEFAULT '[]'::jsonb,
		summary JSONB NOT NULL DEFAULT '{}'::jsonb,
		started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		finished_at TIMESTAMPTZ,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to apply discovery runs DDL: %v", err)
	}
	return pool, func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
	}
}
