package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

// UniverseRepo implements universe.UniverseRepository using PostgreSQL.
type UniverseRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that UniverseRepo satisfies UniverseRepository.
var _ universe.UniverseRepository = (*UniverseRepo)(nil)

// NewUniverseRepo returns a UniverseRepo backed by the given connection pool.
func NewUniverseRepo(pool *pgxpool.Pool) *UniverseRepo {
	return &UniverseRepo{pool: pool}
}

// Upsert inserts or updates a single tracked ticker.
func (r *UniverseRepo) Upsert(ctx context.Context, ticker *universe.TrackedTicker) error {
	if ticker == nil {
		return errors.New("postgres: upsert universe ticker: ticker is nil")
	}
	normalizedTicker := canonicalUniverseTicker(ticker.Ticker)
	if normalizedTicker == "" {
		return errors.New("postgres: upsert universe ticker: ticker is empty")
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO universe_tickers (ticker, name, exchange, index_group, watch_score, last_scanned, active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (ticker) DO UPDATE SET
		     name         = EXCLUDED.name,
		     exchange     = EXCLUDED.exchange,
		     index_group  = EXCLUDED.index_group,
		     watch_score  = EXCLUDED.watch_score,
		     last_scanned = EXCLUDED.last_scanned,
		     active       = EXCLUDED.active,
		     updated_at   = NOW()`,
		normalizedTicker,
		ticker.Name,
		ticker.Exchange,
		ticker.IndexGroup,
		ticker.WatchScore,
		ticker.LastScanned,
		ticker.Active,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert universe ticker: %w", err)
	}
	return nil
}

// UpsertBatch inserts or updates a batch of tracked tickers using a single
// round-trip with a CTE.
func (r *UniverseRepo) UpsertBatch(ctx context.Context, tickers []universe.TrackedTicker) error {
	if len(tickers) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(tickers))
	for i := range tickers {
		t := tickers[i]
		t.Ticker = canonicalUniverseTicker(t.Ticker)
		if t.Ticker == "" {
			continue
		}
		if _, ok := seen[t.Ticker]; ok {
			continue
		}
		seen[t.Ticker] = struct{}{}
		if err := r.Upsert(ctx, &t); err != nil {
			return fmt.Errorf("postgres: upsert universe batch: %w", err)
		}
	}
	return nil
}

// List returns tracked tickers matching the provided filter with pagination.
func (r *UniverseRepo) List(ctx context.Context, filter universe.ListFilter, limit, offset int) ([]universe.TrackedTicker, error) {
	query, args := buildUniverseListQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list universe tickers: %w", err)
	}
	defer rows.Close()

	var tickers []universe.TrackedTicker
	for rows.Next() {
		t, err := scanTrackedTicker(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list universe tickers scan: %w", err)
		}
		tickers = append(tickers, *t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list universe tickers rows: %w", err)
	}

	return tickers, nil
}

// Watchlist returns the top N active tickers ordered by watch_score descending.
func (r *UniverseRepo) Watchlist(ctx context.Context, topN int) ([]universe.TrackedTicker, error) {
	rows, err := r.pool.Query(ctx,
		universeWatchlistQuery,
		topN,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: watchlist: %w", err)
	}
	defer rows.Close()

	var tickers []universe.TrackedTicker
	for rows.Next() {
		t, err := scanTrackedTicker(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: watchlist scan: %w", err)
		}
		tickers = append(tickers, *t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: watchlist rows: %w", err)
	}

	return tickers, nil
}

const universeWatchlistQuery = `SELECT normalized_ticker, name, exchange, index_group, watch_score, last_scanned, active, created_at, updated_at
	 FROM (
		 SELECT DISTINCT ON (upper(trim(ticker)))
			upper(trim(ticker)) AS normalized_ticker,
			name, exchange, index_group, watch_score, last_scanned, active, created_at, updated_at
		 FROM universe_tickers
		 WHERE active = true
		 ORDER BY upper(trim(ticker)), watch_score DESC,
			(ticker = upper(trim(ticker))) DESC, updated_at DESC NULLS LAST, created_at DESC
	 ) normalized
	 ORDER BY watch_score DESC, normalized_ticker
	 LIMIT $1`

// UpdateScore updates the watch_score for a single ticker.
func (r *UniverseRepo) UpdateScore(ctx context.Context, ticker string, score float64) error {
	ticker = canonicalUniverseTicker(ticker)
	if ticker == "" {
		return errors.New("postgres: update universe score: ticker is empty")
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE universe_tickers
		 SET watch_score = $1, last_scanned = NOW(), updated_at = NOW()
		 WHERE ticker = $2`,
		score, ticker,
	)
	if err != nil {
		return fmt.Errorf("postgres: update universe score: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: update universe score %s: %w", ticker, ErrNotFound)
	}
	return nil
}

func canonicalUniverseTicker(ticker string) string {
	return strings.ToUpper(strings.TrimSpace(ticker))
}

// Count returns the total number of tickers in the universe.
func (r *UniverseRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM universe_tickers`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres: count universe tickers: %w", err)
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func scanTrackedTicker(sc scanner) (*universe.TrackedTicker, error) {
	var t universe.TrackedTicker
	err := sc.Scan(
		&t.Ticker,
		&t.Name,
		&t.Exchange,
		&t.IndexGroup,
		&t.WatchScore,
		&t.LastScanned,
		&t.Active,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func buildUniverseListQuery(filter universe.ListFilter, limit, offset int) (string, []any) {
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

	if filter.IndexGroup != "" {
		conditions = append(conditions, "index_group = "+nextArg(filter.IndexGroup))
	}

	if filter.Active != nil {
		conditions = append(conditions, "active = "+nextArg(*filter.Active))
	}

	if filter.Search != "" {
		pattern := "%" + filter.Search + "%"
		conditions = append(conditions, fmt.Sprintf("(ticker ILIKE %s OR name ILIKE %s)", nextArg(pattern), nextArg(pattern)))
	}

	base := `SELECT ticker, name, exchange, index_group, watch_score, last_scanned, active, created_at, updated_at
		 FROM universe_tickers`

	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	base += " ORDER BY watch_score DESC"
	if limit <= 0 {
		limit = 10000 // no limit requested — use a large default
	}
	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}
