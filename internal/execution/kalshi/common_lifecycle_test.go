package kalshi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
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

func TestMapKalshiCommonOrderRequestProjectsExactV2Facts(t *testing.T) {
	for _, outcome := range []string{"yes", "no"} {
		for _, side := range []lifecycle.Side{lifecycle.SideBuy, lifecycle.SideSell} {
			for _, tif := range []lifecycle.TimeInForce{lifecycle.TimeInForceGTC, lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK} {
				name := outcome + "/" + string(side) + "/" + string(tif)
				t.Run(name, func(t *testing.T) {
					policy, order, primary, contract := kalshiCommonFixture(outcome, side, tif)
					request, err := MapCommonOrderRequest(policy, primary, contract, order, CommonRouteFacts{Subaccount: 7, ExchangeIndex: 0})
					if err != nil {
						t.Fatal(err)
					}
					wantBook := map[string]string{"yes/buy": "bid", "yes/sell": "ask", "no/buy": "ask", "no/sell": "bid"}[outcome+"/"+string(side)]
					wantPrice := "0.37"
					if outcome == "no" {
						wantPrice = "0.63"
					}
					wantTIF := map[lifecycle.TimeInForce]string{lifecycle.TimeInForceGTC: "good_till_canceled", lifecycle.TimeInForceIOC: "immediate_or_cancel", lifecycle.TimeInForceFOK: "fill_or_kill"}[tif]
					if request.Ticker != "KX-TEST" || request.ClientOrderID != order.ID.String() || request.Side != wantBook ||
						request.Count != "12.50" || request.Price != wantPrice || request.TimeInForce != wantTIF ||
						request.Subaccount != 7 || request.ExchangeIndex != 0 || request.Outcome != outcome || request.Action != string(side) {
						t.Fatalf("request = %#v", request)
					}
					encoded, err := json.Marshal(request)
					if err != nil {
						t.Fatal(err)
					}
					for _, forbidden := range []string{"outcome", "action", "yes_price", "no_price", "expiration_time", "post_only", "reduce_only"} {
						var object map[string]any
						if err := json.Unmarshal(encoded, &object); err != nil {
							t.Fatal(err)
						}
						if _, exists := object[forbidden]; exists {
							t.Fatalf("wire request contains internal/unsupported field %q: %s", forbidden, encoded)
						}
					}
				})
			}
		}
	}
}

func TestMapKalshiCommonOrderRequestRejectsBeforeTransport(t *testing.T) {
	policy, order, primary, contract := kalshiCommonFixture("yes", lifecycle.SideBuy, lifecycle.TimeInForceGTC)
	tests := map[string]func(*lifecycle.Order, *instrument.Instrument, *instrument.VenueContract, *CommonRouteFacts){
		"day": func(o *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, _ *CommonRouteFacts) {
			o.TimeInForce = lifecycle.TimeInForceDay
		},
		"gtd": func(o *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, _ *CommonRouteFacts) {
			o.TimeInForce = lifecycle.TimeInForceGTD
		},
		"market": func(o *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, _ *CommonRouteFacts) {
			o.OrderType = lifecycle.OrderMarket
		},
		"missing outcome": func(_ *lifecycle.Order, _ *instrument.Instrument, c *instrument.VenueContract, _ *CommonRouteFacts) {
			c.Metadata = json.RawMessage(`{}`)
		},
		"uppercase outcome": func(_ *lifecycle.Order, _ *instrument.Instrument, c *instrument.VenueContract, _ *CommonRouteFacts) {
			c.Metadata = json.RawMessage(`{"kalshi_v2":{"outcome":"YES"}}`)
		},
		"extra metadata": func(_ *lifecycle.Order, _ *instrument.Instrument, c *instrument.VenueContract, _ *CommonRouteFacts) {
			c.Metadata = json.RawMessage(`{"kalshi_v2":{"outcome":"yes","extra":true}}`)
		},
		"contract mismatch": func(o *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, _ *CommonRouteFacts) {
			o.VenueContractID = uuid.New()
		},
		"off lot": func(o *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, _ *CommonRouteFacts) {
			o.Quantity = decimal.RequireFromString("12.501")
		},
		"off tick": func(o *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, _ *CommonRouteFacts) {
			p := decimal.RequireFromString("0.371")
			o.LimitPrice = &p
		},
		"negative subaccount": func(_ *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, r *CommonRouteFacts) {
			r.Subaccount = -1
		},
		"unsupported exchange index": func(_ *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract, r *CommonRouteFacts) {
			r.ExchangeIndex = 1
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			o, p, c := *order, *primary, *contract
			r := CommonRouteFacts{Subaccount: 0, ExchangeIndex: 0}
			mutate(&o, &p, &c, &r)
			if _, err := MapCommonOrderRequest(policy, &p, &c, &o, r); err == nil {
				t.Fatal("invalid request mapped")
			}
		})
	}
}

