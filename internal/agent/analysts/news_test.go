package analysts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestNewsAnalystName(t *testing.T) {
	na := NewNewsAnalyst(&mockProvider{}, "openai", "test-model", nil)
	if na.Name() != "news_analyst" {
		t.Errorf("Name() = %q, want %q", na.Name(), "news_analyst")
	}
}

func TestNewsAnalystRole(t *testing.T) {
	na := NewNewsAnalyst(&mockProvider{}, "openai", "test-model", nil)
	if na.Role() != agent.AgentRoleNewsAnalyst {
		t.Errorf("Role() = %q, want %q", na.Role(), agent.AgentRoleNewsAnalyst)
	}
}

func TestNewsAnalystPhase(t *testing.T) {
	na := NewNewsAnalyst(&mockProvider{}, "openai", "test-model", nil)
	if na.Phase() != agent.PhaseAnalysis {
		t.Errorf("Phase() = %q, want %q", na.Phase(), agent.PhaseAnalysis)
	}
}

func TestNewsAnalystExecute(t *testing.T) {
	wantContent := "## Sentiment Summary\nOverall bullish with high confidence."
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: wantContent,
			Usage: llm.CompletionUsage{
				PromptTokens:     150,
				CompletionTokens: 90,
			},
			Model: "gpt-4",
		},
	}

	na := NewNewsAnalyst(mock, "openai", "gpt-4", nil)

	ts := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	state := &agent.PipelineState{
		Ticker: "AAPL",
		News: []data.NewsArticle{
			{
				Title:       "Apple Beats Earnings Estimates",
				Summary:     "Apple reported Q2 earnings above expectations.",
				URL:         "https://example.com/apple-earnings",
				Source:      "Reuters",
				PublishedAt: ts,
				Sentiment:   0.85,
			},
		},
	}

	err := na.Execute(context.Background(), state)
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
	if mock.lastReq.Messages[0].Content != NewsAnalystSystemPrompt {
		t.Error("system message does not match NewsAnalystSystemPrompt")
	}
	if mock.lastReq.Messages[1].Role != "user" {
		t.Errorf("second message role = %q, want %q", mock.lastReq.Messages[1].Role, "user")
	}

	// Verify user prompt references the ticker and article data.
	userMsg := mock.lastReq.Messages[1].Content
	for _, want := range []string{"AAPL", "Apple Beats Earnings Estimates", "Reuters", "0.85"} {
		if !strings.Contains(userMsg, want) {
			t.Errorf("user prompt missing expected content %q", want)
		}
	}

	// Verify model was passed through.
	if mock.lastReq.Model != "gpt-4" {
		t.Errorf("request model = %q, want %q", mock.lastReq.Model, "gpt-4")
	}

	// Verify report stored in state.
	report, ok := state.AnalystReports[agent.AgentRoleNewsAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state")
	}
	if report != wantContent {
		t.Errorf("stored report = %q, want %q", report, wantContent)
	}

	// Verify decision recorded in state.
	dec, ok := state.Decision(agent.AgentRoleNewsAnalyst, agent.PhaseAnalysis, nil)
	if !ok {
		t.Fatal("decision not recorded in state")
	}
	if dec.OutputText != wantContent {
		t.Errorf("decision output = %q, want %q", dec.OutputText, wantContent)
	}
	if dec.LLMResponse == nil {
		t.Fatal("decision LLM response is nil")
	}
	if dec.LLMResponse.Response.Usage.PromptTokens != 150 {
		t.Errorf("prompt tokens = %d, want 150", dec.LLMResponse.Response.Usage.PromptTokens)
	}
	if dec.LLMResponse.Provider != "openai" {
		t.Errorf("decision provider = %q, want %q", dec.LLMResponse.Provider, "openai")
	}
}

func TestNewsAnalystExecuteNoArticles(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{Content: "should not be called"},
	}

	na := NewNewsAnalyst(mock, "openai", "gpt-4", nil)
	state := &agent.PipelineState{Ticker: "TSLA"}

	err := na.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	// Verify LLM was NOT called.
	if got := mock.calls.Load(); got != 0 {
		t.Errorf("LLM called %d times, want 0 when no articles", got)
	}

	// Verify static report stored in state.
	report, ok := state.AnalystReports[agent.AgentRoleNewsAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state")
	}
	if !strings.Contains(report, "No news articles available") {
		t.Errorf("report should indicate no data, got: %q", report)
	}

	// Verify decision recorded without LLM metadata.
	dec, ok := state.Decision(agent.AgentRoleNewsAnalyst, agent.PhaseAnalysis, nil)
	if !ok {
		t.Fatal("decision not recorded in state")
	}
	if !strings.Contains(dec.OutputText, "No news articles available") {
		t.Errorf("decision output should indicate no data, got: %q", dec.OutputText)
	}
	if dec.LLMResponse != nil {
		t.Error("decision LLM response should be nil when no articles")
	}
}

func TestNewsAnalystExecuteNilNews(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{Content: "should not be called"},
	}

	na := NewNewsAnalyst(mock, "openai", "gpt-4", nil)
	state := &agent.PipelineState{
		Ticker: "GOOG",
		News:   nil,
	}

	err := na.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	// Verify LLM was NOT called.
	if got := mock.calls.Load(); got != 0 {
		t.Errorf("LLM called %d times, want 0 when News is nil", got)
	}

	// Verify static report stored.
	report, ok := state.AnalystReports[agent.AgentRoleNewsAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state")
	}
	if !strings.Contains(report, "No news articles available") {
		t.Errorf("report should indicate no data, got: %q", report)
	}
}

func TestNewsAnalystExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("rate limit exceeded"),
	}

	na := NewNewsAnalyst(mock, "openai", "gpt-4", nil)
	state := &agent.PipelineState{
		Ticker: "TSLA",
		News: []data.NewsArticle{
			{Title: "Test Article", PublishedAt: time.Now(), Sentiment: 0.5},
		},
	}

	err := na.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() should return error when LLM fails")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("error should wrap LLM error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "news_analyst") {
		t.Errorf("error should include node name, got: %v", err)
	}
}

func TestNewsAnalystExecuteNilProvider(t *testing.T) {
	na := NewNewsAnalyst(nil, "openai", "gpt-4", nil)

	// With no news, nil provider should not cause an error (LLM is not needed).
	noNewsState := &agent.PipelineState{Ticker: "GOOG"}
	if err := na.Execute(context.Background(), noNewsState); err != nil {
		t.Fatalf("Execute() should succeed with nil provider when no news: %v", err)
	}

	// With news, nil provider should return an error.
	state := &agent.PipelineState{
		Ticker: "GOOG",
		News: []data.NewsArticle{
			{Title: "Test Article", PublishedAt: time.Now(), Sentiment: 0.5},
		},
	}
	err := na.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() should return error when provider is nil and news is present")
	}
	if !strings.Contains(err.Error(), "provider is nil") {
		t.Errorf("error should mention nil provider, got: %v", err)
	}
}

func TestNewsAnalystImplementsNode(t *testing.T) {
	// Compile-time check that *NewsAnalyst satisfies agent.Node.
	var _ agent.Node = (*NewsAnalyst)(nil)
}
