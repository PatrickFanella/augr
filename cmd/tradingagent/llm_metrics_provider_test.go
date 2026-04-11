package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
)

type llmMetricsStubProvider struct{}

func (llmMetricsStubProvider) Complete(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content: "ok",
		Model:   "gpt-5-mini",
		Usage:   llm.CompletionUsage{PromptTokens: 11, CompletionTokens: 7},
	}, nil
}

func TestLLMMetricsProvider_RecordsMetrics(t *testing.T) {
	t.Parallel()

	m := metrics.New()
	wrapped := newLLMMetricsProvider(llmMetricsStubProvider{}, "openai", "bull_researcher", m)
	if _, err := wrapped.Complete(context.Background(), llm.CompletionRequest{Model: "gpt-5"}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"tradingagent_llm_calls_total", "provider=\"openai\"", "agent_role=\"bull_researcher\"", "tradingagent_llm_tokens_total", "tradingagent_llm_latency_seconds"} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q", want)
		}
	}
}
