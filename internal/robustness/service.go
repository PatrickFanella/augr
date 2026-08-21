package robustness

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
)

// Store exposes immutable evidence reads and writes only. It deliberately has
// no best/current lookup and no promotion, retirement, scheduling, or deploy
// mutation.
type Store interface {
	GetEvaluation(context.Context, uuid.UUID) (*evaluation.Report, error)
	RegisterPolicy(context.Context, *Policy) (*Policy, error)
	RegisterFamily(context.Context, *Family) (*Family, error)
	RecordAssessment(context.Context, *Assessment) (*Assessment, error)
}

type Service struct{ store Store }

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("robustness store is required")
	}
	return &Service{store: store}, nil
}

// Assess reloads every exact OVR-304 parent by identity before calculating and
// persisting a complete family-wide result. Caller-supplied report payloads are
// never trusted as persistence evidence.
func (service *Service) Assess(ctx context.Context, input AssessmentInput) (*Assessment, error) {
	if service == nil || service.store == nil || input.Policy == nil || input.Family == nil {
		return nil, fmt.Errorf("robustness assessment requires policy and family")
	}
	for candidateIndex := range input.Candidates {
		for foldIndex := range input.Candidates[candidateIndex].Folds {
			fold := &input.Candidates[candidateIndex].Folds[foldIndex]
			if fold.Baseline == nil {
				return nil, fmt.Errorf("robustness baseline report is required")
			}
			baseline, err := service.store.GetEvaluation(ctx, fold.Baseline.ID())
			if err != nil {
				return nil, fmt.Errorf("load robustness baseline report: %w", err)
			}
			fold.Baseline = baseline
			for scenarioIndex := range fold.Perturbations {
				scenario := &fold.Perturbations[scenarioIndex]
				if scenario.Report == nil {
					return nil, fmt.Errorf("robustness perturbation report is required")
				}
				report, err := service.store.GetEvaluation(ctx, scenario.Report.ID())
				if err != nil {
					return nil, fmt.Errorf("load robustness perturbation report: %w", err)
				}
				scenario.Report = report
			}
		}
	}
	assessment, err := NewAssessment(input)
	if err != nil {
		return nil, err
	}
	policy, err := service.store.RegisterPolicy(ctx, input.Policy)
	if err != nil {
		return nil, fmt.Errorf("register robustness policy: %w", err)
	}
	if policy == nil || policy.ID() != input.Policy.ID() || policy.Digest() != input.Policy.Digest() {
		return nil, fmt.Errorf("registered robustness policy diverged")
	}
	family, err := service.store.RegisterFamily(ctx, input.Family)
	if err != nil {
		return nil, fmt.Errorf("register robustness family: %w", err)
	}
	if family == nil || family.ID() != input.Family.ID() || family.Digest() != input.Family.Digest() {
		return nil, fmt.Errorf("registered robustness family diverged")
	}
	persisted, err := service.store.RecordAssessment(ctx, assessment)
	if err != nil {
		return nil, fmt.Errorf("record robustness assessment: %w", err)
	}
	if persisted.ID() != assessment.ID() || persisted.Digest() != assessment.Digest() {
		return nil, fmt.Errorf("persisted robustness assessment diverged")
	}
	return persisted, nil
}
