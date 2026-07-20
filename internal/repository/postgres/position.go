package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
			stop_loss, take_profit, closed_at, asset_class, underlying_ticker,
			option_type, strike, expiry, contract_multiplier, leg_group_id,
			delta, gamma, theta, vega
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
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
		position.AssetClass, nullString(position.UnderlyingTicker), position.OptionType,
		position.Strike, position.Expiry, position.ContractMultiplier, position.LegGroupID,
		position.Delta, position.Gamma, position.Theta, position.Vega,
	)

	if err := row.Scan(&position.ID, &position.OpenedAt); err != nil {
		return fmt.Errorf("postgres: create position: %w", err)
	}

	return nil
}

// CreateAlpacaOwned atomically creates or reuses an open Alpaca-owned position.
func (r *PositionRepo) CreateAlpacaOwned(ctx context.Context, position *domain.Position) error {
	if position == nil {
		return fmt.Errorf("postgres: create alpaca-owned position: position is nil")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: create alpaca-owned position begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, alpacaOwnedLockKey(position.Ticker, position.Side)); err != nil {
		return fmt.Errorf("postgres: create alpaca-owned position advisory lock: %w", err)
	}

	row := tx.QueryRow(ctx, positionSelectSQL+` WHERE p.closed_at IS NULL AND p.ticker = $1 AND p.side = $2 AND (
		EXISTS (SELECT 1 FROM position_provenance pp WHERE pp.position_id = p.id AND pp.broker = 'alpaca') OR
		EXISTS (SELECT 1 FROM trades t JOIN orders o ON o.id = t.order_id WHERE t.position_id = p.id AND o.broker = 'alpaca')
	) ORDER BY p.opened_at ASC, p.id ASC LIMIT 1`, position.Ticker, position.Side)
	if existing, err := scanPosition(row); err == nil {
		*position = *existing
		return tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres: create alpaca-owned position lookup existing: %w", err)
	}

	insertRow := tx.QueryRow(ctx, `INSERT INTO positions (
		strategy_id, ticker, side, quantity, avg_entry,
		current_price, unrealized_pnl, realized_pnl,
		stop_loss, take_profit, closed_at, asset_class, underlying_ticker,
		option_type, strike, expiry, contract_multiplier, leg_group_id,
		delta, gamma, theta, vega
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
	RETURNING id, opened_at`,
		position.StrategyID, position.Ticker, position.Side, position.Quantity, position.AvgEntry,
		position.CurrentPrice, position.UnrealizedPnL, position.RealizedPnL, position.StopLoss, position.TakeProfit,
		position.ClosedAt, position.AssetClass, nullString(position.UnderlyingTicker), position.OptionType,
		position.Strike, position.Expiry, position.ContractMultiplier, position.LegGroupID,
		position.Delta, position.Gamma, position.Theta, position.Vega,
	)
	if err := insertRow.Scan(&position.ID, &position.OpenedAt); err != nil {
		return fmt.Errorf("postgres: create alpaca-owned position insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO position_provenance (position_id, broker) VALUES ($1, 'alpaca')`, position.ID); err != nil {
		return fmt.Errorf("postgres: create alpaca-owned position provenance: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: create alpaca-owned position commit: %w", err)
	}
	return nil
}

// Get retrieves a position by ID. It returns ErrNotFound when no row matches.
func (r *PositionRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Position, error) {
	row := r.pool.QueryRow(ctx, positionSelectSQL+` WHERE p.id = $1`, id)

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
		     , asset_class = $12, underlying_ticker = $13, option_type = $14
		     , strike = $15, expiry = $16, contract_multiplier = $17, leg_group_id = $18
		     , delta = $19, gamma = $20, theta = $21, vega = $22
		 WHERE id = $23
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
		position.AssetClass, nullString(position.UnderlyingTicker), position.OptionType,
		position.Strike, position.Expiry, position.ContractMultiplier, position.LegGroupID,
		position.Delta, position.Gamma, position.Theta, position.Vega,
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

// ListOpenAlpacaOwned returns open positions that can be proven Alpaca-owned via
// linked trades whose orders were recorded with broker='alpaca'.
func (r *PositionRepo) ListOpenAlpacaOwned(ctx context.Context, limit, offset int) ([]domain.Position, error) {
	rows, err := r.pool.Query(ctx, positionSelectSQL+` WHERE p.closed_at IS NULL AND (
		EXISTS (SELECT 1 FROM position_provenance pp WHERE pp.position_id = p.id AND pp.broker = 'alpaca') OR
		EXISTS (SELECT 1 FROM trades t JOIN orders o ON o.id = t.order_id WHERE t.position_id = p.id AND o.broker = 'alpaca')
	) ORDER BY p.opened_at ASC, p.id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list open alpaca-owned positions: %w", err)
	}
	defer rows.Close()
	var positions []domain.Position
	for rows.Next() {
		position, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list open alpaca-owned positions scan: %w", err)
		}
		positions = append(positions, *position)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list open alpaca-owned positions rows: %w", err)
	}
	return positions, nil
}

func alpacaOwnedLockKey(ticker string, side domain.PositionSide) int64 {
	h := sha256.Sum256([]byte("alpaca-owned|" + strings.ToUpper(strings.TrimSpace(ticker)) + "|" + string(side)))
	return int64(binary.BigEndian.Uint64(h[:8]) &^ (1 << 63))
}

// GetByStrategy returns positions for the given strategy with optional
// filtering and pagination.
func (r *PositionRepo) GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	query, args := buildPositionScopedQuery("p.strategy_id", strategyID, filter, limit, offset)
	return r.list(ctx, query, args, "get positions by strategy")
}

const positionSelectSQL = `SELECT p.id, p.strategy_id, s.market_type, p.ticker, p.side,
		p.quantity::double precision, p.avg_entry::double precision,
		p.current_price::double precision, p.unrealized_pnl::double precision,
		p.realized_pnl::double precision, p.stop_loss::double precision,
		p.take_profit::double precision, p.opened_at, p.closed_at,
		p.asset_class, p.underlying_ticker, p.option_type, p.strike::double precision,
		p.expiry, p.contract_multiplier::double precision, p.leg_group_id,
		p.delta::double precision, p.gamma::double precision, p.theta::double precision,
		p.vega::double precision
	 FROM positions p
	 LEFT JOIN strategies s ON s.id = p.strategy_id`

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
		marketType    *string
		currentPrice  *float64
		unrealizedPnL *float64
		stopLoss      *float64
		takeProfit    *float64
		closedAt      *time.Time
		underlying    *string
	)

	err := sc.Scan(
		&position.ID,
		&strategyID,
		&marketType,
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
		&position.AssetClass, &underlying, &position.OptionType,
		&position.Strike, &position.Expiry, &position.ContractMultiplier,
		&position.LegGroupID, &position.Delta, &position.Gamma, &position.Theta,
		&position.Vega,
	)
	if err != nil {
		return nil, err
	}

	position.StrategyID = strategyID
	if marketType != nil {
		position.MarketType = domain.MarketType(strings.TrimSpace(*marketType)).Normalize()
	}
	position.CurrentPrice = currentPrice
	position.UnrealizedPnL = unrealizedPnL
	position.StopLoss = stopLoss
	position.TakeProfit = takeProfit
	position.ClosedAt = closedAt
	if underlying != nil {
		position.UnderlyingTicker = *underlying
	}

	return &position, nil
}

// buildPositionListQuery constructs the SELECT query and arguments for List
// with dynamic WHERE conditions.
// Count returns the total number of positions matching the filter (ignoring pagination).
func (r *PositionRepo) Count(ctx context.Context, filter repository.PositionFilter) (int, error) {
	query, args := buildPositionCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count positions: %w", err)
	}
	return total, nil
}

func buildPositionCountQuery(filter repository.PositionFilter) (string, []any) {
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
	if filter.Side != "" {
		conditions = append(conditions, "side = "+nextArg(filter.Side))
	}
	if filter.OpenedAfter != nil {
		conditions = append(conditions, "opened_at >= "+nextArg(*filter.OpenedAfter))
	}
	if filter.OpenedBefore != nil {
		conditions = append(conditions, "opened_at <= "+nextArg(*filter.OpenedBefore))
	}
	query := `SELECT COUNT(*) FROM positions`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query, args
}

// CountOpen returns the number of open (closed_at IS NULL) positions matching the filter.
func (r *PositionRepo) CountOpen(ctx context.Context, filter repository.PositionFilter) (int, error) {
	query, args := buildPositionOpenCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count open positions: %w", err)
	}
	return total, nil
}

func (r *PositionRepo) CountOpenByMarket(ctx context.Context, filter repository.PositionFilter) (map[domain.MarketType]int, error) {
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
	conditions = append(conditions, "p.closed_at IS NULL")
	if filter.Ticker != "" {
		conditions = append(conditions, "p.ticker = "+nextArg(filter.Ticker))
	}
	if filter.Side != "" {
		conditions = append(conditions, "p.side = "+nextArg(filter.Side))
	}
	if filter.OpenedAfter != nil {
		conditions = append(conditions, "p.opened_at >= "+nextArg(*filter.OpenedAfter))
	}
	if filter.OpenedBefore != nil {
		conditions = append(conditions, "p.opened_at <= "+nextArg(*filter.OpenedBefore))
	}
	query := `SELECT COALESCE(s.market_type, '') AS market_type, COUNT(*)
		FROM positions p LEFT JOIN strategies s ON s.id = p.strategy_id
		WHERE ` + strings.Join(conditions, " AND ") + ` GROUP BY COALESCE(s.market_type, '') ORDER BY COALESCE(s.market_type, '')`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: count open positions by market: %w", err)
	}
	defer rows.Close()
	out := map[domain.MarketType]int{}
	for rows.Next() {
		var key string
		var total int
		if err := rows.Scan(&key, &total); err != nil {
			return nil, fmt.Errorf("postgres: count open positions by market scan: %w", err)
		}
		out[domain.MarketType(key)] = total
	}
	return out, rows.Err()
}

func (r *PositionRepo) GrossExposureOpen(ctx context.Context, filter repository.PositionFilter) (float64, error) {
	var total float64
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
	conditions = append(conditions, "p.closed_at IS NULL")
	if filter.Ticker != "" {
		conditions = append(conditions, "p.ticker = "+nextArg(filter.Ticker))
	}
	if filter.Side != "" {
		conditions = append(conditions, "p.side = "+nextArg(filter.Side))
	}
	if filter.OpenedAfter != nil {
		conditions = append(conditions, "p.opened_at >= "+nextArg(*filter.OpenedAfter))
	}
	if filter.OpenedBefore != nil {
		conditions = append(conditions, "p.opened_at <= "+nextArg(*filter.OpenedBefore))
	}
	query := `SELECT COALESCE(SUM(COALESCE(p.current_price, p.avg_entry) * p.quantity),0)
		FROM positions p WHERE ` + strings.Join(conditions, " AND ")
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: gross exposure open: %w", err)
	}
	return total, nil
}

func buildPositionOpenCountQuery(filter repository.PositionFilter) (string, []any) {
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
	conditions = append(conditions, "closed_at IS NULL")
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
	query := `SELECT COUNT(*) FROM positions WHERE ` + strings.Join(conditions, " AND ")
	return query, args
}

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
		conditions = append(conditions, "p.closed_at IS NULL")
	}

	if filter.Ticker != "" {
		conditions = append(conditions, "p.ticker = "+nextArg(filter.Ticker))
	}

	if filter.Side != "" {
		conditions = append(conditions, "p.side = "+nextArg(filter.Side))
	}

	if filter.OpenedAfter != nil {
		conditions = append(conditions, "p.opened_at >= "+nextArg(*filter.OpenedAfter))
	}

	if filter.OpenedBefore != nil {
		conditions = append(conditions, "p.opened_at <= "+nextArg(*filter.OpenedBefore))
	}

	query := positionSelectSQL
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY p.opened_at DESC, p.id DESC"
	query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return query, args
}
