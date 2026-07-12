package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// OrderRepo implements repository.OrderRepository using PostgreSQL.
type OrderRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that OrderRepo satisfies OrderRepository.
var _ repository.OrderRepository = (*OrderRepo)(nil)

// NewOrderRepo returns an OrderRepo backed by the given connection pool.
func NewOrderRepo(pool *pgxpool.Pool) *OrderRepo {
	return &OrderRepo{pool: pool}
}

// Create inserts a new order and populates the generated ID and CreatedAt on
// the provided struct.
func (r *OrderRepo) Create(ctx context.Context, order *domain.Order) error {
	marketType := order.MarketType.Normalize()
	if marketType == "" {
		marketType = domain.MarketTypeStock
	}
	order.MarketType = marketType

	row := r.pool.QueryRow(ctx,
		`INSERT INTO orders (
			strategy_id, pipeline_run_id, external_id, ticker, market_type, side, order_type,
			quantity, limit_price, stop_price, filled_quantity, filled_avg_price,
			status, broker, submitted_at, filled_at, asset_class, underlying_ticker,
			option_type, strike, expiry, contract_multiplier, position_intent, leg_group_id,
			prediction_side, polymarket_intent
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
		 RETURNING id, created_at`,
		order.StrategyID,
		order.PipelineRunID,
		nullString(order.ExternalID),
		order.Ticker,
		marketType,
		order.Side,
		order.OrderType,
		order.Quantity,
		order.LimitPrice,
		order.StopPrice,
		order.FilledQuantity,
		order.FilledAvgPrice,
		order.Status,
		nullString(order.Broker),
		order.SubmittedAt,
		order.FilledAt,
		order.AssetClass,
		nullString(order.UnderlyingTicker),
		order.OptionType,
		order.Strike,
		order.Expiry,
		order.ContractMultiplier,
		order.PositionIntent,
		order.LegGroupID,
		nullString(order.PredictionSide),
		nullString(order.PolymarketIntent),
	)

	if err := row.Scan(&order.ID, &order.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create order: %w", err)
	}

	return nil
}

// Get retrieves an order by ID. It returns ErrNotFound when no row matches.
func (r *OrderRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx, orderSelectSQL+` WHERE id = $1`, id)

	order, err := scanOrder(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get order %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get order: %w", err)
	}

	return order, nil
}

// List returns orders matching the provided filter with pagination.
func (r *OrderRepo) List(ctx context.Context, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	query, args := buildOrderListQuery(filter, limit, offset)
	return r.list(ctx, query, args, "list orders")
}

// Update persists changes to an existing order. It returns ErrNotFound when no
// row matches the order ID.
func (r *OrderRepo) Update(ctx context.Context, order *domain.Order) error {
	marketType := order.MarketType.Normalize()
	if marketType == "" {
		marketType = domain.MarketTypeStock
	}
	order.MarketType = marketType

	row := r.pool.QueryRow(ctx,
		`UPDATE orders
		 SET strategy_id = $1,
		     pipeline_run_id = $2,
		     external_id = $3,
		     ticker = $4,
		     market_type = $5,
		     side = $6,
		     order_type = $7,
		     quantity = $8,
		     limit_price = $9,
		     stop_price = $10,
		     filled_quantity = $11,
		     filled_avg_price = $12,
		     status = $13,
		     broker = $14,
		     submitted_at = $15,
		     filled_at = $16
		     , asset_class = $17
		     , underlying_ticker = $18
		     , option_type = $19
		     , strike = $20
		     , expiry = $21
		     , contract_multiplier = $22
		     , position_intent = $23
		     , leg_group_id = $24
		     , prediction_side = $25
		     , polymarket_intent = $26
		 WHERE id = $27
		 RETURNING id`,
		order.StrategyID,
		order.PipelineRunID,
		nullString(order.ExternalID),
		order.Ticker,
		marketType,
		order.Side,
		order.OrderType,
		order.Quantity,
		order.LimitPrice,
		order.StopPrice,
		order.FilledQuantity,
		order.FilledAvgPrice,
		order.Status,
		nullString(order.Broker),
		order.SubmittedAt,
		order.FilledAt,
		order.AssetClass,
		nullString(order.UnderlyingTicker),
		order.OptionType,
		order.Strike,
		order.Expiry,
		order.ContractMultiplier,
		order.PositionIntent,
		order.LegGroupID,
		nullString(order.PredictionSide),
		nullString(order.PolymarketIntent),
		order.ID,
	)

	var updatedID uuid.UUID
	if err := row.Scan(&updatedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update order %s: %w", order.ID, ErrNotFound)
		}
		return fmt.Errorf("postgres: update order: %w", err)
	}

	return nil
}

