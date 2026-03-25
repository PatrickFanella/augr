package agent

import "github.com/PatrickFanella/get-rich-quick/internal/data"

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
