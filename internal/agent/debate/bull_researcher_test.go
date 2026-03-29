package debate

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestNewBullResearcherNilLogger(t *testing.T) {
	bull := NewBullResearcher(nil, "openai", "model", nil)
	if bull == nil {
		t.Fatal("NewBullResearcher() returned nil")
	}
}

func TestBullResearcherNodeInterface(t *testing.T) {
	bull := NewBullResearcher(nil, "openai", "model", slog.Default())

	if got := bull.Name(); got != "bull_researcher" {
		t.Fatalf("Name() = %q, want %q", got, "bull_researcher")
	}
	if got := bull.Role(); got != agent.AgentRoleBullResearcher {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleBullResearcher)
	}
	if got := bull.Phase(); got != agent.PhaseResearchDebate {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseResearchDebate)
	}
}

func TestBullResearcherExecuteStoresContributionAndDecision(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Revenue growth is accelerating and margins are expanding.",
			Usage: llm.CompletionUsage{
				PromptTokens:     110,
				CompletionTokens: 50,
			},
		},
	}

	bull := NewBullResearcher(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		AnalystReports: map[agent.AgentRole]string{
			agent.AgentRoleMarketAnalyst: "Trend is bullish.",
			agent.AgentRoleNewsAnalyst:   "Positive coverage.",
		},
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleBearResearcher: "Risks are elevated.",
					},
				},
			},
		},
	}

	if err := bull.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify contribution was stored in the current round.
	got := state.ResearchDebate.Rounds[0].Contributions[agent.AgentRoleBullResearcher]
	want := "Revenue growth is accelerating and margins are expanding."
	if got != want {
		t.Fatalf("contribution = %q, want %q", got, want)
	}

	// Verify that RecordDecision was called (decision is retrievable).
	roundNumber := 1
	decision, ok := state.Decision(agent.AgentRoleBullResearcher, agent.PhaseResearchDebate, &roundNumber)
	if !ok {
		t.Fatal("Decision() not found for bull_researcher")
	}
	if decision.OutputText != want {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, want)
	}
	if decision.LLMResponse == nil || decision.LLMResponse.Response == nil {
		t.Fatal("decision LLM response is nil")
	}
	if decision.LLMResponse.Response.Usage.PromptTokens != 110 {
		t.Fatalf("prompt tokens = %d, want 110", decision.LLMResponse.Response.Usage.PromptTokens)
	}
	if decision.LLMResponse.Response.Usage.CompletionTokens != 50 {
		t.Fatalf("completion tokens = %d, want 50", decision.LLMResponse.Response.Usage.CompletionTokens)
	}
	if decision.LLMResponse.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", decision.LLMResponse.Provider, "test-provider")
	}
	if decision.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("model in response = %q, want %q", decision.LLMResponse.Response.Model, "test-model")
	}

	// Verify the system prompt was the bull researcher prompt.
	if mock.lastReq.Messages[0].Content != BullResearcherSystemPrompt {
		t.Fatalf("system prompt mismatch:\ngot:  %q\nwant: %q", mock.lastReq.Messages[0].Content, BullResearcherSystemPrompt)
	}

	// Verify the model was forwarded.
	if mock.lastReq.Model != "test-model" {
		t.Fatalf("model = %q, want %q", mock.lastReq.Model, "test-model")
	}
}

func TestBullResearcherExecuteNilProvider(t *testing.T) {
	bull := NewBullResearcher(nil, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := bull.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "bull_researcher (research_debate): nil llm provider"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestBullResearcherExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("service unavailable"),
	}

	bull := NewBullResearcher(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := bull.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "bull_researcher (research_debate): llm completion failed: service unavailable"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}

	// Verify no contribution was stored on error.
	if got := state.ResearchDebate.Rounds[0].Contributions[agent.AgentRoleBullResearcher]; got != "" {
		t.Fatalf("contribution should be empty on error, got %q", got)
	}
}

func TestBullResearcherExecuteNoRounds(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Bull case without rounds.",
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}

	bull := NewBullResearcher(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{},
	}

	// Execute should succeed even with no rounds; it calls the LLM but
	// does not store a contribution or decision since there is no round.
	if err := bull.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// No decision should be recorded when there are no rounds.
	roundNumber := 0
	if _, ok := state.Decision(agent.AgentRoleBullResearcher, agent.PhaseResearchDebate, &roundNumber); ok {
		t.Fatal("Decision() should not be recorded when no rounds exist (round 0)")
	}

	// Also ensure no decision is recorded under a nil round key.
	if _, ok := state.Decision(agent.AgentRoleBullResearcher, agent.PhaseResearchDebate, nil); ok {
		t.Fatal("Decision() should not be recorded when no rounds exist (nil round)")
	}
}

func TestBullResearcherDebate(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Bull thesis: strong revenue and expanding TAM.",
			Usage: llm.CompletionUsage{
				PromptTokens:     125,
				CompletionTokens: 35,
			},
		},
	}

	bull := NewBullResearcher(mock, "test-provider", "test-model", slog.Default())

	input := agent.DebateInput{
		Ticker: "GOOG",
		Rounds: []agent.DebateRound{
			{
				Number: 1,
				Contributions: map[agent.AgentRole]string{
					agent.AgentRoleBearResearcher: "Regulatory risk is high.",
				},
			},
		},
		ContextReports: map[agent.AgentRole]string{
			agent.AgentRoleMarketAnalyst: "Trend remains positive.",
		},
	}

	output, err := bull.Debate(context.Background(), input)
	if err != nil {
		t.Fatalf("Debate() error = %v, want nil", err)
	}

	// Verify contribution content.
	want := "Bull thesis: strong revenue and expanding TAM."
	if output.Contribution != want {
		t.Fatalf("Contribution = %q, want %q", output.Contribution, want)
	}

	// Verify LLMResponse metadata.
	if output.LLMResponse == nil {
		t.Fatal("LLMResponse is nil")
	}
	if output.LLMResponse.Provider != "test-provider" {
		t.Fatalf("Provider = %q, want %q", output.LLMResponse.Provider, "test-provider")
	}
	if output.LLMResponse.Response == nil {
		t.Fatal("LLMResponse.Response is nil")
	}
	if output.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("Model = %q, want %q", output.LLMResponse.Response.Model, "test-model")
	}
	if output.LLMResponse.Response.Usage.PromptTokens != 125 {
		t.Fatalf("PromptTokens = %d, want 125", output.LLMResponse.Response.Usage.PromptTokens)
	}
	if output.LLMResponse.Response.Usage.CompletionTokens != 35 {
		t.Fatalf("CompletionTokens = %d, want 35", output.LLMResponse.Response.Usage.CompletionTokens)
	}

	// Verify the system prompt was the bull researcher prompt.
	if mock.lastReq.Messages[0].Content != BullResearcherSystemPrompt {
		t.Fatalf("system prompt mismatch")
	}
}

// Verify BullResearcher satisfies the agent.DebaterNode interface at compile time.
var _ agent.DebaterNode = (*BullResearcher)(nil)
