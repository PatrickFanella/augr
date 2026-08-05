package polymarket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGammaClient_GetMarket(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"slug":"will-trump-win-2024",
				"conditionId":"0xcond",
				"clobTokenIds":["token-yes","token-no"],
				"outcomes":["Yes","No"],
				"outcomePrices":[0.61,"0.39"],
				"enableOrderBook":true,
				"negRisk":false,
				"minimum_tick_size":"0.01",
				"minimum_order_size":2
			}
		]`))
	}))
	defer server.Close()

	client := NewGammaClient(server.URL, server.Client())
	market, err := client.GetMarket(context.Background(), " Will-Trump-Win-2024 ")
	if err != nil {
		t.Fatalf("GetMarket() error = %v", err)
	}
	if market.Slug != "will-trump-win-2024" {
		t.Fatalf("Slug = %q, want %q", market.Slug, "will-trump-win-2024")
	}
	if market.ConditionID != "0xcond" {
		t.Fatalf("ConditionID = %q, want %q", market.ConditionID, "0xcond")
	}
	if market.YesTokenID != "token-yes" || market.NoTokenID != "token-no" {
		t.Fatalf("token ids = %q/%q, want token-yes/token-no", market.YesTokenID, market.NoTokenID)
	}
	if market.YesOutcome != "YES" || market.NoOutcome != "NO" {
		t.Fatalf("outcomes = %q/%q, want YES/NO", market.YesOutcome, market.NoOutcome)
	}
	if !market.EnableOrderBook || market.NegRisk {
		t.Fatalf("flags = %+v, want orderbook enabled and neg risk false", market)
	}
	if !almostEqual(market.MinimumTickSize, 0.01) || !almostEqual(market.MinimumOrderSize, 2) {
		t.Fatalf("sizes = %v/%v, want 0.01/2", market.MinimumTickSize, market.MinimumOrderSize)
	}
	if len(market.OutcomePrices) != 2 || !almostEqual(market.OutcomePrices[0], 0.61) || !almostEqual(market.OutcomePrices[1], 0.39) {
		t.Fatalf("OutcomePrices = %#v, want [0.61 0.39]", market.OutcomePrices)
	}

	select {
	case request := <-requests:
		if request != "/markets?slug=will-trump-win-2024" {
			t.Fatalf("request URI = %q, want /markets?slug=will-trump-win-2024", request)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestGammaClient_GetMarket_StringifiedArraysAndOutcomeTokenMapping(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("slug"); got != "encoded-market" {
			t.Fatalf("slug query = %q, want encoded-market", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[{
				"slug":"encoded-market",
				"condition_id":"0xencoded",
				"clobTokenIds":"[\"token-no\",\"token-yes\"]",
				"outcomes":"[\"No\",\"Yes\"]",
				"outcomePrices":"[\"0.42\",0.58]",
				"enable_order_book":true,
				"neg_risk":true,
				"minimumTickSize":"0.001",
				"minimumOrderSize":"5"
			}]
		}`))
	}))
	defer server.Close()

	market, err := NewGammaClient(server.URL, server.Client()).GetMarket(context.Background(), "encoded-market")
	if err != nil {
		t.Fatalf("GetMarket() error = %v", err)
	}
	if market.YesTokenID != "token-yes" || market.NoTokenID != "token-no" {
		t.Fatalf("token ids = %q/%q, want token-yes/token-no", market.YesTokenID, market.NoTokenID)
	}
	if market.YesOutcome != "YES" || market.NoOutcome != "NO" {
		t.Fatalf("outcomes = %q/%q, want YES/NO", market.YesOutcome, market.NoOutcome)
	}
	if len(market.Outcomes) != 2 || market.Outcomes[0] != "NO" || market.Outcomes[1] != "YES" {
		t.Fatalf("Outcomes = %#v, want [NO YES]", market.Outcomes)
	}
	if len(market.OutcomePrices) != 2 || !almostEqual(market.OutcomePrices[0], 0.42) || !almostEqual(market.OutcomePrices[1], 0.58) {
		t.Fatalf("OutcomePrices = %#v, want [0.42 0.58]", market.OutcomePrices)
	}
	if !market.EnableOrderBook || !market.NegRisk {
		t.Fatalf("flags = %+v, want orderbook enabled and neg risk true", market)
	}
	if !almostEqual(market.MinimumTickSize, 0.001) || !almostEqual(market.MinimumOrderSize, 5) {
		t.Fatalf("sizes = %v/%v, want 0.001/5", market.MinimumTickSize, market.MinimumOrderSize)
	}
}

