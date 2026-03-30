package polymarket

import (
	"bytes"
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
	clobBaseURL    = "https://clob.polymarket.com"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for the Polymarket CLOB API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	apiKey     string
	apiSecret  string
	passphrase string
}

// ErrorResponse captures Polymarket's standard error response shape.
type ErrorResponse struct {
	Message string `json:"error"`

	statusCode int
}

// NewClient constructs a Polymarket CLOB HTTP client.
// If logger is nil, slog.Default() is used.
func NewClient(apiKey, apiSecret, passphrase string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		apiKey:     strings.TrimSpace(apiKey),
		apiSecret:  strings.TrimSpace(apiSecret),
		passphrase: strings.TrimSpace(passphrase),
		baseURL:    clobBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// StatusCode returns the HTTP status code associated with the error.
func (e *ErrorResponse) StatusCode() int {
	if e == nil {
		return 0
	}

	return e.statusCode
}

func (e *ErrorResponse) Error() string {
	if e == nil {
		return "polymarket: request failed"
	}

	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = http.StatusText(e.statusCode)
	}
	if message == "" {
		message = "request failed"
	}

	return fmt.Sprintf("polymarket: %s (status=%d)", message, e.statusCode)
}

// SetBaseURL overrides the configured base URL. This is primarily useful for testing.
func (c *Client) SetBaseURL(baseURL string) {
	if c == nil {
		return
	}

	c.baseURL = baseURL
}

// SetHTTPClient replaces the underlying HTTP client. This is primarily useful for testing.
func (c *Client) SetHTTPClient(httpClient *http.Client) {
	if c == nil || httpClient == nil {
		return
	}

	c.httpClient = httpClient
}

// SetTimeout updates the timeout used by the underlying HTTP client.
func (c *Client) SetTimeout(timeout time.Duration) {
	if c == nil {
		return
	}
	logger := c.getLogger()
	if timeout <= 0 {
		logger.Warn("polymarket: ignoring invalid timeout", slog.String("timeout", timeout.String()))
		return
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultTimeout}
	}

	c.httpClient.Timeout = timeout
}

// Get issues a GET request and returns the raw response body.
func (c *Client) Get(ctx context.Context, requestPath string, params url.Values) ([]byte, error) {
	return c.do(ctx, http.MethodGet, requestPath, params, nil)
}

// Post issues an authenticated POST request with a JSON body and returns the raw response body.
func (c *Client) Post(ctx context.Context, requestPath string, body any) ([]byte, error) {
	return c.do(ctx, http.MethodPost, requestPath, nil, body)
}

// Delete issues an authenticated DELETE request and returns the raw response body.
func (c *Client) Delete(ctx context.Context, requestPath string, body any) ([]byte, error) {
	return c.do(ctx, http.MethodDelete, requestPath, nil, body)
}

func (c *Client) do(ctx context.Context, method, requestPath string, params url.Values, requestBody any) ([]byte, error) {
	if c == nil {
		return nil, errors.New("polymarket: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("polymarket: api key is required")
	}
	if c.apiSecret == "" {
		return nil, errors.New("polymarket: api secret is required")
	}

	logger := c.getLogger()
	httpClient := c.getHTTPClient()

	requestURL, err := c.buildURL(requestPath, params)
	if err != nil {
		return nil, err
	}

	bodyReader, err := marshalRequestBody(requestBody)
	if err != nil {
		return nil, fmt.Errorf("polymarket: marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("polymarket: create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("POLY-ADDRESS", c.apiKey)
	req.Header.Set("POLY-SIGNATURE", c.apiSecret)
	if c.passphrase != "" {
		req.Header.Set("POLY-PASSPHRASE", c.passphrase)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	startedAt := time.Now()
	logger.Debug("polymarket: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Warn("polymarket: request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("polymarket: do request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Warn("polymarket: failed to close response body", slog.Any("error", closeErr))
		}
	}()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("polymarket: read response body: %w", err)
	}

	logger.Debug("polymarket: received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, parseErrorResponse(resp.StatusCode, responseBody)
	}

	return responseBody, nil
}

func marshalRequestBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(payload), nil
}

func parseErrorResponse(statusCode int, body []byte) *ErrorResponse {
	errResp := &ErrorResponse{statusCode: statusCode}
	if len(body) == 0 {
		return errResp
	}

	if err := json.Unmarshal(body, errResp); err != nil {
		errResp.Message = strings.TrimSpace(string(body))
	}
	if strings.TrimSpace(errResp.Message) == "" {
		errResp.Message = strings.TrimSpace(string(body))
	}

	return errResp
}

func (c *Client) buildURL(requestPath string, params url.Values) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("polymarket: parse base url: %w", err)
	}

	baseURL.Path = joinPath(baseURL.Path, requestPath)
	baseURL.RawPath = ""
	if unescapedPath, err := url.PathUnescape(baseURL.Path); err == nil && unescapedPath != baseURL.Path {
		baseURL.RawPath = baseURL.Path
		baseURL.Path = unescapedPath
	}

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
	if trimmedPath == "" {
		if basePath == "" {
			return "/"
		}
		return basePath
	}

	cleanPath := "/" + strings.TrimLeft(trimmedPath, "/")
	if basePath == "" || basePath == "/" {
		return cleanPath
	}

	return strings.TrimRight(basePath, "/") + cleanPath
}

func (c *Client) getLogger() *slog.Logger {
	if c == nil || c.logger == nil {
		return slog.Default()
	}

	return c.logger
}

func (c *Client) getHTTPClient() *http.Client {
	if c == nil || c.httpClient == nil {
		return &http.Client{Timeout: defaultTimeout}
	}

	return c.httpClient
}
