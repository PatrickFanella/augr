package llm

import (
	"context"
	"log/slog"
)

// FallbackProvider wraps a primary and secondary Provider. If the primary
// provider fails, the request is retried against the secondary provider.
// Fallback events are logged for observability.
type FallbackProvider struct {
	primary   Provider
	secondary Provider
	logger    *slog.Logger
}

// NewFallbackProvider constructs a FallbackProvider that tries primary first
// and falls back to secondary on any error.
// If logger is nil, slog.Default() is used.
func NewFallbackProvider(primary, secondary Provider, logger *slog.Logger) *FallbackProvider {
	if logger == nil {
		logger = slog.Default()
	}

	return &FallbackProvider{
		primary:   primary,
		secondary: secondary,
		logger:    logger,
	}
}

// Complete tries the primary provider first. On failure it logs the event and
// attempts the secondary provider. If both fail the secondary error is returned.
func (f *FallbackProvider) Complete(ctx context.Context, request CompletionRequest) (*CompletionResponse, error) {
	resp, err := f.primary.Complete(ctx, request)
	if err == nil {
		return resp, nil
	}

	f.logger.Warn("llm: primary provider failed, falling back to secondary",
		slog.Any("error", err),
	)

	return f.secondary.Complete(ctx, request)
}
