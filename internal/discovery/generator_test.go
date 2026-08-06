package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestGenerateStrategyWithEvidenceRecordsHashesAndAttemptsWithoutContent(t *testing.T) {
	t.Parallel()

	provider := &stubCompletionProvider{responses: []*llm.CompletionResponse{
		{Content: ""},
		{Content: "SECRET_REASONING\n" + validStrategyJSON, Model: "openai/test", Usage: llm.CompletionUsage{PromptTokens: 11, CompletionTokens: 22}, LatencyMS: 333, CostUSD: 0.01, UsedFallback: true},
	}}
	generated, evidence, err := GenerateStrategyWithEvidence(context.Background(), GeneratorConfig{
		Provider: provider, Model: "requested/test", MaxRetries: 1,
	}, ScreenResult{Ticker: "TRACE"}, nil)
	if err != nil || generated == nil {
		t.Fatalf("GenerateStrategyWithEvidence() = (%#v, %v), want success", generated, err)
	}
	if evidence == nil || evidence.Ticker != "TRACE" || len(evidence.Attempts) != 2 || evidence.Config == nil {
		t.Fatalf("generation evidence = %#v", evidence)
	}
	if evidence.Attempts[0].Outcome != "validation_retry" || evidence.Attempts[1].Outcome != "success_after_retry" {
		t.Fatalf("attempt outcomes = %#v", evidence.Attempts)
	}
	terminal := evidence.Attempts[1]
	if terminal.ResponseModel != "openai/test" || terminal.PromptTokens != 11 || terminal.CompletionTokens != 22 || terminal.LatencyMS != 333 || !terminal.UsedFallback || terminal.ContentSHA256 == "" {
		t.Fatalf("terminal attempt evidence = %#v", terminal)
	}
	encoded, marshalErr := json.Marshal(evidence)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), "SECRET_REASONING") {
		t.Fatalf("raw model content leaked into evidence: %s", encoded)
	}
}

