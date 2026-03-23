package debate

import (
	"context"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// BearResearcherSystemPrompt instructs the LLM to act as a senior bear
// researcher who identifies risks, argues the bearish case, counters bull
// arguments, and flags data weaknesses.
const BearResearcherSystemPrompt = `You are a senior bear researcher at a trading firm. Your role is to identify every possible risk and build the strongest case AGAINST buying the current asset.

Your responsibilities:
- Identify all risks, weaknesses, and red flags in the data
- Argue the bearish/short case with conviction
- Counter every bullish argument with specific evidence
- Flag any data gaps, unreliable metrics, or overly optimistic assumptions
- Highlight potential catalysts for downside price movement
- Question the quality and reliability of the underlying data

Be thorough, skeptical, and data-driven. Challenge the bull's thesis point by point. Every claim must reference specific data from the analyst reports.`

// BearResearcher is a research-debate-phase Node that builds the bearish case
// against the asset under review. It embeds BaseDebater for shared LLM calling
// logic and writes its contribution into the current debate round.
type BearResearcher struct {
	BaseDebater
	providerName string
}

// NewBearResearcher returns a BearResearcher wired to the given LLM provider
// and model. providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewBearResearcher(provider llm.Provider, providerName, model string, logger *slog.Logger) *BearResearcher {
	return &BearResearcher{
		BaseDebater: NewBaseDebater(
			agent.AgentRoleBearResearcher,
			agent.PhaseResearchDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
	}
}

// Name returns the human-readable name for this node.
func (b *BearResearcher) Name() string { return "bear_researcher" }

// Role returns the agent role constant.
func (b *BearResearcher) Role() agent.AgentRole { return agent.AgentRoleBearResearcher }

// Phase returns the pipeline phase this node belongs to.
func (b *BearResearcher) Phase() agent.Phase { return agent.PhaseResearchDebate }

// Execute calls the LLM with the bear researcher system prompt, previous
// debate rounds, and analyst reports. It stores the response as a contribution
// in the current debate round and records the decision for persistence.
func (b *BearResearcher) Execute(ctx context.Context, state *agent.PipelineState) error {
	rounds := state.ResearchDebate.Rounds

	content, usage, err := b.CallWithContext(
		ctx,
		BearResearcherSystemPrompt,
		rounds,
		state.AnalystReports,
	)
	if err != nil {
		return err
	}

	// Store the contribution in the current (last) debate round and record
	// the decision so the pipeline can persist it with LLM metadata.
	if len(rounds) > 0 {
		current := &state.ResearchDebate.Rounds[len(rounds)-1]
		if current.Contributions == nil {
			current.Contributions = make(map[agent.AgentRole]string)
		}
		current.Contributions[agent.AgentRoleBearResearcher] = content

		roundNumber := current.Number
		state.RecordDecision(
			agent.AgentRoleBearResearcher,
			agent.PhaseResearchDebate,
			&roundNumber,
			content,
			&agent.DecisionLLMResponse{
				Provider: b.providerName,
				Response: &llm.CompletionResponse{
					Content: content,
					Model:   b.model,
					Usage:   usage,
				},
			},
		)
	}

	return nil
}
