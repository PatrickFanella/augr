package edgar

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

const baseURL = "https://data.sec.gov"
const tickerMapURL = "https://www.sec.gov/files/company_tickers.json"

// Client is a rate-limited HTTP client for the SEC EDGAR API.
type Client struct {
	httpClient *http.Client
	userAgent  string
	limiter    *data.RateLimiter // 10 req/sec
	logger     *slog.Logger
}

// NewClient constructs an EDGAR HTTP client.
// The SEC requires User-Agent in the format "CompanyName AdminEmail".
func NewClient(appName, appEmail string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  fmt.Sprintf("%s %s", appName, appEmail),
		limiter:    data.NewRateLimiter(10, time.Second),
		logger:     logger,
	}
}

// Get fetches the given URL, injecting the required User-Agent header
// and waiting on the rate limiter before each request.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("edgar: rate limiter: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("edgar: create request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	c.logger.Debug("edgar: sending request", slog.String("url", url))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("edgar: do request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("edgar: failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("edgar: read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("edgar: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}
