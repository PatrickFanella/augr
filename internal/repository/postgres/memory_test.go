package postgres

import (
	"context"
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

// ---------------------------------------------------------------------------
// Unit tests – query builder
// ---------------------------------------------------------------------------

func TestBuildSearchQuery_NoFTS_NoFilters(t *testing.T) {
	query, args := buildSearchQuery("", repository.MemorySearchFilter{}, 10, 0)

	// limit + offset = 2 args
	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}
	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}
	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM agent_memories")
	assertContains(t, query, "ORDER BY created_at DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "situation_tsv")
	assertNotContains(t, query, "agent_role =")
	assertNotContains(t, query, "rank")
}

func TestBuildSearchQuery_FTS_NoFilters(t *testing.T) {
	query, args := buildSearchQuery("bullish trend", repository.MemorySearchFilter{}, 5, 0)

	// fts_query + limit + offset = 3 args
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "bullish trend" {
		t.Errorf("expected args[0] = FTS query, got %v", args[0])
	}

	assertContains(t, query, "situation_tsv @@ plainto_tsquery('english', $1)")
	assertContains(t, query, "ts_rank(situation_tsv, plainto_tsquery('english', $1)) AS rank")
	assertContains(t, query, "ORDER BY rank DESC, created_at DESC")
	assertContains(t, query, "LIMIT $2 OFFSET $3")
}

func TestBuildSearchQuery_FTS_WithRoleFilter(t *testing.T) {
	filter := repository.MemorySearchFilter{
		AgentRole: domain.AgentRoleTrader,
	}

	query, args := buildSearchQuery("market downturn", filter, 5, 0)

	// fts_query + role + limit + offset = 4 args
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "situation_tsv @@ plainto_tsquery('english', $1)")
	assertContains(t, query, "agent_role = $2")
	assertContains(t, query, "LIMIT $3 OFFSET $4")

	if args[1] != domain.AgentRoleTrader {
		t.Errorf("expected args[1] = trader role, got %v", args[1])
	}
}

func TestBuildSearchQuery_NoFTS_AllFilters(t *testing.T) {
	runID := uuid.New()
	minScore := 0.75
	now := time.Now()
	before := now.Add(time.Hour)
	filter := repository.MemorySearchFilter{
		AgentRole:         domain.AgentRoleMarketAnalyst,
		PipelineRunID:     &runID,
		MinRelevanceScore: &minScore,
		CreatedAfter:      &now,
		CreatedBefore:     &before,
	}

	query, args := buildSearchQuery("", filter, 10, 20)

	// role + run_id + min_score + after + before + limit + offset = 7 args
	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "agent_role = $1")
	assertContains(t, query, "pipeline_run_id = $2")
	assertContains(t, query, "relevance_score >= $3")
	assertContains(t, query, "created_at >= $4")
	assertContains(t, query, "created_at < $5")
	assertContains(t, query, "LIMIT $6 OFFSET $7")
	assertContains(t, query, "ORDER BY created_at DESC")
	assertNotContains(t, query, "rank")
}

func TestBuildSearchQuery_FTS_AllFilters(t *testing.T) {
	runID := uuid.New()
	minScore := 0.5
	now := time.Now()
	before := now.Add(time.Hour)
	filter := repository.MemorySearchFilter{
		AgentRole:         domain.AgentRoleBullResearcher,
		PipelineRunID:     &runID,
		MinRelevanceScore: &minScore,
		CreatedAfter:      &now,
		CreatedBefore:     &before,
	}

	query, args := buildSearchQuery("positive outlook", filter, 3, 0)

	// fts_query + role + run_id + min_score + after + before + limit + offset = 8 args
	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "situation_tsv @@ plainto_tsquery('english', $1)")
	assertContains(t, query, "agent_role = $2")
	assertContains(t, query, "pipeline_run_id = $3")
	assertContains(t, query, "relevance_score >= $4")
	assertContains(t, query, "created_at >= $5")
	assertContains(t, query, "created_at < $6")
	assertContains(t, query, "LIMIT $7 OFFSET $8")
	assertContains(t, query, "ORDER BY rank DESC, created_at DESC")
}

