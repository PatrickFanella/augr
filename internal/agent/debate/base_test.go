package debate

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

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

func TestFormatRoundsForPrompt(t *testing.T) {
	rounds := []agent.DebateRound{
		{
			Number: 1,
			Contributions: map[agent.AgentRole]string{
				agent.AgentRoleBullResearcher: "Revenue growth is accelerating.",
				agent.AgentRoleBearResearcher: "Margins are under pressure.",
			},
		},
		{
			Number:        2,
			Contributions: map[agent.AgentRole]string{},
		},
	}

	got := formatRoundsForPrompt(rounds)
	want := "Round 1:\n" +
		"- bear_researcher: Margins are under pressure.\n" +
		"- bull_researcher: Revenue growth is accelerating.\n\n" +
		"Round 2:\n" +
		"- No contributions recorded."

	if got != want {
		t.Fatalf("formatRoundsForPrompt() = %q, want %q", got, want)
	}
}

func TestBaseDebaterCallWithContextSendsCorrectMessages(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Bull case updated.",
			Usage: llm.CompletionUsage{
				PromptTokens:     91,
				CompletionTokens: 27,
			},
		},
	}

	debater := NewBaseDebater(
		agent.AgentRoleBullResearcher,
		agent.PhaseResearchDebate,
		mock,
		"deep-model",
		slog.Default(),
	)

	content, usage, err := debater.callWithContext(
		context.Background(),
		"You are the bull researcher.",
		[]agent.DebateRound{
			{
				Number: 1,
				Contributions: map[agent.AgentRole]string{
					agent.AgentRoleBullResearcher: "Opening bull thesis.",
					agent.AgentRoleBearResearcher: "Initial rebuttal.",
				},
			},
		},
		map[agent.AgentRole]string{
			agent.AgentRoleNewsAnalyst:   "News flow is mixed.",
			agent.AgentRoleMarketAnalyst: "Trend remains constructive.",
		},
	)
	if err != nil {
		t.Fatalf("callWithContext() error = %v, want nil", err)
	}

	if content != "Bull case updated." {
		t.Fatalf("content = %q, want %q", content, "Bull case updated.")
	}
	if usage.PromptTokens != 91 || usage.CompletionTokens != 27 {
		t.Fatalf("usage = %+v, want prompt=91 completion=27", usage)
	}
	if got := mock.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if mock.lastReq.Model != "deep-model" {
		t.Fatalf("request model = %q, want %q", mock.lastReq.Model, "deep-model")
	}
	if len(mock.lastReq.Messages) != 2 {
		t.Fatalf("request messages = %d, want 2", len(mock.lastReq.Messages))
	}
	if got := mock.lastReq.Messages[0]; got.Role != "system" || got.Content != "You are the bull researcher." {
		t.Fatalf("system message = %+v, want role=system content=%q", got, "You are the bull researcher.")
	}

	wantUser := "Previous debate rounds:\n" +
		"Round 1:\n" +
		"- bear_researcher: Initial rebuttal.\n" +
		"- bull_researcher: Opening bull thesis.\n\n" +
		"Analyst reports:\n" +
		"market_analyst:\n" +
		"Trend remains constructive.\n\n" +
		"news_analyst:\n" +
		"News flow is mixed."
	if got := mock.lastReq.Messages[1]; got.Role != "user" || got.Content != wantUser {
		t.Fatalf("user message = %+v, want role=user content=%q", got, wantUser)
	}
}

func TestBaseDebaterCallWithContextIncludesRoleAndPhaseInErrors(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("boom"),
	}

	debater := NewBaseDebater(
		agent.AgentRoleBearResearcher,
		agent.PhaseResearchDebate,
		mock,
		"deep-model",
		slog.Default(),
	)

	_, _, err := debater.callWithContext(context.Background(), "system", nil, nil)
	if err == nil {
		t.Fatal("callWithContext() error = nil, want non-nil")
	}

	want := "bear_researcher (research_debate): llm completion failed: boom"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
