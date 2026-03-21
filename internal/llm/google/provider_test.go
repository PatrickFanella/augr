package google_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	googleprovider "github.com/PatrickFanella/get-rich-quick/internal/llm/google"
)

func TestCompleteUsesConfiguredModelAndTracksUsage(t *testing.T) {
	t.Parallel()

	requestBodyChannel := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v1beta/models/gemini-3.1-flash:generateContent" {
			t.Fatalf("request path = %s, want /v1beta/models/gemini-3.1-flash:generateContent", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-key" {
			t.Fatalf("x-goog-api-key header = %q, want %q", got, "test-key")
		}

		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodyChannel <- requestBody

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[
				{
					"content":{"parts":[{"text":"Hello back"}],"role":"model"},
					"finishReason":"STOP",
					"index":0
				}
			],
			"modelVersion":"gemini-3.1-flash",
			"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":7,"totalTokenCount":18}
		}`))
	}))
	defer server.Close()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   googleprovider.ModelGemini31Flash,
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
	if response.Model != "gemini-3.1-flash" {
		t.Fatalf("response.Model = %q, want %q", response.Model, "gemini-3.1-flash")
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
	generationConfig, ok := requestBody["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig type = %T, want map[string]any", requestBody["generationConfig"])
	}
	if got := generationConfig["temperature"]; got != 0.4 {
		t.Fatalf("generationConfig.temperature = %v, want %v", got, 0.4)
	}
	if got := generationConfig["maxOutputTokens"]; got != float64(32) {
		t.Fatalf("generationConfig.maxOutputTokens = %v, want %d", got, 32)
	}

	systemInstruction, ok := requestBody["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("systemInstruction type = %T, want map[string]any", requestBody["systemInstruction"])
	}
	parts, ok := systemInstruction["parts"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("systemInstruction.parts = %v, want one part", systemInstruction["parts"])
	}
}

func TestCompleteSupportsRequestOverridesAndJSONMode(t *testing.T) {
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
			"candidates":[{"content":{"parts":[{"text":"{\"answer\":\"ok\"}"}],"role":"model"},"index":0}],
			"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":9}
		}`))
	}))
	defer server.Close()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey:  "test-key",
		BaseURL: strings.TrimSuffix(server.URL, "/"),
		Model:   googleprovider.ModelGemini31Flash,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	response, err := provider.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemini-3.1-pro",
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
	if response.Model != "gemini-3.1-pro" {
		t.Fatalf("response.Model = %q, want request model fallback", response.Model)
	}

	requestBody := <-requestBodyChannel
	generationConfig, ok := requestBody["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig type = %T, want map[string]any", requestBody["generationConfig"])
	}
	if got := generationConfig["responseMimeType"]; got != "application/json" {
		t.Fatalf("generationConfig.responseMimeType = %v, want %q", got, "application/json")
	}
}

func TestCompleteWrapsSDKErrorsWithoutRetries(t *testing.T) {
	t.Parallel()

	var requestCounter atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCounter.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"backend unavailable"}}`))
	}))
	defer server.Close()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   googleprovider.ModelGemini31Flash,
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
	if !strings.Contains(err.Error(), "google: complete request") {
		t.Fatalf("Complete() error = %q, want wrapped context", err)
	}
	if requestCounter.Load() != 1 {
		t.Fatalf("request count = %d, want %d", requestCounter.Load(), 1)
	}
}

func TestCompleteRejectsUnsupportedMessageRole(t *testing.T) {
	t.Parallel()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey: "test-key",
		Model:  googleprovider.ModelGemini31Flash,
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
	if !strings.Contains(err.Error(), `google: unsupported message role "tool"`) {
		t.Fatalf("Complete() error = %q, want unsupported role message", err)
	}
}

func TestCompleteRejectsUnsupportedResponseFormat(t *testing.T) {
	t.Parallel()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey: "test-key",
		Model:  googleprovider.ModelGemini31Flash,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "return xml"},
		},
		ResponseFormat: &llm.ResponseFormat{
			Type: llm.ResponseFormatType("xml"),
		},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `google: unsupported response format type "xml"`) {
		t.Fatalf("Complete() error = %q, want unsupported format message", err)
	}
}

func TestCompleteRejectsInvalidJSONSchema(t *testing.T) {
	t.Parallel()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey: "test-key",
		Model:  googleprovider.ModelGemini31Flash,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	_, err = provider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "return json"},
		},
		ResponseFormat: &llm.ResponseFormat{
			Type:   llm.ResponseFormatJSONObject,
			Schema: json.RawMessage(`{"type":"object"`),
		},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "google: parse response format schema") {
		t.Fatalf("Complete() error = %q, want schema parse error", err)
	}
}

func TestCompleteRejectsMessagesWithOnlySystemRole(t *testing.T) {
	t.Parallel()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey: "test-key",
		Model:  googleprovider.ModelGemini31Flash,
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
	if !strings.Contains(err.Error(), "google: at least one user or assistant message is required") {
		t.Fatalf("Complete() error = %q, want user/assistant message requirement error", err)
	}
}

func TestNewProviderRejectsEmptyAPIKey(t *testing.T) {
	t.Parallel()

	_, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey: "",
		Model:  googleprovider.ModelGemini31Flash,
	})
	if err == nil {
		t.Fatal("NewProvider() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "google: api key is required") {
		t.Fatalf("NewProvider() error = %q, want api key error", err)
	}
}

func TestCompleteErrorsOnEmptyCandidates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer server.Close()

	provider, err := googleprovider.NewProvider(googleprovider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   googleprovider.ModelGemini31Flash,
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
	if !strings.Contains(err.Error(), "google: completion response did not include any candidates") {
		t.Fatalf("Complete() error = %q, want empty candidates error", err)
	}
}

func TestDefaultModelsByTier(t *testing.T) {
	t.Parallel()

	models := googleprovider.DefaultModelsByTier()
	if models[llm.ModelTierDeepThink] != googleprovider.ModelGemini31Pro {
		t.Fatalf("deep think model = %q, want %q", models[llm.ModelTierDeepThink], googleprovider.ModelGemini31Pro)
	}
	if models[llm.ModelTierQuickThink] != googleprovider.ModelGemini31Flash {
		t.Fatalf("quick think model = %q, want %q", models[llm.ModelTierQuickThink], googleprovider.ModelGemini31Flash)
	}

	models[llm.ModelTierQuickThink] = "mutated"
	if googleprovider.DefaultModelsByTier()[llm.ModelTierQuickThink] != googleprovider.ModelGemini31Flash {
		t.Fatal("DefaultModelsByTier() returned a shared map")
	}
}
