package risk

import (
	"context"

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
	input := agent.DebateInput{
		Ticker: state.Ticker,
		Rounds: state.RiskDebate.Rounds,
		ContextReports: map[agent.AgentRole]string{
			agent.AgentRoleTrader: agent.MarshalTradingPlanSafe(state.TradingPlan),
		},
	}
	output, err := debateRiskFromInput(ctx, debater, systemPrompt, providerName, input)
	if err != nil {
		return err
	}
	agent.ApplyDebateOutput(state, role, agent.PhaseRiskDebate, state.RiskDebate.Rounds, output)
	return nil
}

// debateRiskFromInput contains the core Debate logic shared by all risk debate
// agents. It calls the LLM with the given system prompt, debate rounds, and
// context reports and returns a DebateOutput.
func debateRiskFromInput(
	ctx context.Context,
	debater debate.BaseDebater,
	systemPrompt string,
	providerName string,
	input agent.DebateInput,
) (agent.DebateOutput, error) {
	content, usage, err := debater.CallWithContext(
		ctx,
		systemPrompt,
		input.Rounds,
		input.ContextReports,
	)
	if err != nil {
		return agent.DebateOutput{}, err
	}

	return agent.DebateOutput{
		Contribution: content,
		LLMResponse: &agent.DecisionLLMResponse{
			Provider: providerName,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   debater.Model(),
				Usage:   usage,
			},
		},
	}, nil
}
