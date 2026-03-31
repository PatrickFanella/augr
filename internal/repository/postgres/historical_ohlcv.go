package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

var _ repository.HistoricalOHLCVRepository = (*MarketDataCacheRepo)(nil)

const historicalOHLCVUpsertBatchSize = 500

// UpsertHistoricalOHLCV stores historical bars in a queryable, indexed table.
func (r *MarketDataCacheRepo) UpsertHistoricalOHLCV(ctx context.Context, bars []domain.HistoricalOHLCV) error {
	if len(bars) == 0 {
		return nil
	}

	for start := 0; start < len(bars); start += historicalOHLCVUpsertBatchSize {
		end := start + historicalOHLCVUpsertBatchSize
		if end > len(bars) {
			end = len(bars)
		}
		if err := r.upsertHistoricalOHLCVBatch(ctx, bars[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func (r *MarketDataCacheRepo) upsertHistoricalOHLCVBatch(ctx context.Context, bars []domain.HistoricalOHLCV) (err error) {
	var batch pgx.Batch
	for _, bar := range bars {
		batch.Queue(
			`INSERT INTO historical_ohlcv
				(ticker, provider, timeframe, bar_time, open, high, low, close, volume)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 ON CONFLICT (ticker, provider, timeframe, bar_time)
			 DO UPDATE SET
			 	open = EXCLUDED.open,
			 	high = EXCLUDED.high,
			 	low = EXCLUDED.low,
			 	close = EXCLUDED.close,
			 	volume = EXCLUDED.volume`,
			bar.Ticker,
			bar.Provider,
			bar.Timeframe,
			bar.Timestamp.UTC(),
			bar.Open,
			bar.High,
			bar.Low,
			bar.Close,
			bar.Volume,
		)
	}

	results := r.pool.SendBatch(ctx, &batch)
	defer func() {
		if closeErr := results.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("postgres: close historical ohlcv batch: %w", closeErr)
		}
	}()
	for range bars {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("postgres: upsert historical ohlcv: %w", err)
		}
	}

	return nil
}

// ListHistoricalOHLCV returns stored bars filtered by ticker/timeframe/date range.
func (r *MarketDataCacheRepo) ListHistoricalOHLCV(ctx context.Context, filter repository.HistoricalOHLCVFilter) ([]domain.HistoricalOHLCV, error) {
	query, args := buildHistoricalOHLCVQuery(filter)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list historical ohlcv: %w", err)
	}
	defer rows.Close()

	var bars []domain.HistoricalOHLCV
	for rows.Next() {
		var bar domain.HistoricalOHLCV
		if err := rows.Scan(
			&bar.Ticker,
			&bar.Provider,
			&bar.Timeframe,
			&bar.Timestamp,
			&bar.Open,
			&bar.High,
			&bar.Low,
			&bar.Close,
			&bar.Volume,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan historical ohlcv: %w", err)
		}
		bars = append(bars, bar)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate historical ohlcv: %w", err)
	}

	return bars, nil
}

// UpsertHistoricalOHLCVCoverage stores a fetched range for incremental syncs.
func (r *MarketDataCacheRepo) UpsertHistoricalOHLCVCoverage(ctx context.Context, coverage domain.HistoricalOHLCVCoverage) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO historical_ohlcv_coverage
			(ticker, provider, timeframe, range_from, range_to, fetched_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (ticker, provider, timeframe, range_from, range_to)
		 DO UPDATE SET fetched_at = EXCLUDED.fetched_at`,
		coverage.Ticker,
		coverage.Provider,
		coverage.Timeframe,
		coverage.DateFrom.UTC(),
		coverage.DateTo.UTC(),
		coverage.FetchedAt.UTC(),
	); err != nil {
		return fmt.Errorf("postgres: upsert historical ohlcv coverage: %w", err)
	}

	return nil
}

// ListHistoricalOHLCVCoverage returns downloaded coverage windows ordered by start time.
func (r *MarketDataCacheRepo) ListHistoricalOHLCVCoverage(ctx context.Context, filter repository.HistoricalOHLCVCoverageFilter) ([]domain.HistoricalOHLCVCoverage, error) {
	query, args := buildHistoricalOHLCVCoverageQuery(filter)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list historical ohlcv coverage: %w", err)
	}
	defer rows.Close()

	var coverage []domain.HistoricalOHLCVCoverage
	for rows.Next() {
		var item domain.HistoricalOHLCVCoverage
		if err := rows.Scan(
			&item.Ticker,
			&item.Provider,
			&item.Timeframe,
			&item.DateFrom,
			&item.DateTo,
			&item.FetchedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan historical ohlcv coverage: %w", err)
		}
		coverage = append(coverage, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate historical ohlcv coverage: %w", err)
	}

	return coverage, nil
}

func buildHistoricalOHLCVQuery(filter repository.HistoricalOHLCVFilter) (string, []any) {
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

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}
	if filter.Provider != "" {
		conditions = append(conditions, "provider = "+nextArg(filter.Provider))
	}
	if filter.Timeframe != "" {
		conditions = append(conditions, "timeframe = "+nextArg(filter.Timeframe))
	}
	if !filter.From.IsZero() {
		conditions = append(conditions, "bar_time >= "+nextArg(filter.From.UTC()))
	}
	if !filter.To.IsZero() {
		conditions = append(conditions, "bar_time <= "+nextArg(filter.To.UTC()))
	}

	query := `SELECT ticker, provider, timeframe, bar_time, open, high, low, close, volume
		FROM historical_ohlcv`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY bar_time ASC"

	return query, args
}

func buildHistoricalOHLCVCoverageQuery(filter repository.HistoricalOHLCVCoverageFilter) (string, []any) {
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

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}
	if filter.Provider != "" {
		conditions = append(conditions, "provider = "+nextArg(filter.Provider))
	}
	if filter.Timeframe != "" {
		conditions = append(conditions, "timeframe = "+nextArg(filter.Timeframe))
	}
	if !filter.To.IsZero() {
		conditions = append(conditions, "range_from <= "+nextArg(filter.To.UTC()))
	}
	if !filter.From.IsZero() {
		conditions = append(conditions, "range_to >= "+nextArg(filter.From.UTC()))
	}

	query := `SELECT ticker, provider, timeframe, range_from, range_to, fetched_at
		FROM historical_ohlcv_coverage`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY range_from ASC, range_to ASC"

	return query, args
}
