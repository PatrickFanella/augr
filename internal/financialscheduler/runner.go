package financialscheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrOccurrenceNotAcquired = errors.New("financial scheduler: occurrence not acquired")
	ErrLeaseLost             = errors.New("financial scheduler: lease lost")
)

type LeaseStore interface {
	Acquire(context.Context, *Occurrence, uuid.UUID, time.Duration) (Acquisition, error)
	Renew(context.Context, Lease, time.Duration) (Lease, error)
	ClaimEffect(context.Context, Lease, *Effect) (*Effect, error)
	Complete(context.Context, Lease, bool, string) error
}

type Runner struct {
	store     LeaseStore
	ownerID   uuid.UUID
	leaseTTL  time.Duration
	heartbeat time.Duration
}

type RunResult struct {
	Executed bool
	Terminal bool
	Fence    int64
}

type Session struct {
	store LeaseStore
	mu    sync.Mutex
	lease Lease
}

func NewRunner(store LeaseStore, ownerID uuid.UUID, leaseTTL, heartbeat time.Duration) (*Runner, error) {
	if store == nil || ownerID == uuid.Nil {
		return nil, fmt.Errorf("financial scheduler: runner store and owner are required")
	}
	if leaseTTL < time.Second || heartbeat <= 0 || heartbeat >= leaseTTL/2 {
		return nil, fmt.Errorf("financial scheduler: runner lease and heartbeat bounds are invalid")
	}
	return &Runner{store: store, ownerID: ownerID, leaseTTL: leaseTTL, heartbeat: heartbeat}, nil
}

func (r *Runner) Run(ctx context.Context, occurrence *Occurrence, job func(context.Context, *Session) error) (RunResult, error) {
	if ctx == nil || occurrence == nil || job == nil {
		return RunResult{}, fmt.Errorf("financial scheduler: run context, occurrence, and job are required")
	}
	acquisition, err := r.store.Acquire(ctx, occurrence, r.ownerID, r.leaseTTL)
	if err != nil {
		return RunResult{}, err
	}
	if !acquisition.Acquired {
		return RunResult{Terminal: acquisition.Terminal, Fence: acquisition.Lease.FenceToken}, ErrOccurrenceNotAcquired
	}
	session := &Session{store: r.store, lease: acquisition.Lease}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	renewDone := make(chan error, 1)
	go session.renewLoop(runCtx, r.leaseTTL, r.heartbeat, cancel, stop, renewDone)
	jobErr := invokeJob(runCtx, session, job)
	close(stop)
	renewErr := <-renewDone
	if renewErr != nil {
		return RunResult{Executed: true, Fence: acquisition.Lease.FenceToken}, fmt.Errorf("%w: %v", ErrLeaseLost, renewErr)
	}
	if ctxErr := runCtx.Err(); ctxErr != nil && jobErr == nil {
		jobErr = ctxErr
	}
	session.mu.Lock()
	lease := session.lease
	completeErr := r.store.Complete(context.WithoutCancel(ctx), lease, jobErr == nil, outcomeDigest(jobErr))
	session.mu.Unlock()
	if completeErr != nil {
		return RunResult{Executed: true, Fence: lease.FenceToken}, fmt.Errorf("%w: complete: %v", ErrLeaseLost, completeErr)
	}
	return RunResult{Executed: true, Terminal: true, Fence: lease.FenceToken}, jobErr
}

func (s *Session) ClaimEffect(ctx context.Context, effect *Effect) (*Effect, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.ClaimEffect(ctx, s.lease, effect)
}

func (s *Session) Lease() Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lease
}

func (s *Session) renewLoop(ctx context.Context, ttl, heartbeat time.Duration, cancel context.CancelFunc, stop <-chan struct{}, done chan<- error) {
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ctx.Done():
			done <- ctx.Err()
			return
		case <-ticker.C:
			s.mu.Lock()
			renewed, err := s.store.Renew(context.WithoutCancel(ctx), s.lease, ttl)
			if err == nil {
				s.lease = renewed
			}
			s.mu.Unlock()
			if err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func invokeJob(ctx context.Context, session *Session, job func(context.Context, *Session) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("financial scheduler: job panicked (%T)", recovered)
		}
	}()
	return job(ctx, session)
}

func outcomeDigest(err error) string {
	value := "succeeded"
	if err != nil {
		digest := sha256.Sum256([]byte(err.Error()))
		value = "failed@sha256:" + hex.EncodeToString(digest[:])
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
