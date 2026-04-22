package polygon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func isRateLimitError(err error, polyErr **ErrorResponse) bool {
	if errors.As(err, polyErr) && (*polyErr).StatusCode() == 429 {
		return true
	}
	return false
}

// TickerInfo represents a single ticker entry from the Polygon reference API.
type TickerInfo struct {
	Ticker          string `json:"ticker"`
	Name            string `json:"name"`
	PrimaryExchange string `json:"primary_exchange"`
	Type            string `json:"type"`
	Active          bool   `json:"active"`
}

// TickerSnapshot represents a single ticker's snapshot from the Polygon snapshot API.
type TickerSnapshot struct {
	Ticker          string      `json:"ticker"`
	TodaysChangePct float64     `json:"todaysChangePerc"`
	Day             SnapshotBar `json:"day"`
	PrevDay         SnapshotBar `json:"prevDay"`
}

// SnapshotBar holds OHLCV + VWAP data from a snapshot bar.
type SnapshotBar struct {
	Open   float64 `json:"o"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Close  float64 `json:"c"`
	Volume float64 `json:"v"`
	VWAP   float64 `json:"vw"`
}

// tickerListResponse models the paginated /v3/reference/tickers response.
type tickerListResponse struct {
	Results []TickerInfo `json:"results"`
	NextURL string       `json:"next_url"`
}

// snapshotResponse models the /v2/snapshot response (top-level key is "tickers").
type snapshotResponse struct {
	Tickers []TickerSnapshot `json:"tickers"`
}

// groupedDailyResponse models the /v2/aggs/grouped response.
type groupedDailyResponse struct {
	Results []groupedDailyResult `json:"results"`
}

type groupedDailyResult struct {
	Ticker    string  `json:"T"`
	Open      float64 `json:"o"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Close     float64 `json:"c"`
	Volume    float64 `json:"v"`
	Timestamp int64   `json:"t"`
}

// ListActiveTickers returns all active tickers matching the given market and
// ticker type from the Polygon reference API. It follows pagination via next_url.
func (c *Client) ListActiveTickers(ctx context.Context, market, tickerType string) ([]TickerInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("polygon: client is nil")
	}

	requestPath := "/v3/reference/tickers"
	baseParams := url.Values{
		"market": []string{market},
		"type":   []string{tickerType},
		"active": []string{"true"},
		"limit":  []string{"1000"},
	}
	params := cloneQueryValues(baseParams)

	tickers := make([]TickerInfo, 0, 1024)
	for {
		body, err := c.Get(ctx, requestPath, params)
		if err != nil {
			// On rate limit (429), return what we have so far instead of failing.
			var polyErr *ErrorResponse
			if len(tickers) > 0 && isRateLimitError(err, &polyErr) {
				c.logger.Warn("polygon: rate limited during ticker list, returning partial results",
					slog.Int("tickers_fetched", len(tickers)),
				)
				return tickers, nil
			}
			return nil, fmt.Errorf("polygon: list active tickers: %w", err)
		}

		var resp tickerListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("polygon: decode tickers response: %w", err)
		}

		tickers = append(tickers, resp.Results...)

		if strings.TrimSpace(resp.NextURL) == "" {
			break
		}

		requestPath, params, err = nextPageRequest(resp.NextURL, baseParams)
		if err != nil {
			return nil, fmt.Errorf("polygon: parse next_url: %w", err)
		}

		// Rate limit pause: Polygon free tier allows 5 req/min, so paginated
		// reference-ticker requests need roughly 12s spacing to avoid 429s.
		delay := c.tickerPageDelay
		if delay <= 0 {
			delay = 12 * time.Second
		}
		sleeper := c.sleeper
		if sleeper == nil {
			sleeper = sleepWithContext
		}
		if err := sleeper(ctx, delay); err != nil {
			return tickers, err
		}
	}

	return tickers, nil
}

// BulkSnapshot returns snapshots for the specified tickers (or all tickers if
// the slice is empty) from the Polygon snapshot API.
func (c *Client) BulkSnapshot(ctx context.Context, tickers []string) ([]TickerSnapshot, error) {
	if c == nil {
		return nil, fmt.Errorf("polygon: client is nil")
	}

	requestPath := "/v2/snapshot/locale/us/markets/stocks/tickers"
	params := url.Values{}
	if len(tickers) > 0 {
		params.Set("tickers", strings.Join(tickers, ","))
	}

	body, err := c.Get(ctx, requestPath, params)
	if err != nil {
		return nil, fmt.Errorf("polygon: bulk snapshot: %w", err)
	}

	var resp snapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("polygon: decode snapshot response: %w", err)
	}

	return resp.Tickers, nil
}

// GroupedDailyBars returns OHLCV bars for every ticker on a given date from the
// Polygon grouped daily endpoint. The date should be in YYYY-MM-DD format.
func (c *Client) GroupedDailyBars(ctx context.Context, date string) (map[string]domain.OHLCV, error) {
	if c == nil {
		return nil, fmt.Errorf("polygon: client is nil")
	}

	requestPath := fmt.Sprintf("/v2/aggs/grouped/locale/us/market/stocks/%s", url.PathEscape(date))
	params := url.Values{
		"adjusted": []string{"true"},
	}

	body, err := c.Get(ctx, requestPath, params)
	if err != nil {
		return nil, fmt.Errorf("polygon: grouped daily bars: %w", err)
	}

	var resp groupedDailyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("polygon: decode grouped daily response: %w", err)
	}

	bars := make(map[string]domain.OHLCV, len(resp.Results))
	for _, r := range resp.Results {
		bars[r.Ticker] = domain.OHLCV{
			Timestamp: time.UnixMilli(r.Timestamp).UTC(),
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			Close:     r.Close,
			Volume:    r.Volume,
		}
	}

	return bars, nil
}
