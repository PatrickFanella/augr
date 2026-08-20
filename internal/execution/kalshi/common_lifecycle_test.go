package kalshi

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
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
