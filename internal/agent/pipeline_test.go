package agent

import (
	"context"
	"errors"
	"fmt"
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

// mockDebateNode is a test double for a PhaseResearchDebate Node.
type mockDebateNode struct {
	name    string
	role    AgentRole
	execute func(ctx context.Context, state *PipelineState) error
}

func (m *mockDebateNode) Name() string    { return m.name }
func (m *mockDebateNode) Role() AgentRole { return m.role }
func (m *mockDebateNode) Phase() Phase    { return PhaseResearchDebate }
func (m *mockDebateNode) Execute(ctx context.Context, state *PipelineState) error {
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

// TestExecuteResearchDebatePhase_RoundsExecuteInOrder verifies that
// executeResearchDebatePhase runs 3 rounds sequentially (bull, bear per round)
// followed by the InvestJudge, and emits a DebateRoundCompleted event per round.
func TestExecuteResearchDebatePhase_RoundsExecuteInOrder(t *testing.T) {
	runID := uuid.New()
	stratID := uuid.New()

	var order []string

	bullNode := &mockDebateNode{
		name: "bull_researcher",
		role: AgentRoleBullResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "bull")
			idx := len(state.ResearchDebate.Rounds) - 1
			state.ResearchDebate.Rounds[idx].Contributions[AgentRoleBullResearcher] = "bull argument"
			return nil
		},
	}

	bearNode := &mockDebateNode{
		name: "bear_researcher",
		role: AgentRoleBearResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "bear")
			idx := len(state.ResearchDebate.Rounds) - 1
			state.ResearchDebate.Rounds[idx].Contributions[AgentRoleBearResearcher] = "bear argument"
			return nil
		},
	}

	judgeNode := &mockDebateNode{
		name: "invest_judge",
		role: AgentRoleInvestJudge,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "judge")
			state.ResearchDebate.InvestmentPlan = "accumulate"
			return nil
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{ResearchDebateRounds: 3},
		nil, nil, events, slog.Default(),
	)
	pipeline.RegisterNode(bullNode)
	pipeline.RegisterNode(bearNode)
	pipeline.RegisterNode(judgeNode)

	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    stratID,
		Ticker:        "AAPL",
	}

	err := pipeline.executeResearchDebatePhase(context.Background(), state)
	if err != nil {
		t.Fatalf("executeResearchDebatePhase() error = %v, want nil", err)
	}

	// Verify execution order: bull, bear, bull, bear, bull, bear, judge.
	wantOrder := []string{"bull", "bear", "bull", "bear", "bull", "bear", "judge"}
	if len(order) != len(wantOrder) {
		t.Fatalf("got %d executions, want %d: %v", len(order), len(wantOrder), order)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Errorf("execution[%d] = %q, want %q", i, order[i], wantOrder[i])
		}
	}

	// Verify 3 DebateRoundCompleted events with correct metadata.
	close(events)
	var emitted []PipelineEvent
	for e := range events {
		emitted = append(emitted, e)
	}
	if len(emitted) != 3 {
		t.Fatalf("got %d events, want 3", len(emitted))
	}
	for i, e := range emitted {
		if e.Type != DebateRoundCompleted {
			t.Errorf("event[%d].Type = %q, want %q", i, e.Type, DebateRoundCompleted)
		}
		if e.Round != i+1 {
			t.Errorf("event[%d].Round = %d, want %d", i, e.Round, i+1)
		}
		if e.Phase != PhaseResearchDebate {
			t.Errorf("event[%d].Phase = %q, want %q", i, e.Phase, PhaseResearchDebate)
		}
		if e.PipelineRunID != runID {
			t.Errorf("event[%d].PipelineRunID = %v, want %v", i, e.PipelineRunID, runID)
		}
		if e.StrategyID != stratID {
			t.Errorf("event[%d].StrategyID = %v, want %v", i, e.StrategyID, stratID)
		}
		if e.OccurredAt.IsZero() {
			t.Errorf("event[%d].OccurredAt is zero", i)
		}
	}

	// The investment plan must be set by the judge.
	if state.ResearchDebate.InvestmentPlan != "accumulate" {
		t.Errorf("InvestmentPlan = %q, want %q", state.ResearchDebate.InvestmentPlan, "accumulate")
	}
}

