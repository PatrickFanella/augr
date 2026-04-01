package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ErrNotFound is an alias for the repository-level sentinel so that existing
// postgres code and tests can reference it without a package prefix.
var ErrNotFound = repository.ErrNotFound

// StrategyRepo implements repository.StrategyRepository using PostgreSQL.
type StrategyRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that StrategyRepo satisfies StrategyRepository.
var _ repository.StrategyRepository = (*StrategyRepo)(nil)

// NewStrategyRepo returns a StrategyRepo backed by the given connection pool.
func NewStrategyRepo(pool *pgxpool.Pool) *StrategyRepo {
	return &StrategyRepo{pool: pool}
}

// Create inserts a new strategy and populates the generated ID and timestamps
// on the provided struct.
func (r *StrategyRepo) Create(ctx context.Context, s *domain.Strategy) error {
	configBytes, err := marshalConfig(s.Config)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO strategies (name, description, ticker, market_type, schedule_cron, config, status, skip_next_run, is_paper)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at, updated_at`,
		s.Name,
		s.Description,
		s.Ticker,
		s.MarketType,
		s.ScheduleCron,
		configBytes,
		s.Status,
		s.SkipNextRun,
		s.IsPaper,
	)

	if err := row.Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create strategy: %w", err)
	}

	return nil
}

// Get retrieves a strategy by ID. It returns ErrNotFound when no row matches.
func (r *StrategyRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Strategy, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, description, ticker, market_type, schedule_cron, config, status, skip_next_run, is_paper, created_at, updated_at
		 FROM strategies
		 WHERE id = $1`,
		id,
	)

	s, err := scanStrategy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get strategy %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get strategy: %w", err)
	}

	return s, nil
}

// List returns strategies matching the provided filter with pagination.
func (r *StrategyRepo) List(ctx context.Context, filter repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error) {
	query, args := buildListQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list strategies: %w", err)
	}
	defer rows.Close()

	var strategies []domain.Strategy
	for rows.Next() {
		s, err := scanStrategy(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list strategies scan: %w", err)
		}
		strategies = append(strategies, *s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list strategies rows: %w", err)
	}

	return strategies, nil
}

// Update persists changes to an existing strategy. It returns ErrNotFound when
// no row matches the strategy ID.
func (r *StrategyRepo) Update(ctx context.Context, s *domain.Strategy) error {
	configBytes, err := marshalConfig(s.Config)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`UPDATE strategies
		 SET name = $1, description = $2, ticker = $3, market_type = $4,
		     schedule_cron = $5, config = $6, status = $7, skip_next_run = $8, is_paper = $9,
		     updated_at = NOW()
		 WHERE id = $10
		 RETURNING updated_at`,
		s.Name,
		s.Description,
		s.Ticker,
		s.MarketType,
		s.ScheduleCron,
		configBytes,
		s.Status,
		s.SkipNextRun,
		s.IsPaper,
		s.ID,
	)

	if err := row.Scan(&s.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update strategy %s: %w", s.ID, ErrNotFound)
		}
		return fmt.Errorf("postgres: update strategy: %w", err)
	}

	return nil
}

// Delete removes a strategy by ID. It returns ErrNotFound when no row matches.
func (r *StrategyRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM strategies WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("postgres: delete strategy: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete strategy %s: %w", id, ErrNotFound)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanner is satisfied by both pgx.Row and pgx.Rows, allowing a single scan
// helper for all query paths.
type scanner interface {
	Scan(dest ...any) error
}

// scanStrategy scans a single row (pgx.Row or pgx.Rows) into a Strategy.
func scanStrategy(sc scanner) (*domain.Strategy, error) {
	var s domain.Strategy
	var configBytes []byte

	err := sc.Scan(
		&s.ID,
		&s.Name,
		&s.Description,
		&s.Ticker,
		&s.MarketType,
		&s.ScheduleCron,
		&configBytes,
		&s.Status,
		&s.SkipNextRun,
		&s.IsPaper,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	s.Config = json.RawMessage(configBytes)
	return &s, nil
}

// buildListQuery constructs the SELECT query and arguments for List with
// dynamic WHERE conditions. All values are parameterized.
func buildListQuery(filter repository.StrategyFilter, limit, offset int) (string, []any) {
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

	if filter.MarketType != "" {
		conditions = append(conditions, "market_type = "+nextArg(filter.MarketType))
	}

	if filter.Status != "" {
		conditions = append(conditions, "status = "+nextArg(filter.Status))
	}

	if filter.IsPaper != nil {
		conditions = append(conditions, "is_paper = "+nextArg(*filter.IsPaper))
	}

	base := `SELECT id, name, description, ticker, market_type, schedule_cron, config, status, skip_next_run, is_paper, created_at, updated_at
		 FROM strategies`

	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	base += " ORDER BY created_at DESC"
	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}

// marshalConfig ensures the Config JSONB value is valid JSON. A nil or empty
// value defaults to {}.
func marshalConfig(cfg json.RawMessage) ([]byte, error) {
	if len(cfg) == 0 {
		return []byte("{}"), nil
	}

	if !json.Valid(cfg) {
		return nil, fmt.Errorf("postgres: strategy config is not valid JSON")
	}

	return cfg, nil
}
