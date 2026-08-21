package simulation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestSimulationPersistenceRecordsRawBeforeEveryFill(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("persist-order", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.30"), Size: decimal.NewFromInt(6)},
		},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordingSimulationPersistence(fixture.aggregate)
	persisted, err := PersistResult(context.Background(), store, fixture.account.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"raw", "fill", "raw", "fill"}
	if len(store.calls) != len(want) {
		t.Fatalf("persistence calls = %v, want %v", store.calls, want)
	}
	for index := range want {
		if store.calls[index] != want[index] {
			t.Fatalf("persistence calls = %v, want %v", store.calls, want)
		}
	}
	if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 2 || len(store.rawEvents) != 2 {
		t.Fatalf("persisted state = %s fills:%d raw:%d", persisted.State, len(persisted.Fills), len(store.rawEvents))
	}
}

func TestSimulationPersistenceNeverWritesRawForNonFillTransition(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.orderType = lifecycle.OrderLimit
		config.limitPrice = decimalTestPointer("10.20")
	})
	snapshot := fixture.snapshot("persist-working", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordingSimulationPersistence(fixture.aggregate)
	persisted, err := PersistResult(context.Background(), store, fixture.account.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 1 || store.calls[0] != "transition" || len(store.rawEvents) != 0 || persisted.State != lifecycle.StateWorking {
		t.Fatalf("non-fill persistence = calls:%v raw:%d state:%s", store.calls, len(store.rawEvents), persisted.State)
	}
}

func TestSimulationPersistenceRetryAfterRawOnlyFailureKeepsOrder(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("persist-retry", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordingSimulationPersistence(fixture.aggregate)
	injected := errors.New("injected fill failure")
	store.failFillOnce = injected
	if _, err := PersistResult(context.Background(), store, fixture.account.ID, result); !errors.Is(err, injected) {
		t.Fatalf("first persistence error = %v", err)
	}
	if len(store.rawEvents) != 1 || store.current.State != lifecycle.StateRouted || len(store.calls) != 2 || store.calls[0] != "raw" || store.calls[1] != "fill" {
		t.Fatalf("interrupted persistence = calls:%v raw:%d state:%s", store.calls, len(store.rawEvents), store.current.State)
	}
	persisted, err := PersistResult(context.Background(), store, fixture.account.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 1 || len(store.rawEvents) != 1 {
		t.Fatalf("retry persistence = state:%s fills:%d raw:%d", persisted.State, len(persisted.Fills), len(store.rawEvents))
	}
	want := []string{"raw", "fill", "raw", "fill"}
	for index := range want {
		if store.calls[index] != want[index] {
			t.Fatalf("retry calls = %v, want %v", store.calls, want)
		}
	}
}

func TestSimulationPersistenceStopsBeforeFillWhenRawWriteFails(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("raw-failure", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordingSimulationPersistence(fixture.aggregate)
	injected := errors.New("injected raw failure")
	store.failRawOnce = injected
	if _, err := PersistResult(context.Background(), store, fixture.account.ID, result); !errors.Is(err, injected) {
		t.Fatalf("persistence error = %v", err)
	}
	if len(store.calls) != 1 || store.calls[0] != "raw" || len(store.rawEvents) != 0 || len(store.current.Fills) != 0 {
		t.Fatalf("raw failure effects = calls:%v raw:%d fills:%d", store.calls, len(store.rawEvents), len(store.current.Fills))
	}
}

func TestRegisterSimulationPolicyPersistsExactArtifact(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	store := newRecordingSimulationPolicyStore()
	createdAt := fixture.routeAt.Add(-time.Hour)
	registered, err := RegisterPolicy(context.Background(), store, fixture.policy, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := RegisterPolicy(context.Background(), store, fixture.policy, createdAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.artifacts) != 1 || !SamePolicyArtifactPayload(registered, replayed) || registered.CreatedAt != createdAt {
		t.Fatalf("registered/replayed policy = %#v/%#v count:%d", registered, replayed, len(store.artifacts))
	}
}

type recordingSimulationPersistence struct {
	current      *lifecycle.Aggregate
	calls        []string
	rawEvents    map[uuid.UUID]*ledger.EconomicSourceEvent
	failRawOnce  error
	failFillOnce error
}

func newRecordingSimulationPersistence(current *lifecycle.Aggregate) *recordingSimulationPersistence {
	return &recordingSimulationPersistence{current: current, rawEvents: make(map[uuid.UUID]*ledger.EconomicSourceEvent)}
}

func (store *recordingSimulationPersistence) RecordEconomicSourceEvent(_ context.Context, event *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error) {
	store.calls = append(store.calls, "raw")
	if store.failRawOnce != nil {
		err := store.failRawOnce
		store.failRawOnce = nil
		return nil, err
	}
	if existing := store.rawEvents[event.ID]; existing != nil {
		if !ledger.SameEconomicSourceEventPayload(existing, event) {
			return nil, errors.New("raw replay conflict")
		}
		return existing, nil
	}
	cloned := *event
	cloned.RawPayload = append([]byte(nil), event.RawPayload...)
	store.rawEvents[event.ID] = &cloned
	return &cloned, nil
}

func (store *recordingSimulationPersistence) ApplyExecutionFill(_ context.Context, accountID uuid.UUID, transition *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	store.calls = append(store.calls, "fill")
	if store.failFillOnce != nil {
		err := store.failFillOnce
		store.failFillOnce = nil
		return nil, err
	}
	if accountID != store.current.Intent.AccountID {
		return nil, errors.New("account mismatch")
	}
	for _, event := range store.current.Events {
		if event.ID == transition.Event.ID {
			return store.current, nil
		}
	}
	next, err := lifecycle.ApplyTransition(store.current, transition)
	if err != nil {
		return nil, err
	}
	store.current = next
	return next, nil
}

func (store *recordingSimulationPersistence) ApplyExecutionTransition(_ context.Context, accountID uuid.UUID, transition *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	store.calls = append(store.calls, "transition")
	if accountID != store.current.Intent.AccountID {
		return nil, errors.New("account mismatch")
	}
	for _, event := range store.current.Events {
		if event.ID == transition.Event.ID {
			return store.current, nil
		}
	}
	next, err := lifecycle.ApplyTransition(store.current, transition)
	if err != nil {
		return nil, err
	}
	store.current = next
	return next, nil
}

type recordingSimulationPolicyStore struct {
	artifacts map[string]*PolicyArtifact
}

func newRecordingSimulationPolicyStore() *recordingSimulationPolicyStore {
	return &recordingSimulationPolicyStore{artifacts: make(map[string]*PolicyArtifact)}
}

func (store *recordingSimulationPolicyStore) RegisterSimulationPolicy(_ context.Context, artifact *PolicyArtifact) (*PolicyArtifact, error) {
	if existing := store.artifacts[artifact.Version]; existing != nil {
		if !SamePolicyArtifactPayload(existing, artifact) {
			return nil, errors.New("policy replay conflict")
		}
		return existing, nil
	}
	cloned := *artifact
	cloned.CanonicalBytes = append([]byte(nil), artifact.CanonicalBytes...)
	store.artifacts[artifact.Version] = &cloned
	return &cloned, nil
}
