package risk

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// mockProvider is a test double for llm.Provider.
type mockProvider struct {
	response *llm.CompletionResponse
	err      error
	calls    atomic.Int32
	lastReq  llm.CompletionRequest
}

func (m *mockProvider) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.calls.Add(1)
	m.lastReq = req
	return m.response, m.err
}

func TestNewAggressiveRiskNilLogger(t *testing.T) {
	a := NewAggressiveRisk(nil, "openai", "model", nil)
	if a == nil {
		t.Fatal("NewAggressiveRisk() returned nil")
	}
}

func TestAggressiveRiskNodeInterface(t *testing.T) {
	a := NewAggressiveRisk(nil, "openai", "model", slog.Default())

	if got := a.Name(); got != "aggressive_analyst" {
		t.Fatalf("Name() = %q, want %q", got, "aggressive_analyst")
	}
	if got := a.Role(); got != agent.AgentRoleAggressiveAnalyst {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleAggressiveAnalyst)
	}
	if got := a.Phase(); got != agent.PhaseRiskDebate {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseRiskDebate)
	}
}

func TestAggressiveRiskExecuteStoresContributionAndDecision(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Position size should be increased to capture the full upside.",
			Usage: llm.CompletionUsage{
				PromptTokens:     150,
				CompletionTokens: 60,
			},
		},
	}

	a := NewAggressiveRisk(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "TSLA",
		TradingPlan: agent.TradingPlan{
			Action:       agent.PipelineSignalBuy,
			Ticker:       "TSLA",
			EntryPrice:   250.00,
			PositionSize: 100,
			StopLoss:     240.00,
			TakeProfit:   280.00,
			Confidence:   0.8,
			RiskReward:   3.0,
			Rationale:    "Strong momentum with breakout pattern.",
		},
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{
					Number:        1,
					Contributions: make(map[agent.AgentRole]string),
				},
			},
		},
	}

	if err := a.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify contribution was stored in the current round.
	got := state.RiskDebate.Rounds[0].Contributions[agent.AgentRoleAggressiveAnalyst]
	want := "Position size should be increased to capture the full upside."
	if got != want {
		t.Fatalf("contribution = %q, want %q", got, want)
	}

	// Verify that RecordDecision was called (decision is retrievable).
	roundNumber := 1
	decision, ok := state.Decision(agent.AgentRoleAggressiveAnalyst, agent.PhaseRiskDebate, &roundNumber)
	if !ok {
		t.Fatal("Decision() not found for aggressive_analyst")
	}
	if decision.OutputText != want {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, want)
	}
	if decision.LLMResponse == nil || decision.LLMResponse.Response == nil {
		t.Fatal("decision LLM response is nil")
	}
	if decision.LLMResponse.Response.Usage.PromptTokens != 150 {
		t.Fatalf("prompt tokens = %d, want 150", decision.LLMResponse.Response.Usage.PromptTokens)
	}
	if decision.LLMResponse.Response.Usage.CompletionTokens != 60 {
		t.Fatalf("completion tokens = %d, want 60", decision.LLMResponse.Response.Usage.CompletionTokens)
	}
	if decision.LLMResponse.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", decision.LLMResponse.Provider, "test-provider")
	}
	if decision.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("model in response = %q, want %q", decision.LLMResponse.Response.Model, "test-model")
	}

	// Verify the system prompt was the aggressive risk prompt.
	if mock.lastReq.Messages[0].Content != AggressiveRiskSystemPrompt {
		t.Fatalf("system prompt mismatch:\ngot:  %q\nwant: %q", mock.lastReq.Messages[0].Content, AggressiveRiskSystemPrompt)
	}

	// Verify the model was forwarded.
	if mock.lastReq.Model != "test-model" {
		t.Fatalf("model = %q, want %q", mock.lastReq.Model, "test-model")
	}

	// Verify the trading plan is included in the user message context.
	userMsg := mock.lastReq.Messages[1].Content
	if len(userMsg) == 0 {
		t.Fatal("user message is empty")
	}
	// The trading plan should be serialised as JSON under the trader role.
	if !strings.Contains(userMsg, "trader") {
		t.Fatalf("user message should reference trader role, got: %q", userMsg)
	}
}

func TestAggressiveRiskExecuteNilProvider(t *testing.T) {
	a := NewAggressiveRisk(nil, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := a.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "aggressive_analyst (risk_debate): nil llm provider"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestAggressiveRiskExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("service unavailable"),
	}

	a := NewAggressiveRisk(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := a.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "aggressive_analyst (risk_debate): llm completion failed: service unavailable"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}

	// Verify no contribution was stored on error.
	if got := state.RiskDebate.Rounds[0].Contributions[agent.AgentRoleAggressiveAnalyst]; got != "" {
		t.Fatalf("contribution should be empty on error, got %q", got)
	}
}

func TestAggressiveRiskExecuteNoRounds(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Aggressive case without rounds.",
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}

	a := NewAggressiveRisk(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		RiskDebate: agent.RiskDebateState{},
	}

	// Execute should succeed even with no rounds; it calls the LLM but
	// does not store a contribution or decision since there is no round.
	if err := a.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// No decision should be recorded when there are no rounds.
	roundNumber := 0
	if _, ok := state.Decision(agent.AgentRoleAggressiveAnalyst, agent.PhaseRiskDebate, &roundNumber); ok {
		t.Fatal("Decision() should not be recorded when no rounds exist (round 0)")
	}

	// Also ensure no decision is recorded under a nil round key.
	if _, ok := state.Decision(agent.AgentRoleAggressiveAnalyst, agent.PhaseRiskDebate, nil); ok {
		t.Fatal("Decision() should not be recorded when no rounds exist (nil round)")
	}
}

// Verify AggressiveRisk satisfies the agent.Node interface at compile time.
var _ agent.Node = (*AggressiveRisk)(nil)
