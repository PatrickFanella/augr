package agent

import (
	"sync"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// MarketData bundles the OHLCV bars and technical indicators that earlier
// pipeline stages have collected for the current ticker.
type MarketData struct {
	Bars       []domain.OHLCV     `json:"bars,omitempty"`
	Indicators []domain.Indicator `json:"indicators,omitempty"`
}

type AgentRole = domain.AgentRole

const (
	AgentRoleMarketAnalyst       = domain.AgentRoleMarketAnalyst
	AgentRoleFundamentalsAnalyst = domain.AgentRoleFundamentalsAnalyst
	AgentRoleBullResearcher      = domain.AgentRoleBullResearcher
	AgentRoleBearResearcher      = domain.AgentRoleBearResearcher
	AgentRoleTrader              = domain.AgentRoleTrader
	AgentRoleInvestJudge         = domain.AgentRoleInvestJudge
	AgentRoleRiskManager         = domain.AgentRoleRiskManager
	AgentRoleAggressiveAnalyst   = domain.AgentRoleAggressiveAnalyst
	AgentRoleConservativeAnalyst = domain.AgentRoleConservativeAnalyst
	AgentRoleNeutralAnalyst      = domain.AgentRoleNeutralAnalyst
	AgentRoleNewsAnalyst         = domain.AgentRoleNewsAnalyst
)

type Phase = domain.Phase

const (
	PhaseAnalysis       = domain.PhaseAnalysis
	PhaseResearchDebate = domain.PhaseResearchDebate
	PhaseTrading        = domain.PhaseTrading
	PhaseRiskDebate     = domain.PhaseRiskDebate
)

type PipelineSignal = domain.PipelineSignal

const (
	PipelineSignalBuy  = domain.PipelineSignalBuy
	PipelineSignalSell = domain.PipelineSignalSell
	PipelineSignalHold = domain.PipelineSignalHold
)

// PipelineState carries the mutable state shared across all pipeline phases.
type PipelineState struct {
	PipelineRunID  uuid.UUID            `json:"pipeline_run_id"`
	StrategyID     uuid.UUID            `json:"strategy_id"`
	Ticker         string               `json:"ticker"`
	Market         *MarketData          `json:"market,omitempty"`
	News           []data.NewsArticle   `json:"news,omitempty"`
	Fundamentals   *data.Fundamentals   `json:"fundamentals,omitempty"`
	AnalystReports map[AgentRole]string `json:"analyst_reports,omitempty"`
	ResearchDebate ResearchDebateState  `json:"research_debate"`
	TradingPlan    TradingPlan          `json:"trading_plan"`
	RiskDebate     RiskDebateState      `json:"risk_debate"`
	FinalSignal    FinalSignal          `json:"final_signal"`
	// Errors holds internal errors encountered during pipeline execution.
	// It is intentionally excluded from JSON output via `json:"-"`.
	Errors []error `json:"-"`
	// mu protects concurrent writes to AnalystReports during the analysis phase.
	// It is a pointer so that copying PipelineState does not copy a sync.Mutex by value.
	mu *sync.Mutex
	// decisions stores per-node outputs and optional LLM metadata for persistence.
	// It is intentionally excluded from JSON output.
	decisions map[decisionKey]NodeDecision
}

// SetAnalystReport stores the analyst report for the given role in a thread-safe manner.
func (s *PipelineState) SetAnalystReport(role AgentRole, report string) {
	s.ensureMutex()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AnalystReports == nil {
		s.AnalystReports = make(map[AgentRole]string)
	}
	s.AnalystReports[role] = report
}

// DecisionLLMResponse captures the persisted LLM metadata for a node decision.
type DecisionLLMResponse struct {
	Provider string                  `json:"provider,omitempty"`
	Response *llm.CompletionResponse `json:"response,omitempty"`
}

// NodeDecision stores a node's output text and optional LLM metadata.
type NodeDecision struct {
	OutputText  string               `json:"output_text"`
	LLMResponse *DecisionLLMResponse `json:"llm_response,omitempty"`
}

type decisionKey struct {
	role     AgentRole
	phase    Phase
	round    int
	hasRound bool
}

// RecordDecision stores a node decision so the pipeline can persist it after execution.
func (s *PipelineState) RecordDecision(role AgentRole, phase Phase, roundNumber *int, output string, llmResponse *DecisionLLMResponse) {
	s.ensureMutex()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.decisions == nil {
		s.decisions = make(map[decisionKey]NodeDecision)
	}

	s.decisions[newDecisionKey(role, phase, roundNumber)] = NodeDecision{
		OutputText:  output,
		LLMResponse: llmResponse,
	}
}

// Decision returns a recorded node decision, if one has been stored on the state.
func (s *PipelineState) Decision(role AgentRole, phase Phase, roundNumber *int) (NodeDecision, bool) {
	s.ensureMutex()
	s.mu.Lock()
	defer s.mu.Unlock()

	decision, ok := s.decisions[newDecisionKey(role, phase, roundNumber)]
	return decision, ok
}

func (s *PipelineState) ensureMutex() {
	if s.mu == nil {
		s.mu = &sync.Mutex{}
	}
}

func newDecisionKey(role AgentRole, phase Phase, roundNumber *int) decisionKey {
	key := decisionKey{
		role:  role,
		phase: phase,
	}
	if roundNumber != nil {
		key.round = *roundNumber
		key.hasRound = true
	}
	return key
}

// DebateRound stores the contributions made during a single debate round.
type DebateRound struct {
	Number        int                  `json:"number"`
	Contributions map[AgentRole]string `json:"contributions,omitempty"`
}

// ResearchDebateState stores the state accumulated during the research debate phase.
type ResearchDebateState struct {
	Rounds         []DebateRound `json:"rounds,omitempty"`
	InvestmentPlan string        `json:"investment_plan,omitempty"`
}

// TradingPlan stores the structured output produced by the trader phase.
type TradingPlan struct {
	Action       PipelineSignal `json:"action,omitempty"`
	Ticker       string         `json:"ticker,omitempty"`
	EntryType    string         `json:"entry_type,omitempty"`
	EntryPrice   float64        `json:"entry_price,omitempty"`
	PositionSize float64        `json:"position_size,omitempty"`
	StopLoss     float64        `json:"stop_loss,omitempty"`
	TakeProfit   float64        `json:"take_profit,omitempty"`
	TimeHorizon  string         `json:"time_horizon,omitempty"`
	Confidence   float64        `json:"confidence,omitempty"`
	Rationale    string         `json:"rationale,omitempty"`
	RiskReward   float64        `json:"risk_reward,omitempty"`
}

// RiskDebateState stores the state accumulated during the risk debate phase.
type RiskDebateState struct {
	Rounds      []DebateRound `json:"rounds,omitempty"`
	FinalSignal string        `json:"final_signal,omitempty"`
}

// FinalSignal stores the extracted pipeline signal and confidence.
type FinalSignal struct {
	Signal     PipelineSignal `json:"signal,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
}
