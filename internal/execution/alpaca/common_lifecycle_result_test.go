package alpaca

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

func TestPlanOrderResultMapsEveryReviewedAlpacaStatusExactly(t *testing.T) {
	policy, err := venue.ReviewedPolicy(venue.ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	for _, mapping := range policy.Mappings() {
		if mapping.Namespace != venue.MappingOrderStatus {
			continue
		}
		mapping := mapping
		t.Run(mapping.Value, func(t *testing.T) {
			fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
			fact := fixture.orderFact(t, mapping.Value, "0", "")
			result, planErr := PlanOrderResult(fixture.context, venue.ObservationOrderSnapshot, fact)
			if planErr != nil {
				t.Fatalf("PlanOrderResult() error = %v", planErr)
			}
			if len(result.Steps) != 1 {
				t.Fatalf("steps = %d, want 1", len(result.Steps))
			}
			step := result.Steps[0]
			wantOutcome := mapping.Outcome
			wantState := lifecycle.StateRouted
			var wantKind lifecycle.EventKind
			switch mapping.Outcome {
			case venue.OutcomeAcknowledge:
				wantState = lifecycle.StateWorking
				wantKind = lifecycle.EventOrderWorking
			case venue.OutcomeCancelled:
				wantState = lifecycle.StateCancelled
				wantKind = lifecycle.EventOrderCancelled
			case venue.OutcomeExpired:
				wantState = lifecycle.StateExpired
				wantKind = lifecycle.EventOrderExpired
			case venue.OutcomeRejected:
				wantState = lifecycle.StateRejected
				wantKind = lifecycle.EventOrderRejected
			case venue.OutcomeContradiction:
				wantState = lifecycle.StateFailedReconciliation
				wantKind = lifecycle.EventContradictoryVenueState
			case venue.OutcomeFillNotice:
				// Status cumulative/average fields are notices, never economics.
				if step.EconomicSourceEvent != nil || step.Transition != nil {
					t.Fatalf("fill notice gained economics: %#v", step)
				}
			}
			if step.Observation.MappedOutcome != wantOutcome || result.Aggregate.State != wantState {
				t.Fatalf("mapping = outcome:%s state:%s, want %s/%s", step.Observation.MappedOutcome, result.Aggregate.State, wantOutcome, wantState)
			}
			if wantKind != "" && (step.Transition == nil || step.Transition.Event.Kind != wantKind) {
				t.Fatalf("transition = %#v, want %s", step.Transition, wantKind)
			}
		})
	}
}

func TestPlanOrderResultTurnsRepeatedAcknowledgementIntoDurableNoChange(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
	first, err := PlanOrderResult(fixture.context, venue.ObservationSubmitResponse, fixture.orderFact(t, "new", "0", ""))
	if err != nil {
		t.Fatal(err)
	}
	fixture.context.Aggregate = first.Aggregate
	repeated, err := PlanOrderResult(fixture.context, venue.ObservationOrderSnapshot, fixture.orderFact(t, "accepted", "0", ""))
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Steps[0].Observation.MappedOutcome != venue.OutcomeNoChange || repeated.Steps[0].Transition != nil ||
		repeated.Aggregate.State != lifecycle.StateWorking {
		t.Fatalf("repeated acknowledgement = %#v", repeated)
	}
}

func TestPlanOrderResultFailsUnknownMalformedAndContradictoryFactsClosed(t *testing.T) {
	for name, fact := range map[string]func(alpacaLifecycleFixture) *CommonOrderFact{
		"unknown status": func(fixture alpacaLifecycleFixture) *CommonOrderFact {
			return fixture.orderFact(t, "provider_added_this_yesterday", "0", "")
		},
		"wrong client": func(fixture alpacaLifecycleFixture) *CommonOrderFact {
			value := fixture.orderFact(t, "new", "0", "")
			value.Order.ClientOrderID = uuid.NewString()
			refreshOrderFactEvidence(t, value)
			return value
		},
		"wrong external id after binding": func(fixture alpacaLifecycleFixture) *CommonOrderFact {
			value := fixture.orderFact(t, "new", "0", "")
			value.Order.ID = "other-external-id"
			refreshOrderFactEvidence(t, value)
			return value
		},
		"replacement link": func(fixture alpacaLifecycleFixture) *CommonOrderFact {
			return fixture.orderFact(t, "new", "0", "replacement-1")
		},
		"malformed object": func(_ alpacaLifecycleFixture) *CommonOrderFact {
			return &CommonOrderFact{RawPayload: json.RawMessage(`{}`)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
			if name == "wrong external id after binding" {
				ack, err := PlanOrderResult(fixture.context, venue.ObservationSubmitResponse, fixture.orderFact(t, "new", "0", ""))
				if err != nil {
					t.Fatal(err)
				}
				fixture.context.Aggregate = ack.Aggregate
			}
			result, err := PlanOrderResult(fixture.context, venue.ObservationOrderSnapshot, fact(fixture))
			if err != nil {
				t.Fatalf("PlanOrderResult() should produce durable failure evidence: %v", err)
			}
			step := result.Steps[0]
			if result.Aggregate.State != lifecycle.StateFailedReconciliation || step.Transition == nil ||
				step.Transition.Event.Kind != lifecycle.EventContradictoryVenueState &&
					step.Transition.Event.Kind != lifecycle.EventUnknownVenueState || step.EconomicSourceEvent != nil {
				t.Fatalf("failure result = %#v", result)
			}
			if name == "unknown status" && step.Observation.MappedOutcome != venue.OutcomeUnknownState {
				t.Fatalf("unknown outcome = %s", step.Observation.MappedOutcome)
			}
			store := newAlpacaResultStore(fixture.context.Aggregate)
			persisted, err := venue.PersistResult(context.Background(), store, fixture.context.Account.ID, result)
			if err != nil {
				t.Fatalf("contradiction evidence was not journalable: %v", err)
			}
			if persisted.State != lifecycle.StateFailedReconciliation || len(store.observations) != 1 {
				t.Fatalf("persisted failure = state:%s observations:%d", persisted.State, len(store.observations))
			}
		})
	}
}

func TestPlanTradeUpdateJournalsFillNoticesWithoutEconomics(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
	ack, err := PlanOrderResult(fixture.context, venue.ObservationSubmitResponse, fixture.orderFact(t, "new", "0", ""))
	if err != nil {
		t.Fatal(err)
	}
	fixture.context.Aggregate = ack.Aggregate
	raw := fixture.tradeUpdateJSON("partial_fill", "0.4")
	update, err := ParseTradeUpdate(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := PlanTradeUpdateResult(fixture.context, update)
	if err != nil {
		t.Fatal(err)
	}
	step := result.Steps[0]
	if step.Observation.Kind != venue.ObservationTradeUpdate || step.Observation.MappedOutcome != venue.OutcomeFillNotice ||
		step.EconomicSourceEvent != nil || step.Transition != nil || result.Aggregate.State != lifecycle.StateWorking {
		t.Fatalf("trade update result = %#v", result)
	}
	if string(step.Observation.RawPayload) != string(raw) {
		t.Fatalf("stream evidence changed: got %s want %s", step.Observation.RawPayload, raw)
	}
}

func TestPlanFillActivitiesBuildsOnlyAuthoritativeExactEconomicGraphs(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
	facts := []FillActivityFact{
		fixture.fillFact(t, "fill-1", "0.4", "100.01", "0.4", "0.6", "0.02"),
		fixture.fillFact(t, "fill-2", "0.6", "100.02", "1", "0", ""),
	}
	result, err := PlanFillActivityResult(fixture.context, facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 || result.Aggregate.State != lifecycle.StateFilled || len(result.Aggregate.Fills) != 2 {
		t.Fatalf("fill result = steps:%d state:%s fills:%d", len(result.Steps), result.Aggregate.State, len(result.Aggregate.Fills))
	}
	for index, step := range result.Steps {
		if step.Observation.MappedOutcome != venue.OutcomeFill || step.EconomicSourceEvent == nil ||
			step.Transition == nil || step.Transition.Normalization == nil || step.Transition.Fill == nil {
			t.Fatalf("fill step %d lacks raw graph: %#v", index, step)
		}
		if step.Observation.SourceNamespace != fixture.context.Policy.AuthoritativeFillNamespace() ||
			step.Observation.SourceEventID != facts[index].Activity.ID ||
			step.EconomicSourceEvent.ID != step.Transition.Normalization.SourceEvent.ID ||
			step.Transition.Fill.SourceEventID != facts[index].Activity.ID {
			t.Fatalf("fill step %d identity = %#v", index, step)
		}
		wantKind := lifecycle.EventFillRecorded
		if index == 0 {
			wantKind = lifecycle.EventFillAcknowledged
		}
		if step.Transition.Event.Kind != wantKind {
			t.Fatalf("fill step %d kind = %s, want %s", index, step.Transition.Event.Kind, wantKind)
		}
	}
	cost := result.Steps[0].Transition.Normalization.CostAmount
	if cost == nil || !cost.Equal(decimal.RequireFromString("0.02")) ||
		result.Steps[0].Transition.Normalization.CostCurrency != "USD" {
		t.Fatalf("exact fill commission = %v %s", cost, result.Steps[0].Transition.Normalization.CostCurrency)
	}
	if result.Steps[1].Transition.Normalization.CostAmount != nil {
		t.Fatal("absent commission created a cost")
	}

	replay, err := PlanFillActivityResult(fixture.context, facts)
	if err != nil {
		t.Fatal(err)
	}
	for index := range result.Steps {
		if replay.Steps[index].Observation.ID != result.Steps[index].Observation.ID ||
			replay.Steps[index].EconomicSourceEvent.ID != result.Steps[index].EconomicSourceEvent.ID ||
			replay.Steps[index].Transition.Fill.ID != result.Steps[index].Transition.Fill.ID {
			t.Fatalf("replay identities diverged at step %d", index)
		}
	}
}

func TestPlanFillActivitiesKeepsDistinctProviderIDsDistinct(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(2))
	first := fixture.fillFact(t, "equal-fill-1", "1", "99.99", "1", "1", "")
	second := fixture.fillFact(t, "equal-fill-2", "1", "99.99", "2", "0", "")
	result, err := PlanFillActivityResult(fixture.context, []FillActivityFact{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.Fills[0].ID == result.Aggregate.Fills[1].ID ||
		result.Steps[0].EconomicSourceEvent.ID == result.Steps[1].EconomicSourceEvent.ID {
		t.Fatal("distinct provider activity IDs collapsed")
	}
}

func TestPlanFillActivitiesFailsContradictionsBeforeEconomics(t *testing.T) {
	for name, mutate := range map[string]func(*FillActivityFact){
		"wrong order":           func(fact *FillActivityFact) { fact.Activity.OrderID = "other-order" },
		"wrong client":          func(fact *FillActivityFact) { fact.Activity.ClientOrderID = uuid.NewString() },
		"wrong symbol":          func(fact *FillActivityFact) { fact.Activity.Symbol = "MSFT" },
		"wrong side":            func(fact *FillActivityFact) { fact.Activity.Side = "sell" },
		"impossible cumulative": func(fact *FillActivityFact) { fact.Activity.CumulativeQuantity = "0.9" },
		"impossible leaves":     func(fact *FillActivityFact) { fact.Activity.LeavesQuantity = "0.7" },
		"invalid quantity":      func(fact *FillActivityFact) { fact.Activity.Quantity = "NaN" },
		"negative commission":   func(fact *FillActivityFact) { fact.Activity.Commission = "-0.01" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
			ack, err := PlanOrderResult(
				fixture.context, venue.ObservationSubmitResponse, fixture.orderFact(t, "new", "0", ""),
			)
			if err != nil {
				t.Fatal(err)
			}
			fixture.context.Aggregate = ack.Aggregate
			fact := fixture.fillFact(t, "fill-bad", "0.4", "100", "0.4", "0.6", "")
			mutate(&fact)
			refreshFillFactEvidence(t, &fact)
			result, err := PlanFillActivityResult(fixture.context, []FillActivityFact{fact})
			if err != nil {
				t.Fatalf("contradiction should remain journalable: %v", err)
			}
			step := result.Steps[0]
			if step.Observation.MappedOutcome != venue.OutcomeContradiction || step.EconomicSourceEvent != nil ||
				step.Transition == nil || step.Transition.Event.Kind != lifecycle.EventContradictoryVenueState ||
				result.Aggregate.State != lifecycle.StateFailedReconciliation {
				t.Fatalf("contradictory fill result = %#v", result)
			}
		})
	}
}

func TestPlanFillCorrectionAndBustReferenceExactPriorFillWithoutEconomics(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
	originalFact := fixture.fillFact(t, "fill-original", "1", "100", "1", "0", "")
	original, err := PlanFillActivityResult(fixture.context, []FillActivityFact{originalFact})
	if err != nil {
		t.Fatal(err)
	}

	for _, activityType := range []string{"trade_correct", "trade_bust"} {
		t.Run(activityType, func(t *testing.T) {
			context := fixture.context
			context.Aggregate = original.Aggregate
			fact := fixture.revisionFact(t, activityType, "revision-"+activityType, "fill-original")
			result, err := PlanFillActivityResult(context, []FillActivityFact{fact})
			if err != nil {
				t.Fatal(err)
			}
			step := result.Steps[0]
			wantOutcome := venue.OutcomeCorrection
			wantKind := lifecycle.EventFillCorrectionObserved
			if activityType == "trade_bust" {
				wantOutcome = venue.OutcomeBust
				wantKind = lifecycle.EventFillBustObserved
			}
			if step.Observation.MappedOutcome != wantOutcome || step.EconomicSourceEvent != nil ||
				step.Transition == nil || step.Transition.Event.Kind != wantKind ||
				step.Transition.Event.OriginalFillID == nil ||
				*step.Transition.Event.OriginalFillID != original.Aggregate.Fills[0].ID ||
				len(result.Aggregate.Fills) != 1 || result.Aggregate.State != lifecycle.StateFailedReconciliation {
				t.Fatalf("revision result = %#v", result)
			}
		})
	}

	t.Run("unknown original", func(t *testing.T) {
		context := fixture.context
		context.Aggregate = original.Aggregate
		fact := fixture.revisionFact(t, "trade_correct", "unknown-revision", "missing-fill")
		result, err := PlanFillActivityResult(context, []FillActivityFact{fact})
		if err != nil {
			t.Fatal(err)
		}
		if result.Steps[0].Observation.MappedOutcome != venue.OutcomeContradiction ||
			result.Steps[0].EconomicSourceEvent != nil || result.Aggregate.State != lifecycle.StateFailedReconciliation {
			t.Fatalf("unknown original result = %#v", result)
		}
	})
}

func TestPlannedFillResultSatisfiesRawFirstVenuePersistenceContract(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
	result, err := PlanFillActivityResult(fixture.context, []FillActivityFact{
		fixture.fillFact(t, "persist-fill-1", "0.4", "100.01", "0.4", "0.6", "0.01"),
		fixture.fillFact(t, "persist-fill-2", "0.6", "100.02", "1", "0", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	store := newAlpacaResultStore(fixture.context.Aggregate)
	persisted, err := venue.PersistResult(context.Background(), store, fixture.context.Account.ID, result)
	if err != nil {
		t.Fatalf("PersistResult() error = %v", err)
	}
	if persisted.State != lifecycle.StateFilled || fmt.Sprint(store.calls) !=
		"[observation economic fill observation economic fill]" {
		t.Fatalf("persistence = state:%s calls:%v", persisted.State, store.calls)
	}
}

func TestPersistCancellationThenDeleteNeverClaimsProviderConfirmation(t *testing.T) {
	fixture := newAlpacaLifecycleFixture(t, decimal.NewFromInt(1))
	ack, err := PlanOrderResult(
		fixture.context, venue.ObservationSubmitResponse, fixture.orderFact(t, "new", "0", ""),
	)
	if err != nil {
		t.Fatal(err)
	}
	store := newAlpacaCancelStore(ack.Aggregate)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !store.hasCancelCommand() {
			t.Error("provider DELETE arrived before the local cancellation command committed")
		}
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/orders/alpaca-order-1" {
			t.Errorf("cancel request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	persisted, err := PersistCancellationThenDelete(
		context.Background(), store, fixture.context.Account.ID, ack.Aggregate,
		fixture.now.Add(5*time.Second), newLoopbackCommonLifecycleClient(t, server.URL),
	)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != lifecycle.StateWorking || !store.hasCancelCommand() {
		t.Fatalf("cancel result = state:%s events:%d", persisted.State, len(persisted.Events))
	}
	for index := range persisted.Events {
		if persisted.Events[index].Kind == lifecycle.EventOrderCancelled {
			t.Fatal("DELETE response was misrepresented as provider cancellation")
		}
	}
}

type alpacaLifecycleFixture struct {
	context CommonLifecycleContext
	now     time.Time
}

func newAlpacaLifecycleFixture(t *testing.T, quantity decimal.Decimal) alpacaLifecycleFixture {
	t.Helper()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "Alpaca lifecycle", Environment: domain.AccountEnvironmentPaperScored, Venue: "alpaca",
		BaseCurrency: "USD", StorageNamespace: "paper_scored/alpaca-lifecycle", StartingCapital: decimal.NewFromInt(10000),
		BuyingPowerMultiplier: decimal.NewFromInt(1), MarginProfile: domain.MarginProfileCash,
		CreatedBy: "test", CreationMetadata: json.RawMessage(`{}`), CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := venue.ReviewedPolicy(venue.ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	order, primary, contract := commonLifecycleOrderFixture(venue.Capability{
		AssetClass: instrument.AssetClassEquity, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceDay,
	})
	order.AccountID = account.ID
	order.Quantity = quantity
	order.IntentID = uuid.New()
	order.InstrumentID = primary.ID
	order.VenueContractID = contract.ID
	limit := decimal.RequireFromString("100")
	order.LimitPrice = &limit
	allocated := quantity
	aggregate := &lifecycle.Aggregate{
		Intent: lifecycle.Intent{
			ID: order.IntentID, AccountID: account.ID, Environment: account.Environment, InstrumentID: primary.ID,
			OriginType: ledger.ExecutionOriginStrategyVersion, OriginID: "strategy-version-1", StrategyVersionID: "strategy-version-1",
		},
		State: lifecycle.StateRouted, AllocatedQuantity: &allocated, Order: order,
	}
	return alpacaLifecycleFixture{
		context: CommonLifecycleContext{
			Policy: policy, Aggregate: aggregate, Account: account, Instrument: primary,
			VenueContract: contract, ReceivedAt: now.Add(10 * time.Second),
		},
		now: now,
	}
}

func (fixture alpacaLifecycleFixture) orderFact(
	t *testing.T,
	status, filledQuantity, replacedBy string,
) *CommonOrderFact {
	t.Helper()
	order := fixture.context.Aggregate.Order
	raw := json.RawMessage(fmt.Sprintf(
		`{"id":"alpaca-order-1","client_order_id":%q,"symbol":%q,"side":%q,"type":%q,"time_in_force":%q,"qty":%q,"filled_qty":%q,"filled_avg_price":"999.99","status":%q,"updated_at":"%s","replaced_by":%q}`,
		order.ClientOrderID, fixture.context.VenueContract.ContractID, order.Side, order.OrderType, order.TimeInForce,
		order.Quantity.String(), filledQuantity, status, fixture.now.Add(time.Second).Format(time.RFC3339Nano), replacedBy,
	))
	fact, err := decodeCommonOrder(raw, "test fixture")
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func (fixture alpacaLifecycleFixture) tradeUpdateJSON(event, filledQuantity string) json.RawMessage {
	order := fixture.context.Aggregate.Order
	return json.RawMessage(fmt.Sprintf(
		`{"event":%q,"timestamp":"%s","order":{"id":"alpaca-order-1","client_order_id":%q,"symbol":%q,"side":%q,"type":%q,"time_in_force":%q,"qty":%q,"filled_qty":%q,"filled_avg_price":"999.99","status":"partially_filled","updated_at":"%s"}}`,
		event, fixture.now.Add(2*time.Second).Format(time.RFC3339Nano), order.ClientOrderID,
		fixture.context.VenueContract.ContractID, order.Side, order.OrderType, order.TimeInForce,
		order.Quantity.String(), filledQuantity, fixture.now.Add(2*time.Second).Format(time.RFC3339Nano),
	))
}

func (fixture alpacaLifecycleFixture) fillFact(
	t *testing.T,
	id, quantity, price, cumulative, leaves, commission string,
) FillActivityFact {
	t.Helper()
	order := fixture.context.Aggregate.Order
	raw := json.RawMessage(fmt.Sprintf(
		`{"id":%q,"activity_type":"FILL","order_id":"alpaca-order-1","client_order_id":%q,"qty":%q,"price":%q,"side":%q,"symbol":%q,"transaction_time":"%s","cum_qty":%q,"leaves_qty":%q,"commission":%q}`,
		id, order.ClientOrderID, quantity, price, order.Side, fixture.context.VenueContract.ContractID,
		fixture.now.Add(3*time.Second).Format(time.RFC3339Nano), cumulative, leaves, commission,
	))
	page, err := decodeFillActivityPage(append(append(json.RawMessage{'['}, raw...), ']'))
	if err != nil {
		t.Fatal(err)
	}
	return page[0]
}

func (fixture alpacaLifecycleFixture) revisionFact(
	t *testing.T,
	activityType, id, originalActivityID string,
) FillActivityFact {
	t.Helper()
	order := fixture.context.Aggregate.Order
	raw := json.RawMessage(fmt.Sprintf(
		`{"id":%q,"activity_type":%q,"order_id":"alpaca-order-1","client_order_id":%q,"qty":"1","price":"100","side":%q,"symbol":%q,"transaction_time":"%s","cum_qty":"1","leaves_qty":"0","original_activity_id":%q}`,
		id, activityType, order.ClientOrderID, order.Side, fixture.context.VenueContract.ContractID,
		fixture.now.Add(4*time.Second).Format(time.RFC3339Nano), originalActivityID,
	))
	page, err := decodeFillActivityPage(append(append(json.RawMessage{'['}, raw...), ']'))
	if err != nil {
		t.Fatal(err)
	}
	return page[0]
}

func refreshOrderFactEvidence(t *testing.T, fact *CommonOrderFact) {
	t.Helper()
	raw, err := json.Marshal(fact.Order)
	if err != nil {
		t.Fatal(err)
	}
	fact.RawPayload = raw
}

func refreshFillFactEvidence(t *testing.T, fact *FillActivityFact) {
	t.Helper()
	raw, err := json.Marshal(fact.Activity)
	if err != nil {
		t.Fatal(err)
	}
	fact.RawPayload = raw
}

type alpacaResultStore struct {
	current      *lifecycle.Aggregate
	calls        []string
	observations map[uuid.UUID]*venue.Observation
	economic     map[uuid.UUID]*ledger.EconomicSourceEvent
}

type alpacaCancelStore struct {
	current *lifecycle.Aggregate
}

func newAlpacaCancelStore(current *lifecycle.Aggregate) *alpacaCancelStore {
	return &alpacaCancelStore{current: current}
}

func (store *alpacaCancelStore) ApplyExecutionTransition(
	_ context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	if accountID != store.current.Intent.AccountID {
		return nil, fmt.Errorf("account mismatch")
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

func (store *alpacaCancelStore) hasCancelCommand() bool {
	for index := range store.current.Events {
		if store.current.Events[index].Kind == lifecycle.EventCancelRequested {
			return true
		}
	}
	return false
}

func newAlpacaResultStore(current *lifecycle.Aggregate) *alpacaResultStore {
	return &alpacaResultStore{
		current: current, observations: make(map[uuid.UUID]*venue.Observation),
		economic: make(map[uuid.UUID]*ledger.EconomicSourceEvent),
	}
}

func (store *alpacaResultStore) RecordVenueObservation(
	_ context.Context,
	observation *venue.Observation,
) (*venue.Observation, error) {
	store.calls = append(store.calls, "observation")
	if existing := store.observations[observation.ID]; existing != nil {
		return existing, nil
	}
	store.observations[observation.ID] = observation
	return observation, nil
}

func (store *alpacaResultStore) RecordEconomicSourceEvent(
	_ context.Context,
	event *ledger.EconomicSourceEvent,
) (*ledger.EconomicSourceEvent, error) {
	store.calls = append(store.calls, "economic")
	if existing := store.economic[event.ID]; existing != nil {
		return existing, nil
	}
	store.economic[event.ID] = event
	return event, nil
}

func (store *alpacaResultStore) ApplyExecutionFill(
	_ context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	store.calls = append(store.calls, "fill")
	if accountID != store.current.Intent.AccountID {
		return nil, fmt.Errorf("account mismatch")
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

func (store *alpacaResultStore) ApplyExecutionTransition(
	ctx context.Context,
	accountID uuid.UUID,
	transition *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	store.calls = append(store.calls, "transition")
	return store.ApplyExecutionFill(ctx, accountID, transition)
}
