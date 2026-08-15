package accountingrecon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRunnerRequiresVerifiedMatchingActiveCaptureLeaseAndPersistsOnce(t *testing.T) {
	t.Parallel()

	request := validRunRequest()
	legacy := equalSourceStub{source: SourceLegacy}
	ledgerSource := equalSourceStub{source: SourceLedger}
	store := &recordingEvidenceStore{}
	runner := NewRunner(&staticFence{lease: &mutableLease{accountID: request.AccountID, asOf: request.AsOf, acquiredAt: request.AsOf, id: "fence:run", epoch: 1, active: true, verified: true}}, legacy, ledgerSource, store)
	run, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if run == nil || store.calls != 1 || store.run.ID != run.ID {
		t.Fatalf("persisted run = %+v calls=%d", store.run, store.calls)
	}
	if runner.fence.(*staticFence).lease.Active() {
		t.Fatal("capture lease remained active after source bytes were built")
	}
}

func TestRunnerRejectsLeaseOrSourceFailureWithoutEvidence(t *testing.T) {
	t.Parallel()

	request := validRunRequest()
	tests := map[string]struct {
		lease  *mutableLease
		legacy SnapshotSourceReader
	}{
		"unverified":               {lease: &mutableLease{accountID: request.AccountID, asOf: request.AsOf, acquiredAt: request.AsOf, id: "fence:x", epoch: 1, active: true}},
		"wrong account":            {lease: &mutableLease{accountID: uuid.New(), asOf: request.AsOf, acquiredAt: request.AsOf, id: "fence:x", epoch: 1, active: true, verified: true}},
		"missing acquisition time": {lease: &mutableLease{accountID: request.AccountID, asOf: request.AsOf, id: "fence:x", epoch: 1, active: true, verified: true}},
		"source failure":           {lease: &mutableLease{accountID: request.AccountID, asOf: request.AsOf, acquiredAt: request.AsOf, id: "fence:x", epoch: 1, active: true, verified: true}, legacy: errorSource{err: errors.New("capture failed")}},
	}
	for name, testCase := range tests {
		name, testCase := name, testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			legacy := testCase.legacy
			if legacy == nil {
				legacy = equalSourceStub{source: SourceLegacy}
			}
			store := &recordingEvidenceStore{}
			runner := NewRunner(&staticFence{lease: testCase.lease}, legacy, equalSourceStub{source: SourceLedger}, store)
			if _, err := runner.Run(context.Background(), request); err == nil {
				t.Fatal("Run() unexpectedly succeeded")
			}
			if store.calls != 0 {
				t.Fatalf("evidence store calls = %d, want 0", store.calls)
			}
		})
	}
}

func TestRunnerCaptureFenceBlocksBothEconomicWritersBetweenReads(t *testing.T) {
	t.Parallel()

	request := validRunRequest()
	fence := newBlockingFence(request.AccountID, request.AsOf)
	firstRead := make(chan struct{})
	continueRead := make(chan struct{})
	legacy := callbackSource{source: SourceLegacy, callback: func() {
		close(firstRead)
		<-continueRead
	}}
	store := &recordingEvidenceStore{}
	runner := NewRunner(fence, legacy, equalSourceStub{source: SourceLedger}, store)
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(context.Background(), request)
		done <- err
	}()
	<-firstRead

	paperWrite := make(chan struct{})
	ledgerWrite := make(chan struct{})
	go func() { fence.mutate(); close(paperWrite) }()
	go func() { fence.mutate(); close(ledgerWrite) }()
	select {
	case <-paperWrite:
		t.Fatal("paper mutation interleaved capture")
	case <-ledgerWrite:
		t.Fatal("ledger normalization interleaved capture")
	case <-time.After(25 * time.Millisecond):
	}
	close(continueRead)
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case <-paperWrite:
	case <-time.After(time.Second):
		t.Fatal("paper mutation did not resume after capture")
	}
	select {
	case <-ledgerWrite:
	case <-time.After(time.Second):
		t.Fatal("ledger mutation did not resume after capture")
	}
	if store.calls != 1 {
		t.Fatalf("evidence store calls = %d, want 1", store.calls)
	}
}

type staticLease struct {
	accountID  uuid.UUID
	asOf       time.Time
	acquiredAt time.Time
	id         string
	epoch      uint64
	active     bool
	verified   bool
}

