package polymarket

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

func TestGetOHLCVUsesYesTokenForPriceHistory(t *testing.T) {
	t.Parallel()

	const (
		slug     = "will-example-happen"
		yesToken = "yes-token"
	)
	var gotMarket string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/markets":
			if got := r.URL.Query().Get("market_slug"); got != slug {
				t.Fatalf("market_slug = %q, want %q", got, slug)
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"condition_id":"cond-1","tokens":[{"token_id":%q,"outcome":"Yes"},{"token_id":"no-token","outcome":"No"}]}]}`, yesToken)
		case "/prices-history":
			gotMarket = r.URL.Query().Get("market")
			_, _ = fmt.Fprint(w, `{"history":[{"t":1700000000,"p":0.61}]}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(server.URL, nil)
	provider.client = server.Client()
	from := time.Unix(1699999900, 0).UTC()
	to := time.Unix(1700000100, 0).UTC()
	bars, err := provider.GetOHLCV(context.Background(), slug, data.Timeframe5m, from, to)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if gotMarket != yesToken {
		t.Fatalf("prices-history market = %q, want yes token %q", gotMarket, yesToken)
	}
	if len(bars) != 1 || bars[0].Close != 0.61 {
		t.Fatalf("unexpected bars: %+v", bars)
	}
}
