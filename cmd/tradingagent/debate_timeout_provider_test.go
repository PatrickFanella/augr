package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type timeoutProviderCall struct {
	model    string
	deadline bool
}

type timeoutProviderStub struct {
	calls []timeoutProviderCall
	fn    func(int, llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (s *timeoutProviderStub) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	_, hasDeadline := ctx.Deadline()
	s.calls = append(s.calls, timeoutProviderCall{model: req.Model, deadline: hasDeadline})
	if s.fn != nil {
		return s.fn(len(s.calls), req)
	}
	return &llm.CompletionResponse{Content: "ok", Model: req.Model}, nil
}

func TestDebateTimeoutFallbackProvider_RetriesTimedOutCallWithQuickModel(t *testing.T) {
	t.Parallel()

	provider := &timeoutProviderStub{
		fn: func(call int, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
			if call == 1 {
				return nil, context.DeadlineExceeded
			}
			return &llm.CompletionResponse{Content: "ok", Model: req.Model}, nil
		},
	}
	wrapped := newDebateTimeoutFallbackProvider(provider, "gpt-5-mini", 30*time.Second, slogDiscardLogger())

	resp, err := wrapped.Complete(context.Background(), llm.CompletionRequest{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp == nil || resp.Model != "gpt-5-mini" {
		t.Fatalf("response model = %v, want gpt-5-mini", resp)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(provider.calls))
	}
	if provider.calls[0].model != "gpt-5" || provider.calls[1].model != "gpt-5-mini" {
		t.Fatalf("models = %+v, want [gpt-5 gpt-5-mini]", provider.calls)
	}
	if !provider.calls[0].deadline || !provider.calls[1].deadline {
		t.Fatalf("expected deadlines on both calls, got %+v", provider.calls)
	}
}

func TestDebateTimeoutFallbackProvider_DoesNotRetryNonTimeoutErrors(t *testing.T) {
	t.Parallel()

	provider := &timeoutProviderStub{fn: func(int, llm.CompletionRequest) (*llm.CompletionResponse, error) {
		return nil, errors.New("boom")
	}}
	wrapped := newDebateTimeoutFallbackProvider(provider, "gpt-5-mini", 30*time.Second, slogDiscardLogger())

	_, err := wrapped.Complete(context.Background(), llm.CompletionRequest{Model: "gpt-5"})
	if err == nil {
		t.Fatal("Complete() error = nil, want error")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(provider.calls))
	}
}

func TestBuildRunnerDefinition_UsesEnvBoundedDebateTimeoutFallback(t *testing.T) {
	t.Parallel()

	resolved := agent.ResolvedConfig{
		LLMConfig: agent.ResolvedLLMConfig{
			QuickThinkModel: "gpt-5-mini",
			DeepThinkModel:  "gpt-5",
		},
		PipelineConfig: agent.ResolvedPipelineConfig{
			DebateTimeoutSeconds: 10,
		},
	}

	if got := effectiveDebateCallTimeout(30*time.Second, resolved); got != 10*time.Second {
		t.Fatalf("effectiveDebateCallTimeout() = %s, want %s", got, 10*time.Second)
	}

	definition, err := buildRunnerDefinition(captureProvider{}, "openai", resolved, 30*time.Second, nil, slogDiscardLogger())
	if err != nil {
		t.Fatalf("buildRunnerDefinition() error = %v", err)
	}

	if _, err := definition.Research.Debaters[0].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"}); err != nil {
		t.Fatalf("debate call error = %v", err)
	}
	if _, err := definition.Risk.Judge.JudgeRisk(context.Background(), agent.RiskJudgeInput{Ticker: "AAPL", TradingPlan: agent.TradingPlan{Ticker: "AAPL"}}); err != nil {
		t.Fatalf("risk judge call error = %v", err)
	}
}

func TestEffectiveDebateCallTimeout(t *testing.T) {
	t.Run("uses llm timeout default when no overrides", func(t *testing.T) {
		t.Setenv("LLM_DEBATE_TIMEOUT", "")

		resolved := agent.ResolvedConfig{}
		if got := effectiveDebateCallTimeout(30*time.Second, resolved); got != 30*time.Second {
			t.Fatalf("effectiveDebateCallTimeout() = %s, want %s", got, 30*time.Second)
		}
	})

	t.Run("uses strategy override when present", func(t *testing.T) {
		t.Setenv("LLM_DEBATE_TIMEOUT", "45s")

		resolved := agent.ResolvedConfig{
			PipelineConfig: agent.ResolvedPipelineConfig{DebateTimeoutSeconds: 10},
		}
		if got := effectiveDebateCallTimeout(30*time.Second, resolved); got != 10*time.Second {
			t.Fatalf("effectiveDebateCallTimeout() = %s, want %s", got, 10*time.Second)
		}
	})

	t.Run("uses env override when strategy override absent", func(t *testing.T) {
		t.Setenv("LLM_DEBATE_TIMEOUT", "45s")

		resolved := agent.ResolvedConfig{}
		if got := effectiveDebateCallTimeout(30*time.Second, resolved); got != 45*time.Second {
			t.Fatalf("effectiveDebateCallTimeout() = %s, want %s", got, 45*time.Second)
		}
	})
}
