package analysts

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// PromptBuilder builds the user prompt from pipeline state.
// Return ("", false) to skip the LLM call (e.g., no fundamentals for crypto).
type PromptBuilder func(state *agent.PipelineState) (userPrompt string, shouldCall bool)

// Compile-time check: *BaseAnalyst implements agent.AnalystNode.
var _ agent.AnalystNode = (*BaseAnalyst)(nil)

// BaseAnalystConfig holds all the parameters needed to construct a BaseAnalyst.
type BaseAnalystConfig struct {
	Provider     llm.Provider
	ProviderName string
	Model        string
	Logger       *slog.Logger
	Role         agent.AgentRole
	Name         string
	SystemPrompt string
	BuildPrompt  PromptBuilder
	SkipMessage  string // message stored when shouldCall=false
}

// BaseAnalyst holds the common dependencies and Execute logic shared by all
// analyst nodes.
type BaseAnalyst struct {
	provider     llm.Provider
	providerName string
	model        string
	logger       *slog.Logger
	role         agent.AgentRole
	name         string
	systemPrompt string
	buildPrompt  PromptBuilder
	skipMessage  string
}

// NewBaseAnalyst creates a BaseAnalyst from the given config. A nil logger is
// replaced with the default logger.
func NewBaseAnalyst(cfg BaseAnalystConfig) BaseAnalyst {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return BaseAnalyst{
		provider:     cfg.Provider,
		providerName: cfg.ProviderName,
		model:        cfg.Model,
		logger:       logger,
		role:         cfg.Role,
		name:         cfg.Name,
		systemPrompt: cfg.SystemPrompt,
		buildPrompt:  cfg.BuildPrompt,
		skipMessage:  cfg.SkipMessage,
	}
}

// Name returns the human-readable name for this node.
func (b *BaseAnalyst) Name() string { return b.name }

// Role returns the agent role used to key reports and decisions in the state.
func (b *BaseAnalyst) Role() agent.AgentRole { return b.role }

// Phase returns the pipeline phase this node belongs to.
func (b *BaseAnalyst) Phase() agent.Phase { return agent.PhaseAnalysis }

// Execute builds the prompt, calls the LLM, and stores the report and decision
// in the pipeline state. When the PromptBuilder returns shouldCall=false the
// LLM is skipped and the configured SkipMessage is stored instead.
func (b *BaseAnalyst) Execute(ctx context.Context, state *agent.PipelineState) error {
	input := agent.AnalysisInput{
		Ticker:       state.Ticker,
		Market:       state.Market,
		News:         state.News,
		Fundamentals: state.Fundamentals,
		Social:       state.Social,
	}
	output, err := b.Analyze(ctx, input)
	if err != nil {
		return err
	}
	state.SetAnalystReport(b.role, output.Report)
	state.RecordDecision(b.role, b.Phase(), nil, output.Report, output.LLMResponse)
	return nil
}

// Analyze implements the AnalystNode interface. It builds the prompt from the
// typed input, calls the LLM, and returns the analysis report. When the
// PromptBuilder returns shouldCall=false the LLM is skipped and the configured
// SkipMessage is returned instead.
func (b *BaseAnalyst) Analyze(ctx context.Context, input agent.AnalysisInput) (agent.AnalysisOutput, error) {
	// Build a minimal PipelineState from the typed input so the existing
	// PromptBuilder closures continue to work without signature changes.
	// TODO: refactor PromptBuilder to accept AnalysisInput directly,
	// eliminating this transitional shim.
	syntheticState := &agent.PipelineState{
		Ticker:       input.Ticker,
		Market:       input.Market,
		News:         input.News,
		Fundamentals: input.Fundamentals,
		Social:       input.Social,
	}

	userPrompt, shouldCall := b.buildPrompt(syntheticState)
	if !shouldCall {
		msg := b.skipMessage
		b.logger.InfoContext(ctx, strings.ReplaceAll(b.name, "_", " ")+" skipped")
		return agent.AnalysisOutput{Report: msg}, nil
	}

	if b.provider == nil {
		return agent.AnalysisOutput{}, fmt.Errorf("%s: provider is nil", b.name)
	}

	resp, err := b.provider.Complete(ctx, llm.CompletionRequest{
		Model: b.model,
		Messages: []llm.Message{
			{Role: "system", Content: b.systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return agent.AnalysisOutput{}, fmt.Errorf("%s: llm completion failed: %w", b.name, err)
	}

	b.logger.InfoContext(ctx, strings.ReplaceAll(b.name, "_", " ")+" report generated",
		slog.Int("prompt_tokens", resp.Usage.PromptTokens),
		slog.Int("completion_tokens", resp.Usage.CompletionTokens),
	)

	return agent.AnalysisOutput{
		Report: resp.Content,
		LLMResponse: &agent.DecisionLLMResponse{
			Provider: b.providerName,
			Response: resp,
		},
	}, nil
}
