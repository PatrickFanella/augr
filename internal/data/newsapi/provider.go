package newsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	defaultBaseURL     = "https://newsapi.org"
	defaultTimeout     = 30 * time.Second
	freeTierRequestCap = 100
	freeTierRateWindow = 24 * time.Hour
	newsAPIMaxPageSize = 100
	everythingEndpoint = "/v2/everything"
)

// Provider retrieves news data from NewsAPI.
type Provider struct {
	client *Client
}

// Client is a small HTTP client for NewsAPI.
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	logger       *slog.Logger
	rateLimiters []*data.RateLimiter
}

// ErrorResponse captures NewsAPI error payloads.
type ErrorResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`

	statusCode int
}

type everythingResponse struct {
	Status       string            `json:"status"`
	TotalResults int               `json:"totalResults"`
	Articles     []everythingEntry `json:"articles"`
}

type everythingEntry struct {
	Source      everythingSource `json:"source"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	URL         string           `json:"url"`
	PublishedAt string           `json:"publishedAt"`
}

type everythingSource struct {
	Name string `json:"name"`
}

var _ data.DataProvider = (*Provider)(nil)

// NewProvider constructs a NewsAPI provider.
func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

// NewClient constructs a NewsAPI HTTP client.
// If logger is nil, slog.Default() is used.
func NewClient(apiKey string, logger *slog.Logger, rateLimiters ...*data.RateLimiter) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	client := &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
		rateLimiters: []*data.RateLimiter{
			data.NewRateLimiter(freeTierRequestCap, freeTierRateWindow),
		},
	}

	for _, limiter := range rateLimiters {
		if limiter != nil {
			client.rateLimiters = append(client.rateLimiters, limiter)
		}
	}

	return client
}

// SetTimeout updates the timeout used by the underlying HTTP client.
func (c *Client) SetTimeout(timeout time.Duration) {
	if c == nil {
		return
	}
	if timeout <= 0 {
		c.logger.Warn("newsapi: ignoring invalid timeout", slog.String("timeout", timeout.String()))
		return
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}

	c.httpClient.Timeout = timeout
}

// GetOHLCV is not supported by the NewsAPI provider yet.
func (p *Provider) GetOHLCV(_ context.Context, _ string, _ data.Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("newsapi: provider is nil")
	}

	return nil, fmt.Errorf("newsapi: GetOHLCV: %w", data.ErrNotImplemented)
}

// GetFundamentals is not supported by the NewsAPI provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("newsapi: provider is nil")
	}

	return data.Fundamentals{}, fmt.Errorf("newsapi: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews returns articles from NewsAPI's everything endpoint.
func (p *Provider) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("newsapi: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("newsapi: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("newsapi: ticker is required")
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, errors.New("newsapi: from must be before or equal to to")
	}

	params := url.Values{
		"q":        []string{ticker},
		"pageSize": []string{strconv.Itoa(newsAPIMaxPageSize)},
	}
	if !from.IsZero() {
		params.Set("from", from.UTC().Format(time.RFC3339))
	}
	if !to.IsZero() {
		params.Set("to", to.UTC().Format(time.RFC3339))
	}

	body, err := p.client.Get(ctx, params)
	if err != nil {
		return nil, err
	}

	var response everythingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("newsapi: decode everything response: %w", err)
	}

	articles := make([]data.NewsArticle, 0, len(response.Articles))
	for _, article := range response.Articles {
		publishedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(article.PublishedAt))
		if err != nil {
			return nil, fmt.Errorf("newsapi: parse article publishedAt %q: %w", article.PublishedAt, err)
		}

		articles = append(articles, data.NewsArticle{
			Title:       article.Title,
			Summary:     article.Description,
			URL:         article.URL,
			Source:      article.Source.Name,
			PublishedAt: publishedAt.UTC(),
		})
	}

	return articles, nil
}

// GetSocialSentiment is not supported by the NewsAPI provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string) (data.SocialSentiment, error) {
	if p == nil {
		return data.SocialSentiment{}, errors.New("newsapi: provider is nil")
	}

	return data.SocialSentiment{}, fmt.Errorf("newsapi: GetSocialSentiment: %w", data.ErrNotImplemented)
}

