package llm_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type labelRecordingMetrics struct{ labels []string }

func (m *labelRecordingMetrics) RecordLLMRetry(provider string) {
	m.labels = append(m.labels, provider)
}

func TestComposerBuildProviderForSelection_ResolvesModelOverride(t *testing.T) {
	t.Parallel()

	var captured llm.OpenAIProviderConfig
	composer := llm.NewComposer(llm.RuntimeProviderFactories{
		OpenAI: func(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
			captured = cfg
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
				return &llm.CompletionResponse{Content: "ok"}, nil
			}), nil
		},
	})

	cfg := config.LLMConfig{Providers: config.LLMProviderConfigs{OpenAI: config.LLMProviderConfig{APIKey: "openai-key", BaseURL: "https://example.invalid", Model: "configured-model"}}}

	provider, err := composer.BuildProviderForSelection(cfg, "  openai  ", "explicit-model", discardLogger())
	if err != nil {
		t.Fatalf("BuildProviderForSelection() error = %v", err)
	}
	if provider == nil {
		t.Fatal("BuildProviderForSelection() provider = nil")
	}
	if captured.Model != "explicit-model" {
		t.Fatalf("captured.Model = %q, want explicit-model", captured.Model)
	}

	_, err = composer.BuildProviderForSelection(cfg, "openai", "", discardLogger())
	if err != nil {
		t.Fatalf("BuildProviderForSelection() error = %v", err)
	}
	if captured.Model != "configured-model" {
		t.Fatalf("captured.Model = %q, want configured-model", captured.Model)
	}
}

func TestComposerBuildProviderForSelection_SupportsAliases(t *testing.T) {
	t.Parallel()

	var selected atomic.Int32
	composer := llm.NewComposer(llm.RuntimeProviderFactories{
		OpenAI: func(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
			selected.Store(1)
			if cfg.Model == "" {
				t.Fatal("openai factory received blank model")
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), nil
		},
		Anthropic: func(cfg llm.AnthropicProviderConfig) (llm.Provider, error) {
			selected.Store(2)
			if cfg.Model == "" {
				t.Fatal("anthropic factory received blank model")
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), nil
		},
		Google: func(cfg llm.GoogleProviderConfig) (llm.Provider, error) {
			selected.Store(3)
			if cfg.Model == "" {
				t.Fatal("google factory received blank model")
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), nil
		},
		OpenRouter: func(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
			selected.Store(4)
			if cfg.Model == "" {
				t.Fatal("openrouter factory received blank model")
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), nil
		},
		XAI: func(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
			selected.Store(5)
			if cfg.Model == "" {
				t.Fatal("xai factory received blank model")
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), nil
		},
		Ollama: func(cfg llm.OllamaProviderConfig) (llm.Provider, error) {
			selected.Store(6)
			if cfg.Model == "" {
				t.Fatal("ollama factory received blank model")
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), nil
		},
	})

	cases := []struct {
		name         string
		providerName string
		configure    func()
		wantSelected int32
	}{
		{name: "openai", providerName: "openai", configure: func() {}, wantSelected: 1},
		{name: "anthropic", providerName: "anthropic", configure: func() {}, wantSelected: 2},
		{name: "google", providerName: "google", configure: func() {}, wantSelected: 3},
		{name: "openrouter", providerName: "openrouter", configure: func() {}, wantSelected: 4},
		{name: "xai", providerName: "xai", configure: func() {}, wantSelected: 5},
		{name: "ollama", providerName: "ollama", configure: func() {}, wantSelected: 6},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.LLMConfig{
				Providers: config.LLMProviderConfigs{
					OpenAI:     config.LLMProviderConfig{APIKey: "k", Model: "gpt-5-mini"},
					Anthropic:  config.LLMProviderConfig{APIKey: "k", Model: "claude-haiku-4-5"},
					Google:     config.LLMProviderConfig{APIKey: "k", Model: "gemini-3.1-flash"},
					OpenRouter: config.LLMProviderConfig{APIKey: "k", Model: "openrouter-model"},
					XAI:        config.LLMProviderConfig{APIKey: "k", Model: "grok-3-mini"},
					Ollama:     config.OllamaConfig{BaseURL: "http://localhost:11434/v1", APIKey: "k", Model: "llama3.2"},
				},
			}

			selected.Store(0)
			provider, err := composer.BuildProviderForSelection(cfg, tc.providerName, "", discardLogger())
			if err != nil {
				t.Fatalf("BuildProviderForSelection() error = %v", err)
			}
			if provider == nil {
				t.Fatal("BuildProviderForSelection() provider = nil")
			}
			if got := selected.Load(); got != tc.wantSelected {
				t.Fatalf("selected factory = %d, want %d", got, tc.wantSelected)
			}
		})
	}
}

