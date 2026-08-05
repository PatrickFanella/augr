package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConnectionSendsMarketSubscribeAndHeartbeat(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	gotSubscribe := make(chan map[string]any, 1)
	gotPing := make(chan struct{}, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer func() { _ = c.Close() }()

		_, payload, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read subscribe: %v", err)
		}
		var sub map[string]any
		if err := json.Unmarshal(payload, &sub); err != nil {
			t.Fatalf("unmarshal subscribe: %v", err)
		}
		gotSubscribe <- sub
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if string(msg) == "PING" {
				gotPing <- struct{}{}
				return
			}
		}
	}))
	defer ts.Close()

	ticks := make(chan Tick, 1)
	books := make(chan BookSnapshot, 1)
	dropped := &atomic.Uint64{}
	c := newConnection(7, Config{WSURL: "ws" + ts.URL[4:], AssetIDs: []string{"asset-1"}, AssetIDToSlug: map[string]string{"asset-1": "slug-a"}}, ticks, books, dropped)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Dial(ctx); err != nil {
		t.Fatalf("dial: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case sub := <-gotSubscribe:
		if sub["type"] != "market" {
			t.Fatalf("subscribe type = %v", sub["type"])
		}
		if !reflect.DeepEqual(sub["assets_ids"], []any{"asset-1"}) && !reflect.DeepEqual(sub["assets_ids"], []string{"asset-1"}) {
			t.Fatalf("subscribe assets_ids = %#v", sub["assets_ids"])
		}
		if sub["custom_feature_enabled"] != true {
			t.Fatalf("custom_feature_enabled = %v", sub["custom_feature_enabled"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for subscribe")
	}
	select {
	case <-gotPing:
	case <-time.After(12 * time.Second):
		t.Fatal("timeout waiting for ping")
	}
	cancel()
	_ = c.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for run to exit")
	}
}

func TestConnectionParsesMarketEvents(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer func() { _ = c.Close() }()
		_, _, _ = c.ReadMessage()
		msgs := []any{
			map[string]any{"event_type": "book", "asset_id": "asset-1", "bids": [][]string{{"0.40", "1.0"}}, "asks": [][]string{{"0.60", "2.0"}}},
			map[string]any{"event_type": "best_bid_ask", "asset_id": "asset-1", "best_bid": "0.41", "best_ask": "0.61", "spread": "0.20"},
			map[string]any{"event_type": "price_change", "asset_id": "asset-1", "price_changes": []map[string]any{{"side": "BUY", "price": "0.42", "size": "3.0"}}},
			map[string]any{"event_type": "last_trade_price", "asset_id": "asset-1", "last_trade_price": "0.43", "side": "SELL", "size": "4.0"},
		}
		for _, m := range msgs {
			b, _ := json.Marshal(m)
			if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	ticks := make(chan Tick, 8)
	books := make(chan BookSnapshot, 8)
	dropped := &atomic.Uint64{}
	c := newConnection(1, Config{WSURL: "ws" + ts.URL[4:], AssetIDs: []string{"asset-1"}, AssetIDToSlug: map[string]string{"asset-1": "slug-a"}, DropFirstTickPerConn: false}, ticks, books, dropped)
	if err := c.Dial(context.Background()); err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Run(context.Background()); err == nil {
		t.Fatal("expected error from closed server")
	}
	var gotBooks []BookSnapshot
	for len(gotBooks) < 2 {
		select {
		case bs := <-books:
			gotBooks = append(gotBooks, bs)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for books")
		}
	}
	if gotBooks[0].Slug != "slug-a" || gotBooks[0].BestBid != 0.40 || gotBooks[0].BestAsk != 0.60 {
		t.Fatalf("unexpected book: %+v", gotBooks[0])
	}
	if gotBooks[1].BestBid != 0.41 || gotBooks[1].BestAsk != 0.61 {
		t.Fatalf("unexpected best bid/ask book: %+v", gotBooks[1])
	}
	var gotTicks []Tick
	for len(gotTicks) < 2 {
		select {
		case tk := <-ticks:
			gotTicks = append(gotTicks, tk)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for ticks")
		}
	}
	if gotTicks[0].Slug != "slug-a" || gotTicks[0].Price != 0.42 || gotTicks[0].Side != "BUY" {
		t.Fatalf("unexpected price_change tick: %+v", gotTicks[0])
	}
	if gotTicks[1].Price != 0.43 || gotTicks[1].Side != "SELL" {
		t.Fatalf("unexpected last_trade_price tick: %+v", gotTicks[1])
	}
}

func TestConnectionParsesMarketEventArray(t *testing.T) {
	ticks := make(chan Tick, 2)
	books := make(chan BookSnapshot, 2)
	dropped := &atomic.Uint64{}
	c := newConnection(1, Config{AssetIDToSlug: map[string]string{"asset-1": "slug-a"}, DropFirstTickPerConn: false}, ticks, books, dropped)
	c.handleMessage([]byte(`[
		{"event_type":"book","asset_id":"asset-1","bids":[{"price":"0.40","size":"1.0"}],"asks":[{"price":"0.60","size":"2.0"}]},
		{"event_type":"last_trade_price","asset_id":"asset-1","last_trade_price":"0.43","side":"SELL","size":"4.0"}
	]`))
	select {
	case bs := <-books:
		if bs.Slug != "slug-a" || bs.BestBid != 0.40 || bs.BestAsk != 0.60 {
			t.Fatalf("unexpected book: %+v", bs)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for book")
	}
	select {
	case tk := <-ticks:
		if tk.Slug != "slug-a" || tk.Price != 0.43 || tk.Side != "SELL" {
			t.Fatalf("unexpected tick: %+v", tk)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tick")
	}
}
