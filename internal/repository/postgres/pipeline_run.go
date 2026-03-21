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

// PipelineRunRepo implements repository.PipelineRunRepository using PostgreSQL.
type PipelineRunRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that PipelineRunRepo satisfies PipelineRunRepository.
var _ repository.PipelineRunRepository = (*PipelineRunRepo)(nil)

// NewPipelineRunRepo returns a PipelineRunRepo backed by the given connection
// pool.
func NewPipelineRunRepo(pool *pgxpool.Pool) *PipelineRunRepo {
	return &PipelineRunRepo{pool: pool}
}

// Create inserts a new pipeline run and populates the generated ID on the
// provided struct.
func (r *PipelineRunRepo) Create(ctx context.Context, run *domain.PipelineRun) error {
	configSnapshot, err := marshalConfigSnapshot(run.ConfigSnapshot)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO pipeline_runs (
			strategy_id, ticker, trade_date, status, signal, started_at, completed_at, error_message, config_snapshot
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		run.StrategyID,
		run.Ticker,
		run.TradeDate,
		run.Status,
		run.Signal,
		run.StartedAt,
		run.CompletedAt,
		run.ErrorMessage,
		configSnapshot,
	)

	if err := row.Scan(&run.ID); err != nil {
		return fmt.Errorf("postgres: create pipeline run: %w", err)
	}

	return nil
}

// Get retrieves a pipeline run by its composite key. It returns ErrNotFound
// when no row matches.
func (r *PipelineRunRepo) Get(ctx context.Context, id uuid.UUID, tradeDate time.Time) (*domain.PipelineRun, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, strategy_id, ticker, trade_date, status, signal, started_at, completed_at, error_message, config_snapshot
		 FROM pipeline_runs
		 WHERE id = $1 AND trade_date = $2::date`,
		id,
		tradeDate,
	)

	run, err := scanPipelineRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get pipeline run %s on %s: %w", id, tradeDate.Format("2006-01-02"), ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get pipeline run: %w", err)
	}

	return run, nil
}

// List returns pipeline runs matching the provided filter with pagination.
func (r *PipelineRunRepo) List(ctx context.Context, filter repository.PipelineRunFilter, limit, offset int) ([]domain.PipelineRun, error) {
	query, args := buildPipelineRunListQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list pipeline runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.PipelineRun
	for rows.Next() {
		run, err := scanPipelineRun(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list pipeline runs scan: %w", err)
		}
		runs = append(runs, *run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list pipeline runs rows: %w", err)
	}

	return runs, nil
}

// UpdateStatus updates the status fields for a pipeline run. It returns
// ErrNotFound when no row matches the provided composite key.
func (r *PipelineRunRepo) UpdateStatus(ctx context.Context, id uuid.UUID, tradeDate time.Time, update repository.PipelineRunStatusUpdate) error {
	row := r.pool.QueryRow(ctx,
		`UPDATE pipeline_runs
		 SET status = $1, completed_at = $2, error_message = $3
		 WHERE id = $4 AND trade_date = $5::date
		 RETURNING id`,
		update.Status,
		update.CompletedAt,
		update.ErrorMessage,
		id,
		tradeDate,
	)

	var updatedID uuid.UUID
	if err := row.Scan(&updatedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update pipeline run %s on %s: %w", id, tradeDate.Format("2006-01-02"), ErrNotFound)
		}
		return fmt.Errorf("postgres: update pipeline run: %w", err)
	}

	return nil
}

// scanPipelineRun scans a single row (pgx.Row or pgx.Rows) into a PipelineRun.
func scanPipelineRun(sc scanner) (*domain.PipelineRun, error) {
	var (
		run                domain.PipelineRun
		signal             string
		configSnapshotJSON []byte
	)

	err := sc.Scan(
		&run.ID,
		&run.StrategyID,
		&run.Ticker,
		&run.TradeDate,
		&run.Status,
		&signal,
		&run.StartedAt,
		&run.CompletedAt,
		&run.ErrorMessage,
		&configSnapshotJSON,
	)
	if err != nil {
		return nil, err
	}

	run.Signal = domain.PipelineSignal(signal)
	if configSnapshotJSON != nil {
		run.ConfigSnapshot = json.RawMessage(configSnapshotJSON)
	}

	return &run, nil
}

// buildPipelineRunListQuery constructs the SELECT query and arguments for List
// with dynamic WHERE conditions. All values are parameterized.
func buildPipelineRunListQuery(filter repository.PipelineRunFilter, limit, offset int) (string, []any) {
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

	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}

	if filter.Status != "" {
		conditions = append(conditions, "status = "+nextArg(filter.Status))
	}

	if filter.TradeDate != nil {
		conditions = append(conditions, "trade_date = "+nextArg(*filter.TradeDate)+"::date")
	}

	if filter.StartedAfter != nil {
		conditions = append(conditions, "started_at >= "+nextArg(*filter.StartedAfter))
	}

	if filter.StartedBefore != nil {
		conditions = append(conditions, "started_at <= "+nextArg(*filter.StartedBefore))
	}

	base := `SELECT id, strategy_id, ticker, trade_date, status, signal, started_at, completed_at, error_message, config_snapshot
		 FROM pipeline_runs`

	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	base += " ORDER BY started_at DESC, id DESC"
	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}

// marshalConfigSnapshot ensures the config_snapshot JSONB value is valid JSON.
// A nil or empty value is stored as SQL NULL.
func marshalConfigSnapshot(cfg json.RawMessage) ([]byte, error) {
	if len(cfg) == 0 {
		return nil, nil
	}

	if !json.Valid(cfg) {
		return nil, fmt.Errorf("postgres: pipeline run config snapshot is not valid JSON")
	}

	return cfg, nil
}
