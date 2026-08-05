package signal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPolymarketFetchMarketAcceptsNumericVolume(t *testing.T) {
	t.Parallel()
	_, summary := fetchMarketSummary(t, `{"data":[{"market_slug":"slug-1","question":"Q","condition_id":"cond-1","volume_24hr":123.45}]}`)
	if summary.volume24h != 123.45 {
		t.Fatalf("volume24h = %v, want 123.45", summary.volume24h)
	}
}

func TestPolymarketFetchMarketAcceptsStringVolume(t *testing.T) {
	t.Parallel()
	_, summary := fetchMarketSummary(t, `{"data":[{"market_slug":"slug-1","question":"Q","condition_id":"cond-1","volume_24hr":"123.45"}]}`)
	if summary.volume24h != 123.45 {
		t.Fatalf("volume24h = %v, want 123.45", summary.volume24h)
	}
}

func TestPolymarketPollUsesGammaYesTokenForPriceLookup(t *testing.T) {
	t.Parallel()

	const (
		slug        = "slug-1"
		conditionID = "cond-1"
		yesToken    = "yes-token"
	)

	var gotPriceToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/markets":
			if got := r.URL.Query().Get("slug"); got != slug {
				t.Fatalf("/markets slug = %q, want %q", got, slug)
			}
			_, _ = fmt.Fprintf(w, `[{"slug":%q,"question":"Will it happen?","conditionId":%q,"clobTokenIds":[%q,"no-token"],"outcomes":["Yes","No"],"active":true,"closed":false,"acceptingOrders":true,"enableOrderBook":true,"volume24hrClob":321.5}]`, slug, conditionID, yesToken)
		case "/price":
			gotPriceToken = r.URL.Query().Get("token_id")
			if gotPriceToken != yesToken {
				http.Error(w, "wrong token", http.StatusNotFound)
				return
			}
			if got := r.URL.Query().Get("side"); got != "BUY" {
				t.Fatalf("/price side = %q, want BUY", got)
			}
			_, _ = fmt.Fprint(w, `{"market":"slug-1","price":"0.61"}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	p := NewPolymarketSource(PolymarketSourceConfig{CLOBURL: server.URL, Interval: time.Second}, nil)
	p.client = server.Client()
	p.state[slug] = &marketState{lastYesPrice: 0.50}

	events := p.poll(context.Background(), slug)
	if gotPriceToken != yesToken {
		t.Fatalf("price token_id = %q, want %q", gotPriceToken, yesToken)
	}
	if len(events) == 0 {
		t.Fatal("expected price-move event when Gamma yes token is used")
	}
}

func fetchMarketSummary(t *testing.T, body string) (*PolymarketSource, clobMarketSummary) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	p := NewPolymarketSource(PolymarketSourceConfig{CLOBURL: server.URL, Interval: time.Second}, nil)
	summary, err := p.fetchMarket(context.Background(), "slug-1")
	if err != nil {
		t.Fatalf("fetchMarket() error = %v", err)
	}
	return p, summary
}
