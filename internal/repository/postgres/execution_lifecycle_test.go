package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestExecutionLifecycleRepoReplaysProposalAndRejectsChangedPayload(t *testing.T) {
	fixture := newExecutionLifecycleFixture(t)
	proposed := fixture.propose(t, "proposal-replay")

	created, err := fixture.repo.ProposeExecutionIntent(fixture.ctx, proposed)
	if err != nil {
		t.Fatalf("ProposeExecutionIntent() error = %v", err)
	}
	replayed, err := fixture.repo.ProposeExecutionIntent(fixture.ctx, proposed)
	if err != nil {
		t.Fatalf("ProposeExecutionIntent(retry) error = %v", err)
	}
	if replayed.Intent.ID != created.Intent.ID || len(replayed.Events) != 1 {
		t.Fatalf("replayed proposal = %s/%d events, want %s/1", replayed.Intent.ID, len(replayed.Events), created.Intent.ID)
	}

	changedInput := fixture.proposeInput("proposal-replay")
	changedInput.DesiredQuantityDelta = decimal.NewFromInt(9)
	changed, err := lifecycle.Propose(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.ProposeExecutionIntent(fixture.ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed proposal error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestExecutionLifecycleRepoConcurrentProposalRetryConverges(t *testing.T) {
	fixture := newExecutionLifecycleFixture(t)
	proposed := fixture.propose(t, "proposal-concurrent")
	const writers = 8
	errorsFound := make(chan error, writers)
	var wait sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			result, err := fixture.repo.ProposeExecutionIntent(fixture.ctx, proposed)
			if err != nil {
				errorsFound <- err
				return
			}
			if result.Intent.ID != proposed.Intent.ID || len(result.Events) != 1 {
				errorsFound <- errors.New("concurrent proposal returned a different aggregate")
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent ProposeExecutionIntent() error = %v", err)
	}
	var intentCount, eventCount int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM execution_intents WHERE id = $1),
		(SELECT COUNT(*) FROM execution_lifecycle_events WHERE intent_id = $1)`, proposed.Intent.ID).Scan(&intentCount, &eventCount); err != nil {
		t.Fatal(err)
	}
	if intentCount != 1 || eventCount != 1 {
		t.Fatalf("concurrent proposal counts = intent:%d event:%d, want 1/1", intentCount, eventCount)
	}
}

func TestExecutionLifecycleRepoConcurrentRouteAndAcknowledgementRetriesConverge(t *testing.T) {
	fixture := newExecutionLifecycleFixture(t)
	approved := fixture.persistRiskApproved(t, "concurrent-route")
	route := fixture.routeTransition(t, approved, "concurrent-route")

	routed := applyExecutionTransitionConcurrently(t, fixture, route, lifecycle.StateRouted)
	var orderCount, routeEventCount int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM execution_orders WHERE intent_id = $1),
		(SELECT COUNT(*) FROM execution_lifecycle_events WHERE intent_id = $1 AND kind = 'order_routed')`,
		routed.Intent.ID,
	).Scan(&orderCount, &routeEventCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 || routeEventCount != 1 {
		t.Fatalf("concurrent route counts = order:%d event:%d, want 1/1", orderCount, routeEventCount)
	}

	ackInput := fixture.nextEvent(
		routed,
		"ack-concurrent-route",
		"simulation",
		"simulation-policy-v1",
		"acknowledged",
		json.RawMessage(`{"external_order_id":"sim-order-concurrent-route"}`),
	)
	acknowledgement, err := lifecycle.Acknowledge(
		routed,
		"sim-order-concurrent-route",
		ackInput,
		ackInput.ReceivedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	working := applyExecutionTransitionConcurrently(t, fixture, acknowledgement, lifecycle.StateWorking)
	var bindingCount, acknowledgementEventCount int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM execution_order_bindings WHERE order_id = $1),
		(SELECT COUNT(*) FROM execution_lifecycle_events WHERE intent_id = $2 AND kind = 'order_working')`,
		working.Order.ID, working.Intent.ID,
	).Scan(&bindingCount, &acknowledgementEventCount); err != nil {
		t.Fatal(err)
	}
	if bindingCount != 1 || acknowledgementEventCount != 1 {
		t.Fatalf("concurrent acknowledgement counts = binding:%d event:%d, want 1/1", bindingCount, acknowledgementEventCount)
	}
}

func TestExecutionLifecycleRepoPersistsImmediateZeroPriceFillAndReloads(t *testing.T) {
	fixture := newExecutionLifecycleFixture(t)
	routed := fixture.persistRouted(t, "immediate-zero")

	recovery, err := fixture.repo.ListExecutionRecoveryCandidates(fixture.ctx, fixture.account.ID, 10)
	if err != nil {
		t.Fatalf("ListExecutionRecoveryCandidates() error = %v", err)
	}
	if len(recovery) != 1 || recovery[0].Intent.ID != routed.Intent.ID {
		t.Fatalf("routed recovery candidates = %#v", recovery)
	}

	fillTransition := fixture.fillTransition(t, routed, "fill-zero", "8", "0", "sim-order-zero")
	filled, err := fixture.repo.ApplyExecutionFill(fixture.ctx, fixture.account.ID, fillTransition)
	if err != nil {
		t.Fatalf("ApplyExecutionFill() error = %v", err)
	}
	if filled.State != lifecycle.StateFilled || filled.Binding == nil || len(filled.Fills) != 1 || !filled.Fills[0].Price.IsZero() {
		t.Fatalf("filled aggregate = state:%s binding:%v fills:%#v", filled.State, filled.Binding, filled.Fills)
	}

	fresh := NewExecutionLifecycleRepo(fixture.pool)
	reloaded, err := fresh.GetExecutionLifecycle(fixture.ctx, fixture.account.ID, filled.Intent.ID)
	if err != nil {
		t.Fatalf("fresh GetExecutionLifecycle() error = %v", err)
	}
	if reloaded.State != lifecycle.StateFilled || len(reloaded.Events) != 5 || len(reloaded.Fills) != 1 || !reloaded.Fills[0].Price.IsZero() {
		t.Fatalf("reloaded aggregate = state:%s events:%d fills:%#v", reloaded.State, len(reloaded.Events), reloaded.Fills)
	}

	recovery, err = fresh.ListExecutionRecoveryCandidates(fixture.ctx, fixture.account.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovery) != 0 {
		t.Fatalf("filled lifecycle remained recoverable: %#v", recovery)
	}
	assertExecutionFillGraphCounts(t, fixture, filled.Intent.ID, 1, 1, 1, 1)
}

func TestExecutionLifecycleRepoConcurrentImmediateFillRetryConverges(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		quantity string
		state    lifecycle.State
	}{
		{name: "partial", quantity: "3", state: lifecycle.StatePartiallyFilled},
		{name: "complete", quantity: "8", state: lifecycle.StateFilled},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newExecutionLifecycleFixture(t)
			routed := fixture.persistRouted(t, "concurrent-fill-"+testCase.name)
			transition := fixture.fillTransition(
				t,
				routed,
				"fill-concurrent-"+testCase.name,
				testCase.quantity,
				"10.25",
				"sim-order-concurrent-"+testCase.name,
			)

			const writers = 8
			results := make(chan *lifecycle.Aggregate, writers)
			errorsFound := make(chan error, writers)
			var wait sync.WaitGroup
			var ready sync.WaitGroup
			ready.Add(writers)
			start := make(chan struct{})
			for range writers {
				wait.Add(1)
				go func() {
					defer wait.Done()
					ready.Done()
					<-start
					result, err := fixture.repo.ApplyExecutionFill(fixture.ctx, fixture.account.ID, transition)
					if err != nil {
						errorsFound <- err
						return
					}
					results <- result
				}()
			}
			ready.Wait()
			close(start)
			wait.Wait()
			close(errorsFound)
			close(results)
			for err := range errorsFound {
				t.Errorf("concurrent ApplyExecutionFill() error = %v", err)
			}
			for result := range results {
				if result.State != testCase.state || len(result.Fills) != 1 {
					t.Errorf("concurrent result = state:%s fills:%d, want %s/1", result.State, len(result.Fills), testCase.state)
				}
			}
			if t.Failed() {
				return
			}
			assertExecutionFillGraphCounts(t, fixture, routed.Intent.ID, 1, 1, 1, 1)
		})
	}
}

func TestExecutionLifecycleRepoPersistsMultiplePartialFillsExactly(t *testing.T) {
	fixture := newExecutionLifecycleFixture(t)
	routed := fixture.persistRouted(t, "partial-fills")
	first := fixture.fillTransition(t, routed, "fill-partial-1", "3", "10.25", "sim-order-partial")
	partial, err := fixture.repo.ApplyExecutionFill(fixture.ctx, fixture.account.ID, first)
	if err != nil {
		t.Fatalf("ApplyExecutionFill(first) error = %v", err)
	}
	if partial.State != lifecycle.StatePartiallyFilled || partial.Binding == nil || len(partial.Fills) != 1 {
		t.Fatalf("first partial aggregate = state:%s binding:%v fills:%d", partial.State, partial.Binding, len(partial.Fills))
	}
	second := fixture.fillTransition(t, partial, "fill-partial-2", "5", "10.26", "sim-order-partial")
	filled, err := fixture.repo.ApplyExecutionFill(fixture.ctx, fixture.account.ID, second)
	if err != nil {
		t.Fatalf("ApplyExecutionFill(second) error = %v", err)
	}
	if filled.State != lifecycle.StateFilled || len(filled.Fills) != 2 ||
		filled.Events[len(filled.Events)-1].CumulativeFillQuantity == nil ||
		!filled.Events[len(filled.Events)-1].CumulativeFillQuantity.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("final partial aggregate = state:%s fills:%d cumulative:%v", filled.State, len(filled.Fills), filled.Events[len(filled.Events)-1].CumulativeFillQuantity)
	}
	assertExecutionFillGraphCounts(t, fixture, filled.Intent.ID, 2, 1, 2, 2)
}

func TestExecutionLifecycleRepoConcurrentCorrectionAndBustRetriesFailClosedOnce(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		kind             lifecycle.EventKind
		observationClass lifecycle.ObservationClass
		reasonCode       string
	}{
		{name: "correction", kind: lifecycle.EventFillCorrectionObserved, observationClass: lifecycle.ObservationCorrection, reasonCode: "fill_corrected"},
		{name: "bust", kind: lifecycle.EventFillBustObserved, observationClass: lifecycle.ObservationBust, reasonCode: "fill_busted"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newExecutionLifecycleFixture(t)
			routed := fixture.persistRouted(t, testCase.name)
			fillTransition := fixture.fillTransition(t, routed, "fill-"+testCase.name, "8", "10.25", "sim-order-"+testCase.name)
			filled, err := fixture.repo.ApplyExecutionFill(fixture.ctx, fixture.account.ID, fillTransition)
			if err != nil {
				t.Fatal(err)
			}
			revisionInput := lifecycle.EventInput{
				Source: fillTransition.Fill.Source, SourceNamespace: fillTransition.Fill.SourceNamespace,
				SourceEventID: testCase.name + "-observation-1", SourceRevision: "2",
				ObservationClass: testCase.observationClass, ObservationDiscriminator: "revision:2",
				SourceAt:   filled.Events[len(filled.Events)-1].ReceivedAt.Add(time.Second - time.Millisecond),
				ReceivedAt: filled.Events[len(filled.Events)-1].ReceivedAt.Add(time.Second),
				Actor:      "simulation-reconciler", ReasonCode: testCase.reasonCode,
				Evidence:       json.RawMessage(`{"revision":"2","status":"revised"}`),
				OriginalFillID: &fillTransition.Fill.ID, OriginalSourceEventID: fillTransition.Fill.SourceEventID,
			}
			revision, err := lifecycle.FailReconciliation(
				filled,
				testCase.kind,
				revisionInput,
				revisionInput.ReceivedAt,
			)
			if err != nil {
				t.Fatal(err)
			}
			const writers = 8
			errorsFound := make(chan error, writers)
			var wait sync.WaitGroup
			var ready sync.WaitGroup
			ready.Add(writers)
			start := make(chan struct{})
			for range writers {
				wait.Add(1)
				go func() {
					defer wait.Done()
					ready.Done()
					<-start
					result, err := fixture.repo.ApplyExecutionTransition(fixture.ctx, fixture.account.ID, revision)
					if err != nil {
						errorsFound <- err
						return
					}
					if result.State != lifecycle.StateFailedReconciliation {
						errorsFound <- errors.New(testCase.name + " did not fail closed")
					}
				}()
			}
			ready.Wait()
			close(start)
			wait.Wait()
			close(errorsFound)
			for err := range errorsFound {
				t.Errorf("concurrent %s error = %v", testCase.name, err)
			}
			var revisionCount int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT COUNT(*) FROM execution_lifecycle_events
				WHERE intent_id = $1 AND kind = $2`, filled.Intent.ID, testCase.kind).Scan(&revisionCount); err != nil {
				t.Fatal(err)
			}
			if revisionCount != 1 {
				t.Fatalf("%s event count = %d, want 1", testCase.name, revisionCount)
			}
			assertExecutionFillGraphCounts(t, fixture, filled.Intent.ID, 1, 1, 1, 1)

			changedInput := revisionInput
			changedInput.SourceEventID = testCase.name + "-observation-2"
			changedInput.Evidence = json.RawMessage(`{"revision":"2","status":"different"}`)
			changed, err := lifecycle.FailReconciliation(
				filled,
				testCase.kind,
				changedInput,
				changedInput.ReceivedAt,
			)
			if err != nil {
				t.Fatal(err)
			}
			if changed.Event.ID != revision.Event.ID {
				t.Fatalf("changed %s ID = %s, want stable identity %s", testCase.name, changed.Event.ID, revision.Event.ID)
			}
			if _, err := fixture.repo.ApplyExecutionTransition(fixture.ctx, fixture.account.ID, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
				t.Fatalf("changed %s error = %v, want ErrIdempotencyConflict", testCase.name, err)
			}

			tx, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(fixture.ctx) }()
			if err := insertExecutionLifecycleEvent(fixture.ctx, tx, &changed.Event); err == nil ||
				!strings.Contains(err.Error(), "execution lifecycle event idempotency conflict") {
				t.Fatalf("direct changed %s insert error = %v, want database idempotency conflict", testCase.name, err)
			}
		})
	}
}

