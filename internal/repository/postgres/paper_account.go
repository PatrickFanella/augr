package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PaperAccountRepo provides provenance-safe reads for paper-account restoration.
type PaperAccountRepo struct{ pool *DB }

var _ repository.PaperAccountRepository = (*PaperAccountRepo)(nil)

func NewPaperAccountRepo(db *DB) *PaperAccountRepo { return &PaperAccountRepo{pool: db} }

func (r *PaperAccountRepo) ListPaperTrades(ctx context.Context, limit, offset int) ([]domain.Trade, error) {
	rows, err := r.pool.Pool.Query(ctx, `SELECT t.id, t.external_id, t.order_id, t.position_id, t.ticker, t.side,
			t.quantity::double precision, t.price::double precision, t.fee::double precision,
			t.executed_at, t.created_at, t.asset_class, t.open_close,
			COALESCE(t.contract_multiplier, 100)::double precision, COALESCE(t.premium, 0)::double precision,
			COALESCE(t.exit_reason, '')
		FROM trades t
		INNER JOIN orders o ON o.id = t.order_id AND o.broker = 'paper'
		ORDER BY t.executed_at DESC, t.created_at DESC, t.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list paper trades: %w", err)
	}
	defer rows.Close()
	var out []domain.Trade
	for rows.Next() {
		tr, err := scanTrade(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list paper trades scan: %w", err)
		}
		out = append(out, *tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list paper trades rows: %w", err)
	}
	return out, nil
}

func (r *PaperAccountRepo) GetOpenPaperPositions(ctx context.Context, limit, offset int) ([]domain.Position, error) {
	rows, err := r.pool.Pool.Query(ctx, `SELECT p.id, p.strategy_id, s.market_type, p.ticker, p.side,
		p.quantity::double precision, p.avg_entry::double precision,
		p.current_price::double precision, p.unrealized_pnl::double precision,
		p.realized_pnl::double precision, p.stop_loss::double precision,
		p.take_profit::double precision, p.opened_at, p.closed_at,
		p.asset_class, p.underlying_ticker, p.option_type, p.strike::double precision,
		p.expiry, p.contract_multiplier::double precision, p.leg_group_id,
		p.delta::double precision, p.gamma::double precision, p.theta::double precision,
		p.vega::double precision
		FROM positions p
		INNER JOIN strategies s ON s.id = p.strategy_id AND s.is_paper = true
		WHERE p.closed_at IS NULL
		AND EXISTS (
			SELECT 1
			FROM trades t
			INNER JOIN orders o ON o.id = t.order_id
			WHERE t.position_id = p.id AND o.broker = 'paper'
		)
		ORDER BY p.opened_at ASC, p.id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: get open paper positions: %w", err)
	}
	defer rows.Close()
	var out []domain.Position
	for rows.Next() {
		pos, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: get open paper positions scan: %w", err)
		}
		out = append(out, *pos)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get open paper positions rows: %w", err)
	}
	return out, nil
}

func (r *PaperAccountRepo) ListOpenPaperOrders(ctx context.Context, limit, offset int) ([]domain.Order, error) {
	rows, err := r.pool.Pool.Query(ctx, `SELECT o.id, o.strategy_id, o.pipeline_run_id, o.external_id, o.ticker,
		o.market_type, o.side, o.order_type, o.quantity::double precision, o.limit_price::double precision,
		o.stop_price::double precision, o.filled_quantity::double precision, o.filled_avg_price::double precision,
		o.status, o.broker, o.submitted_at, o.filled_at, o.created_at, o.asset_class, o.underlying_ticker,
		o.option_type, o.strike::double precision, o.expiry, o.contract_multiplier::double precision,
		COALESCE(o.position_intent, ''), o.leg_group_id, COALESCE(o.prediction_side, ''), COALESCE(o.polymarket_intent, '')
		FROM orders o
		WHERE o.broker = 'paper' AND o.status IN ('submitted', 'partial')
		ORDER BY o.submitted_at ASC, o.id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list open paper orders: %w", err)
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		ord, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list open paper orders scan: %w", err)
		}
		out = append(out, *ord)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list open paper orders rows: %w", err)
	}
	return out, nil
}

func (r *PaperAccountRepo) GetMaxPaperExternalIDSequence(ctx context.Context) (uint64, error) {
	var maxSeq sql.NullInt64
	if err := r.pool.Pool.QueryRow(ctx, `SELECT COALESCE(MAX((regexp_match(external_id, '^paper-([0-9]+)$'))[1]::bigint), 0) FROM orders WHERE broker = 'paper' AND external_id ~ '^paper-[0-9]+$'`).Scan(&maxSeq); err != nil {
		return 0, fmt.Errorf("postgres: get max paper external id sequence: %w", err)
	}
	if !maxSeq.Valid || maxSeq.Int64 < 0 {
		return 0, nil
	}
	return uint64(maxSeq.Int64), nil
}
