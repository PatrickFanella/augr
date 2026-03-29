package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/memory"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// TestIntegration_MemoryReflection_EndToEnd validates the full reflection
// flow: create position + pipeline run + decisions → reflect with mock LLM →
// verify memories stored in the real database and retrievable via FTS.
func TestIntegration_MemoryReflection_EndToEnd(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	// 1. Create a strategy.
	strategy := createStrategy(t, ctx, r.Strategy, "Reflect Test", "AAPL")

	// 2. Create the position first so its DB opened_at (NOW()) naturally
	//    precedes the pipeline run's started_at.
	pos := createPosition(t, ctx, r.Position, strategy.ID, "AAPL", domain.PositionSideLong, 10, 180.00)

	// Reload the position to get the DB-assigned opened_at.
	pos, err := r.Position.Get(ctx, pos.ID)
	if err != nil {
		t.Fatalf("Get() position: %v", err)
	}

	// 3. Create a completed pipeline run whose started_at is after the
	//    position's opened_at so findPipelineRun can locate it.
	tradeDate := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	run := &domain.PipelineRun{
		StrategyID: strategy.ID,
		Ticker:     "AAPL",
		TradeDate:  tradeDate,
		Status:     domain.PipelineStatusRunning,
		StartedAt:  pos.OpenedAt.Add(1 * time.Second),
	}
	if err := r.PipelineRun.Create(ctx, run); err != nil {
		t.Fatalf("PipelineRun.Create(): %v", err)
	}

	// Mark the pipeline run as completed.
	completedAt := run.StartedAt.Add(30 * time.Minute)
	buySignal := domain.PipelineSignalBuy
	if err := r.PipelineRun.UpdateStatus(ctx, run.ID, run.TradeDate, repository.PipelineRunStatusUpdate{
		Status:      domain.PipelineStatusCompleted,
		Signal:      &buySignal,
		CompletedAt: &completedAt,
	}); err != nil {
		t.Fatalf("UpdateStatus(): %v", err)
	}

	// 4. Create agent decisions for this run (the 5 reflection roles).
	reflectionRoles := []domain.AgentRole{
		domain.AgentRoleBullResearcher,
		domain.AgentRoleBearResearcher,
		domain.AgentRoleTrader,
		domain.AgentRoleInvestJudge,
		domain.AgentRoleRiskManager,
	}

	for _, role := range reflectionRoles {
		d := &domain.AgentDecision{
			PipelineRunID: run.ID,
			AgentRole:     role,
			Phase:         domain.PhaseTrading,
			OutputText:    string(role) + ": recommends buying AAPL based on technical strength",
		}
		if err := r.AgentDecision.Create(ctx, d); err != nil {
			t.Fatalf("Create() decision for %s: %v", role, err)
		}
	}

	// 5. Close the position (Update persists closed_at and realized_pnl
	//    but not opened_at, which stays at the DB-assigned value).
	closedAt := completedAt.Add(24 * time.Hour)
	pos.RealizedPnL = 75.0
	pos.ClosedAt = &closedAt
	if err := r.Position.Update(ctx, pos); err != nil {
		t.Fatalf("Update() position: %v", err)
	}

	// 6. Run the reflector with a mock LLM.
	mockLLM := &mockLLMProvider{
		response: &llm.CompletionResponse{
			Content: "Always verify bullish signals with volume confirmation before entering a trade.",
		},
	}

	reflector := memory.NewReflector(
		r.Memory, r.PipelineRun, r.AgentDecision, r.Position,
		mockLLM, "test-model", discardLogger(),
	)

	if err := reflector.Reflect(ctx, pos.ID); err != nil {
		t.Fatalf("Reflect(): %v", err)
	}

	// 7. Verify LLM was called 5 times (once per reflection role).
	if mockLLM.calls != 5 {
		t.Fatalf("expected 5 LLM calls, got %d", mockLLM.calls)
	}

	// 8. Verify memories were stored in the database.
	allMemories, err := r.Memory.Search(ctx, "", repository.MemorySearchFilter{}, 20, 0)
	if err != nil {
		t.Fatalf("Search() all memories: %v", err)
	}
	if len(allMemories) != 5 {
		t.Fatalf("expected 5 stored memories, got %d", len(allMemories))
	}

	// 9. Verify each role has a memory.
	seenRoles := make(map[domain.AgentRole]bool)
	for _, m := range allMemories {
		seenRoles[m.AgentRole] = true

		if m.Recommendation != mockLLM.response.Content {
			t.Errorf("memory recommendation mismatch for %s: got %q", m.AgentRole, m.Recommendation)
		}
		if m.PipelineRunID == nil || *m.PipelineRunID != run.ID {
			t.Errorf("memory PipelineRunID mismatch for %s", m.AgentRole)
		}
		if m.Situation == "" {
			t.Errorf("empty situation for %s", m.AgentRole)
		}
		if m.Outcome == "" {
			t.Errorf("empty outcome for %s", m.AgentRole)
		}
	}

	for _, role := range reflectionRoles {
		if !seenRoles[role] {
			t.Errorf("missing memory for role %s", role)
		}
	}

	// 10. Verify FTS retrieval works on stored memories.
	// Reflector stores Situation as "Ticker: AAPL, Side: long, ..." so
	// search for a term that actually appears in the situation_tsv column.
	ftsResults, err := r.Memory.Search(ctx, "AAPL", repository.MemorySearchFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("Search() FTS: %v", err)
	}
	// All 5 memories have "AAPL" in their situation text.
	if len(ftsResults) < 1 {
		t.Fatal("expected at least 1 FTS result for 'AAPL'")
	}
}

