package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	openaiprovider "github.com/PatrickFanella/get-rich-quick/internal/llm/openai"
)

func TestCompleteUsesConfiguredModelAndTracksUsage(t *testing.T) {
	t.Parallel()

	requestBodyCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("request path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization header = %q, want %q", got, "Bearer test-key")
		}

		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodyCh <- requestBody

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-123",
			"object":"chat.completion",
			"created":1730000000,
			"model":"gpt-5-mini",
			"choices":[
				{
					"index":0,
					"finish_reason":"stop",
					"logprobs":null,
					"message":{"role":"assistant","content":"Hello back","refusal":""}
				}
			],
			"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
		}`))
	}))
	defer server.Close()

	provider, err := openaiprovider.NewProvider(openaiprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   openaiprovider.ModelGPT5Mini,
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
	if response.Model != "gpt-5-mini" {
		t.Fatalf("response.Model = %q, want %q", response.Model, "gpt-5-mini")
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

	requestBody := <-requestBodyCh
	if got := requestBody["model"]; got != openaiprovider.ModelGPT5Mini {
		t.Fatalf("request model = %v, want %q", got, openaiprovider.ModelGPT5Mini)
	}
	if got := requestBody["temperature"]; got != 0.4 {
		t.Fatalf("request temperature = %v, want %v", got, 0.4)
	}
	if got := requestBody["max_completion_tokens"]; got != float64(32) {
		t.Fatalf("request max_completion_tokens = %v, want %d", got, 32)
	}

	messages, ok := requestBody["messages"].([]any)
	if !ok {
		t.Fatalf("request messages type = %T, want []any", requestBody["messages"])
	}
	if len(messages) != 2 {
		t.Fatalf("request message count = %d, want %d", len(messages), 2)
	}

	firstMessage, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("first message type = %T, want map[string]any", messages[0])
	}
	if firstMessage["role"] != "system" || firstMessage["content"] != "You are concise." {
		t.Fatalf("first message = %#v, want system prompt", firstMessage)
	}
}

func TestCompleteSupportsRequestOverridesAndJSONMode(t *testing.T) {
	t.Parallel()

	requestBodyCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodyCh <- requestBody

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-456",
			"object":"chat.completion",
			"created":1730000001,
			"model":"gpt-5.4",
			"choices":[
				{
					"index":0,
					"finish_reason":"stop",
					"logprobs":null,
					"message":{"role":"assistant","content":"{\"answer\":\"ok\"}","refusal":""}
				}
			],
			"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29}
		}`))
	}))
	defer server.Close()

	provider, err := openaiprovider.NewProvider(openaiprovider.Config{
		APIKey:  "test-key",
		BaseURL: strings.TrimSuffix(server.URL, "/"),
		Model:   openaiprovider.ModelGPT5Mini,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	response, err := provider.Complete(context.Background(), llm.CompletionRequest{
		Model: "gpt-5.4",
		Messages: []llm.Message{
			{Role: "user", Content: "Return JSON"},
		},
		ResponseFormat: &llm.ResponseFormat{
			Type: llm.ResponseFormatJSONObject,
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if response.Content != `{"answer":"ok"}` {
		t.Fatalf("response.Content = %q, want JSON payload", response.Content)
	}

	requestBody := <-requestBodyCh
	if got := requestBody["model"]; got != "gpt-5.4" {
		t.Fatalf("request model = %v, want %q", got, "gpt-5.4")
	}

	responseFormat, ok := requestBody["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format type = %T, want map[string]any", requestBody["response_format"])
	}
	if responseFormat["type"] != "json_object" {
		t.Fatalf("response_format.type = %v, want %q", responseFormat["type"], "json_object")
	}
}

func TestCompleteWrapsSDKErrorsWithoutRetries(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"backend unavailable","type":"server_error"}}`))
	}))
	defer server.Close()

	provider, err := openaiprovider.NewProvider(openaiprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   openaiprovider.ModelGPT5Mini,
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
	if !strings.Contains(err.Error(), "openai: complete request") {
		t.Fatalf("Complete() error = %q, want wrapped context", err)
	}
	if requestCount.Load() != 1 {
		t.Fatalf("request count = %d, want %d (retries disabled)", requestCount.Load(), 1)
	}
}

func TestCompleteRejectsUnsupportedMessageRole(t *testing.T) {
	t.Parallel()

	provider, err := openaiprovider.NewProvider(openaiprovider.Config{
		APIKey: "test-key",
		Model:  openaiprovider.ModelGPT5Mini,
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
	if !strings.Contains(err.Error(), `openai: unsupported message role "tool"`) {
		t.Fatalf("Complete() error = %q, want unsupported role message", err)
	}
}

func TestDefaultModelsByTier(t *testing.T) {
	t.Parallel()

	models := openaiprovider.DefaultModelsByTier()
	if models[llm.ModelTierDeepThink] != openaiprovider.ModelGPT52 {
		t.Fatalf("deep think model = %q, want %q", models[llm.ModelTierDeepThink], openaiprovider.ModelGPT52)
	}
	if models[llm.ModelTierQuickThink] != openaiprovider.ModelGPT5Mini {
		t.Fatalf("quick think model = %q, want %q", models[llm.ModelTierQuickThink], openaiprovider.ModelGPT5Mini)
	}

	models[llm.ModelTierQuickThink] = "mutated"
	if openaiprovider.DefaultModelsByTier()[llm.ModelTierQuickThink] != openaiprovider.ModelGPT5Mini {
		t.Fatal("DefaultModelsByTier() returned a shared map")
	}
}
