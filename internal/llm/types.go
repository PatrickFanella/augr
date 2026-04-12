package llm

import "encoding/json"

// ModelTier identifies the level of reasoning required for a request.
type ModelTier string

const (
	// ModelTierDeepThink is intended for higher-quality, more expensive reasoning tasks.
	ModelTierDeepThink ModelTier = "deep_think"
	// ModelTierQuickThink is intended for faster, lower-cost reasoning tasks.
	ModelTierQuickThink ModelTier = "quick_think"
)

// String returns the string representation of a ModelTier.
func (t ModelTier) String() string {
	return string(t)
}

// Message represents a single chat message in a completion request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormatType identifies the requested output format for a completion.
type ResponseFormatType string

const (
	// ResponseFormatText requests plain text output.
	ResponseFormatText ResponseFormatType = "text"
	// ResponseFormatJSONObject requests structured JSON object output.
	ResponseFormatJSONObject ResponseFormatType = "json_object"
)

// ResponseFormat describes the expected response shape.
//
// Schema is optional and allows future providers to pass through a JSON schema
// for structured output modes supported by OpenAI, Anthropic, or Google APIs.
type ResponseFormat struct {
	Type   ResponseFormatType `json:"type"`
	Schema json.RawMessage    `json:"schema,omitempty"`
}

// CompletionRequest captures the provider-agnostic input required for a chat completion.
type CompletionRequest struct {
	Model          string          `json:"model,omitempty"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// CompletionUsage tracks token counts returned by the provider.
type CompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// CompletionResponse captures the provider-agnostic result of a chat completion.
type CompletionResponse struct {
	Content      string          `json:"content"`
	Usage        CompletionUsage `json:"usage"`
	Model        string          `json:"model,omitempty"`
	LatencyMS    int             `json:"latency_ms,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	UsedFallback bool            `json:"used_fallback,omitempty"`
	TimedOut     bool            `json:"timed_out,omitempty"`
}