// TestIntegration_MemoryReflection_OpenPositionFails verifies reflection
// is rejected for a position that is still open.
func TestIntegration_MemoryReflection_OpenPositionFails(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	strategy := createStrategy(t, ctx, r.Strategy, "Open Pos Test", "TSLA")
	pos := createPosition(t, ctx, r.Position, strategy.ID, "TSLA", domain.PositionSideLong, 5, 200.00)

	mockLLM := &mockLLMProvider{
		response: &llm.CompletionResponse{Content: "unused"},
	}

	reflector := memory.NewReflector(
		r.Memory, r.PipelineRun, r.AgentDecision, r.Position,
		mockLLM, "test-model", discardLogger(),
	)

	err := reflector.Reflect(ctx, pos.ID)
	if err == nil {
		t.Fatal("expected error for open position, got nil")
	}

	if mockLLM.calls != 0 {
		t.Errorf("expected 0 LLM calls for open position, got %d", mockLLM.calls)
	}
}

// TestIntegration_MemoryReflection_SearchAndDelete validates that memories
// stored by the reflector can be searched and deleted.
func TestIntegration_MemoryReflection_SearchAndDelete(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	// Manually create a memory.
	runID := uuid.New()
	mem := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "AAPL displayed a strong bullish reversal with increasing volume",
		Recommendation: "Enter long position with stop below support",
		Outcome:        "profit of 5.00%",
		PipelineRunID:  &runID,
	}
	if err := r.Memory.Create(ctx, mem); err != nil {
		t.Fatalf("Create() memory: %v", err)
	}

	// Search should find it.
	results, err := r.Memory.Search(ctx, "bullish reversal", repository.MemorySearchFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != mem.ID {
		t.Fatalf("expected memory ID %s, got %s", mem.ID, results[0].ID)
	}

	// Delete.
	if err := r.Memory.Delete(ctx, mem.ID); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	// Search should now return empty.
	results, err = r.Memory.Search(ctx, "bullish reversal", repository.MemorySearchFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("Search() after delete: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

// TestIntegration_MemoryReflection_RoleFilter validates that memory search
// correctly filters by agent role.
func TestIntegration_MemoryReflection_RoleFilter(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	// Create memories for different roles.
	mem1 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleTrader,
		Situation:      "Market volatility spike detected",
		Recommendation: "Reduce position sizes",
	}
	mem2 := &domain.AgentMemory{
		AgentRole:      domain.AgentRoleRiskManager,
		Situation:      "Market volatility exceeding thresholds",
		Recommendation: "Tighten stop-losses",
	}

	for _, m := range []*domain.AgentMemory{mem1, mem2} {
		if err := r.Memory.Create(ctx, m); err != nil {
			t.Fatalf("Create(): %v", err)
		}
	}

	// Search with role filter.
	results, err := r.Memory.Search(ctx, "volatility", repository.MemorySearchFilter{
		AgentRole: domain.AgentRoleTrader,
	}, 10, 0)
	if err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with trader role filter, got %d", len(results))
	}
	if results[0].AgentRole != domain.AgentRoleTrader {
		t.Fatalf("expected trader role, got %s", results[0].AgentRole)
	}
}

// TestIntegration_PipelineExecution_PersistRunAndDecisions validates that the
// RepoPersister correctly persists pipeline runs and agent decisions to the
// real database when using the real repository implementations.
func TestIntegration_PipelineExecution_PersistRunAndDecisions(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	// 1. Create a strategy.
	strategy := createStrategy(t, ctx, r.Strategy, "Pipeline Test", "AAPL")

	// 2. Create a pipeline run.
	tradeDate := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	run := &domain.PipelineRun{
		StrategyID:     strategy.ID,
		Ticker:         "AAPL",
		TradeDate:      tradeDate,
		Status:         domain.PipelineStatusRunning,
		Signal:         domain.PipelineSignalHold,
		StartedAt:      tradeDate.Add(9*time.Hour + 30*time.Minute),
		ConfigSnapshot: json.RawMessage(`{"lookback":20,"model":"gpt-4o"}`),
	}

	if err := r.PipelineRun.Create(ctx, run); err != nil {
		t.Fatalf("PipelineRun.Create(): %v", err)
	}

	// 3. Persist multiple agent decisions for different phases.
	decisions := []*domain.AgentDecision{
		{
			PipelineRunID: run.ID,
			AgentRole:     domain.AgentRoleMarketAnalyst,
			Phase:         domain.PhaseAnalysis,
			OutputText:    "AAPL shows bullish momentum with RSI at 65",
			LLMProvider:   "openai",
			LLMModel:      "gpt-4o",
			PromptTokens:  200,
		},
		{
			PipelineRunID: run.ID,
			AgentRole:     domain.AgentRoleBullResearcher,
			Phase:         domain.PhaseResearchDebate,
			RoundNumber:   intPtr(1),
			OutputText:    "Strong earnings support further upside",
		},
		{
			PipelineRunID: run.ID,
			AgentRole:     domain.AgentRoleBearResearcher,
			Phase:         domain.PhaseResearchDebate,
			RoundNumber:   intPtr(1),
			OutputText:    "Valuation is stretched after recent run-up",
		},
		{
			PipelineRunID: run.ID,
			AgentRole:     domain.AgentRoleTrader,
			Phase:         domain.PhaseTrading,
			OutputText:    "Execute buy order for 10 shares at market",
			OutputStructured: json.RawMessage(`{"signal":"buy","quantity":10}`),
		},
	}

	for _, d := range decisions {
		if err := r.AgentDecision.Create(ctx, d); err != nil {
			t.Fatalf("AgentDecision.Create(): %v", err)
		}
	}

	// 4. Complete the pipeline run.
	completedAt := run.StartedAt.Add(5 * time.Minute)
	buySignal := domain.PipelineSignalBuy
	if err := r.PipelineRun.UpdateStatus(ctx, run.ID, run.TradeDate, repository.PipelineRunStatusUpdate{
		Status:      domain.PipelineStatusCompleted,
		Signal:      &buySignal,
		CompletedAt: &completedAt,
	}); err != nil {
		t.Fatalf("UpdateStatus(): %v", err)
	}

	// 5. Verify the pipeline run is persisted and completed.
	gotRun, err := r.PipelineRun.Get(ctx, run.ID, run.TradeDate)
	if err != nil {
		t.Fatalf("PipelineRun.Get(): %v", err)
	}
	if gotRun.Status != domain.PipelineStatusCompleted {
		t.Fatalf("expected completed, got %q", gotRun.Status)
	}
	if gotRun.Signal != domain.PipelineSignalBuy {
		t.Fatalf("expected buy signal, got %q", gotRun.Signal)
	}
	if gotRun.CompletedAt == nil || !gotRun.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected CompletedAt=%v, got %v", completedAt, gotRun.CompletedAt)
	}

	// 6. Verify all 4 decisions are retrievable.
	allDecisions, err := r.AgentDecision.GetByRun(ctx, run.ID, repository.AgentDecisionFilter{}, 20, 0)
	if err != nil {
		t.Fatalf("GetByRun(): %v", err)
	}
	if len(allDecisions) != 4 {
		t.Fatalf("expected 4 decisions, got %d", len(allDecisions))
	}

	// 7. Verify filtering by phase.
	researchDecisions, err := r.AgentDecision.GetByRun(ctx, run.ID, repository.AgentDecisionFilter{
		Phase: domain.PhaseResearchDebate,
	}, 20, 0)
	if err != nil {
		t.Fatalf("GetByRun() research: %v", err)
	}
	if len(researchDecisions) != 2 {
		t.Fatalf("expected 2 research decisions, got %d", len(researchDecisions))
	}

	// 8. Verify filtering by strategy.
	stratRuns, err := r.PipelineRun.List(ctx, repository.PipelineRunFilter{
		StrategyID: &strategy.ID,
	}, 10, 0)
	if err != nil {
		t.Fatalf("PipelineRun.List(): %v", err)
	}
	if len(stratRuns) != 1 {
		t.Fatalf("expected 1 run for strategy, got %d", len(stratRuns))
	}
}

