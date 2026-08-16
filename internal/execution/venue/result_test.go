package venue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

func TestPersistResultRecordsObservationBeforeTransition(t *testing.T) {
	fixture := newVenueResultFixture(t, OutcomeRejected)
	store := newRecordingVenueResultStore(fixture.initial)

	persisted, err := PersistResult(context.Background(), store, fixture.initial.Intent.AccountID, fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 2 || store.calls[0] != "observation" || store.calls[1] != "transition" {
		t.Fatalf("persistence order = %v, want [observation transition]", store.calls)
	}
	if persisted.State != lifecycle.StateRejected {
		t.Fatalf("persisted state = %s, want rejected", persisted.State)
	}
}

func TestPersistResultStopsAfterNoChangeObservation(t *testing.T) {
	fixture := newVenueResultFixture(t, OutcomeNoChange)
	store := newRecordingVenueResultStore(fixture.initial)

	persisted, err := PersistResult(context.Background(), store, fixture.initial.Intent.AccountID, fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 1 || store.calls[0] != "observation" || persisted.State != lifecycle.StateRouted {
		t.Fatalf("no-change persistence = calls:%v state:%s", store.calls, persisted.State)
	}
}

func TestPersistResultPreflightsInvalidPlansBeforeWriting(t *testing.T) {
	fixture := newVenueResultFixture(t, OutcomeRejected)
	for name, mutate := range map[string]func(*Result){
		"nil steps":       func(result *Result) { result.Steps = nil },
		"nil observation": func(result *Result) { result.Steps[0].Observation = nil },
		"wrong policy":    func(result *Result) { result.Steps[0].Observation.PolicyVersion = strings.Repeat("0", 64) },
		"wrong account":   func(result *Result) { result.Steps[0].Observation.AccountID = uuid.New() },
		"wrong order":     func(result *Result) { result.Steps[0].Observation.OrderID = uuid.New() },
		"out of order": func(result *Result) {
			result.Steps[0].Transition.Event.PriorState = lifecycle.StateWorking
		},
		"mismatched time": func(result *Result) {
			result.Steps[0].Transition.Event.SourceAt = result.Steps[0].Transition.Event.SourceAt.Add(time.Microsecond)
		},
		"fill without raw boundaries": func(result *Result) {
			result.Steps[0].Observation.MappedOutcome = OutcomeFill
			result.Steps[0].Transition.Event.Kind = lifecycle.EventFillAcknowledged
		},
		"mismatched evidence": func(result *Result) {
			raw := json.RawMessage(`{"different":true}`)
			result.Steps[0].Transition.Event.Evidence = raw
			digest := sha256.Sum256(raw)
			result.Steps[0].Transition.Event.EvidenceSHA256 = hex.EncodeToString(digest[:])
		},
		"economic source on terminal": func(result *Result) {
			result.Steps[0].EconomicSourceEvent = &ledger.EconomicSourceEvent{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneVenueResultFixture(fixture.result)
			mutate(candidate)
			store := newRecordingVenueResultStore(fixture.initial)
			if _, err := PersistResult(context.Background(), store, fixture.initial.Intent.AccountID, candidate); err == nil {
				t.Fatal("invalid venue result unexpectedly persisted")
			}
			if len(store.calls) != 0 {
				t.Fatalf("invalid result wrote calls %v before failing", store.calls)
			}
		})
	}
}

func TestPersistResultRetryConvergesAfterObservationOnlyFailure(t *testing.T) {
	fixture := newVenueResultFixture(t, OutcomeRejected)
	store := newRecordingVenueResultStore(fixture.initial)
	injected := errors.New("injected transition failure")
	store.failTransitionOnce = injected

	if _, err := PersistResult(context.Background(), store, fixture.initial.Intent.AccountID, fixture.result); !errors.Is(err, injected) {
		t.Fatalf("first persistence error = %v, want injected failure", err)
	}
	if len(store.observations) != 1 || store.current.State != lifecycle.StateRouted {
		t.Fatalf("interrupted persistence = observations:%d state:%s", len(store.observations), store.current.State)
	}
	persisted, err := PersistResult(context.Background(), store, fixture.initial.Intent.AccountID, fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateRejected || len(store.observations) != 1 {
		t.Fatalf("retry result = state:%s observations:%d", persisted.State, len(store.observations))
	}
}

func TestCancellationCommandUsesFixedContentBoundIdentity(t *testing.T) {
	fixture := newVenueResultFixture(t, OutcomeRejected)
	working := venueResultWorkingAggregate(t, fixture)
	requestedAt := venueResultTime().Add(3 * time.Hour)
	command, err := NewCancellationCommand(working, requestedAt)
	if err != nil {
		t.Fatal(err)
	}
	store := newRecordingVenueResultStore(working)
	persisted, err := PersistCancellationCommand(
		context.Background(), store, working.Intent.AccountID, working, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateWorking || len(store.calls) != 1 || store.calls[0] != "transition" {
		t.Fatalf("cancel persistence = calls:%v state:%s", store.calls, persisted.State)
	}
	if command.Event.Source != "venue_command" || command.Event.SourceEventID != working.Order.ID.String()+"/cancel-request-v1" ||
		command.Event.SourceNamespace != PolicySchemaV1+"/kalshi/"+working.Order.PolicyVersion+"/cancel-request-v1" {
		t.Fatalf("cancel command source identity = %s/%s/%s", command.Event.Source, command.Event.SourceNamespace, command.Event.SourceEventID)
	}

	for name, change := range map[string]func(*lifecycle.Aggregate, *lifecycle.Transition){
		"endpoint or body": func(_ *lifecycle.Aggregate, candidate *lifecycle.Transition) {
			candidate.Event.Evidence = json.RawMessage(`{"path_template":"/changed"}`)
			digest := sha256.Sum256(candidate.Event.Evidence)
			candidate.Event.EvidenceSHA256 = hex.EncodeToString(digest[:])
		},
		"binding": func(candidate *lifecycle.Aggregate, _ *lifecycle.Transition) {
			candidate.Binding.ID = uuid.New()
		},
		"external id": func(candidate *lifecycle.Aggregate, _ *lifecycle.Transition) {
			candidate.Binding.ExternalOrderID = "changed-external-id"
		},
		"policy": func(candidate *lifecycle.Aggregate, _ *lifecycle.Transition) {
			candidate.Order.PolicyVersion = PolicySchemaV1 + "@sha256:" + strings.Repeat("0", 64)
		},
		"client id": func(candidate *lifecycle.Aggregate, _ *lifecycle.Transition) {
			candidate.Order.ClientOrderID = uuid.NewString()
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidateAggregate := cloneVenueCancellationAggregate(working)
			candidateCommand := *command
			candidateCommand.Event = command.Event
			candidateCommand.Event.Evidence = append([]byte(nil), command.Event.Evidence...)
			change(candidateAggregate, &candidateCommand)
			before := len(store.calls)
			if _, err := PersistCancellationCommand(
				context.Background(), store, working.Intent.AccountID, candidateAggregate, &candidateCommand,
			); err == nil {
				t.Fatal("changed cancel command unexpectedly persisted")
			}
			if len(store.calls) != before {
				t.Fatalf("changed cancel command reached store: %v", store.calls)
			}
		})
	}
}

type venueResultFixture struct {
	initial     *lifecycle.Aggregate
	observation *Observation
	transition  *lifecycle.Transition
	result      *Result
}

func newVenueResultFixture(t *testing.T, outcome MappedOutcome) venueResultFixture {
	t.Helper()
	accountID := uuid.New()
	intentID := uuid.New()
	orderID := uuid.New()
	instrumentID := uuid.New()
	contractID := uuid.New()
	policy, err := ReviewedPolicy(ProviderKalshi)
	if err != nil {
		t.Fatal(err)
	}
	quantity := decimal.NewFromInt(8)
	initial := &lifecycle.Aggregate{
		Intent: lifecycle.Intent{
			ID: intentID, AccountID: accountID, Environment: domain.AccountEnvironmentPaperScored,
			InstrumentID: instrumentID, OriginType: ledger.ExecutionOriginStrategyVersion,
			OriginID: "strategy-version-1", StrategyVersionID: "strategy-version-1",
		},
		State: lifecycle.StateRouted, AllocatedQuantity: &quantity,
		Order: &lifecycle.Order{
			ID: orderID, IntentID: intentID, AccountID: accountID, InstrumentID: instrumentID,
			ClientOrderID: orderID.String(), Side: lifecycle.SideBuy, Quantity: quantity,
			Venue: "kalshi", VenueContractID: contractID, PolicyKind: lifecycle.PolicyVenue,
			PolicyVersion: policy.Version(),
		},
	}
	receivedAt := venueResultTime().Add(time.Hour)
	providerPrice := decimal.RequireFromString("0.42")
	providerState := "rejected"
	if outcome == OutcomeNoChange {
		providerState = "resting"
	}
	raw := json.RawMessage(`{"status":"` + providerState + `"}`)
	observation, err := NewObservation(ObservationInput{
		AccountID: accountID, IntentID: intentID, OrderID: orderID, VenueContractID: contractID,
		Provider: ProviderKalshi, Venue: "kalshi", PolicyVersion: policy.Version(),
		Kind: ObservationOrderSnapshot, ProviderState: providerState, MappedOutcome: outcome,
		ExternalOrderID: "external-1", ClientOrderID: orderID.String(), ProviderContractID: "KX-TEST",
		CanonicalOutcome: "yes", ProviderBookSide: "bid", ProviderAction: "buy", ProviderPrice: &providerPrice,
		IdentityKind: SourceIdentityProvider, SourceNamespace: "kalshi/portfolio/order-snapshots",
		SourceEventID: "snapshot-1", SourceRevision: "1", SourceAt: receivedAt.Add(-time.Millisecond),
		ReceivedAt: receivedAt, RawPayload: raw, CreatedAt: receivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := &Result{Initial: initial, Aggregate: initial, Steps: []ResultStep{{Observation: observation}}}
	var transition *lifecycle.Transition
	if outcome == OutcomeRejected {
		transition, err = lifecycle.ObserveOrderTerminal(initial, lifecycle.EventOrderRejected, lifecycle.EventInput{
			Source: "kalshi", SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
			SourceRevision: observation.SourceRevision, SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
			Actor: "venue-adapter", ReasonCode: "provider_rejected", Evidence: observation.RawPayload,
		}, observation.CreatedAt)
		if err != nil {
			t.Fatal(err)
		}
		finalAggregate, applyErr := lifecycle.ApplyTransition(initial, transition)
		if applyErr != nil {
			t.Fatal(applyErr)
		}
		result.Aggregate = finalAggregate
		result.Steps[0].Transition = transition
	}
	return venueResultFixture{initial: initial, observation: observation, transition: transition, result: result}
}

func venueResultWorkingAggregate(t *testing.T, fixture venueResultFixture) *lifecycle.Aggregate {
	t.Helper()
	input := lifecycle.EventInput{
		Source: "kalshi", SourceNamespace: fixture.observation.SourceNamespace,
		SourceEventID: "working-ack", SourceAt: fixture.observation.SourceAt,
		ReceivedAt: fixture.observation.ReceivedAt, Actor: "venue-adapter",
		ReasonCode: "provider_acknowledged", Evidence: json.RawMessage(`{"status":"resting"}`),
	}
	transition, err := lifecycle.Acknowledge(fixture.initial, "external-1", input, fixture.observation.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	working, err := lifecycle.ApplyTransition(fixture.initial, transition)
	if err != nil {
		t.Fatal(err)
	}
	return working
}

func cloneVenueResultFixture(value *Result) *Result {
	cloned := *value
	cloned.Steps = append([]ResultStep(nil), value.Steps...)
	for index := range cloned.Steps {
		if cloned.Steps[index].Observation != nil {
			observation := *cloned.Steps[index].Observation
			observation.RawPayload = append([]byte(nil), observation.RawPayload...)
			observation.Payload = append([]byte(nil), observation.Payload...)
			cloned.Steps[index].Observation = &observation
		}
		if cloned.Steps[index].Transition != nil {
			transition := *cloned.Steps[index].Transition
			transition.Event = cloned.Steps[index].Transition.Event
			transition.Event.Evidence = append([]byte(nil), transition.Event.Evidence...)
			cloned.Steps[index].Transition = &transition
		}
	}
	return &cloned
}

func cloneVenueCancellationAggregate(value *lifecycle.Aggregate) *lifecycle.Aggregate {
	cloned := *value
	if value.Order != nil {
		order := *value.Order
		cloned.Order = &order
	}
	if value.Binding != nil {
		binding := *value.Binding
		cloned.Binding = &binding
	}
	cloned.Events = append([]lifecycle.Event(nil), value.Events...)
	return &cloned
}

type recordingVenueResultStore struct {
	current            *lifecycle.Aggregate
	calls              []string
	observations       map[uuid.UUID]*Observation
	economicEvents     map[uuid.UUID]*ledger.EconomicSourceEvent
	failTransitionOnce error
}

func newRecordingVenueResultStore(current *lifecycle.Aggregate) *recordingVenueResultStore {
	return &recordingVenueResultStore{
		current: current, observations: make(map[uuid.UUID]*Observation),
		economicEvents: make(map[uuid.UUID]*ledger.EconomicSourceEvent),
	}
}

func (store *recordingVenueResultStore) RecordVenueObservation(
	_ context.Context,
	observation *Observation,
) (*Observation, error) {
	store.calls = append(store.calls, "observation")
	if existing := store.observations[observation.ID]; existing != nil {
		if !SameObservationPayload(existing, observation) {
			return nil, errors.New("observation conflict")
		}
		return existing, nil
	}
	store.observations[observation.ID] = observation
	return observation, nil
}

func (store *recordingVenueResultStore) RecordEconomicSourceEvent(
	_ context.Context,
	event *ledger.EconomicSourceEvent,
) (*ledger.EconomicSourceEvent, error) {
	store.calls = append(store.calls, "economic")
	if existing := store.economicEvents[event.ID]; existing != nil {
		if !ledger.SameEconomicSourceEventPayload(existing, event) {
			return nil, errors.New("economic conflict")
		}
		return existing, nil
	}
	store.economicEvents[event.ID] = event
	return event, nil
}

func (store *recordingVenueResultStore) ApplyExecutionFill(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	store.calls = append(store.calls, "fill")
	return store.apply(ctx, accountID, transition)
}

func (store *recordingVenueResultStore) ApplyExecutionTransition(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	store.calls = append(store.calls, "transition")
	return store.apply(ctx, accountID, transition)
}

func (store *recordingVenueResultStore) apply(
	_ context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	if store.failTransitionOnce != nil {
		err := store.failTransitionOnce
		store.failTransitionOnce = nil
		return nil, err
	}
	if accountID != store.current.Intent.AccountID {
		return nil, errors.New("account mismatch")
	}
	for index := range store.current.Events {
		if store.current.Events[index].ID == transition.Event.ID {
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

func venueResultTime() time.Time {
	return time.Date(2026, 8, 15, 23, 30, 0, 123456000, time.UTC)
}
