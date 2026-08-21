package evaluation

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
)

// Store is deliberately limited to exact identity reads and immutable writes.
// It has no best/current lookup and no lifecycle or deployment mutation.
type Store interface {
	GetResult(context.Context, uuid.UUID) (*experimentrun.Result, error)
	RegisterPolicy(context.Context, *Policy) (*Policy, error)
	RecordEvaluation(context.Context, *Report) (*Report, error)
}

type Service struct{ store Store }

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("evaluation store is required")
	}
	return &Service{store: store}, nil
}

type Request struct {
	ResultID uuid.UUID
	ReportInput
}

// Evaluate reloads the exact completed OVR-303 result before calculation,
// registers the immutable policy, and atomically records the report graph.
func (service *Service) Evaluate(ctx context.Context, request Request) (*Report, error) {
	if service == nil || service.store == nil || request.ResultID == uuid.Nil || request.Policy == nil {
		return nil, fmt.Errorf("evaluation request requires result identity and policy")
	}
	result, err := service.store.GetResult(ctx, request.ResultID)
	if err != nil {
		return nil, fmt.Errorf("load completed experiment result: %w", err)
	}
	request.Result = result
	report, err := NewReport(request.ReportInput)
	if err != nil {
		return nil, err
	}
	policy, err := service.store.RegisterPolicy(ctx, request.Policy)
	if err != nil {
		return nil, fmt.Errorf("register evaluation policy: %w", err)
	}
	if policy.ID() != request.Policy.ID() || policy.Digest() != request.Policy.Digest() {
		return nil, fmt.Errorf("registered evaluation policy diverged")
	}
	persisted, err := service.store.RecordEvaluation(ctx, report)
	if err != nil {
		return nil, fmt.Errorf("record trade portfolio evaluation: %w", err)
	}
	if persisted.ID() != report.ID() || persisted.Digest() != report.Digest() {
		return nil, fmt.Errorf("persisted evaluation diverged")
	}
	return persisted, nil
}
