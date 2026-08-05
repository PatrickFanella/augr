package debate

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
			Content:   "Bull case updated.",
			LatencyMS: 321,
			CostUSD:   0.0123,
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

	content, promptText, response, err := debater.CallWithContext(
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
		t.Fatalf("CallWithContext() error = %v, want nil", err)
	}

	if content != "Bull case updated." {
		t.Fatalf("content = %q, want %q", content, "Bull case updated.")
	}
	if response.Usage.PromptTokens != 91 || response.Usage.CompletionTokens != 27 {
		t.Fatalf("usage = %+v, want prompt=91 completion=27", response.Usage)
	}
	if response.LatencyMS != 321 || response.CostUSD != 0.0123 {
		t.Fatalf("provider metadata lost: latency=%d cost=%f", response.LatencyMS, response.CostUSD)
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
	wantPromptText := "You are the bull researcher.\n\n" + wantUser
	if promptText != wantPromptText {
		t.Fatalf("prompt text = %q, want %q", promptText, wantPromptText)
	}
}

func TestBaseDebaterCallWithContextCompactsBeforeProvider(t *testing.T) {
	mock := &mockProvider{response: &llm.CompletionResponse{Content: "ok"}}
	debater := NewBaseDebater(agent.AgentRoleBullResearcher, agent.PhaseResearchDebate, mock, "deep-model", slog.Default())
	rounds := make([]agent.DebateRound, 20)
	for i := range rounds {
		rounds[i] = agent.DebateRound{Number: i + 1, Contributions: map[agent.AgentRole]string{agent.AgentRoleBullResearcher: strings.Repeat("b", 8*1024), agent.AgentRoleBearResearcher: strings.Repeat("b", 8*1024)}}
	}
	reports := map[agent.AgentRole]string{agent.AgentRoleMarketAnalyst: strings.Repeat("m", 20*1024), agent.AgentRoleNewsAnalyst: strings.Repeat("n", 20*1024), agent.AgentRoleFundamentalsAnalyst: strings.Repeat("f", 20*1024)}
	_, _, _, err := debater.CallWithContext(context.Background(), strings.Repeat("s", 32), rounds, reports)
	if err != nil {
		t.Fatal(err)
	}
	if got := mock.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if got := llm.PromptBytes(mock.lastReq.Messages); got > maxDebatePromptBytes {
		t.Fatalf("prompt bytes = %d, max = %d", got, maxDebatePromptBytes)
	}
}

func TestBaseDebaterCallWithContextRejectsOversizedSystemPrompt(t *testing.T) {
	mock := &mockProvider{response: &llm.CompletionResponse{Content: "ok"}}
	debater := NewBaseDebater(agent.AgentRoleBullResearcher, agent.PhaseResearchDebate, mock, "deep-model", slog.Default())
	_, _, _, err := debater.CallWithContext(context.Background(), strings.Repeat("x", maxDebatePromptBytes+1), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*llm.PromptTooLargeError); ok {
		t.Fatal("expected wrapped error, got raw")
	}
	if got := mock.calls.Load(); got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
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

	_, _, _, err := debater.CallWithContext(context.Background(), "system", nil, nil)
	if err == nil {
		t.Fatal("CallWithContext() error = nil, want non-nil")
	}

	want := "bear_researcher (research_debate): llm completion failed: boom"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
