package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PolymarketSource is a SignalSource that polls the Polymarket CLOB API for
// watched prediction markets, emitting RawSignalEvents when YES price moves
// exceed a threshold or volume spikes beyond a rolling average multiplier.
type PolymarketSource struct {
	clobURL               string
	client                *http.Client
	interval              time.Duration
	priceMoveThreshold    float64 // emit if |ΔpYES| >= this (e.g. 0.05 = 5pp)
	volumeSpikeMultiplier float64 // emit if vol > multiplier * rolling avg
	logger                *slog.Logger

	mu      sync.Mutex
	markets []string // watched market slugs
	state   map[string]*marketState
}

type marketState struct {
	lastYesPrice float64
	volHistory   []float64 // rolling 6-sample (1h at 10m interval) volume average
}

// PolymarketSourceConfig holds options for the Polymarket signal source.
type PolymarketSourceConfig struct {
	CLOBURL               string
	Interval              time.Duration
	PriceMoveThreshold    float64 // default 0.05
	VolumeSpikeMultiplier float64 // default 3.0
}

// NewPolymarketSource creates a PolymarketSource. The watched market list is
// initially empty; call SetWatchedMarkets before or after Start.
func NewPolymarketSource(cfg PolymarketSourceConfig, logger *slog.Logger) *PolymarketSource {
	if cfg.CLOBURL == "" {
		cfg.CLOBURL = "https://clob.polymarket.com"
	}
	if cfg.Interval == 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.PriceMoveThreshold == 0 {
		cfg.PriceMoveThreshold = 0.05
	}
	if cfg.VolumeSpikeMultiplier == 0 {
		cfg.VolumeSpikeMultiplier = 3.0
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PolymarketSource{
		clobURL:               strings.TrimRight(cfg.CLOBURL, "/"),
		client:                &http.Client{Timeout: 10 * time.Second},
		interval:              cfg.Interval,
		priceMoveThreshold:    cfg.PriceMoveThreshold,
		volumeSpikeMultiplier: cfg.VolumeSpikeMultiplier,
		logger:                logger,
		state:                 make(map[string]*marketState),
	}
}

// Name returns the source identifier.
func (p *PolymarketSource) Name() string { return "polymarket-clob" }

// SetWatchedMarkets updates the list of market slugs to monitor.
// Safe to call concurrently and after Start.
func (p *PolymarketSource) SetWatchedMarkets(slugs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.markets = append(p.markets[:0], slugs...)
}

// Start polls watched markets on the configured interval and emits events on
// price moves or volume spikes. The channel is closed when ctx is cancelled.
func (p *PolymarketSource) Start(ctx context.Context) (<-chan RawSignalEvent, error) {
	ch := make(chan RawSignalEvent, 64)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.mu.Lock()
				slugs := append([]string(nil), p.markets...)
				p.mu.Unlock()

				for _, slug := range slugs {
					evts := p.poll(ctx, slug)
					for _, evt := range evts {
						select {
						case ch <- evt:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()
	return ch, nil
}

// — CLOB API types —

type clobMarketResp struct {
	Data []struct {
		Slug        string  `json:"market_slug"`
		Question    string  `json:"question"`
		ConditionID string  `json:"condition_id"`
		Volume24h   float64 `json:"volume_24hr"`
	} `json:"data"`
}

type clobPricesResp struct {
	Market string `json:"market"`
	Price  string `json:"price"` // YES price as decimal string
}

// poll fetches current state for one market and returns any signal events.
func (p *PolymarketSource) poll(ctx context.Context, slug string) []RawSignalEvent {
	market, err := p.fetchMarket(ctx, slug)
	if err != nil {
		p.logger.Warn("polymarket source: fetch market failed",
			slog.String("slug", slug), slog.Any("error", err))
		return nil
	}

	yesPrice, err := p.fetchYesPrice(ctx, market.conditionID)
	if err != nil {
		p.logger.Warn("polymarket source: fetch price failed",
			slog.String("slug", slug), slog.Any("error", err))
		return nil
	}

	p.mu.Lock()
	ms, exists := p.state[slug]
	if !exists {
		ms = &marketState{lastYesPrice: yesPrice}
		p.state[slug] = ms
		p.mu.Unlock()
		return nil // first observation; no baseline to compare against
	}
	lastPrice := ms.lastYesPrice
	ms.lastYesPrice = yesPrice

	// Rolling volume history (cap at 6 samples).
	ms.volHistory = append(ms.volHistory, market.volume24h)
	if len(ms.volHistory) > 6 {
		ms.volHistory = ms.volHistory[1:]
	}
	volHistory := append([]float64(nil), ms.volHistory...)
	p.mu.Unlock()

	var evts []RawSignalEvent

	// Price move detection.
	priceChange := yesPrice - lastPrice
	if math.Abs(priceChange) >= p.priceMoveThreshold {
		direction := "up"
		if priceChange < 0 {
			direction = "down"
		}
		evts = append(evts, RawSignalEvent{
			Source: "polymarket-clob",
			Title:  fmt.Sprintf("%s YES price moved %+.1f%%: %.1f%% → %.1f%%", slug, priceChange*100, lastPrice*100, yesPrice*100),
			Body:   fmt.Sprintf("%s: %s. YES price %s by %.1f percentage points to %.3f.", slug, market.question, direction, math.Abs(priceChange)*100, yesPrice),
			Metadata: map[string]any{
				"market":           slug,
				"price_change_pct": priceChange * 100,
				"yes_price":        yesPrice,
				"volume_24h":       market.volume24h,
			},
			ReceivedAt: time.Now(),
		})
	}

	// Volume spike detection.
	if len(volHistory) >= 3 {
		sum := 0.0
		for _, v := range volHistory[:len(volHistory)-1] {
			sum += v
		}
		rollingAvg := sum / float64(len(volHistory)-1)
		currentVol := market.volume24h
		if rollingAvg > 0 && currentVol > rollingAvg*p.volumeSpikeMultiplier {
			evts = append(evts, RawSignalEvent{
				Source: "polymarket-clob",
				Title:  fmt.Sprintf("%s volume spike: $%.0f USDC (%.1fx avg)", slug, currentVol, currentVol/rollingAvg),
				Body:   fmt.Sprintf("%s: %s. 24h volume spiked to $%.0f USDC, %.1fx the rolling average ($%.0f).", slug, market.question, currentVol, currentVol/rollingAvg, rollingAvg),
				Metadata: map[string]any{
					"market":      slug,
					"volume":      currentVol,
					"rolling_avg": rollingAvg,
					"spike_ratio": currentVol / rollingAvg,
					"yes_price":   yesPrice,
				},
				ReceivedAt: time.Now(),
			})
		}
	}

	return evts
}

type clobMarketSummary struct {
	conditionID string
	question    string
	volume24h   float64
}

func (p *PolymarketSource) fetchMarket(ctx context.Context, slug string) (clobMarketSummary, error) {
	u, err := url.Parse(p.clobURL + "/markets")
	if err != nil {
		return clobMarketSummary{}, err
	}
	q := u.Query()
	q.Set("market_slug", slug)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return clobMarketSummary{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return clobMarketSummary{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return clobMarketSummary{}, fmt.Errorf("markets HTTP %d", resp.StatusCode)
	}

	var page clobMarketResp
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return clobMarketSummary{}, err
	}
	if len(page.Data) == 0 {
		return clobMarketSummary{}, fmt.Errorf("no market for slug %q", slug)
	}
	m := page.Data[0]
	return clobMarketSummary{
		conditionID: m.ConditionID,
		question:    m.Question,
		volume24h:   m.Volume24h,
	}, nil
}

func (p *PolymarketSource) fetchYesPrice(ctx context.Context, conditionID string) (float64, error) {
	u, err := url.Parse(p.clobURL + "/price")
	if err != nil {
		return 0, err
	}
	q := u.Query()
	q.Set("token_id", conditionID)
	q.Set("side", "BUY")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("price HTTP %d", resp.StatusCode)
	}

	var pr clobPricesResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return 0, err
	}

	var price float64
	if _, err := fmt.Sscanf(pr.Price, "%f", &price); err != nil {
		return 0, fmt.Errorf("parse price %q: %w", pr.Price, err)
	}
	return price, nil
}
