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

// ---------------------------------------------------------------------------
// Unit tests – query builder
// ---------------------------------------------------------------------------

func TestBuildExpireQuery_OnlyExpiresBefore(t *testing.T) {
	threshold := time.Now()
	query, args := buildExpireQuery(repository.MarketDataCacheExpireFilter{
		ExpiresBefore: threshold,
	})

	// Only expires_at < $1 condition + no optional filters → 1 arg.
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	if args[0] != threshold {
		t.Errorf("expected args[0] = threshold, got %v", args[0])
	}

	assertContains(t, query, "DELETE FROM market_data_cache")
	assertContains(t, query, "expires_at < $1")
	assertNotContains(t, query, "ticker")
	assertNotContains(t, query, "provider")
	assertNotContains(t, query, "data_type")
}

func TestBuildExpireQuery_AllFilters(t *testing.T) {
	threshold := time.Now()
	query, args := buildExpireQuery(repository.MarketDataCacheExpireFilter{
		ExpiresBefore: threshold,
		Ticker:        "AAPL",
		Provider:      "alpaca",
		DataType:      "ohlcv",
	})

	// expires_at + ticker + provider + data_type = 4 args.
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}

	assertContains(t, query, "expires_at < $1")
	assertContains(t, query, "ticker = $2")
	assertContains(t, query, "provider = $3")
	assertContains(t, query, "data_type = $4")
}

func TestBuildExpireQuery_TickerOnly(t *testing.T) {
	threshold := time.Now()
	query, args := buildExpireQuery(repository.MarketDataCacheExpireFilter{
		ExpiresBefore: threshold,
		Ticker:        "TSLA",
	})

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}

	assertContains(t, query, "expires_at < $1")
	assertContains(t, query, "ticker = $2")
	assertNotContains(t, query, "provider")
	assertNotContains(t, query, "data_type")
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestMarketDataCacheRepoIntegration_SetAndGet(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)
	dateFrom := now.AddDate(0, 0, -7)
	dateTo := now

	md := &domain.MarketData{
		Ticker:    "AAPL",
		Provider:  "alpaca",
		DataType:  "ohlcv",
		Timeframe: "1d",
		DateFrom:  &dateFrom,
		DateTo:    &dateTo,
		Data:      json.RawMessage(`{"bars":[{"close":180.5}]}`),
		FetchedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}

	if err := repo.Set(ctx, md); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if md.ID == uuid.Nil {
		t.Fatal("expected Set() to populate ID")
	}

	key := repository.MarketDataCacheKey{
		Ticker:    md.Ticker,
		Provider:  md.Provider,
		DataType:  md.DataType,
		Timeframe: md.Timeframe,
		DateFrom:  md.DateFrom,
		DateTo:    md.DateTo,
	}
	got, err := repo.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ID != md.ID {
		t.Errorf("ID: want %s, got %s", md.ID, got.ID)
	}
	if got.Ticker != md.Ticker {
		t.Errorf("Ticker: want %q, got %q", md.Ticker, got.Ticker)
	}
	if got.Provider != md.Provider {
		t.Errorf("Provider: want %q, got %q", md.Provider, got.Provider)
	}
	if got.DataType != md.DataType {
		t.Errorf("DataType: want %q, got %q", md.DataType, got.DataType)
	}
	if got.Timeframe != md.Timeframe {
		t.Errorf("Timeframe: want %q, got %q", md.Timeframe, got.Timeframe)
	}
	if !jsonBytesEqual(got.Data, md.Data) {
		t.Errorf("Data: want %s, got %s", md.Data, got.Data)
	}
}

func TestMarketDataCacheRepoIntegration_SetReplacesPreviousEntry(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)

	first := &domain.MarketData{
		Ticker:    "MSFT",
		Provider:  "alpaca",
		DataType:  "ohlcv",
		Timeframe: "1h",
		Data:      json.RawMessage(`{"bars":[{"close":300.0}]}`),
		FetchedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.Set(ctx, first); err != nil {
		t.Fatalf("Set() first error = %v", err)
	}

	// Replace with updated data.
	second := &domain.MarketData{
		Ticker:    "MSFT",
		Provider:  "alpaca",
		DataType:  "ohlcv",
		Timeframe: "1h",
		Data:      json.RawMessage(`{"bars":[{"close":310.0}]}`),
		FetchedAt: now.Add(time.Minute),
		ExpiresAt: now.Add(2 * time.Hour),
	}
	if err := repo.Set(ctx, second); err != nil {
		t.Fatalf("Set() second error = %v", err)
	}

	key := repository.MarketDataCacheKey{
		Ticker:    "MSFT",
		Provider:  "alpaca",
		DataType:  "ohlcv",
		Timeframe: "1h",
	}
	got, err := repo.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() after replace error = %v", err)
	}

	// Should return the second (most recent) entry.
	if got.ID != second.ID {
		t.Errorf("expected second entry (ID=%s), got ID=%s", second.ID, got.ID)
	}
	if !jsonBytesEqual(got.Data, second.Data) {
		t.Errorf("Data: want %s, got %s", second.Data, got.Data)
	}
}

