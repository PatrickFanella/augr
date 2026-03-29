package debate

import (
	"context"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// BullResearcherSystemPrompt instructs the LLM to act as a senior bull
// researcher who identifies opportunities, argues the bullish case, counters
// bear arguments, and highlights data strengths.
const BullResearcherSystemPrompt = `You are a senior bull researcher at a trading firm. Your role is to identify every possible opportunity and build the strongest case FOR buying the current asset.

Your responsibilities:
- Identify all strengths, catalysts, and positive signals in the data
- Argue the bullish/long case with conviction
- Counter every bearish argument with specific evidence
- Highlight data quality, reliable metrics, and conservatively positive assumptions
- Identify potential catalysts for upside price movement
- Emphasize the quality and reliability of supporting data

Be thorough, optimistic, and data-driven. Challenge the bear's thesis point by point. Every claim must reference specific data from the analyst reports.`

// BullResearcher is a research-debate-phase Node that builds the bullish case
// for the asset under review. It embeds BaseDebater for shared LLM calling
// logic and writes its contribution into the current debate round.
type BullResearcher struct {
	BaseDebater
	providerName string
}

// Compile-time check: *BullResearcher implements agent.DebaterNode.
var _ agent.DebaterNode = (*BullResearcher)(nil)

// NewBullResearcher returns a BullResearcher wired to the given LLM provider
// and model. providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewBullResearcher(provider llm.Provider, providerName, model string, logger *slog.Logger) *BullResearcher {
	return &BullResearcher{
		BaseDebater: NewBaseDebater(
			agent.AgentRoleBullResearcher,
			agent.PhaseResearchDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
	}
}

// Name returns the human-readable name for this node.
func (b *BullResearcher) Name() string { return "bull_researcher" }

// Role returns the agent role constant.
func (b *BullResearcher) Role() agent.AgentRole { return agent.AgentRoleBullResearcher }

// Phase returns the pipeline phase this node belongs to.
func (b *BullResearcher) Phase() agent.Phase { return agent.PhaseResearchDebate }

// Execute calls the LLM with the bull researcher system prompt, previous
// debate rounds, and analyst reports. It stores the response as a contribution
// in the current debate round and records the decision for persistence.
func (b *BullResearcher) Execute(ctx context.Context, state *agent.PipelineState) error {
	input := agent.DebateInput{
		Ticker:         state.Ticker,
		Rounds:         state.ResearchDebate.Rounds,
		ContextReports: state.AnalystReports,
	}
	output, err := b.Debate(ctx, input)
	if err != nil {
		return err
	}
	agent.ApplyDebateOutput(state, b.Role(), b.Phase(), state.ResearchDebate.Rounds, output)
	return nil
}

// Debate implements the DebaterNode interface. It calls the LLM with the bull
// researcher system prompt, previous debate rounds, and context reports, and
// returns the debate contribution.
func (b *BullResearcher) Debate(ctx context.Context, input agent.DebateInput) (agent.DebateOutput, error) {
	content, usage, err := b.CallWithContext(
		ctx,
		BullResearcherSystemPrompt,
		input.Rounds,
		input.ContextReports,
	)
	if err != nil {
		return agent.DebateOutput{}, err
	}

	return agent.DebateOutput{
		Contribution: content,
		LLMResponse: &agent.DecisionLLMResponse{
			Provider: b.providerName,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   b.model,
				Usage:   usage,
			},
		},
	}, nil
}
