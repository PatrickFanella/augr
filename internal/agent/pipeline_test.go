package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
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
	if got := err.Error(); !strings.Contains(got, wantSubstr) {
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

// ---------------------------------------------------------------------------
// executeRiskDebatePhase tests
// ---------------------------------------------------------------------------

// mockRiskDebateNode is a test double for a PhaseRiskDebate Node.
type mockRiskDebateNode struct {
	name    string
	role    AgentRole
	execute func(ctx context.Context, state *PipelineState) error
}

func (m *mockRiskDebateNode) Name() string    { return m.name }
func (m *mockRiskDebateNode) Role() AgentRole { return m.role }
func (m *mockRiskDebateNode) Phase() Phase    { return PhaseRiskDebate }
func (m *mockRiskDebateNode) Execute(ctx context.Context, state *PipelineState) error {
	return m.execute(ctx, state)
}

// TestExecuteRiskDebatePhase_RoundsExecuteInOrder verifies that
// executeRiskDebatePhase runs N rounds sequentially (aggressive, conservative,
// neutral per round) followed by the RiskManager, and emits a
// DebateRoundCompleted event per round.
func TestExecuteRiskDebatePhase_RoundsExecuteInOrder(t *testing.T) {
	runID := uuid.New()
	stratID := uuid.New()

	var order []string

	aggressiveNode := &mockRiskDebateNode{
		name: "aggressive_analyst",
		role: AgentRoleAggressiveAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "aggressive")
			idx := len(state.RiskDebate.Rounds) - 1
			state.RiskDebate.Rounds[idx].Contributions[AgentRoleAggressiveAnalyst] = "aggressive argument"
			return nil
		},
	}

	conservativeNode := &mockRiskDebateNode{
		name: "conservative_analyst",
		role: AgentRoleConservativeAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "conservative")
			idx := len(state.RiskDebate.Rounds) - 1
			state.RiskDebate.Rounds[idx].Contributions[AgentRoleConservativeAnalyst] = "conservative argument"
			return nil
		},
	}

	neutralNode := &mockRiskDebateNode{
		name: "neutral_analyst",
		role: AgentRoleNeutralAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "neutral")
			idx := len(state.RiskDebate.Rounds) - 1
			state.RiskDebate.Rounds[idx].Contributions[AgentRoleNeutralAnalyst] = "neutral argument"
			return nil
		},
	}

	riskManagerNode := &mockRiskDebateNode{
		name: "risk_manager",
		role: AgentRoleRiskManager,
		execute: func(_ context.Context, state *PipelineState) error {
			order = append(order, "risk_manager")
			state.RiskDebate.FinalSignal = "approve with reduced size"
			return nil
		},
	}

	events := make(chan PipelineEvent, 10)
	pipeline := NewPipeline(
		PipelineConfig{RiskDebateRounds: 3},
		nil, nil, events, slog.Default(),
	)
	pipeline.RegisterNode(aggressiveNode)
	pipeline.RegisterNode(conservativeNode)
	pipeline.RegisterNode(neutralNode)
	pipeline.RegisterNode(riskManagerNode)

	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    stratID,
		Ticker:        "AAPL",
	}

	err := pipeline.executeRiskDebatePhase(context.Background(), state)
	if err != nil {
		t.Fatalf("executeRiskDebatePhase() error = %v, want nil", err)
	}

	// Verify execution order: aggressive, conservative, neutral x3 rounds, then risk_manager.
	wantOrder := []string{
		"aggressive", "conservative", "neutral",
		"aggressive", "conservative", "neutral",
		"aggressive", "conservative", "neutral",
		"risk_manager",
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("got %d executions, want %d: %v", len(order), len(wantOrder), order)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Errorf("execution[%d] = %q, want %q", i, order[i], wantOrder[i])
		}
	}

	// Verify 3 rounds accumulated in state.
	if got := len(state.RiskDebate.Rounds); got != 3 {
		t.Fatalf("got %d rounds, want 3", got)
	}
	for i, round := range state.RiskDebate.Rounds {
		if round.Number != i+1 {
			t.Errorf("round[%d].Number = %d, want %d", i, round.Number, i+1)
		}
		if got := round.Contributions[AgentRoleAggressiveAnalyst]; got != "aggressive argument" {
			t.Errorf("round[%d] aggressive = %q, want %q", i, got, "aggressive argument")
		}
		if got := round.Contributions[AgentRoleConservativeAnalyst]; got != "conservative argument" {
			t.Errorf("round[%d] conservative = %q, want %q", i, got, "conservative argument")
		}
		if got := round.Contributions[AgentRoleNeutralAnalyst]; got != "neutral argument" {
			t.Errorf("round[%d] neutral = %q, want %q", i, got, "neutral argument")
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
		if e.Phase != PhaseRiskDebate {
			t.Errorf("event[%d].Phase = %q, want %q", i, e.Phase, PhaseRiskDebate)
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

	// The final signal must be set by the risk manager.
	if state.RiskDebate.FinalSignal != "approve with reduced size" {
		t.Errorf("RiskDebate.FinalSignal = %q, want %q", state.RiskDebate.FinalSignal, "approve with reduced size")
	}
}

// TestExecuteRiskDebatePhase_FinalSignalExtractedFromRiskManager verifies that
// the RiskManager node populates the RiskDebate.FinalSignal field and that
// state accumulated across rounds is available to the RiskManager.
func TestExecuteRiskDebatePhase_FinalSignalExtractedFromRiskManager(t *testing.T) {
	aggressiveNode := &mockRiskDebateNode{
		name: "aggressive_analyst",
		role: AgentRoleAggressiveAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.RiskDebate.Rounds) - 1
			round := &state.RiskDebate.Rounds[idx]
			round.Contributions[AgentRoleAggressiveAnalyst] = fmt.Sprintf("aggressive_r%d", round.Number)
			return nil
		},
	}

	conservativeNode := &mockRiskDebateNode{
		name: "conservative_analyst",
		role: AgentRoleConservativeAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.RiskDebate.Rounds) - 1
			round := &state.RiskDebate.Rounds[idx]
			round.Contributions[AgentRoleConservativeAnalyst] = fmt.Sprintf("conservative_r%d", round.Number)
			return nil
		},
	}

	neutralNode := &mockRiskDebateNode{
		name: "neutral_analyst",
		role: AgentRoleNeutralAnalyst,
		execute: func(_ context.Context, state *PipelineState) error {
			idx := len(state.RiskDebate.Rounds) - 1
			round := &state.RiskDebate.Rounds[idx]
			round.Contributions[AgentRoleNeutralAnalyst] = fmt.Sprintf("neutral_r%d", round.Number)
			return nil
		},
	}

	riskManagerNode := &mockRiskDebateNode{
		name: "risk_manager",
		role: AgentRoleRiskManager,
		execute: func(_ context.Context, state *PipelineState) error {
			// The risk manager reads all rounds and produces a final signal.
			state.RiskDebate.FinalSignal = fmt.Sprintf(
				"final verdict based on %d rounds", len(state.RiskDebate.Rounds),
			)
			return nil
		},
	}

	pipeline := NewPipeline(
		PipelineConfig{RiskDebateRounds: 2},
		nil, nil, make(chan PipelineEvent, 10), slog.Default(),
	)
	pipeline.RegisterNode(aggressiveNode)
	pipeline.RegisterNode(conservativeNode)
	pipeline.RegisterNode(neutralNode)
	pipeline.RegisterNode(riskManagerNode)

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "TSLA",
	}

	if err := pipeline.executeRiskDebatePhase(context.Background(), state); err != nil {
		t.Fatalf("executeRiskDebatePhase() error = %v, want nil", err)
	}

	// Verify 2 rounds accumulated in state.
	if got := len(state.RiskDebate.Rounds); got != 2 {
		t.Fatalf("got %d rounds, want 2", got)
	}

	// Each round must have contributions from all three analysts.
	for i, round := range state.RiskDebate.Rounds {
		roundNum := i + 1
		if round.Number != roundNum {
			t.Errorf("round[%d].Number = %d, want %d", i, round.Number, roundNum)
		}
		wantAggressive := fmt.Sprintf("aggressive_r%d", roundNum)
		if got := round.Contributions[AgentRoleAggressiveAnalyst]; got != wantAggressive {
			t.Errorf("round[%d] aggressive = %q, want %q", i, got, wantAggressive)
		}
		wantConservative := fmt.Sprintf("conservative_r%d", roundNum)
		if got := round.Contributions[AgentRoleConservativeAnalyst]; got != wantConservative {
			t.Errorf("round[%d] conservative = %q, want %q", i, got, wantConservative)
		}
		wantNeutral := fmt.Sprintf("neutral_r%d", roundNum)
		if got := round.Contributions[AgentRoleNeutralAnalyst]; got != wantNeutral {
			t.Errorf("round[%d] neutral = %q, want %q", i, got, wantNeutral)
		}
	}

	// Risk manager must have produced a final signal referencing all 2 rounds.
	wantSignal := "final verdict based on 2 rounds"
	if state.RiskDebate.FinalSignal != wantSignal {
		t.Errorf("RiskDebate.FinalSignal = %q, want %q", state.RiskDebate.FinalSignal, wantSignal)
	}
}

