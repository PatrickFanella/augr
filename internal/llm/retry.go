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
	timerFn     func(time.Duration) (<-chan time.Time, func() bool) // overridable for testing
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

// defaultTimerFn wraps time.NewTimer into the timerFn signature, returning
// the timer's channel and its Stop method.
func defaultTimerFn(d time.Duration) (<-chan time.Time, func() bool) {
	t := time.NewTimer(d)
	return t.C, t.Stop
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
		timerFn:     defaultTimerFn,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// SetTimerFn overrides the function used to create backoff timers between retries.
// This is intended for testing only. The function should return a channel that
// fires after the duration and a stop function to release resources.
func (r *RetryProvider) SetTimerFn(fn func(time.Duration) (<-chan time.Time, func() bool)) {
	r.timerFn = fn
}

// Complete executes the completion request with retry logic. Token usage is
// aggregated across all attempts (including failed ones that return partial usage).
func (r *RetryProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	if r == nil || r.provider == nil {
		return nil, errors.New("llm: retry provider is nil")
	}

	var (
		lastErr    error
		totalUsage CompletionUsage
	)

	for attempt := range r.maxAttempts {
		// Check for cancellation before each attempt.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if attempt > 0 {
			delay := r.backoffDelay(attempt - 1)

			r.logger.Warn("llm: retrying after transient error",
				slog.Int("attempt", attempt+1),
				slog.Int("max_attempts", r.maxAttempts),
				slog.String("delay", delay.String()),
				slog.Any("error", lastErr),
			)

			ch, stop := r.timerFn(delay)
			select {
			case <-ctx.Done():
				stop()
				return nil, ctx.Err()
			case <-ch:
			}
		}

		resp, err := r.provider.Complete(ctx, request)

		// Aggregate usage from partial responses.
		if resp != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		}

		if err == nil {
			if resp == nil {
				return nil, errors.New("llm: provider returned nil response without error")
			}
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

// isRetryable classifies an error as retryable. Retryable errors include:
//   - Rate limit (HTTP 429)
//   - Server errors (HTTP 5xx)
//
// Non-retryable errors include:
//   - Context canceled (caller-initiated)
//   - Context deadline exceeded (timeout)
//   - Bad request (HTTP 400)
//   - Authentication errors (HTTP 401, 403)
//   - Other 4xx client errors
//   - Unknown error types (non-transient by default)
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context canceled by caller: not retryable.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Context deadline exceeded (timeout): non-retryable.
	if errors.Is(err, context.DeadlineExceeded) {
		return false
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

	// Unknown error type: treat as non-retryable by default. This keeps the
	// retry policy limited to clearly transient cases (timeouts, 429, 5xx).
	return false
}
