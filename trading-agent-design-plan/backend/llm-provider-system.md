---
title: "LLM Provider System"
date: 2026-03-20
tags: [backend, llm, providers, openai, anthropic, ollama]
---

# LLM Provider System

A multi-provider abstraction layer in Go that supports OpenAI, Anthropic, Google, and Ollama with a unified interface, two-tier model strategy, and automatic fallback.

## Provider Interface

```go
// internal/llm/provider.go
type Provider interface {
    // Complete sends a chat completion request and returns the response
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
    // Name returns the provider identifier
    Name() string
}

type CompletionRequest struct {
    SystemPrompt string
    Messages     []Message
    Temperature  float64
    MaxTokens    int
    JSONMode     bool    // request structured JSON output
}

type CompletionResponse struct {
    Content         string
    PromptTokens    int
    CompletionTokens int
    Model           string
    LatencyMs       int64
}

type Message struct {
    Role    string // "user", "assistant"
    Content string
}
```

## Factory Pattern

```go
// internal/llm/factory.go
type Factory interface {
    CreateProvider(cfg ProviderConfig) (Provider, error)
}

type ProviderConfig struct {
    ProviderName string // "openai", "anthropic", "google", "ollama"
    Model        string // "claude-sonnet-4-6", "gpt-5.2", etc.
    APIKey       string // from environment
    BaseURL      string // override for Ollama, OpenRouter, xAI
}

func NewFactory() Factory {
    return &defaultFactory{}
}

func (f *defaultFactory) CreateProvider(cfg ProviderConfig) (Provider, error) {
    switch cfg.ProviderName {
    case "openai":
        return NewOpenAIProvider(cfg)
    case "anthropic":
        return NewAnthropicProvider(cfg)
    case "google":
        return NewGoogleProvider(cfg)
    case "ollama":
        return NewOllamaProvider(cfg)
    default:
        return nil, fmt.Errorf("unknown provider: %s", cfg.ProviderName)
    }
}
```

## Provider Implementations

### OpenAI

Uses the official `github.com/openai/openai-go` SDK:

```go
// internal/llm/openai.go
type OpenAIProvider struct {
    client *openai.Client
    model  string
}

func NewOpenAIProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
    opts := []option.RequestOption{
        option.WithAPIKey(cfg.APIKey),
    }
    if cfg.BaseURL != "" {
        opts = append(opts, option.WithBaseURL(cfg.BaseURL))
    }
    return &OpenAIProvider{
        client: openai.NewClient(opts...),
        model:  cfg.Model,
    }, nil
}
```

Also handles **xAI** (Grok) and **OpenRouter** via custom `BaseURL`.

### Anthropic

Uses `github.com/anthropics/anthropic-sdk-go`:

```go
// internal/llm/anthropic.go
type AnthropicProvider struct {
    client *anthropic.Client
    model  string
}
```

Key difference: Anthropic uses a separate `system` parameter rather than a system message in the messages array.

### Google (Gemini)

Uses `cloud.google.com/go/vertexai/genai` or the REST API directly.

### Ollama (Local)

OpenAI-compatible API at `http://localhost:11434/v1`:

```go
func NewOllamaProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
    cfg.BaseURL = "http://localhost:11434/v1"
    cfg.APIKey = "ollama" // Ollama ignores API keys
    return NewOpenAIProvider(cfg)
}
```

## Two-Tier Model Strategy

Following the TradingAgents pattern, the system uses two LLM tiers:

| Tier            | Usage                                        | Example Models                   | Rationale                                         |
| --------------- | -------------------------------------------- | -------------------------------- | ------------------------------------------------- |
| **Deep Think**  | Research debates, risk judgment, trader plan | `claude-sonnet-4-6`, `gpt-5.2`   | Complex reasoning requires capable models         |
| **Quick Think** | Analyst reports, signal extraction           | `claude-haiku-4-5`, `gpt-5-mini` | Simpler tasks benefit from speed and cost savings |

Configuration:

```go
type LLMConfig struct {
    DeepThink ProviderConfig // used for debates and judgment
    QuickThink ProviderConfig // used for analysis and extraction
}
```

Agents declare which tier they need:

```go
type AgentTier int
const (
    QuickThink AgentTier = iota
    DeepThink
)
```

## Retry and Fallback

```go
// internal/llm/retry.go
type RetryProvider struct {
    primary   Provider
    fallback  Provider
    maxRetries int
    baseDelay  time.Duration
}

func (r *RetryProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
    var lastErr error
    for attempt := 0; attempt <= r.maxRetries; attempt++ {
        resp, err := r.primary.Complete(ctx, req)
        if err == nil {
            return resp, nil
        }
        lastErr = err
        if !isRetryable(err) {
            break
        }
        time.Sleep(r.baseDelay * time.Duration(1<<attempt))
    }

    // Try fallback provider
    if r.fallback != nil {
        return r.fallback.Complete(ctx, req)
    }
    return nil, fmt.Errorf("all attempts failed: %w", lastErr)
}

func isRetryable(err error) bool {
    // Rate limits (429) and server errors (5xx) are retryable
    var apiErr *APIError
    if errors.As(err, &apiErr) {
        return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
    }
    return false
}
```

## Token Tracking

Every response records token usage for cost monitoring:

```go
type UsageTracker struct {
    mu      sync.Mutex
    records []UsageRecord
}

type UsageRecord struct {
    Provider        string
    Model           string
    PromptTokens    int
    CompletionTokens int
    Timestamp       time.Time
    PipelineRunID   uuid.UUID
}
```

Token costs are stored in `agent_decisions` and surfaced in the dashboard.

## Supported Models

| Provider  | Models                                               | Notes                                       |
| --------- | ---------------------------------------------------- | ------------------------------------------- |
| OpenAI    | gpt-5.4, gpt-5.2, gpt-5-mini                         | GPT-5 family: no temperature/top_p override |
| Anthropic | claude-opus-4-6, claude-sonnet-4-6, claude-haiku-4-5 | Separate system prompt parameter            |
| Google    | gemini-3.1                                           | Supports vision (future: chart analysis)    |
| Ollama    | llama3, mistral, etc.                                | Local inference; no API cost                |

---

**Related:** [[agent-orchestration-engine]] · [[technology-stack]] · [[agent-system-overview]]