// ---------------------------------------------------------------------------
// Execute (top-level) tests
// ---------------------------------------------------------------------------

// mockPipelineRunRepo is a test double for repository.PipelineRunRepository.
type mockPipelineRunRepo struct {
	createFn       func(ctx context.Context, run *domain.PipelineRun) error
	updateStatusFn func(ctx context.Context, id uuid.UUID, tradeDate time.Time, update repository.PipelineRunStatusUpdate) error
}

func (m *mockPipelineRunRepo) Create(ctx context.Context, run *domain.PipelineRun) error {
	if m.createFn != nil {
		return m.createFn(ctx, run)
	}
	return nil
}

func (m *mockPipelineRunRepo) Get(_ context.Context, _ uuid.UUID, _ time.Time) (*domain.PipelineRun, error) {
	return nil, nil
}

func (m *mockPipelineRunRepo) List(_ context.Context, _ repository.PipelineRunFilter, _, _ int) ([]domain.PipelineRun, error) {
	return nil, nil
}

func (m *mockPipelineRunRepo) UpdateStatus(ctx context.Context, id uuid.UUID, tradeDate time.Time, update repository.PipelineRunStatusUpdate) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, id, tradeDate, update)
	}
	return nil
}

// mockPhaseNode is a flexible test double that can represent any phase/role combination.
type mockPhaseNode struct {
	name    string
	role    AgentRole
	phase   Phase
	execute func(ctx context.Context, state *PipelineState) error
}

