package polymarket

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type connection struct {
	id               int
	wsURL            string
	assetIDs         []string
	assetIDToSlug    map[string]string
	dropFirst        bool
	logger           *slog.Logger
	ticks            chan<- Tick
	books            chan<- BookSnapshot
	dropped          *atomic.Uint64
	conn             *websocket.Conn
	ctx              context.Context
	cancel           context.CancelFunc
	firstTickDropped bool
}

func newConnection(id int, cfg Config, ticks chan<- Tick, books chan<- BookSnapshot, dropped *atomic.Uint64) *connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &connection{
		id:            id,
		wsURL:         cfg.WSURL,
		assetIDs:      append([]string(nil), cfg.AssetIDs...),
		assetIDToSlug: cloneStringMap(cfg.AssetIDToSlug),
		dropFirst:     cfg.DropFirstTickPerConn,
		logger:        cfg.Logger,
		ticks:         ticks,
		books:         books,
		dropped:       dropped,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (c *connection) log() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}
	return slog.Default()
}

func (c *connection) Dial(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, nil)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *connection) Run(ctx context.Context) error {
	if c.conn == nil {
		if err := c.Dial(ctx); err != nil {
			return err
		}
	}
	defer func() { _ = c.Close() }()

	sub := map[string]any{"type": "market", "assets_ids": c.assetIDs, "custom_feature_enabled": true}
	if err := c.conn.WriteJSON(sub); err != nil {
		return err
	}
	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	pingErr := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				pingErr <- nil
				return
			case <-ticker.C:
				if err := c.conn.WriteMessage(websocket.TextMessage, []byte("PING")); err != nil {
					pingErr <- err
					return
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-pingErr:
			return err
		default:
		}

		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		c.handleMessage(msg)
	}
}

func (c *connection) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

type wireMessage struct {
	EventType      string          `json:"event_type"`
	Type           string          `json:"type"`
	Market         string          `json:"market"`
	AssetID        string          `json:"asset_id"`
	Bids           json.RawMessage `json:"bids"`
	Asks           json.RawMessage `json:"asks"`
	PriceChanges   []priceChange   `json:"price_changes"`
	Side           string          `json:"side"`
	Price          string          `json:"price"`
	Size           string          `json:"size"`
	LastTradePrice string          `json:"last_trade_price"`
	BestBid        string          `json:"best_bid"`
	BestAsk        string          `json:"best_ask"`
	Spread         string          `json:"spread"`
}

type priceChange struct {
	Price string `json:"price"`
	Side  string `json:"side"`
	Size  string `json:"size"`
}

// handleMessage decodes provisional Polymarket CLOB websocket frames.
// The shape is intentionally loose and may be refined in later phases.
func (c *connection) handleMessage(msg []byte) {
	var batch []json.RawMessage
	if err := json.Unmarshal(msg, &batch); err == nil {
		for _, item := range batch {
			c.handleMessage(item)
		}
		return
	}

	var wm wireMessage
	if err := json.Unmarshal(msg, &wm); err != nil {
		c.log().Debug("dropping unparseable message", "conn_id", c.id, "err", err)
		return
	}

	slug := c.slugForWire(wm)
	now := time.Now().UTC()
	bids := parseWireLevels(wm.Bids)
	asks := parseWireLevels(wm.Asks)
	if len(bids) > 0 || len(asks) > 0 || strings.EqualFold(wm.EventType, "book") || strings.EqualFold(wm.EventType, "best_bid_ask") {
		bs := BookSnapshot{Slug: slug, ReceivedAt: now, ConnID: c.id}
		bs.Bids = bids
		bs.Asks = asks
		if len(bs.Bids) > 0 {
			bs.BestBid = bs.Bids[0].Price
		}
		if len(bs.Asks) > 0 {
			bs.BestAsk = bs.Asks[0].Price
		}
		if bs.BestBid == 0 {
			bs.BestBid, _ = strconv.ParseFloat(wm.BestBid, 64)
		}
		if bs.BestAsk == 0 {
			bs.BestAsk, _ = strconv.ParseFloat(wm.BestAsk, 64)
		}
		select {
		case c.books <- bs:
		default:
			c.addDropped()
		}
	}
	if wm.Side != "" || wm.Price != "" || wm.LastTradePrice != "" || strings.EqualFold(wm.EventType, "last_trade_price") {
		if c.dropFirst && !c.firstTickDropped {
			c.firstTickDropped = true
			return
		}
		priceStr := wm.Price
		if priceStr == "" {
			priceStr = wm.LastTradePrice
		}
		price, _ := strconv.ParseFloat(priceStr, 64)
		size, _ := strconv.ParseFloat(wm.Size, 64)
		tick := Tick{Slug: slug, Side: wm.Side, Price: price, Size: size, ReceivedAt: now, ConnID: c.id}
		select {
		case c.ticks <- tick:
		default:
			c.addDropped()
		}
	}
	for _, pc := range wm.PriceChanges {
		if c.dropFirst && !c.firstTickDropped {
			c.firstTickDropped = true
			continue
		}
		price, _ := strconv.ParseFloat(pc.Price, 64)
		size, _ := strconv.ParseFloat(pc.Size, 64)
		tick := Tick{Slug: slug, Side: pc.Side, Price: price, Size: size, ReceivedAt: now, ConnID: c.id}
		select {
		case c.ticks <- tick:
		default:
			c.addDropped()
		}
	}
}

func parseWireLevels(raw json.RawMessage) []Level {
	if len(raw) == 0 {
		return nil
	}
	var arrayLevels [][]string
	if err := json.Unmarshal(raw, &arrayLevels); err == nil {
		out := make([]Level, 0, len(arrayLevels))
		for _, lvl := range arrayLevels {
			if len(lvl) < 2 {
				continue
			}
			p, _ := strconv.ParseFloat(lvl[0], 64)
			s, _ := strconv.ParseFloat(lvl[1], 64)
			out = append(out, Level{Price: p, Size: s})
		}
		return out
	}
	var objectLevels []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	}
	if err := json.Unmarshal(raw, &objectLevels); err != nil {
		return nil
	}
	out := make([]Level, 0, len(objectLevels))
	for _, lvl := range objectLevels {
		p, _ := strconv.ParseFloat(lvl.Price, 64)
		s, _ := strconv.ParseFloat(lvl.Size, 64)
		out = append(out, Level{Price: p, Size: s})
	}
	return out
}

func (c *connection) slugForWire(wm wireMessage) string {
	if slug := c.assetIDToSlug[wm.AssetID]; slug != "" {
		return slug
	}
	if wm.Market != "" {
		return wm.Market
	}
	if len(c.assetIDs) > 0 {
		if slug := c.assetIDToSlug[c.assetIDs[0]]; slug != "" {
			return slug
		}
	}
	return ""
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (c *connection) addDropped() {
	if c.dropped != nil {
		c.dropped.Add(1)
	}
}
