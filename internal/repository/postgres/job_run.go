package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobRun represents a single execution of an automation job.
type JobRun struct {
	ID          uuid.UUID  `json:"id"`
	JobName     string     `json:"job_name"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationNs  int64      `json:"duration_ns,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// JobRunSummary holds aggregate stats for a single job name.
type JobRunSummary struct {
	JobName    string     `json:"job_name"`
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastResult string     `json:"last_result"`
	LastError  string     `json:"last_error,omitempty"`
	RunCount   int        `json:"run_count"`
	ErrorCount int        `json:"error_count"`
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
	row := r.pool.QueryRow(ctx,
		`INSERT INTO automation_job_runs (id, job_name, status, started_at, completed_at, duration_ns, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at`,
		run.ID, run.JobName, run.Status, run.StartedAt, run.CompletedAt, run.DurationNs, nullString(run.Error),
	)
	return row.Scan(&run.CreatedAt)
}

// ListByJob returns recent runs for a specific job, newest first.
func (r *JobRunRepo) ListByJob(ctx context.Context, jobName string, limit int) ([]JobRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, job_name, status, started_at, completed_at, duration_ns, error, created_at
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

	var runs []JobRun
	for rows.Next() {
		var (
			run       JobRun
			errStr    *string
			completed *time.Time
		)
		if err := rows.Scan(&run.ID, &run.JobName, &run.Status, &run.StartedAt, &completed, &run.DurationNs, &errStr, &run.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan job run: %w", err)
		}
		if completed != nil {
			run.CompletedAt = completed
		}
		if errStr != nil {
			run.Error = *errStr
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

	// Fill in last_result and last_error from the most recent run per job.
	for i, s := range summaries {
		var status string
		var errStr *string
		err := r.pool.QueryRow(ctx,
			`SELECT status, error FROM automation_job_runs
			 WHERE job_name = $1 ORDER BY started_at DESC LIMIT 1`,
			s.JobName,
		).Scan(&status, &errStr)
		if err == nil {
			summaries[i].LastResult = status
			if errStr != nil {
				summaries[i].LastError = *errStr
			}
		}
	}

	return summaries, nil
}