func TestBuildSearchQuery_WhitespaceOnlyQuery(t *testing.T) {
	// Search() trims whitespace before calling buildSearchQuery, so
	// a whitespace-only input arrives here as "".
	query, args := buildSearchQuery("", repository.MemorySearchFilter{}, 10, 0)

	// Should behave identically to the no-FTS path.
	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	assertNotContains(t, query, "situation_tsv")
	assertNotContains(t, query, "rank")
	assertContains(t, query, "ORDER BY created_at DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
}

// ---------------------------------------------------------------------------
// Unit test – nilIfEmpty
// ---------------------------------------------------------------------------

func TestNilIfEmpty(t *testing.T) {
	if nilIfEmpty("") != nil {
		t.Error("expected nil for empty string")
	}
	s := "hello"
	got := nilIfEmpty(s)
	if got == nil || *got != s {
		t.Errorf("expected pointer to %q, got %v", s, got)
	}
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestMemoryRepoIntegration_CreateAndSearch(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	runID := uuid.New()
	m1 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleMarketAnalyst,
		Situation:      "AAPL showing a strong bullish reversal with increasing volume",
		Recommendation: "Consider buying AAPL",
		Outcome:        "Price increased 5%",
		PipelineRunID:  &runID,
	}
	m2 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleMarketAnalyst,
		Situation:      "MSFT earnings beat expectations with cloud revenue growth",
		Recommendation: "MSFT is a strong hold",
	}
	m3 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "Market-wide bearish sentiment following interest rate hike",
		Recommendation: "Reduce exposure to equities",
	}

	for _, m := range []*domain.AgentMemory{m1, m2, m3} {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if m.ID == uuid.Nil {
			t.Fatal("expected Create() to populate ID")
		}
		if m.CreatedAt.IsZero() {
			t.Fatal("expected Create() to populate CreatedAt")
		}
	}

	// FTS search: "bullish" should match m1.
	results, err := repo.Search(ctx, "bullish", repository.MemorySearchFilter{}, 5, 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 result for 'bullish' search")
	}
	if results[0].ID != m1.ID {
		t.Errorf("expected first result to be m1 (ID=%s), got ID=%s", m1.ID, results[0].ID)
	}
	// m1 was created without a stored relevance_score; RelevanceScore
	// reflects the stored column, not the ts_rank used for ordering.
	if results[0].RelevanceScore != nil {
		t.Errorf("expected nil RelevanceScore (no stored value), got %v", *results[0].RelevanceScore)
	}

	// Verify full round-trip on m1.
	got := results[0]
	if got.AgentRole != m1.AgentRole {
		t.Errorf("AgentRole: want %q, got %q", m1.AgentRole, got.AgentRole)
	}
	if got.Situation != m1.Situation {
		t.Errorf("Situation: want %q, got %q", m1.Situation, got.Situation)
	}
	if got.Recommendation != m1.Recommendation {
		t.Errorf("Recommendation: want %q, got %q", m1.Recommendation, got.Recommendation)
	}
	if got.Outcome != m1.Outcome {
		t.Errorf("Outcome: want %q, got %q", m1.Outcome, got.Outcome)
	}
	if got.PipelineRunID == nil || *got.PipelineRunID != runID {
		t.Errorf("PipelineRunID: want %s, got %v", runID, got.PipelineRunID)
	}
}

func TestMemoryRepoIntegration_SearchWithRoleFilter(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	// Insert memories with different roles but overlapping situation text.
	m1 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleMarketAnalyst,
		Situation:      "Stock market experiencing significant volatility",
		Recommendation: "Wait for clarity",
	}
	m2 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "Volatility index spiking during market selloff",
		Recommendation: "Tighten stops",
	}

	for _, m := range []*domain.AgentMemory{m1, m2} {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Search with role filter – should return only trader's memory.
	results, err := repo.Search(ctx, "volatility", repository.MemorySearchFilter{
		AgentRole: domain.AgentRoleTrader,
	}, 5, 0)
	if err != nil {
		t.Fatalf("Search() with role filter error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with trader role, got %d", len(results))
	}
	if results[0].ID != m2.ID {
		t.Errorf("expected m2 (trader), got ID=%s", results[0].ID)
	}
}