func TestKalshiCommonLifecycleClientUsesV2EndpointsAndPaginatesRecovery(t *testing.T) {
	transport := &scriptedSignedClient{}
	transport.post = []scriptedResponse{{body: []byte(`{"order":{"order_id":"external-1","client_order_id":"client-1","ticker":"KX-TEST","side":"bid","action":"buy","outcome_side":"yes","book_side":"bid","type":"limit","status":"resting","yes_price_dollars":"0.37","no_price_dollars":"0.63","fill_count_fp":"0.00","remaining_count_fp":"12.50","initial_count_fp":"12.50","subaccount_number":7,"exchange_index":0}}`)}}
	transport.get = []scriptedResponse{
		{body: []byte(`{"orders":[{"order_id":"other","client_order_id":"other"}],"cursor":"next"}`)},
		{body: []byte(`{"orders":[],"cursor":""}`)},
		{body: []byte(`{"orders":[{"order_id":"external-1","client_order_id":"client-1","ticker":"KX-TEST","side":"bid","action":"buy","outcome_side":"yes","book_side":"bid","type":"limit","status":"resting","yes_price_dollars":"0.37","no_price_dollars":"0.63","fill_count_fp":"0.00","remaining_count_fp":"12.50","initial_count_fp":"12.50","subaccount_number":7,"exchange_index":0}],"cursor":""}`)},
		{body: []byte(`{"fills":[{"fill_id":"fill-1","trade_id":"trade-1","order_id":"external-1","ticker":"KX-TEST","side":"yes","action":"buy","outcome_side":"yes","book_side":"bid","count_fp":"5.00","yes_price_dollars":"0.37","no_price_dollars":"0.63","fee_cost":"0.08","created_time":"2026-08-20T12:00:01Z","subaccount_number":7,"exchange_index":0}],"cursor":"fills-next"}`)},
		{body: []byte(`{"fills":[],"cursor":""}`)},
		{body: []byte(`{"fills":[{"fill_id":"fill-2","trade_id":"trade-2","order_id":"external-1","ticker":"KX-TEST","side":"yes","action":"buy","outcome_side":"yes","book_side":"bid","count_fp":"7.50","yes_price_dollars":"0.37","no_price_dollars":"0.63","fee_cost":"0.12","created_time":"2026-08-20T12:00:02Z","subaccount_number":7,"exchange_index":0}],"cursor":""}`)},
	}
	client, err := NewCommonLifecycleClient(transport)
	if err != nil {
		t.Fatal(err)
	}
	request := CommonOrderRequest{Ticker: "KX-TEST", ClientOrderID: "client-1", Side: "bid", Count: "12.50", Price: "0.37", TimeInForce: "good_till_canceled", Subaccount: 7, ExchangeIndex: 0, Outcome: "yes", Action: "buy"}
	if _, err := client.Submit(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	fact, err := client.FindByClientOrderID(context.Background(), "client-1", 7)
	if err != nil || fact.Order.ID != "external-1" {
		t.Fatalf("find = %#v, %v", fact, err)
	}
	fills, err := client.ListFills(context.Background(), "external-1", "KX-TEST", 7)
	if err != nil || len(fills) != 2 || fills[0].Fill.ID != "fill-1" || fills[1].Fill.ID != "fill-2" {
		t.Fatalf("fills = %#v, %v", fills, err)
	}
	if err := client.Cancel(context.Background(), "external-1", 7, 0); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /portfolio/events/orders", "GET /portfolio/orders?limit=1000&subaccount=7", "GET /portfolio/orders?cursor=next&limit=1000&subaccount=7",
		"GET /portfolio/orders/historical?limit=1000&subaccount=7", "GET /portfolio/fills?cursor=&limit=1000&order_id=external-1&subaccount=7&ticker=KX-TEST",
		"GET /portfolio/fills?cursor=fills-next&limit=1000&order_id=external-1&subaccount=7&ticker=KX-TEST", "GET /portfolio/fills/historical?cursor=&limit=1000&order_id=external-1&subaccount=7&ticker=KX-TEST",
		"DELETE /portfolio/events/orders/external-1?exchange_index=0&subaccount=7",
	}
	if !reflect.DeepEqual(transport.calls, want) {
		t.Fatalf("calls = %#v\nwant %#v", transport.calls, want)
	}
}

