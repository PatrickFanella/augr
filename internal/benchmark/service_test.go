package benchmark

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type memoryStore struct {
	experiment  *strategycatalog.Experiment
	manifest    *dataset.Manifest
	evaluation  *evaluation.Report
	declaration *Declaration
	report      *Report
	err         error
}

func (s *memoryStore) GetResearchExperiment(context.Context, uuid.UUID) (*strategycatalog.Experiment, error) {
	return s.experiment, s.err
}

func (s *memoryStore) GetDatasetManifest(context.Context, uuid.UUID) (*dataset.Manifest, error) {
	return s.manifest, s.err
}

func (s *memoryStore) GetEvaluation(context.Context, uuid.UUID) (*evaluation.Report, error) {
	return s.evaluation, s.err
}

func (s *memoryStore) RegisterDeclaration(_ context.Context, value *Declaration) (*Declaration, error) {
	s.declaration = value
	return value, s.err
}

func (s *memoryStore) RecordReport(_ context.Context, value *Report) (*Report, error) {
	s.report = value
	return value, s.err
}

func TestServiceReloadsExactParentsAndPersistsDerivedReport(t *testing.T) {
	t.Parallel()
	declaration, expected := benchmarkFixture(t)
	experiment, evaluationReport := benchmarkParents(t)
	store := &memoryStore{experiment: experiment, manifest: fixtureManifest(t), evaluation: evaluationReport}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Evaluate(context.Background(), declaration, evaluationReport.ID())
	if err != nil || got.ID() != expected.ID() || store.declaration.ID() != declaration.ID() || store.report.ID() != expected.ID() {
		t.Fatalf("evaluate = %+v/%v", got, err)
	}
}

func TestServiceFailsClosedBeforeWrites(t *testing.T) {
	t.Parallel()
	declaration, _ := benchmarkFixture(t)
	experiment, evaluationReport := benchmarkParents(t)
	store := &memoryStore{experiment: experiment, manifest: fixtureManifest(t), evaluation: evaluationReport, err: errors.New("offline")}
	service, _ := NewService(store)
	if _, err := service.Evaluate(context.Background(), declaration, evaluationReport.ID()); err == nil || store.declaration != nil || store.report != nil {
		t.Fatalf("failure = %v writes=%v/%v", err, store.declaration, store.report)
	}
}