func (m *mockPhaseNode) Name() string    { return m.name }
func (m *mockPhaseNode) Role() AgentRole { return m.role }
func (m *mockPhaseNode) Phase() Phase    { return m.phase }
func (m *mockPhaseNode) Execute(ctx context.Context, state *PipelineState) error {
	return m.execute(ctx, state)
}

// registerAllPhaseNodes registers the minimal set of nodes required by all four
// phases. The supplied executionLog slice, if non-nil, records the order of
// phase executions. Callers can override individual node behaviours via the
// optional overrides map keyed by AgentRole.
func registerAllPhaseNodes(
	p *Pipeline,
	executionLog *[]string,
	overrides map[AgentRole]func(context.Context, *PipelineState) error,
) {
	mkExec := func(role AgentRole, phaseName string) func(context.Context, *PipelineState) error {
		if overrides != nil {
			if fn, ok := overrides[role]; ok {
				return func(ctx context.Context, state *PipelineState) error {
					if executionLog != nil {
						*executionLog = append(*executionLog, phaseName)
					}
					return fn(ctx, state)
				}
			}
		}
		return func(_ context.Context, _ *PipelineState) error {
			if executionLog != nil {
				*executionLog = append(*executionLog, phaseName)
			}
			return nil
		}
	}

	// Analysis phase — one analyst node.
	p.RegisterNode(&mockPhaseNode{
		name: "market_analyst", role: AgentRoleMarketAnalyst, phase: PhaseAnalysis,
		execute: mkExec(AgentRoleMarketAnalyst, "analysis"),
	})

	// Research debate phase.
	p.RegisterNode(&mockPhaseNode{
		name: "bull_researcher", role: AgentRoleBullResearcher, phase: PhaseResearchDebate,
		execute: func(ctx context.Context, state *PipelineState) error {
			fn := mkExec(AgentRoleBullResearcher, "research_debate")
			idx := len(state.ResearchDebate.Rounds) - 1
			state.ResearchDebate.Rounds[idx].Contributions[AgentRoleBullResearcher] = "bull"
			return fn(ctx, state)
		},
	})
	p.RegisterNode(&mockPhaseNode{
		name: "bear_researcher", role: AgentRoleBearResearcher, phase: PhaseResearchDebate,
		execute: func(ctx context.Context, state *PipelineState) error {
			fn := mkExec(AgentRoleBearResearcher, "research_debate")
			idx := len(state.ResearchDebate.Rounds) - 1
			state.ResearchDebate.Rounds[idx].Contributions[AgentRoleBearResearcher] = "bear"
			return fn(ctx, state)
		},
	})
	p.RegisterNode(&mockPhaseNode{
		name: "invest_judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate,
		execute: func(ctx context.Context, state *PipelineState) error {
			fn := mkExec(AgentRoleInvestJudge, "research_debate")
			state.ResearchDebate.InvestmentPlan = "accumulate"
			return fn(ctx, state)
		},
	})

	// Trading phase.
	p.RegisterNode(&mockPhaseNode{
		name: "trader", role: AgentRoleTrader, phase: PhaseTrading,
		execute: func(ctx context.Context, state *PipelineState) error {
			fn := mkExec(AgentRoleTrader, "trading")
			state.TradingPlan = TradingPlan{Action: PipelineSignalBuy, Ticker: state.Ticker}
			return fn(ctx, state)
		},
	})

	// Risk debate phase.
	riskNoop := func(role AgentRole) func(context.Context, *PipelineState) error {
		return func(ctx context.Context, state *PipelineState) error {
			if overrides != nil {
				if fn, ok := overrides[role]; ok {
					return fn(ctx, state)
				}
			}
			idx := len(state.RiskDebate.Rounds) - 1
			state.RiskDebate.Rounds[idx].Contributions[role] = string(role)
			return nil
		}
	}
	p.RegisterNode(&mockPhaseNode{
		name: "aggressive_analyst", role: AgentRoleAggressiveAnalyst, phase: PhaseRiskDebate,
		execute: riskNoop(AgentRoleAggressiveAnalyst),
	})
	p.RegisterNode(&mockPhaseNode{
		name: "conservative_analyst", role: AgentRoleConservativeAnalyst, phase: PhaseRiskDebate,
		execute: riskNoop(AgentRoleConservativeAnalyst),
	})
	p.RegisterNode(&mockPhaseNode{
		name: "neutral_analyst", role: AgentRoleNeutralAnalyst, phase: PhaseRiskDebate,
		execute: riskNoop(AgentRoleNeutralAnalyst),
	})
	p.RegisterNode(&mockPhaseNode{
		name: "risk_manager", role: AgentRoleRiskManager, phase: PhaseRiskDebate,
		execute: func(ctx context.Context, state *PipelineState) error {
			fn := mkExec(AgentRoleRiskManager, "risk_debate")
			state.RiskDebate.FinalSignal = "approved"
			return fn(ctx, state)
		},
	})
}

