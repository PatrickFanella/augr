package alphavantage

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

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

const (
	defaultBaseURL = "https://www.alphavantage.co/query"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for Alpha Vantage APIs.
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	logger       *slog.Logger
	rateLimiters []*data.RateLimiter
}

// ErrorResponse captures Alpha Vantage's standard error response shapes.
type ErrorResponse struct {
	Information  string `json:"Information"`
	Note         string `json:"Note"`
	ErrorMessage string `json:"Error Message"`
	Message      string `json:"message"`

	statusCode int
}

// NewClient constructs an Alpha Vantage HTTP client.
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
		c.logger.Warn("alphavantage: ignoring invalid timeout", slog.String("timeout", timeout.String()))
		return
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}

	c.httpClient.Timeout = timeout
}

// Get issues a GET request to the Alpha Vantage query endpoint and returns the raw response body.
func (c *Client) Get(ctx context.Context, params url.Values) ([]byte, error) {
	if c == nil {
		return nil, errors.New("alphavantage: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("alphavantage: api key is required")
	}

	requestURL, err := c.buildURL(params)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("alphavantage: create request: %w", err)
	}

	reservations, err := c.reserveRateLimiters(ctx)
	if err != nil {
		return nil, fmt.Errorf("alphavantage: wait for rate limiter: %w", err)
	}
	committedReservations := false
	defer func() {
		if !committedReservations {
			cancelReservations(reservations)
		}
	}()

	startedAt := time.Now()
	c.logger.Info("alphavantage: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("alphavantage: request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("alphavantage: do request: %w", err)
	}
	commitReservations(reservations)
	committedReservations = true
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("alphavantage: failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("alphavantage: read response body: %w", err)
	}

	durationMS := time.Since(startedAt).Milliseconds()
	c.logger.Info("alphavantage: received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
	)

	if apiErr := parseErrorResponse(resp.StatusCode, body); apiErr != nil {
		c.logger.Warn("alphavantage: non-success response",
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
		return "alphavantage: request failed"
	}

	message := strings.TrimSpace(e.ErrorMessage)
	if message == "" {
		message = strings.TrimSpace(e.Note)
	}
	if message == "" {
		message = strings.TrimSpace(e.Information)
	}
	if message == "" {
		message = strings.TrimSpace(e.Message)
	}
	if message == "" {
		message = http.StatusText(e.statusCode)
	}
	if message == "" {
		message = "request failed"
	}

	return fmt.Sprintf("alphavantage: %s (status=%d)", message, e.statusCode)
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

func (c *Client) buildURL(params url.Values) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("alphavantage: parse base url: %w", err)
	}

	query := baseURL.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	query.Set("apikey", c.apiKey)
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
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

	if isSuccessStatusCode(statusCode) && !errResp.hasErrorMessage() {
		return nil
	}

	if isSuccessStatusCode(statusCode) {
		errResp.statusCode = errResp.syntheticStatusCode()
	}

	if !errResp.hasErrorMessage() && errResp.Message == "" {
		errResp.Message = strings.TrimSpace(string(body))
	}

	return errResp
}

func (e *ErrorResponse) hasErrorMessage() bool {
	if e == nil {
		return false
	}

	return strings.TrimSpace(e.ErrorMessage) != "" ||
		strings.TrimSpace(e.Note) != "" ||
		strings.TrimSpace(e.Information) != "" ||
		strings.TrimSpace(e.Message) != ""
}

func (e *ErrorResponse) syntheticStatusCode() int {
	if e == nil {
		return http.StatusBadGateway
	}

	if strings.TrimSpace(e.Note) != "" {
		return http.StatusTooManyRequests
	}

	message := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		e.ErrorMessage,
		e.Information,
		e.Message,
	}, " ")))
	if strings.Contains(message, "api key") || strings.Contains(message, "apikey") {
		return http.StatusUnauthorized
	}

	return http.StatusBadRequest
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
