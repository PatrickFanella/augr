package main

import (
	"context"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
)

type llmMetricsProvider struct {
	provider     llm.Provider
	providerName string
	agentRole    string
	metrics      *metrics.Metrics
}

func newLLMMetricsProvider(provider llm.Provider, providerName, agentRole string, metrics *metrics.Metrics) llm.Provider {
	if provider == nil || metrics == nil {
		return provider
	}
	return &llmMetricsProvider{provider: provider, providerName: providerName, agentRole: agentRole, metrics: metrics}
}

func (p *llmMetricsProvider) Complete(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	started := time.Now()
	resp, err := p.provider.Complete(ctx, request)
	model := strings.TrimSpace(request.Model)
	if resp != nil && strings.TrimSpace(resp.Model) != "" {
		model = strings.TrimSpace(resp.Model)
	}
	p.metrics.RecordLLMCall(p.providerName, model, p.agentRole)
	p.metrics.ObserveLLMLatency(p.providerName, model, time.Since(started).Seconds())
	if resp != nil {
		p.metrics.RecordLLMTokens(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	return resp, err
}
