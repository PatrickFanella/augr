package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

// This rehearsal complements the provider-specific concurrency suites by
// proving that every raw-first crash boundary is recoverable through the real
// PostgreSQL repositories with an exact Kalshi economic graph.
func TestVenueAdapterIntegratedKalshiCrashBoundaryRecovery(t *testing.T) {
	for _, failurePoint := range []string{"observation", "economic", "fill"} {
		t.Run(failurePoint, func(t *testing.T) {
			fixture := newVenueAdapterRepositoryFixture(t, "kalshi-crash-"+failurePoint)
			policy, err := venue.ReviewedPolicy(venue.ProviderKalshi)
			if err != nil {
				t.Fatal(err)
			}
			adapterContext := kalshi.CommonLifecycleContext{
				Policy: policy, Aggregate: fixture.aggregate, Account: fixture.base.account,
				Instrument: fixture.base.instrument, VenueContract: fixture.base.contract,
				Route:      kalshi.CommonRouteFacts{Subaccount: 0, ExchangeIndex: 0},
				ReceivedAt: fixture.base.baseTime.Add(20 * time.Second),
			}
			fact := kalshiPostgresFillFact(t, adapterContext,
				"kalshi-v2-"+fixture.aggregate.Order.ID.String(), "crash-fill-"+failurePoint,
				fixture.aggregate.Order.Quantity, fixture.base.baseTime.Add(10*time.Second))
			result, err := kalshi.PlanFillResults(adapterContext, []kalshi.CommonFillFact{fact})
			if err != nil {
				t.Fatal(err)
			}
			injected := &injectedPostgresVenueResultStore{
				postgresVenueResultStore: newPostgresVenueResultStore(fixture.pool),
				failurePoint:             failurePoint, failure: errors.New("injected post-commit response loss"),
			}
			if _, err := venue.PersistResult(fixture.ctx, injected, fixture.base.account.ID, result); err == nil {
				t.Fatal("injected boundary did not interrupt persistence")
			}
			freshStore := newPostgresVenueResultStore(fixture.pool)
			persisted, err := venue.PersistResult(fixture.ctx, freshStore, fixture.base.account.ID, result)
			if err != nil {
				t.Fatalf("fresh-process recovery: %v", err)
			}
			if persisted.State != lifecycle.StateFilled || len(persisted.Fills) != 1 {
				t.Fatalf("recovered aggregate = %s/%d", persisted.State, len(persisted.Fills))
			}

			var observations, economics, fills, normalizations, transactions, fees int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
				(SELECT COUNT(*) FROM venue_observations WHERE order_id = $1),
				(SELECT COUNT(*) FROM economic_source_events WHERE account_id = $2 AND source = 'kalshi' AND source_namespace = $3),
				(SELECT COUNT(*) FROM execution_fills WHERE intent_id = $4),
				(SELECT COUNT(*) FROM economic_event_normalizations AS n JOIN execution_fills AS f ON f.normalization_id = n.id WHERE f.intent_id = $4),
				(SELECT COUNT(*) FROM ledger_transactions AS tx JOIN execution_fills AS f ON f.ledger_transaction_id = tx.id WHERE f.intent_id = $4),
				(SELECT COUNT(*) FROM economic_event_normalizations AS n JOIN execution_fills AS f ON f.normalization_id = n.id WHERE f.intent_id = $4 AND n.cost_amount = $5)`,
				fixture.aggregate.Order.ID, fixture.base.account.ID, policy.AuthoritativeFillNamespace(), fixture.aggregate.Intent.ID,
				decimal.RequireFromString("0.01"),
			).Scan(&observations, &economics, &fills, &normalizations, &transactions, &fees); err != nil {
				t.Fatal(err)
			}
			if observations != 1 || economics != 1 || fills != 1 || normalizations != 1 || transactions != 1 || fees != 1 {
				t.Fatalf("recovered graph = %d/%d/%d/%d/%d fees=%d", observations, economics, fills, normalizations, transactions, fees)
			}
		})
	}
}
