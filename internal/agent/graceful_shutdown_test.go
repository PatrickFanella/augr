package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// --------------------------------------------------------------------------
// Graceful-shutdown invariant: pipeline run status is never left at "running"
// --------------------------------------------------------------------------

// TestRepoPersister_RecordRunCompleteSucceedsWithCancelledContext verifies the
// key invariant that makes graceful shutdown safe: RepoPersister.RecordRunComplete
// uses an independent context.Background() for its DB write, so the status
// update succeeds even when the pipeline's execution context has been cancelled
// (e.g. because SIGTERM was received).
//
// Without this property a pipeline run interrupted by SIGTERM could be left
// permanently stuck at status="running" in the database.
func TestRepoPersister_RecordRunCompleteSucceedsWithCancelledContext(t *testing.T) {
	t.Parallel()

	repo := &captureUpdateRunRepo{}

	persister := NewRepoPersister(repo, nil, nil, nil, nil)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before we call RecordRunComplete

	runID := uuid.New()
	tradeDate := time.Now().UTC().Truncate(24 * time.Hour)

	err := persister.RecordRunComplete(cancelledCtx, runID, tradeDate, domain.PipelineStatusFailed, time.Now(), "context canceled", nil)
	if err != nil {
		t.Fatalf("RecordRunComplete with cancelled context returned error: %v; pipeline run would be stuck at 'running'", err)
	}

	if !repo.updateCalled.Load() {
		t.Fatal("UpdateStatus was not called; pipeline run status was not persisted")
	}
	if repo.lastStatus != domain.PipelineStatusFailed {
		t.Fatalf("persisted status = %q, want %q", repo.lastStatus, domain.PipelineStatusFailed)
	}
}

// TestRepoPersister_RecordRunCompleteCompletedStatusWithCancelledContext
// verifies the same property for the "completed" path (all phases succeeded but
// shutdown happened concurrently).
func TestRepoPersister_RecordRunCompleteCompletedStatusWithCancelledContext(t *testing.T) {
	t.Parallel()

	repo := &captureUpdateRunRepo{}

	persister := NewRepoPersister(repo, nil, nil, nil, nil)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	runID := uuid.New()
	tradeDate := time.Now().UTC().Truncate(24 * time.Hour)

	err := persister.RecordRunComplete(cancelledCtx, runID, tradeDate, domain.PipelineStatusCompleted, time.Now(), "", nil)
	if err != nil {
		t.Fatalf("RecordRunComplete (completed) with cancelled context returned error: %v; pipeline run would be stuck at 'running'", err)
	}

	if !repo.updateCalled.Load() {
		t.Fatal("UpdateStatus was not called for completed run")
	}
	if repo.lastStatus != domain.PipelineStatusCompleted {
		t.Fatalf("persisted status = %q, want %q", repo.lastStatus, domain.PipelineStatusCompleted)
	}
}

// --------------------------------------------------------------------------
// captureUpdateRunRepo records UpdateStatus calls for assertions.
// --------------------------------------------------------------------------

type captureUpdateRunRepo struct {
	updateCalled atomic.Bool
	lastStatus   domain.PipelineStatus
}

func (r *captureUpdateRunRepo) Create(_ context.Context, _ *domain.PipelineRun) error { return nil }

func (r *captureUpdateRunRepo) Get(_ context.Context, _ uuid.UUID, _ time.Time) (*domain.PipelineRun, error) {
	return nil, repository.ErrNotFound
}

func (r *captureUpdateRunRepo) List(_ context.Context, _ repository.PipelineRunFilter, _, _ int) ([]domain.PipelineRun, error) {
	return nil, nil
}

func (r *captureUpdateRunRepo) Count(_ context.Context, _ repository.PipelineRunFilter) (int, error) {
	return 0, nil
}

func (r *captureUpdateRunRepo) UpdateStatus(ctx context.Context, _ uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
	// Return an error if the context is already cancelled so that the test
	// fails if RecordRunComplete forwards the caller's context instead of
	// using an independent context.Background() for the DB write.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.updateCalled.Store(true)
	r.lastStatus = update.Status
	return nil
}
