package llm_test

import (
	"context"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type mockCacheMetrics struct {
	hits   int
	misses int
}

func (m *mockCacheMetrics) RecordLLMCacheHit() {
	if m == nil {
		return
	}
	m.hits++
}

func (m *mockCacheMetrics) RecordLLMCacheMiss() {
	if m == nil {
		return
	}
	m.misses++
}

func TestCacheProviderCachesResponsesAndTracksStats(t *testing.T) {
	t.Parallel()

	mock := newMockProvider([]*llm.CompletionResponse{{
		Content:   "cached response",
		Model:     "gpt-5-mini",
		LatencyMS: 42,
		Usage: llm.CompletionUsage{
			PromptTokens:     10,
			CompletionTokens: 4,
		},
	}}, []error{nil})

	cacheProvider, err := llm.NewCacheProvider(mock, llm.NewMemoryResponseCache(), "backtest-v1")
	if err != nil {
		t.Fatalf("NewCacheProvider() error = %v", err)
	}

	collector := llm.NewCacheStatsCollector()
	ctx := llm.WithCacheStatsCollector(context.Background(), collector)
	request := llm.CompletionRequest{
		Model: "gpt-5-mini",
		Messages: []llm.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Summarize AAPL."},
		},
	}

	first, err := cacheProvider.Complete(ctx, request)
	if err != nil {
		t.Fatalf("Complete(first) error = %v", err)
	}
	first.Content = "mutated"

	second, err := cacheProvider.Complete(ctx, request)
	if err != nil {
		t.Fatalf("Complete(second) error = %v", err)
	}

	if mock.calls.Load() != 1 {
		t.Fatalf("underlying calls = %d, want 1", mock.calls.Load())
	}
	if second.Content != "cached response" {
		t.Fatalf("cached response content = %q, want %q", second.Content, "cached response")
	}

	stats := collector.Snapshot()
	if stats.Hits != 1 || stats.Misses != 1 || stats.Requests != 2 {
		t.Fatalf("stats = %+v, want 1 hit, 1 miss, 2 requests", stats)
	}
	if stats.HitRate != 0.5 {
		t.Fatalf("HitRate = %v, want 0.5", stats.HitRate)
	}
	if stats.MissRate != 0.5 {
		t.Fatalf("MissRate = %v, want 0.5", stats.MissRate)
	}
}

func TestCacheProviderTracksStatsPerContext(t *testing.T) {
	t.Parallel()

	mock := newMockProvider([]*llm.CompletionResponse{{
		Content: "ok",
		Model:   "gpt-5-mini",
	}}, []error{nil})

	cacheProvider, err := llm.NewCacheProvider(mock, llm.NewMemoryResponseCache(), "backtest-v1")
	if err != nil {
		t.Fatalf("NewCacheProvider() error = %v", err)
	}

	request := llm.CompletionRequest{
		Model: "gpt-5-mini",
		Messages: []llm.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "prompt"},
		},
	}

	runOne := llm.NewCacheStatsCollector()
	if _, err := cacheProvider.Complete(llm.WithCacheStatsCollector(context.Background(), runOne), request); err != nil {
		t.Fatalf("Complete(run one) error = %v", err)
	}

	runTwo := llm.NewCacheStatsCollector()
	if _, err := cacheProvider.Complete(llm.WithCacheStatsCollector(context.Background(), runTwo), request); err != nil {
		t.Fatalf("Complete(run two) error = %v", err)
	}

	if stats := runOne.Snapshot(); stats.Hits != 0 || stats.Misses != 1 {
		t.Fatalf("run one stats = %+v, want 0 hits and 1 miss", stats)
	}
	if stats := runTwo.Snapshot(); stats.Hits != 1 || stats.Misses != 0 {
		t.Fatalf("run two stats = %+v, want 1 hit and 0 misses", stats)
	}
}

func TestCacheProviderVersionInvalidatesEntries(t *testing.T) {
	t.Parallel()

	mock := newMockProvider([]*llm.CompletionResponse{{
		Content: "versioned",
		Model:   "gpt-5-mini",
	}}, []error{nil})

	cache := llm.NewMemoryResponseCache()
	v1Provider, err := llm.NewCacheProvider(mock, cache, "prompt-v1")
	if err != nil {
		t.Fatalf("NewCacheProvider(v1) error = %v", err)
	}
	v2Provider, err := llm.NewCacheProvider(mock, cache, "prompt-v2")
	if err != nil {
		t.Fatalf("NewCacheProvider(v2) error = %v", err)
	}

	request := llm.CompletionRequest{
		Model: "gpt-5-mini",
		Messages: []llm.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "prompt"},
		},
	}

	if _, err := v1Provider.Complete(context.Background(), request); err != nil {
		t.Fatalf("Complete(v1) error = %v", err)
	}
	if _, err := v2Provider.Complete(context.Background(), request); err != nil {
		t.Fatalf("Complete(v2) error = %v", err)
	}

	if mock.calls.Load() != 2 {
		t.Fatalf("underlying calls = %d, want 2", mock.calls.Load())
	}
}

func TestCacheProviderRequestOptionsInvalidateEntries(t *testing.T) {
	t.Parallel()

	mock := newMockProvider([]*llm.CompletionResponse{{
		Content: "variant",
		Model:   "gpt-5-mini",
	}}, []error{nil})

	cacheProvider, err := llm.NewCacheProvider(mock, llm.NewMemoryResponseCache(), "prompt-v1")
	if err != nil {
		t.Fatalf("NewCacheProvider() error = %v", err)
	}

	base := llm.CompletionRequest{
		Model:       "gpt-5-mini",
		Temperature: 0.1,
		MaxTokens:   100,
		Messages: []llm.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "prompt"},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	}
	variant := base
	variant.Temperature = 0.9

	if _, err := cacheProvider.Complete(context.Background(), base); err != nil {
		t.Fatalf("Complete(base) error = %v", err)
	}
	if _, err := cacheProvider.Complete(context.Background(), variant); err != nil {
		t.Fatalf("Complete(variant) error = %v", err)
	}

	if mock.calls.Load() != 2 {
		t.Fatalf("underlying calls = %d, want 2", mock.calls.Load())
	}
}

func TestCachedProviderRecordsMetricsOnHitAndMiss(t *testing.T) {
	t.Parallel()

	mock := newMockProvider([]*llm.CompletionResponse{{
		Content: "cached response",
		Model:   "gpt-5-mini",
	}}, []error{nil})

	metrics := &mockCacheMetrics{}
	provider := llm.NewCachedProvider(mock, llm.NewMemoryResponseCache()).WithCacheMetrics(metrics)
	request := llm.CompletionRequest{
		Model:    "gpt-5-mini",
		Messages: []llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "prompt"}},
	}

	if _, err := provider.Complete(context.Background(), request); err != nil {
		t.Fatalf("Complete(first) error = %v", err)
	}
	if _, err := provider.Complete(context.Background(), request); err != nil {
		t.Fatalf("Complete(second) error = %v", err)
	}

	if metrics.misses != 1 {
		t.Fatalf("misses = %d, want 1", metrics.misses)
	}
	if metrics.hits != 1 {
		t.Fatalf("hits = %d, want 1", metrics.hits)
	}
}
