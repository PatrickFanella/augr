package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/alpaca"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestAlpacaVenueAdapterStreamAndActivityOrderConverge(t *testing.T) {
	for _, streamFirst := range []bool{true, false} {
		name := "activity-first"
		if streamFirst {
			name = "stream-first"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAlpacaVenueAdapterFixture(t, name)
			fillResult := fixture.planFills(t, []alpaca.FillActivityFact{
				fixture.fillFact(t, "fill-one", "8", "10.25", "8", "0"),
			})
			store := newPostgresVenueResultStore(fixture.pool)
			if streamFirst {
				streamResult := fixture.planStream(t, fixture.aggregate, "fill", "8", fixture.contract.ContractID)
				if _, err := venue.PersistResult(fixture.ctx, store, fixture.account.ID, streamResult); err != nil {
					t.Fatal(err)
				}
				if _, err := venue.PersistResult(fixture.ctx, store, fixture.account.ID, fillResult); err != nil {
					t.Fatal(err)
				}
			} else {
				persisted, err := venue.PersistResult(fixture.ctx, store, fixture.account.ID, fillResult)
				if err != nil {
					t.Fatal(err)
				}
				streamResult := fixture.planStream(t, persisted, "fill", "8", fixture.contract.ContractID)
				if _, err := venue.PersistResult(fixture.ctx, store, fixture.account.ID, streamResult); err != nil {
					t.Fatal(err)
				}
			}

			fixture.assertGraphCounts(t, 2, 1, 1, 1, 1)
			fresh, err := NewExecutionLifecycleRepo(fixture.pool).GetExecutionLifecycle(
				fixture.ctx, fixture.account.ID, fixture.aggregate.Intent.ID,
			)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.State != lifecycle.StateFilled || len(fresh.Fills) != 1 {
				t.Fatalf("fresh replay = state:%s fills:%d", fresh.State, len(fresh.Fills))
			}

			// A fresh process may retry the original stale plan after a committed
			// response timeout. Exact child identities make that replay converge.
			if _, err := venue.PersistResult(
				fixture.ctx, newPostgresVenueResultStore(fixture.pool), fixture.account.ID, fillResult,
			); err != nil {
				t.Fatalf("fresh-process replay: %v", err)
			}
			fixture.assertGraphCounts(t, 2, 1, 1, 1, 1)
		})
	}
}

func TestAlpacaVenueAdapterDistinctEqualFillsAndConcurrentReplayConverge(t *testing.T) {
	fixture := newAlpacaVenueAdapterFixture(t, "equal-concurrent")
	result := fixture.planFills(t, []alpaca.FillActivityFact{
		fixture.fillFact(t, "equal-fill-one", "4", "10.25", "4", "4"),
		fixture.fillFact(t, "equal-fill-two", "4", "10.25", "8", "0"),
	})
	if result.Steps[0].EconomicSourceEvent.ID == result.Steps[1].EconomicSourceEvent.ID ||
		result.Steps[0].Transition.Fill.ID == result.Steps[1].Transition.Fill.ID {
		t.Fatal("distinct provider activity identities collapsed before persistence")
	}

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
			persisted, err := venue.PersistResult(
				fixture.ctx, newPostgresVenueResultStore(fixture.pool), fixture.account.ID, result,
			)
			if err != nil {
				errorsFound <- err
				return
			}
			if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 2 {
				errorsFound <- fmt.Errorf("concurrent replay returned %s/%d fills", persisted.State, len(persisted.Fills))
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent Alpaca replay: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	fixture.assertGraphCounts(t, 2, 2, 2, 2, 2)
}

func TestAlpacaVenueAdapterContradictorySecondFeedAddsNoEconomics(t *testing.T) {
	fixture := newAlpacaVenueAdapterFixture(t, "contradictory-stream")
	fillResult := fixture.planFills(t, []alpaca.FillActivityFact{
		fixture.fillFact(t, "only-economic-fill", "8", "10.25", "8", "0"),
	})
	store := newPostgresVenueResultStore(fixture.pool)
	filled, err := venue.PersistResult(fixture.ctx, store, fixture.account.ID, fillResult)
	if err != nil {
		t.Fatal(err)
	}
	contradiction := fixture.planStream(t, filled, "fill", "8", "WRONG-SYMBOL")
	if contradiction.Steps[0].Observation.MappedOutcome != venue.OutcomeContradiction ||
		contradiction.Steps[0].EconomicSourceEvent != nil {
		t.Fatalf("contradictory stream result = %#v", contradiction.Steps[0])
	}
	failed, err := venue.PersistResult(fixture.ctx, store, fixture.account.ID, contradiction)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != lifecycle.StateFailedReconciliation {
		t.Fatalf("contradictory stream state = %s", failed.State)
	}
	fixture.assertGraphCounts(t, 2, 1, 1, 1, 1)
}

type alpacaVenueAdapterFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	base       executionLifecycleFixture
	account    *domain.Account
	instrument *instrument.Instrument
	contract   *instrument.VenueContract
	policy     *venue.Policy
	aggregate  *lifecycle.Aggregate
	baseTime   time.Time
	externalID string
}

