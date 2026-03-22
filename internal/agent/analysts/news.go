package analysts

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// NewsAnalyst is an analysis-phase Node that calls the LLM with recent news
// articles and stores the resulting sentiment and catalyst report in the
// pipeline state. When no news articles are available it skips the LLM call
// and stores a static message instead.
type NewsAnalyst struct {
	BaseAnalyst
}

// NewNewsAnalyst returns a NewsAnalyst wired to the given LLM provider and
// model. providerName (e.g. "openai") is recorded in decision metadata. A nil
// logger is replaced with the default logger.
func NewNewsAnalyst(provider llm.Provider, providerName, model string, logger *slog.Logger) *NewsAnalyst {
	return &NewsAnalyst{
		BaseAnalyst: NewBaseAnalyst(provider, providerName, model, logger),
	}
}

// Name returns the human-readable name for this node.
func (n *NewsAnalyst) Name() string { return "news_analyst" }

// Role returns the agent role used to key reports and decisions in the state.
func (n *NewsAnalyst) Role() agent.AgentRole { return agent.AgentRoleNewsAnalyst }

// Phase returns the pipeline phase this node belongs to.
func (n *NewsAnalyst) Phase() agent.Phase { return agent.PhaseAnalysis }

// Execute formats the news articles from state into a prompt, calls the LLM,
// and stores the analysis report in state. When no articles are present it
// records a static message without invoking the LLM.
func (n *NewsAnalyst) Execute(ctx context.Context, state *agent.PipelineState) error {
	if len(state.News) == 0 {
		const noDataMsg = "No news articles available. Unable to perform news analysis."
		n.logger.InfoContext(ctx, "news analyst skipped: no articles available")
		state.SetAnalystReport(n.Role(), noDataMsg)
		state.RecordDecision(n.Role(), n.Phase(), nil, noDataMsg, nil)
		return nil
	}

	if n.provider == nil {
		return fmt.Errorf("news_analyst: provider is nil")
	}

	userPrompt := FormatNewsAnalystUserPrompt(state.Ticker, state.News)

	resp, err := n.provider.Complete(ctx, llm.CompletionRequest{
		Model: n.model,
		Messages: []llm.Message{
			{Role: "system", Content: NewsAnalystSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return fmt.Errorf("news_analyst: llm completion failed: %w", err)
	}

	n.logger.InfoContext(ctx, "news analyst report generated",
		slog.Int("prompt_tokens", resp.Usage.PromptTokens),
		slog.Int("completion_tokens", resp.Usage.CompletionTokens),
	)

	state.SetAnalystReport(n.Role(), resp.Content)
	state.RecordDecision(n.Role(), n.Phase(), nil, resp.Content, &agent.DecisionLLMResponse{
		Provider: n.providerName,
		Response: resp,
	})

	return nil
}
