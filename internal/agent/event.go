package agent

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PipelineEventType identifies a user-visible state transition in the pipeline.
type PipelineEventType string

const (
	PipelineStarted       PipelineEventType = "pipeline_started"
	AgentDecisionMade     PipelineEventType = "agent_decision_made"
	DebateRoundCompleted  PipelineEventType = "debate_round_completed"
	SignalGenerated       PipelineEventType = "signal_generated"
	LLMCacheStatsReported PipelineEventType = "llm_cache_stats_reported"
	PipelineCompleted     PipelineEventType = "pipeline_completed"
	PipelineError         PipelineEventType = "pipeline_error"
)

// String returns the string representation of a PipelineEventType.
func (t PipelineEventType) String() string {
	return string(t)
}

// PipelineEvent is emitted for user-visible pipeline state changes.
type PipelineEvent struct {
	Type          PipelineEventType `json:"type"`
	PipelineRunID uuid.UUID         `json:"pipeline_run_id"`
	StrategyID    uuid.UUID         `json:"strategy_id"`
	Ticker        string            `json:"ticker,omitempty"`
	AgentRole     AgentRole         `json:"agent_role,omitempty"`
	Phase         Phase             `json:"phase,omitempty"`
	Round         int               `json:"round,omitempty"`
	Payload       json.RawMessage   `json:"payload,omitempty"`
	Error         string            `json:"error,omitempty"`
	OccurredAt    time.Time         `json:"occurred_at"`
}
