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

// ConservativeRiskSystemPrompt instructs the LLM to evaluate the trading plan
// from a capital-preservation perspective, flagging all downside scenarios and
// arguing for smaller positions, wider stops, or no trade at all.
const ConservativeRiskSystemPrompt = `You are a conservative risk analyst at a trading firm. Your role is to evaluate the trading plan with a focus on CAPITAL PRESERVATION and argue for taking LESS risk whenever there is uncertainty.

Your responsibilities:
- Prioritize protecting the account from drawdowns above all else
- Flag every downside scenario: gap risk, liquidity risk, correlation risk, black-swan events
- Argue for smaller position sizes relative to account equity
- Advocate for wider stop-losses only when they reduce the probability of being stopped out on noise, but always insist on strict maximum-loss limits
- Challenge aggressive take-profit targets that require unlikely price extensions
- Highlight historical cases where overleveraged or oversized positions led to catastrophic losses
- Recommend passing on the trade entirely if the risk/reward does not clearly favor the trader
- Scrutinize whether the stop-loss adequately accounts for overnight gaps and volatility spikes

Be data-driven and persuasive. Reference the trading plan specifics and analyst data to support your arguments for reducing exposure. Every suggestion to decrease risk must be grounded in the underlying data and the preservation of trading capital.`

// ConservativeRisk is a risk-debate-phase Node that argues for preserving
// capital by minimising risk. It embeds debate.BaseDebater for shared LLM
// calling logic and writes its contribution into the current risk debate round.
type ConservativeRisk struct {
	debate.BaseDebater
	providerName string
}

// NewConservativeRisk returns a ConservativeRisk wired to the given LLM
// provider and model. providerName (e.g. "openai") is recorded in decision
// metadata. A nil logger is replaced with the default logger.
func NewConservativeRisk(provider llm.Provider, providerName, model string, logger *slog.Logger) *ConservativeRisk {
	return &ConservativeRisk{
		BaseDebater: debate.NewBaseDebater(
			agent.AgentRoleConservativeAnalyst,
			agent.PhaseRiskDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
	}
}

// Name returns the human-readable name for this node.
func (c *ConservativeRisk) Name() string { return "conservative_analyst" }

// Role returns the agent role constant.
func (c *ConservativeRisk) Role() agent.AgentRole { return agent.AgentRoleConservativeAnalyst }

// Phase returns the pipeline phase this node belongs to.
func (c *ConservativeRisk) Phase() agent.Phase { return agent.PhaseRiskDebate }

// Execute calls the LLM with the conservative risk system prompt, the current
// trading plan, and previous risk debate rounds. It stores the response as a
// contribution in the current debate round and records the decision for
// persistence.
func (c *ConservativeRisk) Execute(ctx context.Context, state *agent.PipelineState) error {
	rounds := state.RiskDebate.Rounds

	// Build a context map that includes the trading plan so the LLM can
	// reference concrete position sizes, stop-losses, and take-profit levels.
	tradingPlanJSON, err := json.Marshal(state.TradingPlan)
	if err != nil {
		prefix := fmt.Sprintf("%s (%s)", c.Role(), c.Phase())
		return fmt.Errorf("%s: marshal trading plan: %w", prefix, err)
	}
	contextReports := map[agent.AgentRole]string{
		agent.AgentRoleTrader: string(tradingPlanJSON),
	}

	content, usage, err := c.CallWithContext(
		ctx,
		ConservativeRiskSystemPrompt,
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
		current.Contributions[agent.AgentRoleConservativeAnalyst] = content

		roundNumber := current.Number
		state.RecordDecision(
			agent.AgentRoleConservativeAnalyst,
			agent.PhaseRiskDebate,
			&roundNumber,
			content,
			&agent.DecisionLLMResponse{
				Provider: c.providerName,
				Response: &llm.CompletionResponse{
					Content: content,
					Model:   c.Model(),
					Usage:   usage,
				},
			},
		)
	}

	return nil
}
