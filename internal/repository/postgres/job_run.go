package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobRun represents a single execution of an automation job.
type JobRun struct {
	ID                  uuid.UUID      `json:"id"`
	JobName             string         `json:"job_name"`
	Status              string         `json:"status"`
	StartedAt           time.Time      `json:"started_at"`
	CompletedAt         *time.Time     `json:"completed_at,omitempty"`
	DurationNs          int64          `json:"duration_ns,omitempty"`
	Result              map[string]int `json:"result,omitempty"`
	Error               string         `json:"error,omitempty"`
	LastErrorAt         *time.Time     `json:"last_error_at,omitempty"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	CreatedAt           time.Time      `json:"created_at"`
}

// JobRunSummary holds aggregate stats for a single job name.
type JobRunSummary struct {
	JobName             string     `json:"job_name"`
	LastRun             *time.Time `json:"last_run,omitempty"`
	LastResult          string     `json:"last_result"`
	LastError           string     `json:"last_error,omitempty"`
	LastErrorAt         *time.Time `json:"last_error_at,omitempty"`
	RunCount            int        `json:"run_count"`
	ErrorCount          int        `json:"error_count"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

// JobRunRepo persists automation job runs to PostgreSQL.
type JobRunRepo struct {
	pool *pgxpool.Pool
}

// NewJobRunRepo returns a new JobRunRepo.
func NewJobRunRepo(pool *pgxpool.Pool) *JobRunRepo {
	return &JobRunRepo{pool: pool}
}

// Create inserts a new job run record.
func (r *JobRunRepo) Create(ctx context.Context, run *JobRun) error {
	run.ID = uuid.New()
	var resultJSON []byte
	if run.Result != nil {
		var err error
		resultJSON, err = json.Marshal(run.Result)
		if err != nil {
			return fmt.Errorf("postgres: marshal job run result: %w", err)
		}
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO automation_job_runs (id, job_name, status, started_at, completed_at, duration_ns, result, error, last_error_at, consecutive_failures)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING created_at`,
		run.ID, run.JobName, run.Status, run.StartedAt, run.CompletedAt, run.DurationNs, resultJSON, nullString(run.Error), run.LastErrorAt, run.ConsecutiveFailures,
	)
	return row.Scan(&run.CreatedAt)
}

// Complete updates an admitted running row with its terminal outcome.
func (r *JobRunRepo) Complete(ctx context.Context, run *JobRun) error {
	var resultJSON []byte
	if run.Result != nil {
		var err error
		resultJSON, err = json.Marshal(run.Result)
		if err != nil {
			return fmt.Errorf("postgres: marshal completed job run result: %w", err)
		}
	}
	commandTag, err := r.pool.Exec(ctx,
		`UPDATE automation_job_runs
		 SET status = $2, completed_at = $3, duration_ns = $4, result = $5,
		     error = $6, last_error_at = $7, consecutive_failures = $8
		 WHERE id = $1 AND completed_at IS NULL`,
		run.ID, run.Status, run.CompletedAt, run.DurationNs, resultJSON,
		nullString(run.Error), run.LastErrorAt, run.ConsecutiveFailures,
	)
	if err != nil {
		return fmt.Errorf("postgres: complete job run: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("postgres: complete job run %s: expected one running row, updated %d", run.ID, commandTag.RowsAffected())
	}
	return nil
}

// FailIncomplete marks rows left running by a prior app process as terminal
// errors before scheduler state is hydrated.
func (r *JobRunRepo) FailIncomplete(ctx context.Context, completedAt time.Time, reason string) (int, error) {
	commandTag, err := r.pool.Exec(ctx,
		`UPDATE automation_job_runs
		 SET status = 'error',
		     completed_at = $1,
		     duration_ns = GREATEST(0, (EXTRACT(EPOCH FROM ($1 - started_at)) * 1000000000)::bigint),
		     error = $2,
		     last_error_at = $1,
		     consecutive_failures = consecutive_failures + 1
		 WHERE completed_at IS NULL`,
		completedAt, reason,
	)
	if err != nil {
		return 0, fmt.Errorf("postgres: fail incomplete job runs: %w", err)
	}
	return int(commandTag.RowsAffected()), nil
}

// ListByJob returns recent runs for a specific job, newest first.
func (r *JobRunRepo) ListByJob(ctx context.Context, jobName string, limit int) ([]JobRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, job_name, status, started_at, completed_at, duration_ns, result, error, last_error_at, consecutive_failures, created_at
		 FROM automation_job_runs
		 WHERE job_name = $1
		 ORDER BY started_at DESC
		 LIMIT $2`,
		jobName, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list job runs: %w", err)
	}
	defer rows.Close()
	return scanJobRuns(rows)
}

// List returns recent automation job runs, newest first.
func (r *JobRunRepo) List(ctx context.Context, limit, offset int) ([]JobRun, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, job_name, status, started_at, completed_at, duration_ns, result, error, last_error_at, consecutive_failures, created_at
		 FROM automation_job_runs
		 ORDER BY started_at DESC
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list automation job runs: %w", err)
	}
	defer rows.Close()
	return scanJobRuns(rows)
}