// Delete removes an order by ID. It returns ErrNotFound when no row matches.
func (r *OrderRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete order: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete order %s: %w", id, ErrNotFound)
	}

	return nil
}

// GetByStrategy returns orders for the given strategy with optional filtering
// and pagination.
func (r *OrderRepo) GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	query, args := buildOrderScopedListQuery("strategy_id", strategyID, filter, limit, offset)
	return r.list(ctx, query, args, "get orders by strategy")
}

// GetByRun returns orders for the given pipeline run with optional filtering
// and pagination.
func (r *OrderRepo) GetByRun(ctx context.Context, runID uuid.UUID, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	query, args := buildOrderScopedListQuery("pipeline_run_id", runID, filter, limit, offset)
	return r.list(ctx, query, args, "get orders by run")
}

const orderSelectSQL = `SELECT id, strategy_id, pipeline_run_id, external_id, ticker, market_type, side,
		order_type, quantity::double precision, limit_price::double precision,
		stop_price::double precision, filled_quantity::double precision,
		filled_avg_price::double precision, status, broker, submitted_at,
		filled_at, created_at, asset_class, underlying_ticker, option_type,
		strike::double precision, expiry, contract_multiplier::double precision,
		position_intent, leg_group_id, prediction_side, polymarket_intent
	 FROM orders`

func (r *OrderRepo) list(ctx context.Context, query string, args []any, op string) ([]domain.Order, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: %s: %w", op, err)
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: %s scan: %w", op, err)
		}
		orders = append(orders, *order)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: %s rows: %w", op, err)
	}

	return orders, nil
}

// scanOrder scans a single row (pgx.Row or pgx.Rows) into an Order. Nullable
// columns are scanned via pointer intermediates and converted to the Go zero
// value when NULL.
func scanOrder(sc scanner) (*domain.Order, error) {
	var (
		order          domain.Order
		strategyID     *uuid.UUID
		pipelineRunID  *uuid.UUID
		externalID     *string
		marketType     *string
		limitPrice     *float64
		stopPrice      *float64
		filledAvgPrice *float64
		broker         *string
		underlying     *string
		submittedAt    *time.Time
		filledAt       *time.Time
	)

	err := sc.Scan(
		&order.ID,
		&strategyID,
		&pipelineRunID,
		&externalID,
		&order.Ticker,
		&marketType,
		&order.Side,
		&order.OrderType,
		&order.Quantity,
		&limitPrice,
		&stopPrice,
		&order.FilledQuantity,
		&filledAvgPrice,
		&order.Status,
		&broker,
		&submittedAt,
		&filledAt,
		&order.CreatedAt,
		&order.AssetClass,
		&underlying,
		&order.OptionType,
		&order.Strike,
		&order.Expiry,
		&order.ContractMultiplier,
		&order.PositionIntent,
		&order.LegGroupID,
		&order.PredictionSide,
		&order.PolymarketIntent,
	)
	if err != nil {
		return nil, err
	}

	order.StrategyID = strategyID
	order.PipelineRunID = pipelineRunID
	order.LimitPrice = limitPrice
	order.StopPrice = stopPrice
	order.FilledAvgPrice = filledAvgPrice
	order.SubmittedAt = submittedAt
	order.FilledAt = filledAt

	if externalID != nil {
		order.ExternalID = *externalID
	}
	if marketType == nil || strings.TrimSpace(*marketType) == "" {
		order.MarketType = domain.MarketTypeStock
	} else {
		order.MarketType = domain.MarketType(strings.TrimSpace(*marketType)).Normalize()
	}
	if broker != nil {
		order.Broker = *broker
	}
	if underlying != nil {
		order.UnderlyingTicker = *underlying
	}

	return &order, nil
}