func newAlpacaVenueAdapterFixture(t *testing.T, key string) alpacaVenueAdapterFixture {
	t.Helper()
	ctx, pool := newVenueAdapterIntegrationPool(t)
	baseTime := time.Date(2026, 8, 15, 20, 0, 0, 123456000, time.UTC)
	suffix := stringsToUpperWithoutHyphens(uuid.NewString())
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "Alpaca venue " + key, Environment: domain.AccountEnvironmentPaperScored,
		Venue: "alpaca", ExternalAccountID: "paper-" + suffix, BaseCurrency: "USD",
		StorageNamespace: "paper_scored/alpaca-" + suffix,
		StartingCapital:  decimal.NewFromInt(100000), BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile: domain.MarginProfileCash, CreatedBy: "integration-test",
		CreationMetadata: json.RawMessage(`{"fixture":"alpaca-venue"}`), CreatedAt: baseTime.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepo(pool).Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:alpaca:" + suffix, AssetClass: instrument.AssetClassEquity,
		PrimaryVenue: "alpaca", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive,
		Metadata: json.RawMessage(`{"fixture":"alpaca-venue"}`), CreatedAt: baseTime.Add(-time.Hour),
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
		InstrumentID: reference.ID, Venue: "alpaca", ContractID: "AUGR" + suffix[:8],
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical,
		ValidFrom: baseTime.Add(-24 * time.Hour), ValidTo: &validTo,
		Metadata: json.RawMessage(`{"fixture":"alpaca-venue"}`), CreatedAt: baseTime.Add(-time.Hour),
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
		InstrumentID: reference.ID, VenueContractID: &contract.ID, Provider: "alpaca", Venue: "alpaca",
		Source: "fixture-feed", ObservationNamespace: "quotes/alpaca", ObservationID: "quote-" + suffix,
		ExchangeAt: &exchangeAt, ReceivedAt: receivedAt, AvailableAt: &availableAt,
		Bid: &bid, Ask: &ask, MarketStatus: "open", SessionStatus: "regular",
		Metadata: json.RawMessage(`{"fixture":"alpaca-venue"}`), CreatedAt: availableAt,
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
	policy, err := venue.ReviewedPolicy(venue.ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(baseTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVenueAdapterRepo(pool).RegisterVenuePolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	aggregate := base.persistRiskApproved(t, key+"-"+suffix)
	eventInput := base.nextEvent(
		aggregate, "route-"+suffix, "router", "venue-route", "order_routed", json.RawMessage(`{"route":"alpaca"}`),
	)
	limitPrice := decimal.RequireFromString("10.25")
	route, err := lifecycle.Route(aggregate, lifecycle.RouteInput{
		OrderIdempotencyKey: "order-" + suffix, Instrument: *reference, VenueContract: *contract,
		RouteSnapshot: *snapshot, QuoteRequirements: marketdata.QuoteRequirements{
			RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
		},
		OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceDay, LimitPrice: &limitPrice,
		PolicyKind: lifecycle.PolicyVenue, PolicyVersion: policy.Version(), Event: eventInput,
		RoutedAt: eventInput.ReceivedAt, CreatedAt: eventInput.ReceivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err = base.repo.ApplyExecutionTransition(ctx, account.ID, route)
	if err != nil {
		t.Fatal(err)
	}
	return alpacaVenueAdapterFixture{
		ctx: ctx, pool: pool, base: base, account: account, instrument: reference, contract: contract,
		policy: policy, aggregate: aggregate, baseTime: baseTime, externalID: "alpaca-order-" + suffix,
	}
}

func (fixture alpacaVenueAdapterFixture) adapterContext(aggregate *lifecycle.Aggregate) alpaca.CommonLifecycleContext {
	return alpaca.CommonLifecycleContext{
		Policy: fixture.policy, Aggregate: aggregate, Account: fixture.account,
		Instrument: fixture.instrument, VenueContract: fixture.contract,
		ReceivedAt: fixture.baseTime.Add(10 * time.Second),
	}
}

func (fixture alpacaVenueAdapterFixture) fillFact(
	t *testing.T,
	id, quantity, price, cumulative, leaves string,
) alpaca.FillActivityFact {
	t.Helper()
	activity := alpaca.FillActivity{
		ID: id + "-" + fixture.aggregate.Order.ID.String(), ActivityType: "FILL",
		OrderID: fixture.externalID, ClientOrderID: fixture.aggregate.Order.ClientOrderID,
		Quantity: quantity, Price: price, Side: string(fixture.aggregate.Order.Side),
		Symbol: fixture.contract.ContractID, TransactionTime: fixture.baseTime.Add(5 * time.Second).Format(time.RFC3339Nano),
		CumulativeQuantity: cumulative, LeavesQuantity: leaves,
	}
	raw, err := json.Marshal(activity)
	if err != nil {
		t.Fatal(err)
	}
	return alpaca.FillActivityFact{Activity: activity, RawPayload: raw}
}

func (fixture alpacaVenueAdapterFixture) planFills(
	t *testing.T,
	facts []alpaca.FillActivityFact,
) *venue.Result {
	t.Helper()
	result, err := alpaca.PlanFillActivityResult(fixture.adapterContext(fixture.aggregate), facts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (fixture alpacaVenueAdapterFixture) planStream(
	t *testing.T,
	aggregate *lifecycle.Aggregate,
	event, filledQuantity, symbol string,
) *venue.Result {
	t.Helper()
	providerOrder := alpaca.CommonOrder{
		ID: fixture.externalID, ClientOrderID: aggregate.Order.ClientOrderID, Symbol: symbol,
		Side: string(aggregate.Order.Side), Type: string(aggregate.Order.OrderType),
		TimeInForce: string(aggregate.Order.TimeInForce), Quantity: aggregate.Order.Quantity.String(),
		FilledQuantity: filledQuantity, FilledAvgPrice: "999.99", Status: "filled",
		UpdatedAt: fixture.baseTime.Add(6 * time.Second).Format(time.RFC3339Nano),
	}
	wire := struct {
		Event     string             `json:"event"`
		Timestamp string             `json:"timestamp"`
		Order     alpaca.CommonOrder `json:"order"`
	}{
		Event: event, Timestamp: fixture.baseTime.Add(6 * time.Second).Format(time.RFC3339Nano), Order: providerOrder,
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	update, err := alpaca.ParseTradeUpdate(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := alpaca.PlanTradeUpdateResult(fixture.adapterContext(aggregate), update)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (fixture alpacaVenueAdapterFixture) assertGraphCounts(
	t *testing.T,
	observationCount, economicCount, fillCount, normalizationCount, transactionCount int,
) {
	t.Helper()
	var observations, economics, fills, normalizations, transactions int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM venue_observations WHERE order_id = $1),
		(SELECT COUNT(*) FROM economic_source_events
		 WHERE account_id = $2 AND source = 'alpaca' AND source_namespace = $3),
		(SELECT COUNT(*) FROM execution_fills WHERE intent_id = $4),
		(SELECT COUNT(*) FROM economic_event_normalizations AS normalization
		 JOIN execution_fills AS fill ON fill.normalization_id = normalization.id
		 WHERE fill.intent_id = $4),
		(SELECT COUNT(*) FROM ledger_transactions AS transaction
		 JOIN execution_fills AS fill ON fill.ledger_transaction_id = transaction.id
		 WHERE fill.intent_id = $4)`, fixture.aggregate.Order.ID, fixture.account.ID,
		fixture.policy.AuthoritativeFillNamespace(), fixture.aggregate.Intent.ID,
	).Scan(&observations, &economics, &fills, &normalizations, &transactions); err != nil {
		t.Fatal(err)
	}
	if observations != observationCount || economics != economicCount || fills != fillCount ||
		normalizations != normalizationCount || transactions != transactionCount {
		t.Fatalf("graph counts = observations:%d economic:%d fills:%d normalizations:%d transactions:%d, want %d/%d/%d/%d/%d",
			observations, economics, fills, normalizations, transactions,
			observationCount, economicCount, fillCount, normalizationCount, transactionCount,
		)
	}
}
