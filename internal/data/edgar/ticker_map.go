package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TickerMap caches the SEC company_tickers.json mapping.
type TickerMap struct {
	mu      sync.RWMutex
	tickers map[string]string // ticker → CIK (zero-padded to 10 digits)
}

type tickerEntry struct {
	CIK    int    `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

// NewTickerMap constructs an empty TickerMap.
func NewTickerMap() *TickerMap {
	return &TickerMap{
		tickers: make(map[string]string),
	}
}

// Load fetches company_tickers.json from the SEC and populates the map.
func (m *TickerMap) Load(ctx context.Context, client *Client) error {
	body, err := client.Get(ctx, tickerMapURL)
	if err != nil {
		return fmt.Errorf("edgar: load ticker map: %w", err)
	}

	var raw map[string]tickerEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("edgar: decode ticker map: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.tickers = make(map[string]string, len(raw))
	for _, entry := range raw {
		ticker := strings.ToUpper(strings.TrimSpace(entry.Ticker))
		if ticker == "" {
			continue
		}
		m.tickers[ticker] = fmt.Sprintf("%010d", entry.CIK)
	}

	return nil
}

// GetCIK returns the zero-padded CIK for a ticker symbol.
func (m *TickerMap) GetCIK(ticker string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cik, ok := m.tickers[strings.ToUpper(strings.TrimSpace(ticker))]
	return cik, ok
}
