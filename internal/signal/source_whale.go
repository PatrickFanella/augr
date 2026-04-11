package signal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	polymarketdata "github.com/PatrickFanella/get-rich-quick/internal/data/polymarket"
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
	clobURL      string
	interval     time.Duration
	minTradeUSDC float64
	minWinRate   float64
	accounts     WhaleAccountLoader // optional; nil = emit only large-trade signals
	logger       *slog.Logger

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

// Start begins polling recent public Polymarket trades. The channel is closed
// when ctx is cancelled.
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
		tradeID := fmt.Sprintf("%s:%s:%d:%g:%g", t.Address, t.MarketSlug, t.Timestamp.Unix(), t.Price, t.Size)
		if _, seen := w.seenTrades[tradeID]; seen {
			continue
		}
		w.seenTrades[tradeID] = struct{}{}

		price := t.Price
		size := t.Size

		isTracked := w.knownAddrs[t.Address]
		isLarge := size >= w.minTradeUSDC
		isNew := t.Address != "" && !w.knownAddrs[t.Address]

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
				t.MarketSlug, side, price, size)
			body = fmt.Sprintf("High-edge Polymarket account %s purchased %s tokens on market %s. "+
				"Price: %.3f, Size: $%.0f USDC. This account has a tracked winning record.",
				t.Address, side, t.MarketSlug, price, size)
		case isLarge && isNew:
			signalKind = "new_account_large_bet"
			title = fmt.Sprintf("%s: new account large bet $%.0f on %s at %.3f",
				t.MarketSlug, size, side, price)
			body = fmt.Sprintf("Unknown/new Polymarket account %s placed an unusually large "+
				"bet of $%.0f USDC on %s tokens in market %s at price %.3f.",
				t.Address, size, side, t.MarketSlug, price)
		default:
			signalKind = "whale_trade"
			title = fmt.Sprintf("%s: whale trade $%.0f on %s at %.3f",
				t.MarketSlug, size, side, price)
			body = fmt.Sprintf("Large Polymarket trade of $%.0f USDC on %s tokens in market %s "+
				"at price %.3f by account %s.",
				size, side, t.MarketSlug, price, t.Address)
		}

		evts = append(evts, RawSignalEvent{
			Source: "polymarket-whale",
			Title:  title,
			Body:   body,
			Metadata: map[string]any{
				"signal_kind": signalKind,
				"account":     t.Address,
				"market":      t.MarketSlug,
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

func (w *WhaleSource) fetchTrades(ctx context.Context, limit int) ([]polymarketdata.RecentTrade, error) {
	return polymarketdata.FetchRecentTrades(ctx, w.clobURL, limit)
}
