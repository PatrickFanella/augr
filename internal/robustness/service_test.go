package robustness

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
)

type serviceStore struct {
	reports  map[uuid.UUID]*evaluation.Report
	recorded *Assessment
}

func (store *serviceStore) GetEvaluation(_ context.Context, id uuid.UUID) (*evaluation.Report, error) {
	return store.reports[id], nil
}

func (store *serviceStore) RegisterPolicy(_ context.Context, value *Policy) (*Policy, error) {
	return value, nil
}

func (store *serviceStore) RegisterFamily(_ context.Context, value *Family) (*Family, error) {
	return value, nil
}

func (store *serviceStore) RecordAssessment(_ context.Context, value *Assessment) (*Assessment, error) {
	store.recorded = value
	return value, nil
}

func TestServiceReloadsExactEvaluationParentsBeforeAssessment(t *testing.T) {
	input, reports := validAssessmentInput(t)
	store := &serviceStore{reports: reports}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := service.Assess(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := NewAssessment(input)
	if assessment.ID() != want.ID() || store.recorded == nil || store.recorded.ID() != want.ID() {
		t.Fatalf("assessment=%v recorded=%v want=%v", assessment, store.recorded, want)
	}
}
