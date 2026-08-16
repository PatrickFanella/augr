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

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestSimulationVenuePersistsRawFirstAndRecoversPinnedPolicy(t *testing.T) {
	fixture := newPostgresSimulationVenueFixture(t, lifecycle.TimeInForceDay, 6*time.Hour)
	snapshot := fixture.snapshot(t, "pinned-policy", fixture.routed.Order.RoutedAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(8)}},
	)
	result, err := fixture.venue.Evaluate(fixture.request(fixture.routed, snapshot, *snapshot.AvailableAt))
	if err != nil {
		t.Fatal(err)
	}
	store := fixture.persistence()
	persisted, err := simulation.PersistResult(fixture.ctx, store, fixture.accountID(), result)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 1 {
		t.Fatalf("persisted lifecycle = state:%s fills:%d", persisted.State, len(persisted.Fills))
	}
	assertSimulationVenueGraphCounts(t, fixture, 1, 1, 1, 1, 1)

	changed := fixture.policyInput()
	changed.Assets[0].Fees.PerOrder = decimal.RequireFromString("9.99")
	current, err := simulation.NewPolicy(changed)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version() == fixture.policy.Version() {
		t.Fatal("changed current policy retained routed version")
	}
	freshPolicies := NewSimulationPolicyRepo(fixture.pool)
	artifact, err := freshPolicies.GetSimulationPolicyByVersion(fixture.ctx, persisted.Order.PolicyVersion)
	if err != nil {
		t.Fatal(err)
	}
	recoveredPolicy, err := simulation.PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	recoveredVenue, err := simulation.NewVenue(recoveredPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredVenue.PolicyVersion() != fixture.policy.Version() || recoveredVenue.PolicyVersion() == current.Version() {
		t.Fatalf("recovered/current policy = %q/%q", recoveredVenue.PolicyVersion(), current.Version())
	}
}

func TestSimulationVenueInterruptedMultiLevelRestartResumes(t *testing.T) {
	fixture := newPostgresSimulationVenueFixture(t, lifecycle.TimeInForceDay, 6*time.Hour)
	snapshot := fixture.snapshot(t, "interrupted-levels", fixture.routed.Order.RoutedAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(3)},
			{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(5)},
		},
	)
	full, err := fixture.venue.Evaluate(fixture.request(fixture.routed, snapshot, *snapshot.AvailableAt))
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Transitions) != 2 || len(full.Fills) != 2 {
		t.Fatalf("multi-level result = transitions:%d fills:%d", len(full.Transitions), len(full.Fills))
	}
	firstAggregate, err := lifecycle.ApplyTransition(fixture.routed, full.Transitions[0])
	if err != nil {
		t.Fatal(err)
	}
	first := &simulation.Result{
		Decision: simulation.DecisionPartiallyFilled, Aggregate: firstAggregate,
		Transitions: full.Transitions[:1], Fills: full.Fills[:1],
	}
	if _, err := simulation.PersistResult(fixture.ctx, fixture.persistence(), fixture.accountID(), first); err != nil {
		t.Fatal(err)
	}
	assertSimulationVenueGraphCounts(t, fixture, 1, 1, 1, 1, 1)

	freshLifecycle := NewExecutionLifecycleRepo(fixture.pool)
	reloaded, err := freshLifecycle.GetExecutionLifecycle(fixture.ctx, fixture.accountID(), fixture.routed.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	freshArtifact, err := NewSimulationPolicyRepo(fixture.pool).GetSimulationPolicyByVersion(fixture.ctx, reloaded.Order.PolicyVersion)
	if err != nil {
		t.Fatal(err)
	}
	freshPolicy, err := simulation.PolicyFromArtifact(*freshArtifact)
	if err != nil {
		t.Fatal(err)
	}
	freshVenue, err := simulation.NewVenue(freshPolicy)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := freshVenue.Evaluate(fixture.request(reloaded, snapshot, *snapshot.AvailableAt))
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Fills) != 1 || resumed.Fills[0].DepthLevel != 1 || resumed.Aggregate.State != lifecycle.StateFilled {
		t.Fatalf("resumed result = state:%s fills:%#v", resumed.Aggregate.State, resumed.Fills)
	}
	if resumed.Fills[0].Fee == nil || full.Fills[1].Fee == nil || !resumed.Fills[0].Fee.Equal(*full.Fills[1].Fee) {
		t.Fatalf("resumed later-level fee = %v, want %v", resumed.Fills[0].Fee, full.Fills[1].Fee)
	}
	if _, err := simulation.PersistResult(fixture.ctx, fixture.persistence(), fixture.accountID(), resumed); err != nil {
		t.Fatal(err)
	}
	assertSimulationVenueGraphCounts(t, fixture, 2, 2, 1, 2, 2)
}

