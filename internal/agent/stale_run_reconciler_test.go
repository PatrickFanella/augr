package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type staleRunRepoStub struct {
	runs    []domain.PipelineRun
	updates []repository.PipelineRunStatusUpdate
	ids     []uuid.UUID
	filter  repository.PipelineRunFilter
	err     error
}

func (s *staleRunRepoStub) Create(context.Context, *domain.PipelineRun) error { return nil }
func (s *staleRunRepoStub) Get(context.Context, uuid.UUID, time.Time) (*domain.PipelineRun, error) {
	return nil, repository.ErrNotFound
}
func (s *staleRunRepoStub) List(_ context.Context, filter repository.PipelineRunFilter, _, _ int) ([]domain.PipelineRun, error) {
	s.filter = filter
	if s.err != nil {
		return nil, s.err
	}
	return s.runs, nil
}
func (s *staleRunRepoStub) Count(context.Context, repository.PipelineRunFilter) (int, error) {
	return len(s.runs), nil
}
func (s *staleRunRepoStub) UpdateStatus(_ context.Context, id uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
	s.ids = append(s.ids, id)
	s.updates = append(s.updates, update)
	return nil
}

type staleAuditLogStub struct {
	entries []*domain.AuditLogEntry
	err     error
}

func (s *staleAuditLogStub) Create(_ context.Context, entry *domain.AuditLogEntry) error {
	if s.err != nil {
		return s.err
	}
	s.entries = append(s.entries, entry)
	return nil
}
func (s *staleAuditLogStub) Query(context.Context, repository.AuditLogFilter, int, int) ([]domain.AuditLogEntry, error) {
	return nil, nil
}
func (s *staleAuditLogStub) Count(context.Context, repository.AuditLogFilter) (int, error) {
	return len(s.entries), nil
}

type staleMetricStub struct{ count int }

func (s *staleMetricStub) RecordStaleRunReconciled() { s.count++ }

func TestStaleRunReconcilerReconcile_MarksRunsFailedAndCancels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	staleRun := domain.PipelineRun{
		ID:        uuid.New(),
		TradeDate: now.Truncate(24 * time.Hour),
		Status:    domain.PipelineStatusRunning,
		StartedAt: now.Add(-45 * time.Minute),
	}
	repo := &staleRunRepoStub{runs: []domain.PipelineRun{staleRun}}
	audit := &staleAuditLogStub{}
	metrics := &staleMetricStub{}
	registry := NewRunContextRegistry()
	cancelled := false
	registry.Register(staleRun.ID, func() { cancelled = true })

	reconciler := NewStaleRunReconciler(repo, audit, registry, metrics, nil, StaleRunReconcilerConfig{
		TTL:      30 * time.Minute,
		Interval: time.Minute,
		Clock:    func() time.Time { return now },
	})

	count, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Reconcile() count = %d, want 1", count)
	}
	if repo.filter.Status != domain.PipelineStatusRunning {
		t.Fatalf("filter.Status = %q, want %q", repo.filter.Status, domain.PipelineStatusRunning)
	}
	if repo.filter.StartedBefore == nil || !repo.filter.StartedBefore.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("StartedBefore = %v, want %v", repo.filter.StartedBefore, now.Add(-30*time.Minute))
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(repo.updates))
	}
	if repo.updates[0].Status != domain.PipelineStatusFailed {
		t.Fatalf("update.Status = %q, want %q", repo.updates[0].Status, domain.PipelineStatusFailed)
	}
	if repo.updates[0].CompletedAt == nil || !repo.updates[0].CompletedAt.Equal(now) {
		t.Fatalf("CompletedAt = %v, want %v", repo.updates[0].CompletedAt, now)
	}
	if repo.updates[0].ErrorMessage != "stale run: exceeded TTL" {
		t.Fatalf("ErrorMessage = %q", repo.updates[0].ErrorMessage)
	}
	if !cancelled {
		t.Fatal("expected registry cancel func to be called")
	}
	if metrics.count != 1 {
		t.Fatalf("metrics count = %d, want 1", metrics.count)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(audit.entries))
	}
	if audit.entries[0].EventType != "pipeline_run.stale_reconciled" {
		t.Fatalf("audit event = %q", audit.entries[0].EventType)
	}
	var details map[string]any
	if err := json.Unmarshal(audit.entries[0].Details, &details); err != nil {
		t.Fatalf("unmarshal audit details: %v", err)
	}
	if details["reason"] != "stale run: exceeded TTL" {
		t.Fatalf("audit reason = %v", details["reason"])
	}
}

func TestStaleRunReconcilerReconcile_SkipsWhenRepoFails(t *testing.T) {
	t.Parallel()

	reconciler := NewStaleRunReconciler(&staleRunRepoStub{err: errors.New("boom")}, nil, nil, nil, nil, StaleRunReconcilerConfig{
		TTL:   time.Minute,
		Clock: time.Now,
	})

	count, err := reconciler.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() error = nil, want error")
	}
	if count != 0 {
		t.Fatalf("Reconcile() count = %d, want 0", count)
	}
}