// TestExecute_HappyPath verifies that Execute runs all four phases in order,
// creates a PipelineRun with status running, updates it to completed, and emits
// PipelineStarted and PipelineCompleted events.
func TestExecute_HappyPath(t *testing.T) {
	stratID := uuid.New()

	var createdRun *domain.PipelineRun
	var updatedStatus domain.PipelineStatus

	repo := &mockPipelineRunRepo{
		createFn: func(_ context.Context, run *domain.PipelineRun) error {
			createdRun = run
			return nil
		},
		updateStatusFn: func(_ context.Context, _ uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
			updatedStatus = update.Status
			return nil
		},
	}

	events := make(chan PipelineEvent, 50)
	var phaseLog []string
	pipeline := NewPipeline(
		PipelineConfig{ResearchDebateRounds: 1, RiskDebateRounds: 1},
		repo, nil, events, slog.Default(),
	)
	registerAllPhaseNodes(pipeline, &phaseLog, nil)

	state, err := pipeline.Execute(context.Background(), stratID, "AAPL")
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify PipelineRun was created with running status.
	if createdRun == nil {
		t.Fatal("PipelineRun was not created")
	}
	if createdRun.Status != domain.PipelineStatusRunning {
		t.Errorf("PipelineRun.Status = %q, want %q", createdRun.Status, domain.PipelineStatusRunning)
	}
	if createdRun.StrategyID != stratID {
		t.Errorf("PipelineRun.StrategyID = %v, want %v", createdRun.StrategyID, stratID)
	}
	if createdRun.Ticker != "AAPL" {
		t.Errorf("PipelineRun.Ticker = %q, want %q", createdRun.Ticker, "AAPL")
	}

	// Verify status updated to completed.
	if updatedStatus != domain.PipelineStatusCompleted {
		t.Errorf("updated status = %q, want %q", updatedStatus, domain.PipelineStatusCompleted)
	}

	// Verify all 4 phases executed in order.
	wantPhases := []string{"analysis", "research_debate", "trading", "risk_debate"}
	if len(phaseLog) < len(wantPhases) {
		t.Fatalf("phase log = %v, want at least %v", phaseLog, wantPhases)
	}
	// Check the first occurrence of each phase appears in order.
	seen := map[string]int{}
	for i, p := range phaseLog {
		if _, ok := seen[p]; !ok {
			seen[p] = i
		}
	}
	for i := 1; i < len(wantPhases); i++ {
		prev := wantPhases[i-1]
		curr := wantPhases[i]
		if seen[curr] <= seen[prev] {
			t.Errorf("phase %q (idx %d) should execute after %q (idx %d)", curr, seen[curr], prev, seen[prev])
		}
	}

	// Verify state is populated.
	if state == nil {
		t.Fatal("Execute() returned nil state")
	}
	if state.StrategyID != stratID {
		t.Errorf("state.StrategyID = %v, want %v", state.StrategyID, stratID)
	}
	if state.Ticker != "AAPL" {
		t.Errorf("state.Ticker = %q, want %q", state.Ticker, "AAPL")
	}

	// Verify events: first must be PipelineStarted, last must be PipelineCompleted.
	close(events)
	var emitted []PipelineEvent
	for e := range events {
		emitted = append(emitted, e)
	}
	if len(emitted) < 2 {
		t.Fatalf("got %d events, want at least 2", len(emitted))
	}
	if emitted[0].Type != PipelineStarted {
		t.Errorf("first event type = %q, want %q", emitted[0].Type, PipelineStarted)
	}
	if emitted[len(emitted)-1].Type != PipelineCompleted {
		t.Errorf("last event type = %q, want %q", emitted[len(emitted)-1].Type, PipelineCompleted)
	}
}

