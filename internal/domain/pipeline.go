package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PipelineStatus represents the execution status of a pipeline run.
type PipelineStatus string

const (
	PipelineStatusRunning   PipelineStatus = "running"
	PipelineStatusCompleted PipelineStatus = "completed"
	PipelineStatusFailed    PipelineStatus = "failed"
	PipelineStatusCancelled PipelineStatus = "cancelled"
)

// String returns the string representation of a PipelineStatus.
func (s PipelineStatus) String() string {
	return string(s)
}

// PipelineSignal represents the trading signal output of a pipeline run.
type PipelineSignal string

const (
	PipelineSignalBuy  PipelineSignal = "buy"
	PipelineSignalSell PipelineSignal = "sell"
	PipelineSignalHold PipelineSignal = "hold"
)

// String returns the string representation of a PipelineSignal.
func (s PipelineSignal) String() string {
	return string(s)
}

// PipelineRun represents a single execution of a trading strategy pipeline.
type PipelineRun struct {
	ID             uuid.UUID       `json:"id"`
	StrategyID     uuid.UUID       `json:"strategy_id"`
	Ticker         string          `json:"ticker"`
	TradeDate      time.Time       `json:"trade_date"`
	Status         PipelineStatus  `json:"status"`
	Signal         PipelineSignal  `json:"signal,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	ConfigSnapshot json.RawMessage `json:"config_snapshot,omitempty"`
}

// PipelineEvent represents a structured event emitted during a pipeline run.
type PipelineEvent struct {
	PipelineRunID uuid.UUID       `json:"pipeline_run_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
}