// TestIntegration_PipelineExecution_FailedRun validates that a failed pipeline
// run is correctly persisted with error message.
func TestIntegration_PipelineExecution_FailedRun(t *testing.T) {
	db := newTestDB(t)
	r := newRepos(db)
	ctx := context.Background()

	strategy := createStrategy(t, ctx, r.Strategy, "Fail Test", "MSFT")
	tradeDate := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	run := createPipelineRun(t, ctx, r.PipelineRun, strategy.ID, "MSFT", tradeDate)

	// Mark as failed.
	completedAt := run.StartedAt.Add(2 * time.Minute)
	if err := r.PipelineRun.UpdateStatus(ctx, run.ID, run.TradeDate, repository.PipelineRunStatusUpdate{
		Status:       domain.PipelineStatusFailed,
		CompletedAt:  &completedAt,
		ErrorMessage: "LLM provider timeout after 30s",
	}); err != nil {
		t.Fatalf("UpdateStatus(): %v", err)
	}

	got, err := r.PipelineRun.Get(ctx, run.ID, run.TradeDate)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Status != domain.PipelineStatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}
	if got.ErrorMessage != "LLM provider timeout after 30s" {
		t.Fatalf("expected error message, got %q", got.ErrorMessage)
	}
}

func intPtr(n int) *int {
	return &n
}