// buildOrderListQuery constructs the SELECT query and arguments for List with
// dynamic WHERE conditions. All values are parameterized.
// Count returns the total number of orders matching the filter (ignoring pagination).
func (r *OrderRepo) Count(ctx context.Context, filter repository.OrderFilter) (int, error) {
	query, args := buildOrderCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count orders: %w", err)
	}
	return total, nil
}

func buildOrderCountQuery(filter repository.OrderFilter) (string, []any) {
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
	if filter.Broker != "" {
		conditions = append(conditions, "broker = "+nextArg(filter.Broker))
	}
	if filter.MarketType != "" {
		conditions = append(conditions, "market_type = "+nextArg(filter.MarketType.Normalize()))
	}
	if filter.Side != "" {
		conditions = append(conditions, "side = "+nextArg(filter.Side))
	}
	if filter.OrderType != "" {
		conditions = append(conditions, "order_type = "+nextArg(filter.OrderType))
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = "+nextArg(filter.Status))
	}
	if filter.SubmittedAfter != nil {
		conditions = append(conditions, "submitted_at >= "+nextArg(*filter.SubmittedAfter))
	}
	if filter.SubmittedBefore != nil {
		conditions = append(conditions, "submitted_at <= "+nextArg(*filter.SubmittedBefore))
	}
	query := `SELECT COUNT(*) FROM orders`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query, args
}

func buildOrderListQuery(filter repository.OrderFilter, limit, offset int) (string, []any) {
	return buildOrderQuery("", nil, filter, limit, offset)
}

// buildOrderScopedListQuery constructs the SELECT query and arguments for
// GetByStrategy and GetByRun using the supplied fixed scope.
func buildOrderScopedListQuery(scopeColumn string, scopeValue uuid.UUID, filter repository.OrderFilter, limit, offset int) (string, []any) {
	return buildOrderQuery(scopeColumn, scopeValue, filter, limit, offset)
}

func buildOrderQuery(scopeColumn string, scopeValue any, filter repository.OrderFilter, limit, offset int) (string, []any) {
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

	if scopeColumn != "" {
		conditions = append(conditions, scopeColumn+" = "+nextArg(scopeValue))
	}

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}

	if filter.Broker != "" {
		conditions = append(conditions, "broker = "+nextArg(filter.Broker))
	}

	if filter.MarketType != "" {
		conditions = append(conditions, "market_type = "+nextArg(filter.MarketType.Normalize()))
	}

	if filter.Side != "" {
		conditions = append(conditions, "side = "+nextArg(filter.Side))
	}

	if filter.OrderType != "" {
		conditions = append(conditions, "order_type = "+nextArg(filter.OrderType))
	}

	if filter.Status != "" {
		conditions = append(conditions, "status = "+nextArg(filter.Status))
	}

	if filter.SubmittedAfter != nil {
		conditions = append(conditions, "submitted_at >= "+nextArg(*filter.SubmittedAfter))
	}

	if filter.SubmittedBefore != nil {
		conditions = append(conditions, "submitted_at <= "+nextArg(*filter.SubmittedBefore))
	}

	query := orderSelectSQL
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY created_at DESC, id DESC"
	query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return query, args
}

func nullString(value string) any {
	if value == "" {
		return nil
	}

	return value
}
