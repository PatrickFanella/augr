package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AuthStyle describes how an API key is injected into requests.
type AuthStyle int

const (
	// AuthStyleNone means no API key is added to the request.
	AuthStyleNone AuthStyle = iota
	// AuthStyleQueryParam adds a key=value pair to the query string.
	AuthStyleQueryParam
	// AuthStyleHeader adds a header Name: Value to the request.
	AuthStyleHeader
)

// AuthConfig describes the authentication mechanism for an API client.
type AuthConfig struct {
	Style      AuthStyle
	ParamName  string // for QueryParam: e.g., "apiKey", "apikey"
	HeaderName string // for Header: e.g., "X-Api-Key", "Authorization"
	Value      string // the API key value
}

// APIClient is a reusable HTTP client for JSON API providers. It handles
// URL construction, authentication injection, rate limiting, logging,
// and non-2xx error wrapping.
type APIClient struct {
	httpClient  *http.Client
	baseURL     string
	auth        AuthConfig
	headers     http.Header
	rateLimiter *RateLimiter
	logger      *slog.Logger
	prefix      string
}

// APIClientConfig holds configuration for constructing an APIClient.
type APIClientConfig struct {
	BaseURL     string
	Auth        AuthConfig
	Headers     http.Header // default headers added to every request
	Timeout     time.Duration
	RateLimiter *RateLimiter
	Logger      *slog.Logger
	Prefix      string
}

// NewAPIClient constructs an APIClient from the supplied configuration.
// If Logger is nil, slog.Default() is used. If Timeout is zero or negative,
// a 30-second default is applied.
func NewAPIClient(cfg APIClientConfig) *APIClient {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	headers := cfg.Headers
	if headers == nil {
		headers = make(http.Header)
	}

	return &APIClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		baseURL:     cfg.BaseURL,
		auth:        cfg.Auth,
		headers:     headers,
		rateLimiter: cfg.RateLimiter,
		logger:      logger,
		prefix:      cfg.Prefix,
	}
}

// APIError represents a non-2xx HTTP response.
type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	if e == nil {
		return "api: request failed"
	}

	body := strings.TrimSpace(string(e.Body))
	if body == "" {
		body = http.StatusText(e.StatusCode)
	}
	if body == "" {
		body = "request failed"
	}

	return fmt.Sprintf("api: %s (status=%d)", body, e.StatusCode)
}

// Get performs a GET request. It returns the response body, HTTP status code,
// and any error. On non-2xx status, the returned error is an *APIError that
// callers can unwrap for provider-specific error parsing.
//
// If a RateLimiter is configured, Get waits for a token before making the
// request.
func (c *APIClient) Get(ctx context.Context, path string, params url.Values) ([]byte, int, error) {
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, 0, fmt.Errorf("%s: wait for rate limiter: %w", c.prefix, err)
		}
	}

	requestURL, err := c.buildURL(path, params)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: create request: %w", c.prefix, err)
	}

	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	if c.auth.Style == AuthStyleHeader {
		req.Header.Set(c.auth.HeaderName, c.auth.Value)
	}

	startedAt := time.Now()
	c.logger.Debug(c.prefix+": sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		err = redactTransportError(err)
		c.logger.Warn(c.prefix+": request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, 0, fmt.Errorf("%s: do request: %w", c.prefix, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn(c.prefix+": failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%s: read response body: %w", c.prefix, err)
	}

	durationMS := time.Since(startedAt).Milliseconds()
	c.logger.Debug(c.prefix+": received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
	)

	if resp.StatusCode >= 400 {
		body = redactConfiguredCredential(body, c.auth.Value)
		return nil, resp.StatusCode, &APIError{StatusCode: resp.StatusCode, Body: body}
	}

	return body, resp.StatusCode, nil
}

func redactConfiguredCredential(body []byte, credential string) []byte {
	credential = strings.TrimSpace(credential)
	if len(body) == 0 || credential == "" {
		return body
	}
	return []byte(strings.ReplaceAll(string(body), credential, "[REDACTED]"))
}

// redactTransportError removes query strings from the URL embedded by
// net/http in transport errors. Query parameters can contain provider
// credentials and must never flow into logs, persisted job errors, or APIs.
func redactTransportError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}

	redacted := *urlErr
	if parsed, parseErr := url.Parse(redacted.URL); parseErr == nil {
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		redacted.URL = parsed.String()
	} else if queryStart := strings.IndexByte(redacted.URL, '?'); queryStart >= 0 {
		redacted.URL = redacted.URL[:queryStart]
	}
	return &redacted
}

// BaseURL returns the base URL of the client.
func (c *APIClient) BaseURL() string {
	return c.baseURL
}

// SetBaseURL sets the base URL of the client. This is primarily useful for testing.
func (c *APIClient) SetBaseURL(baseURL string) {
	c.baseURL = baseURL
}

// SetHTTPClient replaces the underlying *http.Client. This is primarily useful
// for testing.
func (c *APIClient) SetHTTPClient(httpClient *http.Client) {
	c.httpClient = httpClient
}

// SetTimeout updates the timeout used by the underlying HTTP client.
func (c *APIClient) SetTimeout(timeout time.Duration) {
	if timeout <= 0 {
		c.logger.Warn(c.prefix+": ignoring invalid timeout", slog.String("timeout", timeout.String()))
		return
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}

	c.httpClient.Timeout = timeout
}

func (c *APIClient) buildURL(requestPath string, params url.Values) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("%s: parse base url: %w", c.prefix, err)
	}

	baseURL.Path = joinPath(baseURL.Path, requestPath)
	query := baseURL.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}

	if c.auth.Style == AuthStyleQueryParam {
		query.Set(c.auth.ParamName, c.auth.Value)
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
