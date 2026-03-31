package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// DecisionPersister abstracts pipeline run and decision persistence.
type DecisionPersister interface {
	// RecordRunStart persists a new pipeline run record.
	RecordRunStart(ctx context.Context, run *domain.PipelineRun) error
	// RecordRunComplete updates the pipeline run status on completion or failure.
	// It uses its own timeout to avoid being blocked by the caller's context.
	RecordRunComplete(ctx context.Context, runID uuid.UUID, tradeDate time.Time, status domain.PipelineStatus, completedAt time.Time, errMsg string) error
	// PersistSnapshot persists a single pipeline input snapshot.
	PersistSnapshot(ctx context.Context, snapshot *domain.PipelineRunSnapshot) error
	// PersistDecision persists a single agent decision with optional LLM metadata.
	PersistDecision(ctx context.Context, runID uuid.UUID, node Node, roundNumber *int, output string, llmResponse *DecisionLLMResponse) error
	// PersistEvent persists a structured pipeline or agent event.
	PersistEvent(ctx context.Context, event *domain.AgentEvent) error
}
