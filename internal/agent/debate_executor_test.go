package agent

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// stubNode is a simple Node implementation that records Execute calls.
type stubNode struct {
	name     string
	role     AgentRole
	phase    Phase
	execFn   func(context.Context, *PipelineState) error
	executed bool
}

func (n *stubNode) Name() string    { return n.name }
func (n *stubNode) Role() AgentRole { return n.role }
func (n *stubNode) Phase() Phase    { return n.phase }
func (n *stubNode) Execute(ctx context.Context, state *PipelineState) error {
	n.executed = true
	if n.execFn != nil {
		return n.execFn(ctx, state)
	}
	return nil
}

// stubDebaterNode implements both Node and DebaterNode for debate testing.
type stubDebaterNode struct {
	name     string
	role     AgentRole
	phase    Phase
	debateFn func(context.Context, DebateInput) (DebateOutput, error)
	debated  bool
}

func (n *stubDebaterNode) Name() string    { return n.name }
func (n *stubDebaterNode) Role() AgentRole { return n.role }
func (n *stubDebaterNode) Phase() Phase    { return n.phase }
func (n *stubDebaterNode) Execute(ctx context.Context, state *PipelineState) error {
	return nil
}

func (n *stubDebaterNode) Debate(ctx context.Context, input DebateInput) (DebateOutput, error) {
	n.debated = true
	if n.debateFn != nil {
		return n.debateFn(ctx, input)
	}
	return DebateOutput{Contribution: n.name + "-contrib"}, nil
}

// noopDecisionPayload is a DecisionPayload function that always returns empty.
func noopDecisionPayload(_ *PipelineState, _ Node, _ *int) (string, *DecisionLLMResponse, error) {
	return "", nil, nil
}

func testDebateContext(persister DecisionPersister, events chan<- PipelineEvent) DebateContext {
	return DebateContext{
		Helper:          newPhaseHelper(persister, events, slog.Default(), time.Now),
		Persister:       persister,
		Events:          events,
		Logger:          slog.Default(),
		NowFunc:         time.Now,
		DecisionPayload: noopDecisionPayload,
	}
}

func TestDebateExecutor_SingleRound_TwoDebaters(t *testing.T) {
	t.Parallel()
	persister := NoopPersister{}

	debater1 := &stubDebaterNode{name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate}
	debater2 := &stubDebaterNode{name: "bear", role: AgentRoleBearResearcher, phase: PhaseResearchDebate}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	executor := NewDebateExecutor(testDebateContext(persister, nil), DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   1,
		Debaters: []Node{debater1, debater2},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	err := executor.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !debater1.debated {
		t.Error("debater1 was not called")
	}
	if !debater2.debated {
		t.Error("debater2 was not called")
	}
	if !judge.executed {
		t.Error("judge was not called")
	}
	if len(state.ResearchDebate.Rounds) != 1 {
		t.Errorf("rounds = %d, want 1", len(state.ResearchDebate.Rounds))
	}
}

func TestDebateExecutor_MultipleRounds(t *testing.T) {
	t.Parallel()
	persister := NoopPersister{}

	debateCount := 0
	debater := &stubDebaterNode{
		name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate,
		debateFn: func(_ context.Context, _ DebateInput) (DebateOutput, error) {
			debateCount++
			return DebateOutput{Contribution: "round-contrib"}, nil
		},
	}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	executor := NewDebateExecutor(testDebateContext(persister, nil), DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   3,
		Debaters: []Node{debater},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	err := executor.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if debateCount != 3 {
		t.Errorf("debate calls = %d, want 3", debateCount)
	}
	if len(state.ResearchDebate.Rounds) != 3 {
		t.Errorf("rounds = %d, want 3", len(state.ResearchDebate.Rounds))
	}
}

func TestDebateExecutor_RoundClampedToOne(t *testing.T) {
	t.Parallel()
	persister := NoopPersister{}

	debater := &stubDebaterNode{name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	executor := NewDebateExecutor(testDebateContext(persister, nil), DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   0, // Should be clamped to 1.
		Debaters: []Node{debater},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	err := executor.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !debater.debated {
		t.Error("debater was not called after round clamping")
	}
	if len(state.ResearchDebate.Rounds) != 1 {
		t.Errorf("rounds = %d, want 1 (clamped)", len(state.ResearchDebate.Rounds))
	}
}

func TestDebateExecutor_ContextCancellation(t *testing.T) {
	t.Parallel()
	persister := NoopPersister{}

	debater := &stubDebaterNode{name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before execution.

	executor := NewDebateExecutor(testDebateContext(persister, nil), DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   3,
		Debaters: []Node{debater},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	err := executor.Execute(ctx, state)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestDebateExecutor_EventChannelFull_NoBlock(t *testing.T) {
	t.Parallel()
	persister := NoopPersister{}
	// Unbuffered channel — will be full immediately.
	events := make(chan PipelineEvent)

	debater := &stubDebaterNode{name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	executor := NewDebateExecutor(testDebateContext(persister, events), DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   1,
		Debaters: []Node{debater},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	// This should not deadlock even though events channel is full.
	done := make(chan error, 1)
	go func() {
		done <- executor.Execute(context.Background(), state)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute() deadlocked on full events channel")
	}
}

func TestDebateExecutor_NilEventsChannel_NoPanic(t *testing.T) {
	t.Parallel()
	persister := NoopPersister{}

	debater := &stubDebaterNode{name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	state := &PipelineState{
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	executor := NewDebateExecutor(testDebateContext(persister, nil), DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   1,
		Debaters: []Node{debater},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	err := executor.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestDebateExecutor_DecisionsPersistedPerDebater(t *testing.T) {
	t.Parallel()
	persister := newRunnerSpyPersister()

	debater1 := &stubDebaterNode{name: "bull", role: AgentRoleBullResearcher, phase: PhaseResearchDebate}
	debater2 := &stubDebaterNode{name: "bear", role: AgentRoleBearResearcher, phase: PhaseResearchDebate}
	judge := &stubNode{name: "judge", role: AgentRoleInvestJudge, phase: PhaseResearchDebate}

	runID := uuid.New()
	state := &PipelineState{
		PipelineRunID: runID,
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		mu:            &sync.Mutex{},
	}

	executor := NewDebateExecutor(DebateContext{
		Helper:          newPhaseHelper(persister, nil, slog.Default(), time.Now),
		Persister:       persister,
		Logger:          slog.Default(),
		NowFunc:         time.Now,
		DecisionPayload: noopDecisionPayload,
	}, DebateConfig{
		Phase:    PhaseResearchDebate,
		Rounds:   1,
		Debaters: []Node{debater1, debater2},
		Judge:    judge,
		AppendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})

	err := executor.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// 2 debaters + 1 judge = 3 decisions.
	count := persister.decisionCount(runID)
	if count != 3 {
		t.Errorf("persisted decisions = %d, want 3", count)
	}
}
