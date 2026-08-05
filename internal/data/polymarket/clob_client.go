package polymarket

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const defaultCLOBBaseURL = "https://clob.polymarket.com"

// CLOBClient reads public market books from the Polymarket CLOB API.
type CLOBClient interface {
	GetOrderBook(ctx context.Context, tokenID string) (domain.PolymarketBookSnapshot, error)
}

// CLOBHTTPClient implements CLOBClient over HTTP.
type CLOBHTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

var _ CLOBClient = (*CLOBHTTPClient)(nil)

// NewCLOBClient constructs an HTTP CLOB client with official defaults.
func NewCLOBClient(baseURL string, httpClient *http.Client) *CLOBHTTPClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultCLOBBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CLOBHTTPClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: httpClient,
	}
}

// GetOrderBook fetches an order book for the given token ID.
func (c *CLOBHTTPClient) GetOrderBook(ctx context.Context, tokenID string) (domain.PolymarketBookSnapshot, error) {
	if c == nil {
		return domain.PolymarketBookSnapshot{}, fmt.Errorf("polymarket: clob client is nil")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domain.PolymarketBookSnapshot{}, fmt.Errorf("polymarket: token id is required")
	}

	baseURL := c.baseURL
	if baseURL == "" {
		baseURL = defaultCLOBBaseURL
	}
	requestURL, err := url.Parse(baseURL + "/book")
	if err != nil {
		return domain.PolymarketBookSnapshot{}, err
	}
	q := requestURL.Query()
	q.Set("token_id", tokenID)
	requestURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return domain.PolymarketBookSnapshot{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.PolymarketBookSnapshot{}, fmt.Errorf("polymarket: clob get order book: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return domain.PolymarketBookSnapshot{}, fmt.Errorf("polymarket: clob book HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.PolymarketBookSnapshot{}, fmt.Errorf("polymarket: read clob book: %w", err)
	}
	book, err := parseOrderBook(body)
	if err != nil {
		return domain.PolymarketBookSnapshot{}, err
	}
	if book.TokenID == "" {
		book.TokenID = tokenID
	}
	snapshot := book.Snapshot()
	snapshot.TokenID = book.TokenID
	snapshot.ReceivedAt = time.Now().UTC()
	return snapshot, nil
}
