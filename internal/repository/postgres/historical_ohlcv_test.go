package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildHistoricalOHLCVQuery_AllFilters(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)

	query, args := buildHistoricalOHLCVQuery(repository.HistoricalOHLCVFilter{
		Ticker:    "AAPL",
		Provider:  "stock-chain",
		Timeframe: "1d",
		From:      from,
		To:        to,
	})

	if len(args) != 5 {
		t.Fatalf("len(args) = %d, want 5", len(args))
	}
	if args[0] != "AAPL" || args[1] != "stock-chain" || args[2] != "1d" {
		t.Fatalf("unexpected leading args: %#v", args[:3])
	}
	if got, ok := args[3].(time.Time); !ok || !got.Equal(from) {
		t.Fatalf("args[3] = %#v, want %v", args[3], from)
	}
	if got, ok := args[4].(time.Time); !ok || !got.Equal(to) {
		t.Fatalf("args[4] = %#v, want %v", args[4], to)
	}

	assertContains(t, query, "FROM historical_ohlcv")
	assertContains(t, query, "ticker = $1")
	assertContains(t, query, "provider = $2")
	assertContains(t, query, "timeframe = $3")
	assertContains(t, query, "bar_time >= $4")
	assertContains(t, query, "bar_time <= $5")
	assertContains(t, query, "ORDER BY bar_time ASC")
}

func TestBuildHistoricalOHLCVCoverageQuery_FilterOrder(t *testing.T) {
	from := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	to := from.Add(72 * time.Hour)

	query, args := buildHistoricalOHLCVCoverageQuery(repository.HistoricalOHLCVCoverageFilter{
		Ticker:    "MSFT",
		Provider:  "stock-chain",
		Timeframe: "1h",
		From:      from,
		To:        to,
	})

	if len(args) != 5 {
		t.Fatalf("len(args) = %d, want 5", len(args))
	}
	if args[0] != "MSFT" || args[1] != "stock-chain" || args[2] != "1h" {
		t.Fatalf("unexpected leading args: %#v", args[:3])
	}
	if got, ok := args[3].(time.Time); !ok || !got.Equal(to) {
		t.Fatalf("args[3] = %#v, want %v", args[3], to)
	}
	if got, ok := args[4].(time.Time); !ok || !got.Equal(from) {
		t.Fatalf("args[4] = %#v, want %v", args[4], from)
	}

	assertContains(t, query, "FROM historical_ohlcv_coverage")
	assertContains(t, query, "ticker = $1")
	assertContains(t, query, "provider = $2")
	assertContains(t, query, "timeframe = $3")
	assertContains(t, query, "range_from <= $4")
	assertContains(t, query, "range_to >= $5")
	assertContains(t, query, "ORDER BY range_from ASC, range_to ASC")
}

