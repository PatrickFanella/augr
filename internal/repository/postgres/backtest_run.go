package postgres

import (
	"context"
	"encoding/json"
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

// BacktestRunRepo implements repository.BacktestRunRepository using PostgreSQL.
type BacktestRunRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that BacktestRunRepo satisfies BacktestRunRepository.
var _ repository.BacktestRunRepository = (*BacktestRunRepo)(nil)

// NewBacktestRunRepo returns a BacktestRunRepo backed by the given connection pool.
func NewBacktestRunRepo(pool *pgxpool.Pool) *BacktestRunRepo {
	return &BacktestRunRepo{pool: pool}
}

// Create inserts a new persisted backtest run and populates its generated ID and timestamps.
func (r *BacktestRunRepo) Create(ctx context.Context, run *domain.BacktestRun) error {
	if err := run.Validate(); err != nil {
		return fmt.Errorf("postgres: validate backtest run: %w", err)
	}

	metricsJSON := run.Metrics
	tradeLogJSON := run.TradeLog
	equityCurveJSON := run.EquityCurve

	row := r.pool.QueryRow(ctx,
		`INSERT INTO backtest_runs (
			backtest_config_id, metrics, trade_log, equity_curve, run_timestamp, duration_ns, prompt_version, prompt_version_hash
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at, updated_at`,
		run.BacktestConfigID,
		metricsJSON,
		tradeLogJSON,
		equityCurveJSON,
		run.RunTimestamp,
		run.Duration.Nanoseconds(),
		run.PromptVersion,
		run.PromptVersionHash,
	)

	if err := row.Scan(&run.ID, &run.CreatedAt, &run.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create backtest run: %w", err)
	}

	return nil
}

// Get retrieves a persisted backtest run by ID. It returns ErrNotFound when no row matches.
func (r *BacktestRunRepo) Get(ctx context.Context, id uuid.UUID) (*domain.BacktestRun, error) {
	row := r.pool.QueryRow(ctx, backtestRunSelectSQL+` WHERE id = $1`, id)

	run, err := scanBacktestRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get backtest run %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get backtest run: %w", err)
	}

	return run, nil
}

// List returns persisted backtest runs matching the provided filter with pagination.
func (r *BacktestRunRepo) List(ctx context.Context, filter repository.BacktestRunFilter, limit, offset int) ([]domain.BacktestRun, error) {
	query, args := buildBacktestRunListQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list backtest runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.BacktestRun
	for rows.Next() {
		run, err := scanBacktestRun(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list backtest runs scan: %w", err)
		}
		runs = append(runs, *run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list backtest runs rows: %w", err)
	}

	return runs, nil
}

const backtestRunSelectSQL = `SELECT id, backtest_config_id, metrics, trade_log, equity_curve, run_timestamp, duration_ns, prompt_version, prompt_version_hash, created_at, updated_at
	 FROM backtest_runs`

// scanBacktestRun scans a single row (pgx.Row or pgx.Rows) into a BacktestRun.
func scanBacktestRun(sc scanner) (*domain.BacktestRun, error) {
	var (
		run             domain.BacktestRun
		metricsJSON     []byte
		tradeLogJSON    []byte
		equityCurveJSON []byte
		durationNs      int64
	)

	err := sc.Scan(
		&run.ID,
		&run.BacktestConfigID,
		&metricsJSON,
		&tradeLogJSON,
		&equityCurveJSON,
		&run.RunTimestamp,
		&durationNs,
		&run.PromptVersion,
		&run.PromptVersionHash,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	run.Metrics = json.RawMessage(metricsJSON)
	run.TradeLog = json.RawMessage(tradeLogJSON)
	run.EquityCurve = json.RawMessage(equityCurveJSON)
	run.Duration = time.Duration(durationNs)

	return &run, nil
}

// buildBacktestRunListQuery constructs the SELECT query and arguments for List.
func buildBacktestRunListQuery(filter repository.BacktestRunFilter, limit, offset int) (string, []any) {
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

	if filter.BacktestConfigID != nil {
		conditions = append(conditions, "backtest_config_id = "+nextArg(*filter.BacktestConfigID))
	}
	if filter.PromptVersion != "" {
		conditions = append(conditions, "prompt_version = "+nextArg(filter.PromptVersion))
	}
	if filter.PromptVersionHash != "" {
		conditions = append(conditions, "prompt_version_hash = "+nextArg(filter.PromptVersionHash))
	}
	if filter.RunAfter != nil {
		conditions = append(conditions, "run_timestamp >= "+nextArg(*filter.RunAfter))
	}
	if filter.RunBefore != nil {
		conditions = append(conditions, "run_timestamp <= "+nextArg(*filter.RunBefore))
	}

	base := backtestRunSelectSQL
	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	base += " ORDER BY run_timestamp DESC, id DESC"
	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}
