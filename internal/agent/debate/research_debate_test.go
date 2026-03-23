package debate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

// stubNode is a minimal agent.Node that records execution order via a shared
// slice and optionally mutates state through a callback.
type stubNode struct {
	name    string
	role    agent.AgentRole
	phase   agent.Phase
	execFn  func(ctx context.Context, state *agent.PipelineState) error
	callLog *[]string
}

func (s *stubNode) Name() string         { return s.name }
func (s *stubNode) Role() agent.AgentRole { return s.role }
func (s *stubNode) Phase() agent.Phase    { return s.phase }

func (s *stubNode) Execute(ctx context.Context, state *agent.PipelineState) error {
	if s.callLog != nil {
		*s.callLog = append(*s.callLog, s.name)
	}
	if s.execFn != nil {
		return s.execFn(ctx, state)
	}
	return nil
}

// newBullStub returns a stub that writes a contribution into the current round.
func newBullStub(callLog *[]string) *stubNode {
	return &stubNode{
		name:    "bull_researcher",
		role:    agent.AgentRoleBullResearcher,
		phase:   agent.PhaseResearchDebate,
		callLog: callLog,
		execFn: func(_ context.Context, state *agent.PipelineState) error {
			rounds := state.ResearchDebate.Rounds
			if len(rounds) == 0 {
				return nil
			}
			cur := &state.ResearchDebate.Rounds[len(rounds)-1]
			if cur.Contributions == nil {
				cur.Contributions = make(map[agent.AgentRole]string)
			}
			cur.Contributions[agent.AgentRoleBullResearcher] = fmt.Sprintf("bull_r%d", cur.Number)
			return nil
		},
	}
}

// newBearStub returns a stub that writes a contribution into the current round.
func newBearStub(callLog *[]string) *stubNode {
	return &stubNode{
		name:    "bear_researcher",
		role:    agent.AgentRoleBearResearcher,
		phase:   agent.PhaseResearchDebate,
		callLog: callLog,
		execFn: func(_ context.Context, state *agent.PipelineState) error {
			rounds := state.ResearchDebate.Rounds
			if len(rounds) == 0 {
				return nil
			}
			cur := &state.ResearchDebate.Rounds[len(rounds)-1]
			if cur.Contributions == nil {
				cur.Contributions = make(map[agent.AgentRole]string)
			}
			cur.Contributions[agent.AgentRoleBearResearcher] = fmt.Sprintf("bear_r%d", cur.Number)
			return nil
		},
	}
}

// newManagerStub returns a stub that sets the InvestmentPlan.
func newManagerStub(callLog *[]string) *stubNode {
	return &stubNode{
		name:    "research_manager",
		role:    agent.AgentRoleInvestJudge,
		phase:   agent.PhaseResearchDebate,
		callLog: callLog,
		execFn: func(_ context.Context, state *agent.PipelineState) error {
			state.ResearchDebate.InvestmentPlan = `{"direction":"buy","conviction":7}`
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestResearchDebateNodeInterface(t *testing.T) {
	callLog := []string{}
	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		newManagerStub(&callLog),
		3,
		slog.Default(),
	)

	if got := rd.Name(); got != "research_debate" {
		t.Fatalf("Name() = %q, want %q", got, "research_debate")
	}
	if got := rd.Role(); got != agent.AgentRoleInvestJudge {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleInvestJudge)
	}
	if got := rd.Phase(); got != agent.PhaseResearchDebate {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseResearchDebate)
	}
}

// TestResearchDebateThreeRoundsOrder verifies that 3 rounds execute bull, then
// bear in sequence, followed by the manager at the end.
func TestResearchDebateThreeRoundsOrder(t *testing.T) {
	callLog := []string{}
	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		newManagerStub(&callLog),
		3,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	if err := rd.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	wantOrder := []string{
		"bull_researcher", "bear_researcher", // round 1
		"bull_researcher", "bear_researcher", // round 2
		"bull_researcher", "bear_researcher", // round 3
		"research_manager", // judge
	}

	if len(callLog) != len(wantOrder) {
		t.Fatalf("call count = %d, want %d; log = %v", len(callLog), len(wantOrder), callLog)
	}
	for i, want := range wantOrder {
		if callLog[i] != want {
			t.Fatalf("call[%d] = %q, want %q; full log = %v", i, callLog[i], want, callLog)
		}
	}
}

