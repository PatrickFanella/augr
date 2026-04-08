package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// WhaleAccountLoader loads tracked Polymarket accounts for cross-referencing trades.
type WhaleAccountLoader interface {
	ListTrackedAccounts(ctx context.Context, minWinRate float64, limit int) ([]domain.PolymarketAccount, error)
}

// WhaleSource is a SignalSource that monitors Polymarket on-chain trade activity.
// It fires signals when:
//   - A known high-edge account (tracked=true in DB) makes a trade.
//   - Any account makes a trade above MinTradeUSDC.
//   - A brand-new (unseen) account makes a trade above MinTradeUSDC.
type WhaleSource struct {
	clobURL       string
	client        *http.Client
	interval      time.Duration
	minTradeUSDC  float64
	minWinRate    float64
	accounts      WhaleAccountLoader // optional; nil = emit only large-trade signals
	logger        *slog.Logger

	mu          sync.Mutex
	seenTrades  map[string]struct{} // dedup by trade ID
	knownAddrs  map[string]bool     // cached: address → is tracked
	lastRefresh time.Time
}

// WhaleSourceConfig holds options for the whale signal source.
type WhaleSourceConfig struct {
	CLOBURL      string
	Interval     time.Duration
	MinTradeUSDC float64 // default 5000
	MinWinRate   float64 // threshold for listing tracked accounts; default 0.65
}

// NewWhaleSource constructs a WhaleSource.
// accounts may be nil; in that case only large-trade signals fire.
func NewWhaleSource(cfg WhaleSourceConfig, accounts WhaleAccountLoader, logger *slog.Logger) *WhaleSource {
	if cfg.CLOBURL == "" {
		cfg.CLOBURL = "https://clob.polymarket.com"
	}
	if cfg.Interval == 0 {
		cfg.Interval = 15 * time.Second
	}
	if cfg.MinTradeUSDC == 0 {
		cfg.MinTradeUSDC = 5000
	}
	if cfg.MinWinRate == 0 {
		cfg.MinWinRate = 0.65
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &WhaleSource{
		clobURL:      strings.TrimRight(cfg.CLOBURL, "/"),
		client:       &http.Client{Timeout: 15 * time.Second},
		interval:     cfg.Interval,
		minTradeUSDC: cfg.MinTradeUSDC,
		minWinRate:   cfg.MinWinRate,
		accounts:     accounts,
		logger:       logger,
		seenTrades:   make(map[string]struct{}),
		knownAddrs:   make(map[string]bool),
	}
}

// Name returns the source identifier.
func (w *WhaleSource) Name() string { return "polymarket-whale" }

// Start begins polling the Polymarket CLOB trades endpoint. The channel is
// closed when ctx is cancelled.
func (w *WhaleSource) Start(ctx context.Context) (<-chan RawSignalEvent, error) {
	ch := make(chan RawSignalEvent, 64)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				evts := w.poll(ctx)
				for _, evt := range evts {
					select {
					case ch <- evt:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch, nil
}

// — CLOB trades API types —

type whaleCLOBTradesResp struct {
	Data []whaleCLOBTrade `json:"data"`
}

type whaleCLOBTrade struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Market     string `json:"market"`
	Outcome    string `json:"outcome"` // "YES" or "NO"
	Price      string `json:"price"`
	Size       string `json:"size"`
	MatchTime  string `json:"match_time"`
}

func (w *WhaleSource) poll(ctx context.Context) []RawSignalEvent {
	// Refresh known-address cache every 10 minutes.
	w.mu.Lock()
	needsRefresh := w.accounts != nil && time.Since(w.lastRefresh) > 10*time.Minute
	w.mu.Unlock()

	if needsRefresh {
		w.refreshKnownAddrs(ctx)
	}

	trades, err := w.fetchTrades(ctx, 200)
	if err != nil {
		w.logger.Warn("whale source: fetch trades failed", slog.Any("error", err))
		return nil
	}

	var evts []RawSignalEvent
	now := time.Now()

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, t := range trades {
		if _, seen := w.seenTrades[t.ID]; seen {
			continue
		}
		w.seenTrades[t.ID] = struct{}{}

		var price, size float64
		fmt.Sscanf(t.Price, "%f", &price)
		fmt.Sscanf(t.Size, "%f", &size)

		isTracked := w.knownAddrs[t.Owner]
		isLarge := size >= w.minTradeUSDC
		isNew := t.Owner != "" && !w.knownAddrs[t.Owner]

		if !isTracked && !isLarge {
			continue
		}

		side := t.Outcome
		if side == "" {
			side = "YES"
		}

		var signalKind, title, body string
		switch {
		case isTracked:
			signalKind = "high_edge_trade"
			title = fmt.Sprintf("%s: tracked account bought %s at %.3f ($%.0f USDC)",
				t.Market, side, price, size)
			body = fmt.Sprintf("High-edge Polymarket account %s purchased %s tokens on market %s. "+
				"Price: %.3f, Size: $%.0f USDC. This account has a tracked winning record.",
				t.Owner, side, t.Market, price, size)
		case isLarge && isNew:
			signalKind = "new_account_large_bet"
			title = fmt.Sprintf("%s: new account large bet $%.0f on %s at %.3f",
				t.Market, size, side, price)
			body = fmt.Sprintf("Unknown/new Polymarket account %s placed an unusually large "+
				"bet of $%.0f USDC on %s tokens in market %s at price %.3f.",
				t.Owner, size, side, t.Market, price)
		default:
			signalKind = "whale_trade"
			title = fmt.Sprintf("%s: whale trade $%.0f on %s at %.3f",
				t.Market, size, side, price)
			body = fmt.Sprintf("Large Polymarket trade of $%.0f USDC on %s tokens in market %s "+
				"at price %.3f by account %s.",
				size, side, t.Market, price, t.Owner)
		}

		evts = append(evts, RawSignalEvent{
			Source: "polymarket-whale",
			Title:  title,
			Body:   body,
			Metadata: map[string]any{
				"signal_kind": signalKind,
				"account":     t.Owner,
				"market":      t.Market,
				"side":        side,
				"price":       price,
				"size_usdc":   size,
				"is_tracked":  isTracked,
				"is_new":      isNew,
			},
			ReceivedAt: now,
		})
	}

	// Prune seen-trade set: keep only last 2000 IDs.
	if len(w.seenTrades) > 2000 {
		w.seenTrades = make(map[string]struct{})
	}

	return evts
}

func (w *WhaleSource) refreshKnownAddrs(ctx context.Context) {
	accs, err := w.accounts.ListTrackedAccounts(ctx, w.minWinRate, 500)
	if err != nil {
		if err != repository.ErrNotFound {
			w.logger.Warn("whale source: load tracked accounts failed", slog.Any("error", err))
		}
		return
	}

	newMap := make(map[string]bool, len(accs))
	for _, a := range accs {
		newMap[a.Address] = true
	}

	w.mu.Lock()
	w.knownAddrs = newMap
	w.lastRefresh = time.Now()
	w.mu.Unlock()
}

func (w *WhaleSource) fetchTrades(ctx context.Context, limit int) ([]whaleCLOBTrade, error) {
	u, err := url.Parse(w.clobURL + "/data/trades")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CLOB trades HTTP %d", resp.StatusCode)
	}

	var result whaleCLOBTradesResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}
