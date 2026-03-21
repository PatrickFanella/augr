package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// MarketDataCacheRepo implements repository.MarketDataCacheRepository using PostgreSQL.
type MarketDataCacheRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that MarketDataCacheRepo satisfies MarketDataCacheRepository.
var _ repository.MarketDataCacheRepository = (*MarketDataCacheRepo)(nil)

// NewMarketDataCacheRepo returns a MarketDataCacheRepo backed by the given connection pool.
func NewMarketDataCacheRepo(pool *pgxpool.Pool) *MarketDataCacheRepo {
	return &MarketDataCacheRepo{pool: pool}
}

// Get retrieves a non-expired cache entry matching the provided key. It returns
// ErrNotFound when no valid (non-expired) entry exists.
func (r *MarketDataCacheRepo) Get(ctx context.Context, key repository.MarketDataCacheKey) (*domain.MarketData, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, ticker, provider, data_type, timeframe, date_from, date_to,
		        data, fetched_at, expires_at
		   FROM market_data_cache
		  WHERE ticker    = $1
		    AND provider  = $2
		    AND data_type = $3
		    AND COALESCE(timeframe, '') = $4
		    AND date_from IS NOT DISTINCT FROM $5::DATE
		    AND date_to   IS NOT DISTINCT FROM $6::DATE
		    AND (expires_at IS NULL OR expires_at > $7)
		  ORDER BY fetched_at DESC
		  LIMIT 1`,
		key.Ticker,
		key.Provider,
		key.DataType,
		key.Timeframe,
		timeToDatePtr(key.DateFrom),
		timeToDatePtr(key.DateTo),
		time.Now().UTC(),
	)

	md, err := scanMarketData(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("postgres: get market data cache %s/%s/%s: %w",
				key.Ticker, key.Provider, key.DataType, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get market data cache: %w", err)
	}

	return md, nil
}

// Set stores or replaces a cache entry. Any existing entry with the same
// (ticker, provider, data_type, timeframe, date_from, date_to) key is deleted
// before the new entry is inserted. The ExpiresAt field on data controls TTL.
func (r *MarketDataCacheRepo) Set(ctx context.Context, data *domain.MarketData) error {
	row := r.pool.QueryRow(ctx,
		`WITH deleted AS (
			DELETE FROM market_data_cache
			 WHERE ticker    = $1
			   AND provider  = $2
			   AND data_type = $3
			   AND COALESCE(timeframe, '') = $4
			   AND date_from IS NOT DISTINCT FROM $5::DATE
			   AND date_to   IS NOT DISTINCT FROM $6::DATE
		)
		INSERT INTO market_data_cache
			(ticker, provider, data_type, timeframe, date_from, date_to,
			 data, fetched_at, expires_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5::DATE, $6::DATE, $7, $8, $9)
		RETURNING id, fetched_at`,
		data.Ticker,
		data.Provider,
		data.DataType,
		data.Timeframe,
		timeToDatePtr(data.DateFrom),
		timeToDatePtr(data.DateTo),
		[]byte(data.Data),
		data.FetchedAt,
		nullTime(data.ExpiresAt),
	)

	if err := row.Scan(&data.ID, &data.FetchedAt); err != nil {
		return fmt.Errorf("postgres: set market data cache: %w", err)
	}

	return nil
}

// Expire deletes cache entries whose expires_at is before the given threshold.
// The filter may optionally restrict deletion to a specific ticker, provider,
// or data_type.
func (r *MarketDataCacheRepo) Expire(ctx context.Context, filter repository.MarketDataCacheExpireFilter) error {
	query, args := buildExpireQuery(filter)

	if _, err := r.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("postgres: expire market data cache: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanMarketData scans a single row (pgx.Row or pgx.Rows) into a MarketData.
func scanMarketData(sc scanner) (*domain.MarketData, error) {
	var (
		md        domain.MarketData
		timeframe *string
		dateFrom  *time.Time
		dateTo    *time.Time
		expiresAt *time.Time
	)

	if err := sc.Scan(
		&md.ID,
		&md.Ticker,
		&md.Provider,
		&md.DataType,
		&timeframe,
		&dateFrom,
		&dateTo,
		&md.Data,
		&md.FetchedAt,
		&expiresAt,
	); err != nil {
		return nil, err
	}

	if timeframe != nil {
		md.Timeframe = *timeframe
	}
	md.DateFrom = dateFrom
	md.DateTo = dateTo
	if expiresAt != nil {
		md.ExpiresAt = *expiresAt
	}

	return &md, nil
}

// buildExpireQuery constructs the DELETE query and arguments for Expire with
// optional filter conditions. All values are parameterized.
func buildExpireQuery(filter repository.MarketDataCacheExpireFilter) (string, []any) {
	var (
		conditions []string
		args       []any
		argIdx     int
	)

	nextArg := func(v any) string {
		argIdx++
		args = append(args, v)
		return fmt.Sprintf("$%d", argIdx)
	}

	conditions = append(conditions, "expires_at < "+nextArg(filter.ExpiresBefore))

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}
	if filter.Provider != "" {
		conditions = append(conditions, "provider = "+nextArg(filter.Provider))
	}
	if filter.DataType != "" {
		conditions = append(conditions, "data_type = "+nextArg(filter.DataType))
	}

	query := "DELETE FROM market_data_cache WHERE " + strings.Join(conditions, " AND ")

	return query, args
}

// timeToDatePtr converts a *time.Time to a *time.Time truncated to the date
// component, suitable for DATE columns. Returns nil when t is nil.
func timeToDatePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}

// nullTime returns nil for a zero time.Time, otherwise returns the value.
// This prevents storing a zero timestamp in TIMESTAMPTZ columns.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