// TestResearchDebateStateAccumulation verifies that rounds and contributions
// are correctly accumulated in PipelineState after a 3-round debate.
func TestResearchDebateStateAccumulation(t *testing.T) {
	callLog := []string{}
	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		newManagerStub(&callLog),
		3,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	if err := rd.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify 3 rounds were accumulated.
	if got := len(state.ResearchDebate.Rounds); got != 3 {
		t.Fatalf("rounds count = %d, want 3", got)
	}

	for i, round := range state.ResearchDebate.Rounds {
		wantNum := i + 1
		if round.Number != wantNum {
			t.Fatalf("round[%d].Number = %d, want %d", i, round.Number, wantNum)
		}

		wantBull := fmt.Sprintf("bull_r%d", wantNum)
		if got := round.Contributions[agent.AgentRoleBullResearcher]; got != wantBull {
			t.Fatalf("round[%d] bull = %q, want %q", i, got, wantBull)
		}

		wantBear := fmt.Sprintf("bear_r%d", wantNum)
		if got := round.Contributions[agent.AgentRoleBearResearcher]; got != wantBear {
			t.Fatalf("round[%d] bear = %q, want %q", i, got, wantBear)
		}
	}

	// Verify investment plan was set.
	if state.ResearchDebate.InvestmentPlan == "" {
		t.Fatal("InvestmentPlan is empty, want non-empty")
	}
}

// TestResearchDebateSingleRound verifies that a single-round debate works
// correctly (rounds=1).
func TestResearchDebateSingleRound(t *testing.T) {
	callLog := []string{}
	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		newManagerStub(&callLog),
		1,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	if err := rd.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	wantOrder := []string{"bull_researcher", "bear_researcher", "research_manager"}
	if len(callLog) != len(wantOrder) {
		t.Fatalf("call count = %d, want %d; log = %v", len(callLog), len(wantOrder), callLog)
	}
	for i, want := range wantOrder {
		if callLog[i] != want {
			t.Fatalf("call[%d] = %q, want %q; full log = %v", i, callLog[i], want, callLog)
		}
	}

	if got := len(state.ResearchDebate.Rounds); got != 1 {
		t.Fatalf("rounds count = %d, want 1", got)
	}

	round := state.ResearchDebate.Rounds[0]
	if round.Number != 1 {
		t.Fatalf("round.Number = %d, want 1", round.Number)
	}
	if got := round.Contributions[agent.AgentRoleBullResearcher]; got != "bull_r1" {
		t.Fatalf("bull contribution = %q, want %q", got, "bull_r1")
	}
	if got := round.Contributions[agent.AgentRoleBearResearcher]; got != "bear_r1" {
		t.Fatalf("bear contribution = %q, want %q", got, "bear_r1")
	}
	if state.ResearchDebate.InvestmentPlan == "" {
		t.Fatal("InvestmentPlan is empty, want non-empty")
	}
}

// TestResearchDebateRoundsClampedToOne verifies that rounds < 1 are clamped
// to 1 and the debate still executes correctly.
func TestResearchDebateRoundsClampedToOne(t *testing.T) {
	callLog := []string{}
	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		newManagerStub(&callLog),
		0,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	if err := rd.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if got := len(state.ResearchDebate.Rounds); got != 1 {
		t.Fatalf("rounds count = %d, want 1", got)
	}
	if got := len(callLog); got != 3 {
		t.Fatalf("call count = %d, want 3; log = %v", got, callLog)
	}
}