func TestExecutionLifecycleRepoRollsBackWholeFillGraphOnChildFailure(t *testing.T) {
	fixture := newExecutionLifecycleFixture(t)
	routed := fixture.persistRouted(t, "fill-rollback")
	transition := fixture.fillTransition(t, routed, "fill-rollback", "8", "10.25", "sim-order-rollback")
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION reject_test_execution_fill() RETURNS TRIGGER AS $$
		BEGIN RAISE EXCEPTION 'injected execution fill failure'; END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER trg_test_execution_fill_failure BEFORE INSERT ON execution_fills
		FOR EACH ROW EXECUTE FUNCTION reject_test_execution_fill()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(fixture.ctx, `DROP TRIGGER IF EXISTS trg_test_execution_fill_failure ON execution_fills;
			DROP FUNCTION IF EXISTS reject_test_execution_fill()`)
	})

	if _, err := fixture.repo.ApplyExecutionFill(fixture.ctx, fixture.account.ID, transition); err == nil {
		t.Fatal("ApplyExecutionFill() with injected child failure unexpectedly succeeded")
	}
	assertExecutionFillGraphCounts(t, fixture, routed.Intent.ID, 0, 0, 0, 0)
}

func TestExecutionLifecyclePersistentRehearsal(t *testing.T) {
	databaseURL := os.Getenv("AUGR_EXECUTION_LIFECYCLE_REHEARSAL_DB_URL")
	if databaseURL == "" {
		t.Skip("set AUGR_EXECUTION_LIFECYCLE_REHEARSAL_DB_URL only for an explicitly disposable schema-71 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	immediate := newExecutionLifecycleFixtureWithPool(t, ctx, pool)
	routed := immediate.persistRouted(t, "persistent-immediate-"+uuid.NewString())
	transition := immediate.fillTransition(t, routed, "persistent-fill-"+uuid.NewString(), "8", "0", "persistent-order-"+uuid.NewString())
	filled, err := immediate.repo.ApplyExecutionFill(ctx, immediate.account.ID, transition)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded, err := NewExecutionLifecycleRepo(pool).GetExecutionLifecycle(ctx, immediate.account.ID, filled.Intent.ID); err != nil || reloaded.State != lifecycle.StateFilled {
		t.Fatalf("reload immediate rehearsal = state:%v error:%v", func() lifecycle.State {
			if reloaded == nil {
				return lifecycle.StateNone
			}
			return reloaded.State
		}(), err)
	}

	partialFixture := newExecutionLifecycleFixtureWithPool(t, ctx, pool)
	partialRouted := partialFixture.persistRouted(t, "persistent-partial-"+uuid.NewString())
	first := partialFixture.fillTransition(t, partialRouted, "persistent-partial-1-"+uuid.NewString(), "3", "10.25", "persistent-partial-order-"+uuid.NewString())
	partial, err := partialFixture.repo.ApplyExecutionFill(ctx, partialFixture.account.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	second := partialFixture.fillTransition(t, partial, "persistent-partial-2-"+uuid.NewString(), "5", "10.26", partial.Binding.ExternalOrderID)
	completed, err := partialFixture.repo.ApplyExecutionFill(ctx, partialFixture.account.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != lifecycle.StateFilled || len(completed.Fills) != 2 {
		t.Fatalf("partial rehearsal = state:%s fills:%d", completed.State, len(completed.Fills))
	}

	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000071_common_execution_lifecycle.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 71") {
		t.Fatalf("nonempty rehearsal rollback error = %v", err)
	}
}

type executionLifecycleFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	repo       *ExecutionLifecycleRepo
	account    *domain.Account
	instrument *instrument.Instrument
	contract   *instrument.VenueContract
	snapshot   *marketdata.QuoteSnapshot
	baseTime   time.Time
}

func newExecutionLifecycleFixture(t *testing.T) executionLifecycleFixture {
	t.Helper()
	ctx := context.Background()
	pool := newExecutionLifecycleIntegrationPool(t, ctx)
	return newExecutionLifecycleFixtureWithPool(t, ctx, pool)
}

func newExecutionLifecycleFixtureWithPool(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) executionLifecycleFixture {
	t.Helper()
	account, err := NewAccountRepo(pool).GetByID(ctx, uuid.MustParse("00000000-0000-4000-8000-000000000064"))
	if err != nil {
		t.Fatal(err)
	}
	suffix := uuid.NewString()
	baseTime := time.Date(2026, 8, 15, 18, 0, 0, 123456000, time.UTC)
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:lifecycle:" + suffix, AssetClass: instrument.AssetClassEquity,
		PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive,
		Metadata: json.RawMessage(`{"fixture":"execution-lifecycle"}`), CreatedAt: baseTime.Add(-time.Hour),
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
		InstrumentID: reference.ID, Venue: "test-venue", ContractID: "LIFECYCLE-" + suffix,
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical,
		ValidFrom: baseTime.Add(-24 * time.Hour), ValidTo: &validTo,
		Metadata: json.RawMessage(`{"fixture":"execution-lifecycle"}`), CreatedAt: baseTime.Add(-time.Hour),
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
	bid := decimal.RequireFromString("10.24")
	ask := decimal.RequireFromString("10.26")
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: reference.ID, VenueContractID: &contract.ID, Provider: "fixture",
		Venue: contract.Venue, Source: "fixture-feed", ObservationNamespace: "quotes/lifecycle",
		ObservationID: "quote-" + suffix, ExchangeAt: &exchangeAt, ReceivedAt: receivedAt,
		AvailableAt: &availableAt, Bid: &bid, Ask: &ask, MarketStatus: "open",
		SessionStatus: "regular", Metadata: json.RawMessage(`{"fixture":"execution-lifecycle"}`),
		CreatedAt: availableAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return executionLifecycleFixture{
		ctx: ctx, pool: pool, repo: NewExecutionLifecycleRepo(pool), account: account,
		instrument: reference, contract: contract, snapshot: snapshot, baseTime: baseTime,
	}
}

func (fixture executionLifecycleFixture) proposeInput(key string) lifecycle.ProposeInput {
	return lifecycle.ProposeInput{
		Account: *fixture.account, Instrument: *fixture.instrument, DecisionSnapshot: *fixture.snapshot,
		IdempotencyKey: key, DesiredQuantityDelta: decimal.NewFromInt(8), DecisionAt: fixture.baseTime,
		OriginType: ledger.ExecutionOriginStrategyVersion, OriginID: "strategy-version-1",
		StrategyVersionID: "strategy-version-1", Metadata: json.RawMessage(`{"signal":"entry"}`),
		Event: lifecycle.EventInput{
			Source: "strategy", SourceNamespace: "strategy-version-1", SourceEventID: "proposal-" + key,
			SourceAt: fixture.baseTime.Add(-time.Millisecond), ReceivedAt: fixture.baseTime,
			Actor: "strategy-runner", ReasonCode: "signal_proposed", Evidence: json.RawMessage(`{"signal":"entry"}`),
		},
		CreatedAt: fixture.baseTime,
	}
}

func (fixture executionLifecycleFixture) propose(t *testing.T, key string) *lifecycle.Aggregate {
	t.Helper()
	aggregate, err := lifecycle.Propose(fixture.proposeInput(key))
	if err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func (fixture executionLifecycleFixture) persistRouted(t *testing.T, key string) *lifecycle.Aggregate {
	t.Helper()
	aggregate := fixture.persistRiskApproved(t, key)
	route := fixture.routeTransition(t, aggregate, key)
	var err error
	aggregate, err = fixture.repo.ApplyExecutionTransition(fixture.ctx, fixture.account.ID, route)
	if err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func (fixture executionLifecycleFixture) persistRiskApproved(t *testing.T, key string) *lifecycle.Aggregate {
	t.Helper()
	aggregate, err := fixture.repo.ProposeExecutionIntent(fixture.ctx, fixture.propose(t, key))
	if err != nil {
		t.Fatal(err)
	}
	allocationInput := fixture.nextEvent(aggregate, "allocation-"+key, "allocator", "allocation", "allocated", json.RawMessage(`{"quantity":"8"}`))
	allocation, err := lifecycle.Allocate(aggregate, decimal.NewFromInt(8), allocationInput, allocationInput.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err = fixture.repo.ApplyExecutionTransition(fixture.ctx, fixture.account.ID, allocation)
	if err != nil {
		t.Fatal(err)
	}
	riskInput := fixture.nextEvent(aggregate, "risk-"+key, "risk", "risk-policy-v1", "approved", json.RawMessage(`{"approved":true}`))
	approval, err := lifecycle.ApproveRisk(aggregate, riskInput, riskInput.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err = fixture.repo.ApplyExecutionTransition(fixture.ctx, fixture.account.ID, approval)
	if err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func (fixture executionLifecycleFixture) routeTransition(t *testing.T, aggregate *lifecycle.Aggregate, key string) *lifecycle.Transition {
	t.Helper()
	routeInput := fixture.nextEvent(aggregate, "route-"+key, "router", "simulation-policy-v1", "order_routed", json.RawMessage(`{"route":"simulation"}`))
	routedAt := routeInput.ReceivedAt
	route, err := lifecycle.Route(aggregate, lifecycle.RouteInput{
		OrderIdempotencyKey: "order-" + key, Instrument: *fixture.instrument, VenueContract: *fixture.contract,
		RouteSnapshot: *fixture.snapshot, QuoteRequirements: marketdata.QuoteRequirements{
			RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
		},
		OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceDay,
		LimitPrice: decimalExecutionPointer("10.25"), PolicyKind: lifecycle.PolicySimulation,
		PolicyVersion: "simulation-policy-v1", Event: routeInput, RoutedAt: routedAt, CreatedAt: routedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func applyExecutionTransitionConcurrently(
	t *testing.T,
	fixture executionLifecycleFixture,
	transition *lifecycle.Transition,
	wantState lifecycle.State,
) *lifecycle.Aggregate {
	t.Helper()
	const writers = 8
	results := make(chan *lifecycle.Aggregate, writers)
	errorsFound := make(chan error, writers)
	var wait sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			result, err := fixture.repo.ApplyExecutionTransition(fixture.ctx, fixture.account.ID, transition)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	close(results)
	for err := range errorsFound {
		t.Errorf("concurrent ApplyExecutionTransition() error = %v", err)
	}
	var result *lifecycle.Aggregate
	for candidate := range results {
		if candidate.State != wantState {
			t.Errorf("concurrent transition state = %s, want %s", candidate.State, wantState)
		}
		result = candidate
	}
	if t.Failed() {
		t.FailNow()
	}
	if result == nil {
		t.Fatal("concurrent transition returned no aggregate")
	}
	return result
}

func (fixture executionLifecycleFixture) fillTransition(
	t *testing.T,
	aggregate *lifecycle.Aggregate,
	sourceEventID, quantity, price, externalOrderID string,
) *lifecycle.Transition {
	t.Helper()
	receivedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	raw := json.RawMessage(`{"execution_id":"` + sourceEventID + `","quantity":"` + quantity + `","price":"` + price + `"}`)
	source, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: fixture.account.ID, Source: "simulation", SourceNamespace: "simulation-policy-v1",
		SourceEventID: sourceEventID, ObservedAt: receivedAt, RawPayload: raw, CreatedAt: receivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLedgerRepo(fixture.pool).RecordEconomicSourceEvent(fixture.ctx, source); err != nil {
		t.Fatal(err)
	}
	fillID := lifecycle.FillID(aggregate.Order.ID, source.ID)
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent: source, Account: fixture.account, NormalizerVersion: "execution-lifecycle-v1",
			ExecutionOriginType: aggregate.Intent.OriginType, ExecutionOriginID: aggregate.Intent.OriginID,
			ReferenceType: "execution_fill", ReferenceID: fillID.String(), EffectiveAt: receivedAt.Add(-time.Millisecond),
		},
		Instrument: *fixture.instrument, VenueContract: *fixture.contract, Side: ledger.FillSideBuy,
		Quantity: decimal.RequireFromString(quantity), Price: decimal.RequireFromString(price),
	})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := lifecycle.RecordFill(aggregate, lifecycle.FillInput{
		Normalization: normalization, ExternalOrderID: externalOrderID,
		Event: lifecycle.EventInput{
			Source: source.Source, SourceNamespace: source.SourceNamespace, SourceEventID: source.SourceEventID,
			SourceRevision: source.SourceRevision, SourceAt: receivedAt.Add(-time.Millisecond), ReceivedAt: receivedAt,
			Actor: "simulation-venue", ReasonCode: "fill_reported", Evidence: raw,
		},
		CreatedAt: receivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return transition
}

func (fixture executionLifecycleFixture) nextEvent(
	aggregate *lifecycle.Aggregate,
	eventID, source, namespace, reason string,
	evidence json.RawMessage,
) lifecycle.EventInput {
	receivedAt := aggregate.Events[len(aggregate.Events)-1].ReceivedAt.Add(time.Second)
	return lifecycle.EventInput{
		Source: source, SourceNamespace: namespace, SourceEventID: eventID,
		SourceAt: receivedAt.Add(-time.Millisecond), ReceivedAt: receivedAt,
		Actor: source, ReasonCode: reason, Evidence: evidence,
	}
}

func assertExecutionFillGraphCounts(
	t *testing.T,
	fixture executionLifecycleFixture,
	intentID uuid.UUID,
	fillCount, bindingCount, normalizationCount, transactionCount int,
) {
	t.Helper()
	var fills, bindings, normalizations, transactions int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM execution_fills WHERE intent_id = $1),
		(SELECT COUNT(*) FROM execution_order_bindings AS binding
		 JOIN execution_orders AS execution_order ON execution_order.id = binding.order_id
		 WHERE execution_order.intent_id = $1),
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE reference_type = 'execution_fill'),
		(SELECT COUNT(*) FROM ledger_transactions WHERE reference_type = 'execution_fill')`, intentID).Scan(
		&fills, &bindings, &normalizations, &transactions,
	); err != nil {
		t.Fatal(err)
	}
	if fills != fillCount || bindings != bindingCount || normalizations != normalizationCount || transactions != transactionCount {
		t.Fatalf("graph counts = fill:%d binding:%d normalization:%d transaction:%d, want %d/%d/%d/%d",
			fills, bindings, normalizations, transactions, fillCount, bindingCount, normalizationCount, transactionCount)
	}
}

func decimalExecutionPointer(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}

func newExecutionLifecycleIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool := newLedgerIntegrationPool(t, ctx)
	for _, migrationName := range []string{
		"000069_ledger_projections.up.sql",
		"000070_accounting_dual_run.up.sql",
		"000071_common_execution_lifecycle.up.sql",
	} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migrationName)); err != nil {
			t.Fatalf("apply %s: %v", migrationName, err)
		}
	}
	return pool
}

func repositoryMigrationSQL(t *testing.T, migrationName string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations", migrationName))
	if err != nil {
		t.Fatalf("read %s: %v", migrationName, err)
	}
	return string(data)
}
