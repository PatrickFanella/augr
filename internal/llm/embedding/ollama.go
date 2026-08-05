package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm/llamabroker"
)

// DefaultModel is the default embedding model.
const DefaultModel = "nomic-embed-text"

// DefaultBaseURL is the default Ollama server address.
const DefaultBaseURL = "http://localhost:11434"

// DefaultTimeout for a single embed request.
const DefaultTimeout = 30 * time.Second

// DefaultBatchSize is the max texts per single /api/embed call.
const DefaultBatchSize = 64

// OllamaConfig configures the Ollama embedding provider.
type OllamaConfig struct {
	// BaseURL of the Ollama server (without /v1 suffix). Defaults to DefaultBaseURL.
	BaseURL string
	// Model to use for embeddings. Defaults to DefaultModel.
	Model string
	// APIKey for llama-line broker auth.
	APIKey string
	// Timeout per HTTP request. Defaults to DefaultTimeout.
	Timeout time.Duration
	// BatchSize is the maximum texts per /api/embed call. Defaults to DefaultBatchSize.
	BatchSize int
	// HTTPClient overrides the default HTTP client (useful for testing).
	HTTPClient *http.Client
}

// OllamaProvider implements Provider using Ollama's /api/embed endpoint.
type OllamaProvider struct {
	baseURL    string
	model      string
	apiKey     string
	batchSize  int
	httpClient *http.Client
}

var _ Provider = (*OllamaProvider)(nil)

// NewOllamaProvider constructs an embedding provider for a local Ollama server.
func NewOllamaProvider(cfg OllamaConfig) (*OllamaProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("embedding: ollama api key is required (set OLLAMA_API_KEY for llama-line broker)")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &OllamaProvider{
		baseURL:    baseURL,
		model:      model,
		apiKey:     apiKey,
		batchSize:  batchSize,
		httpClient: client,
	}, nil
}

// embedRequest is the JSON body sent to POST /api/embed.
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse is the JSON body returned from POST /api/embed.
type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed returns a single embedding vector for text.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := p.doEmbed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding: empty response from ollama")
	}
	return vecs[0], nil
}

// EmbedBatch returns embedding vectors for each input text. If len(texts)
// exceeds BatchSize, multiple HTTP calls are made transparently.
func (p *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	result := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += p.batchSize {
		end := start + p.batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := p.doEmbed(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embedding: batch [%d:%d]: %w", start, end, err)
		}
		result = append(result, vecs...)
	}

	if len(result) != len(texts) {
		return nil, fmt.Errorf("embedding: expected %d vectors, got %d", len(texts), len(result))
	}
	return result, nil
}

// doEmbed makes a single POST /api/embed call and converts float64→float32.
func (p *OllamaProvider) doEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{
		Model: p.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding: ollama returned %d: %s", resp.StatusCode, string(respBody))
	}

	resp.Body = io.NopCloser(llamabroker.StripSSEPreamble(resp.Body))

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding: decode response: %w", err)
	}

	// Convert [][]float64 → [][]float32 (pgvector uses float32).
	vecs := make([][]float32, len(result.Embeddings))
	for i, emb := range result.Embeddings {
		v := make([]float32, len(emb))
		for j, f := range emb {
			v[j] = float32(f)
		}
		vecs[i] = v
	}
	return vecs, nil
}
