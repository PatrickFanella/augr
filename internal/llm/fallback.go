package llm

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const defaultFallbackTimeout = 60 * time.Second

// FallbackMetrics captures fallback events for observability.
type FallbackMetrics interface {
	RecordLLMFallback(reason string)
}

// FallbackProvider wraps a primary and secondary Provider. If the primary
// provider fails, the request is retried against the secondary provider.
// Fallback events are logged for observability.
//
// Context cancellation errors are returned immediately. DeadlineExceeded
// errors are retried against the secondary provider using a fresh context.
type FallbackProvider struct {
	primary   Provider
	secondary Provider
	logger    *slog.Logger
	metrics   FallbackMetrics
}

// NewFallbackProvider constructs a FallbackProvider that tries primary first
// and falls back to secondary on non-context errors.
// If logger is nil, slog.Default() is used.
func NewFallbackProvider(primary, secondary Provider, logger *slog.Logger) (*FallbackProvider, error) {
	if primary == nil {
		return nil, errors.New("llm: primary provider is nil")
	}
	if secondary == nil {
		return nil, errors.New("llm: secondary provider is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &FallbackProvider{
		primary:   primary,
		secondary: secondary,
		logger:    logger,
	}, nil
}

// WithMetrics wires an optional metrics recorder into the provider.
func (f *FallbackProvider) WithMetrics(metrics FallbackMetrics) *FallbackProvider {
	if f == nil {
		return nil
	}
	f.metrics = metrics
	return f
}

// Complete tries the primary provider first. On failure it logs the event and
// attempts the secondary provider. Context.Canceled is returned immediately
// without fallback. Context.DeadlineExceeded is retried against the secondary
// provider with a fresh context. If both providers fail the secondary error is
// returned.
func (f *FallbackProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	resp, err := f.primary.Complete(ctx, request)
	if err == nil {
		return resp, nil
	}

	if errors.Is(err, context.Canceled) {
		return nil, err
	}

	secondaryCtx := ctx
	if errors.Is(err, context.DeadlineExceeded) {
		secondaryCtx, cancel := newFallbackContext(ctx)
		defer cancel()
		if f.metrics != nil {
			f.metrics.RecordLLMFallback("deadline_exceeded")
		}
		f.logger.Warn("llm: primary provider timed out, falling back to secondary",
			slog.Any("error", err),
		)
		resp, err := f.secondary.Complete(secondaryCtx, request)
		if err == nil && resp != nil {
			resp.UsedFallback = true
			resp.TimedOut = true
		}
		return resp, err
	}
	if f.metrics != nil {
		f.metrics.RecordLLMFallback("provider_error")
	}

	f.logger.Warn("llm: primary provider failed, falling back to secondary",
		slog.Any("error", err),
	)

	resp, err = f.secondary.Complete(secondaryCtx, request)
	if err == nil && resp != nil {
		resp.UsedFallback = true
	}
	return resp, err
}

func newFallbackContext(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(parent)
	if deadline, ok := parent.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			return context.WithTimeout(base, remaining)
		}
	}

	return context.WithTimeout(base, defaultFallbackTimeout)
}
