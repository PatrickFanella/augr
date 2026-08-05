package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PolymarketSource is a SignalSource that polls the Polymarket CLOB API for
// watched prediction markets, emitting RawSignalEvents when YES price moves
// exceed a threshold or volume spikes beyond a rolling average multiplier.
type PolymarketSource struct {
	clobURL               string
	gammaURL              string
	client                *http.Client
	interval              time.Duration
	priceMoveThreshold    float64 // emit if |ΔpYES| >= this (e.g. 0.05 = 5pp)
	volumeSpikeMultiplier float64 // emit if vol > multiplier * rolling avg
	logger                *slog.Logger
	loader                WatchedMarketsLoader

	mu      sync.Mutex
	markets []string // watched market slugs
	state   map[string]*marketState
}

type WatchedMarketsLoader interface {
	ListEnabledSlugs(ctx context.Context) ([]string, error)
}

type marketState struct {
	lastYesPrice float64
	volHistory   []float64 // rolling 6-sample (1h at 10m interval) volume average
}

// PolymarketSourceConfig holds options for the Polymarket signal source.
type PolymarketSourceConfig struct {
	CLOBURL               string
	GammaURL              string
	Interval              time.Duration
	PriceMoveThreshold    float64 // default 0.05
	VolumeSpikeMultiplier float64 // default 3.0
	Loader                WatchedMarketsLoader
}

// NewPolymarketSource creates a PolymarketSource. The watched market list is
// initially empty; call SetWatchedMarkets before or after Start.
func NewPolymarketSource(cfg PolymarketSourceConfig, logger *slog.Logger) *PolymarketSource {
	if cfg.CLOBURL == "" {
		cfg.CLOBURL = "https://clob.polymarket.com"
	}
	if cfg.GammaURL == "" {
		cfg.GammaURL = "https://gamma-api.polymarket.com"
		if !strings.Contains(cfg.CLOBURL, "clob.polymarket.com") {
			// Tests/local fakes often serve both market metadata and CLOB price
			// responses from the same httptest server.
			cfg.GammaURL = cfg.CLOBURL
		}
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
		gammaURL:              strings.TrimRight(cfg.GammaURL, "/"),
		client:                &http.Client{Timeout: 10 * time.Second},
		interval:              cfg.Interval,
		priceMoveThreshold:    cfg.PriceMoveThreshold,
		volumeSpikeMultiplier: cfg.VolumeSpikeMultiplier,
		logger:                logger,
		loader:                cfg.Loader,
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
		if p.loader != nil {
			if slugs, err := p.loader.ListEnabledSlugs(ctx); err == nil {
				p.SetWatchedMarkets(slugs)
			}
			go func() {
				ticker := time.NewTicker(60 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						slugs, err := p.loader.ListEnabledSlugs(ctx)
						if err == nil {
							p.SetWatchedMarkets(slugs)
						}
					}
				}
			}()
		}
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
		Slug        string      `json:"market_slug"`
		Question    string      `json:"question"`
		ConditionID string      `json:"condition_id"`
		Volume24h   jsonFloat   `json:"volume_24hr"`
		Tokens      []clobToken `json:"tokens"`
	} `json:"data"`
}

type gammaMarketResp []struct {
	Slug             string          `json:"slug"`
	Question         string          `json:"question"`
	ConditionID      string          `json:"conditionId"`
	Volume24h        jsonFloat       `json:"volume24hrClob"`
	FallbackVolume24 jsonFloat       `json:"volume24hr"`
	ClobTokenIDs     flexibleStrings `json:"clobTokenIds"`
	Outcomes         flexibleStrings `json:"outcomes"`
	Active           bool            `json:"active"`
	Closed           bool            `json:"closed"`
	AcceptingOrders  bool            `json:"acceptingOrders"`
	EnableOrderBook  bool            `json:"enableOrderBook"`
}

