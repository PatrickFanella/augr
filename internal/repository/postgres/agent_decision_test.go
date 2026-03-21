package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ---------------------------------------------------------------------------
// Unit tests – query builder
// ---------------------------------------------------------------------------

func TestBuildGetByRunQuery_NoFilters(t *testing.T) {
	runID := uuid.New()
	query, args := buildGetByRunQuery(runID, repository.AgentDecisionFilter{}, 10, 0)

	// runID + limit + offset = 3 args
	if len(args) != 3 {
		t.Fatalf("expected 3 args (runID, limit, offset), got %d", len(args))
	}

	if args[0] != runID {
		t.Errorf("expected args[0] = runID %s, got %v", runID, args[0])
	}
	if args[1] != 10 {
		t.Errorf("expected limit=10, got %v", args[1])
	}
	if args[2] != 0 {
		t.Errorf("expected offset=0, got %v", args[2])
	}

	assertContains(t, query, "FROM agent_decisions")
	assertContains(t, query, "pipeline_run_id = $1")
	assertContains(t, query, "ORDER BY phase, round_number NULLS LAST, created_at")
	assertContains(t, query, "LIMIT $2 OFFSET $3")
	assertNotContains(t, query, "agent_role =")
	assertNotContains(t, query, "phase =")
	assertNotContains(t, query, "round_number =")
}

func TestBuildGetByRunQuery_AllFilters(t *testing.T) {
	runID := uuid.New()
	roundNumber := 2
	filter := repository.AgentDecisionFilter{
		AgentRole:   domain.AgentRoleTrader,
		Phase:       domain.PhaseTrading,
		RoundNumber: &roundNumber,
	}

	query, args := buildGetByRunQuery(runID, filter, 25, 50)

	// runID + role + phase + round + limit + offset = 6 args
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "pipeline_run_id = $1")
	assertContains(t, query, "agent_role = $2")
	assertContains(t, query, "phase = $3")
	assertContains(t, query, "round_number = $4")
	assertContains(t, query, "LIMIT $5 OFFSET $6")

	if args[0] != runID {
		t.Errorf("expected args[0] = runID, got %v", args[0])
	}
	if args[1] != domain.AgentRoleTrader {
		t.Errorf("expected args[1] = trader role, got %v", args[1])
	}
	if args[2] != domain.PhaseTrading {
		t.Errorf("expected args[2] = trading phase, got %v", args[2])
	}
	if args[3] != roundNumber {
		t.Errorf("expected args[3] = round %d, got %v", roundNumber, args[3])
	}
	if args[4] != 25 {
		t.Errorf("expected limit=25, got %v", args[4])
	}
	if args[5] != 50 {
		t.Errorf("expected offset=50, got %v", args[5])
	}
}

func TestBuildGetByRunQuery_PartialFilters(t *testing.T) {
	runID := uuid.New()
	filter := repository.AgentDecisionFilter{
		AgentRole: domain.AgentRoleMarketAnalyst,
	}

	query, args := buildGetByRunQuery(runID, filter, 10, 0)

	// runID + role + limit + offset = 4 args
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "pipeline_run_id = $1")
	assertContains(t, query, "agent_role = $2")
	assertNotContains(t, query, "phase =")
	assertNotContains(t, query, "round_number =")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

func TestBuildGetByRunQuery_PhaseOnlyFilter(t *testing.T) {
	runID := uuid.New()
	filter := repository.AgentDecisionFilter{
		Phase: domain.PhaseAnalysis,
	}

	query, args := buildGetByRunQuery(runID, filter, 10, 0)

	// runID + phase + limit + offset = 4 args
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "pipeline_run_id = $1")
	assertNotContains(t, query, "agent_role =")
	assertContains(t, query, "phase = $2")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

// ---------------------------------------------------------------------------
// Unit tests – marshalOutputStructured
// ---------------------------------------------------------------------------