func TestGenerateStrategy_RetriesAfterEmptyResponse(t *testing.T) {
	t.Parallel()

	provider := &stubCompletionProvider{responses: []*llm.CompletionResponse{
		{Content: ""},
		{Content: validStrategyJSON},
	}}
	metric := &stubGeneratorMetrics{}

	got, err := GenerateStrategy(context.Background(), GeneratorConfig{
		Provider:   provider,
		MaxRetries: 1,
		Metrics:    metric,
	}, ScreenResult{Ticker: "MIMI"}, nil)
	if err != nil {
		t.Fatalf("GenerateStrategy() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("GenerateStrategy() = nil, want config")
	}
	if got.Name != "retry-safe" {
		t.Fatalf("GenerateStrategy().Name = %q, want %q", got.Name, "retry-safe")
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if got := metric.outcomes["stock/success_after_retry"]; got != 1 {
		t.Fatalf("success-after-retry metric = %d, want 1", got)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(provider.requests))
	}
	msgs := provider.requests[1].Messages
	if len(msgs) != 3 {
		t.Fatalf("retry messages = %d, want 3", len(msgs))
	}
	if !strings.Contains(msgs[len(msgs)-1].Content, "rules: empty JSON response") {
		t.Fatalf("retry prompt missing empty-response error: %q", msgs[len(msgs)-1].Content)
	}
}

func TestGenerateStrategy_RecoversJSONObjectAfterReasoningProse(t *testing.T) {
	t.Parallel()

	provider := &stubCompletionProvider{responses: []*llm.CompletionResponse{{Content: "**Focusing on valid JSON output**\nThe answer follows.\n" + validStrategyJSON}}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got, err := GenerateStrategy(context.Background(), GeneratorConfig{Provider: provider}, ScreenResult{Ticker: "WY"}, logger)
	if err != nil {
		t.Fatalf("GenerateStrategy() error = %v", err)
	}
	if got == nil || got.Name != "retry-safe" {
		t.Fatalf("GenerateStrategy() = %#v, want recovered strategy", got)
	}
	if strings.Contains(logs.String(), validStrategyJSON) {
		t.Fatal("generator logs included full LLM response")
	}
	if !strings.Contains(logs.String(), "content_sha256") {
		t.Fatal("generator logs missing response hash")
	}
}

func TestGenerateStrategyRejectsCachedResponse(t *testing.T) {
	t.Parallel()

	underlying := &stubCompletionProvider{responses: []*llm.CompletionResponse{{Content: validStrategyJSON}}}
	cached := llm.NewCachedProvider(underlying, llm.NewMemoryResponseCache())
	candidate := ScreenResult{Ticker: "CACHE"}
	if _, err := GenerateStrategy(context.Background(), GeneratorConfig{Provider: cached}, candidate, nil); err != nil {
		t.Fatalf("first GenerateStrategy() error = %v", err)
	}
	if _, err := GenerateStrategy(context.Background(), GeneratorConfig{Provider: cached}, candidate, nil); err == nil || !strings.Contains(err.Error(), "cached model response rejected") {
		t.Fatalf("cached GenerateStrategy() error = %v", err)
	}
	if underlying.calls != 1 {
		t.Fatalf("underlying provider calls = %d, want 1", underlying.calls)
	}
}

func TestGenerateStrategy_ReturnsErrorAfterRepeatedEmptyResponses(t *testing.T) {
	t.Parallel()

	provider := &stubCompletionProvider{responses: []*llm.CompletionResponse{
		{Content: ""},
		{Content: ""},
	}}
	metric := &stubGeneratorMetrics{}

	got, err := GenerateStrategy(context.Background(), GeneratorConfig{
		Provider:   provider,
		MaxRetries: 1,
		Metrics:    metric,
	}, ScreenResult{Ticker: "MIMI"}, nil)
	if err == nil {
		t.Fatal("GenerateStrategy() error = nil, want non-nil")
	}
	if got != nil {
		t.Fatalf("GenerateStrategy() = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), "rules: empty JSON response") {
		t.Fatalf("GenerateStrategy() error = %q, want empty-response error", err.Error())
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if got := metric.outcomes["stock/validation_exhausted"]; got != 1 {
		t.Fatalf("validation-exhausted metric = %d, want 1", got)
	}
}

type stubGeneratorMetrics struct{ outcomes map[string]int }

func (s *stubGeneratorMetrics) RecordGeneratorOutcome(asset, outcome string) {
	if s.outcomes == nil {
		s.outcomes = make(map[string]int)
	}
	s.outcomes[asset+"/"+outcome]++
}

func TestGenerateStrategy_OnlyLogsRetryWhenAnotherAttemptRemains(t *testing.T) {
	t.Parallel()

	provider := &stubCompletionProvider{responses: []*llm.CompletionResponse{
		{Content: ""},
		{Content: ""},
	}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got, err := GenerateStrategy(context.Background(), GeneratorConfig{
		Provider:   provider,
		MaxRetries: 1,
	}, ScreenResult{Ticker: "MIMI"}, logger)
	if err == nil {
		t.Fatal("GenerateStrategy() error = nil, want non-nil")
	}
	if got != nil {
		t.Fatalf("GenerateStrategy() = %#v, want nil", got)
	}
	if count := strings.Count(logs.String(), "discovery/generator: parse/validation failed, retrying"); count != 1 {
		t.Fatalf("retry warn count = %d, want 1\nlogs:\n%s", count, logs.String())
	}
}

type stubCompletionProvider struct {
	responses []*llm.CompletionResponse
	requests  []llm.CompletionRequest
	calls     int
}

func (s *stubCompletionProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.requests = append(s.requests, request)
	idx := s.calls
	s.calls++
	if idx >= len(s.responses) {
		return s.responses[len(s.responses)-1], nil
	}
	return s.responses[idx], nil
}

const validStrategyJSON = `{"version":1,"name":"retry-safe","description":"minimal valid strategy","entry":{"operator":"AND","conditions":[{"field":"rsi_14","op":"lt","value":30}]},"exit":{"operator":"OR","conditions":[{"field":"rsi_14","op":"gt","value":70}]},"position_sizing":{"method":"fixed_fraction","fraction_pct":5},"stop_loss":{"method":"fixed_pct","pct":2},"take_profit":{"method":"risk_reward","ratio":2.5}}`
