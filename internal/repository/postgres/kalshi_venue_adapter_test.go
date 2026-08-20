package postgres

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

func TestKalshiVenueAdapterPartialFullConcurrentReplayAndRestartConverge(t *testing.T) {
	fixture := newVenueAdapterRepositoryFixture(t, "kalshi-common-convergence")
	policy, err := venue.ReviewedPolicy(venue.ProviderKalshi)
	if err != nil {
		t.Fatal(err)
	}
	externalID := "kalshi-v2-" + fixture.aggregate.Order.ID.String()
	half := fixture.aggregate.Order.Quantity.Div(decimal.NewFromInt(2))
	context := kalshi.CommonLifecycleContext{
		Policy: policy, Aggregate: fixture.aggregate, Account: fixture.base.account,
		Instrument: fixture.base.instrument, VenueContract: fixture.base.contract,
		Route:      kalshi.CommonRouteFacts{Subaccount: 0, ExchangeIndex: 0},
		ReceivedAt: fixture.base.baseTime.Add(20 * time.Second),
	}
	facts := []kalshi.CommonFillFact{
		kalshiPostgresFillFact(t, context, externalID, "kalshi-fill-one", half, fixture.base.baseTime.Add(10*time.Second)),
		kalshiPostgresFillFact(t, context, externalID, "kalshi-fill-two", half, fixture.base.baseTime.Add(11*time.Second)),
	}
	result, err := kalshi.PlanFillResults(context, facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.State != lifecycle.StateFilled || len(result.Steps) != 2 {
		t.Fatalf("planned result = %#v", result)
	}

	const writers = 8
	var ready, wait sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	errorsFound := make(chan error, writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			persisted, persistErr := venue.PersistResult(fixture.ctx, newPostgresVenueResultStore(fixture.pool), fixture.base.account.ID, result)
			if persistErr != nil {
				errorsFound <- persistErr
				return
			}
			if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 2 {
				errorsFound <- fmt.Errorf("replay returned %s/%d", persisted.State, len(persisted.Fills))
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for persistErr := range errorsFound {
		t.Errorf("concurrent Kalshi replay: %v", persistErr)
	}
	if t.Failed() {
		t.FailNow()
	}

	var observations, economics, fills, normalizations, transactions int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT COUNT(*) FROM venue_observations WHERE order_id = $1),
		(SELECT COUNT(*) FROM economic_source_events WHERE account_id = $2 AND source = 'kalshi' AND source_namespace = $3),
		(SELECT COUNT(*) FROM execution_fills WHERE intent_id = $4),
		(SELECT COUNT(*) FROM economic_event_normalizations AS normalization JOIN execution_fills AS fill ON fill.normalization_id = normalization.id WHERE fill.intent_id = $4),
		(SELECT COUNT(*) FROM ledger_transactions AS transaction JOIN execution_fills AS fill ON fill.ledger_transaction_id = transaction.id WHERE fill.intent_id = $4)`,
		fixture.aggregate.Order.ID, fixture.base.account.ID, policy.AuthoritativeFillNamespace(), fixture.aggregate.Intent.ID,
	).Scan(&observations, &economics, &fills, &normalizations, &transactions); err != nil {
		t.Fatal(err)
	}
	if observations != 2 || economics != 2 || fills != 2 || normalizations != 2 || transactions != 2 {
		t.Fatalf("graph counts = %d/%d/%d/%d/%d", observations, economics, fills, normalizations, transactions)
	}
	fresh, err := NewExecutionLifecycleRepo(fixture.pool).GetExecutionLifecycle(fixture.ctx, fixture.base.account.ID, fixture.aggregate.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.State != lifecycle.StateFilled || len(fresh.Fills) != 2 {
		t.Fatalf("fresh replay = %s/%d", fresh.State, len(fresh.Fills))
	}
	context.Aggregate = fresh
	executed := kalshiPostgresOrderFact(t, context, externalID, "executed", fixture.aggregate.Order.Quantity, decimal.Zero)
	executedResult, err := kalshi.PlanOrderResult(context, venue.ObservationOrderSnapshot, executed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := venue.PersistResult(fixture.ctx, newPostgresVenueResultStore(fixture.pool), fixture.base.account.ID, executedResult); err != nil {
		t.Fatalf("persist executed evidence: %v", err)
	}
	if _, err := venue.PersistResult(fixture.ctx, newPostgresVenueResultStore(fixture.pool), fixture.base.account.ID, result); err != nil {
		t.Fatalf("fresh-process retry: %v", err)
	}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT COUNT(*) FROM venue_observations WHERE order_id = $1`, fixture.aggregate.Order.ID).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 3 {
		t.Fatalf("observations after executed evidence = %d", observations)
	}
}

func kalshiPostgresFillFact(t *testing.T, context kalshi.CommonLifecycleContext, externalID, fillID string, count decimal.Decimal, createdAt time.Time) kalshi.CommonFillFact {
	t.Helper()
	zero := 0
	fill := kalshi.CommonFill{
		ID: fillID, TradeID: "trade-" + fillID, OrderID: externalID,
		Ticker: context.VenueContract.ContractID, Side: "yes", Action: "buy", OutcomeSide: "yes", BookSide: "bid",
		CountFP: count.StringFixed(2), YesPriceDollars: "0.42", NoPriceDollars: "0.58", FeeCost: "0.01",
		CreatedTime: createdAt.Format(time.RFC3339Nano), SubaccountNumber: &zero, ExchangeIndex: &zero,
	}
	raw, err := json.Marshal(fill)
	if err != nil {
		t.Fatal(err)
	}
	return kalshi.CommonFillFact{Fill: fill, RawPayload: raw}
}

func kalshiPostgresOrderFact(t *testing.T, context kalshi.CommonLifecycleContext, externalID, status string, filled, remaining decimal.Decimal) *kalshi.CommonOrderFact {
	t.Helper()
	zero := 0
	order := kalshi.CommonOrder{
		ID: externalID, ClientOrderID: context.Aggregate.Order.ClientOrderID,
		Ticker: context.VenueContract.ContractID, Side: "yes", Action: "buy", OutcomeSide: "yes", BookSide: "bid", Type: "limit", Status: status,
		YesPriceDollars: "0.42", NoPriceDollars: "0.58", FillCountFP: filled.StringFixed(2), RemainingCountFP: remaining.StringFixed(2),
		InitialCountFP: context.Aggregate.Order.Quantity.StringFixed(2), CreatedTime: context.ReceivedAt.Add(-time.Second).Format(time.RFC3339Nano),
		LastUpdateTime: context.ReceivedAt.Add(-time.Millisecond).Format(time.RFC3339Nano), SubaccountNumber: &zero, ExchangeIndex: &zero,
	}
	raw, err := json.Marshal(order)
	if err != nil {
		t.Fatal(err)
	}
	return &kalshi.CommonOrderFact{Order: order, RawPayload: raw}
}