func (lease staticLease) AccountID() uuid.UUID  { return lease.accountID }
func (lease staticLease) AsOf() time.Time       { return lease.asOf }
func (lease staticLease) AcquiredAt() time.Time { return lease.acquiredAt }
func (lease staticLease) FenceID() string       { return lease.id }
func (lease staticLease) Epoch() uint64         { return lease.epoch }
func (lease staticLease) Active() bool          { return lease.active }
func (lease staticLease) Verified() bool        { return lease.verified }
func (lease staticLease) Release() error        { return nil }

type mutableLease struct {
	mu         sync.Mutex
	accountID  uuid.UUID
	asOf       time.Time
	acquiredAt time.Time
	id         string
	epoch      uint64
	active     bool
	verified   bool
	release    func()
}

func (lease *mutableLease) AccountID() uuid.UUID  { return lease.accountID }
func (lease *mutableLease) AsOf() time.Time       { return lease.asOf }
func (lease *mutableLease) AcquiredAt() time.Time { return lease.acquiredAt }
func (lease *mutableLease) FenceID() string       { return lease.id }
func (lease *mutableLease) Epoch() uint64         { return lease.epoch }
func (lease *mutableLease) Verified() bool        { return lease.verified }
func (lease *mutableLease) Active() bool {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.active
}

func (lease *mutableLease) Release() error {
	lease.mu.Lock()
	if !lease.active {
		lease.mu.Unlock()
		return errors.New("already released")
	}
	lease.active = false
	release := lease.release
	lease.mu.Unlock()
	if release != nil {
		release()
	}
	return nil
}

type staticFence struct{ lease *mutableLease }

func (fence *staticFence) Acquire(context.Context, uuid.UUID, time.Time) (CaptureLease, error) {
	return fence.lease, nil
}

type blockingFence struct {
	mu      sync.RWMutex
	account uuid.UUID
	asOf    time.Time
	writes  int
}

func newBlockingFence(account uuid.UUID, asOf time.Time) *blockingFence {
	return &blockingFence{account: account, asOf: asOf}
}

func (fence *blockingFence) Acquire(context.Context, uuid.UUID, time.Time) (CaptureLease, error) {
	fence.mu.RLock()
	return &mutableLease{accountID: fence.account, asOf: fence.asOf, acquiredAt: fence.asOf, id: "fence:blocking", epoch: 1, active: true, verified: true, release: fence.mu.RUnlock}, nil
}

func (fence *blockingFence) mutate() {
	fence.mu.Lock()
	fence.writes++
	fence.mu.Unlock()
}

type equalSourceStub struct{ source SnapshotSource }

func (source equalSourceStub) Capture(_ context.Context, request SourceRequest, lease CaptureLease) (*Snapshot, error) {
	input := completeSnapshotInput(source.source)
	input.AccountID, input.AsOf, input.ObservedAt = request.AccountID, request.AsOf, request.AsOf.Add(time.Second)
	input.ProjectionVersion, input.MarkSource, input.MarkNamespace, input.MaxMarkAge = request.ProjectionVersion, request.MarkSource, request.MarkNamespace, request.MaxMarkAge
	input.CaptureFenceID, input.CaptureEpoch = lease.FenceID(), lease.Epoch()
	input.EvidenceID = source.source.String() + ":" + request.AsOf.Format(time.RFC3339Nano)
	return NewSnapshot(input)
}

type callbackSource struct {
	source   SnapshotSource
	callback func()
}

func (source callbackSource) Capture(ctx context.Context, request SourceRequest, lease CaptureLease) (*Snapshot, error) {
	if source.callback != nil {
		source.callback()
	}
	return equalSourceStub{source: source.source}.Capture(ctx, request, lease)
}

type errorSource struct{ err error }

func (source errorSource) Capture(context.Context, SourceRequest, CaptureLease) (*Snapshot, error) {
	return nil, source.err
}

type recordingEvidenceStore struct {
	calls int
	run   *Run
}

func (store *recordingEvidenceStore) RecordAccountingRun(_ context.Context, run *Run) (*Run, error) {
	store.calls++
	store.run = run
	return run, nil
}

func validRunRequest() RunRequest {
	return RunRequest{
		AccountID:         uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		AsOf:              time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		ProjectionVersion: "ledger_fifo_v1", MarkSource: "polygon", MarkNamespace: "quotes/scored", MaxMarkAge: 5 * time.Minute,
		Generator: "dual-run-worker", GeneratedAt: time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC),
	}
}