// TestExecute_PhaseFailureUpdatesRunStatus verifies that when a phase fails,
// the PipelineRun status is updated to failed with the error message, and a
// PipelineError event is emitted.
func TestExecute_PhaseFailureUpdatesRunStatus(t *testing.T) {
	stratID := uuid.New()

	var updatedStatus domain.PipelineStatus
	var updatedErrMsg string

	repo := &mockPipelineRunRepo{
		updateStatusFn: func(_ context.Context, _ uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
			updatedStatus = update.Status
			updatedErrMsg = update.ErrorMessage
			return nil
		},
	}

	events := make(chan PipelineEvent, 50)
	pipeline := NewPipeline(
		PipelineConfig{ResearchDebateRounds: 1, RiskDebateRounds: 1},
		repo, nil, events, slog.Default(),
	)

	tradeErr := errors.New("simulated trading failure")

	registerAllPhaseNodes(pipeline, nil, map[AgentRole]func(context.Context, *PipelineState) error{
		AgentRoleTrader: func(_ context.Context, _ *PipelineState) error {
			return tradeErr
		},
	})

	state, err := pipeline.Execute(context.Background(), stratID, "AAPL")
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !errors.Is(err, tradeErr) {
		t.Errorf("error = %v, want %v", err, tradeErr)
	}

	// State must still be returned (partial).
	if state == nil {
		t.Fatal("Execute() returned nil state on failure")
	}

	// Status must be failed with error message.
	if updatedStatus != domain.PipelineStatusFailed {
		t.Errorf("updated status = %q, want %q", updatedStatus, domain.PipelineStatusFailed)
	}
	if !strings.Contains(updatedErrMsg, "simulated trading failure") {
		t.Errorf("error message = %q, want substring %q", updatedErrMsg, "simulated trading failure")
	}

	// PipelineError event must be emitted.
	close(events)
	var errorEvents []PipelineEvent
	for e := range events {
		if e.Type == PipelineError {
			errorEvents = append(errorEvents, e)
		}
	}
	if len(errorEvents) != 1 {
		t.Fatalf("got %d PipelineError events, want 1", len(errorEvents))
	}
	if !strings.Contains(errorEvents[0].Error, "simulated trading failure") {
		t.Errorf("PipelineError.Error = %q, want substring %q", errorEvents[0].Error, "simulated trading failure")
	}
}