func TestCLOBClient_GetOrderBook(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"market":"0xcond",
			"asset_id":"token-yes",
			"slug":"will-trump-win-2024",
			"outcome":"yes",
			"min_order_size":"1",
			"tick_size":0.01,
			"neg_risk":true,
			"last_trade_price":"0.52",
			"bids":[{"price":"0.48","size":"12"},{"price":0.5,"size":10}],
			"asks":[{"price":0.56,"size":4},{"price":"0.55","size":"8"}]
		}`))
	}))
	defer server.Close()

	client := NewCLOBClient(server.URL, server.Client())
	snapshot, err := client.GetOrderBook(context.Background(), " token-yes ")
	if err != nil {
		t.Fatalf("GetOrderBook() error = %v", err)
	}
	if snapshot.TokenID != "token-yes" {
		t.Fatalf("TokenID = %q, want %q", snapshot.TokenID, "token-yes")
	}
	if snapshot.Slug != "will-trump-win-2024" {
		t.Fatalf("Slug = %q, want %q", snapshot.Slug, "will-trump-win-2024")
	}
	if snapshot.Outcome != "YES" {
		t.Fatalf("Outcome = %q, want YES", snapshot.Outcome)
	}
	if !almostEqual(snapshot.BestBid, 0.5) || !almostEqual(snapshot.BestAsk, 0.55) {
		t.Fatalf("best bid/ask = %v/%v, want 0.5/0.55", snapshot.BestBid, snapshot.BestAsk)
	}
	if !almostEqual(snapshot.Midpoint, 0.525) || !almostEqual(snapshot.Spread, 0.05) {
		t.Fatalf("mid/spread = %v/%v, want 0.525/0.05", snapshot.Midpoint, snapshot.Spread)
	}
	if !almostEqual(snapshot.AskDepthUSD, 6.64) {
		t.Fatalf("AskDepthUSD = %v, want 6.64", snapshot.AskDepthUSD)
	}
	if snapshot.Bids[0].Price != 0.5 || snapshot.Asks[0].Price != 0.55 {
		t.Fatalf("sorted book not preserved: bids=%#v asks=%#v", snapshot.Bids, snapshot.Asks)
	}

	select {
	case request := <-requests:
		if request != "/book?token_id=token-yes" {
			t.Fatalf("request URI = %q, want /book?token_id=token-yes", request)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func almostEqual(got, want float64) bool {
	const eps = 1e-9
	if got > want {
		return got-want < eps
	}
	return want-got < eps
}

func TestClients_RejectEmptyInputsBeforeHTTP(t *testing.T) {
	t.Parallel()

	var gammaHits, clobHits int
	gamma := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { gammaHits++; t.Fatal("gamma should not be called") }))
	defer gamma.Close()
	clob := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { clobHits++; t.Fatal("clob should not be called") }))
	defer clob.Close()

	if _, err := NewGammaClient(gamma.URL, gamma.Client()).GetMarket(context.Background(), "   "); err == nil {
		t.Fatal("GetMarket() error = nil, want non-nil")
	}
	if _, err := NewCLOBClient(clob.URL, clob.Client()).GetOrderBook(context.Background(), ""); err == nil {
		t.Fatal("GetOrderBook() error = nil, want non-nil")
	}
	if gammaHits != 0 || clobHits != 0 {
		t.Fatalf("server hits = %d/%d, want 0/0", gammaHits, clobHits)
	}
}

func TestClients_SurfaceHTTPError(t *testing.T) {
	t.Parallel()

	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer gamma.Close()
	clob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer clob.Close()

	if _, err := NewGammaClient(gamma.URL, gamma.Client()).GetMarket(context.Background(), "slug"); err == nil || err.Error() != "polymarket: gamma markets HTTP 502" {
		t.Fatalf("GetMarket() error = %v, want HTTP 502", err)
	}
	if _, err := NewCLOBClient(clob.URL, clob.Client()).GetOrderBook(context.Background(), "token"); err == nil || err.Error() != "polymarket: clob book HTTP 503" {
		t.Fatalf("GetOrderBook() error = %v, want HTTP 503", err)
	}
}
