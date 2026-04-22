package polygon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

const (
	defaultBaseURL = "https://api.polygon.io"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for Polygon.io APIs.
type Client struct {
	apiKey          string
	baseURL         string
	httpClient      *http.Client
	api             *data.APIClient
	logger          *slog.Logger
	tickerPageDelay time.Duration
	sleeper         func(context.Context, time.Duration) error
}

// ErrorResponse captures Polygon's standard error response shape.
type ErrorResponse struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	ErrorMsg  string `json:"error"`
	Message   string `json:"message"`

	statusCode int
}

// NewClient constructs a Polygon.io HTTP client.
// If logger is nil, slog.Default() is used.
func NewClient(apiKey string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	trimmedKey := strings.TrimSpace(apiKey)
	httpClient := &http.Client{
		Timeout: defaultTimeout,
	}

	api := data.NewAPIClient(data.APIClientConfig{
		BaseURL: defaultBaseURL,
		Auth: data.AuthConfig{
			Style:     data.AuthStyleQueryParam,
			ParamName: "apiKey",
			Value:     trimmedKey,
		},
		Timeout: defaultTimeout,
		Logger:  logger,
		Prefix:  "polygon",
	})
	api.SetHTTPClient(httpClient)

	return &Client{
		apiKey:          trimmedKey,
		baseURL:         defaultBaseURL,
		httpClient:      httpClient,
		api:             api,
		logger:          logger,
		tickerPageDelay: 12 * time.Second,
		sleeper:         sleepWithContext,
	}
}

// SetTimeout updates the timeout used by the underlying HTTP client.
func (c *Client) SetTimeout(timeout time.Duration) {
	if c == nil {
		return
	}
	if timeout <= 0 {
		c.logger.Warn("polygon: ignoring invalid timeout", slog.String("timeout", timeout.String()))
		return
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}

	c.httpClient.Timeout = timeout
}

// SetTickerPageDelay overrides the inter-page delay used by ListActiveTickers.
func (c *Client) SetTickerPageDelay(delay time.Duration) {
	if c == nil || delay <= 0 {
		return
	}
	c.tickerPageDelay = delay
}

// SetSleeper overrides the delay implementation used by ListActiveTickers.
func (c *Client) SetSleeper(sleeper func(context.Context, time.Duration) error) {
	if c == nil || sleeper == nil {
		return
	}
	c.sleeper = sleeper
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Get issues a GET request to the supplied Polygon API path and returns the raw response body.
func (c *Client) Get(ctx context.Context, requestPath string, params url.Values) ([]byte, error) {
	if c == nil {
		return nil, errors.New("polygon: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("polygon: api key is required")
	}

	// Sync baseURL in case tests changed it directly.
	if c.baseURL != c.api.BaseURL() {
		c.api.SetBaseURL(c.baseURL)
	}

	body, statusCode, err := c.api.Get(ctx, requestPath, params)
	if err != nil {
		var apiErr *data.APIError
		if errors.As(err, &apiErr) {
			polygonErr := parseErrorResponse(apiErr.StatusCode, apiErr.Body)
			c.logger.Warn("polygon: non-success response",
				slog.Int("status", statusCode),
				slog.String("request_id", polygonErr.RequestID),
				slog.Any("error", polygonErr),
			)
			return nil, polygonErr
		}
		return nil, err
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
		return "polygon: request failed"
	}

	message := strings.TrimSpace(e.ErrorMsg)
	if message == "" {
		message = strings.TrimSpace(e.Message)
	}
	if message == "" {
		message = http.StatusText(e.statusCode)
	}
	if message == "" {
		message = "request failed"
	}

	if e.RequestID != "" {
		return fmt.Sprintf("polygon: %s (status=%d, request_id=%s)", message, e.statusCode, e.RequestID)
	}

	return fmt.Sprintf("polygon: %s (status=%d)", message, e.statusCode)
}

func parseErrorResponse(statusCode int, body []byte) *ErrorResponse {
	errResp := &ErrorResponse{statusCode: statusCode}
	if len(body) == 0 {
		return errResp
	}

	if err := json.Unmarshal(body, errResp); err != nil {
		errResp.Message = strings.TrimSpace(string(body))
	}

	if errResp.ErrorMsg == "" && errResp.Message == "" {
		errResp.Message = strings.TrimSpace(string(body))
	}

	return errResp
}
