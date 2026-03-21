package agent

import (
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type AgentRole = domain.AgentRole

const (
	AgentRoleMarketAnalyst  = domain.AgentRoleMarketAnalyst
	AgentRoleBullResearcher = domain.AgentRoleBullResearcher
	AgentRoleBearResearcher = domain.AgentRoleBearResearcher
	AgentRoleTrader         = domain.AgentRoleTrader
	AgentRoleInvestJudge    = domain.AgentRoleInvestJudge
	AgentRoleRiskManager    = domain.AgentRoleRiskManager
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
	AnalystReports map[AgentRole]string `json:"analyst_reports,omitempty"`
	ResearchDebate ResearchDebateState  `json:"research_debate"`
	TradingPlan    TradingPlan          `json:"trading_plan"`
	RiskDebate     RiskDebateState      `json:"risk_debate"`
	FinalSignal    FinalSignal          `json:"final_signal"`
	// Errors holds internal errors encountered during pipeline execution.
	// It is intentionally excluded from JSON output via `json:"-"`.
	Errors []error `json:"-"`
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
