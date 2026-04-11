package stocktwits

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	baseURL        = "https://api.stocktwits.com/api/2"
	defaultTimeout = 15 * time.Second
)

// TrendingSymbol is a trending ticker from StockTwits.
type TrendingSymbol struct {
	Symbol         string  `json:"symbol"`
	Title          string  `json:"title"`
	TrendingScore  float64 `json:"trending_score"`
	WatchlistCount int     `json:"watchlist_count"`
	Sector         string  `json:"sector"`
	Summary        string  // from trends.summary
}

// SymbolSentiment is the sentiment breakdown for a ticker.
type SymbolSentiment struct {
	Symbol     string
	Bullish    int
	Bearish    int
	Total      int
	Score      float64 // bullish / total, 0-1
	MeasuredAt time.Time
}

// Client fetches data from StockTwits.
type Client struct {
	client *http.Client
	logger *slog.Logger
}

// NewClient creates a StockTwits client.
func NewClient(logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		client: &http.Client{Timeout: defaultTimeout},
		logger: logger,
	}
}

// GetTrending returns the current trending symbols.
func (c *Client) GetTrending(ctx context.Context) ([]TrendingSymbol, error) {
	body, err := c.get(ctx, "/trending/symbols.json")
	if err != nil {
		return nil, fmt.Errorf("stocktwits: trending: %w", err)
	}

	var resp struct {
		Symbols []struct {
			Symbol         string  `json:"symbol"`
			Title          string  `json:"title"`
			TrendingScore  float64 `json:"trending_score"`
			WatchlistCount int     `json:"watchlist_count"`
			Sector         string  `json:"sector"`
			Trends         struct {
				Summary string `json:"summary"`
			} `json:"trends"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("stocktwits: parse trending: %w", err)
	}

	symbols := make([]TrendingSymbol, 0, len(resp.Symbols))
	for _, s := range resp.Symbols {
		symbols = append(symbols, TrendingSymbol{
			Symbol:         s.Symbol,
			Title:          s.Title,
			TrendingScore:  s.TrendingScore,
			WatchlistCount: s.WatchlistCount,
			Sector:         s.Sector,
			Summary:        s.Trends.Summary,
		})
	}

	return symbols, nil
}

// GetSymbolSentiment returns sentiment for a specific symbol from its message stream.
func (c *Client) GetSymbolSentiment(ctx context.Context, symbol string) (*SymbolSentiment, error) {
	path := fmt.Sprintf("/streams/symbol/%s.json", symbol)
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("stocktwits: symbol %s: %w", symbol, err)
	}

	var resp struct {
		Messages []struct {
			Entities struct {
				Sentiment *struct {
					Basic string `json:"basic"` // "Bullish" or "Bearish"
				} `json:"sentiment"`
			} `json:"entities"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("stocktwits: parse symbol %s: %w", symbol, err)
	}

	var bullish, bearish int
	for _, msg := range resp.Messages {
		if msg.Entities.Sentiment == nil {
			continue
		}
		switch msg.Entities.Sentiment.Basic {
		case "Bullish":
			bullish++
		case "Bearish":
			bearish++
		}
	}

	total := bullish + bearish
	var score float64
	if total > 0 {
		score = float64(bullish) / float64(total)
	}

	return &SymbolSentiment{
		Symbol:     symbol,
		Bullish:    bullish,
		Bearish:    bearish,
		Total:      total,
		Score:      score,
		MeasuredAt: time.Now(),
	}, nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "get-rich-quick/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}
