package analysts

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// MarketAnalyst is an analysis-phase Node that calls the LLM with technical
// market data (OHLCV bars and indicators) and stores the resulting report in
// the pipeline state.
type MarketAnalyst struct {
	BaseAnalyst
}

// NewMarketAnalyst returns a MarketAnalyst wired to the given LLM provider and
// model. providerName (e.g. "openai") is recorded in decision metadata. A nil
// logger is replaced with the default logger.
func NewMarketAnalyst(provider llm.Provider, providerName, model string, logger *slog.Logger) *MarketAnalyst {
	return &MarketAnalyst{
		BaseAnalyst: NewBaseAnalyst(provider, providerName, model, logger),
	}
}

// Name returns the human-readable name for this node.
func (m *MarketAnalyst) Name() string { return "market_analyst" }

// Role returns the agent role used to key reports and decisions in the state.
func (m *MarketAnalyst) Role() agent.AgentRole { return agent.AgentRoleMarketAnalyst }

// Phase returns the pipeline phase this node belongs to.
func (m *MarketAnalyst) Phase() agent.Phase { return agent.PhaseAnalysis }

// Execute formats the market data from state into a prompt, calls the LLM, and
// stores the analysis report in state.
func (m *MarketAnalyst) Execute(ctx context.Context, state *agent.PipelineState) error {
	if m.provider == nil {
		return fmt.Errorf("market_analyst: provider is nil")
	}

	var bars []domain.OHLCV
	var indicators []domain.Indicator
	if state.Market != nil {
		bars = state.Market.Bars
		indicators = state.Market.Indicators
	}

	userPrompt := FormatMarketAnalystUserPrompt(
		state.Ticker,
		bars,
		indicators,
	)

	resp, err := m.provider.Complete(ctx, llm.CompletionRequest{
		Model: m.model,
		Messages: []llm.Message{
			{Role: "system", Content: MarketAnalystSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return fmt.Errorf("market_analyst: llm completion failed: %w", err)
	}

	m.logger.InfoContext(ctx, "market analyst report generated",
		slog.Int("prompt_tokens", resp.Usage.PromptTokens),
		slog.Int("completion_tokens", resp.Usage.CompletionTokens),
	)

	state.SetAnalystReport(m.Role(), resp.Content)
	state.RecordDecision(m.Role(), m.Phase(), nil, resp.Content, &agent.DecisionLLMResponse{
		Provider: m.providerName,
		Response: resp,
	})

	return nil
}
