package llm

import (
	"context"
	"errors"
	"log/slog"
)

// FallbackProvider wraps a primary and secondary Provider. If the primary
// provider fails, the request is retried against the secondary provider.
// Fallback events are logged for observability.
//
// Context errors (Canceled, DeadlineExceeded) are returned immediately
// without attempting the secondary provider.
type FallbackProvider struct {
	primary   Provider
	secondary Provider
	logger    *slog.Logger
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

// Complete tries the primary provider first. On failure it logs the event and
// attempts the secondary provider. Context errors (Canceled, DeadlineExceeded)
// are returned immediately without fallback. If both providers fail the
// secondary error is returned.
func (f *FallbackProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	resp, err := f.primary.Complete(ctx, request)
	if err == nil {
		return resp, nil
	}

	// Context errors mean the caller canceled or timed out; don't waste
	// resources calling the secondary provider.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}

	f.logger.Warn("llm: primary provider failed, falling back to secondary",
		slog.Any("error", err),
	)

	return f.secondary.Complete(ctx, request)
}