type clobToken struct {
	TokenID string `json:"token_id"`
	Outcome string `json:"outcome"`
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

	priceTokenID := market.yesTokenID
	if priceTokenID == "" {
		priceTokenID = market.conditionID
	}
	yesPrice, err := p.fetchYesPrice(ctx, priceTokenID)
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
	yesTokenID  string
	question    string
	volume24h   float64
}

type jsonFloat float64

func (f *jsonFloat) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "" {
		*f = 0
		return nil
	}
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = jsonFloat(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			*f = 0
			return nil
		}
		var parsed float64
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			*f = jsonFloat(parsed)
			return nil
		}
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			*f = jsonFloat(parsed)
			return nil
		}
	}
	return fmt.Errorf("invalid float %q", trimmed)
}

type flexibleStrings []string

func (s *flexibleStrings) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		*s = values
		return nil
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err == nil {
		if strings.TrimSpace(encoded) == "" {
			*s = nil
			return nil
		}
		if err := json.Unmarshal([]byte(encoded), &values); err != nil {
			return err
		}
		*s = values
		return nil
	}
	return fmt.Errorf("invalid string list %q", strings.TrimSpace(string(data)))
}

func (p *PolymarketSource) fetchMarket(ctx context.Context, slug string) (clobMarketSummary, error) {
	if summary, err := p.fetchGammaMarket(ctx, slug); err == nil {
		return summary, nil
	}
	return p.fetchClobMarket(ctx, slug)
}

func (p *PolymarketSource) fetchGammaMarket(ctx context.Context, slug string) (clobMarketSummary, error) {
	u, err := url.Parse(p.gammaURL + "/markets")
	if err != nil {
		return clobMarketSummary{}, err
	}
	q := u.Query()
	q.Set("slug", slug)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return clobMarketSummary{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return clobMarketSummary{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return clobMarketSummary{}, fmt.Errorf("gamma markets HTTP %d", resp.StatusCode)
	}

	var page gammaMarketResp
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return clobMarketSummary{}, err
	}
	for _, m := range page {
		if m.Slug != slug {
			continue
		}
		if !m.Active || m.Closed || !m.AcceptingOrders || !m.EnableOrderBook {
			return clobMarketSummary{}, fmt.Errorf("market %q is not accepting CLOB orders", slug)
		}
		volume := float64(m.Volume24h)
		if volume == 0 {
			volume = float64(m.FallbackVolume24)
		}
		return clobMarketSummary{
			conditionID: m.ConditionID,
			yesTokenID:  yesTokenID(m.ClobTokenIDs, m.Outcomes),
			question:    m.Question,
			volume24h:   volume,
		}, nil
	}
	return clobMarketSummary{}, fmt.Errorf("no market for slug %q", slug)
}

func (p *PolymarketSource) fetchClobMarket(ctx context.Context, slug string) (clobMarketSummary, error) {
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return clobMarketSummary{}, fmt.Errorf("markets HTTP %d", resp.StatusCode)
	}

	var page clobMarketResp
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return clobMarketSummary{}, err
	}
	for _, m := range page.Data {
		if m.Slug != slug {
			continue
		}
		return clobMarketSummary{
			conditionID: m.ConditionID,
			yesTokenID:  yesTokenIDFromTokens(m.Tokens),
			question:    m.Question,
			volume24h:   float64(m.Volume24h),
		}, nil
	}
	return clobMarketSummary{}, fmt.Errorf("no market for slug %q", slug)
}

func yesTokenID(tokenIDs, outcomes []string) string {
	for i, outcome := range outcomes {
		if strings.EqualFold(strings.TrimSpace(outcome), "yes") && i < len(tokenIDs) {
			return tokenIDs[i]
		}
	}
	if len(tokenIDs) > 0 {
		return tokenIDs[0]
	}
	return ""
}

func yesTokenIDFromTokens(tokens []clobToken) string {
	for _, token := range tokens {
		if strings.EqualFold(strings.TrimSpace(token.Outcome), "yes") {
			return token.TokenID
		}
	}
	if len(tokens) > 0 {
		return tokens[0].TokenID
	}
	return ""
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
	defer func() { _ = resp.Body.Close() }()
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
