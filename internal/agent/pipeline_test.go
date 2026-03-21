package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockAnalystNode is a test double for a PhaseAnalysis Node.
type mockAnalystNode struct {
	name    string
	role    AgentRole
	execute func(ctx context.Context, state *PipelineState) error
}

func (m *mockAnalystNode) Name() string    { return m.name }
func (m *mockAnalystNode) Role() AgentRole { return m.role }
func (m *mockAnalystNode) Phase() Phase    { return PhaseAnalysis }
func (m *mockAnalystNode) Execute(ctx context.Context, state *PipelineState) error {
	return m.execute(ctx, state)
}

// TestExecuteAnalysisPhase verifies that executeAnalysisPhase:
//   - Runs all PhaseAnalysis nodes concurrently.
//   - Does not abort the phase when one node fails (partial-failure tolerance).
//   - Emits an AgentDecisionMade event for each successfully completed node.
//   - Cancels slow nodes when the phase timeout fires.
func TestExecuteAnalysisPhase(t *testing.T) {
	runID := uuid.New()
	stratID := uuid.New()

	var slowCancelled atomic.Bool

	// Node 1: succeeds immediately, writes its report.
	node1 := &mockAnalystNode{
		name: "market_analyst",
		role: AgentRoleMarketAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			state.SetAnalystReport(AgentRoleMarketAnalyst, "bullish trend")
			return nil
		},
	}

	// Node 2: succeeds immediately, writes its report.
	node2 := &mockAnalystNode{
		name: "bull_researcher",
		role: AgentRoleBullResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			state.SetAnalystReport(AgentRoleBullResearcher, "strong momentum")
			return nil
		},
	}

	// Node 3: slow – blocks indefinitely until its context is cancelled by the timeout.
	node3 := &mockAnalystNode{
		name: "bear_researcher",
		role: AgentRoleBearResearcher,
		execute: func(ctx context.Context, _ *PipelineState) error {
			select {
			case <-ctx.Done():
				slowCancelled.Store(true)
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		},
	}

	// Node 4: fails immediately with a non-context error.
	node4 := &mockAnalystNode{
		name: "risk_manager",
		role: AgentRoleRiskManager,
		execute: func(_ context.Context, _ *PipelineState) error {
			return errors.New("simulated analyst failure")
		},
	}

	const phaseTimeout = 200 * time.Millisecond
	events := make(chan PipelineEvent, 10)

	pipeline := NewPipeline(
		PipelineConfig{PhaseTimeout: phaseTimeout},
		nil, // pipelineRunRepo not required for this unit test
		nil, // agentDecisionRepo not required for this unit test
		events,
		slog.Default(),
	)
	pipeline.RegisterNode(node1)
	pipeline.RegisterNode(node2)
	pipeline.RegisterNode(node3)
	pipeline.RegisterNode(node4)

	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    stratID,
		Ticker:        "AAPL",
	}

	start := time.Now()
	err := pipeline.executeAnalysisPhase(context.Background(), state)
	elapsed := time.Since(start)

	// The phase must return nil even though two nodes did not complete successfully.
	if err != nil {
		t.Fatalf("executeAnalysisPhase() error = %v, want nil", err)
	}

	// The phase must complete within a reasonable bound of the timeout.
	// Allow up to 2.5x the configured timeout to account for goroutine scheduling overhead.
	const maxElapsed = phaseTimeout * 5 / 2
	if elapsed > maxElapsed {
		t.Fatalf("executeAnalysisPhase() took %v, want < %v", elapsed, maxElapsed)
	}

	// Successful nodes must have written their reports to shared state.
	if got := state.AnalystReports[AgentRoleMarketAnalyst]; got != "bullish trend" {
		t.Errorf("AnalystReports[market_analyst] = %q, want %q", got, "bullish trend")
	}
	if got := state.AnalystReports[AgentRoleBullResearcher]; got != "strong momentum" {
		t.Errorf("AnalystReports[bull_researcher] = %q, want %q", got, "strong momentum")
	}

	// The slow node must have had its context cancelled by the phase timeout.
	if !slowCancelled.Load() {
		t.Error("slow node context was not cancelled by phase timeout")
	}

	// Exactly two AgentDecisionMade events must be emitted (one per successful node).
	close(events)
	var emitted []PipelineEvent
	for e := range events {
		emitted = append(emitted, e)
	}
	if len(emitted) != 2 {
		t.Fatalf("got %d AgentDecisionMade events, want 2", len(emitted))
	}
	for _, e := range emitted {
		if e.Type != AgentDecisionMade {
			t.Errorf("event type = %q, want %q", e.Type, AgentDecisionMade)
		}
		if e.PipelineRunID != runID {
			t.Errorf("event PipelineRunID = %v, want %v", e.PipelineRunID, runID)
		}
		if e.StrategyID != stratID {
			t.Errorf("event StrategyID = %v, want %v", e.StrategyID, stratID)
		}
		if e.Ticker != "AAPL" {
			t.Errorf("event Ticker = %q, want %q", e.Ticker, "AAPL")
		}
		if e.Phase != PhaseAnalysis {
			t.Errorf("event Phase = %q, want %q", e.Phase, PhaseAnalysis)
		}
		if e.OccurredAt.IsZero() {
			t.Error("event OccurredAt is zero")
		}
	}
}