func TestMarketDataCacheRepoIntegration_GetExpiredReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)

	// Store an entry that is already expired.
	md := &domain.MarketData{
		Ticker:    "GOOG",
		Provider:  "alpaca",
		DataType:  "ohlcv",
		Data:      json.RawMessage(`{}`),
		FetchedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour), // already expired
	}
	if err := repo.Set(ctx, md); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	key := repository.MarketDataCacheKey{
		Ticker:   "GOOG",
		Provider: "alpaca",
		DataType: "ohlcv",
	}
	_, err := repo.Get(ctx, key)
	if err == nil {
		t.Fatal("expected ErrNotFound for expired entry, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMarketDataCacheRepoIntegration_GetMissingKeyReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	key := repository.MarketDataCacheKey{
		Ticker:   "NOPE",
		Provider: "alpaca",
		DataType: "ohlcv",
	}
	_, err := repo.Get(ctx, key)
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMarketDataCacheRepoIntegration_ExpireRemovesEntries(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)

	// Insert an entry that is expired (expires_at in the past).
	expired := &domain.MarketData{
		Ticker:    "IBM",
		Provider:  "alpaca",
		DataType:  "ohlcv",
		Data:      json.RawMessage(`{}`),
		FetchedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour),
	}
	if err := repo.Set(ctx, expired); err != nil {
		t.Fatalf("Set() expired error = %v", err)
	}

	// Insert a live entry.
	live := &domain.MarketData{
		Ticker:    "IBM",
		Provider:  "alpaca",
		DataType:  "fundamentals",
		Data:      json.RawMessage(`{"pe":12}`),
		FetchedAt: now,
		ExpiresAt: now.Add(6 * time.Hour),
	}
	if err := repo.Set(ctx, live); err != nil {
		t.Fatalf("Set() live error = %v", err)
	}

	// Run cleanup: remove entries that expired before now.
	if err := repo.Expire(ctx, repository.MarketDataCacheExpireFilter{
		ExpiresBefore: now,
	}); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}

	// Expired entry should no longer be found via any direct DB query.
	// We verify indirectly: expired entry has already-expired expires_at so
	// Get() would return ErrNotFound anyway, but the row should also be gone.
	// Query the table directly.
	var count int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM market_data_cache WHERE ticker = 'IBM' AND data_type = 'ohlcv'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected expired row to be deleted, found %d rows", count)
	}

	// Live entry should still exist.
	liveKey := repository.MarketDataCacheKey{
		Ticker:   "IBM",
		Provider: "alpaca",
		DataType: "fundamentals",
	}
	got, err := repo.Get(ctx, liveKey)
	if err != nil {
		t.Fatalf("Get() live entry after Expire() error = %v", err)
	}
	if got.ID != live.ID {
		t.Errorf("expected live entry ID=%s, got %s", live.ID, got.ID)
	}
}

func TestMarketDataCacheRepoIntegration_ExpireWithTickerFilter(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-1 * time.Hour)

	for _, ticker := range []string{"AAPL", "MSFT"} {
		md := &domain.MarketData{
			Ticker:    ticker,
			Provider:  "alpaca",
			DataType:  "ohlcv",
			Data:      json.RawMessage(`{}`),
			FetchedAt: past,
			ExpiresAt: past,
		}
		if err := repo.Set(ctx, md); err != nil {
			t.Fatalf("Set() %s error = %v", ticker, err)
		}
	}

	// Expire only AAPL entries.
	if err := repo.Expire(ctx, repository.MarketDataCacheExpireFilter{
		ExpiresBefore: now,
		Ticker:        "AAPL",
	}); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}

	var aaplCount, msftCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM market_data_cache WHERE ticker = 'AAPL'`,
	).Scan(&aaplCount); err != nil {
		t.Fatalf("AAPL count error = %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM market_data_cache WHERE ticker = 'MSFT'`,
	).Scan(&msftCount); err != nil {
		t.Fatalf("MSFT count error = %v", err)
	}

	if aaplCount != 0 {
		t.Errorf("expected AAPL row to be deleted, found %d rows", aaplCount)
	}
	if msftCount != 1 {
		t.Errorf("expected MSFT row to remain, found %d rows", msftCount)
	}
}

func TestMarketDataCacheRepoIntegration_NullableFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMarketDataCacheIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	now := time.Now().UTC().Truncate(time.Second)

	// No timeframe, no date range, no expires_at.
	md := &domain.MarketData{
		Ticker:    "SPY",
		Provider:  "polygon",
		DataType:  "fundamentals",
		Data:      json.RawMessage(`{"pe_ratio":25.5}`),
		FetchedAt: now,
		// ExpiresAt zero → stored as NULL
	}
	if err := repo.Set(ctx, md); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	key := repository.MarketDataCacheKey{
		Ticker:   "SPY",
		Provider: "polygon",
		DataType: "fundamentals",
	}
	got, err := repo.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.Timeframe != "" {
		t.Errorf("expected empty Timeframe, got %q", got.Timeframe)
	}
	if got.DateFrom != nil {
		t.Errorf("expected nil DateFrom, got %v", got.DateFrom)
	}
	if got.DateTo != nil {
		t.Errorf("expected nil DateTo, got %v", got.DateTo)
	}
	// ExpiresAt was stored as NULL → should come back as zero value.
	if !got.ExpiresAt.IsZero() {
		t.Errorf("expected zero ExpiresAt (NULL stored), got %v", got.ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// Integration test helper
// ---------------------------------------------------------------------------

func newMarketDataCacheIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_mdcache_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TABLE market_data_cache (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			ticker     TEXT        NOT NULL,
			provider   TEXT        NOT NULL,
			data_type  TEXT        NOT NULL,
			timeframe  TEXT,
			date_from  DATE,
			date_to    DATE,
			data       JSONB       NOT NULL DEFAULT '{}',
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ
		)`,
		`CREATE INDEX idx_market_data_cache_ticker_provider ON market_data_cache (ticker, provider)`,
		`CREATE INDEX idx_market_data_cache_expires_at ON market_data_cache (expires_at)`,
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