// TestExecute_ContextCancellationStopsExecution verifies that cancelling the
// parent context stops pipeline execution and updates the run status to failed.
func TestExecute_ContextCancellationStopsExecution(t *testing.T) {
	stratID := uuid.New()

	var updatedStatus domain.PipelineStatus

	repo := &mockPipelineRunRepo{
		updateStatusFn: func(_ context.Context, _ uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
			updatedStatus = update.Status
			return nil
		},
	}

	events := make(chan PipelineEvent, 50)
	pipeline := NewPipeline(
		PipelineConfig{ResearchDebateRounds: 1, RiskDebateRounds: 1},
		repo, nil, events, slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context when the analysis phase runs.
	registerAllPhaseNodes(pipeline, nil, map[AgentRole]func(context.Context, *PipelineState) error{
		AgentRoleMarketAnalyst: func(_ context.Context, _ *PipelineState) error {
			cancel()
			return context.Canceled
		},
	})

	_, err := pipeline.Execute(ctx, stratID, "AAPL")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	// Status must be failed.
	if updatedStatus != domain.PipelineStatusFailed {
		t.Errorf("updated status = %q, want %q", updatedStatus, domain.PipelineStatusFailed)
	}
}

// TestExecute_PipelineTimeoutTriggersCancellation verifies that the
// pipeline-level timeout from config cancels execution.
func TestExecute_PipelineTimeoutTriggersCancellation(t *testing.T) {
	stratID := uuid.New()

	var updatedStatus domain.PipelineStatus

	repo := &mockPipelineRunRepo{
		updateStatusFn: func(_ context.Context, _ uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
			updatedStatus = update.Status
			return nil
		},
	}

	events := make(chan PipelineEvent, 50)
	pipeline := NewPipeline(
		PipelineConfig{
			PipelineTimeout:      100 * time.Millisecond,
			ResearchDebateRounds: 1,
			RiskDebateRounds:     1,
		},
		repo, nil, events, slog.Default(),
	)

	// The analysis phase will block until the pipeline timeout fires.
	registerAllPhaseNodes(pipeline, nil, map[AgentRole]func(context.Context, *PipelineState) error{
		AgentRoleMarketAnalyst: func(ctx context.Context, _ *PipelineState) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
	})

	start := time.Now()
	_, err := pipeline.Execute(context.Background(), stratID, "AAPL")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}

	// Must complete within a reasonable bound of the timeout.
	const maxElapsed = 2 * time.Second
	if elapsed > maxElapsed {
		t.Errorf("Execute() took %v, want < %v", elapsed, maxElapsed)
	}

	// Status must be failed.
	if updatedStatus != domain.PipelineStatusFailed {
		t.Errorf("updated status = %q, want %q", updatedStatus, domain.PipelineStatusFailed)
	}
}