// Count returns the total number of automation job run records.
func (r *JobRunRepo) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM automation_job_runs`).Scan(&count); err != nil {
		return 0, fmt.Errorf("postgres: count automation job runs: %w", err)
	}
	return count, nil
}

type jobRunRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanJobRuns(rows jobRunRows) ([]JobRun, error) {
	var runs []JobRun
	for rows.Next() {
		var (
			run       JobRun
			resultRaw []byte
			errStr    *string
			completed *time.Time
			lastErrAt *time.Time
		)
		if err := rows.Scan(&run.ID, &run.JobName, &run.Status, &run.StartedAt, &completed, &run.DurationNs, &resultRaw, &errStr, &lastErrAt, &run.ConsecutiveFailures, &run.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan job run: %w", err)
		}
		if completed != nil {
			run.CompletedAt = completed
		}
		if len(resultRaw) > 0 {
			if err := json.Unmarshal(resultRaw, &run.Result); err != nil {
				return nil, fmt.Errorf("postgres: unmarshal job run result: %w", err)
			}
		}
		if errStr != nil {
			run.Error = *errStr
		}
		if lastErrAt != nil {
			run.LastErrorAt = lastErrAt
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// Summaries returns aggregate stats per job name, used to hydrate the orchestrator on startup.
func (r *JobRunRepo) Summaries(ctx context.Context) ([]JobRunSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT
			job_name,
			MAX(started_at) AS last_run,
			COUNT(*) AS run_count,
			COUNT(*) FILTER (WHERE status = 'error') AS error_count
		 FROM automation_job_runs
		 GROUP BY job_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: job run summaries: %w", err)
	}
	defer rows.Close()

	var summaries []JobRunSummary
	for rows.Next() {
		var s JobRunSummary
		if err := rows.Scan(&s.JobName, &s.LastRun, &s.RunCount, &s.ErrorCount); err != nil {
			return nil, fmt.Errorf("postgres: scan job run summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill in last_result, last_error, last_error_at, and consecutive_failures
	// from persisted columns on the most recent run per job.
	for i, s := range summaries {
		var status string
		var errStr *string
		var lastErrAt *time.Time
		var consecutiveFailures int
		err := r.pool.QueryRow(ctx,
			`SELECT status, error, last_error_at, consecutive_failures
			 FROM automation_job_runs
			 WHERE job_name = $1 ORDER BY started_at DESC LIMIT 1`,
			s.JobName,
		).Scan(&status, &errStr, &lastErrAt, &consecutiveFailures)
		if err == nil {
			summaries[i].LastResult = status
			if errStr != nil {
				summaries[i].LastError = *errStr
			}
			summaries[i].LastErrorAt = lastErrAt
			summaries[i].ConsecutiveFailures = consecutiveFailures
		}
	}

	return summaries, nil
}
