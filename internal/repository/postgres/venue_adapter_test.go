package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestVenueAdapterRepoRegistersLoadsAndReplaysExactPolicy(t *testing.T) {
	ctx, pool := newVenueAdapterIntegrationPool(t)
	repo := NewVenueAdapterRepo(pool)
	artifact := newVenuePolicyArtifact(t, venue.ProviderKalshi)

	persisted, err := repo.RegisterVenuePolicy(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RegisterVenuePolicy(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetVenuePolicyByVersion(ctx, artifact.Version)
	if err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]*venue.PolicyArtifact{
		"persisted": persisted,
		"replayed":  replayed,
		"loaded":    loaded,
	} {
		if !venue.SamePolicyArtifactPayload(candidate, artifact) || candidate.CreatedAt != artifact.CreatedAt {
			t.Fatalf("%s venue policy differs from exact artifact", name)
		}
	}

	changed := *artifact
	changed.CanonicalBytes = append(append([]byte(nil), artifact.CanonicalBytes...), ' ')
	if _, err := repo.RegisterVenuePolicy(ctx, &changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed venue policy error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestVenueAdapterRepoConcurrentPolicyRegistrationConverges(t *testing.T) {
	ctx, pool := newVenueAdapterIntegrationPool(t)
	repo := NewVenueAdapterRepo(pool)
	artifact := newVenuePolicyArtifact(t, venue.ProviderAlpaca)
	const writers = 8
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	errorsFound := make(chan error, writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			candidate, err := repo.RegisterVenuePolicy(ctx, artifact)
			if err != nil {
				errorsFound <- err
				return
			}
			if !venue.SamePolicyArtifactPayload(candidate, artifact) {
				errorsFound <- errors.New("concurrent venue policy registration returned changed artifact")
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent venue policy registration: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM venue_adapter_policy_artifacts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent venue policy count = %d, want 1", count)
	}
}

func TestVenueAdapterRepoRecordsLoadsAndReplaysExactObservation(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "observation-replay")
	repo := NewVenueAdapterRepo(fixture.pool)
	observation := fixture.observation(t, "snapshot-1", venue.OutcomeAcknowledge)

	persisted, err := repo.RecordVenueObservation(fixture.ctx, observation)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RecordVenueObservation(fixture.ctx, observation)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetVenueObservationByID(fixture.ctx, observation.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]*venue.Observation{
		"persisted": persisted,
		"replayed":  replayed,
		"loaded":    loaded,
	} {
		if !venue.SameObservationPayload(candidate, observation) || candidate.CreatedAt != observation.CreatedAt {
			t.Fatalf("%s venue observation differs from exact fact", name)
		}
	}

	changed := *observation
	changed.SourceRevision = "changed"
	if _, err := repo.RecordVenueObservation(fixture.ctx, &changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed observation error = %v, want ErrIdempotencyConflict", err)
	}
	if _, err := repo.GetVenueObservationByID(fixture.ctx, uuid.New()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing observation error = %v, want ErrNotFound", err)
	}
}

func TestVenueAdapterRepoConcurrentObservationReplayConverges(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "observation-concurrency")
	repo := NewVenueAdapterRepo(fixture.pool)
	observation := fixture.observation(t, "snapshot-concurrent", venue.OutcomeNoChange)
	const writers = 8
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	errorsFound := make(chan error, writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			candidate, err := repo.RecordVenueObservation(fixture.ctx, observation)
			if err != nil {
				errorsFound <- err
				return
			}
			if !venue.SameObservationPayload(candidate, observation) {
				errorsFound <- errors.New("concurrent venue observation replay returned changed fact")
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent venue observation replay: %v", err)
	}
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT COUNT(*) FROM venue_observations WHERE id=$1`, observation.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent venue observation count = %d, want 1", count)
	}
}

func TestVenueResultPersistenceLinksRawAcknowledgementAndCancellationCommand(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "raw-ack-cancel")
	store := newPostgresVenueResultStore(fixture.pool)
	working := fixture.persistAcknowledgement(t, store, "ack-1")
	if working.State != lifecycle.StateWorking || working.Binding == nil {
		t.Fatalf("acknowledged state = %s binding:%v", working.State, working.Binding)
	}

	command, err := venue.NewCancellationCommand(working, working.Events[len(working.Events)-1].ReceivedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	results := make(chan *lifecycle.Aggregate, writers)
	errorsFound := make(chan error, writers)
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			ready.Wait()
			persisted, persistErr := venue.PersistCancellationCommand(
				fixture.ctx, fixture.base.repo, fixture.base.account.ID, working, command,
			)
			if persistErr != nil {
				errorsFound <- persistErr
				return
			}
			results <- persisted
		}()
	}
	wait.Wait()
	close(errorsFound)
	close(results)
	for err := range errorsFound {
		t.Errorf("concurrent cancellation command: %v", err)
	}
	for result := range results {
		if result.State != lifecycle.StateWorking {
			t.Errorf("cancellation command changed state to %s", result.State)
		}
	}
	var commands, cancelObservations int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM execution_lifecycle_events WHERE intent_id=$1 AND kind='cancel_requested'),
		(SELECT COUNT(*) FROM venue_observations WHERE order_id=$2 AND source_namespace LIKE '%cancel-request-v1')`,
		working.Intent.ID, working.Order.ID,
	).Scan(&commands, &cancelObservations); err != nil {
		t.Fatal(err)
	}
	if commands != 1 || cancelObservations != 0 {
		t.Fatalf("cancel facts = commands:%d provider_observations:%d, want 1/0", commands, cancelObservations)
	}

	reloaded, err := fixture.base.repo.GetExecutionLifecycle(fixture.ctx, fixture.base.account.ID, working.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	replayedCommand, err := venue.NewCancellationCommand(reloaded, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !lifecycle.SameEventPayload(&replayedCommand.Event, &command.Event) {
		t.Fatal("fresh-process cancellation retry did not recover exact persisted command")
	}
}

func TestVenueCancellationCommandRecoversAfterCommittedTransportTimeout(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "cancel-timeout")
	working := fixture.persistAcknowledgement(t, newPostgresVenueResultStore(fixture.pool), "cancel-timeout-ack")
	command, err := venue.NewCancellationCommand(working, working.Events[len(working.Events)-1].ReceivedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected lost commit acknowledgement")
	store := &injectedCancellationPersistence{repository: fixture.base.repo, failure: injected}
	if _, err := venue.PersistCancellationCommand(
		fixture.ctx, store, fixture.base.account.ID, working, command,
	); !errors.Is(err, injected) {
		t.Fatalf("post-commit cancellation error = %v, want injected", err)
	}

	reloaded, err := fixture.base.repo.GetExecutionLifecycle(fixture.ctx, fixture.base.account.ID, working.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := venue.NewCancellationCommand(reloaded, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := venue.PersistCancellationCommand(
		fixture.ctx, fixture.base.repo, fixture.base.account.ID, reloaded, replayed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateWorking || !lifecycle.SameEventPayload(&command.Event, &replayed.Event) {
		t.Fatalf("recovered cancellation command = state:%s event:%s", persisted.State, replayed.Event.ID)
	}
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT COUNT(*) FROM execution_lifecycle_events
		WHERE intent_id=$1 AND kind='cancel_requested'`, working.Intent.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered cancellation command count = %d, want 1", count)
	}
}

func TestVenueResultPersistenceLinksRawTerminalObservation(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "raw-terminal")
	store := newPostgresVenueResultStore(fixture.pool)
	observation := fixture.stateObservation(t, fixture.aggregate, "cancelled-1", "canceled", venue.OutcomeCancelled)
	transition, err := lifecycle.ObserveOrderTerminal(
		fixture.aggregate,
		lifecycle.EventOrderCancelled,
		fixture.eventInput(observation, "provider_cancelled"),
		observation.CreatedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	finalAggregate, err := lifecycle.ApplyTransition(fixture.aggregate, transition)
	if err != nil {
		t.Fatal(err)
	}
	result := &venue.Result{
		Initial: fixture.aggregate, Aggregate: finalAggregate,
		Steps: []venue.ResultStep{{Observation: observation, Transition: transition}},
	}
	persisted, err := venue.PersistResult(fixture.ctx, store, fixture.base.account.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateCancelled {
		t.Fatalf("terminal result state = %s, want cancelled", persisted.State)
	}
}

func TestVenueResultPersistenceLinksBothRawBoundariesBeforeFill(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "raw-fill")
	store := newPostgresVenueResultStore(fixture.pool)
	working := fixture.persistAcknowledgement(t, store, "fill-ack")
	observation, economic, transition, finalAggregate := fixture.fillResult(t, working, "fill-1", "2", "0.42")
	result := &venue.Result{
		Initial: working, Aggregate: finalAggregate,
		Steps: []venue.ResultStep{{
			Observation: observation, EconomicSourceEvent: economic, Transition: transition,
		}},
	}
	persisted, err := venue.PersistResult(fixture.ctx, store, fixture.base.account.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StatePartiallyFilled || len(persisted.Fills) != 1 {
		t.Fatalf("fill result = state:%s fills:%d", persisted.State, len(persisted.Fills))
	}
	var observations, economicEvents, fills, normalizations int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM venue_observations WHERE id=$1),
		(SELECT COUNT(*) FROM economic_source_events WHERE id=$2),
		(SELECT COUNT(*) FROM execution_fills WHERE id=$3),
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id=$2)`,
		observation.ID, economic.ID, transition.Fill.ID,
	).Scan(&observations, &economicEvents, &fills, &normalizations); err != nil {
		t.Fatal(err)
	}
	if observations != 1 || economicEvents != 1 || fills != 1 || normalizations != 1 {
		t.Fatalf("raw-first fill graph = observation:%d economic:%d fill:%d normalization:%d", observations, economicEvents, fills, normalizations)
	}
}

func TestVenueResultPersistenceRestartsAfterEveryRetainedChildWrite(t *testing.T) {
	for _, failurePoint := range []string{"observation", "economic", "fill"} {
		t.Run(failurePoint, func(t *testing.T) {
			fixture := newVenueAdapterRepositoryFixture(t, "restart-"+failurePoint)
			baseStore := newPostgresVenueResultStore(fixture.pool)
			working := fixture.persistAcknowledgement(t, baseStore, "restart-ack-"+failurePoint)
			observation, economic, transition, finalAggregate := fixture.fillResult(
				t, working, "restart-fill-"+failurePoint, "2", "0.42",
			)
			result := &venue.Result{
				Initial: working, Aggregate: finalAggregate,
				Steps: []venue.ResultStep{{
					Observation: observation, EconomicSourceEvent: economic, Transition: transition,
				}},
			}
			injected := errors.New("injected post-write " + failurePoint + " failure")
			store := &injectedPostgresVenueResultStore{
				postgresVenueResultStore: baseStore, failurePoint: failurePoint, failure: injected,
			}
			if _, err := venue.PersistResult(fixture.ctx, store, fixture.base.account.ID, result); !errors.Is(err, injected) {
				t.Fatalf("first persistence error = %v, want injected", err)
			}
			persisted, err := venue.PersistResult(fixture.ctx, store, fixture.base.account.ID, result)
			if err != nil {
				t.Fatalf("restart after %s: %v", failurePoint, err)
			}
			if persisted.State != lifecycle.StatePartiallyFilled || len(persisted.Fills) != 1 {
				t.Fatalf("restart after %s = state:%s fills:%d", failurePoint, persisted.State, len(persisted.Fills))
			}
			var observations, economicEvents, fills int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
				(SELECT COUNT(*) FROM venue_observations WHERE id=$1),
				(SELECT COUNT(*) FROM economic_source_events WHERE id=$2),
				(SELECT COUNT(*) FROM execution_fills WHERE id=$3)`,
				observation.ID, economic.ID, transition.Fill.ID,
			).Scan(&observations, &economicEvents, &fills); err != nil {
				t.Fatal(err)
			}
			if observations != 1 || economicEvents != 1 || fills != 1 {
				t.Fatalf("restart counts after %s = observation:%d economic:%d fill:%d", failurePoint, observations, economicEvents, fills)
			}
		})
	}
}

func TestVenueResultPersistenceConcurrentPartialAndFinalFillReplayConverges(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "concurrent-fills")
	store := newPostgresVenueResultStore(fixture.pool)
	working := fixture.persistAcknowledgement(t, store, "concurrent-fill-ack")

	firstObservation, firstEconomic, firstTransition, partialAggregate := fixture.fillResult(
		t, working, "concurrent-fill-1", "2", "0.42",
	)
	partialResult := &venue.Result{
		Initial: working, Aggregate: partialAggregate,
		Steps: []venue.ResultStep{{
			Observation: firstObservation, EconomicSourceEvent: firstEconomic, Transition: firstTransition,
		}},
	}
	persistedPartial := persistVenueResultConcurrently(t, fixture, store, partialResult, lifecycle.StatePartiallyFilled)

	secondObservation, secondEconomic, secondTransition, filledAggregate := fixture.fillResult(
		t, persistedPartial, "concurrent-fill-2", "6", "0.43",
	)
	finalResult := &venue.Result{
		Initial: persistedPartial, Aggregate: filledAggregate,
		Steps: []venue.ResultStep{{
			Observation: secondObservation, EconomicSourceEvent: secondEconomic, Transition: secondTransition,
		}},
	}
	persistedFinal := persistVenueResultConcurrently(t, fixture, store, finalResult, lifecycle.StateFilled)
	if len(persistedFinal.Fills) != 2 {
		t.Fatalf("concurrent final fill count = %d, want 2", len(persistedFinal.Fills))
	}
	var observations, economicEvents, fills int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM venue_observations WHERE order_id=$1 AND mapped_outcome='fill'),
		(SELECT COUNT(*) FROM economic_source_events WHERE source='kalshi' AND source_namespace='kalshi/portfolio/fills'),
		(SELECT COUNT(*) FROM execution_fills WHERE order_id=$1)`, persistedFinal.Order.ID,
	).Scan(&observations, &economicEvents, &fills); err != nil {
		t.Fatal(err)
	}
	if observations != 2 || economicEvents != 2 || fills != 2 {
		t.Fatalf("concurrent fill facts = observation:%d economic:%d fill:%d, want 2/2/2", observations, economicEvents, fills)
	}
}

func TestVenueResultPersistenceConcurrentAcknowledgementTerminalAndFailureConverge(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		wantState lifecycle.State
		build     func(*testing.T, venueAdapterRepositoryFixture) *venue.Result
	}{
		{
			name: "no change", wantState: lifecycle.StateRouted,
			build: func(t *testing.T, fixture venueAdapterRepositoryFixture) *venue.Result {
				observation := fixture.stateObservation(t, fixture.aggregate, "concurrent-no-change", "resting", venue.OutcomeNoChange)
				return &venue.Result{
					Initial: fixture.aggregate, Aggregate: fixture.aggregate,
					Steps: []venue.ResultStep{{Observation: observation}},
				}
			},
		},
		{
			name: "acknowledgement", wantState: lifecycle.StateWorking,
			build: func(t *testing.T, fixture venueAdapterRepositoryFixture) *venue.Result {
				return fixture.acknowledgementResult(t, "concurrent-ack")
			},
		},
		{
			name: "terminal", wantState: lifecycle.StateCancelled,
			build: func(t *testing.T, fixture venueAdapterRepositoryFixture) *venue.Result {
				observation := fixture.stateObservation(t, fixture.aggregate, "concurrent-cancel", "canceled", venue.OutcomeCancelled)
				transition, err := lifecycle.ObserveOrderTerminal(
					fixture.aggregate, lifecycle.EventOrderCancelled,
					fixture.eventInput(observation, "provider_cancelled"), observation.CreatedAt,
				)
				if err != nil {
					t.Fatal(err)
				}
				finalAggregate, err := lifecycle.ApplyTransition(fixture.aggregate, transition)
				if err != nil {
					t.Fatal(err)
				}
				return &venue.Result{
					Initial: fixture.aggregate, Aggregate: finalAggregate,
					Steps: []venue.ResultStep{{Observation: observation, Transition: transition}},
				}
			},
		},
		{
			name: "unknown state", wantState: lifecycle.StateFailedReconciliation,
			build: func(t *testing.T, fixture venueAdapterRepositoryFixture) *venue.Result {
				observation := fixture.stateObservation(t, fixture.aggregate, "concurrent-unknown", "future_state", venue.OutcomeUnknownState)
				transition, err := lifecycle.FailReconciliation(
					fixture.aggregate, lifecycle.EventUnknownVenueState,
					fixture.eventInput(observation, "unknown_provider_state"), observation.CreatedAt,
				)
				if err != nil {
					t.Fatal(err)
				}
				finalAggregate, err := lifecycle.ApplyTransition(fixture.aggregate, transition)
				if err != nil {
					t.Fatal(err)
				}
				return &venue.Result{
					Initial: fixture.aggregate, Aggregate: finalAggregate,
					Steps: []venue.ResultStep{{Observation: observation, Transition: transition}},
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newVenueAdapterRepositoryFixture(t, "concurrent-"+testCase.name)
			store := newPostgresVenueResultStore(fixture.pool)
			persisted := persistVenueResultConcurrently(t, fixture, store, testCase.build(t, fixture), testCase.wantState)
			if persisted.State != testCase.wantState {
				t.Fatalf("concurrent %s state = %s, want %s", testCase.name, persisted.State, testCase.wantState)
			}
		})
	}
}

func persistVenueResultConcurrently(
	t *testing.T,
	fixture venueAdapterRepositoryFixture,
	store *postgresVenueResultStore,
	result *venue.Result,
	wantState lifecycle.State,
) *lifecycle.Aggregate {
	t.Helper()
	const writers = 8
	results := make(chan *lifecycle.Aggregate, writers)
	errorsFound := make(chan error, writers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			persisted, err := venue.PersistResult(fixture.ctx, store, fixture.base.account.ID, result)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- persisted
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	close(results)
	for err := range errorsFound {
		t.Errorf("concurrent venue result: %v", err)
	}
	var last *lifecycle.Aggregate
	for persisted := range results {
		if persisted.State != wantState {
			t.Errorf("concurrent venue result state = %s, want %s", persisted.State, wantState)
		}
		last = persisted
	}
	if t.Failed() {
		t.FailNow()
	}
	if last == nil {
		t.Fatal("concurrent venue result returned no aggregate")
	}
	return last
}

func newVenueAdapterIntegrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool := newSimulationPolicyIntegrationPool(t)
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000073_venue_adapter_observations.up.sql")); err != nil {
		t.Fatalf("apply migration 73: %v", err)
	}
	return ctx, pool
}

func newVenuePolicyArtifact(t *testing.T, provider venue.Provider) *venue.PolicyArtifact {
	t.Helper()
	policy, err := venue.ReviewedPolicy(provider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(time.Date(2026, 8, 15, 22, 0, 0, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

type venueAdapterRepositoryFixture struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	base      executionLifecycleFixture
	artifact  *venue.PolicyArtifact
	aggregate *lifecycle.Aggregate
}

func newVenueAdapterRepositoryFixture(t *testing.T, key string) venueAdapterRepositoryFixture {
	return newVenueAdapterRepositoryFixtureForOutcome(t, key, "yes")
}

func newVenueAdapterRepositoryFixtureForOutcome(t *testing.T, key, outcome string) venueAdapterRepositoryFixture {
	t.Helper()
	if outcome != "yes" && outcome != "no" {
		t.Fatalf("unsupported fixture outcome %q", outcome)
	}
	ctx, pool := newVenueAdapterIntegrationPool(t)
	return newVenueAdapterRepositoryFixtureWithPoolAndOutcome(t, ctx, pool, key, outcome)
}

func newVenueAdapterRepositoryFixtureWithPool(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	key string,
) venueAdapterRepositoryFixture {
	return newVenueAdapterRepositoryFixtureWithPoolAndOutcome(t, ctx, pool, key, "yes")
}

func newVenueAdapterRepositoryFixtureWithPoolAndOutcome(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	key, outcome string,
) venueAdapterRepositoryFixture {
	t.Helper()
	baseTime := time.Date(2026, 8, 15, 23, 0, 0, 123456000, time.UTC)
	suffix := uuid.NewString()
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "Kalshi venue adapter " + key, Environment: domain.AccountEnvironmentPaperScored,
		Venue: "kalshi", ExternalAccountID: "paper-" + suffix, BaseCurrency: "USD",
		StorageNamespace: "paper_scored/kalshi-venue-" + suffix,
		StartingCapital:  decimal.NewFromInt(100000), BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile: domain.MarginProfileCash, CreatedBy: "integration-test",
		CreationMetadata: json.RawMessage(`{"fixture":"venue-adapter"}`), CreatedAt: baseTime.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepo(pool).Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "kalshi:venue-adapter:" + suffix, AssetClass: instrument.AssetClassPredictionContract,
		PrimaryVenue: "kalshi", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementBinary, Status: instrument.StatusActive,
		Metadata: json.RawMessage(`{"fixture":"venue-adapter"}`), CreatedAt: baseTime.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	reference, err = NewInstrumentRepo(pool).CreateInstrument(ctx, reference)
	if err != nil {
		t.Fatal(err)
	}
	validTo := baseTime.Add(24 * time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: reference.ID, Venue: "kalshi", ContractID: "KX-" + stringsToUpperWithoutHyphens(suffix),
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementBinary,
		ValidFrom: baseTime.Add(-24 * time.Hour), ValidTo: &validTo,
		Metadata: json.RawMessage(`{"kalshi_v2":{"outcome":"` + outcome + `"}}`), CreatedAt: baseTime.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err = NewInstrumentRepo(pool).RegisterVenueContract(ctx, contract)
	if err != nil {
		t.Fatal(err)
	}
	exchangeAt := baseTime.Add(-3 * time.Second)
	receivedAt := exchangeAt.Add(time.Second)
	availableAt := receivedAt.Add(time.Second)
	bid := decimal.RequireFromString("0.41")
	ask := decimal.RequireFromString("0.43")
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: reference.ID, VenueContractID: &contract.ID, Provider: "kalshi", Venue: "kalshi",
		Source: "fixture-feed", ObservationNamespace: "quotes/kalshi", ObservationID: "quote-" + suffix,
		ExchangeAt: &exchangeAt, ReceivedAt: receivedAt, AvailableAt: &availableAt,
		Bid: &bid, Ask: &ask, MarketStatus: "open", SessionStatus: "regular",
		Metadata: json.RawMessage(`{"fixture":"venue-adapter"}`), CreatedAt: availableAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	base := executionLifecycleFixture{
		ctx: ctx, pool: pool, repo: NewExecutionLifecycleRepo(pool), account: account,
		instrument: reference, contract: contract, snapshot: snapshot, baseTime: baseTime,
	}
	artifact := newVenuePolicyArtifact(t, venue.ProviderKalshi)
	if _, err := NewVenueAdapterRepo(pool).RegisterVenuePolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	aggregate := base.persistRiskApproved(t, key)
	eventInput := base.nextEvent(
		aggregate, "route-"+key, "router", "venue-route", "order_routed", json.RawMessage(`{"route":"kalshi"}`),
	)
	route, err := lifecycle.Route(aggregate, lifecycle.RouteInput{
		OrderIdempotencyKey: "order-" + key, Instrument: *reference, VenueContract: *contract,
		RouteSnapshot: *snapshot, QuoteRequirements: marketdata.QuoteRequirements{
			RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
		},
		OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceGTC,
		LimitPrice: decimalExecutionPointer("0.42"), PolicyKind: lifecycle.PolicyVenue,
		PolicyVersion: artifact.Version, Event: eventInput,
		RoutedAt: eventInput.ReceivedAt, CreatedAt: eventInput.ReceivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err = base.repo.ApplyExecutionTransition(ctx, account.ID, route)
	if err != nil {
		t.Fatal(err)
	}
	return venueAdapterRepositoryFixture{ctx: ctx, pool: pool, base: base, artifact: artifact, aggregate: aggregate}
}

func (fixture venueAdapterRepositoryFixture) observation(
	t *testing.T,
	sourceEventID string,
	outcome venue.MappedOutcome,
) *venue.Observation {
	t.Helper()
	providerPrice := decimal.RequireFromString("0.42")
	receivedAt := fixture.aggregate.Events[len(fixture.aggregate.Events)-1].ReceivedAt.Add(time.Second)
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: fixture.base.account.ID, IntentID: fixture.aggregate.Intent.ID,
		OrderID: fixture.aggregate.Order.ID, VenueContractID: fixture.base.contract.ID,
		Provider: venue.ProviderKalshi, Venue: "kalshi", PolicyVersion: fixture.artifact.Version,
		Kind: venue.ObservationOrderSnapshot, ProviderState: "resting", MappedOutcome: outcome,
		ExternalOrderID: "external-" + fixture.aggregate.Order.ID.String(),
		ClientOrderID:   fixture.aggregate.Order.ClientOrderID, ProviderContractID: fixture.base.contract.ContractID,
		CanonicalOutcome: "yes", ProviderBookSide: "bid", ProviderAction: "buy", ProviderPrice: &providerPrice,
		IdentityKind: venue.SourceIdentityProvider, SourceNamespace: "kalshi/portfolio/order-snapshots",
		SourceEventID: sourceEventID, SourceRevision: "1", SourceAt: receivedAt.Add(-time.Millisecond),
		ReceivedAt: receivedAt, RawPayload: json.RawMessage(`{"status":"resting"}`), CreatedAt: receivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func (fixture venueAdapterRepositoryFixture) stateObservation(
	t *testing.T,
	aggregate *lifecycle.Aggregate,
	sourceEventID, providerState string,
	outcome venue.MappedOutcome,
) *venue.Observation {
	t.Helper()
	providerPrice := decimal.RequireFromString("0.42")
	receivedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	input := venue.ObservationInput{
		AccountID: fixture.base.account.ID, IntentID: aggregate.Intent.ID,
		OrderID: aggregate.Order.ID, VenueContractID: fixture.base.contract.ID,
		Provider: venue.ProviderKalshi, Venue: "kalshi", PolicyVersion: fixture.artifact.Version,
		Kind: venue.ObservationOrderSnapshot, ProviderState: providerState, MappedOutcome: outcome,
		ExternalOrderID: "external-" + aggregate.Order.ID.String(),
		ClientOrderID:   aggregate.Order.ClientOrderID, ProviderContractID: fixture.base.contract.ContractID,
		CanonicalOutcome: "yes", ProviderBookSide: "bid", ProviderAction: "buy", ProviderPrice: &providerPrice,
		IdentityKind: venue.SourceIdentityProvider, SourceNamespace: "kalshi/portfolio/order-snapshots",
		SourceEventID: sourceEventID, SourceRevision: "1", SourceAt: receivedAt.Add(-time.Millisecond),
		ReceivedAt: receivedAt, RawPayload: json.RawMessage(`{"status":"` + providerState + `"}`),
		CreatedAt: receivedAt,
	}
	if aggregate.Binding != nil {
		input.BindingID = &aggregate.Binding.ID
		input.ExternalOrderID = aggregate.Binding.ExternalOrderID
	}
	observation, err := venue.NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func (fixture venueAdapterRepositoryFixture) eventInput(
	observation *venue.Observation,
	reasonCode string,
) lifecycle.EventInput {
	return lifecycle.EventInput{
		Source: string(observation.Provider), SourceNamespace: observation.SourceNamespace,
		SourceEventID: observation.SourceEventID, SourceRevision: observation.SourceRevision,
		SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
		Actor: "venue-adapter", ReasonCode: reasonCode, Evidence: observation.RawPayload,
	}
}

func (fixture venueAdapterRepositoryFixture) persistAcknowledgement(
	t *testing.T,
	store *postgresVenueResultStore,
	sourceEventID string,
) *lifecycle.Aggregate {
	t.Helper()
	result := fixture.acknowledgementResult(t, sourceEventID)
	persisted, err := venue.PersistResult(fixture.ctx, store, fixture.base.account.ID, result)
	if err != nil {
		t.Fatal(err)
	}
	return persisted
}

func (fixture venueAdapterRepositoryFixture) acknowledgementResult(
	t *testing.T,
	sourceEventID string,
) *venue.Result {
	t.Helper()
	observation := fixture.stateObservation(t, fixture.aggregate, sourceEventID, "resting", venue.OutcomeAcknowledge)
	transition, err := lifecycle.Acknowledge(
		fixture.aggregate, observation.ExternalOrderID,
		fixture.eventInput(observation, "provider_acknowledged"), observation.CreatedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	finalAggregate, err := lifecycle.ApplyTransition(fixture.aggregate, transition)
	if err != nil {
		t.Fatal(err)
	}
	return &venue.Result{
		Initial: fixture.aggregate, Aggregate: finalAggregate,
		Steps: []venue.ResultStep{{Observation: observation, Transition: transition}},
	}
}

func (fixture venueAdapterRepositoryFixture) fillResult(
	t *testing.T,
	aggregate *lifecycle.Aggregate,
	sourceEventID, quantity, price string,
) (*venue.Observation, *ledger.EconomicSourceEvent, *lifecycle.Transition, *lifecycle.Aggregate) {
	t.Helper()
	receivedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	raw := json.RawMessage(`{"fill_id":"` + sourceEventID + `","count_fp":"` + quantity + `","yes_price_dollars":"` + price + `"}`)
	providerPrice := decimal.RequireFromString(price)
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: fixture.base.account.ID, IntentID: aggregate.Intent.ID,
		OrderID: aggregate.Order.ID, BindingID: &aggregate.Binding.ID,
		VenueContractID: fixture.base.contract.ID, Provider: venue.ProviderKalshi,
		Venue: "kalshi", PolicyVersion: fixture.artifact.Version, Kind: venue.ObservationFill,
		ProviderState: "fill", MappedOutcome: venue.OutcomeFill,
		ExternalOrderID: aggregate.Binding.ExternalOrderID, ClientOrderID: aggregate.Order.ClientOrderID,
		ProviderContractID: fixture.base.contract.ContractID, CanonicalOutcome: "yes",
		ProviderBookSide: "bid", ProviderAction: "buy", ProviderPrice: &providerPrice,
		IdentityKind: venue.SourceIdentityProvider, SourceNamespace: "kalshi/portfolio/fills",
		SourceEventID: sourceEventID, SourceRevision: "1", SourceAt: receivedAt.Add(-time.Millisecond),
		ReceivedAt: receivedAt, RawPayload: raw, CreatedAt: receivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	economic, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: observation.AccountID, Source: string(observation.Provider),
		SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
		SourceRevision: observation.SourceRevision, ObservedAt: observation.ReceivedAt,
		RawPayload: observation.RawPayload, CreatedAt: observation.CreatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	fillID := lifecycle.FillID(aggregate.Order.ID, economic.ID)
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent: economic, Account: fixture.base.account, NormalizerVersion: "venue-adapter-v1",
			ExecutionOriginType: aggregate.Intent.OriginType, ExecutionOriginID: aggregate.Intent.OriginID,
			ReferenceType: "execution_fill", ReferenceID: fillID.String(), EffectiveAt: observation.SourceAt,
		},
		Instrument: *fixture.base.instrument, VenueContract: *fixture.base.contract, Side: ledger.FillSideBuy,
		Quantity: decimal.RequireFromString(quantity), Price: decimal.RequireFromString(price),
	})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := lifecycle.RecordFill(aggregate, lifecycle.FillInput{
		Normalization: normalization, ExternalOrderID: aggregate.Binding.ExternalOrderID,
		Event: fixture.eventInput(observation, "fill_reported"), CreatedAt: observation.CreatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	finalAggregate, err := lifecycle.ApplyTransition(aggregate, transition)
	if err != nil {
		t.Fatal(err)
	}
	return observation, economic, transition, finalAggregate
}

type postgresVenueResultStore struct {
	venue     *VenueAdapterRepo
	economic  *LedgerRepo
	lifecycle *ExecutionLifecycleRepo
}

type injectedCancellationPersistence struct {
	repository *ExecutionLifecycleRepo
	failure    error
	fired      bool
}

func (store *injectedCancellationPersistence) ApplyExecutionTransition(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	persisted, err := store.repository.ApplyExecutionTransition(ctx, accountID, transition)
	if err != nil {
		return nil, err
	}
	if !store.fired {
		store.fired = true
		return nil, store.failure
	}
	return persisted, nil
}

type injectedPostgresVenueResultStore struct {
	*postgresVenueResultStore
	failurePoint string
	failure      error
	fired        bool
}

func (store *injectedPostgresVenueResultStore) failAfter(point string) error {
	if !store.fired && store.failurePoint == point {
		store.fired = true
		return store.failure
	}
	return nil
}

func (store *injectedPostgresVenueResultStore) RecordVenueObservation(
	ctx context.Context,
	observation *venue.Observation,
) (*venue.Observation, error) {
	persisted, err := store.postgresVenueResultStore.RecordVenueObservation(ctx, observation)
	if err != nil {
		return nil, err
	}
	if err := store.failAfter("observation"); err != nil {
		return nil, err
	}
	return persisted, nil
}

func (store *injectedPostgresVenueResultStore) RecordEconomicSourceEvent(
	ctx context.Context,
	event *ledger.EconomicSourceEvent,
) (*ledger.EconomicSourceEvent, error) {
	persisted, err := store.postgresVenueResultStore.RecordEconomicSourceEvent(ctx, event)
	if err != nil {
		return nil, err
	}
	if err := store.failAfter("economic"); err != nil {
		return nil, err
	}
	return persisted, nil
}

func (store *injectedPostgresVenueResultStore) ApplyExecutionFill(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	persisted, err := store.postgresVenueResultStore.ApplyExecutionFill(ctx, accountID, transition)
	if err != nil {
		return nil, err
	}
	if err := store.failAfter("fill"); err != nil {
		return nil, err
	}
	return persisted, nil
}

func newPostgresVenueResultStore(pool *pgxpool.Pool) *postgresVenueResultStore {
	return &postgresVenueResultStore{
		venue: NewVenueAdapterRepo(pool), economic: NewLedgerRepo(pool), lifecycle: NewExecutionLifecycleRepo(pool),
	}
}

func (store *postgresVenueResultStore) RecordVenueObservation(
	ctx context.Context,
	observation *venue.Observation,
) (*venue.Observation, error) {
	return store.venue.RecordVenueObservation(ctx, observation)
}

func (store *postgresVenueResultStore) RecordEconomicSourceEvent(
	ctx context.Context,
	event *ledger.EconomicSourceEvent,
) (*ledger.EconomicSourceEvent, error) {
	return store.economic.RecordEconomicSourceEvent(ctx, event)
}

func (store *postgresVenueResultStore) ApplyExecutionFill(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	return store.lifecycle.ApplyExecutionFill(ctx, accountID, transition)
}

func (store *postgresVenueResultStore) ApplyExecutionTransition(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	return store.lifecycle.ApplyExecutionTransition(ctx, accountID, transition)
}

func stringsToUpperWithoutHyphens(value string) string {
	var result []byte
	for index := range len(value) {
		character := value[index]
		if character == '-' {
			continue
		}
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}
		result = append(result, character)
	}
	return string(result)
}
