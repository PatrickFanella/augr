package risk

import (
	"context"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/debate"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// AggressiveRiskSystemPrompt instructs the LLM to evaluate the trading plan
// from a return-maximisation perspective, arguing for larger positions, viewing
// tight stops as missed opportunity, and focusing on upside potential.
const AggressiveRiskSystemPrompt = `You are an aggressive risk analyst at a trading firm. Your role is to evaluate the trading plan from a return-maximization perspective and argue for taking MORE risk when the opportunity justifies it.

Your responsibilities:
- Advocate for larger position sizes when the risk/reward is favorable
- Argue that overly tight stop-losses are missed opportunities that get shaken out by normal volatility
- Focus on the upside potential and opportunity cost of being too conservative
- Challenge conservative risk limits that may leave significant returns on the table
- Highlight historical cases where larger positions in high-conviction trades paid off
- Push back against unnecessarily cautious position sizing and risk parameters

Be data-driven and persuasive. Reference the trading plan specifics and analyst data to support your arguments for capturing maximum upside. Every suggestion to increase risk must be grounded in the underlying data and opportunity.`

// AggressiveRisk is a risk-debate-phase Node that argues for maximising
// returns by accepting more risk. It embeds debate.BaseDebater for shared LLM
// calling logic and writes its contribution into the current risk debate round.
type AggressiveRisk struct {
	debate.BaseDebater
	providerName string
}

// Compile-time check: *AggressiveRisk implements agent.DebaterNode.
var _ agent.DebaterNode = (*AggressiveRisk)(nil)

// NewAggressiveRisk returns an AggressiveRisk wired to the given LLM provider
// and model. providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewAggressiveRisk(provider llm.Provider, providerName, model string, logger *slog.Logger) *AggressiveRisk {
	return &AggressiveRisk{
		BaseDebater: debate.NewBaseDebater(
			agent.AgentRoleAggressiveAnalyst,
			agent.PhaseRiskDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
	}
}

// Name returns the human-readable name for this node.
func (a *AggressiveRisk) Name() string { return "aggressive_analyst" }

// Role returns the agent role constant.
func (a *AggressiveRisk) Role() agent.AgentRole { return agent.AgentRoleAggressiveAnalyst }

// Phase returns the pipeline phase this node belongs to.
func (a *AggressiveRisk) Phase() agent.Phase { return agent.PhaseRiskDebate }

// Execute calls the LLM with the aggressive risk system prompt, the current
// trading plan, and previous risk debate rounds. It stores the response as a
// contribution in the current debate round and records the decision for
// persistence.
func (a *AggressiveRisk) Execute(ctx context.Context, state *agent.PipelineState) error {
	return executeRiskDebate(ctx, state, a.BaseDebater, agent.AgentRoleAggressiveAnalyst, AggressiveRiskSystemPrompt, a.providerName)
}

// Debate implements the DebaterNode interface. It calls the LLM with the
// aggressive risk system prompt and returns the debate contribution.
func (a *AggressiveRisk) Debate(ctx context.Context, input agent.DebateInput) (agent.DebateOutput, error) {
	return debateRiskFromInput(ctx, a.BaseDebater, AggressiveRiskSystemPrompt, a.providerName, input)
}