func TestSimulationVenueConcurrentObservationConverges(t *testing.T) {
	fixture := newPostgresSimulationVenueFixture(t, lifecycle.TimeInForceDay, 6*time.Hour)
	snapshot := fixture.snapshot(t, "concurrent-levels", fixture.routed.Order.RoutedAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(3)},
			{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(5)},
		},
	)
	result, err := fixture.venue.Evaluate(fixture.request(fixture.routed, snapshot, *snapshot.AvailableAt))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var ready sync.WaitGroup
	var wait sync.WaitGroup
	ready.Add(workers)
	start := make(chan struct{})
	errorsFound := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			persisted, persistErr := simulation.PersistResult(fixture.ctx, fixture.persistence(), fixture.accountID(), result)
			if persistErr != nil {
				errorsFound <- persistErr
				return
			}
			if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 2 {
				errorsFound <- errors.New("concurrent persistence returned incomplete lifecycle")
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent simulation persistence: %v", err)
	}
	assertSimulationVenueGraphCounts(t, fixture, 2, 2, 1, 2, 2)
}

func TestSimulationVenueSessionExpiryPersistsOnceWithoutEconomics(t *testing.T) {
	for name, closeOffset := range map[string]time.Duration{
		"normal":   6 * time.Hour,
		"half-day": 3*time.Hour + 30*time.Minute,
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newPostgresSimulationVenueFixture(t, lifecycle.TimeInForceDay, closeOffset)
			closeAt := fixture.sessionClose
			snapshot := fixture.snapshot(t, "expiry-"+name, closeAt,
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(8)}},
			)
			result, err := fixture.venue.Evaluate(fixture.request(fixture.routed, snapshot, closeAt))
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := simulation.PersistResult(fixture.ctx, fixture.persistence(), fixture.accountID(), result)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.State != lifecycle.StateExpired {
				t.Fatalf("persisted expiry state = %s", persisted.State)
			}
			later := fixture.snapshot(t, "after-expiry-"+name, closeAt.Add(time.Second),
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(8)}},
			)
			noop, err := fixture.venue.Evaluate(fixture.request(persisted, later, *later.AvailableAt))
			if err != nil {
				t.Fatal(err)
			}
			if len(noop.Transitions) != 0 || len(noop.Fills) != 0 {
				t.Fatalf("post-expiry replay = transitions:%d fills:%d", len(noop.Transitions), len(noop.Fills))
			}
			assertSimulationVenueGraphCounts(t, fixture, 0, 0, 0, 0, 0)
		})
	}
}

func TestSimulationVenueFOKAndStaleQuoteCreateNoEconomicRows(t *testing.T) {
	fixture := newPostgresSimulationVenueFixture(t, lifecycle.TimeInForceFOK, 6*time.Hour)
	snapshot := fixture.snapshot(t, "fok-insufficient", fixture.routed.Order.RoutedAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(3)}},
	)
	result, err := fixture.venue.Evaluate(fixture.request(fixture.routed, snapshot, *snapshot.AvailableAt))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := simulation.PersistResult(fixture.ctx, fixture.persistence(), fixture.accountID(), result); err != nil {
		t.Fatal(err)
	}
	assertSimulationVenueGraphCounts(t, fixture, 0, 0, 0, 0, 0)

	staleFixture := newPostgresSimulationVenueFixture(t, lifecycle.TimeInForceDay, 6*time.Hour)
	stale := staleFixture.snapshot(t, "stale", staleFixture.routed.Order.RoutedAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(8)}},
	)
	oldExchange := staleFixture.routed.Order.RoutedAt.Add(-3 * time.Second)
	stale.ExchangeAt = &oldExchange
	if _, err := staleFixture.venue.Evaluate(staleFixture.request(staleFixture.routed, stale, *stale.AvailableAt)); err == nil {
		t.Fatal("stale quote unexpectedly evaluated")
	}
	assertSimulationVenueGraphCounts(t, staleFixture, 0, 0, 0, 0, 0)
}

type postgresSimulationVenueFixture struct {
	ctx          context.Context
	pool         *pgxpool.Pool
	execution    executionLifecycleFixture
	policy       *simulation.Policy
	venue        *simulation.Venue
	routed       *lifecycle.Aggregate
	sessionClose time.Time
}