func TestMemoryRepoIntegration_Delete(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	m := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleRiskManager,
		Situation:      "Portfolio risk exceeding maximum threshold",
		Recommendation: "Reduce position sizes",
	}
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Delete the memory.
	if err := repo.Delete(ctx, m.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Search should return nothing.
	results, err := repo.Search(ctx, "risk", repository.MemorySearchFilter{}, 5, 0)
	if err != nil {
		t.Fatalf("Search() after delete error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestMemoryRepoIntegration_DeleteUnknownID(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	err := repo.Delete(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error when deleting unknown ID, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryRepoIntegration_SearchEmptyResult(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	results, err := repo.Search(ctx, "nonexistent", repository.MemorySearchFilter{}, 5, 0)
	if err != nil {
		t.Fatalf("Search() for nonexistent error = %v", err)
	}
	if results != nil {
		t.Errorf("expected nil slice for empty result, got %v", results)
	}
}

func TestMemoryRepoIntegration_SearchNoFTS(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	m1 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "First memory situation",
		Recommendation: "First recommendation",
	}
	m2 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "Second memory situation",
		Recommendation: "Second recommendation",
	}

	for _, m := range []*domain.AgentMemory{m1, m2} {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Search without FTS query – should return all, ordered by created_at DESC.
	results, err := repo.Search(ctx, "", repository.MemorySearchFilter{
		AgentRole: domain.AgentRoleTrader,
	}, 10, 0)
	if err != nil {
		t.Fatalf("Search() no FTS error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Most recent first.
	if results[0].ID != m2.ID {
		t.Errorf("expected first result to be m2 (most recent), got ID=%s", results[0].ID)
	}
}

func TestMemoryRepoIntegration_Pagination(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	for i := 0; i < 5; i++ {
		m := &domain.AgentMemory{
			AgentRole:      domain.AgentRoleMarketAnalyst,
			Situation:      "Market analysis report number",
			Recommendation: "Hold positions",
		}
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	page1, err := repo.Search(ctx, "", repository.MemorySearchFilter{}, 3, 0)
	if err != nil {
		t.Fatalf("Search() page 1 error = %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("expected 3 results on page 1, got %d", len(page1))
	}

	page2, err := repo.Search(ctx, "", repository.MemorySearchFilter{}, 3, 3)
	if err != nil {
		t.Fatalf("Search() page 2 error = %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("expected 2 results on page 2, got %d", len(page2))
	}

	// Ensure no overlap between pages.
	ids := make(map[uuid.UUID]bool)
	for _, m := range append(page1, page2...) {
		if ids[m.ID] {
			t.Errorf("duplicate ID %s across pages", m.ID)
		}
		ids[m.ID] = true
	}
}

func TestMemoryRepoIntegration_NullableFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	// Memory with all optional fields omitted.
	m := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleInvestJudge,
		Situation:      "Minimal memory with no optional fields",
		Recommendation: "No recommendation",
	}

	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	results, err := repo.Search(ctx, "", repository.MemorySearchFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.Outcome != "" {
		t.Errorf("expected empty Outcome, got %q", got.Outcome)
	}
	if got.PipelineRunID != nil {
		t.Errorf("expected nil PipelineRunID, got %v", got.PipelineRunID)
	}
	if got.RelevanceScore != nil {
		t.Errorf("expected nil RelevanceScore, got %v", got.RelevanceScore)
	}
}

func TestMemoryRepoIntegration_FTSRelevanceRanking(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	// m1 contains "bullish" once; m2 has a more relevant situation text.
	m1 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleMarketAnalyst,
		Situation:      "The technology sector shows mixed signals with some bullish indicators",
		Recommendation: "Monitor closely",
	}
	m2 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleMarketAnalyst,
		Situation:      "Strong bullish reversal pattern with bullish engulfing candle confirmed on daily chart",
		Recommendation: "Buy signal",
	}

	for _, m := range []*domain.AgentMemory{m1, m2} {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	results, err := repo.Search(ctx, "bullish", repository.MemorySearchFilter{}, 5, 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both memories were created without a stored relevance_score, so
	// RelevanceScore should be nil (ts_rank is used only for ordering).
	for i, r := range results {
		if r.RelevanceScore != nil {
			t.Errorf("result[%d]: expected nil RelevanceScore (no stored value), got %v", i, *r.RelevanceScore)
		}
	}

	// The result with more occurrences of "bullish" should rank first.
	if results[0].ID != m2.ID {
		t.Errorf("expected m2 (more relevant) to rank first, got m1")
	}
}

func TestMemoryRepoIntegration_SearchWithDateFilter(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newMemoryIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewMemoryRepo(pool)

	m := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "Date filter test memory",
		Recommendation: "Test",
	}
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// CreatedAfter in the future should return nothing.
	futureTime := time.Now().Add(24 * time.Hour)
	results, err := repo.Search(ctx, "", repository.MemorySearchFilter{
		CreatedAfter: &futureTime,
	}, 10, 0)
	if err != nil {
		t.Fatalf("Search() with date filter error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results with future CreatedAfter, got %d", len(results))
	}

	// CreatedBefore in the future should return the memory.
	results, err = repo.Search(ctx, "", repository.MemorySearchFilter{
		CreatedBefore: &futureTime,
	}, 10, 0)
	if err != nil {
		t.Fatalf("Search() with date filter error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result with future CreatedBefore, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Integration test helper
// ---------------------------------------------------------------------------

func newMemoryIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_memory_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	ddl := []string{
		`CREATE TABLE agent_memories (
			id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			agent_role       TEXT        NOT NULL,
			situation        TEXT        NOT NULL,
			situation_tsv    TSVECTOR,
			recommendation   TEXT        NOT NULL DEFAULT '',
			outcome          TEXT,
			pipeline_run_id  UUID,
			relevance_score  NUMERIC(5, 4),
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE OR REPLACE FUNCTION agent_memories_tsv_trigger() RETURNS trigger AS $$
		 BEGIN
			NEW.situation_tsv := to_tsvector('english', NEW.situation);
			RETURN NEW;
		 END;
		 $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER trg_agent_memories_tsv
			BEFORE INSERT OR UPDATE OF situation ON agent_memories
			FOR EACH ROW EXECUTE FUNCTION agent_memories_tsv_trigger()`,
		`CREATE INDEX idx_agent_memories_situation_tsv ON agent_memories USING GIN (situation_tsv)`,
		`CREATE INDEX idx_agent_memories_agent_role ON agent_memories (agent_role)`,
		`CREATE INDEX idx_agent_memories_pipeline_run_id ON agent_memories (pipeline_run_id)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}
