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

func TestFundamentalsAnalystName(t *testing.T) {
	fa := NewFundamentalsAnalyst(&mockProvider{}, "openai", "test-model", nil)
	if fa.Name() != "fundamentals_analyst" {
		t.Errorf("Name() = %q, want %q", fa.Name(), "fundamentals_analyst")
	}
}

func TestFundamentalsAnalystRole(t *testing.T) {
	fa := NewFundamentalsAnalyst(&mockProvider{}, "openai", "test-model", nil)
	if fa.Role() != agent.AgentRoleFundamentalsAnalyst {
		t.Errorf("Role() = %q, want %q", fa.Role(), agent.AgentRoleFundamentalsAnalyst)
	}
}

func TestFundamentalsAnalystPhase(t *testing.T) {
	fa := NewFundamentalsAnalyst(&mockProvider{}, "openai", "test-model", nil)
	if fa.Phase() != agent.PhaseAnalysis {
		t.Errorf("Phase() = %q, want %q", fa.Phase(), agent.PhaseAnalysis)
	}
}

func TestFundamentalsAnalystExecute(t *testing.T) {
	wantContent := "## Valuation Assessment\nFairly valued based on P/E."
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

	fa := NewFundamentalsAnalyst(mock, "openai", "gpt-4", nil)

	state := &agent.PipelineState{
		Ticker: "AAPL",
		Fundamentals: &data.Fundamentals{
			Ticker:           "AAPL",
			MarketCap:        2800000000000,
			PERatio:          28.5,
			EPS:              6.42,
			Revenue:          394000000000,
			RevenueGrowthYoY: 0.08,
			GrossMargin:      0.438,
			DebtToEquity:     1.87,
			FreeCashFlow:     111000000000,
			DividendYield:    0.0055,
			FetchedAt:        time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC),
		},
	}

	err := fa.Execute(context.Background(), state)
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
	if mock.lastReq.Messages[0].Content != FundamentalsAnalystSystemPrompt {
		t.Error("system message does not match FundamentalsAnalystSystemPrompt")
	}
	if mock.lastReq.Messages[1].Role != "user" {
		t.Errorf("second message role = %q, want %q", mock.lastReq.Messages[1].Role, "user")
	}

	// Verify user prompt references the ticker and data.
	userMsg := mock.lastReq.Messages[1].Content
	for _, want := range []string{"AAPL", "28.50", "6.42", "Market Cap"} {
		if !strings.Contains(userMsg, want) {
			t.Errorf("user prompt missing expected content %q", want)
		}
	}

	// Verify model was passed through.
	if mock.lastReq.Model != "gpt-4" {
		t.Errorf("request model = %q, want %q", mock.lastReq.Model, "gpt-4")
	}

	// Verify report stored in state.
	report, ok := state.AnalystReports[agent.AgentRoleFundamentalsAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state")
	}
	if report != wantContent {
		t.Errorf("stored report = %q, want %q", report, wantContent)
	}

	// Verify decision recorded in state.
	dec, ok := state.Decision(agent.AgentRoleFundamentalsAnalyst, agent.PhaseAnalysis, nil)
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

func TestFundamentalsAnalystExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("rate limit exceeded"),
	}

	fa := NewFundamentalsAnalyst(mock, "openai", "gpt-4", nil)
	state := &agent.PipelineState{
		Ticker: "TSLA",
		Fundamentals: &data.Fundamentals{
			Ticker:  "TSLA",
			PERatio: 60.0,
		},
	}

	err := fa.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() should return error when LLM fails")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("error should wrap LLM error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "fundamentals_analyst") {
		t.Errorf("error should include node name, got: %v", err)
	}
}

func TestFundamentalsAnalystExecuteNilProvider(t *testing.T) {
	fa := NewFundamentalsAnalyst(nil, "openai", "gpt-4", nil)
	state := &agent.PipelineState{
		Ticker:       "GOOG",
		Fundamentals: &data.Fundamentals{Ticker: "GOOG"},
	}

	err := fa.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() should return error when provider is nil")
	}
	if !strings.Contains(err.Error(), "provider is nil") {
		t.Errorf("error should mention nil provider, got: %v", err)
	}
}

func TestFundamentalsAnalystExecuteNilFundamentals(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "should not be called",
		},
	}

	fa := NewFundamentalsAnalyst(mock, "openai", "gpt-4", nil)
	state := &agent.PipelineState{
		Ticker:       "BTC-USD",
		Fundamentals: nil,
	}

	err := fa.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	// Verify LLM was NOT called.
	if got := mock.calls.Load(); got != 0 {
		t.Errorf("LLM called %d times, want 0 when fundamentals are nil", got)
	}

	// Verify a report was still stored.
	report, ok := state.AnalystReports[agent.AgentRoleFundamentalsAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state when fundamentals are nil")
	}
	if !strings.Contains(report, "No fundamentals available") {
		t.Errorf("report = %q, want message about no fundamentals available", report)
	}

	// Verify decision was recorded without LLM metadata.
	dec, ok := state.Decision(agent.AgentRoleFundamentalsAnalyst, agent.PhaseAnalysis, nil)
	if !ok {
		t.Fatal("decision not recorded in state when fundamentals are nil")
	}
	if !strings.Contains(dec.OutputText, "No fundamentals available") {
		t.Errorf("decision output = %q, want message about no fundamentals", dec.OutputText)
	}
	if dec.LLMResponse != nil {
		t.Errorf("decision LLM response should be nil when fundamentals are nil, got %+v", dec.LLMResponse)
	}
}

func TestFundamentalsAnalystExecuteNilFundamentalsNilProvider(t *testing.T) {
	fa := NewFundamentalsAnalyst(nil, "openai", "gpt-4", nil)
	state := &agent.PipelineState{
		Ticker:       "BTC-USD",
		Fundamentals: nil,
	}

	err := fa.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	// Verify a report was still stored even with nil provider.
	report, ok := state.AnalystReports[agent.AgentRoleFundamentalsAnalyst]
	if !ok {
		t.Fatal("analyst report not stored in state")
	}
	if !strings.Contains(report, "No fundamentals available") {
		t.Errorf("report = %q, want message about no fundamentals available", report)
	}
}

func TestFundamentalsAnalystImplementsNode(t *testing.T) {
	// Compile-time check that *FundamentalsAnalyst satisfies agent.Node.
	var _ agent.Node = (*FundamentalsAnalyst)(nil)
}

func TestFundamentalsAnalystAnalyzeSkipsWhenNilFundamentals(t *testing.T) {
	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "should not be called",
		},
	}

	fa := NewFundamentalsAnalyst(mock, "openai", "gpt-4", nil)

	input := agent.AnalysisInput{
		Ticker:       "BTC-USD",
		Fundamentals: nil,
	}

	output, err := fa.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}

	// Verify LLM was NOT called.
	if got := mock.calls.Load(); got != 0 {
		t.Errorf("LLM called %d times, want 0 when fundamentals are nil", got)
	}

	// Verify the skip message is returned as the report.
	if !strings.Contains(output.Report, "No fundamentals available") {
		t.Errorf("Report = %q, want message about no fundamentals available", output.Report)
	}

	// Verify LLMResponse is nil when skipped.
	if output.LLMResponse != nil {
		t.Errorf("LLMResponse should be nil when skipped, got %+v", output.LLMResponse)
	}
}
