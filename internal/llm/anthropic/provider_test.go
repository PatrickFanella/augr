package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	anthropicprovider "github.com/PatrickFanella/get-rich-quick/internal/llm/anthropic"
)

func TestCompleteUsesConfiguredModelAndTracksUsage(t *testing.T) {
	t.Parallel()

	requestBodyChannel := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("request path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Fatalf("X-Api-Key header = %q, want %q", got, "test-key")
		}

		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodyChannel <- requestBody

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test123",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Hello back"}],
			"model": "claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage": {
				"input_tokens": 11,
				"output_tokens": 7,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens": 0
			}
		}`))
	}))
	defer server.Close()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   anthropicprovider.ModelClaudeSonnet,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	response, err := provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are concise."},
			{Role: "user", Content: "Say hello"},
		},
		Temperature: 0.4,
		MaxTokens:   32,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if response.Content != "Hello back" {
		t.Fatalf("response.Content = %q, want %q", response.Content, "Hello back")
	}
	if response.Model != "claude-sonnet-4-6" {
		t.Fatalf("response.Model = %q, want %q", response.Model, "claude-sonnet-4-6")
	}
	if response.Usage.PromptTokens != 11 {
		t.Fatalf("response.Usage.PromptTokens = %d, want %d", response.Usage.PromptTokens, 11)
	}
	if response.Usage.CompletionTokens != 7 {
		t.Fatalf("response.Usage.CompletionTokens = %d, want %d", response.Usage.CompletionTokens, 7)
	}
	if response.LatencyMS < 0 {
		t.Fatalf("response.LatencyMS = %d, want >= 0", response.LatencyMS)
	}

	requestBody := <-requestBodyChannel
	if got := requestBody["model"]; got != anthropicprovider.ModelClaudeSonnet {
		t.Fatalf("request model = %v, want %q", got, anthropicprovider.ModelClaudeSonnet)
	}
	if got := requestBody["temperature"]; got != 0.4 {
		t.Fatalf("request temperature = %v, want %v", got, 0.4)
	}
	if got := requestBody["max_tokens"]; got != float64(32) {
		t.Fatalf("request max_tokens = %v, want %d", got, 32)
	}

	messages, ok := requestBody["messages"].([]any)
	if !ok {
		t.Fatalf("request messages type = %T, want []any", requestBody["messages"])
	}
	if len(messages) != 1 {
		t.Fatalf("request message count = %d, want %d (system message is separate)", len(messages), 1)
	}

	system, ok := requestBody["system"].([]any)
	if !ok {
		t.Fatalf("request system type = %T, want []any", requestBody["system"])
	}
	if len(system) != 1 {
		t.Fatalf("request system block count = %d, want %d", len(system), 1)
	}
}

func TestCompleteUsesRequestModelOverride(t *testing.T) {
	t.Parallel()

	requestBodyChannel := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodyChannel <- requestBody

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test456",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "ok"}],
			"model": "claude-opus-4-6",
			"stop_reason": "end_turn",
			"usage": {
				"input_tokens": 5,
				"output_tokens": 2,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens": 0
			}
		}`))
	}))
	defer server.Close()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey:  "test-key",
		BaseURL: strings.TrimSuffix(server.URL, "/"),
		Model:   anthropicprovider.ModelClaudeSonnet,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	response, err := provider.Complete(context.Background(), llm.CompletionRequest{
		Model: anthropicprovider.ModelClaudeOpus,
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if response.Content != "ok" {
		t.Fatalf("response.Content = %q, want %q", response.Content, "ok")
	}

	requestBody := <-requestBodyChannel
	if got := requestBody["model"]; got != anthropicprovider.ModelClaudeOpus {
		t.Fatalf("request model = %v, want %q", got, anthropicprovider.ModelClaudeOpus)
	}
}

func TestCompleteUsesDefaultMaxTokensWhenNotSet(t *testing.T) {
	t.Parallel()

	requestBodyChannel := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodyChannel <- requestBody

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test789",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "response"}],
			"model": "claude-haiku-4-5",
			"stop_reason": "end_turn",
			"usage": {
				"input_tokens": 5,
				"output_tokens": 1,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens": 0
			}
		}`))
	}))
	defer server.Close()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   anthropicprovider.ModelClaudeHaiku,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	requestBody := <-requestBodyChannel
	if got, ok := requestBody["max_tokens"].(float64); !ok || got <= 0 {
		t.Fatalf("request max_tokens = %v, want positive default", requestBody["max_tokens"])
	}
}

func TestCompleteWrapsSDKErrorsWithoutRetries(t *testing.T) {
	t.Parallel()

	var requestCounter atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCounter.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"server error"}}`))
	}))
	defer server.Close()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   anthropicprovider.ModelClaudeSonnet,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "anthropic: complete request") {
		t.Fatalf("Complete() error = %q, want wrapped context", err)
	}
	if requestCounter.Load() != 1 {
		t.Fatalf("request count = %d, want %d (retries disabled)", requestCounter.Load(), 1)
	}
}

func TestCompleteRejectsUnsupportedMessageRole(t *testing.T) {
	t.Parallel()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey: "test-key",
		Model:  anthropicprovider.ModelClaudeSonnet,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "tool", Content: "not supported"},
		},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `anthropic: unsupported message role "tool"`) {
		t.Fatalf("Complete() error = %q, want unsupported role message", err)
	}
}

func TestCompleteRejectsUnsupportedResponseFormat(t *testing.T) {
	t.Parallel()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey: "test-key",
		Model:  anthropicprovider.ModelClaudeSonnet,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "return json"},
		},
		ResponseFormat: &llm.ResponseFormat{
			Type: llm.ResponseFormatJSONObject,
		},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `anthropic: unsupported response format type`) {
		t.Fatalf("Complete() error = %q, want unsupported format message", err)
	}
}

func TestCompleteRejectsMessagesWithOnlySystemRole(t *testing.T) {
	t.Parallel()

	provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey: "test-key",
		Model:  anthropicprovider.ModelClaudeSonnet,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are an assistant."},
		},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "anthropic: at least one user or assistant message is required") {
		t.Fatalf("Complete() error = %q, want user/assistant message requirement error", err)
	}
}

func TestNewProviderRejectsEmptyAPIKey(t *testing.T) {
	t.Parallel()

	_, err := anthropicprovider.NewProvider(anthropicprovider.Config{
		APIKey: "",
		Model:  anthropicprovider.ModelClaudeSonnet,
	})
	if err == nil {
		t.Fatal("NewProvider() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "anthropic: api key is required") {
		t.Fatalf("NewProvider() error = %q, want api key error", err)
	}
}

func TestDefaultModelsByTier(t *testing.T) {
	t.Parallel()

	models := anthropicprovider.DefaultModelsByTier()
	if models[llm.ModelTierDeepThink] != anthropicprovider.ModelClaudeSonnet {
		t.Fatalf("deep think model = %q, want %q", models[llm.ModelTierDeepThink], anthropicprovider.ModelClaudeSonnet)
	}
	if models[llm.ModelTierQuickThink] != anthropicprovider.ModelClaudeHaiku {
		t.Fatalf("quick think model = %q, want %q", models[llm.ModelTierQuickThink], anthropicprovider.ModelClaudeHaiku)
	}

	models[llm.ModelTierQuickThink] = "mutated"
	if anthropicprovider.DefaultModelsByTier()[llm.ModelTierQuickThink] != anthropicprovider.ModelClaudeHaiku {
		t.Fatal("DefaultModelsByTier() returned a shared map")
	}
}
