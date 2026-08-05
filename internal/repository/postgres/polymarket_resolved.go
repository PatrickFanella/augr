package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PolymarketResolvedMarketsRepo stores processed resolution markers.
type PolymarketResolvedMarketsRepo struct{ pool *pgxpool.Pool }

var _ repository.PolymarketResolvedMarketsRepository = (*PolymarketResolvedMarketsRepo)(nil)

// NewPolymarketResolvedMarketsRepo creates a resolved-markets repository.
func NewPolymarketResolvedMarketsRepo(pool *pgxpool.Pool) *PolymarketResolvedMarketsRepo {
	return &PolymarketResolvedMarketsRepo{pool: pool}
}

func (r *PolymarketResolvedMarketsRepo) IsProcessed(ctx context.Context, slug string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM polymarket_resolved_markets WHERE slug=$1)`, slug).Scan(&ok)
	return ok, err
}

func (r *PolymarketResolvedMarketsRepo) MarkProcessed(ctx context.Context, slug, winningSide string, resolvedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO polymarket_resolved_markets (slug, winning_side, resolved_at) VALUES ($1,$2,$3) ON CONFLICT (slug) DO UPDATE SET winning_side=EXCLUDED.winning_side, resolved_at=EXCLUDED.resolved_at, processed_at=NOW()`, slug, winningSide, resolvedAt)
	return err
}
