package analysts

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// mockProvider is a test double that records the request and returns a
// pre-configured response.
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

func TestMarketAnalystName(t *testing.T) {
	ma := NewMarketAnalyst(&mockProvider{}, "test-model", nil)
	if ma.Name() != "market_analyst" {
		t.Errorf("Name() = %q, want %q", ma.Name(), "market_analyst")
	}
}

func TestMarketAnalystRole(t *testing.T) {
	ma := NewMarketAnalyst(&mockProvider{}, "test-model", nil)
	if ma.Role() != agent.AgentRoleMarketAnalyst {
		t.Errorf("Role() = %q, want %q", ma.Role(), agent.AgentRoleMarketAnalyst)
	}
}

func TestMarketAnalystPhase(t *testing.T) {
	ma := NewMarketAnalyst(&mockProvider{}, "test-model", nil)
	if ma.Phase() != agent.PhaseAnalysis {
		t.Errorf("Phase() = %q, want %q", ma.Phase(), agent.PhaseAnalysis)
	}
}

func TestMarketAnalystExecute(t *testing.T) {
	wantContent := "## Trend Analysis\nBullish trend confirmed."
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: wantContent,
			Usage: llm.CompletionUsage{
				PromptTokens:     120,
				CompletionTokens: 80,
			},
			Model: "gpt-4",
		},
	}

	ma := NewMarketAnalyst(mock, "gpt-4", nil)

	ts := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	state := &agent.PipelineState{
		Ticker: "AAPL",
		Market: agent.MarketData{
			Bars: []domain.OHLCV{
				{Timestamp: ts, Open: 100.50, High: 105.25, Low: 99.75, Close: 103.00, Volume: 1500000},
			},
			Indicators: []domain.Indicator{
				{Name: "RSI_14", Value: 65.3, Timestamp: ts},
			},
		},
	}

	err := ma.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	// Verify LLM was called exactly once.
	if got := mock.calls.Load(); got != 1 {
		t.Errorf("LLM called %d times, want 1", got)
	}

	// Verify the request contains system and user messages.
	if len(mock.lastReq.Messages) != 2 {
		t.Fatalf("request has %d messages, want 2", len(mock.lastReq.Messages))
	}
	if mock.lastReq.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", mock.lastReq.Messages[0].Role, "system")
	}
	if mock.lastReq.Messages[0].Content != MarketAnalystSystemPrompt {
		t.Error("system message does not match MarketAnalystSystemPrompt")
	}
	if mock.lastReq.Messages[1].Role != "user" {
		t.Errorf("second message role = %q, want %q", mock.lastReq.Messages[1].Role, "user")
	}

	// Verify user prompt references the ticker and data.
	userMsg := mock.lastReq.Messages[1].Content
	for _, want := range []string{"AAPL", "100.50", "RSI_14", "65.3000"} {
		if !strings.Contains(userMsg, want) {
			t.Errorf("user prompt missing expected content %q", want)
		}
	}

	// Verify model was passed through.
	if mock.lastReq.Model != "gpt-4" {
		t.Errorf("request model = %q, want %q", mock.lastReq.Model, "gpt-4")
	}

	// Verify report stored in state.
	report, ok := state.AnalystReports[agent.AgentRoleMarketAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state")
	}
	if report != wantContent {
		t.Errorf("stored report = %q, want %q", report, wantContent)
	}

	// Verify decision recorded in state.
	dec, ok := state.Decision(agent.AgentRoleMarketAnalyst, agent.PhaseAnalysis, nil)
	if !ok {
		t.Fatal("decision not recorded in state")
	}
	if dec.OutputText != wantContent {
		t.Errorf("decision output = %q, want %q", dec.OutputText, wantContent)
	}
	if dec.LLMResponse == nil {
		t.Fatal("decision LLM response is nil")
	}
	if dec.LLMResponse.Response.Usage.PromptTokens != 120 {
		t.Errorf("prompt tokens = %d, want 120", dec.LLMResponse.Response.Usage.PromptTokens)
	}
}

func TestMarketAnalystExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("rate limit exceeded"),
	}

	ma := NewMarketAnalyst(mock, "gpt-4", nil)
	state := &agent.PipelineState{Ticker: "TSLA"}

	err := ma.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() should return error when LLM fails")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("error should wrap LLM error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "market_analyst") {
		t.Errorf("error should include node name, got: %v", err)
	}
}

func TestMarketAnalystExecuteNilProvider(t *testing.T) {
	ma := NewMarketAnalyst(nil, "gpt-4", nil)
	state := &agent.PipelineState{Ticker: "GOOG"}

	err := ma.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() should return error when provider is nil")
	}
	if !strings.Contains(err.Error(), "provider is nil") {
		t.Errorf("error should mention nil provider, got: %v", err)
	}
}

func TestMarketAnalystImplementsNode(t *testing.T) {
	// Compile-time check that *MarketAnalyst satisfies agent.Node.
	var _ agent.Node = (*MarketAnalyst)(nil)
}
