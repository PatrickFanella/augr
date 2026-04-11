package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultDataAPIBaseURL = "https://data-api.polymarket.com"

// RecentTrade is the subset of the public trades payload needed by the signal
// and automation pipelines.
type RecentTrade struct {
	Address    string
	MarketSlug string
	Outcome    string
	Price      float64
	Size       float64
	Timestamp  time.Time
}

type recentTradeResponse struct {
	ProxyWallet string  `json:"proxyWallet"`
	Slug        string  `json:"slug"`
	Outcome     string  `json:"outcome"`
	Price       float64 `json:"price"`
	Size        float64 `json:"size"`
	Timestamp   int64   `json:"timestamp"`
}

// DataAPIBaseURLFromCLOB converts the configured CLOB URL to the public data
// API host used for trades/positions. For nonstandard hosts (for example local
// stubs in tests), it preserves the provided origin and only strips path/query.
func DataAPIBaseURLFromCLOB(clobURL string) string {
	rawURL := strings.TrimSpace(clobURL)
	if rawURL == "" {
		return defaultDataAPIBaseURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return defaultDataAPIBaseURL
	}

	if strings.HasPrefix(parsed.Host, "clob.") {
		parsed.Host = "data-api." + strings.TrimPrefix(parsed.Host, "clob.")
	}

	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimRight(parsed.String(), "/")
}

// FetchRecentTrades retrieves recent public trades from the Polymarket Data API.
func FetchRecentTrades(ctx context.Context, clobURL string, limit int) ([]RecentTrade, error) {
	baseURL := DataAPIBaseURLFromCLOB(clobURL)
	u, err := url.Parse(baseURL + "/trades")
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

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("polymarket data api trades HTTP %d", resp.StatusCode)
	}

	var raw []recentTradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	trades := make([]RecentTrade, 0, len(raw))
	for _, trade := range raw {
		outcome := trade.Outcome
		if outcome == "" {
			outcome = "YES"
		}

		trades = append(trades, RecentTrade{
			Address:    trade.ProxyWallet,
			MarketSlug: trade.Slug,
			Outcome:    outcome,
			Price:      trade.Price,
			Size:       trade.Size,
			Timestamp:  time.Unix(trade.Timestamp, 0).UTC(),
		})
	}

	return trades, nil
}
