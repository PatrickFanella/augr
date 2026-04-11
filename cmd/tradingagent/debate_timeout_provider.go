package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type debateTimeoutFallbackProvider struct {
	provider      llm.Provider
	fallbackModel string
	timeout       time.Duration
	logger        *slog.Logger
}

func newDebateTimeoutFallbackProvider(provider llm.Provider, fallbackModel string, timeout time.Duration, logger *slog.Logger) llm.Provider {
	if provider == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &debateTimeoutFallbackProvider{
		provider:      provider,
		fallbackModel: strings.TrimSpace(fallbackModel),
		timeout:       timeout,
		logger:        logger,
	}
}

func (p *debateTimeoutFallbackProvider) Complete(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if p == nil || p.provider == nil || p.timeout <= 0 {
		return p.provider.Complete(ctx, request)
	}

	firstCtx, cancel := context.WithTimeout(ctx, p.timeout)
	resp, err := p.provider.Complete(firstCtx, request)
	cancel()
	if err == nil {
		return resp, nil
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	originalModel := strings.TrimSpace(request.Model)
	if p.fallbackModel == "" || p.fallbackModel == originalModel {
		return nil, err
	}

	retryReq := request
	retryReq.Model = p.fallbackModel
	p.logger.Warn("debate: LLM call timed out, retrying with quick model",
		slog.String("original_model", originalModel),
		slog.String("fallback_model", p.fallbackModel),
	)
	retryCtx, retryCancel := context.WithTimeout(ctx, p.timeout)
	defer retryCancel()
	return p.provider.Complete(retryCtx, retryReq)
}
