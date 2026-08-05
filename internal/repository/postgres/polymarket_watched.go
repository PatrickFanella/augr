package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PolymarketWatchedMarketsRepo persists watched market slugs.
type PolymarketWatchedMarketsRepo struct{ pool *pgxpool.Pool }

var _ repository.PolymarketWatchedMarketsRepository = (*PolymarketWatchedMarketsRepo)(nil)

// NewPolymarketWatchedMarketsRepo creates a watched-markets repository.
func NewPolymarketWatchedMarketsRepo(pool *pgxpool.Pool) *PolymarketWatchedMarketsRepo {
	return &PolymarketWatchedMarketsRepo{pool: pool}
}

func (r *PolymarketWatchedMarketsRepo) List(ctx context.Context, onlyEnabled bool) ([]domain.PolymarketWatchedMarket, error) {
	q := `SELECT slug, enabled, added_at, COALESCE(added_by,''), COALESCE(note,'') FROM polymarket_watched_markets`
	if onlyEnabled {
		q += ` WHERE enabled=true`
	}
	q += ` ORDER BY added_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PolymarketWatchedMarket
	for rows.Next() {
		var m domain.PolymarketWatchedMarket
		if err := rows.Scan(&m.Slug, &m.Enabled, &m.AddedAt, &m.AddedBy, &m.Note); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PolymarketWatchedMarketsRepo) Add(ctx context.Context, m *domain.PolymarketWatchedMarket) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO polymarket_watched_markets (slug, enabled, added_at, added_by, note) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (slug) DO UPDATE SET enabled=EXCLUDED.enabled, added_by=EXCLUDED.added_by, note=EXCLUDED.note`, m.Slug, true, m.AddedAt, m.AddedBy, m.Note)
	if err != nil {
		return fmt.Errorf("add watched market: %w", err)
	}
	return nil
}

func (r *PolymarketWatchedMarketsRepo) Remove(ctx context.Context, slug string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM polymarket_watched_markets WHERE slug=$1`, slug)
	return err
}

func (r *PolymarketWatchedMarketsRepo) SetEnabled(ctx context.Context, slug string, enabled bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE polymarket_watched_markets SET enabled=$2 WHERE slug=$1`, slug, enabled)
	return err
}
