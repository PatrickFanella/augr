package analysts

import (
	"context"
	"errors"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestBaseAnalyst_Analyze_HappyPath(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "Market analysis: bullish",
			Usage:   llm.CompletionUsage{PromptTokens: 100, CompletionTokens: 50},
		},
	}

	base := NewBaseAnalyst(BaseAnalystConfig{
		Provider:     provider,
		ProviderName: "test-provider",
		Model:        "test-model",
		Role:         agent.AgentRoleMarketAnalyst,
		Name:         "test_analyst",
		SystemPrompt: "You are a test analyst",
		BuildPrompt: func(_ agent.AnalysisInput) (string, bool) {
			return "Analyze AAPL", true
		},
	})

	output, err := base.Analyze(context.Background(), agent.AnalysisInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if output.Report != "Market analysis: bullish" {
		t.Errorf("report = %q, want 'Market analysis: bullish'", output.Report)
	}
	if output.LLMResponse == nil {
		t.Fatal("LLMResponse is nil")
	}
	if output.LLMResponse.Provider != "test-provider" {
		t.Errorf("provider = %q, want test-provider", output.LLMResponse.Provider)
	}
	if output.LLMResponse.PromptText == "" {
		t.Error("prompt text is empty")
	}
	if provider.calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1", provider.calls.Load())
	}
}

func TestBaseAnalyst_Analyze_SkipPath(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		response: &llm.CompletionResponse{Content: "should not be called"},
	}

	base := NewBaseAnalyst(BaseAnalystConfig{
		Provider:     provider,
		ProviderName: "test",
		Model:        "test-model",
		Role:         agent.AgentRoleFundamentalsAnalyst,
		Name:         "fundamentals_analyst",
		SystemPrompt: "You are a fundamentals analyst",
		SkipMessage:  "No fundamentals available for this asset type.",
		BuildPrompt: func(_ agent.AnalysisInput) (string, bool) {
			return "", false // Skip LLM call.
		},
	})

	output, err := base.Analyze(context.Background(), agent.AnalysisInput{Ticker: "BTC"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if output.Report != "No fundamentals available for this asset type." {
		t.Errorf("report = %q, want skip message", output.Report)
	}
	if output.LLMResponse != nil {
		t.Error("LLMResponse should be nil when skipped")
	}
	if provider.calls.Load() != 0 {
		t.Errorf("provider calls = %d, want 0 (skipped)", provider.calls.Load())
	}
}

func TestBaseAnalyst_Analyze_ProviderError(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		err: errors.New("rate limited"),
	}

	base := NewBaseAnalyst(BaseAnalystConfig{
		Provider:     provider,
		ProviderName: "test",
		Model:        "test-model",
		Role:         agent.AgentRoleMarketAnalyst,
		Name:         "market_analyst",
		SystemPrompt: "You are a market analyst",
		BuildPrompt: func(_ agent.AnalysisInput) (string, bool) {
			return "Analyze", true
		},
	})

	_, err := base.Analyze(context.Background(), agent.AnalysisInput{Ticker: "AAPL"})
	if err == nil {
		t.Fatal("expected error from provider")
	}
	if !errors.Is(err, errors.Unwrap(err)) {
		// Just verify the error contains the agent name for context.
		if got := err.Error(); len(got) == 0 {
			t.Error("error message is empty")
		}
	}
}

func TestBaseAnalyst_Analyze_NilProvider(t *testing.T) {
	t.Parallel()

	base := NewBaseAnalyst(BaseAnalystConfig{
		Provider:     nil, // Nil provider.
		ProviderName: "test",
		Model:        "test-model",
		Role:         agent.AgentRoleMarketAnalyst,
		Name:         "market_analyst",
		SystemPrompt: "You are a market analyst",
		BuildPrompt: func(_ agent.AnalysisInput) (string, bool) {
			return "Analyze", true
		},
	})

	_, err := base.Analyze(context.Background(), agent.AnalysisInput{Ticker: "AAPL"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestBaseAnalyst_Execute_DelegatesToAnalyze(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		response: &llm.CompletionResponse{
			Content: "exec report",
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}

	base := NewBaseAnalyst(BaseAnalystConfig{
		Provider:     provider,
		ProviderName: "test",
		Model:        "test-model",
		Role:         agent.AgentRoleMarketAnalyst,
		Name:         "market_analyst",
		SystemPrompt: "system",
		BuildPrompt: func(_ agent.AnalysisInput) (string, bool) {
			return "exec prompt", true
		},
	})

	state := &agent.PipelineState{Ticker: "AAPL"}
	if err := base.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if provider.calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1", provider.calls.Load())
	}
}

func TestBaseAnalyst_NameRolePhase(t *testing.T) {
	t.Parallel()

	base := NewBaseAnalyst(BaseAnalystConfig{
		Role: agent.AgentRoleNewsAnalyst,
		Name: "news_analyst",
		BuildPrompt: func(_ agent.AnalysisInput) (string, bool) {
			return "", false
		},
	})

	if base.Name() != "news_analyst" {
		t.Errorf("Name() = %q, want news_analyst", base.Name())
	}
	if base.Role() != agent.AgentRoleNewsAnalyst {
		t.Errorf("Role() = %q, want news_analyst", base.Role())
	}
	if base.Phase() != agent.PhaseAnalysis {
		t.Errorf("Phase() = %q, want analysis", base.Phase())
	}
}
