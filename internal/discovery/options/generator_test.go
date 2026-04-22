package options

import (
	"context"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestGenerateOptionsStrategy_RetriesAfterEmptyResponse(t *testing.T) {
	t.Parallel()

	provider := &stubOptionsCompletionProvider{responses: []*llm.CompletionResponse{
		{Content: ""},
		{Content: validOptionsStrategyJSON},
	}}

	got, err := GenerateOptionsStrategy(context.Background(), discovery.GeneratorConfig{
		Provider:   provider,
		MaxRetries: 1,
	}, OptionsScoredCandidate{OptionsScreenResult: OptionsScreenResult{Ticker: "NVDA"}}, nil)
	if err != nil {
		t.Fatalf("GenerateOptionsStrategy() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("GenerateOptionsStrategy() = nil, want config")
	}
	if string(got.StrategyType) != "bull_put_spread" {
		t.Fatalf("GenerateOptionsStrategy().StrategyType = %q, want %q", got.StrategyType, "bull_put_spread")
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(provider.requests))
	}
	msgs := provider.requests[1].Messages
	if len(msgs) < 4 {
		t.Fatalf("retry messages = %d, want at least 4", len(msgs))
	}
	if !strings.Contains(msgs[len(msgs)-1].Content, "rules: empty JSON response") {
		t.Fatalf("retry prompt missing empty-response error: %q", msgs[len(msgs)-1].Content)
	}
}

func TestGenerateOptionsStrategy_ReturnsErrorAfterRepeatedEmptyResponses(t *testing.T) {
	t.Parallel()

	provider := &stubOptionsCompletionProvider{responses: []*llm.CompletionResponse{
		{Content: ""},
		{Content: ""},
	}}

	got, err := GenerateOptionsStrategy(context.Background(), discovery.GeneratorConfig{
		Provider:   provider,
		MaxRetries: 1,
	}, OptionsScoredCandidate{OptionsScreenResult: OptionsScreenResult{Ticker: "GOOG"}}, nil)
	if err == nil {
		t.Fatal("GenerateOptionsStrategy() error = nil, want non-nil")
	}
	if got != nil {
		t.Fatalf("GenerateOptionsStrategy() = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), "rules: empty JSON response") {
		t.Fatalf("GenerateOptionsStrategy() error = %q, want empty-response error", err.Error())
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
}

func TestOptionsGeneratorSystemPrompt_UsesOpConditionField(t *testing.T) {
	t.Parallel()

	if count := strings.Count(optionsGeneratorSystemPrompt, `{"field": "<field_name>", "op": "<op>", "value": <number>}`); count != 2 {
		t.Fatalf("condition field schema appears %d times, want 2", count)
	}
}

type stubOptionsCompletionProvider struct {
	responses []*llm.CompletionResponse
	requests  []llm.CompletionRequest
	calls     int
}

func (s *stubOptionsCompletionProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.requests = append(s.requests, request)
	idx := s.calls
	s.calls++
	if idx >= len(s.responses) {
		return s.responses[len(s.responses)-1], nil
	}
	return s.responses[idx], nil
}

const validOptionsStrategyJSON = `{"version":1,"strategy_type":"bull_put_spread","underlying":"NVDA","entry":{"operator":"AND","conditions":[{"field":"iv_rank","op":"gt","value":55}]},"exit":{"operator":"OR","conditions":[{"field":"pnl_pct","op":"gte","value":50}]},"leg_selection":{"short_put":{"option_type":"put","delta_target":0.25,"dte_min":25,"dte_max":45,"side":"sell","position_intent":"sell_to_open","ratio":1},"long_put":{"option_type":"put","delta_target":0.10,"dte_min":25,"dte_max":45,"side":"buy","position_intent":"buy_to_open","ratio":1}},"position_sizing":{"method":"max_risk","max_risk_usd":1000},"management":{"close_at_profit_pct":50,"close_at_dte":7,"roll_at_dte":0,"stop_loss_pct":100}}`