// TestExecuteResearchDebatePhase_RoundContextAccumulates verifies that each
// round's nodes can read state accumulated from previous rounds, and that the
// judge can read all rounds when producing the investment plan.
func TestExecuteResearchDebatePhase_RoundContextAccumulates(t *testing.T) {
	bullNode := &mockDebateNode{
		name: "bull_researcher",
		role: AgentRoleBullResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.ResearchDebate.Rounds) - 1
			round := &state.ResearchDebate.Rounds[idx]
			round.Contributions[AgentRoleBullResearcher] = fmt.Sprintf("bull_r%d", round.Number)
			return nil
		},
	}

	bearNode := &mockDebateNode{
		name: "bear_researcher",
		role: AgentRoleBearResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.ResearchDebate.Rounds) - 1
			round := &state.ResearchDebate.Rounds[idx]
			// Bear reads the current round's bull contribution to prove ordering.
			bullContrib := round.Contributions[AgentRoleBullResearcher]
			// Bear also reads the number of prior completed rounds.
			priorRounds := len(state.ResearchDebate.Rounds) - 1
			round.Contributions[AgentRoleBearResearcher] = fmt.Sprintf(
				"bear_r%d(rebutting:%s,prior:%d)", round.Number, bullContrib, priorRounds,
			)
			return nil
		},
	}

	judgeNode := &mockDebateNode{
		name: "invest_judge",
		role: AgentRoleInvestJudge,
		execute: func(_ context.Context, state *PipelineState) error {
			state.ResearchDebate.InvestmentPlan = fmt.Sprintf(
				"plan based on %d rounds", len(state.ResearchDebate.Rounds),
			)
			return nil
		},
	}

	pipeline := NewPipeline(
		PipelineConfig{ResearchDebateRounds: 3},
		nil, nil, make(chan PipelineEvent, 10), slog.Default(),
	)
	pipeline.RegisterNode(bullNode)
	pipeline.RegisterNode(bearNode)
	pipeline.RegisterNode(judgeNode)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
	}

	if err := pipeline.executeResearchDebatePhase(context.Background(), state); err != nil {
		t.Fatalf("executeResearchDebatePhase() error = %v, want nil", err)
	}

	// Verify 3 rounds accumulated in state.
	if got := len(state.ResearchDebate.Rounds); got != 3 {
		t.Fatalf("got %d rounds, want 3", got)
	}

	// Each round must have the expected contributions.
	for i, round := range state.ResearchDebate.Rounds {
		roundNum := i + 1
		if round.Number != roundNum {
			t.Errorf("round[%d].Number = %d, want %d", i, round.Number, roundNum)
		}

		wantBull := fmt.Sprintf("bull_r%d", roundNum)
		if got := round.Contributions[AgentRoleBullResearcher]; got != wantBull {
			t.Errorf("round[%d] bull = %q, want %q", i, got, wantBull)
		}

		wantBear := fmt.Sprintf("bear_r%d(rebutting:%s,prior:%d)", roundNum, wantBull, i)
		if got := round.Contributions[AgentRoleBearResearcher]; got != wantBear {
			t.Errorf("round[%d] bear = %q, want %q", i, got, wantBear)
		}
	}

	// Judge must have produced a plan referencing all 3 rounds.
	wantPlan := "plan based on 3 rounds"
	if state.ResearchDebate.InvestmentPlan != wantPlan {
		t.Errorf("InvestmentPlan = %q, want %q", state.ResearchDebate.InvestmentPlan, wantPlan)
	}
}

