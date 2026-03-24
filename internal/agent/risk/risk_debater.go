package risk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/debate"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// executeRiskDebate contains the shared Execute logic for risk debate agents.
// It marshals the trading plan as LLM context, calls the LLM with the given
// system prompt, stores the contribution in the current debate round, and
// records the decision with full LLM metadata.
func executeRiskDebate(
	ctx context.Context,
	state *agent.PipelineState,
	debater debate.BaseDebater,
	role agent.AgentRole,
	systemPrompt string,
	providerName string,
) error {
	rounds := state.RiskDebate.Rounds

	// Build a context map that includes the trading plan so the LLM can
	// reference concrete position sizes, stop-losses, and take-profit levels.
	tradingPlanJSON, err := json.Marshal(state.TradingPlan)
	if err != nil {
		prefix := fmt.Sprintf("%s (%s)", role, agent.PhaseRiskDebate)
		return fmt.Errorf("%s: marshal trading plan: %w", prefix, err)
	}
	contextReports := map[agent.AgentRole]string{
		agent.AgentRoleTrader: string(tradingPlanJSON),
	}

	content, usage, err := debater.CallWithContext(
		ctx,
		systemPrompt,
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
		current.Contributions[role] = content

		roundNumber := current.Number
		state.RecordDecision(
			role,
			agent.PhaseRiskDebate,
			&roundNumber,
			content,
			&agent.DecisionLLMResponse{
				Provider: providerName,
				Response: &llm.CompletionResponse{
					Content: content,
					Model:   debater.Model(),
					Usage:   usage,
				},
			},
		)
	}

	return nil
}