func newPostgresSimulationVenueFixture(
	t *testing.T,
	timeInForce lifecycle.TimeInForce,
	closeOffset time.Duration,
) postgresSimulationVenueFixture {
	t.Helper()
	ctx := context.Background()
	pool := newExecutionLifecycleIntegrationPool(t, ctx)
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000072_simulation_policy_artifacts.up.sql")); err != nil {
		t.Fatalf("apply migration 72: %v", err)
	}
	execution := newExecutionLifecycleFixtureWithPool(t, ctx, pool)
	sessionOpen := execution.baseTime.Add(-time.Hour)
	sessionClose := execution.baseTime.Add(closeOffset)
	policyInput := postgresSimulationPolicyInput(sessionOpen, sessionClose)
	policy, err := simulation.NewPolicy(policyInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := simulation.RegisterPolicy(ctx, NewSimulationPolicyRepo(pool), policy, execution.baseTime.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	approved := execution.persistRiskApproved(t, "simulation-venue-"+uuid.NewString())
	routeEvent := execution.nextEvent(
		approved, "route-simulation-venue", "router", "simulation/route", "order_routed",
		json.RawMessage(`{"route":"simulation"}`),
	)
	route, err := lifecycle.Route(approved, lifecycle.RouteInput{
		OrderIdempotencyKey: "simulation-venue-order", Instrument: *execution.instrument,
		VenueContract: *execution.contract, RouteSnapshot: *execution.snapshot,
		QuoteRequirements: marketdata.QuoteRequirements{
			RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
		},
		OrderType: lifecycle.OrderLimit, TimeInForce: timeInForce,
		LimitPrice: decimalExecutionPointer("10.25"), PolicyKind: lifecycle.PolicySimulation,
		PolicyVersion: policy.Version(), Event: routeEvent,
		RoutedAt: routeEvent.ReceivedAt, CreatedAt: routeEvent.ReceivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	routed, err := execution.repo.ApplyExecutionTransition(ctx, execution.account.ID, route)
	if err != nil {
		t.Fatal(err)
	}
	venue, err := simulation.NewVenue(policy)
	if err != nil {
		t.Fatal(err)
	}
	return postgresSimulationVenueFixture{
		ctx: ctx, pool: pool, execution: execution, policy: policy, venue: venue,
		routed: routed, sessionClose: sessionClose,
	}
}

func postgresSimulationPolicyInput(openAt, closeAt time.Time) simulation.PolicyInput {
	return simulation.PolicyInput{
		Schema: simulation.PolicySchemaV1,
		Assets: []simulation.AssetPolicy{{
			AssetClass: instrument.AssetClassEquity,
			OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
			TimeInForce: []lifecycle.TimeInForce{
				lifecycle.TimeInForceDay, lifecycle.TimeInForceGTC,
				lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK,
			},
			QuoteRequirements: marketdata.QuoteRequirements{
				RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
				RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true,
				RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"},
				AllowedSessionStatuses: []string{"regular"}, MaxAge: 2 * time.Second,
			},
			MaxDepthParticipation: decimal.NewFromInt(1), FixedLatency: 100 * time.Millisecond,
			Calendar: simulation.CalendarPolicy{
				Kind:     simulation.CalendarExplicitSessions,
				Sessions: []simulation.SessionWindow{{Label: "fixture-session", OpenAt: openAt, CloseAt: closeAt}},
			},
			Fees: simulation.FeePolicy{
				PerOrder: decimal.RequireFromString("1.25"), PerUnit: decimal.RequireFromString("0.01"),
				NotionalBPS: decimal.RequireFromString("2"), Scale: 4,
			},
		}},
	}
}

func (fixture postgresSimulationVenueFixture) policyInput() simulation.PolicyInput {
	return postgresSimulationPolicyInput(fixture.routed.Order.RoutedAt.Add(-time.Hour), fixture.sessionClose)
}

func (fixture postgresSimulationVenueFixture) accountID() uuid.UUID {
	return fixture.execution.account.ID
}

func (fixture postgresSimulationVenueFixture) persistence() *postgresSimulationPersistence {
	return &postgresSimulationPersistence{
		ledger: NewLedgerRepo(fixture.pool), lifecycle: NewExecutionLifecycleRepo(fixture.pool),
	}
}

func (fixture postgresSimulationVenueFixture) request(
	aggregate *lifecycle.Aggregate,
	snapshot marketdata.QuoteSnapshot,
	evaluatedAt time.Time,
) simulation.EvaluationRequest {
	return simulation.EvaluationRequest{
		Account: *fixture.execution.account, Instrument: *fixture.execution.instrument,
		VenueContract: *fixture.execution.contract, Aggregate: aggregate,
		Snapshot: snapshot, EvaluatedAt: evaluatedAt,
	}
}

func (fixture postgresSimulationVenueFixture) snapshot(
	t *testing.T,
	label string,
	availableAt time.Time,
	bids, asks []marketdata.DepthLevelInput,
) marketdata.QuoteSnapshot {
	t.Helper()
	exchangeAt := availableAt.Add(-50 * time.Millisecond)
	receivedAt := availableAt.Add(-25 * time.Millisecond)
	var bid, bidSize, ask, askSize *decimal.Decimal
	if len(bids) > 0 {
		bidValue, sizeValue := bids[0].Price, bids[0].Size
		bid, bidSize = &bidValue, &sizeValue
	}
	if len(asks) > 0 {
		askValue, sizeValue := asks[0].Price, asks[0].Size
		ask, askSize = &askValue, &sizeValue
	}
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: fixture.execution.instrument.ID, VenueContractID: &fixture.execution.contract.ID,
		Provider: "simulation-fixture", Venue: fixture.execution.contract.Venue, Source: "fixture-feed",
		ObservationNamespace: "quotes/simulation-venue", ObservationID: label, SourceRevision: "v1",
		ExchangeAt: &exchangeAt, ReceivedAt: receivedAt, AvailableAt: &availableAt,
		Bid: bid, BidSize: bidSize, Ask: ask, AskSize: askSize,
		MarketStatus: "open", SessionStatus: "regular", Bids: bids, Asks: asks,
		Metadata: json.RawMessage(`{"fixture":"simulation-venue"}`), CreatedAt: availableAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := NewQuoteSnapshotRepo(fixture.pool).RecordQuoteSnapshot(fixture.ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return *persisted
}

type postgresSimulationPersistence struct {
	ledger    *LedgerRepo
	lifecycle *ExecutionLifecycleRepo
}

func (store *postgresSimulationPersistence) RecordEconomicSourceEvent(ctx context.Context, event *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error) {
	return store.ledger.RecordEconomicSourceEvent(ctx, event)
}

func (store *postgresSimulationPersistence) ApplyExecutionFill(ctx context.Context, accountID uuid.UUID, transition *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	return store.lifecycle.ApplyExecutionFill(ctx, accountID, transition)
}

func (store *postgresSimulationPersistence) ApplyExecutionTransition(ctx context.Context, accountID uuid.UUID, transition *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	return store.lifecycle.ApplyExecutionTransition(ctx, accountID, transition)
}

func assertSimulationVenueGraphCounts(
	t *testing.T,
	fixture postgresSimulationVenueFixture,
	rawCount, fillCount, bindingCount, normalizationCount, transactionCount int,
) {
	t.Helper()
	var raw, fills, bindings, normalizations, transactions int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM economic_source_events WHERE account_id = $1 AND source = 'simulation'),
		(SELECT COUNT(*) FROM execution_fills WHERE intent_id = $2),
		(SELECT COUNT(*) FROM execution_order_bindings AS binding
		 JOIN execution_orders AS execution_order ON execution_order.id = binding.order_id
		 WHERE execution_order.intent_id = $2),
		(SELECT COUNT(*) FROM economic_event_normalizations AS normalization
		 JOIN economic_source_events AS source_event ON source_event.id = normalization.source_event_id
		 WHERE source_event.account_id = $1 AND normalization.reference_type = 'execution_fill'),
		(SELECT COUNT(*) FROM ledger_transactions WHERE account_id = $1 AND reference_type = 'execution_fill')`,
		fixture.accountID(), fixture.routed.Intent.ID,
	).Scan(&raw, &fills, &bindings, &normalizations, &transactions); err != nil {
		t.Fatal(err)
	}
	if raw != rawCount || fills != fillCount || bindings != bindingCount || normalizations != normalizationCount || transactions != transactionCount {
		t.Fatalf("simulation graph counts = raw:%d fill:%d binding:%d normalization:%d transaction:%d, want %d/%d/%d/%d/%d",
			raw, fills, bindings, normalizations, transactions,
			rawCount, fillCount, bindingCount, normalizationCount, transactionCount,
		)
	}
}
