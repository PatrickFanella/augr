package discovery

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DiscoveryRun is a persisted record of a discovery pipeline execution.
type DiscoveryRun struct {
	ID          uuid.UUID       `json:"id"`
	Config      json.RawMessage `json:"config"`
	Result      json.RawMessage `json:"result"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	DurationNS  int64           `json:"duration_ns"`
	Candidates  int             `json:"candidates"`
	Deployed    int             `json:"deployed"`
	CreatedAt   time.Time       `json:"created_at"`
}

// RunRepository persists discovery run records.
type RunRepository interface {
	Create(ctx context.Context, config, result json.RawMessage, startedAt time.Time, duration time.Duration, candidates, deployed int) error
	List(ctx context.Context, limit, offset int) ([]DiscoveryRun, error)
	Count(ctx context.Context) (int, error)
}
