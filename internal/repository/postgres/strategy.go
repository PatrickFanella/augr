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
		`INSERT INTO strategies (name, description, ticker, market_type, schedule_cron, config, status, skip_next_run, is_paper, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
		s.Status == domain.StrategyStatusActive,
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
		s, err := scanStrategyWithLatestRun(rows)
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

// Count returns the total number of strategies that match the filter,
// ignoring any pagination (limit/offset).
func (r *StrategyRepo) Count(ctx context.Context, filter repository.StrategyFilter) (int, error) {
	query, args := buildStrategyCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count strategies: %w", err)
	}
	return total, nil
}

func (r *StrategyRepo) CountByMarket(ctx context.Context, filter repository.StrategyFilter) (map[domain.MarketType]int, error) {
	var (
		conditions []string
		args       []any
		argIdx     int
	)
	nextArg := func(v any) string { argIdx++; args = append(args, v); return fmt.Sprintf("$%d", argIdx) }
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
	query := `SELECT market_type, COUNT(*) FROM strategies`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " GROUP BY market_type ORDER BY market_type"
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: count strategies by market: %w", err)
	}
	defer rows.Close()
	out := map[domain.MarketType]int{}
	for rows.Next() {
		var key string
		var total int
		if err := rows.Scan(&key, &total); err != nil {
			return nil, fmt.Errorf("postgres: count strategies by market scan: %w", err)
		}
		out[domain.MarketType(key)] = total
	}
	return out, rows.Err()
}

// buildStrategyCountQuery constructs a SELECT COUNT(*) query for strategies
// with the same filter conditions used by buildListQuery.
func buildStrategyCountQuery(filter repository.StrategyFilter) (string, []any) {
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

	base := `SELECT COUNT(*) FROM strategies`
	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}
	return base, args
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

// TransitionPaperStatus atomically changes a paper strategy from one status to
// another. It returns ErrNotFound when no row satisfies the ID, paper-mode, and
// status preconditions.
func (r *StrategyRepo) TransitionPaperStatus(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (*domain.Strategy, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE strategies
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND is_paper = TRUE AND status = $3
		 RETURNING id, name, description, ticker, market_type, schedule_cron, config, status, skip_next_run, is_paper, created_at, updated_at`,
		toStatus,
		id,
		fromStatus,
	)

	s, err := scanStrategy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: transition paper strategy %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: transition paper strategy: %w", err)
	}
	return s, nil
}

// MarkPaperSkipNext atomically marks an active paper strategy to skip its next
// scheduled run. It returns ErrNotFound when the ID, paper-mode, and active
// status preconditions are not all satisfied.
func (r *StrategyRepo) MarkPaperSkipNext(ctx context.Context, id uuid.UUID) (*domain.Strategy, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE strategies
		 SET skip_next_run = TRUE, updated_at = NOW()
		 WHERE id = $1 AND is_paper = TRUE AND status = $2
		 RETURNING id, name, description, ticker, market_type, schedule_cron, config, status, skip_next_run, is_paper, created_at, updated_at`,
		id,
		domain.StrategyStatusActive,
	)

	s, err := scanStrategy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: mark paper skip-next %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: mark paper skip-next: %w", err)
	}
	return s, nil
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

// UpdateThesis persists the serialised active thesis JSON for the given strategy.
// Passing nil clears the stored thesis.
func (r *StrategyRepo) UpdateThesis(ctx context.Context, strategyID uuid.UUID, thesis json.RawMessage) error {
	var thesisArg interface{}
	if len(thesis) > 0 {
		thesisArg = []byte(thesis)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE strategies SET active_thesis = $1, updated_at = NOW() WHERE id = $2`,
		thesisArg,
		strategyID,
	)
	if err != nil {
		return fmt.Errorf("postgres: update thesis %s: %w", strategyID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: update thesis %s: %w", strategyID, ErrNotFound)
	}
	return nil
}

// GetThesisRaw returns the serialised active thesis JSON for the given strategy.
// Returns nil, nil when no thesis is stored.
func (r *StrategyRepo) GetThesisRaw(ctx context.Context, strategyID uuid.UUID) (json.RawMessage, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT active_thesis FROM strategies WHERE id = $1`,
		strategyID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get thesis %s: %w", strategyID, err)
	}
	return json.RawMessage(raw), nil
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

func scanStrategyWithLatestRun(sc scanner) (*domain.Strategy, error) {
	var (
		s             domain.Strategy
		configBytes   []byte
		latestRunJSON []byte
	)

	if err := sc.Scan(
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
		&latestRunJSON,
	); err != nil {
		return nil, err
	}

	s.Config = json.RawMessage(configBytes)
	if len(latestRunJSON) != 0 {
		var summary domain.StrategyLatestRunSummary
		if err := json.Unmarshal(latestRunJSON, &summary); err != nil {
			return nil, fmt.Errorf("postgres: unmarshal strategy latest run summary: %w", err)
		}
		s.LatestRunSummary = &summary
	}

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
		conditions = append(conditions, "s.ticker = "+nextArg(filter.Ticker))
	}

	if filter.MarketType != "" {
		conditions = append(conditions, "s.market_type = "+nextArg(filter.MarketType))
	}

	if filter.Status != "" {
		conditions = append(conditions, "s.status = "+nextArg(filter.Status))
	}

	if filter.IsPaper != nil {
		conditions = append(conditions, "s.is_paper = "+nextArg(*filter.IsPaper))
	}

	base := `SELECT s.id, s.name, s.description, s.ticker, s.market_type, s.schedule_cron, s.config, s.status, s.skip_next_run, s.is_paper, s.created_at, s.updated_at, latest_run_summary.latest_run_summary
		 FROM strategies s
		 LEFT JOIN LATERAL (
             SELECT jsonb_build_object(
                 'id', pr.id,
                 'strategy_id', pr.strategy_id,
                 'ticker', pr.ticker,
                 'status', pr.status,
                 'signal', pr.signal,
                 'started_at', pr.started_at,
                 'completed_at', pr.completed_at
             ) AS latest_run_summary
             FROM pipeline_runs pr
             WHERE pr.strategy_id = s.id
             ORDER BY pr.started_at DESC, pr.id DESC
             LIMIT 1
		 ) AS latest_run_summary ON TRUE`

	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	base += " ORDER BY s.created_at DESC"
	if limit > 0 {
		base += fmt.Sprintf(" LIMIT %s", nextArg(limit))
	}
	if offset > 0 {
		base += fmt.Sprintf(" OFFSET %s", nextArg(offset))
	}

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
