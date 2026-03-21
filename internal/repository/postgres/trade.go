package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// TradeRepo implements repository.TradeRepository using PostgreSQL.
type TradeRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that TradeRepo satisfies TradeRepository.
var _ repository.TradeRepository = (*TradeRepo)(nil)

// NewTradeRepo returns a TradeRepo backed by the given connection pool.
func NewTradeRepo(pool *pgxpool.Pool) *TradeRepo {
	return &TradeRepo{pool: pool}
}

// Create inserts a new trade and populates the generated ID and CreatedAt on
// the provided struct.
func (r *TradeRepo) Create(ctx context.Context, trade *domain.Trade) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO trades (
			order_id, position_id, ticker, side, quantity, price, fee, executed_at
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at`,
		trade.OrderID,
		trade.PositionID,
		trade.Ticker,
		trade.Side,
		trade.Quantity,
		trade.Price,
		trade.Fee,
		trade.ExecutedAt,
	)

	if err := row.Scan(&trade.ID, &trade.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create trade: %w", err)
	}

	return nil
}

// GetByOrder returns trades for the given order with optional filtering and
// pagination.
func (r *TradeRepo) GetByOrder(ctx context.Context, orderID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	query, args := buildTradeScopedListQuery("order_id", orderID, filter, limit, offset)
	return r.list(ctx, query, args, "get trades by order")
}

// GetByPosition returns trades for the given position with optional filtering
// and pagination.
func (r *TradeRepo) GetByPosition(ctx context.Context, positionID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	query, args := buildTradeScopedListQuery("position_id", positionID, filter, limit, offset)
	return r.list(ctx, query, args, "get trades by position")
}

const tradeSelectSQL = `SELECT id, order_id, position_id, ticker, side,
		quantity::double precision, price::double precision, fee::double precision,
		executed_at, created_at
	 FROM trades`

func (r *TradeRepo) list(ctx context.Context, query string, args []any, op string) ([]domain.Trade, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: %s: %w", op, err)
	}
	defer rows.Close()

	var trades []domain.Trade
	for rows.Next() {
		trade, err := scanTrade(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: %s scan: %w", op, err)
		}
		trades = append(trades, *trade)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: %s rows: %w", op, err)
	}

	return trades, nil
}

// scanTrade scans a single row (pgx.Row or pgx.Rows) into a Trade. Nullable
// columns are scanned via pointer intermediates and converted to the Go zero
// value when NULL.
func scanTrade(sc scanner) (*domain.Trade, error) {
	var (
		trade      domain.Trade
		orderID    *uuid.UUID
		positionID *uuid.UUID
	)

	err := sc.Scan(
		&trade.ID,
		&orderID,
		&positionID,
		&trade.Ticker,
		&trade.Side,
		&trade.Quantity,
		&trade.Price,
		&trade.Fee,
		&trade.ExecutedAt,
		&trade.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	trade.OrderID = orderID
	trade.PositionID = positionID

	return &trade, nil
}

// buildTradeScopedListQuery constructs the SELECT query and arguments for
// GetByOrder and GetByPosition using the supplied fixed scope.
func buildTradeScopedListQuery(scopeColumn string, scopeValue uuid.UUID, filter repository.TradeFilter, limit, offset int) (string, []any) {
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

	conditions = append(conditions, scopeColumn+" = "+nextArg(scopeValue))

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}

	if filter.Side != "" {
		conditions = append(conditions, "side = "+nextArg(filter.Side))
	}

	if filter.ExecutedAfter != nil {
		conditions = append(conditions, "executed_at >= "+nextArg(*filter.ExecutedAfter))
	}

	if filter.ExecutedBefore != nil {
		conditions = append(conditions, "executed_at <= "+nextArg(*filter.ExecutedBefore))
	}

	query := tradeSelectSQL + " WHERE " + strings.Join(conditions, " AND ")
	query += " ORDER BY executed_at DESC, created_at DESC, id DESC"
	query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return query, args
}
