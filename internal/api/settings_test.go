package api

import (
	"net/http"
	"testing"
	"time"
)

func TestGetSettings(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Settings = NewMemorySettingsService(SettingsBootstrap{
		LLM: llmSettingsState{
			DefaultProvider: "openai",
			DeepThinkModel:  "gpt-5.2",
			QuickThinkModel: "gpt-5-mini",
			Providers: llmProvidersState{
				OpenAI: providerState{
					APIKey:  "sk-openai-1234",
					BaseURL: "https://api.openai.com/v1",
					Model:   "gpt-5-mini",
				},
				Anthropic: providerState{
					APIKey: "sk-ant-9999",
					Model:  "claude-3-7-sonnet-latest",
				},
				Google: providerState{
					Model: "gemini-2.5-flash",
				},
				OpenRouter: providerState{
					APIKey:  "sk-or-5678",
					BaseURL: "https://openrouter.ai/api/v1",
					Model:   "openai/gpt-4.1-mini",
				},
				XAI: providerState{
					Model: "grok-3-mini",
				},
				Ollama: OllamaSettings{
					BaseURL: "http://localhost:11434",
					Model:   "llama3.2",
				},
			},
		},
		Risk: RiskSettings{
			MaxPositionSizePct:         10,
			MaxDailyLossPct:            2,
			MaxDrawdownPct:             10,
			MaxOpenPositions:           8,
			MaxTotalExposurePct:        90,
			MaxPerMarketExposurePct:    40,
			CircuitBreakerThresholdPct: 5,
			CircuitBreakerCooldownMin:  15,
		},
		Environment: "test",
		Version:     "v1.2.3",
		ConnectedBrokers: []BrokerConnection{
			{Name: "alpaca", PaperMode: true, Configured: true},
			{Name: "binance", PaperMode: false, Configured: false},
		},
		StartedAt: time.Now().Add(-2 * time.Minute),
	})

	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/settings", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := decodeJSON[SettingsResponse](t, rr)
	if body.LLM.DefaultProvider != "openai" {
		t.Fatalf("default provider = %q, want %q", body.LLM.DefaultProvider, "openai")
	}
	if !body.LLM.Providers.OpenAI.APIKeyConfigured {
		t.Fatal("expected openai key to be marked configured")
	}
	if body.LLM.Providers.OpenAI.APIKeyLast4 != "1234" {
		t.Fatalf("openai key last4 = %q, want %q", body.LLM.Providers.OpenAI.APIKeyLast4, "1234")
	}
	if body.System.Version != "v1.2.3" {
		t.Fatalf("version = %q, want %q", body.System.Version, "v1.2.3")
	}
	if len(body.System.ConnectedBrokers) != 2 {
		t.Fatalf("connected brokers = %d, want 2", len(body.System.ConnectedBrokers))
	}
	if body.System.UptimeSeconds < 60 {
		t.Fatalf("uptime_seconds = %d, want at least 60", body.System.UptimeSeconds)
	}
}

