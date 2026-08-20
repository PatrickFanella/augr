package benchmark

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

// Store exposes exact immutable reads and append-only writes. It deliberately
// has no latest/best selector or lifecycle, scheduling, allocation, or trading
// mutation.
type Store interface {
	GetResearchExperiment(context.Context, uuid.UUID) (*strategycatalog.Experiment, error)
	GetDatasetManifest(context.Context, uuid.UUID) (*dataset.Manifest, error)
	GetEvaluation(context.Context, uuid.UUID) (*evaluation.Report, error)
	RegisterDeclaration(context.Context, *Declaration) (*Declaration, error)
	RecordReport(context.Context, *Report) (*Report, error)
}

type Service struct{ store Store }

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("benchmark store is required")
	}
	return &Service{store: store}, nil
}

// Evaluate reloads every exact parent before deriving and recording an
// opportunity-cost report. The caller supplies no return, score, or outcome.
func (service *Service) Evaluate(ctx context.Context, declaration *Declaration, evaluationID uuid.UUID) (*Report, error) {
	if service == nil || service.store == nil || declaration == nil || evaluationID == uuid.Nil {
		return nil, fmt.Errorf("benchmark evaluation request is invalid")
	}
	experiment, err := service.store.GetResearchExperiment(ctx, declaration.ExperimentID())
	if err != nil {
		return nil, fmt.Errorf("load benchmark experiment: %w", err)
	}
	manifest, err := service.store.GetDatasetManifest(ctx, declaration.ManifestID())
	if err != nil {
		return nil, fmt.Errorf("load benchmark manifest: %w", err)
	}
	validated, err := DeclarationFromCanonical(declaration.ID(), declaration.Digest(), declaration.CanonicalBytes(), experiment, manifest)
	if err != nil {
		return nil, fmt.Errorf("validate benchmark declaration parents: %w", err)
	}
	evaluationReport, err := service.store.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return nil, fmt.Errorf("load benchmark evaluation: %w", err)
	}
	report, err := NewReport(validated, evaluationReport)
	if err != nil {
		return nil, err
	}
	persistedDeclaration, err := service.store.RegisterDeclaration(ctx, validated)
	if err != nil {
		return nil, fmt.Errorf("register benchmark declaration: %w", err)
	}
	if persistedDeclaration == nil || persistedDeclaration.ID() != validated.ID() || persistedDeclaration.Digest() != validated.Digest() {
		return nil, fmt.Errorf("registered benchmark declaration diverged")
	}
	persisted, err := service.store.RecordReport(ctx, report)
	if err != nil {
		return nil, fmt.Errorf("record benchmark report: %w", err)
	}
	if persisted == nil || persisted.ID() != report.ID() || persisted.Digest() != report.Digest() {
		return nil, fmt.Errorf("persisted benchmark report diverged")
	}
	return persisted, nil
}