func TestMarketDataCacheRepoIntegration_HistoricalOHLCVRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newHistoricalOHLCVIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMarketDataCacheRepo(pool)

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	mid := start.Add(24 * time.Hour)
	end := mid.Add(24 * time.Hour)

	if err := repo.UpsertHistoricalOHLCV(ctx, []domain.HistoricalOHLCV{
		{
			Ticker: "AAPL", Provider: "stock-chain", Timeframe: "1d",
			Timestamp: mid, Open: 101, High: 103, Low: 100, Close: 102, Volume: 2000,
		},
		{
			Ticker: "AAPL", Provider: "stock-chain", Timeframe: "1d",
			Timestamp: start, Open: 99, High: 101, Low: 98, Close: 100, Volume: 1500,
		},
	}); err != nil {
		t.Fatalf("UpsertHistoricalOHLCV() initial error = %v", err)
	}

	if err := repo.UpsertHistoricalOHLCV(ctx, []domain.HistoricalOHLCV{
		{
			Ticker: "AAPL", Provider: "stock-chain", Timeframe: "1d",
			Timestamp: mid, Open: 101, High: 104, Low: 100, Close: 103, Volume: 2100,
		},
		{
			Ticker: "AAPL", Provider: "other-provider", Timeframe: "1d",
			Timestamp: start, Open: 500, High: 501, Low: 499, Close: 500.5, Volume: 1,
		},
	}); err != nil {
		t.Fatalf("UpsertHistoricalOHLCV() update error = %v", err)
	}

	gotBars, err := repo.ListHistoricalOHLCV(ctx, repository.HistoricalOHLCVFilter{
		Ticker:    "AAPL",
		Provider:  "stock-chain",
		Timeframe: "1d",
		From:      start,
		To:        end,
	})
	if err != nil {
		t.Fatalf("ListHistoricalOHLCV() error = %v", err)
	}

	if len(gotBars) != 2 {
		t.Fatalf("len(gotBars) = %d, want 2", len(gotBars))
	}
	if !gotBars[0].Timestamp.Equal(start) || !gotBars[1].Timestamp.Equal(mid) {
		t.Fatalf("bar order = %v, %v; want %v, %v", gotBars[0].Timestamp, gotBars[1].Timestamp, start, mid)
	}
	if gotBars[1].Close != 103 || gotBars[1].High != 104 || gotBars[1].Volume != 2100 {
		t.Fatalf("updated bar = %#v, want updated values", gotBars[1])
	}

	firstFetchedAt := end
	secondFetchedAt := end.Add(time.Hour)
	coverage := domain.HistoricalOHLCVCoverage{
		Ticker: "AAPL", Provider: "stock-chain", Timeframe: "1d",
		DateFrom: start, DateTo: mid, FetchedAt: firstFetchedAt,
	}
	if err := repo.UpsertHistoricalOHLCVCoverage(ctx, coverage); err != nil {
		t.Fatalf("UpsertHistoricalOHLCVCoverage() initial error = %v", err)
	}
	coverage.FetchedAt = secondFetchedAt
	if err := repo.UpsertHistoricalOHLCVCoverage(ctx, coverage); err != nil {
		t.Fatalf("UpsertHistoricalOHLCVCoverage() update error = %v", err)
	}
	if err := repo.UpsertHistoricalOHLCVCoverage(ctx, domain.HistoricalOHLCVCoverage{
		Ticker: "AAPL", Provider: "stock-chain", Timeframe: "1d",
		DateFrom: end, DateTo: end, FetchedAt: secondFetchedAt,
	}); err != nil {
		t.Fatalf("UpsertHistoricalOHLCVCoverage() second range error = %v", err)
	}

	gotCoverage, err := repo.ListHistoricalOHLCVCoverage(ctx, repository.HistoricalOHLCVCoverageFilter{
		Ticker:    "AAPL",
		Provider:  "stock-chain",
		Timeframe: "1d",
		From:      start,
		To:        end,
	})
	if err != nil {
		t.Fatalf("ListHistoricalOHLCVCoverage() error = %v", err)
	}

	if len(gotCoverage) != 2 {
		t.Fatalf("len(gotCoverage) = %d, want 2", len(gotCoverage))
	}
	if !gotCoverage[0].DateFrom.Equal(start) || !gotCoverage[0].DateTo.Equal(mid) {
		t.Fatalf("coverage[0] = %#v, want %v..%v", gotCoverage[0], start, mid)
	}
	if !gotCoverage[0].FetchedAt.Equal(secondFetchedAt) {
		t.Fatalf("coverage[0].FetchedAt = %v, want %v", gotCoverage[0].FetchedAt, secondFetchedAt)
	}
	if !gotCoverage[1].DateFrom.Equal(end) || !gotCoverage[1].DateTo.Equal(end) {
		t.Fatalf("coverage[1] = %#v, want %v..%v", gotCoverage[1], end, end)
	}
}

func newHistoricalOHLCVIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_histohlcv_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TABLE historical_ohlcv (
			ticker     TEXT             NOT NULL,
			provider   TEXT             NOT NULL,
			timeframe  TEXT             NOT NULL,
			bar_time   TIMESTAMPTZ      NOT NULL,
			open       DOUBLE PRECISION NOT NULL,
			high       DOUBLE PRECISION NOT NULL,
			low        DOUBLE PRECISION NOT NULL,
			close      DOUBLE PRECISION NOT NULL,
			volume     DOUBLE PRECISION NOT NULL,
			PRIMARY KEY (ticker, provider, timeframe, bar_time)
		)`,
		`CREATE INDEX idx_historical_ohlcv_ticker_provider_timeframe_bar_time
			ON historical_ohlcv (ticker, provider, timeframe, bar_time)`,
		`CREATE TABLE historical_ohlcv_coverage (
			ticker     TEXT        NOT NULL,
			provider   TEXT        NOT NULL,
			timeframe  TEXT        NOT NULL,
			range_from TIMESTAMPTZ NOT NULL,
			range_to   TIMESTAMPTZ NOT NULL,
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (ticker, provider, timeframe, range_from, range_to)
		)`,
		`CREATE INDEX idx_historical_ohlcv_coverage_ticker_provider_timeframe_range
			ON historical_ohlcv_coverage (ticker, provider, timeframe, range_from, range_to)`,
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
