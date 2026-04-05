package finnhub

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
	defaultBaseURL = "https://finnhub.io/api/v1"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for Finnhub APIs.
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	api          *data.APIClient
	logger       *slog.Logger
	rateLimiters []*data.RateLimiter
}

// ErrorResponse captures Finnhub's standard error response shape.
type ErrorResponse struct {
	ErrorMsg string `json:"error"`

	statusCode int
}

// NewClient constructs a Finnhub HTTP client.
// If logger is nil, slog.Default() is used.
func NewClient(apiKey string, logger *slog.Logger, rateLimiters ...*data.RateLimiter) *Client {
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
			ParamName: "token",
			Value:     trimmedKey,
		},
		Timeout: defaultTimeout,
		Logger:  logger,
		Prefix:  "finnhub",
	})
	api.SetHTTPClient(httpClient)

	client := &Client{
		apiKey:     trimmedKey,
		baseURL:    defaultBaseURL,
		httpClient: httpClient,
		api:        api,
		logger:     logger,
	}

	for _, limiter := range rateLimiters {
		if limiter != nil {
			client.rateLimiters = append(client.rateLimiters, limiter)
		}
	}

	return client
}

// Get issues a GET request to the supplied Finnhub API path and returns the raw response body.
func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if c == nil {
		return nil, errors.New("finnhub: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("finnhub: api key is required")
	}

	// Sync baseURL in case tests changed it directly.
	if c.baseURL != c.api.BaseURL() {
		c.api.SetBaseURL(c.baseURL)
	}

	reservations, err := c.reserveRateLimiters(ctx)
	if err != nil {
		return nil, fmt.Errorf("finnhub: wait for rate limiter: %w", err)
	}
	committedReservations := false
	defer func() {
		if !committedReservations {
			cancelReservations(reservations)
		}
	}()

	body, _, err := c.api.Get(ctx, path, params)
	if err != nil {
		var apiErr *data.APIError
		if errors.As(err, &apiErr) {
			commitReservations(reservations)
			committedReservations = true

			finnhubErr := parseErrorResponse(apiErr.StatusCode, apiErr.Body)
			c.logger.Warn("finnhub: non-success response",
				slog.Int("status", finnhubErr.StatusCode()),
				slog.Any("error", finnhubErr),
			)
			return nil, finnhubErr
		}
		return nil, err
	}

	commitReservations(reservations)
	committedReservations = true

	return body, nil
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

// StatusCode returns the HTTP status code for the error response.
func (e *ErrorResponse) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.statusCode
}

func (e *ErrorResponse) Error() string {
	if e == nil {
		return "finnhub: request failed"
	}

	message := strings.TrimSpace(e.ErrorMsg)
	if message == "" {
		message = http.StatusText(e.statusCode)
	}
	if message == "" {
		message = "request failed"
	}

	return fmt.Sprintf("finnhub: %s (status=%d)", message, e.statusCode)
}

func parseErrorResponse(statusCode int, body []byte) *ErrorResponse {
	errResp := &ErrorResponse{statusCode: statusCode}
	if len(body) == 0 {
		return errResp
	}

	if err := json.Unmarshal(body, errResp); err != nil {
		errResp.ErrorMsg = strings.TrimSpace(string(body))
	}

	if errResp.ErrorMsg == "" {
		errResp.ErrorMsg = strings.TrimSpace(string(body))
	}

	return errResp
}
