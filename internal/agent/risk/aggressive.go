package risk

import (
	"context"
	"encoding/json"
	"fmt"
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
func (a *AggressiveRisk) Name() string { return "aggressive_risk" }

// Role returns the agent role constant.
func (a *AggressiveRisk) Role() agent.AgentRole { return agent.AgentRoleAggressiveAnalyst }

// Phase returns the pipeline phase this node belongs to.
func (a *AggressiveRisk) Phase() agent.Phase { return agent.PhaseRiskDebate }

// Execute calls the LLM with the aggressive risk system prompt, the current
// trading plan, and previous risk debate rounds. It stores the response as a
// contribution in the current debate round and records the decision for
// persistence.
func (a *AggressiveRisk) Execute(ctx context.Context, state *agent.PipelineState) error {
	rounds := state.RiskDebate.Rounds

	// Build a context map that includes the trading plan so the LLM can
	// reference concrete position sizes, stop-losses, and take-profit levels.
	tradingPlanJSON, err := json.Marshal(state.TradingPlan)
	if err != nil {
		return fmt.Errorf("aggressive_analyst (risk_debate): marshal trading plan: %w", err)
	}
	contextReports := map[agent.AgentRole]string{
		agent.AgentRoleTrader: string(tradingPlanJSON),
	}

	content, usage, err := a.CallWithContext(
		ctx,
		AggressiveRiskSystemPrompt,
		rounds,
		contextReports,
	)
	if err != nil {
		return err
	}

	// Store the contribution in the current (last) debate round and record
	// the decision so the pipeline can persist it with LLM metadata.
	if len(rounds) > 0 {
		current := &state.RiskDebate.Rounds[len(rounds)-1]
		if current.Contributions == nil {
			current.Contributions = make(map[agent.AgentRole]string)
		}
		current.Contributions[agent.AgentRoleAggressiveAnalyst] = content

		roundNumber := current.Number
		state.RecordDecision(
			agent.AgentRoleAggressiveAnalyst,
			agent.PhaseRiskDebate,
			&roundNumber,
			content,
			&agent.DecisionLLMResponse{
				Provider: a.providerName,
				Response: &llm.CompletionResponse{
					Content: content,
					Model:   a.Model(),
					Usage:   usage,
				},
			},
		)
	}

	return nil
}