// TestResearchDebateBullError verifies that an error from the bull node stops
// execution and propagates the error.
func TestResearchDebateBullError(t *testing.T) {
	callLog := []string{}
	bullErr := errors.New("bull failure")
	bull := &stubNode{
		name:    "bull_researcher",
		role:    agent.AgentRoleBullResearcher,
		phase:   agent.PhaseResearchDebate,
		callLog: &callLog,
		execFn:  func(_ context.Context, _ *agent.PipelineState) error { return bullErr },
	}

	rd := NewResearchDebate(
		bull,
		newBearStub(&callLog),
		newManagerStub(&callLog),
		2,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	err := rd.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !errors.Is(err, bullErr) {
		t.Fatalf("error = %v, want wrapped %v", err, bullErr)
	}

	// Bear and manager should not have been called.
	if len(callLog) != 1 {
		t.Fatalf("call count = %d, want 1; log = %v", len(callLog), callLog)
	}
}

// TestResearchDebateBearError verifies that an error from the bear node stops
// execution after bull runs.
func TestResearchDebateBearError(t *testing.T) {
	callLog := []string{}
	bearErr := errors.New("bear failure")
	bear := &stubNode{
		name:    "bear_researcher",
		role:    agent.AgentRoleBearResearcher,
		phase:   agent.PhaseResearchDebate,
		callLog: &callLog,
		execFn:  func(_ context.Context, _ *agent.PipelineState) error { return bearErr },
	}

	rd := NewResearchDebate(
		newBullStub(&callLog),
		bear,
		newManagerStub(&callLog),
		2,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	err := rd.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !errors.Is(err, bearErr) {
		t.Fatalf("error = %v, want wrapped %v", err, bearErr)
	}

	// Bull should have run, bear should have been called (and failed), manager should not.
	if len(callLog) != 2 {
		t.Fatalf("call count = %d, want 2; log = %v", len(callLog), callLog)
	}
}

// TestResearchDebateManagerError verifies that an error from the manager node
// is propagated after all rounds complete.
func TestResearchDebateManagerError(t *testing.T) {
	callLog := []string{}
	mgrErr := errors.New("manager failure")
	mgr := &stubNode{
		name:    "research_manager",
		role:    agent.AgentRoleInvestJudge,
		phase:   agent.PhaseResearchDebate,
		callLog: &callLog,
		execFn:  func(_ context.Context, _ *agent.PipelineState) error { return mgrErr },
	}

	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		mgr,
		1,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	err := rd.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !errors.Is(err, mgrErr) {
		t.Fatalf("error = %v, want wrapped %v", err, mgrErr)
	}

	// Bull and bear should have run for 1 round, then manager should have been called.
	wantCalls := []string{"bull_researcher", "bear_researcher", "research_manager"}
	if len(callLog) != len(wantCalls) {
		t.Fatalf("call count = %d, want %d; log = %v", len(callLog), len(wantCalls), callLog)
	}
}

// TestResearchDebateContextCanceled verifies that a canceled context stops
// execution before starting the next round.
func TestResearchDebateContextCanceled(t *testing.T) {
	callLog := []string{}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after first round completes.
	bull := &stubNode{
		name:    "bull_researcher",
		role:    agent.AgentRoleBullResearcher,
		phase:   agent.PhaseResearchDebate,
		callLog: &callLog,
		execFn: func(_ context.Context, state *agent.PipelineState) error {
			rounds := state.ResearchDebate.Rounds
			if len(rounds) > 0 {
				cur := &state.ResearchDebate.Rounds[len(rounds)-1]
				if cur.Contributions == nil {
					cur.Contributions = make(map[agent.AgentRole]string)
				}
				cur.Contributions[agent.AgentRoleBullResearcher] = fmt.Sprintf("bull_r%d", cur.Number)
				// Cancel after completing the first round's bull call.
				if cur.Number == 1 {
					cancel()
				}
			}
			return nil
		},
	}

	rd := NewResearchDebate(
		bull,
		newBearStub(&callLog),
		newManagerStub(&callLog),
		3,
		slog.Default(),
	)

	state := &agent.PipelineState{}

	err := rd.Execute(ctx, state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	// Round 1 should complete (bull + bear), then context check before round 2 should fail.
	if got := len(state.ResearchDebate.Rounds); got != 1 {
		t.Fatalf("rounds = %d, want 1 (context should stop before round 2)", got)
	}
}

// TestNewResearchDebateNilLogger verifies that a nil logger defaults to
// slog.Default() without panicking.
func TestNewResearchDebateNilLogger(t *testing.T) {
	callLog := []string{}
	rd := NewResearchDebate(
		newBullStub(&callLog),
		newBearStub(&callLog),
		newManagerStub(&callLog),
		1,
		nil,
	)
	if rd == nil {
		t.Fatal("NewResearchDebate() returned nil")
	}
}

// Verify ResearchDebate satisfies the agent.Node interface at compile time.
var _ agent.Node = (*ResearchDebate)(nil)