// TestExecuteResearchDebatePhase_CancellationStopsCleanly verifies that
// cancelling the parent context mid-debate stops execution and returns the
// context error without running subsequent rounds or the judge.
func TestExecuteResearchDebatePhase_CancellationStopsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var executionLog []string

	bullNode := &mockDebateNode{
		name: "bull_researcher",
		role: AgentRoleBullResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.ResearchDebate.Rounds) - 1
			executionLog = append(executionLog, fmt.Sprintf("bull_%d", state.ResearchDebate.Rounds[idx].Number))
			state.ResearchDebate.Rounds[idx].Contributions[AgentRoleBullResearcher] = "bull"
			return nil
		},
	}

	bearNode := &mockDebateNode{
		name: "bear_researcher",
		role: AgentRoleBearResearcher,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.ResearchDebate.Rounds) - 1
			executionLog = append(executionLog, fmt.Sprintf("bear_%d", state.ResearchDebate.Rounds[idx].Number))
			state.ResearchDebate.Rounds[idx].Contributions[AgentRoleBearResearcher] = "bear"
			// Cancel after round 2 completes.
			if state.ResearchDebate.Rounds[idx].Number == 2 {
				cancel()
			}
			return nil
		},
	}

	judgeNode := &mockDebateNode{
		name: "invest_judge",
		role: AgentRoleInvestJudge,
		execute: func(_ context.Context, _ *PipelineState) error {
			executionLog = append(executionLog, "judge")
			return nil
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{ResearchDebateRounds: 5}, // more rounds than will execute
		nil, nil, events, slog.Default(),
	)
	pipeline.RegisterNode(bullNode)
	pipeline.RegisterNode(bearNode)
	pipeline.RegisterNode(judgeNode)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
	}

	err := pipeline.executeResearchDebatePhase(ctx, state)

	// Must return context.Canceled.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	// Only rounds 1 and 2 should have executed; the judge must not run.
	wantLog := []string{"bull_1", "bear_1", "bull_2", "bear_2"}
	if len(executionLog) != len(wantLog) {
		t.Fatalf("execution log = %v, want %v", executionLog, wantLog)
	}
	for i := range wantLog {
		if executionLog[i] != wantLog[i] {
			t.Errorf("executionLog[%d] = %q, want %q", i, executionLog[i], wantLog[i])
		}
	}

	// Only 2 complete rounds should be in state.
	if got := len(state.ResearchDebate.Rounds); got != 2 {
		t.Errorf("got %d rounds in state, want 2", got)
	}

	// Events: round 1's event is always emitted (context still active).
	// Round 2's event is non-deterministic because the context cancellation
	// in bear races with the channel send in the select statement.
	close(events)
	var emitted int
	for range events {
		emitted++
	}
	if emitted < 1 || emitted > 2 {
		t.Errorf("got %d events, want 1 or 2", emitted)
	}
}

// ---------------------------------------------------------------------------
// executeTradingPhase tests
// ---------------------------------------------------------------------------

// mockTradingNode is a test double for a PhaseTrading Node.
type mockTradingNode struct {
	name    string
	role    AgentRole
	execute func(ctx context.Context, state *PipelineState) error
}

func (m *mockTradingNode) Name() string    { return m.name }
func (m *mockTradingNode) Role() AgentRole { return m.role }
func (m *mockTradingNode) Phase() Phase    { return PhaseTrading }
func (m *mockTradingNode) Execute(ctx context.Context, state *PipelineState) error {
	return m.execute(ctx, state)
}

