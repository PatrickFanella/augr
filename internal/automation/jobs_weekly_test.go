package automation

import "testing"

func TestStrategyTournamentDescriptionMatchesReadOnlyBehavior(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.registerWeeklyJobs()

	status := singleJobStatus(t, orch, "strategy_tournament")
	if status.Description != "Rank active strategies and recommend review candidates" {
		t.Fatalf("strategy_tournament description = %q", status.Description)
	}
}

func TestUniverseRefreshCompletionErrorRejectsEmptyProviderResult(t *testing.T) {
	t.Parallel()

	if err := universeRefreshCompletionError(1); err != nil {
		t.Fatalf("universeRefreshCompletionError(1) = %v, want nil", err)
	}
	if err := universeRefreshCompletionError(0); err == nil {
		t.Fatal("universeRefreshCompletionError(0) = nil, want error")
	}
}
