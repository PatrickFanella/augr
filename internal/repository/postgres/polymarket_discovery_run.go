package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type PolymarketDiscoveryRunRepo struct{ pool *pgxpool.Pool }

var _ repository.PolymarketDiscoveryRunRepository = (*PolymarketDiscoveryRunRepo)(nil)

func NewPolymarketDiscoveryRunRepo(pool *pgxpool.Pool) *PolymarketDiscoveryRunRepo {
	return &PolymarketDiscoveryRunRepo{pool: pool}
}

func (r *PolymarketDiscoveryRunRepo) Create(ctx context.Context, run *domain.PolymarketDiscoveryRun) error {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	candidates, accepted, deployed, errs, summary, err := marshalPolymarketDiscoveryRunJSON(*run)
	if err != nil {
		return err
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO polymarket_discovery_runs
		(id, status, phase, candidate_index, candidates, accepted, deployed, errors, summary, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING started_at, updated_at`,
		run.ID, run.Status, run.Phase, run.CandidateIndex, candidates, accepted, deployed, errs, summary, run.CompletedAt,
	)
	if err := row.Scan(&run.StartedAt, &run.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create polymarket discovery run: %w", err)
	}
	return nil
}

func (r *PolymarketDiscoveryRunRepo) Get(ctx context.Context, id uuid.UUID) (*domain.PolymarketDiscoveryRun, error) {
	run, err := scanPolymarketDiscoveryRun(r.pool.QueryRow(ctx, polymarketDiscoverySelectSQL+` WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get polymarket discovery run %s: %w", id, repository.ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get polymarket discovery run: %w", err)
	}
	return run, nil
}

func (r *PolymarketDiscoveryRunRepo) GetActive(ctx context.Context) (*domain.PolymarketDiscoveryRun, error) {
	run, err := scanPolymarketDiscoveryRun(r.pool.QueryRow(ctx, polymarketDiscoverySelectSQL+` WHERE status = 'running' ORDER BY updated_at DESC LIMIT 1`))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get active polymarket discovery run: %w", err)
	}
	return run, nil
}

func (r *PolymarketDiscoveryRunRepo) Update(ctx context.Context, run *domain.PolymarketDiscoveryRun) error {
	candidates, accepted, deployed, errs, summary, err := marshalPolymarketDiscoveryRunJSON(*run)
	if err != nil {
		return err
	}
	row := r.pool.QueryRow(ctx, `UPDATE polymarket_discovery_runs SET
		status = $2, phase = $3, candidate_index = $4, candidates = $5, accepted = $6, deployed = $7, errors = $8, summary = $9, completed_at = $10, updated_at = NOW()
		WHERE id = $1 RETURNING updated_at`,
		run.ID, run.Status, run.Phase, run.CandidateIndex, candidates, accepted, deployed, errs, summary, run.CompletedAt,
	)
	if err := row.Scan(&run.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update polymarket discovery run %s: %w", run.ID, repository.ErrNotFound)
		}
		return fmt.Errorf("postgres: update polymarket discovery run: %w", err)
	}
	return nil
}

func (r *PolymarketDiscoveryRunRepo) ListLatest(ctx context.Context, limit int) ([]domain.PolymarketDiscoveryRun, error) {
	query, args := buildPolymarketDiscoveryListLatestQuery(limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list polymarket discovery runs: %w", err)
	}
	defer rows.Close()
	var runs []domain.PolymarketDiscoveryRun
	for rows.Next() {
		run, err := scanPolymarketDiscoveryRun(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan polymarket discovery run: %w", err)
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list polymarket discovery runs rows: %w", err)
	}
	return runs, nil
}

const polymarketDiscoverySelectSQL = `SELECT id, status, phase, candidate_index, candidates, accepted, deployed, errors, summary, started_at, updated_at, completed_at FROM polymarket_discovery_runs`

func buildPolymarketDiscoveryListLatestQuery(limit int) (string, []any) {
	if limit <= 0 {
		limit = 20
	}
	return polymarketDiscoverySelectSQL + ` ORDER BY started_at DESC, id DESC LIMIT $1`, []any{limit}
}

func marshalPolymarketDiscoveryRunJSON(run domain.PolymarketDiscoveryRun) ([]byte, []byte, []byte, []byte, []byte, error) {
	candidates, err := json.Marshal(run.Candidates)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("postgres: marshal polymarket candidates: %w", err)
	}
	accepted, err := json.Marshal(run.Accepted)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("postgres: marshal polymarket accepted: %w", err)
	}
	deployed, err := json.Marshal(run.Deployed)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("postgres: marshal polymarket deployed: %w", err)
	}
	errs, err := json.Marshal(run.Errors)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("postgres: marshal polymarket errors: %w", err)
	}
	summary, err := json.Marshal(run.Summary)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("postgres: marshal polymarket summary: %w", err)
	}
	return candidates, accepted, deployed, errs, summary, nil
}

func scanPolymarketDiscoveryRun(sc scanner) (*domain.PolymarketDiscoveryRun, error) {
	var run domain.PolymarketDiscoveryRun
	var candidates, accepted, deployed, errs, summary []byte
	var completedAt *time.Time
	if err := sc.Scan(&run.ID, &run.Status, &run.Phase, &run.CandidateIndex, &candidates, &accepted, &deployed, &errs, &summary, &run.StartedAt, &run.UpdatedAt, &completedAt); err != nil {
		return nil, err
	}
	run.CompletedAt = completedAt
	if err := json.Unmarshal(candidates, &run.Candidates); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal polymarket candidates: %w", err)
	}
	if err := json.Unmarshal(accepted, &run.Accepted); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal polymarket accepted: %w", err)
	}
	if err := json.Unmarshal(deployed, &run.Deployed); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal polymarket deployed: %w", err)
	}
	if err := json.Unmarshal(errs, &run.Errors); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal polymarket errors: %w", err)
	}
	if err := json.Unmarshal(summary, &run.Summary); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal polymarket summary: %w", err)
	}
	return &run, nil
}
