package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PipelineRunSnapshotRepo implements repository.PipelineRunSnapshotRepository using PostgreSQL.
type PipelineRunSnapshotRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that PipelineRunSnapshotRepo satisfies PipelineRunSnapshotRepository.
var _ repository.PipelineRunSnapshotRepository = (*PipelineRunSnapshotRepo)(nil)

const pipelineRunSnapshotSelectSQL = `SELECT id, pipeline_run_id, data_type, payload, created_at FROM pipeline_run_snapshots`

// NewPipelineRunSnapshotRepo returns a PipelineRunSnapshotRepo backed by the given connection pool.
func NewPipelineRunSnapshotRepo(pool *pgxpool.Pool) *PipelineRunSnapshotRepo {
	return &PipelineRunSnapshotRepo{pool: pool}
}

// Create inserts a new pipeline run snapshot and populates the generated ID and CreatedAt on the provided struct.
func (r *PipelineRunSnapshotRepo) Create(ctx context.Context, snapshot *domain.PipelineRunSnapshot) error {
	payload, err := marshalPipelineRunSnapshotPayload(snapshot.Payload)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO pipeline_run_snapshots (pipeline_run_id, data_type, payload)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		snapshot.PipelineRunID,
		snapshot.DataType,
		payload,
	)

	if err := row.Scan(&snapshot.ID, &snapshot.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create pipeline run snapshot: %w", err)
	}

	return nil
}

// GetByRun returns snapshots for the given pipeline run ordered by creation time, then id.
func (r *PipelineRunSnapshotRepo) GetByRun(ctx context.Context, runID uuid.UUID) ([]domain.PipelineRunSnapshot, error) {
	rows, err := r.pool.Query(ctx,
		pipelineRunSnapshotSelectSQL+` WHERE pipeline_run_id = $1 ORDER BY created_at, id`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get pipeline run snapshots by run: %w", err)
	}
	defer rows.Close()

	var snapshots []domain.PipelineRunSnapshot
	for rows.Next() {
		snapshot, err := scanPipelineRunSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: get pipeline run snapshots by run scan: %w", err)
		}
		snapshots = append(snapshots, *snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get pipeline run snapshots by run rows: %w", err)
	}

	return snapshots, nil
}

func scanPipelineRunSnapshot(sc scanner) (*domain.PipelineRunSnapshot, error) {
	var (
		snapshot    domain.PipelineRunSnapshot
		payloadJSON []byte
	)

	if err := sc.Scan(
		&snapshot.ID,
		&snapshot.PipelineRunID,
		&snapshot.DataType,
		&payloadJSON,
		&snapshot.CreatedAt,
	); err != nil {
		return nil, err
	}

	snapshot.Payload = json.RawMessage(payloadJSON)

	return &snapshot, nil
}

func marshalPipelineRunSnapshotPayload(payload json.RawMessage) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("postgres: pipeline run snapshot payload is required")
	}

	if !json.Valid(payload) {
		return nil, fmt.Errorf("postgres: pipeline run snapshot payload is not valid JSON")
	}

	return payload, nil
}
