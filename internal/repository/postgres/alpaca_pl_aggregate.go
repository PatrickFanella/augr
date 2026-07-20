package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AlpacaPLAggregateRepo provides read-only Alpaca-only local P/L aggregates.
type AlpacaPLAggregateRepo struct {
	pool *pgxpool.Pool
}

var _ repository.AlpacaPLAggregateRepository = (*AlpacaPLAggregateRepo)(nil)

func NewAlpacaPLAggregateRepo(pool *pgxpool.Pool) *AlpacaPLAggregateRepo {
	return &AlpacaPLAggregateRepo{pool: pool}
}

func (r *AlpacaPLAggregateRepo) ClosedRealizedPnL(ctx context.Context) (float64, error) {
	var total float64
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(COALESCE(p.realized_pnl, 0)), 0)
		FROM positions p
		WHERE p.closed_at IS NOT NULL AND (
			EXISTS (SELECT 1 FROM position_provenance pp WHERE pp.position_id = p.id AND pp.broker = 'alpaca') OR
			EXISTS (
				SELECT 1
				FROM trades t
				JOIN orders o ON o.id = t.order_id
				WHERE t.position_id = p.id AND o.broker = 'alpaca'
			)
		)`).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: alpaca closed realized pnl: %w", err)
	}
	return total, nil
}

func (r *AlpacaPLAggregateRepo) OpenUnrealizedPnL(ctx context.Context) (float64, error) {
	var total float64
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(COALESCE(p.unrealized_pnl, 0)), 0)
		FROM positions p
		WHERE p.closed_at IS NULL AND (
			EXISTS (SELECT 1 FROM position_provenance pp WHERE pp.position_id = p.id AND pp.broker = 'alpaca') OR
			EXISTS (
				SELECT 1
				FROM trades t
				JOIN orders o ON o.id = t.order_id
				WHERE t.position_id = p.id AND o.broker = 'alpaca'
			)
		)`).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: alpaca open unrealized pnl: %w", err)
	}
	return total, nil
}

func (r *AlpacaPLAggregateRepo) TradeCount(ctx context.Context) (int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)
		FROM trades t
		JOIN orders o ON o.id = t.order_id
		WHERE o.broker = 'alpaca'`).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: alpaca trade count: %w", err)
	}
	return total, nil
}

func (r *AlpacaPLAggregateRepo) FeeTotal(ctx context.Context) (float64, error) {
	var total float64
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(COALESCE(t.fee, 0)), 0)
		FROM trades t
		JOIN orders o ON o.id = t.order_id
		WHERE o.broker = 'alpaca'`).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: alpaca fee total: %w", err)
	}
	return total, nil
}
