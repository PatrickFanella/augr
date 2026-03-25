package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentRole identifies the role an agent plays in the pipeline.
type AgentRole string

const (
	AgentRoleMarketAnalyst       AgentRole = "market_analyst"
	AgentRoleFundamentalsAnalyst AgentRole = "fundamentals_analyst"
	AgentRoleNewsAnalyst         AgentRole = "news_analyst"
	AgentRoleSocialMediaAnalyst  AgentRole = "social_media_analyst"
	AgentRoleBullResearcher      AgentRole = "bull_researcher"
	AgentRoleBearResearcher      AgentRole = "bear_researcher"
	AgentRoleTrader              AgentRole = "trader"
	AgentRoleInvestJudge         AgentRole = "invest_judge"
	AgentRoleRiskManager         AgentRole = "risk_manager"
	AgentRoleAggressiveAnalyst   AgentRole = "aggressive_analyst"
	AgentRoleConservativeAnalyst AgentRole = "conservative_analyst"
	AgentRoleNeutralAnalyst      AgentRole = "neutral_analyst"
	AgentRoleAggressiveRisk      AgentRole = "aggressive_risk"
	AgentRoleConservativeRisk    AgentRole = "conservative_risk"
	AgentRoleNeutralRisk         AgentRole = "neutral_risk"
)

// String returns the string representation of an AgentRole.
func (r AgentRole) String() string {
	return string(r)
}

// Phase identifies which phase of the pipeline an agent decision belongs to.
type Phase string

const (
	PhaseAnalysis       Phase = "analysis"
	PhaseResearchDebate Phase = "research_debate"
	PhaseTrading        Phase = "trading"
	PhaseRiskDebate     Phase = "risk_debate"
)

// String returns the string representation of a Phase.
func (p Phase) String() string {
	return string(p)
}

// ValidAgentRoles contains all defined agent roles.
var ValidAgentRoles = map[AgentRole]bool{
	AgentRoleMarketAnalyst:       true,
	AgentRoleFundamentalsAnalyst: true,
	AgentRoleNewsAnalyst:         true,
	AgentRoleSocialMediaAnalyst:  true,
	AgentRoleBullResearcher:      true,
	AgentRoleBearResearcher:      true,
	AgentRoleTrader:              true,
	AgentRoleInvestJudge:         true,
	AgentRoleRiskManager:         true,
	AgentRoleAggressiveAnalyst:   true,
	AgentRoleConservativeAnalyst: true,
	AgentRoleNeutralAnalyst:      true,
	AgentRoleAggressiveRisk:      true,
	AgentRoleConservativeRisk:    true,
	AgentRoleNeutralRisk:         true,
}

// IsValid returns true if the role is a defined AgentRole constant.
func (r AgentRole) IsValid() bool {
	return ValidAgentRoles[r]
}

// ValidPhases contains all defined pipeline phases.
var ValidPhases = map[Phase]bool{
	PhaseAnalysis:       true,
	PhaseResearchDebate: true,
	PhaseTrading:        true,
	PhaseRiskDebate:     true,
}

// IsValid returns true if the phase is a defined Phase constant.
func (p Phase) IsValid() bool {
	return ValidPhases[p]
}

// AgentDecision stores the output of an agent during a pipeline run.
type AgentDecision struct {
	ID               uuid.UUID       `json:"id"`
	PipelineRunID    uuid.UUID       `json:"pipeline_run_id"`
	AgentRole        AgentRole       `json:"agent_role"`
	Phase            Phase           `json:"phase"`
	RoundNumber      *int            `json:"round_number,omitempty"`
	InputSummary     string          `json:"input_summary,omitempty"`
	OutputText       string          `json:"output_text"`
	OutputStructured json.RawMessage `json:"output_structured,omitempty"`
	LLMProvider      string          `json:"llm_provider,omitempty"`
	LLMModel         string          `json:"llm_model,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	LatencyMS        int             `json:"latency_ms,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}