func TestMarshalOutputStructured_ValidJSON(t *testing.T) {
	input := json.RawMessage(`{"signal":"buy","confidence":0.85}`)

	got, err := marshalOutputStructured(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != `{"signal":"buy","confidence":0.85}` {
		t.Errorf("expected pass-through, got %s", got)
	}
}

func TestMarshalOutputStructured_NilDefaultsToNull(t *testing.T) {
	got, err := marshalOutputStructured(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil, got %s", got)
	}
}

func TestMarshalOutputStructured_EmptyDefaultsToNull(t *testing.T) {
	got, err := marshalOutputStructured(json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil for empty input, got %s", got)
	}
}

func TestMarshalOutputStructured_InvalidJSON(t *testing.T) {
	_, err := marshalOutputStructured(json.RawMessage(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestAgentDecisionRepoIntegration_CreateAndGetByRun(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newAgentDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAgentDecisionRepo(pool)
	runID := uuid.New()
	otherRunID := uuid.New()

	round1 := 1
	d1 := &domain.AgentDecision{
		PipelineRunID:    runID,
		AgentRole:        domain.AgentRoleMarketAnalyst,
		Phase:            domain.PhaseAnalysis,
		InputSummary:     "AAPL price data",
		OutputText:       "Bullish trend detected",
		OutputStructured: json.RawMessage(`{"trend":"bullish"}`),
		LLMProvider:      "openai",
		LLMModel:         "gpt-4o",
		PromptTokens:     200,
		CompletionTokens: 50,
		LatencyMS:        450,
	}
	d2 := &domain.AgentDecision{
		PipelineRunID: runID,
		AgentRole:     domain.AgentRoleBullResearcher,
		Phase:         domain.PhaseResearchDebate,
		RoundNumber:   &round1,
		OutputText:    "Strong fundamentals support bullish case",
	}
	// A decision for a different run – should not appear in filtered results.
	d3 := &domain.AgentDecision{
		PipelineRunID: otherRunID,
		AgentRole:     domain.AgentRoleTrader,
		Phase:         domain.PhaseTrading,
		OutputText:    "Decision for other run",
	}

	for _, d := range []*domain.AgentDecision{d1, d2, d3} {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if d.ID == uuid.Nil {
			t.Fatal("expected Create() to populate ID")
		}
		if d.CreatedAt.IsZero() {
			t.Fatal("expected Create() to populate CreatedAt")
		}
	}

	decisions, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}

	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions for runID, got %d", len(decisions))
	}

	// Verify full LLM metadata round-trip on d1.
	var got *domain.AgentDecision
	for i := range decisions {
		if decisions[i].ID == d1.ID {
			got = &decisions[i]
			break
		}
	}
	if got == nil {
		t.Fatal("expected to find d1 in GetByRun() results")
	}

	if got.AgentRole != d1.AgentRole {
		t.Errorf("AgentRole: want %q, got %q", d1.AgentRole, got.AgentRole)
	}
	if got.Phase != d1.Phase {
		t.Errorf("Phase: want %q, got %q", d1.Phase, got.Phase)
	}
	if got.InputSummary != d1.InputSummary {
		t.Errorf("InputSummary: want %q, got %q", d1.InputSummary, got.InputSummary)
	}
	if got.OutputText != d1.OutputText {
		t.Errorf("OutputText: want %q, got %q", d1.OutputText, got.OutputText)
	}
	if string(got.OutputStructured) != string(d1.OutputStructured) {
		t.Errorf("OutputStructured: want %s, got %s", d1.OutputStructured, got.OutputStructured)
	}
	if got.LLMProvider != d1.LLMProvider {
		t.Errorf("LLMProvider: want %q, got %q", d1.LLMProvider, got.LLMProvider)
	}
	if got.LLMModel != d1.LLMModel {
		t.Errorf("LLMModel: want %q, got %q", d1.LLMModel, got.LLMModel)
	}
	if got.PromptTokens != d1.PromptTokens {
		t.Errorf("PromptTokens: want %d, got %d", d1.PromptTokens, got.PromptTokens)
	}
	if got.CompletionTokens != d1.CompletionTokens {
		t.Errorf("CompletionTokens: want %d, got %d", d1.CompletionTokens, got.CompletionTokens)
	}
	if got.LatencyMS != d1.LatencyMS {
		t.Errorf("LatencyMS: want %d, got %d", d1.LatencyMS, got.LatencyMS)
	}
}

func TestAgentDecisionRepoIntegration_FilterByRoleAndPhase(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newAgentDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAgentDecisionRepo(pool)
	runID := uuid.New()

	round1 := 1
	round2 := 2

	decisions := []*domain.AgentDecision{
		{PipelineRunID: runID, AgentRole: domain.AgentRoleBullResearcher, Phase: domain.PhaseResearchDebate, RoundNumber: &round1, OutputText: "Bull round 1"},
		{PipelineRunID: runID, AgentRole: domain.AgentRoleBearResearcher, Phase: domain.PhaseResearchDebate, RoundNumber: &round1, OutputText: "Bear round 1"},
		{PipelineRunID: runID, AgentRole: domain.AgentRoleBullResearcher, Phase: domain.PhaseResearchDebate, RoundNumber: &round2, OutputText: "Bull round 2"},
		{PipelineRunID: runID, AgentRole: domain.AgentRoleTrader, Phase: domain.PhaseTrading, OutputText: "Trader output"},
	}

	for _, d := range decisions {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Filter by agent role.
	bullDecisions, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{
		AgentRole: domain.AgentRoleBullResearcher,
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() by role error = %v", err)
	}
	if len(bullDecisions) != 2 {
		t.Fatalf("expected 2 bull researcher decisions, got %d", len(bullDecisions))
	}

	// Filter by phase.
	researchDecisions, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{
		Phase: domain.PhaseResearchDebate,
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() by phase error = %v", err)
	}
	if len(researchDecisions) != 3 {
		t.Fatalf("expected 3 research_debate decisions, got %d", len(researchDecisions))
	}

	// Filter by role and phase combined (GetByRunAndRole behaviour).
	bearResearch, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{
		AgentRole: domain.AgentRoleBearResearcher,
		Phase:     domain.PhaseResearchDebate,
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() by role+phase error = %v", err)
	}
	if len(bearResearch) != 1 {
		t.Fatalf("expected 1 bear researcher decision, got %d", len(bearResearch))
	}
	if bearResearch[0].OutputText != "Bear round 1" {
		t.Errorf("unexpected output text: %q", bearResearch[0].OutputText)
	}

	// Filter by round number.
	round1Decisions, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{
		RoundNumber: &round1,
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() by round error = %v", err)
	}
	if len(round1Decisions) != 2 {
		t.Fatalf("expected 2 round-1 decisions, got %d", len(round1Decisions))
	}
}

func TestAgentDecisionRepoIntegration_OrderByPhaseAndRound(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newAgentDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAgentDecisionRepo(pool)
	runID := uuid.New()

	round2 := 2
	round1 := 1

	// Insert decisions out of order to verify the ORDER BY clause.
	toInsert := []*domain.AgentDecision{
		{PipelineRunID: runID, AgentRole: domain.AgentRoleTrader, Phase: domain.PhaseTrading, OutputText: "trading"},
		{PipelineRunID: runID, AgentRole: domain.AgentRoleBullResearcher, Phase: domain.PhaseResearchDebate, RoundNumber: &round2, OutputText: "bull round 2"},
		{PipelineRunID: runID, AgentRole: domain.AgentRoleMarketAnalyst, Phase: domain.PhaseAnalysis, OutputText: "analysis"},
		{PipelineRunID: runID, AgentRole: domain.AgentRoleBearResearcher, Phase: domain.PhaseResearchDebate, RoundNumber: &round1, OutputText: "bear round 1"},
	}

	for _, d := range toInsert {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	decisions, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}

	if len(decisions) != 4 {
		t.Fatalf("expected 4 decisions, got %d", len(decisions))
	}

	// ORDER BY phase (ASC), round_number NULLS LAST, created_at.
	// Phases in alphabetical order: analysis < research_debate < trading.
	if decisions[0].Phase != domain.PhaseAnalysis {
		t.Errorf("expected first decision phase=%q, got %q", domain.PhaseAnalysis, decisions[0].Phase)
	}

	// Both research_debate decisions come before trading.
	if decisions[1].Phase != domain.PhaseResearchDebate || decisions[2].Phase != domain.PhaseResearchDebate {
		t.Errorf("expected decisions[1] and [2] to be research_debate, got %q and %q",
			decisions[1].Phase, decisions[2].Phase)
	}

	// Within research_debate, round 1 before round 2.
	if decisions[1].RoundNumber == nil || *decisions[1].RoundNumber != 1 {
		t.Errorf("expected decisions[1].RoundNumber=1, got %v", decisions[1].RoundNumber)
	}
	if decisions[2].RoundNumber == nil || *decisions[2].RoundNumber != 2 {
		t.Errorf("expected decisions[2].RoundNumber=2, got %v", decisions[2].RoundNumber)
	}

	if decisions[3].Phase != domain.PhaseTrading {
		t.Errorf("expected last decision phase=%q, got %q", domain.PhaseTrading, decisions[3].Phase)
	}
}

func TestAgentDecisionRepoIntegration_Pagination(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newAgentDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAgentDecisionRepo(pool)
	runID := uuid.New()

	for i := 0; i < 5; i++ {
		d := &domain.AgentDecision{
			PipelineRunID: runID,
			AgentRole:     domain.AgentRoleMarketAnalyst,
			Phase:         domain.PhaseAnalysis,
			OutputText:    "output",
		}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	page1, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{}, 3, 0)
	if err != nil {
		t.Fatalf("GetByRun() page 1 error = %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("expected 3 results on page 1, got %d", len(page1))
	}

	page2, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{}, 3, 3)
	if err != nil {
		t.Fatalf("GetByRun() page 2 error = %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("expected 2 results on page 2, got %d", len(page2))
	}

	// Ensure no overlap between pages.
	ids := make(map[uuid.UUID]bool)
	for _, d := range append(page1, page2...) {
		if ids[d.ID] {
			t.Errorf("duplicate ID %s across pages", d.ID)
		}
		ids[d.ID] = true
	}
}

func TestAgentDecisionRepoIntegration_EmptyResult(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newAgentDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAgentDecisionRepo(pool)

	decisions, err := repo.GetByRun(ctx, uuid.New(), repository.AgentDecisionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() for unknown run error = %v", err)
	}

	if decisions != nil {
		t.Errorf("expected nil slice for empty result, got %v", decisions)
	}
}

func TestAgentDecisionRepoIntegration_NullableFieldsRoundTrip(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newAgentDecisionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAgentDecisionRepo(pool)
	runID := uuid.New()

	// Decision with all optional fields omitted (zero values).
	d := &domain.AgentDecision{
		PipelineRunID: runID,
		AgentRole:     domain.AgentRoleInvestJudge,
		Phase:         domain.PhaseRiskDebate,
		OutputText:    "minimal decision",
	}

	if err := repo.Create(ctx, d); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	decisions, err := repo.GetByRun(ctx, runID, repository.AgentDecisionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	got := decisions[0]
	if got.RoundNumber != nil {
		t.Errorf("expected nil RoundNumber, got %v", got.RoundNumber)
	}
	if got.InputSummary != "" {
		t.Errorf("expected empty InputSummary, got %q", got.InputSummary)
	}
	if got.OutputStructured != nil {
		t.Errorf("expected nil OutputStructured, got %s", got.OutputStructured)
	}
	if got.LLMProvider != "" {
		t.Errorf("expected empty LLMProvider, got %q", got.LLMProvider)
	}
	if got.LLMModel != "" {
		t.Errorf("expected empty LLMModel, got %q", got.LLMModel)
	}
	if got.PromptTokens != 0 {
		t.Errorf("expected PromptTokens=0, got %d", got.PromptTokens)
	}
	if got.CompletionTokens != 0 {
		t.Errorf("expected CompletionTokens=0, got %d", got.CompletionTokens)
	}
	if got.LatencyMS != 0 {
		t.Errorf("expected LatencyMS=0, got %d", got.LatencyMS)
	}
}

// ---------------------------------------------------------------------------
// Integration test helper
// ---------------------------------------------------------------------------

func newAgentDecisionIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_agent_decision_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TABLE agent_decisions (
			id                UUID        NOT NULL DEFAULT gen_random_uuid(),
			pipeline_run_id   UUID        NOT NULL,
			agent_role        TEXT        NOT NULL,
			phase             TEXT        NOT NULL,
			round_number      INT,
			input_summary     TEXT,
			output_text       TEXT        NOT NULL,
			output_structured JSONB,
			llm_provider      TEXT,
			llm_model         TEXT,
			prompt_tokens     INT,
			completion_tokens INT,
			latency_ms        INT,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (id, created_at)
		) PARTITION BY RANGE (created_at)`,
		`CREATE TABLE agent_decisions_2026_q1 PARTITION OF agent_decisions
			FOR VALUES FROM ('2026-01-01') TO ('2026-04-01')`,
		`CREATE TABLE agent_decisions_default PARTITION OF agent_decisions DEFAULT`,
		`CREATE INDEX idx_agent_decisions_pipeline_run_id ON agent_decisions (pipeline_run_id)`,
		`CREATE INDEX idx_agent_decisions_agent_role ON agent_decisions (agent_role)`,
		`CREATE INDEX idx_agent_decisions_created_at ON agent_decisions (created_at)`,
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
