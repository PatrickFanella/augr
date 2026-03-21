package llm

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"
)

const (
	defaultMaxAttempts = 3
	defaultBaseDelay   = 1 * time.Second
	defaultJitterPct   = 0.20
)

// statusCoder is implemented by errors that carry an HTTP status code.
type statusCoder interface {
	StatusCode() int
}

// RetryProvider wraps a Provider with exponential-backoff retry logic.
// It retries on transient errors (rate limits, server errors, timeouts) and
// does not retry on client errors (bad request, auth failures).
type RetryProvider struct {
	provider    Provider
	maxAttempts int
	baseDelay   time.Duration
	jitterPct   float64
	logger      *slog.Logger
	sleepFn     func(time.Duration) // overridable for testing
}

// RetryOption configures a RetryProvider.
type RetryOption func(*RetryProvider)

// WithMaxAttempts sets the maximum number of attempts (including the first).
func WithMaxAttempts(n int) RetryOption {
	return func(r *RetryProvider) {
		if n > 0 {
			r.maxAttempts = n
		}
	}
}

// WithBaseDelay sets the base delay for exponential backoff.
func WithBaseDelay(d time.Duration) RetryOption {
	return func(r *RetryProvider) {
		if d > 0 {
			r.baseDelay = d
		}
	}
}

// NewRetryProvider wraps provider with retry logic using exponential backoff.
// If logger is nil, slog.Default() is used.
func NewRetryProvider(provider Provider, logger *slog.Logger, opts ...RetryOption) *RetryProvider {
	if logger == nil {
		logger = slog.Default()
	}

	r := &RetryProvider{
		provider:    provider,
		maxAttempts: defaultMaxAttempts,
		baseDelay:   defaultBaseDelay,
		jitterPct:   defaultJitterPct,
		logger:      logger,
		sleepFn:     time.Sleep,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// SetSleepFn overrides the function used to sleep between retries. This is
// intended for testing only.
func (r *RetryProvider) SetSleepFn(fn func(time.Duration)) {
	r.sleepFn = fn
}

// Complete executes the completion request with retry logic. Token usage is
// aggregated across all attempts (including failed ones that return partial usage).
func (r *RetryProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	var (
		lastErr    error
		totalUsage CompletionUsage
	)

	for attempt := range r.maxAttempts {
		if attempt > 0 {
			delay := r.backoffDelay(attempt - 1)

			r.logger.Warn("llm: retrying after transient error",
				slog.Int("attempt", attempt+1),
				slog.Int("max_attempts", r.maxAttempts),
				slog.String("delay", delay.String()),
				slog.Any("error", lastErr),
			)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-r.sleepCh(delay):
			}
		}

		resp, err := r.provider.Complete(ctx, request)

		// Aggregate usage from partial responses.
		if resp != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		}

		if err == nil {
			resp.Usage = totalUsage
			return resp, nil
		}

		lastErr = err

		if !isRetryable(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

// backoffDelay returns the delay for the given retry index (0-based) with jitter.
// Delay = baseDelay * 2^retryIndex ± jitterPct.
func (r *RetryProvider) backoffDelay(retryIndex int) time.Duration {
	base := float64(r.baseDelay) * math.Pow(2, float64(retryIndex))
	jitter := base * r.jitterPct * (2*rand.Float64() - 1) // [-jitterPct, +jitterPct]
	d := time.Duration(base + jitter)
	if d < 0 {
		d = 0
	}
	return d
}

// sleepCh returns a channel that is closed after the given duration, using the
// overridable sleepFn for testability.
func (r *RetryProvider) sleepCh(d time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		r.sleepFn(d)
		close(ch)
	}()
	return ch
}

// isRetryable classifies an error as retryable. Retryable errors include:
//   - Rate limit (HTTP 429)
//   - Server errors (HTTP 5xx)
//   - Context deadline exceeded (timeout)
//
// Non-retryable errors include:
//   - Context canceled (caller-initiated)
//   - Bad request (HTTP 400)
//   - Authentication errors (HTTP 401, 403)
//   - Other 4xx client errors
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context canceled by caller: not retryable.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Context deadline exceeded (timeout): retryable.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for HTTP status code via interface.
	var sc statusCoder
	if errors.As(err, &sc) {
		code := sc.StatusCode()
		switch {
		case code == 429:
			return true
		case code >= 500:
			return true
		default:
			return false
		}
	}

	// Unknown error type: assume transient and allow retry.
	return true
}
