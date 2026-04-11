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
		ticker.Ticker,
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

	const batchSize = 500
	for i := 0; i < len(tickers); i += batchSize {
		end := i + batchSize
		if end > len(tickers) {
			end = len(tickers)
		}
		if err := r.upsertChunk(ctx, tickers[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *UniverseRepo) upsertChunk(ctx context.Context, tickers []universe.TrackedTicker) error {
	var b strings.Builder
	args := make([]any, 0, len(tickers)*7)
	argIdx := 0

	b.WriteString(`INSERT INTO universe_tickers (ticker, name, exchange, index_group, watch_score, last_scanned, active) VALUES `)

	for i, t := range tickers {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			argIdx+1, argIdx+2, argIdx+3, argIdx+4, argIdx+5, argIdx+6, argIdx+7)
		args = append(args, t.Ticker, t.Name, t.Exchange, t.IndexGroup, t.WatchScore, t.LastScanned, t.Active)
		argIdx += 7
	}

	b.WriteString(` ON CONFLICT (ticker) DO UPDATE SET
		name         = EXCLUDED.name,
		exchange     = EXCLUDED.exchange,
		index_group  = EXCLUDED.index_group,
		active       = EXCLUDED.active,
		updated_at   = NOW()`)

	_, err := r.pool.Exec(ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("postgres: upsert universe batch: %w", err)
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
		`SELECT ticker, name, exchange, index_group, watch_score, last_scanned, active, created_at, updated_at
		 FROM universe_tickers
		 WHERE active = true
		 ORDER BY watch_score DESC
		 LIMIT $1`,
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

// UpdateScore updates the watch_score for a single ticker.
func (r *UniverseRepo) UpdateScore(ctx context.Context, ticker string, score float64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE universe_tickers SET watch_score = $1, updated_at = NOW() WHERE ticker = $2`,
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
