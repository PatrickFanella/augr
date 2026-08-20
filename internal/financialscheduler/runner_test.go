package financialscheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type runnerStore struct {
	mu       sync.Mutex
	lease    Lease
	terminal bool
	renewErr error
	effects  map[uuid.UUID]*Effect
}

func (s *runnerStore) Acquire(_ context.Context, occurrence *Occurrence, owner uuid.UUID, ttl time.Duration) (Acquisition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal {
		return Acquisition{Terminal: true}, nil
	}
	if s.lease.OwnerID != uuid.Nil {
		return Acquisition{Lease: s.lease}, nil
	}
	s.lease = Lease{OccurrenceID: occurrence.ID, OwnerID: owner, FenceToken: 1, Sequence: 1, ExpiresAt: time.Now().Add(ttl)}
	return Acquisition{Lease: s.lease, Acquired: true}, nil
}
func (s *runnerStore) Renew(_ context.Context, lease Lease, ttl time.Duration) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.renewErr != nil {
		return Lease{}, s.renewErr
	}
	lease.Sequence++
	lease.ExpiresAt = time.Now().Add(ttl)
	s.lease = lease
	return lease, nil
}
func (s *runnerStore) ClaimEffect(_ context.Context, lease Lease, effect *Effect) (*Effect, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease.OwnerID != s.lease.OwnerID || lease.FenceToken != s.lease.FenceToken || lease.Sequence != s.lease.Sequence {
		return nil, ErrLeaseLost
	}
	if s.effects == nil {
		s.effects = map[uuid.UUID]*Effect{}
	}
	if prior := s.effects[effect.ID]; prior != nil {
		if prior.SHA256 != effect.SHA256 {
			return nil, errors.New("conflict")
		}
		return prior, nil
	}
	s.effects[effect.ID] = effect
	return effect, nil
}
func (s *runnerStore) Complete(_ context.Context, lease Lease, _ bool, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease.Sequence != s.lease.Sequence {
		return ErrLeaseLost
	}
	s.terminal = true
	return nil
}

func TestTwoRunnersExecuteOneOccurrence(t *testing.T) {
	store := &runnerStore{}
	a, _ := NewRunner(store, uuid.MustParse("60400000-0000-4000-8000-000000000021"), time.Second, 100*time.Millisecond)
	b, _ := NewRunner(store, uuid.MustParse("60400000-0000-4000-8000-000000000022"), time.Second, 100*time.Millisecond)
	occurrence, _ := NewOccurrence(OccurrenceInput{"portfolio_allocator", "runner-v1", TriggerScheduled, time.Date(2026, 8, 20, 21, 0, 0, 0, time.UTC), uuid.Nil})
	effect, _ := NewEffect(EffectInput{occurrence.ID, EffectIntent, "account/strategy/slot", strings.Repeat("a", 64)})
	started := make(chan struct{})
	release := make(chan struct{})
	job := func(ctx context.Context, session *Session) error {
		close(started)
		<-release
		_, err := session.ClaimEffect(ctx, effect)
		return err
	}
	firstDone := make(chan error, 1)
	go func() { _, err := a.Run(context.Background(), occurrence, job); firstDone <- err }()
	<-started
	result, err := b.Run(context.Background(), occurrence, job)
	if !errors.Is(err, ErrOccurrenceNotAcquired) || result.Executed {
		t.Fatalf("second=%+v/%v", result, err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if len(store.effects) != 1 {
		t.Fatalf("effects=%d", len(store.effects))
	}
}

func TestRunnerCancelsOnLostRenewal(t *testing.T) {
	store := &runnerStore{renewErr: errors.New("fence replaced")}
	runner, _ := NewRunner(store, uuid.New(), time.Second, 10*time.Millisecond)
	occurrence, _ := NewOccurrence(OccurrenceInput{"kalshi_settlement", "runner-v1", TriggerScheduled, time.Date(2026, 8, 20, 21, 1, 0, 0, time.UTC), uuid.Nil})
	result, err := runner.Run(context.Background(), occurrence, func(ctx context.Context, _ *Session) error { <-ctx.Done(); return ctx.Err() })
	if !result.Executed || !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lost=%+v/%v", result, err)
	}
}