func TestKalshiCommonLifecycleClientFailsClosedAndRecoversAmbiguousSubmit(t *testing.T) {
	request := CommonOrderRequest{Ticker: "KX-TEST", ClientOrderID: "client-1", Side: "bid", Count: "12.50", Price: "0.37", TimeInForce: "good_till_canceled", Subaccount: 7, ExchangeIndex: 0, Outcome: "yes", Action: "buy"}
	recoveredBody := []byte(`{"orders":[{"order_id":"external-1","client_order_id":"client-1","ticker":"KX-TEST","side":"yes","action":"buy","outcome_side":"yes","book_side":"bid","type":"limit","status":"resting","yes_price_dollars":"0.37","no_price_dollars":"0.63","fill_count_fp":"0.00","remaining_count_fp":"12.50","initial_count_fp":"12.50","subaccount_number":7,"exchange_index":0}],"cursor":""}`)
	t.Run("ambiguous submit", func(t *testing.T) {
		transport := &scriptedSignedClient{post: []scriptedResponse{{err: errors.New("connection reset after write")}}, get: []scriptedResponse{{body: recoveredBody}}}
		client, _ := NewCommonLifecycleClient(transport)
		fact, err := client.SubmitOrLookup(context.Background(), request)
		if err != nil || fact.Order.ID != "external-1" || len(transport.calls) != 2 {
			t.Fatalf("recovery = %#v, %v, calls=%v", fact, err, transport.calls)
		}
	})
	t.Run("rate limit propagates", func(t *testing.T) {
		transport := &scriptedSignedClient{post: []scriptedResponse{{err: errors.New("kalshi POST rate limited: status=429")}}}
		client, _ := NewCommonLifecycleClient(transport)
		if _, err := client.SubmitOrLookup(context.Background(), request); err == nil || len(transport.calls) != 1 {
			t.Fatalf("error=%v calls=%v", err, transport.calls)
		}
	})
	t.Run("context cancellation propagates", func(t *testing.T) {
		transport := &scriptedSignedClient{post: []scriptedResponse{{err: context.Canceled}}}
		client, _ := NewCommonLifecycleClient(transport)
		if _, err := client.SubmitOrLookup(context.Background(), request); !errors.Is(err, context.Canceled) || len(transport.calls) != 1 {
			t.Fatalf("error=%v calls=%v", err, transport.calls)
		}
	})
	t.Run("malformed JSON", func(t *testing.T) {
		transport := &scriptedSignedClient{post: []scriptedResponse{{body: []byte(`{"order":`)}}}
		client, _ := NewCommonLifecycleClient(transport)
		if _, err := client.Submit(context.Background(), request); err == nil {
			t.Fatal("malformed response accepted")
		}
	})
	t.Run("multiple exact matches", func(t *testing.T) {
		body := []byte(`{"orders":[{"order_id":"one","client_order_id":"client-1"},{"order_id":"two","client_order_id":"client-1"}],"cursor":""}`)
		transport := &scriptedSignedClient{get: []scriptedResponse{{body: body}}}
		client, _ := NewCommonLifecycleClient(transport)
		if _, err := client.FindByClientOrderID(context.Background(), "client-1", 7); err == nil {
			t.Fatal("multiple client matches accepted")
		}
	})
	t.Run("repeated cursor", func(t *testing.T) {
		page := scriptedResponse{body: []byte(`{"orders":[],"cursor":"same"}`)}
		transport := &scriptedSignedClient{get: []scriptedResponse{page, page}}
		client, _ := NewCommonLifecycleClient(transport)
		if _, err := client.FindByClientOrderID(context.Background(), "client-1", 7); err == nil {
			t.Fatal("repeated cursor accepted")
		}
	})
}

