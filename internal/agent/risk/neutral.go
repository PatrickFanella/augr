package risk

import (
	"context"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/debate"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// NeutralRiskSystemPrompt instructs the LLM to evaluate the trading plan from
// a balanced perspective, weighing both upside potential and downside risk
// through expected value and probability-weighted outcomes.
const NeutralRiskSystemPrompt = `You are a neutral risk analyst at a trading firm. Your role is to provide a balanced, objective assessment of the trading plan by weighing both upside potential and downside risk without bias toward either.

Your responsibilities:
- Evaluate trades using expected value and probability-weighted outcomes rather than best-case or worst-case scenarios alone
- Consider both the upside potential and downside risk objectively, giving each fair weight
- Assess whether the risk/reward ratio is accurately priced given current market conditions and volatility
- Identify assumptions in the trading plan that may skew the risk assessment in either direction
- Provide a clear probability-weighted framework: likelihood of hitting take-profit vs stop-loss vs other outcomes
- Challenge both overly aggressive and overly conservative arguments with data-driven reasoning
- Evaluate position sizing relative to conviction level and expected value, not just maximum loss
- Consider correlation risk, portfolio context, and how this trade fits the overall risk budget

Be data-driven and impartial. Reference the trading plan specifics and analyst data to support your balanced assessment. Every conclusion must be grounded in objective analysis of the underlying data and probabilities.`

// NeutralRisk is a risk-debate-phase Node that provides a balanced assessment
// of both upside and downside. It embeds debate.BaseDebater for shared LLM
// calling logic and writes its contribution into the current risk debate round.
type NeutralRisk struct {
	debate.BaseDebater
	providerName string
}

// Compile-time check: *NeutralRisk implements agent.DebaterNode.
var _ agent.DebaterNode = (*NeutralRisk)(nil)

// NewNeutralRisk returns a NeutralRisk wired to the given LLM provider and
// model. providerName (e.g. "openai") is recorded in decision metadata. A nil
// logger is replaced with the default logger.
func NewNeutralRisk(provider llm.Provider, providerName, model string, logger *slog.Logger) *NeutralRisk {
	return &NeutralRisk{
		BaseDebater: debate.NewBaseDebater(
			agent.AgentRoleNeutralAnalyst,
			agent.PhaseRiskDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
	}
}

// Name returns the human-readable name for this node.
func (n *NeutralRisk) Name() string { return "neutral_analyst" }

// Role returns the agent role constant.
func (n *NeutralRisk) Role() agent.AgentRole { return agent.AgentRoleNeutralAnalyst }

// Phase returns the pipeline phase this node belongs to.
func (n *NeutralRisk) Phase() agent.Phase { return agent.PhaseRiskDebate }

// Execute calls the LLM with the neutral risk system prompt, the current
// trading plan, and previous risk debate rounds. It stores the response as a
// contribution in the current debate round and records the decision for
// persistence.
func (n *NeutralRisk) Execute(ctx context.Context, state *agent.PipelineState) error {
	return executeRiskDebate(ctx, state, n.BaseDebater, agent.AgentRoleNeutralAnalyst, NeutralRiskSystemPrompt, n.providerName)
}

// Debate implements the DebaterNode interface. It calls the LLM with the
// neutral risk system prompt and returns the debate contribution.
func (n *NeutralRisk) Debate(ctx context.Context, input agent.DebateInput) (agent.DebateOutput, error) {
	return debateRiskFromInput(ctx, n.BaseDebater, NeutralRiskSystemPrompt, n.providerName, input)
}
