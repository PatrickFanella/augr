package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// Provider implements data.DataProvider for Polymarket prediction markets.
// The ticker is a Polymarket market slug (e.g. "will-trump-win-2024").
// OHLCV bars are synthesized from YES-token price history; volume is always 0.
// GetNews and GetSocialSentiment return ErrNotImplemented.
type Provider struct {
	clobURL string
	client  *http.Client
	logger  *slog.Logger
}

// NewProvider creates a Polymarket data provider backed by the CLOB API.
func NewProvider(clobURL string, logger *slog.Logger) *Provider {
	if clobURL == "" {
		clobURL = "https://clob.polymarket.com"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		clobURL: strings.TrimRight(clobURL, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
		logger:  logger,
	}
}

// GetOHLCV returns synthetic OHLCV bars derived from Polymarket YES price history.
// Close = YES price (0–1). Volume is always 0.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	marketID, err := p.resolvePriceHistoryMarketID(ctx, ticker)
	if err != nil {
		return nil, fmt.Errorf("polymarket: resolve slug %q: %w", ticker, err)
	}

	pts, err := p.fetchPriceHistory(ctx, marketID, timeframe, from, to)
	if err != nil {
		return nil, fmt.Errorf("polymarket: price history for %q: %w", ticker, err)
	}

	return bucketOHLCV(pts, timeframe, from, to), nil
}

// GetFundamentals returns empty fundamentals; Polymarket has no financial statements.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	return data.Fundamentals{}, nil
}

// GetNews returns ErrNotImplemented.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	return nil, data.ErrNotImplemented
}

// GetSocialSentiment returns ErrNotImplemented.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]data.SocialSentiment, error) {
	return nil, data.ErrNotImplemented
}

// — internal API types —

type marketsPage struct {
	Data []struct {
		ConditionID string `json:"condition_id"`
		Tokens      []struct {
			TokenID string `json:"token_id"`
			Outcome string `json:"outcome"`
		} `json:"tokens"`
	} `json:"data"`
}

type pricePoint struct {
	T int64   `json:"t"` // Unix seconds
	P float64 `json:"p"` // YES price 0..1
}

type priceHistoryResponse struct {
	History []pricePoint `json:"history"`
}

// resolvePriceHistoryMarketID fetches the YES token id for a market slug. The
// CLOB prices-history endpoint is token-oriented, while generated strategy
// tickers store the human slug.
func (p *Provider) resolvePriceHistoryMarketID(ctx context.Context, slug string) (string, error) {
	u, err := url.Parse(p.clobURL + "/markets")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("market_slug", slug)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("markets HTTP %d", resp.StatusCode)
	}

	var page marketsPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return "", fmt.Errorf("decode markets: %w", err)
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("no market found for slug %q", slug)
	}
	for _, token := range page.Data[0].Tokens {
		if strings.EqualFold(strings.TrimSpace(token.Outcome), "yes") && strings.TrimSpace(token.TokenID) != "" {
			return token.TokenID, nil
		}
	}
	if len(page.Data[0].Tokens) > 0 && strings.TrimSpace(page.Data[0].Tokens[0].TokenID) != "" {
		return page.Data[0].Tokens[0].TokenID, nil
	}
	return page.Data[0].ConditionID, nil
}

// fidelityMinutes converts a Timeframe to the CLOB API fidelity parameter (minutes).
func fidelityMinutes(tf data.Timeframe) int {
	switch tf {
	case data.Timeframe1m:
		return 1
	case data.Timeframe5m:
		return 5
	case data.Timeframe15m:
		return 15
	case data.Timeframe1h:
		return 60
	case data.Timeframe1d:
		return 1440
	default:
		return 60
	}
}

// fetchPriceHistory retrieves YES-token price ticks from the Polymarket CLOB API.
func (p *Provider) fetchPriceHistory(ctx context.Context, marketID string, tf data.Timeframe, from, to time.Time) ([]pricePoint, error) {
	u, err := url.Parse(p.clobURL + "/prices-history")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("market", marketID)
	q.Set("startTs", fmt.Sprintf("%d", from.Unix()))
	q.Set("endTs", fmt.Sprintf("%d", to.Unix()))
	q.Set("fidelity", fmt.Sprintf("%d", fidelityMinutes(tf)))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prices-history HTTP %d", resp.StatusCode)
	}

	var phResp priceHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&phResp); err != nil {
		return nil, fmt.Errorf("decode prices-history: %w", err)
	}
	return phResp.History, nil
}

// bucketOHLCV groups price ticks into OHLCV bars aligned to the timeframe.
func bucketOHLCV(pts []pricePoint, tf data.Timeframe, from, to time.Time) []domain.OHLCV {
	if len(pts) == 0 {
		return nil
	}

	sort.Slice(pts, func(i, j int) bool { return pts[i].T < pts[j].T })

	step := timeframeDuration(tf)
	if step == 0 {
		step = time.Hour
	}

	type bucket struct{ o, h, l, c float64 }
	bars := make(map[time.Time]*bucket)

	fromUTC := from.UTC()
	toUTC := to.UTC()
	for _, pt := range pts {
		ts := time.Unix(pt.T, 0).UTC()
		if ts.Before(fromUTC) || ts.After(toUTC) {
			continue
		}
		key := ts.Truncate(step)
		b, ok := bars[key]
		if !ok {
			bars[key] = &bucket{o: pt.P, h: pt.P, l: pt.P, c: pt.P}
		} else {
			if pt.P > b.h {
				b.h = pt.P
			}
			if pt.P < b.l {
				b.l = pt.P
			}
			b.c = pt.P
		}
	}

	keys := make([]time.Time, 0, len(bars))
	for k := range bars {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	ohlcv := make([]domain.OHLCV, 0, len(keys))
	for _, k := range keys {
		b := bars[k]
		ohlcv = append(ohlcv, domain.OHLCV{
			Timestamp: k,
			Open:      b.o,
			High:      b.h,
			Low:       b.l,
			Close:     b.c,
			Volume:    0,
		})
	}
	return ohlcv
}

func timeframeDuration(tf data.Timeframe) time.Duration {
	switch tf {
	case data.Timeframe1m:
		return time.Minute
	case data.Timeframe5m:
		return 5 * time.Minute
	case data.Timeframe15m:
		return 15 * time.Minute
	case data.Timeframe1h:
		return time.Hour
	case data.Timeframe1d:
		return 24 * time.Hour
	default:
		return 0
	}
}
