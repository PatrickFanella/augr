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

// PositionRepo implements repository.PositionRepository using PostgreSQL.
type PositionRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that PositionRepo satisfies PositionRepository.
var _ repository.PositionRepository = (*PositionRepo)(nil)

// NewPositionRepo returns a PositionRepo backed by the given connection pool.
func NewPositionRepo(pool *pgxpool.Pool) *PositionRepo {
	return &PositionRepo{pool: pool}
}

// Create inserts a new position and populates the generated ID and OpenedAt on
// the provided struct.
func (r *PositionRepo) Create(ctx context.Context, position *domain.Position) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO positions (
			strategy_id, ticker, side, quantity, avg_entry,
			current_price, unrealized_pnl, realized_pnl,
			stop_loss, take_profit, closed_at
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, opened_at`,
		position.StrategyID,
		position.Ticker,
		position.Side,
		position.Quantity,
		position.AvgEntry,
		position.CurrentPrice,
		position.UnrealizedPnL,
		position.RealizedPnL,
		position.StopLoss,
		position.TakeProfit,
		position.ClosedAt,
	)

	if err := row.Scan(&position.ID, &position.OpenedAt); err != nil {
		return fmt.Errorf("postgres: create position: %w", err)
	}

	return nil
}

// Get retrieves a position by ID. It returns ErrNotFound when no row matches.
func (r *PositionRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Position, error) {
	row := r.pool.QueryRow(ctx, positionSelectSQL+` WHERE id = $1`, id)

	position, err := scanPosition(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get position %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get position: %w", err)
	}

	return position, nil
}

// List returns positions matching the provided filter with pagination.
func (r *PositionRepo) List(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	query, args := buildPositionListQuery(filter, limit, offset)
	return r.list(ctx, query, args, "list positions")
}

// Update persists changes to an existing position. It returns ErrNotFound when
// no row matches the position ID.
func (r *PositionRepo) Update(ctx context.Context, position *domain.Position) error {
	row := r.pool.QueryRow(ctx,
		`UPDATE positions
		 SET strategy_id = $1,
		     ticker = $2,
		     side = $3,
		     quantity = $4,
		     avg_entry = $5,
		     current_price = $6,
		     unrealized_pnl = $7,
		     realized_pnl = $8,
		     stop_loss = $9,
		     take_profit = $10,
		     closed_at = $11
		 WHERE id = $12
		 RETURNING id`,
		position.StrategyID,
		position.Ticker,
		position.Side,
		position.Quantity,
		position.AvgEntry,
		position.CurrentPrice,
		position.UnrealizedPnL,
		position.RealizedPnL,
		position.StopLoss,
		position.TakeProfit,
		position.ClosedAt,
		position.ID,
	)

	var updatedID uuid.UUID
	if err := row.Scan(&updatedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update position %s: %w", position.ID, ErrNotFound)
		}
		return fmt.Errorf("postgres: update position: %w", err)
	}

	return nil
}

// Delete removes a position by ID. It returns ErrNotFound when no row matches.
func (r *PositionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM positions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete position: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete position %s: %w", id, ErrNotFound)
	}

	return nil
}

// GetOpen returns positions that have not been closed (closed_at IS NULL),
// matching the provided filter with pagination.
func (r *PositionRepo) GetOpen(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	query, args := buildPositionOpenQuery(filter, limit, offset)
	return r.list(ctx, query, args, "get open positions")
}

// GetByStrategy returns positions for the given strategy with optional
// filtering and pagination.
func (r *PositionRepo) GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	query, args := buildPositionScopedQuery("strategy_id", strategyID, filter, limit, offset)
	return r.list(ctx, query, args, "get positions by strategy")
}

const positionSelectSQL = `SELECT id, strategy_id, ticker, side,
		quantity::double precision, avg_entry::double precision,
		current_price::double precision, unrealized_pnl::double precision,
		realized_pnl::double precision, stop_loss::double precision,
		take_profit::double precision, opened_at, closed_at
	 FROM positions`

func (r *PositionRepo) list(ctx context.Context, query string, args []any, op string) ([]domain.Position, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: %s: %w", op, err)
	}
	defer rows.Close()

	var positions []domain.Position
	for rows.Next() {
		position, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: %s scan: %w", op, err)
		}
		positions = append(positions, *position)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: %s rows: %w", op, err)
	}

	return positions, nil
}

// scanPosition scans a single row (pgx.Row or pgx.Rows) into a Position.
// Nullable columns are scanned via pointer intermediates and converted to the Go
// zero value when NULL.
func scanPosition(sc scanner) (*domain.Position, error) {
	var (
		position      domain.Position
		strategyID    *uuid.UUID
		currentPrice  *float64
		unrealizedPnL *float64
		stopLoss      *float64
		takeProfit    *float64
		closedAt      *time.Time
	)

	err := sc.Scan(
		&position.ID,
		&strategyID,
		&position.Ticker,
		&position.Side,
		&position.Quantity,
		&position.AvgEntry,
		&currentPrice,
		&unrealizedPnL,
		&position.RealizedPnL,
		&stopLoss,
		&takeProfit,
		&position.OpenedAt,
		&closedAt,
	)
	if err != nil {
		return nil, err
	}

	position.StrategyID = strategyID
	position.CurrentPrice = currentPrice
	position.UnrealizedPnL = unrealizedPnL
	position.StopLoss = stopLoss
	position.TakeProfit = takeProfit
	position.ClosedAt = closedAt

	return &position, nil
}

// buildPositionListQuery constructs the SELECT query and arguments for List
// with dynamic WHERE conditions.
func buildPositionListQuery(filter repository.PositionFilter, limit, offset int) (string, []any) {
	return buildPositionQuery("", nil, false, filter, limit, offset)
}

// buildPositionOpenQuery constructs the SELECT query and arguments for GetOpen,
// filtering to positions where closed_at IS NULL.
func buildPositionOpenQuery(filter repository.PositionFilter, limit, offset int) (string, []any) {
	return buildPositionQuery("", nil, true, filter, limit, offset)
}

// buildPositionScopedQuery constructs the SELECT query and arguments for
// GetByStrategy using the supplied fixed scope column and value.
func buildPositionScopedQuery(scopeColumn string, scopeValue uuid.UUID, filter repository.PositionFilter, limit, offset int) (string, []any) {
	return buildPositionQuery(scopeColumn, scopeValue, false, filter, limit, offset)
}

func buildPositionQuery(scopeColumn string, scopeValue any, openOnly bool, filter repository.PositionFilter, limit, offset int) (string, []any) {
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

	if openOnly {
		conditions = append(conditions, "closed_at IS NULL")
	}

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}

	if filter.Side != "" {
		conditions = append(conditions, "side = "+nextArg(filter.Side))
	}

	if filter.OpenedAfter != nil {
		conditions = append(conditions, "opened_at >= "+nextArg(*filter.OpenedAfter))
	}

	if filter.OpenedBefore != nil {
		conditions = append(conditions, "opened_at <= "+nextArg(*filter.OpenedBefore))
	}

	query := positionSelectSQL
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY opened_at DESC, id DESC"
	query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return query, args
}
