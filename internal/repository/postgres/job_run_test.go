package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeJobRunRows struct {
	rows [][]any
	idx  int
	err  error
}

func (f *fakeJobRunRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}

func (f *fakeJobRunRows) Scan(dest ...any) error {
	row := f.rows[f.idx-1]
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = row[i].(uuid.UUID)
		case *string:
			*d = row[i].(string)
		case **time.Time:
			if row[i] == nil {
				*d = nil
				continue
			}
			v := row[i].(time.Time)
			*d = &v
		case *int64:
			*d = row[i].(int64)
		case *int:
			*d = row[i].(int)
		case *time.Time:
			*d = row[i].(time.Time)
		case *[]byte:
			if row[i] == nil {
				*d = nil
				continue
			}
			*d = row[i].([]byte)
		case **string:
			if row[i] == nil {
				*d = nil
				continue
			}
			v := row[i].(string)
			*d = &v
		default:
			panic("unexpected scan destination")
		}
	}
	return nil
}

func (f *fakeJobRunRows) Err() error { return f.err }

func TestScanJobRunsIncludesResult(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	completed := started.Add(2 * time.Minute)
	lastErrAt := started.Add(90 * time.Second)
	created := started.Add(3 * time.Minute)
	rows := &fakeJobRunRows{rows: [][]any{{
		uuid.New(),
		"options_discovery",
		"ok",
		started,
		completed,
		int64(12345),
		[]byte(`{"candidates":12,"winners":3}`),
		nil,
		lastErrAt,
		7,
		created,
	}}}

	runs, err := scanJobRuns(rows)
	if err != nil {
		t.Fatalf("scanJobRuns error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if runs[0].Result["candidates"] != 12 || runs[0].Result["winners"] != 3 {
		t.Fatalf("Result = %#v, want counts preserved", runs[0].Result)
	}
	if runs[0].CompletedAt == nil || !runs[0].CompletedAt.Equal(completed) {
		t.Fatalf("CompletedAt = %v, want %v", runs[0].CompletedAt, completed)
	}
	if runs[0].LastErrorAt == nil || !runs[0].LastErrorAt.Equal(lastErrAt) {
		t.Fatalf("LastErrorAt = %v, want %v", runs[0].LastErrorAt, lastErrAt)
	}
}

func TestJobRunLifecycleIntegration(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newJobRunIntegrationPool(t, ctx)
	defer cleanup()
	repo := NewJobRunRepo(pool)
	started := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	run := &JobRun{JobName: "integration_job", Status: "running", StartedAt: started}
	if err := repo.Create(ctx, run); err != nil {
		t.Fatalf("Create(running) error = %v", err)
	}
	var status string
	var completedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, completed_at FROM automation_job_runs WHERE id=$1`, run.ID).Scan(&status, &completedAt); err != nil {
		t.Fatal(err)
	}
	if status != "running" || completedAt != nil {
		t.Fatalf("admission row status=%q completed_at=%v", status, completedAt)
	}

	completed := started.Add(10 * time.Second)
	run.Status = "ok"
	run.CompletedAt = &completed
	run.DurationNs = int64(10 * time.Second)
	run.Result = map[string]int{"items": 3}
	if err := repo.Complete(ctx, run); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	stored, err := repo.ListByJob(ctx, run.JobName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != run.ID || stored[0].Status != "ok" || stored[0].CompletedAt == nil || stored[0].Result["items"] != 3 {
		t.Fatalf("completed row = %+v", stored)
	}

	orphan := &JobRun{JobName: "orphaned_job", Status: "running", StartedAt: started}
	if err := repo.Create(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	recoveredAt := completed.Add(time.Minute)
	recovered, err := repo.FailIncomplete(ctx, recoveredAt, "process restarted")
	if err != nil {
		t.Fatalf("FailIncomplete() error = %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	orphans, err := repo.ListByJob(ctx, orphan.JobName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].Status != "error" || orphans[0].CompletedAt == nil || orphans[0].Error != "process restarted" || orphans[0].ConsecutiveFailures != 1 {
		t.Fatalf("recovered row = %+v", orphans)
	}
}

func newJobRunIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}
	admin, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("connect integration database: %v", err)
	}
	schema := "job_run_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatalf("create integration schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE automation_job_runs (
		id uuid PRIMARY KEY,
		job_name text NOT NULL,
		status text NOT NULL,
		started_at timestamptz NOT NULL,
		completed_at timestamptz,
		duration_ns bigint,
		result jsonb,
		error text,
		last_error_at timestamptz,
		consecutive_failures integer NOT NULL DEFAULT 0,
		created_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		pool.Close()
		_, _ = admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
		t.Fatalf("create job run table: %v", err)
	}
	return pool, func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	}
}
