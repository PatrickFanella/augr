package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
)

// DiscoveryRunRepo implements discovery.RunRepository using PostgreSQL.
type DiscoveryRunRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that DiscoveryRunRepo satisfies RunRepository.
var _ discovery.RunRepository = (*DiscoveryRunRepo)(nil)

// NewDiscoveryRunRepo returns a DiscoveryRunRepo backed by the given connection pool.
func NewDiscoveryRunRepo(pool *pgxpool.Pool) *DiscoveryRunRepo {
	return &DiscoveryRunRepo{pool: pool}
}

// Create inserts a new discovery run record.
func (r *DiscoveryRunRepo) Create(ctx context.Context, config, result json.RawMessage, startedAt time.Time, duration time.Duration, candidates, deployed int) error {
	completedAt := time.Now().UTC()
	_, err := r.pool.Exec(ctx,
		`INSERT INTO discovery_runs (config, result, started_at, completed_at, duration_ns, candidates, deployed)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		config,
		result,
		startedAt,
		completedAt,
		duration.Nanoseconds(),
		candidates,
		deployed,
	)
	if err != nil {
		return fmt.Errorf("postgres: create discovery run: %w", err)
	}
	return nil
}

// List returns discovery runs ordered by started_at descending.
func (r *DiscoveryRunRepo) List(ctx context.Context, limit, offset int) ([]discovery.DiscoveryRun, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, config, result, started_at, completed_at, duration_ns, candidates, deployed, created_at
		 FROM discovery_runs
		 ORDER BY started_at DESC
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list discovery runs: %w", err)
	}
	defer rows.Close()

	var runs []discovery.DiscoveryRun
	for rows.Next() {
		var run discovery.DiscoveryRun
		var config, result []byte
		if err := rows.Scan(
			&run.ID,
			&config,
			&result,
			&run.StartedAt,
			&run.CompletedAt,
			&run.DurationNS,
			&run.Candidates,
			&run.Deployed,
			&run.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan discovery run: %w", err)
		}
		run.Config = json.RawMessage(config)
		run.Result = json.RawMessage(result)
		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate discovery run rows: %w", err)
	}

	return runs, nil
}

// Count returns the total number of discovery run records.
func (r *DiscoveryRunRepo) Count(ctx context.Context) (int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM discovery_runs`).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count discovery runs: %w", err)
	}
	return total, nil
}