// TestExecuteTradingPhase_Success verifies that executeTradingPhase executes
// the Trader node, updates PipelineState, and emits an AgentDecisionMade event.
func TestExecuteTradingPhase_Success(t *testing.T) {
	runID := uuid.New()
	stratID := uuid.New()

	traderNode := &mockTradingNode{
		name: "trader",
		role: AgentRoleTrader,
		execute: func(_ context.Context, state *PipelineState) error {
			state.TradingPlan = TradingPlan{
				Action:     PipelineSignalBuy,
				Ticker:     state.Ticker,
				EntryPrice: 150.0,
				Confidence: 0.85,
				Rationale:  "strong momentum",
			}
			return nil
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{},
		nil, nil, events, slog.Default(),
	)
	pipeline.RegisterNode(traderNode)

	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    stratID,
		Ticker:        "AAPL",
	}

	err := pipeline.executeTradingPhase(context.Background(), state)
	if err != nil {
		t.Fatalf("executeTradingPhase() error = %v, want nil", err)
	}

	// Verify the trading plan was populated.
	if state.TradingPlan.Action != PipelineSignalBuy {
		t.Errorf("TradingPlan.Action = %q, want %q", state.TradingPlan.Action, PipelineSignalBuy)
	}
	if state.TradingPlan.Ticker != "AAPL" {
		t.Errorf("TradingPlan.Ticker = %q, want %q", state.TradingPlan.Ticker, "AAPL")
	}
	if state.TradingPlan.EntryPrice != 150.0 {
		t.Errorf("TradingPlan.EntryPrice = %v, want 150.0", state.TradingPlan.EntryPrice)
	}
	if state.TradingPlan.Confidence != 0.85 {
		t.Errorf("TradingPlan.Confidence = %v, want 0.85", state.TradingPlan.Confidence)
	}

	// Exactly one AgentDecisionMade event must be emitted.
	close(events)
	var emittedEvents []PipelineEvent
	for e := range events {
		emittedEvents = append(emittedEvents, e)
	}
	if len(emittedEvents) != 1 {
		t.Fatalf("got %d events, want 1", len(emittedEvents))
	}

	e := emittedEvents[0]
	if e.Type != AgentDecisionMade {
		t.Errorf("event Type = %q, want %q", e.Type, AgentDecisionMade)
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
	if e.AgentRole != AgentRoleTrader {
		t.Errorf("event AgentRole = %q, want %q", e.AgentRole, AgentRoleTrader)
	}
	if e.Phase != PhaseTrading {
		t.Errorf("event Phase = %q, want %q", e.Phase, PhaseTrading)
	}
	if e.OccurredAt.IsZero() {
		t.Error("event OccurredAt is zero")
	}
}

// TestExecuteTradingPhase_NoTraderNode verifies that executeTradingPhase
// returns an error when no Trader node is registered.
func TestExecuteTradingPhase_NoTraderNode(t *testing.T) {
	pipeline := NewPipeline(
		PipelineConfig{},
		nil, nil, make(chan PipelineEvent, 10), slog.Default(),
	)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
	}

	err := pipeline.executeTradingPhase(context.Background(), state)
	if err == nil {
		t.Fatal("executeTradingPhase() error = nil, want non-nil")
	}

	wantSubstr := "trading phase requires a trader node"
	if got := err.Error(); !contains(got, wantSubstr) {
		t.Errorf("error = %q, want substring %q", got, wantSubstr)
	}
}

// TestExecuteTradingPhase_ExecutionError verifies that executeTradingPhase
// propagates errors from the Trader node and does not emit an event.
func TestExecuteTradingPhase_ExecutionError(t *testing.T) {
	traderNode := &mockTradingNode{
		name: "trader",
		role: AgentRoleTrader,
		execute: func(_ context.Context, _ *PipelineState) error {
			return errors.New("simulated trader failure")
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{},
		nil, nil, events, slog.Default(),
	)
	pipeline.RegisterNode(traderNode)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
	}

	err := pipeline.executeTradingPhase(context.Background(), state)
	if err == nil {
		t.Fatal("executeTradingPhase() error = nil, want non-nil")
	}
	if got := err.Error(); got != "simulated trader failure" {
		t.Errorf("error = %q, want %q", got, "simulated trader failure")
	}

	// No events should be emitted on failure.
	close(events)
	var count int
	for range events {
		count++
	}
	if count != 0 {
		t.Errorf("got %d events, want 0", count)
	}
}

// TestExecuteTradingPhase_ContextCancellation verifies that
// executeTradingPhase respects context cancellation and returns the context error.
func TestExecuteTradingPhase_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	traderNode := &mockTradingNode{
		name: "trader",
		role: AgentRoleTrader,
		execute: func(ctx context.Context, _ *PipelineState) error {
			return ctx.Err()
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{},
		nil, nil, events, slog.Default(),
	)
	pipeline.RegisterNode(traderNode)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
	}

	err := pipeline.executeTradingPhase(ctx, state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	// No events should be emitted on cancellation.
	close(events)
	var count int
	for range events {
		count++
	}
	if count != 0 {
		t.Errorf("got %d events, want 0", count)
	}
}

// contains reports whether s contains substr (simple helper avoiding strings import).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}