package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestMarshalOvernightBacktestJSONSlices(t *testing.T) {
	run := domain.NewOvernightBacktestRun()
	run.Candidates = []domain.OvernightBacktestCandidate{{Ticker: "AAPL", Close: 200}}
	run.Generated = []domain.OvernightBacktestGenerated{{Ticker: "AAPL", Config: json.RawMessage(`{}`), Evidence: json.RawMessage(`{"attempts":[]}`)}}
	run.Errors = []string{"sample error"}
	run.Summary = domain.OvernightBacktestSummary{Candidates: 1, Generated: 1}
	_, _, _, _, err := marshalOvernightBacktestRunJSON(run)
	if err != nil {
		t.Fatalf("marshalOvernightBacktestRunJSON() error = %v", err)
	}
}

func TestBuildOvernightBacktestListLatestLimit(t *testing.T) {
	query, args := buildOvernightBacktestListLatestQuery(0)
	if len(args) != 1 || args[0] != 20 {
		t.Fatalf("args = %#v, want default limit 20", args)
	}
	assertContains(t, query, "FROM overnight_backtest_runs")
	assertContains(t, query, "ORDER BY started_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1")
}

func TestOvernightBacktestRunRepoIntegration_CRUD(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newOvernightBacktestIntegrationPool(t, ctx)
	defer cleanup()
	repo := NewOvernightBacktestRunRepo(pool)
	run := domain.NewOvernightBacktestRun()
	run.Candidates = []domain.OvernightBacktestCandidate{{Ticker: "MSFT", Close: 300}}
	run.Generated = []domain.OvernightBacktestGenerated{{Ticker: "MSFT", Config: json.RawMessage(`{}`), Evidence: json.RawMessage(`{"attempts":[]}`)}}
	if err := repo.Create(ctx, &run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if run.StartedAt.IsZero() || run.UpdatedAt.IsZero() {
		t.Fatalf("created timestamps should be populated: started=%v updated=%v", run.StartedAt, run.UpdatedAt)
	}
	if time.Since(run.StartedAt) > time.Minute {
		t.Fatalf("StartedAt = %v, want recent timestamp", run.StartedAt)
	}
	got, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Candidates[0].Ticker != "MSFT" {
		t.Fatalf("candidate ticker = %q, want MSFT", got.Candidates[0].Ticker)
	}
	if string(got.Generated[0].Evidence) != `{"attempts":[]}` {
		t.Fatalf("generated evidence = %s", got.Generated[0].Evidence)
	}
	active, err := repo.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive() error = %v", err)
	}
	if active.ID != run.ID {
		t.Fatalf("active ID = %s, want %s", active.ID, run.ID)
	}
	run.Phase = domain.OvernightBacktestPhaseGenerate
	run.CandidateIndex = 1
	run.Summary = domain.OvernightBacktestSummary{Candidates: 2, Generated: 2, Swept: 2, Validated: 2, Deployed: 1, Created: 1, Reused: 1}
	if err := repo.Update(ctx, &run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	updated, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get() updated error = %v", err)
	}
	if updated.Phase != domain.OvernightBacktestPhaseGenerate || updated.CandidateIndex != 1 {
		t.Fatalf("updated phase/index = %s/%d", updated.Phase, updated.CandidateIndex)
	}
	if updated.Summary.Created != 1 || updated.Summary.Reused != 1 || updated.Summary.Deployed != 1 {
		t.Fatalf("updated deployment summary = %+v", updated.Summary)
	}
	now := time.Now().UTC()
	run.Status = domain.OvernightBacktestStatusCompleted
	run.Phase = domain.OvernightBacktestPhaseDone
	run.CompletedAt = &now
	if err := repo.Update(ctx, &run); err != nil {
		t.Fatalf("complete Update() error = %v", err)
	}
	_, err = repo.GetActive(ctx)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetActive() error = %v, want ErrNotFound", err)
	}
	latest, err := repo.ListLatest(ctx, 5)
	if err != nil {
		t.Fatalf("ListLatest() error = %v", err)
	}
	if len(latest) != 1 || latest[0].ID != run.ID {
		t.Fatalf("latest = %#v, want completed run", latest)
	}
}

func newOvernightBacktestIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}
	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}
	schemaName := "integration_overnight_backtest_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+pqQuoteIdent(schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}
	ddl := `CREATE TABLE overnight_backtest_runs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
		phase TEXT NOT NULL CHECK (phase IN ('screen', 'generate', 'sweep_validate_deploy', 'done')),
		candidate_index INTEGER NOT NULL DEFAULT 0 CHECK (candidate_index >= 0),
		candidates JSONB NOT NULL DEFAULT '[]'::jsonb,
		generated JSONB NOT NULL DEFAULT '[]'::jsonb,
		errors JSONB NOT NULL DEFAULT '[]'::jsonb,
		summary JSONB NOT NULL DEFAULT '{}'::jsonb,
		started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		completed_at TIMESTAMPTZ
	)`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to apply test schema DDL: %v", err)
	}
	return pool, func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+pqQuoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
	}
}

func pqQuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