// Get issues a GET request to the NewsAPI everything endpoint and returns the raw response body.
func (c *Client) Get(ctx context.Context, params url.Values) ([]byte, error) {
	if c == nil {
		return nil, errors.New("newsapi: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("newsapi: api key is required")
	}

	requestURL, err := c.buildURL(everythingEndpoint, params)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("newsapi: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	reservations, err := c.reserveRateLimiters(ctx)
	if err != nil {
		return nil, fmt.Errorf("newsapi: wait for rate limiter: %w", err)
	}
	committedReservations := false
	defer func() {
		if !committedReservations {
			cancelReservations(reservations)
		}
	}()

	startedAt := time.Now()
	c.logger.Info("newsapi: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("newsapi: request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("newsapi: do request: %w", err)
	}
	commitReservations(reservations)
	committedReservations = true
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("newsapi: failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("newsapi: read response body: %w", err)
	}

	durationMS := time.Since(startedAt).Milliseconds()
	c.logger.Info("newsapi: received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
	)

	if apiErr := parseErrorResponse(resp.StatusCode, body); apiErr != nil {
		c.logger.Warn("newsapi: non-success response",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Int("status", apiErr.StatusCode()),
			slog.Any("error", apiErr),
			slog.Int64("duration_ms", durationMS),
		)
		return nil, apiErr
	}

	return body, nil
}

// StatusCode returns the HTTP status code for the error response.
func (e *ErrorResponse) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.statusCode
}

func (e *ErrorResponse) Error() string {
	if e == nil {
		return "newsapi: request failed"
	}

	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = http.StatusText(e.statusCode)
	}
	if message == "" {
		message = "request failed"
	}

	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("newsapi: %s (status=%d, code=%s)", message, e.statusCode, e.Code)
	}

	return fmt.Sprintf("newsapi: %s (status=%d)", message, e.statusCode)
}

func (c *Client) reserveRateLimiters(ctx context.Context) ([]*data.Reservation, error) {
	reservations := make([]*data.Reservation, 0, len(c.rateLimiters))
	for _, limiter := range c.rateLimiters {
		if limiter == nil {
			continue
		}
		reservation, err := limiter.Reserve(ctx)
		if err != nil {
			cancelReservations(reservations)
			return nil, err
		}
		reservations = append(reservations, reservation)
	}

	return reservations, nil
}

func (c *Client) buildURL(requestPath string, params url.Values) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("newsapi: parse base url: %w", err)
	}

	baseURL.Path = joinPath(baseURL.Path, requestPath)
	query := baseURL.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func joinPath(basePath, requestPath string) string {
	trimmedPath := strings.TrimSpace(requestPath)
	cleanPath := "/" + strings.TrimLeft(trimmedPath, "/")
	if trimmedPath == "" {
		cleanPath = "/"
	}

	if basePath == "" || basePath == "/" {
		return cleanPath
	}

	return strings.TrimRight(basePath, "/") + cleanPath
}

func parseErrorResponse(statusCode int, body []byte) *ErrorResponse {
	if isSuccessStatusCode(statusCode) && len(body) == 0 {
		return nil
	}

	errResp := &ErrorResponse{statusCode: statusCode}
	if len(body) > 0 {
		if err := json.Unmarshal(body, errResp); err != nil {
			if !isSuccessStatusCode(statusCode) {
				errResp.Message = strings.TrimSpace(string(body))
			} else {
				return nil
			}
		}
	}

	if isSuccessStatusCode(statusCode) && strings.EqualFold(errResp.Status, "ok") && !errResp.hasErrorMessage() {
		return nil
	}
	if isSuccessStatusCode(statusCode) && !errResp.hasErrorMessage() {
		return nil
	}
	if isSuccessStatusCode(statusCode) {
		errResp.statusCode = errResp.syntheticStatusCode()
	}

	if strings.TrimSpace(errResp.Message) == "" {
		errResp.Message = strings.TrimSpace(string(body))
	}

	return errResp
}

func (e *ErrorResponse) hasErrorMessage() bool {
	if e == nil {
		return false
	}

	return strings.TrimSpace(e.Code) != "" ||
		strings.TrimSpace(e.Message) != "" ||
		strings.EqualFold(strings.TrimSpace(e.Status), "error")
}

func (e *ErrorResponse) syntheticStatusCode() int {
	if e == nil {
		return http.StatusBadGateway
	}

	message := strings.ToLower(strings.TrimSpace(strings.Join([]string{e.Code, e.Message}, " ")))
	switch {
	case strings.Contains(message, "rate"):
		return http.StatusTooManyRequests
	case strings.Contains(message, "api key"), strings.Contains(message, "apikey"), strings.Contains(message, "apikeyinvalid"):
		return http.StatusUnauthorized
	default:
		return http.StatusBadRequest
	}
}

func isSuccessStatusCode(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func commitReservations(reservations []*data.Reservation) {
	for _, reservation := range reservations {
		if reservation == nil {
			continue
		}
		reservation.Commit()
	}
}

func cancelReservations(reservations []*data.Reservation) {
	for i := len(reservations) - 1; i >= 0; i-- {
		if reservations[i] == nil {
			continue
		}
		reservations[i].Cancel()
	}
}
