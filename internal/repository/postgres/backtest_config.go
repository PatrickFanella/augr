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

// BacktestConfigRepo implements repository.BacktestConfigRepository using PostgreSQL.
type BacktestConfigRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that BacktestConfigRepo satisfies BacktestConfigRepository.
var _ repository.BacktestConfigRepository = (*BacktestConfigRepo)(nil)

// NewBacktestConfigRepo returns a BacktestConfigRepo backed by the given connection pool.
func NewBacktestConfigRepo(pool *pgxpool.Pool) *BacktestConfigRepo {
	return &BacktestConfigRepo{pool: pool}
}

// Create inserts a new backtest configuration and populates the generated ID and timestamps.
func (r *BacktestConfigRepo) Create(ctx context.Context, config *domain.BacktestConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("postgres: validate backtest config: %w", err)
	}

	simulationJSON, err := marshalBacktestSimulation(config.Simulation)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO backtest_configs (
			strategy_id, name, description, start_date, end_date, simulation_params
		)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at, updated_at`,
		config.StrategyID,
		config.Name,
		config.Description,
		config.StartDate,
		config.EndDate,
		simulationJSON,
	)

	if err := row.Scan(&config.ID, &config.CreatedAt, &config.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create backtest config: %w", err)
	}

	return nil
}

// Get retrieves a backtest configuration by ID. It returns ErrNotFound when no row matches.
func (r *BacktestConfigRepo) Get(ctx context.Context, id uuid.UUID) (*domain.BacktestConfig, error) {
	row := r.pool.QueryRow(ctx, backtestConfigSelectSQL+` WHERE id = $1`, id)

	config, err := scanBacktestConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get backtest config %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get backtest config: %w", err)
	}

	return config, nil
}

// List returns backtest configurations matching the provided filter with pagination.
func (r *BacktestConfigRepo) List(ctx context.Context, filter repository.BacktestConfigFilter, limit, offset int) ([]domain.BacktestConfig, error) {
	query, args := buildBacktestConfigListQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list backtest configs: %w", err)
	}
	defer rows.Close()

	var configs []domain.BacktestConfig
	for rows.Next() {
		config, err := scanBacktestConfig(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list backtest configs scan: %w", err)
		}
		configs = append(configs, *config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list backtest configs rows: %w", err)
	}

	return configs, nil
}

// Update persists changes to an existing backtest configuration.
func (r *BacktestConfigRepo) Update(ctx context.Context, config *domain.BacktestConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("postgres: validate backtest config: %w", err)
	}

	simulationJSON, err := marshalBacktestSimulation(config.Simulation)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`UPDATE backtest_configs
		 SET strategy_id = $1,
		     name = $2,
		     description = $3,
		     start_date = $4,
		     end_date = $5,
		     simulation_params = $6,
		     updated_at = NOW()
		 WHERE id = $7
		 RETURNING updated_at`,
		config.StrategyID,
		config.Name,
		config.Description,
		config.StartDate,
		config.EndDate,
		simulationJSON,
		config.ID,
	)

	if err := row.Scan(&config.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update backtest config %s: %w", config.ID, ErrNotFound)
		}
		return fmt.Errorf("postgres: update backtest config: %w", err)
	}

	return nil
}

// Delete removes a backtest configuration by ID.
func (r *BacktestConfigRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM backtest_configs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete backtest config: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete backtest config %s: %w", id, ErrNotFound)
	}

	return nil
}

const backtestConfigSelectSQL = `SELECT id, strategy_id, name, COALESCE(description, ''), start_date, end_date, simulation_params, created_at, updated_at
	 FROM backtest_configs`

// scanBacktestConfig scans a single row (pgx.Row or pgx.Rows) into a BacktestConfig.
func scanBacktestConfig(sc scanner) (*domain.BacktestConfig, error) {
	var (
		config         domain.BacktestConfig
		simulationJSON []byte
	)

	err := sc.Scan(
		&config.ID,
		&config.StrategyID,
		&config.Name,
		&config.Description,
		&config.StartDate,
		&config.EndDate,
		&simulationJSON,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(simulationJSON) != 0 {
		if err := json.Unmarshal(simulationJSON, &config.Simulation); err != nil {
			return nil, fmt.Errorf("postgres: unmarshal backtest simulation params: %w", err)
		}
	}

	return &config, nil
}

// buildBacktestConfigListQuery constructs the SELECT query and arguments for List.
func buildBacktestConfigListQuery(filter repository.BacktestConfigFilter, limit, offset int) (string, []any) {
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

	if filter.StrategyID != nil {
		conditions = append(conditions, "strategy_id = "+nextArg(*filter.StrategyID))
	}
	if filter.CreatedAfter != nil {
		conditions = append(conditions, "created_at >= "+nextArg(*filter.CreatedAfter))
	}
	if filter.CreatedBefore != nil {
		conditions = append(conditions, "created_at <= "+nextArg(*filter.CreatedBefore))
	}

	base := backtestConfigSelectSQL
	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	base += " ORDER BY created_at DESC, id DESC"
	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}

func marshalBacktestSimulation(sim domain.BacktestSimulationParameters) ([]byte, error) {
	if err := validateOptionalJSON("slippage model", sim.SlippageModel); err != nil {
		return nil, err
	}
	if err := validateOptionalJSON("transaction costs", sim.TransactionCosts); err != nil {
		return nil, err
	}
	if err := validateOptionalJSON("spread model", sim.SpreadModel); err != nil {
		return nil, err
	}

	data, err := json.Marshal(sim)
	if err != nil {
		return nil, fmt.Errorf("postgres: marshal backtest simulation params: %w", err)
	}

	return data, nil
}

func validateOptionalJSON(field string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return fmt.Errorf("postgres: backtest %s is not valid JSON", field)
	}
	return nil
}
