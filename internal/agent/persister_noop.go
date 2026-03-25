package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// NoopPersister is a DecisionPersister that does nothing. Useful for tests
// that don't need persistence.
type NoopPersister struct{}

func (NoopPersister) RecordRunStart(context.Context, *domain.PipelineRun) error { return nil }
func (NoopPersister) RecordRunComplete(context.Context, uuid.UUID, time.Time, domain.PipelineStatus, time.Time, string) error {
	return nil
}
func (NoopPersister) PersistDecision(context.Context, uuid.UUID, Node, *int, string, *DecisionLLMResponse) error {
	return nil
}
