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

func TestMarshalPolymarketDiscoveryJSONSlices(t *testing.T) {
	run := domain.NewPolymarketDiscoveryRun()
	run.Candidates = []domain.PolymarketDiscoveryCandidate{{Slug: "btc", Question: "Will BTC rise?", BestBid: 0.4}}
	run.Accepted = []domain.PolymarketDiscoveryAccepted{{Candidate: run.Candidates[0], Proposal: json.RawMessage(`{"kind":"test"}`)}}
	run.Deployed = []domain.PolymarketDiscoveryDeployed{{StrategyID: "s1", Slug: "btc", Template: "tmpl", Name: "BTC", Direction: "long", Conviction: 0.8, Reused: false}}
	run.Errors = []string{"sample error"}
	run.Summary = domain.PolymarketDiscoverySummary{FetchedAll: 1, Screened: 1, Proposed: 1, Accepted: 1, Deployed: 1}
	_, _, _, _, _, err := marshalPolymarketDiscoveryRunJSON(run)
	if err != nil {
		t.Fatalf("marshalPolymarketDiscoveryRunJSON() error = %v", err)
	}
}

func TestBuildPolymarketDiscoveryListLatestLimit(t *testing.T) {
	query, args := buildPolymarketDiscoveryListLatestQuery(0)
	if len(args) != 1 || args[0] != 20 {
		t.Fatalf("args = %#v, want default limit 20", args)
	}
	assertContains(t, query, "FROM polymarket_discovery_runs")
	assertContains(t, query, "ORDER BY started_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1")
}

func TestPolymarketDiscoveryRunRepoIntegration_CRUD(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPolymarketDiscoveryIntegrationPool(t, ctx)
	defer cleanup()
	repo := NewPolymarketDiscoveryRunRepo(pool)
	run := domain.NewPolymarketDiscoveryRun()
	run.Candidates = []domain.PolymarketDiscoveryCandidate{{Slug: "eth", Question: "Will ETH rise?", Description: "desc", Category: "crypto", ConditionID: "cond", EndDate: "2026-01-01", Volume24Hr: 10, Liquidity: 20, BestBid: 0.4, BestAsk: 0.5, Spread: 0.1, LastTradePrice: 0.45, ResolutionSource: "source"}}
	run.Accepted = []domain.PolymarketDiscoveryAccepted{{Candidate: run.Candidates[0], Proposal: json.RawMessage(`{"approved":true}`)}}
	run.Deployed = []domain.PolymarketDiscoveryDeployed{{StrategyID: "st-1", Slug: "eth", Template: "template", Name: "ETH long", Direction: "long", Conviction: 0.9, Reused: true}}
	run.Errors = []string{"warning one"}
	run.Summary = domain.PolymarketDiscoverySummary{FetchedAll: 1, Screened: 1, Proposed: 1, Accepted: 1, Deployed: 1}
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
	if got.Candidates[0].Slug != "eth" || string(got.Accepted[0].Proposal) != `{"approved":true}` {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	active, err := repo.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive() error = %v", err)
	}
	if active.ID != run.ID {
		t.Fatalf("active ID = %s, want %s", active.ID, run.ID)
	}
	run.Phase = domain.PolymarketDiscoveryPhasePropose
	run.CandidateIndex = 1
	if err := repo.Update(ctx, &run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	updated, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get() updated error = %v", err)
	}
	if updated.Phase != domain.PolymarketDiscoveryPhasePropose || updated.CandidateIndex != 1 {
		t.Fatalf("updated phase/index = %s/%d", updated.Phase, updated.CandidateIndex)
	}
	now := time.Now().UTC()
	run.Status = domain.PolymarketDiscoveryStatusCompleted
	run.Phase = domain.PolymarketDiscoveryPhaseDone
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

func newPolymarketDiscoveryIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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
	schemaName := "integration_polymarket_discovery_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
	ddl := `CREATE TABLE polymarket_discovery_runs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
		phase TEXT NOT NULL CHECK (phase IN ('screen', 'propose', 'deploy', 'done')),
		candidate_index INTEGER NOT NULL DEFAULT 0 CHECK (candidate_index >= 0),
		candidates JSONB NOT NULL DEFAULT '[]'::jsonb,
		accepted JSONB NOT NULL DEFAULT '[]'::jsonb,
		deployed JSONB NOT NULL DEFAULT '[]'::jsonb,
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