func TestUpdateSettings(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Settings = NewMemorySettingsService(SettingsBootstrap{
		LLM: llmSettingsState{
			DefaultProvider: "openai",
			DeepThinkModel:  "gpt-5.2",
			QuickThinkModel: "gpt-5-mini",
			Providers: llmProvidersState{
				OpenAI: providerState{APIKey: "sk-old-1234", Model: "gpt-5-mini"},
				Anthropic: providerState{
					Model: "claude-3-7-sonnet-latest",
				},
				Google: providerState{Model: "gemini-2.5-flash"},
				OpenRouter: providerState{
					Model: "openai/gpt-4.1-mini",
				},
				XAI: providerState{Model: "grok-3-mini"},
				Ollama: OllamaSettings{
					BaseURL: "http://localhost:11434",
					Model:   "llama3.2",
				},
			},
		},
		Risk: RiskSettings{
			MaxPositionSizePct:         10,
			MaxDailyLossPct:            2,
			MaxDrawdownPct:             10,
			MaxOpenPositions:           8,
			MaxTotalExposurePct:        90,
			MaxPerMarketExposurePct:    40,
			CircuitBreakerThresholdPct: 5,
			CircuitBreakerCooldownMin:  15,
		},
	})

	srv := newTestServerWithDeps(t, deps)

	newOpenAIKey := "sk-new-5678"
	payload := SettingsUpdateRequest{
		LLM: LLMSettingsUpdateRequest{
			DefaultProvider: "anthropic",
			DeepThinkModel:  "claude-4-sonnet",
			QuickThinkModel: "claude-3-7-sonnet-latest",
			Providers: LLMProvidersUpdateRequest{
				OpenAI: LLMProviderUpdateRequest{
					APIKey:  &newOpenAIKey,
					BaseURL: "https://api.openai.com/v1",
					Model:   "gpt-4.1-mini",
				},
				Anthropic: LLMProviderUpdateRequest{Model: "claude-4-sonnet"},
				Google:    LLMProviderUpdateRequest{Model: "gemini-2.5-pro"},
				OpenRouter: LLMProviderUpdateRequest{
					BaseURL: "https://openrouter.ai/api/v1",
					Model:   "openai/gpt-4.1",
				},
				XAI: LLMProviderUpdateRequest{
					BaseURL: "https://api.x.ai/v1",
					Model:   "grok-3-beta",
				},
				Ollama: OllamaSettings{
					BaseURL: "http://ollama.internal:11434",
					Model:   "llama3.3",
				},
			},
		},
		Risk: RiskSettings{
			MaxPositionSizePct:         12.5,
			MaxDailyLossPct:            3,
			MaxDrawdownPct:             11,
			MaxOpenPositions:           12,
			MaxTotalExposurePct:        95,
			MaxPerMarketExposurePct:    45,
			CircuitBreakerThresholdPct: 6,
			CircuitBreakerCooldownMin:  20,
		},
	}

	rr := doRequest(t, srv, http.MethodPut, "/api/v1/settings", payload)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := decodeJSON[SettingsResponse](t, rr)
	if body.LLM.DefaultProvider != "anthropic" {
		t.Fatalf("default provider = %q, want %q", body.LLM.DefaultProvider, "anthropic")
	}
	if body.LLM.Providers.OpenAI.APIKeyLast4 != "5678" {
		t.Fatalf("openai key last4 = %q, want %q", body.LLM.Providers.OpenAI.APIKeyLast4, "5678")
	}
	if body.Risk.MaxOpenPositions != 12 {
		t.Fatalf("max open positions = %d, want 12", body.Risk.MaxOpenPositions)
	}
}

func TestUpdateSettingsValidatesPayload(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	payload := SettingsUpdateRequest{
		LLM: LLMSettingsUpdateRequest{
			DefaultProvider: "",
			DeepThinkModel:  "gpt-5.2",
			QuickThinkModel: "gpt-5-mini",
			Providers: LLMProvidersUpdateRequest{
				OpenAI:     LLMProviderUpdateRequest{Model: "gpt-5-mini"},
				Anthropic:  LLMProviderUpdateRequest{Model: "claude-3-7-sonnet-latest"},
				Google:     LLMProviderUpdateRequest{Model: "gemini-2.5-flash"},
				OpenRouter: LLMProviderUpdateRequest{Model: "openai/gpt-4.1-mini"},
				XAI:        LLMProviderUpdateRequest{Model: "grok-3-mini"},
				Ollama:     OllamaSettings{Model: "llama3.2"},
			},
		},
		Risk: RiskSettings{
			MaxPositionSizePct:         10,
			MaxDailyLossPct:            2,
			MaxDrawdownPct:             10,
			MaxOpenPositions:           5,
			MaxTotalExposurePct:        80,
			MaxPerMarketExposurePct:    40,
			CircuitBreakerThresholdPct: 5,
			CircuitBreakerCooldownMin:  15,
		},
	}

	rr := doRequest(t, srv, http.MethodPut, "/api/v1/settings", payload)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