func TestPlanKalshiFillResultsCreateExactEconomicsAndConverge(t *testing.T) {
	fixture := newKalshiResultFixture(t, "no", "12.50")
	facts := []CommonFillFact{
		fixture.fillFact(t, "fill-1", "5.00", "0.63", "0.37", "0.08", 1),
		fixture.fillFact(t, "fill-2", "7.50", "0.62", "0.38", "0.12", 2),
	}
	result, err := PlanFillResults(fixture.context, facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.State != lifecycle.StateFilled || len(result.Steps) != 2 || len(result.Aggregate.Fills) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if !result.Aggregate.Fills[0].Price.Equal(decimal.RequireFromString("0.63")) {
		t.Fatalf("NO economic price = %s", result.Aggregate.Fills[0].Price)
	}
	for index, step := range result.Steps {
		if step.Observation.SourceEventID != facts[index].Fill.ID || step.Observation.CanonicalOutcome != "no" ||
			step.Observation.ProviderBookSide != "ask" || step.Observation.ProviderAction != "buy" ||
			step.EconomicSourceEvent == nil || step.Transition == nil || step.Transition.Fill == nil || step.Transition.Normalization == nil {
			t.Fatalf("step %d = %#v", index, step)
		}
	}
}

func TestPlanKalshiFillResultFailsClosedOnContradictionWithoutEconomics(t *testing.T) {
	fixture := newKalshiResultFixture(t, "yes", "12.50")
	fact := fixture.fillFact(t, "fill-bad", "5.00", "0.37", "0.63", "0.08", 1)
	fact.Fill.Ticker = "OTHER"
	result, err := PlanFillResults(fixture.context, []CommonFillFact{fact})
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.State != lifecycle.StateFailedReconciliation || len(result.Steps) != 1 ||
		result.Steps[0].Observation.MappedOutcome != venue.OutcomeContradiction || result.Steps[0].EconomicSourceEvent != nil ||
		result.Steps[0].Transition == nil || result.Steps[0].Transition.Fill != nil {
		t.Fatalf("contradiction result = %#v", result)
	}
}

func TestPlanKalshiOrderResultsAcceptOnlyExactStatesAndRequireFillDetails(t *testing.T) {
	fixture := newKalshiResultFixture(t, "yes", "12.50")
	resting := fixture.orderFact(t, "resting", "0.00", "12.50")
	ack, err := PlanOrderResult(fixture.context, venue.ObservationSubmitResponse, resting)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Aggregate.State != lifecycle.StateWorking || ack.Aggregate.Binding == nil || ack.Aggregate.Binding.ExternalOrderID != "external-1" {
		t.Fatalf("ack = %#v", ack)
	}

	missing := fixture
	missing.context.Aggregate = ack.Aggregate
	executed := missing.orderFact(t, "executed", "12.50", "0.00")
	failed, err := PlanOrderResult(missing.context, venue.ObservationOrderSnapshot, executed)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Aggregate.State != lifecycle.StateFailedReconciliation || failed.Steps[0].Observation.MappedOutcome != venue.OutcomeContradiction {
		t.Fatalf("executed without fills = %#v", failed)
	}

	legacy := newKalshiResultFixture(t, "yes", "12.50")
	for _, status := range []string{"filled", "open", "new", "pending", "cancelled", "complete", "partially_filled"} {
		legacyFact := legacy.orderFact(t, status, "0.00", "12.50")
		unknown, err := PlanOrderResult(legacy.context, venue.ObservationOrderSnapshot, legacyFact)
		if err != nil {
			t.Fatal(err)
		}
		if unknown.Aggregate.State != lifecycle.StateFailedReconciliation || unknown.Steps[0].Observation.MappedOutcome != venue.OutcomeUnknownState {
			t.Fatalf("legacy status %q = %#v", status, unknown)
		}
	}
}

func TestPlanKalshiExecutedOrderIsEvidenceOnlyAfterExactFills(t *testing.T) {
	fixture := newKalshiResultFixture(t, "yes", "12.50")
	fills, err := PlanFillResults(fixture.context, []CommonFillFact{fixture.fillFact(t, "fill-full", "12.50", "0.37", "0.63", "0.20", 1)})
	if err != nil {
		t.Fatal(err)
	}
	fixture.context.Aggregate = fills.Aggregate
	executed := fixture.orderFact(t, "executed", "12.50", "0.00")
	result, err := PlanOrderResult(fixture.context, venue.ObservationOrderSnapshot, executed)
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.State != lifecycle.StateFilled || len(result.Steps) != 1 || result.Steps[0].Transition != nil || result.Steps[0].Observation.MappedOutcome != venue.OutcomeFillNotice {
		t.Fatalf("executed = %#v", result)
	}
}

type kalshiResultFixture struct {
	context CommonLifecycleContext
	now     time.Time
}

func newKalshiResultFixture(t *testing.T, outcome, quantity string) kalshiResultFixture {
	t.Helper()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	account, err := domain.NewAccount(domain.AccountInput{Name: "Kalshi lifecycle", Environment: domain.AccountEnvironmentPaperScored, Venue: "kalshi", BaseCurrency: "USD", StorageNamespace: "paper_scored/kalshi-lifecycle", StartingCapital: decimal.NewFromInt(10000), BuyingPowerMultiplier: decimal.NewFromInt(1), MarginProfile: domain.MarginProfileCash, CreatedBy: "test", CreationMetadata: json.RawMessage(`{}`), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	policy, order, primary, contract := kalshiCommonFixture(outcome, lifecycle.SideBuy, lifecycle.TimeInForceGTC)
	order.AccountID, order.IntentID = account.ID, uuid.New()
	order.Quantity = decimal.RequireFromString(quantity)
	allocated := order.Quantity
	aggregate := &lifecycle.Aggregate{Intent: lifecycle.Intent{ID: order.IntentID, AccountID: account.ID, Environment: account.Environment, InstrumentID: primary.ID, OriginType: ledger.ExecutionOriginStrategyVersion, OriginID: "strategy-version-1", StrategyVersionID: "strategy-version-1"}, State: lifecycle.StateRouted, AllocatedQuantity: &allocated, Order: order}
	return kalshiResultFixture{context: CommonLifecycleContext{Policy: policy, Aggregate: aggregate, Account: account, Instrument: primary, VenueContract: contract, Route: CommonRouteFacts{Subaccount: 7, ExchangeIndex: 0}, ReceivedAt: now.Add(10 * time.Second)}, now: now}
}

func (fixture kalshiResultFixture) fillFact(t *testing.T, id, count, outcomePrice, complement, fee string, seconds int) CommonFillFact {
	t.Helper()
	outcome, err := exactKalshiOutcome(fixture.context.VenueContract.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	yesPrice, noPrice := outcomePrice, complement
	if outcome == "no" {
		yesPrice, noPrice = complement, outcomePrice
	}
	book := "bid"
	if outcome == "no" {
		book = "ask"
	}
	raw := json.RawMessage(fmt.Sprintf(`{"fill_id":%q,"trade_id":%q,"order_id":"external-1","ticker":%q,"side":%q,"action":"buy","outcome_side":%q,"book_side":%q,"count_fp":%q,"yes_price_dollars":%q,"no_price_dollars":%q,"fee_cost":%q,"created_time":%q,"subaccount_number":7,"exchange_index":0}`, id, "trade-"+id, fixture.context.VenueContract.ContractID, outcome, outcome, book, count, yesPrice, noPrice, fee, fixture.now.Add(time.Duration(seconds)*time.Second).Format(time.RFC3339Nano)))
	var fill CommonFill
	if err := decodeOneJSON(raw, &fill); err != nil {
		t.Fatal(err)
	}
	return CommonFillFact{Fill: fill, RawPayload: raw}
}

func (fixture kalshiResultFixture) orderFact(t *testing.T, status, filled, remaining string) *CommonOrderFact {
	t.Helper()
	outcome, err := exactKalshiOutcome(fixture.context.VenueContract.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	book := expectedKalshiBookSide(outcome, fixture.context.Aggregate.Order.Side)
	yesPrice, noPrice := "0.37", "0.63"
	if outcome == "no" {
		yesPrice, noPrice = "0.63", "0.37"
	}
	raw := json.RawMessage(fmt.Sprintf(`{"order_id":"external-1","client_order_id":%q,"ticker":%q,"side":%q,"action":%q,"outcome_side":%q,"book_side":%q,"type":"limit","status":%q,"yes_price_dollars":%q,"no_price_dollars":%q,"fill_count_fp":%q,"remaining_count_fp":%q,"initial_count_fp":%q,"created_time":%q,"last_update_time":%q,"subaccount_number":7,"exchange_index":0}`, fixture.context.Aggregate.Order.ClientOrderID, fixture.context.VenueContract.ContractID, outcome, fixture.context.Aggregate.Order.Side, outcome, book, status, yesPrice, noPrice, filled, remaining, fixture.context.Aggregate.Order.Quantity.StringFixed(2), fixture.now.Format(time.RFC3339Nano), fixture.now.Add(3*time.Second).Format(time.RFC3339Nano)))
	var order CommonOrder
	if err := decodeOneJSON(raw, &order); err != nil {
		t.Fatal(err)
	}
	return &CommonOrderFact{Order: order, RawPayload: raw}
}

type scriptedResponse struct {
	body []byte
	err  error
}
type scriptedSignedClient struct {
	calls             []string
	get, post, delete []scriptedResponse
}

func (s *scriptedSignedClient) Get(_ context.Context, path string, query url.Values, _ bool) ([]byte, error) {
	s.calls = append(s.calls, "GET "+path+"?"+query.Encode())
	return shiftScript(&s.get)
}

func (s *scriptedSignedClient) Post(_ context.Context, path string, _ any) ([]byte, error) {
	s.calls = append(s.calls, "POST "+path)
	return shiftScript(&s.post)
}

func (s *scriptedSignedClient) Delete(_ context.Context, path string, query url.Values) ([]byte, error) {
	s.calls = append(s.calls, "DELETE "+path+"?"+query.Encode())
	return shiftScript(&s.delete)
}

func shiftScript(items *[]scriptedResponse) ([]byte, error) {
	if len(*items) == 0 {
		return []byte(`{}`), nil
	}
	item := (*items)[0]
	*items = (*items)[1:]
	return item.body, item.err
}

func kalshiCommonFixture(outcome string, side lifecycle.Side, tif lifecycle.TimeInForce) (*venue.Policy, *lifecycle.Order, *instrument.Instrument, *instrument.VenueContract) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	policy, err := venue.ReviewedPolicy(venue.ProviderKalshi)
	if err != nil {
		panic(err)
	}
	instrumentID, orderID := uuid.New(), uuid.New()
	primary := &instrument.Instrument{ID: instrumentID, IdentityKey: "prediction:" + uuid.NewString(), AssetClass: instrument.AssetClassPredictionContract, PrimaryVenue: "kalshi", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.RequireFromString("0.01"), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementBinary, Status: instrument.StatusActive, Metadata: json.RawMessage(`{}`), CreatedAt: now}
	contract := &instrument.VenueContract{ID: uuid.New(), InstrumentID: instrumentID, Venue: "kalshi", ContractID: "KX-TEST", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.RequireFromString("0.01"), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementBinary, ValidFrom: now.Add(-time.Hour), Metadata: json.RawMessage(`{"kalshi_v2":{"outcome":"` + outcome + `"}}`), CreatedAt: now}
	price := decimal.RequireFromString("0.37")
	order := &lifecycle.Order{ID: orderID, IntentID: uuid.New(), AccountID: uuid.New(), InstrumentID: instrumentID, ClientOrderID: orderID.String(), Side: side, OrderType: lifecycle.OrderLimit, TimeInForce: tif, Quantity: decimal.RequireFromString("12.50"), LimitPrice: &price, Venue: "kalshi", VenueContractID: contract.ID, PolicyKind: lifecycle.PolicyVenue, PolicyVersion: policy.Version()}
	return policy, order, primary, contract
}
