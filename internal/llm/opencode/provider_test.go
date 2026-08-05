package opencode_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/opencode"
)

func TestProviderCompleteUsesConfiguredModelAndToolDisabledSession(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "augr" || password != "secret" {
			t.Errorf("BasicAuth() = %q, %q, %v", username, password, ok)
		}
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			var body struct {
				Permission []struct {
					Permission string `json:"permission"`
					Pattern    string `json:"pattern"`
					Action     string `json:"action"`
				} `json:"permission"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Permission) != 1 || body.Permission[0].Permission != "*" || body.Permission[0].Action != "deny" {
				t.Fatalf("session permission = %#v, want wildcard deny", body.Permission)
			}
			_, _ = w.Write([]byte(`{"id":"session-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/session/session-1/message":
			var body struct {
				Agent string `json:"agent"`
				Model struct {
					ProviderID string `json:"providerID"`
					ModelID    string `json:"modelID"`
				} `json:"model"`
				System string `json:"system"`
				Parts  []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Agent != "augr-fallback" || body.Model.ProviderID != "openai" || body.Model.ModelID != "gpt-5.4-mini" {
				t.Fatalf("message routing = %#v, want augr-fallback openai/gpt-5.4-mini", body)
			}
			if strings.Contains(body.Model.ModelID, "primary") {
				t.Fatal("provider forwarded the primary model to the fallback")
			}
			if !strings.Contains(body.System, "Do not use tools") || !strings.Contains(body.System, "exactly one valid JSON object") || !strings.Contains(body.System, `{"type":"object"}`) {
				t.Fatalf("system prompt missing isolation or format instructions: %q", body.System)
			}
			if len(body.Parts) != 1 || !strings.Contains(body.Parts[0].Text, "<user>\nquestion\n</user>") || !strings.Contains(body.Parts[0].Text, "<assistant>\nprior\n</assistant>") {
				t.Fatalf("transcript = %#v, want preserved roles", body.Parts)
			}
			_, _ = w.Write([]byte(`{"info":{"providerID":"openai","modelID":"gpt-5.4-mini","cost":0.012,"tokens":{"input":23,"output":7}},"parts":[{"type":"reasoning","text":"hidden"},{"type":"text","text":"{\"ok\":true}"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/session/session-1":
			_, _ = w.Write([]byte(`true`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := opencode.NewProvider(opencode.Config{
		BaseURL: server.URL, Username: "augr", Password: "secret", Model: "openai/gpt-5.4-mini",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	response, err := provider.Complete(ctx, llm.CompletionRequest{
		Model: "primary/model",
		Messages: []llm.Message{
			{Role: "system", Content: "Be exact."},
			{Role: "user", Content: "question"},
			{Role: "assistant", Content: "prior"},
			{Role: "user", Content: "finish"},
		},
		MaxTokens: 100,
		ResponseFormat: &llm.ResponseFormat{
			Type: llm.ResponseFormatJSONObject, Schema: json.RawMessage(`{"type":"object"}`),
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if response.Content != `{"ok":true}` || response.Model != "openai/gpt-5.4-mini" {
		t.Fatalf("response = %#v", response)
	}
	if response.Usage.PromptTokens != 23 || response.Usage.CompletionTokens != 7 || response.CostUSD != 0.012 {
		t.Fatalf("usage response = %#v", response)
	}
	mu.Lock()
	defer mu.Unlock()
	wantPaths := []string{"POST /session", "POST /session/session-1/message", "DELETE /session/session-1"}
	if strings.Join(paths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("request paths = %v, want %v", paths, wantPaths)
	}
}

func TestProviderReturnsOpenCodeErrorAndDeletesSession(t *testing.T) {
	t.Parallel()
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			_, _ = w.Write([]byte(`{"id":"failed"}`))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"info":{"error":{"name":"ProviderAuthError"}},"parts":[]}`))
		case r.Method == http.MethodDelete:
			deleted = true
			_, _ = w.Write([]byte(`true`))
		}
	}))
	defer server.Close()

	provider, err := opencode.NewProvider(opencode.Config{BaseURL: server.URL, Password: "secret", Model: "openai/gpt-5.4-mini"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "ProviderAuthError") {
		t.Fatalf("Complete() error = %v, want provider auth error", err)
	}
	if !deleted {
		t.Fatal("failed completion did not delete its session")
	}
}

func TestNewProviderValidatesConfiguration(t *testing.T) {
	t.Parallel()
	for _, cfg := range []opencode.Config{
		{Password: "x", Model: "openai/gpt-5.4-mini"},
		{BaseURL: "not-a-url", Password: "x", Model: "openai/gpt-5.4-mini"},
		{BaseURL: "http://localhost", Model: "openai/gpt-5.4-mini"},
		{BaseURL: "http://localhost", Password: "x", Model: "gpt-5.4-mini"},
	} {
		if _, err := opencode.NewProvider(cfg); err == nil {
			t.Fatalf("NewProvider(%#v) error = nil", cfg)
		}
	}
}

func TestProviderLiveOAuthIntegration(t *testing.T) {
	baseURL := os.Getenv("OPENCODE_INTEGRATION_URL")
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")
	if baseURL == "" || password == "" {
		t.Skip("set OPENCODE_INTEGRATION_URL and OPENCODE_SERVER_PASSWORD to run")
	}
	provider, err := opencode.NewProvider(opencode.Config{
		BaseURL: baseURL, Username: "opencode", Password: password, Model: "openai/gpt-5.4-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	response, err := provider.Complete(ctx, llm.CompletionRequest{
		Messages:       []llm.Message{{Role: "user", Content: `Return {"oauth":true}.`}},
		MaxTokens:      20,
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		t.Fatalf("live OAuth completion failed: %v", err)
	}
	var result struct {
		OAuth bool `json:"oauth"`
	}
	if err := json.Unmarshal([]byte(response.Content), &result); err != nil || !result.OAuth {
		t.Fatalf("live response = %q, JSON error = %v", response.Content, err)
	}
	if response.Model != "openai/gpt-5.4-mini" {
		t.Fatalf("live response model = %q", response.Model)
	}
}
