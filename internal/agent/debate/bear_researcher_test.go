package debate

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestNewBearResearcherNilLogger(t *testing.T) {
	bear := NewBearResearcher(nil, "openai", "model", nil)
	if bear == nil {
		t.Fatal("NewBearResearcher() returned nil")
	}
}

func TestBearResearcherNodeInterface(t *testing.T) {
	bear := NewBearResearcher(nil, "openai", "model", slog.Default())

	if got := bear.Name(); got != "bear_researcher" {
		t.Fatalf("Name() = %q, want %q", got, "bear_researcher")
	}
	if got := bear.Role(); got != agent.AgentRoleBearResearcher {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleBearResearcher)
	}
	if got := bear.Phase(); got != agent.PhaseResearchDebate {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseResearchDebate)
	}
}

func TestBearResearcherExecuteStoresContributionAndDecision(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Margins are compressing and revenue growth is decelerating.",
			Usage: llm.CompletionUsage{
				PromptTokens:     120,
				CompletionTokens: 45,
			},
		},
	}

	bear := NewBearResearcher(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		AnalystReports: map[agent.AgentRole]string{
			agent.AgentRoleMarketAnalyst: "Trend is bullish.",
			agent.AgentRoleNewsAnalyst:   "Mixed sentiment.",
		},
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleBullResearcher: "Revenue growth is strong.",
					},
				},
			},
		},
	}

	if err := bear.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify contribution was stored in the current round.
	got := state.ResearchDebate.Rounds[0].Contributions[agent.AgentRoleBearResearcher]
	want := "Margins are compressing and revenue growth is decelerating."
	if got != want {
		t.Fatalf("contribution = %q, want %q", got, want)
	}

	// Verify that RecordDecision was called (decision is retrievable).
	roundNumber := 1
	decision, ok := state.Decision(agent.AgentRoleBearResearcher, agent.PhaseResearchDebate, &roundNumber)
	if !ok {
		t.Fatal("Decision() not found for bear_researcher")
	}
	if decision.OutputText != want {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, want)
	}
	if decision.LLMResponse == nil || decision.LLMResponse.Response == nil {
		t.Fatal("decision LLM response is nil")
	}
	if decision.LLMResponse.Response.Usage.PromptTokens != 120 {
		t.Fatalf("prompt tokens = %d, want 120", decision.LLMResponse.Response.Usage.PromptTokens)
	}
	if decision.LLMResponse.Response.Usage.CompletionTokens != 45 {
		t.Fatalf("completion tokens = %d, want 45", decision.LLMResponse.Response.Usage.CompletionTokens)
	}
	if decision.LLMResponse.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", decision.LLMResponse.Provider, "test-provider")
	}
	if decision.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("model in response = %q, want %q", decision.LLMResponse.Response.Model, "test-model")
	}

	// Verify the system prompt was the bear researcher prompt.
	if mock.lastReq.Messages[0].Content != BearResearcherSystemPrompt {
		t.Fatalf("system prompt mismatch:\ngot:  %q\nwant: %q", mock.lastReq.Messages[0].Content, BearResearcherSystemPrompt)
	}

	// Verify the model was forwarded.
	if mock.lastReq.Model != "test-model" {
		t.Fatalf("model = %q, want %q", mock.lastReq.Model, "test-model")
	}
}

func TestBearResearcherExecuteNilProvider(t *testing.T) {
	bear := NewBearResearcher(nil, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := bear.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "bear_researcher (research_debate): nil llm provider"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestBearResearcherExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("service unavailable"),
	}

	bear := NewBearResearcher(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := bear.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "bear_researcher (research_debate): llm completion failed: service unavailable"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}

	// Verify no contribution was stored on error.
	if got := state.ResearchDebate.Rounds[0].Contributions[agent.AgentRoleBearResearcher]; got != "" {
		t.Fatalf("contribution should be empty on error, got %q", got)
	}
}

func TestBearResearcherExecuteNoRounds(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Bear case without rounds.",
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}

	bear := NewBearResearcher(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{},
	}

	// Execute should succeed even with no rounds; it calls the LLM but
	// does not store a contribution or decision since there is no round.
	if err := bear.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// No decision should be recorded when there are no rounds.
	roundNumber := 0
	if _, ok := state.Decision(agent.AgentRoleBearResearcher, agent.PhaseResearchDebate, &roundNumber); ok {
		t.Fatal("Decision() should not be recorded when no rounds exist (round 0)")
	}

	// Also ensure no decision is recorded under a nil round key.
	if _, ok := state.Decision(agent.AgentRoleBearResearcher, agent.PhaseResearchDebate, nil); ok {
		t.Fatal("Decision() should not be recorded when no rounds exist (nil round)")
	}
}

// Verify BearResearcher satisfies the agent.Node interface at compile time.
var _ agent.Node = (*BearResearcher)(nil)
