package polygon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.polygon.io"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for Polygon.io APIs.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
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

	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// SetTimeout updates the timeout used by the underlying HTTP client.
func (c *Client) SetTimeout(timeout time.Duration) {
	if c == nil || timeout <= 0 {
		return
	}

	c.httpClient.Timeout = timeout
}

// Get issues a GET request to the supplied Polygon API path and returns the raw response body.
func (c *Client) Get(ctx context.Context, requestPath string, params url.Values) ([]byte, error) {
	if c == nil {
		return nil, errors.New("polygon: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("polygon: api key is required")
	}

	requestURL, err := c.buildURL(requestPath, params)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("polygon: create request: %w", err)
	}

	startedAt := time.Now()
	c.logger.Info("polygon: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("polygon: request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("polygon: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("polygon: read response body: %w", err)
	}

	durationMS := time.Since(startedAt).Milliseconds()
	c.logger.Info("polygon: received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
	)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := parseErrorResponse(resp.StatusCode, body)
		c.logger.Warn("polygon: non-success response",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Int("status", resp.StatusCode),
			slog.String("request_id", apiErr.RequestID),
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

func (c *Client) buildURL(requestPath string, params url.Values) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("polygon: parse base url: %w", err)
	}

	baseURL.Path = joinPath(baseURL.Path, requestPath)
	query := baseURL.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	query.Set("apiKey", c.apiKey)
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
}

func joinPath(basePath, requestPath string) string {
	cleanPath := "/" + strings.TrimLeft(strings.TrimSpace(requestPath), "/")
	if strings.TrimSpace(requestPath) == "" {
		cleanPath = "/"
	}

	if basePath == "" || basePath == "/" {
		return cleanPath
	}

	return strings.TrimRight(basePath, "/") + cleanPath
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
