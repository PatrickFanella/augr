package promotion

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type Store interface {
	GetDeployment(context.Context, uuid.UUID) (*strategycatalog.Deployment, error)
	GetAssessment(context.Context, uuid.UUID) (*robustness.Assessment, error)
	GetDecision(context.Context, uuid.UUID) (*Decision, error)
	RegisterPolicy(context.Context, *Policy) (*Policy, error)
	RecordDecision(context.Context, *Decision) (*Decision, error)
}

type Service struct{ store Store }

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("promotion store is required")
	}
	return &Service{store: store}, nil
}

type Request struct {
	DeploymentID    uuid.UUID
	AssessmentID    uuid.UUID
	Policy          *Policy
	PriorDecisionID uuid.UUID
}

// Evaluate reloads all exact parents and derives the outcome. The request has
// no verdict or next-state field, and persistence does not activate a runtime.
func (service *Service) Evaluate(ctx context.Context, request Request) (*Decision, error) {
	if service == nil || service.store == nil || request.DeploymentID == uuid.Nil || request.AssessmentID == uuid.Nil || request.Policy == nil {
		return nil, fmt.Errorf("promotion request requires deployment, assessment, and policy")
	}
	deployment, err := service.store.GetDeployment(ctx, request.DeploymentID)
	if err != nil {
		return nil, fmt.Errorf("load promotion deployment: %w", err)
	}
	assessment, err := service.store.GetAssessment(ctx, request.AssessmentID)
	if err != nil {
		return nil, fmt.Errorf("load promotion assessment: %w", err)
	}
	var prior *Decision
	if request.PriorDecisionID != uuid.Nil {
		prior, err = service.store.GetDecision(ctx, request.PriorDecisionID)
		if err != nil {
			return nil, fmt.Errorf("load prior promotion decision: %w", err)
		}
	}
	decision, err := NewDecision(DecisionInput{Deployment: deployment, Assessment: assessment, Policy: request.Policy, PriorDecision: prior})
	if err != nil {
		return nil, err
	}
	policy, err := service.store.RegisterPolicy(ctx, request.Policy)
	if err != nil {
		return nil, fmt.Errorf("register promotion policy: %w", err)
	}
	if policy == nil || policy.ID() != request.Policy.ID() || policy.Digest() != request.Policy.Digest() {
		return nil, fmt.Errorf("registered promotion policy diverged")
	}
	persisted, err := service.store.RecordDecision(ctx, decision)
	if err != nil {
		return nil, fmt.Errorf("record promotion decision: %w", err)
	}
	if persisted == nil || persisted.ID() != decision.ID() || persisted.Digest() != decision.Digest() {
		return nil, fmt.Errorf("persisted promotion decision diverged")
	}
	return persisted, nil
}
