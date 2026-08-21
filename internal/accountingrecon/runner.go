package accountingrecon

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type CaptureLease interface {
	AccountID() uuid.UUID
	AsOf() time.Time
	AcquiredAt() time.Time
	FenceID() string
	Epoch() uint64
	Active() bool
	Verified() bool
	Release() error
}

type CaptureFence interface {
	Acquire(context.Context, uuid.UUID, time.Time) (CaptureLease, error)
}

type SnapshotSourceReader interface {
	Capture(context.Context, SourceRequest, CaptureLease) (*Snapshot, error)
}

type AccountingEvidenceStore interface {
	RecordAccountingRun(context.Context, *Run) (*Run, error)
}

type RunRequest struct {
	AccountID         uuid.UUID
	AsOf              time.Time
	ProjectionVersion string
	MarkSource        string
	MarkNamespace     string
	MaxMarkAge        time.Duration
	Generator         string
	GeneratedAt       time.Time
	Explanations      []ExplanationInput
}

type Runner struct {
	fence  CaptureFence
	legacy SnapshotSourceReader
	ledger SnapshotSourceReader
	store  AccountingEvidenceStore
}

func NewRunner(fence CaptureFence, legacy, ledger SnapshotSourceReader, store AccountingEvidenceStore) *Runner {
	return &Runner{fence: fence, legacy: legacy, ledger: ledger, store: store}
}

func (runner *Runner) Run(ctx context.Context, request RunRequest) (*Run, error) {
	if runner == nil || runner.fence == nil || runner.legacy == nil || runner.ledger == nil || runner.store == nil {
		return nil, fmt.Errorf("accounting dual-run dependencies are required")
	}
	if request.AccountID == uuid.Nil || request.AsOf.IsZero() || !normalizedRequired(request.Generator, 256) {
		return nil, fmt.Errorf("accounting dual-run request is invalid")
	}
	lease, err := runner.fence.Acquire(ctx, request.AccountID, request.AsOf)
	if err != nil {
		return nil, fmt.Errorf("acquire accounting capture lease: %w", err)
	}
	if lease == nil {
		return nil, fmt.Errorf("acquire accounting capture lease: lease is required")
	}
	released := false
	defer func() {
		if !released {
			_ = lease.Release()
		}
	}()
	if err := validateCaptureLease(request.AccountID, request.AsOf, lease); err != nil {
		return nil, err
	}
	if request.GeneratedAt.Before(lease.AcquiredAt()) {
		return nil, fmt.Errorf("accounting dual-run generated_at precedes capture lease acquisition")
	}

	sourceRequest := SourceRequest{
		AccountID: request.AccountID, AsOf: request.AsOf, ProjectionVersion: request.ProjectionVersion,
		MarkSource: request.MarkSource, MarkNamespace: request.MarkNamespace, MaxMarkAge: request.MaxMarkAge,
	}
	legacySnapshot, err := runner.legacy.Capture(ctx, sourceRequest, lease)
	if err != nil {
		return nil, fmt.Errorf("capture legacy accounting snapshot: %w", err)
	}
	if !lease.Active() {
		return nil, fmt.Errorf("accounting capture lease released during legacy read")
	}
	ledgerSnapshot, err := runner.ledger.Capture(ctx, sourceRequest, lease)
	if err != nil {
		return nil, fmt.Errorf("capture ledger accounting snapshot: %w", err)
	}
	if !lease.Active() {
		return nil, fmt.Errorf("accounting capture lease released during ledger read")
	}
	if err := validateCapturedLease(legacySnapshot, lease); err != nil {
		return nil, fmt.Errorf("legacy accounting snapshot capture fence: %w", err)
	}
	if err := validateCapturedLease(ledgerSnapshot, lease); err != nil {
		return nil, fmt.Errorf("ledger accounting snapshot capture fence: %w", err)
	}
	if err := lease.Release(); err != nil {
		return nil, fmt.Errorf("release accounting capture lease: %w", err)
	}
	released = true

	run, err := Compare(ComparisonInput{Legacy: legacySnapshot, Ledger: ledgerSnapshot, Generator: request.Generator, GeneratedAt: request.GeneratedAt, Explanations: request.Explanations})
	if err != nil {
		return nil, fmt.Errorf("compare accounting snapshots: %w", err)
	}
	persisted, err := runner.store.RecordAccountingRun(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("record accounting reconciliation: %w", err)
	}
	if persisted == nil || persisted.ID != run.ID {
		return nil, fmt.Errorf("record accounting reconciliation returned mismatched evidence")
	}
	return persisted, nil
}

func validateCapturedLease(snapshot *Snapshot, lease CaptureLease) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is required")
	}
	if snapshot.AccountID != lease.AccountID() || !snapshot.AsOf.Equal(lease.AsOf()) || snapshot.CaptureFenceID != lease.FenceID() || snapshot.CaptureEpoch != lease.Epoch() {
		return fmt.Errorf("snapshot does not bind the active capture lease")
	}
	return nil
}

func validateCaptureLease(accountID uuid.UUID, asOf time.Time, lease CaptureLease) error {
	if lease == nil || !lease.Verified() || !lease.Active() || lease.AccountID() != accountID || !lease.AsOf().Equal(asOf) ||
		!normalizedRequired(lease.FenceID(), 256) || lease.Epoch() == 0 {
		return fmt.Errorf("accounting capture lease is absent, inactive, unverified, or mismatched")
	}
	if err := requireUTCMicrosecond("capture lease acquired_at", lease.AcquiredAt()); err != nil || lease.AcquiredAt().Before(asOf) {
		return fmt.Errorf("accounting capture lease acquisition time is invalid")
	}
	return nil
}