func TestComposerWrapProviderChain_FallbackAndCacheToggle(t *testing.T) {
	fallbackCalls := atomic.Int32{}
	composer := llm.NewComposer(llm.RuntimeProviderFactories{
		Anthropic: func(cfg llm.AnthropicProviderConfig) (llm.Provider, error) {
			if cfg.Model != "claude-sonnet-4-6" {
				t.Fatalf("fallback model = %q, want claude-sonnet-4-6", cfg.Model)
			}
			return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
				fallbackCalls.Add(1)
				return &llm.CompletionResponse{Content: "fallback"}, nil
			}), nil
		},
	})

	primary := &trackingProvider{err: errors.New("primary failed")}
	cfg := config.LLMConfig{DefaultProvider: "openai", FallbackProvider: "anthropic", FallbackModel: "claude-sonnet-4-6", ThrottleConcurrency: 1}

	t.Setenv("LLM_CACHE_ENABLED", "false")
	chain := composer.WrapProviderChain(primary, cfg, nil, discardLogger(), nil)
	resp, err := chain.Complete(context.Background(), llm.CompletionRequest{Model: "m", Messages: []llm.Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp == nil || !resp.UsedFallback || resp.Content != "fallback" {
		t.Fatalf("fallback response = %#v, want used fallback content", resp)
	}
	if primary.calls.Load() != 1 {
		t.Fatalf("primary calls = %d, want 1", primary.calls.Load())
	}
	if fallbackCalls.Load() != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls.Load())
	}

	t.Run("cache enabled and disabled", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			cacheEnv  string
			wantCalls int32
		}{
			{name: "enabled", cacheEnv: "true", wantCalls: 1},
			{name: "disabled", cacheEnv: "false", wantCalls: 2},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var calls atomic.Int32
				provider := llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
					calls.Add(1)
					return &llm.CompletionResponse{Content: "cached"}, nil
				})
				t.Setenv("LLM_CACHE_ENABLED", tc.cacheEnv)
				chain := composer.WrapProviderChain(provider, config.LLMConfig{ThrottleConcurrency: 1}, nil, discardLogger(), nil)
				req := llm.CompletionRequest{Model: "m", Messages: []llm.Message{{Role: "user", Content: "cache me"}}}
				if _, err := chain.Complete(context.Background(), req); err != nil {
					t.Fatalf("first Complete() error = %v", err)
				}
				if _, err := chain.Complete(context.Background(), req); err != nil {
					t.Fatalf("second Complete() error = %v", err)
				}
				if got := calls.Load(); got != tc.wantCalls {
					t.Fatalf("provider calls = %d, want %d", got, tc.wantCalls)
				}
			})
		}
	})
}

func TestComposerWrapProviderChain_RetryMetricsBinding(t *testing.T) {
	t.Parallel()

	metrics := &labelRecordingMetrics{}
	mock := newMockProvider(
		[]*llm.CompletionResponse{nil, {Content: "retried"}},
		[]error{&httpError{code: 429, msg: "rate limited"}, nil},
	)

	composer := llm.NewComposer(llm.RuntimeProviderFactories{})
	chain := composer.WrapProviderChain(mock, config.LLMConfig{DefaultProvider: "openai", RetryMaxAttempts: 2, ThrottleConcurrency: 1}, metrics, discardLogger(), nil)
	resp, err := chain.Complete(context.Background(), llm.CompletionRequest{Model: "m", Messages: []llm.Message{{Role: "user", Content: "retry"}}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp == nil || resp.Content != "retried" {
		t.Fatalf("response = %#v, want retried", resp)
	}
	if got := mock.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	if len(metrics.labels) != 1 || metrics.labels[0] != "configured_primary:openai" {
		t.Fatalf("retry metrics labels = %#v, want [configured_primary:openai]", metrics.labels)
	}
}

func TestADR002TwoTierDefaults_AreRegistered(t *testing.T) {
	t.Parallel()

	reg := llm.NewRegistry()
	if err := reg.Register("openai", llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), map[llm.ModelTier]string{llm.ModelTierDeepThink: "gpt-5.2", llm.ModelTierQuickThink: "gpt-5-mini"}); err != nil {
		t.Fatalf("Register(openai) error = %v", err)
	}
	if err := reg.Register("anthropic", llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), map[llm.ModelTier]string{llm.ModelTierDeepThink: "claude-sonnet-4-6", llm.ModelTierQuickThink: "claude-haiku-4-5"}); err != nil {
		t.Fatalf("Register(anthropic) error = %v", err)
	}
	if err := reg.Register("google", llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), map[llm.ModelTier]string{llm.ModelTierDeepThink: "gemini-3.1-pro", llm.ModelTierQuickThink: "gemini-3.1-flash"}); err != nil {
		t.Fatalf("Register(google) error = %v", err)
	}
	if err := reg.Register("ollama", llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), map[llm.ModelTier]string{llm.ModelTierDeepThink: "llama3.2", llm.ModelTierQuickThink: "llama3.2"}); err != nil {
		t.Fatalf("Register(ollama) error = %v", err)
	}

	checks := []struct {
		name     string
		provider string
		tier     llm.ModelTier
		want     string
	}{
		{name: "openai deep", provider: "openai", tier: llm.ModelTierDeepThink, want: "gpt-5.2"},
		{name: "openai quick", provider: "openai", tier: llm.ModelTierQuickThink, want: "gpt-5-mini"},
		{name: "anthropic deep", provider: "anthropic", tier: llm.ModelTierDeepThink, want: "claude-sonnet-4-6"},
		{name: "anthropic quick", provider: "anthropic", tier: llm.ModelTierQuickThink, want: "claude-haiku-4-5"},
		{name: "google deep", provider: "google", tier: llm.ModelTierDeepThink, want: "gemini-3.1-pro"},
		{name: "google quick", provider: "google", tier: llm.ModelTierQuickThink, want: "gemini-3.1-flash"},
		{name: "ollama deep", provider: "ollama", tier: llm.ModelTierDeepThink, want: "llama3.2"},
		{name: "ollama quick", provider: "ollama", tier: llm.ModelTierQuickThink, want: "llama3.2"},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			_, got, err := reg.Resolve(tc.provider, tc.tier)
			if err != nil {
				t.Fatalf("Resolve(%s, %s) error = %v", tc.provider, tc.tier, err)
			}
			if got != tc.want {
				t.Fatalf("Resolve(%s, %s) model = %q, want %q", tc.provider, tc.tier, got, tc.want)
			}
		})
	}
}
