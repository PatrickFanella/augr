package agent

import (
	"encoding/json"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

// AnalysisInput provides read-only context for analyst nodes.
type AnalysisInput struct {
	Ticker       string
	Market       *MarketData
	News         []data.NewsArticle
	Fundamentals *data.Fundamentals
	Social       *data.SocialSentiment
}

// AnalysisOutput is the result of an analyst node's execution.
type AnalysisOutput struct {
	Report      string
	LLMResponse *DecisionLLMResponse
}

// DebateInput provides the accumulated debate context for a debater node.
type DebateInput struct {
	Ticker         string
	Rounds         []DebateRound
	ContextReports map[AgentRole]string
}

// DebateOutput is the result of a debater node's execution.
type DebateOutput struct {
	Contribution string
	LLMResponse  *DecisionLLMResponse
}

// TradingInput provides the research debate results for the trader node.
type TradingInput struct {
	Ticker         string
	InvestmentPlan string
	AnalystReports map[AgentRole]string
}

// TradingOutput is the result of the trader node's execution.
type TradingOutput struct {
	Plan         TradingPlan
	StoredOutput string
	LLMResponse  *DecisionLLMResponse
}

// RiskJudgeInput provides the risk debate results and trading plan for the risk manager.
type RiskJudgeInput struct {
	Ticker      string
	Rounds      []DebateRound
	TradingPlan TradingPlan
}

// RiskJudgeOutput is the result of the risk manager node's execution.
type RiskJudgeOutput struct {
	FinalSignal  FinalSignal
	StoredSignal string
	TradingPlan  TradingPlan // potentially risk-adjusted
	LLMResponse  *DecisionLLMResponse
}

// analysisInputFromState constructs an AnalysisInput from the pipeline state.
func analysisInputFromState(state *PipelineState) AnalysisInput {
	return AnalysisInput{
		Ticker:       state.Ticker,
		Market:       state.Market,
		News:         state.News,
		Fundamentals: state.Fundamentals,
		Social:       state.Social,
	}
}

// applyAnalysisOutput maps an AnalysisOutput back to the pipeline state.
func applyAnalysisOutput(state *PipelineState, role AgentRole, output AnalysisOutput) {
	state.SetAnalystReport(role, output.Report)
	if output.LLMResponse != nil {
		state.RecordDecision(role, PhaseAnalysis, nil, output.Report, output.LLMResponse)
	}
}

// debateInputFromState constructs a DebateInput from the pipeline state for
// research debate nodes.
func debateInputFromState(state *PipelineState) DebateInput {
	return DebateInput{
		Ticker:         state.Ticker,
		Rounds:         state.ResearchDebate.Rounds,
		ContextReports: state.AnalystReports,
	}
}

// riskDebateInputFromState constructs a DebateInput from the pipeline state for
// risk debate nodes, including the trading plan as context.
func riskDebateInputFromState(state *PipelineState) DebateInput {
	return DebateInput{
		Ticker: state.Ticker,
		Rounds: state.RiskDebate.Rounds,
		ContextReports: map[AgentRole]string{
			AgentRoleTrader: MarshalTradingPlanSafe(state.TradingPlan),
		},
	}
}

// ApplyDebateOutput maps a DebateOutput back to the appropriate debate round
// in the pipeline state. It stores the contribution in the current (last) round
// and records the decision.
func ApplyDebateOutput(state *PipelineState, role AgentRole, phase Phase, rounds []DebateRound, output DebateOutput) {
	if len(rounds) == 0 {
		return
	}

	var current *DebateRound
	switch phase {
	case PhaseResearchDebate:
		current = &state.ResearchDebate.Rounds[len(rounds)-1]
	case PhaseRiskDebate:
		current = &state.RiskDebate.Rounds[len(rounds)-1]
	default:
		return
	}

	if current.Contributions == nil {
		current.Contributions = make(map[AgentRole]string)
	}
	current.Contributions[role] = output.Contribution

	roundNumber := current.Number
	state.RecordDecision(role, phase, &roundNumber, output.Contribution, output.LLMResponse)
}

// tradingInputFromState constructs a TradingInput from the pipeline state.
func tradingInputFromState(state *PipelineState) TradingInput {
	return TradingInput{
		Ticker:         state.Ticker,
		InvestmentPlan: state.ResearchDebate.InvestmentPlan,
		AnalystReports: state.AnalystReports,
	}
}

// applyTradingOutput maps a TradingOutput back to the pipeline state.
func applyTradingOutput(state *PipelineState, output TradingOutput) {
	state.TradingPlan = output.Plan
	state.RecordDecision(AgentRoleTrader, PhaseTrading, nil, output.StoredOutput, output.LLMResponse)
}

// riskJudgeInputFromState constructs a RiskJudgeInput from the pipeline state.
func riskJudgeInputFromState(state *PipelineState) RiskJudgeInput {
	return RiskJudgeInput{
		Ticker:      state.Ticker,
		Rounds:      state.RiskDebate.Rounds,
		TradingPlan: state.TradingPlan,
	}
}

// applyRiskJudgeOutput maps a RiskJudgeOutput back to the pipeline state.
func applyRiskJudgeOutput(state *PipelineState, output RiskJudgeOutput) {
	state.FinalSignal = output.FinalSignal
	state.TradingPlan = output.TradingPlan
	state.RiskDebate.FinalSignal = output.StoredSignal
	state.RecordDecision(AgentRoleRiskManager, PhaseRiskDebate, nil, output.StoredSignal, output.LLMResponse)
}

// MarshalTradingPlanSafe marshals the trading plan to JSON, returning an empty
// object on error.
func MarshalTradingPlanSafe(plan TradingPlan) string {
	data, err := json.Marshal(plan)
	if err != nil {
		return "{}"
	}
	return string(data)
}
