package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/llamabroker"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const (
	// DefaultBaseURL is the default Ollama server address including the /v1 path
	// prefix required by Ollama's OpenAI-compatible chat completions endpoint.
	DefaultBaseURL = "http://localhost:11434/v1"

	// ModelLlama3 is the default Llama 3 model served by Ollama.
	ModelLlama3 = "llama3.2"
	// ModelMistral is the Mistral model available on Ollama.
	ModelMistral = "mistral"
)

// Config contains the settings required to create an Ollama provider.
type Config struct {
	// BaseURL is the address of the locally running Ollama server.
	// Defaults to DefaultBaseURL if empty.
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type ssePreambleTransport struct {
	base http.RoundTripper
}

func (t *ssePreambleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Detect streaming before the body is consumed by the upstream call.
	isStreaming := requestIsStreaming(req)

	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusOK && strings.Contains(req.URL.Path, "/chat/completions") {
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/event-stream") {
			if isStreaming {
				// Streaming: strip only broker heartbeats, keep SSE format intact
				// so the openai-go SDK can parse the chunk stream.
				resp.Body = llamabroker.StripBrokerHeartbeatsReadCloser(resp.Body)
				// Content-Type stays text/event-stream — SDK needs it.
			} else {
				// Non-streaming: strip SSE preamble, unwrap the single JSON body.
				resp.Header.Set("Content-Type", "application/json")
				stripped := llamabroker.StripSSEPreambleReadCloser(resp.Body)
				// llama-line now signals errors as SSE status events, handled by
				// StripSSEPreamble. PeekBrokerError handles older bare-JSON errors.
				errMsg, remaining := llamabroker.PeekBrokerError(stripped) //nolint:staticcheck // Required for older llama-line broker responses.
				if errMsg != "" {
					resp.StatusCode = http.StatusBadGateway
					resp.Status = "502 Bad Gateway"
					body := `{"error":{"message":"` + errMsg + `","type":"broker_error","code":"broker_error"}}`
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				}
				// Guard against empty body (e.g. connection dropped mid-stream before
				// a final payload or error event was sent).
				payload, readErr := io.ReadAll(remaining)
				if readErr != nil || len(bytes.TrimSpace(payload)) == 0 {
					resp.StatusCode = http.StatusBadGateway
					resp.Status = "502 Bad Gateway"
					msg := "broker connection closed before response was received"
					if readErr != nil {
						msg = readErr.Error()
					}
					body := `{"error":{"message":"` + msg + `","type":"broker_error","code":"broker_error"}}`
					resp.Body = io.NopCloser(strings.NewReader(body))
					return resp, nil
				}
				resp.Body = io.NopCloser(bytes.NewReader(payload))
			}
		}
	}
	return resp, nil
}

// requestIsStreaming reports whether the request body contains "stream":true.
// It peeks up to 512 bytes and restores the body so the upstream can read it.
func requestIsStreaming(req *http.Request) bool {
	if req.Body == nil || req.GetBody == nil {
		return false
	}
	body, err := req.GetBody()
	if err != nil {
		return false
	}
	buf := make([]byte, 512)
	n, _ := body.Read(buf)
	_ = body.Close()
	return bytes.Contains(buf[:n], []byte(`"stream":true`))
}

// Provider implements llm.Provider using Ollama's OpenAI-compatible HTTP API.
type Provider struct {
	client openaisdk.Client
	model  string
}

var _ llm.Provider = (*Provider)(nil)

// DefaultModelsByTier returns the default Ollama model mapping for the registry.
func DefaultModelsByTier() map[llm.ModelTier]string {
	return map[llm.ModelTier]string{
		llm.ModelTierDeepThink:  ModelLlama3,
		llm.ModelTierQuickThink: ModelLlama3,
	}
}

// NewProvider constructs an Ollama completion provider.
func NewProvider(cfg Config) (*Provider, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("ollama: api key is required (set OLLAMA_API_KEY for llama-line broker)")
	}

	baseTransport := http.DefaultTransport
	client := &http.Client{Transport: &ssePreambleTransport{base: baseTransport}}
	if cfg.HTTPClient != nil {
		cloned := *cfg.HTTPClient
		if cloned.Transport != nil {
			baseTransport = cloned.Transport
		}
		cloned.Transport = &ssePreambleTransport{base: baseTransport}
		client = &cloned
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(0),
		option.WithHTTPClient(client),
	}

	return &Provider{
		client: openaisdk.NewClient(opts...),
		model:  strings.TrimSpace(cfg.Model),
	}, nil
}

// Complete sends a chat completion request to the local Ollama server and returns
// the first response choice.
func (p *Provider) Complete(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if p == nil {
		return nil, errors.New("ollama: provider is nil")
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("ollama: at least one message is required")
	}

	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, errors.New("ollama: model is required")
	}

	messages, err := toChatCompletionMessages(request.Messages)
	if err != nil {
		return nil, err
	}

	responseFormat, err := toResponseFormat(request.ResponseFormat)
	if err != nil {
		return nil, err
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: messages,
	}
	if request.Temperature != 0 {
		params.Temperature = openaisdk.Float(request.Temperature)
	}
	if request.MaxTokens > 0 {
		params.MaxCompletionTokens = openaisdk.Int(int64(request.MaxTokens))
	}
	if responseFormat != nil {
		params.ResponseFormat = *responseFormat
	}

	startedAt := time.Now()
	retryBudget := completeRetryBudget{}
	for attempt := 1; attempt <= retryBudget.maxAttempts(); attempt++ {
		completion, err := p.client.Chat.Completions.New(ctx, params,
			// Disable qwen3 thinking mode so the model returns content directly
			// instead of consuming all tokens on internal chain-of-thought reasoning.
			option.WithJSONSet("think", false),
		)
		if err != nil {
			wrapped := fmt.Errorf("ollama: complete request: %w", err)
			if retryBudget.consume(wrapped) {
				continue
			}
			return nil, wrapped
		}
		if len(completion.Choices) == 0 {
			err := errors.New("ollama: completion response did not include any choices")
			if retryBudget.consume(err) {
				continue
			}
			return nil, err
		}

		content := extractContent(completion.Choices[0].Message)

		return &llm.CompletionResponse{
			Content: parse.ExtractContent(content),
			Usage: llm.CompletionUsage{
				PromptTokens:     int(completion.Usage.PromptTokens),
				CompletionTokens: int(completion.Usage.CompletionTokens),
			},
			Model:     completion.Model,
			LatencyMS: int(time.Since(startedAt).Milliseconds()),
		}, nil
	}

	return nil, errors.New("ollama: exhausted completion attempts")
}

type completeRetryKind int

const (
	completeRetryNone completeRetryKind = iota
	completeRetryNoChoices
	completeRetryBrokerClosed
	completeRetryTimeout
)

type completeRetryBudget struct {
	noChoicesCount   int
	brokerClosedUsed bool
	timeoutUsed      bool
}

func (b *completeRetryBudget) maxAttempts() int {
	if b == nil {
		return 1
	}
	return 3 // initial attempt + bounded retries for one timeout/broker-close or up to two repeated no-choice transients
}

func (b *completeRetryBudget) consume(err error) bool {
	if b == nil {
		return false
	}
	switch classifyCompleteRetryKind(err) {
	case completeRetryNoChoices:
		if b.noChoicesCount >= 2 {
			return false
		}
		b.noChoicesCount++
		return true
	case completeRetryBrokerClosed:
		if b.brokerClosedUsed {
			return false
		}
		b.brokerClosedUsed = true
		return true
	case completeRetryTimeout:
		if b.timeoutUsed {
			return false
		}
		b.timeoutUsed = true
		return true
	default:
		return false
	}
}

func classifyCompleteRetryKind(err error) completeRetryKind {
	if err == nil {
		return completeRetryNone
	}
	if errors.Is(err, context.Canceled) {
		return completeRetryNone
	}
	errText := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errText, "completion response did not include any choices"):
		return completeRetryNoChoices
	case strings.Contains(errText, "broker connection closed before response was received"):
		return completeRetryBrokerClosed
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(errText, "context deadline exceeded"):
		return completeRetryTimeout
	default:
		return completeRetryNone
	}
}

// extractContent returns the usable text from a chat completion message.
// Ollama's OpenAI-compatible endpoint puts qwen3's chain-of-thought output
// into a non-standard "reasoning" JSON field and leaves "content" empty.
// When that happens we fall back to the reasoning field.
func extractContent(msg openaisdk.ChatCompletionMessage) string {
	if msg.Content != "" {
		return msg.Content
	}

	reasoningField, ok := msg.JSON.ExtraFields["reasoning"]
	if !ok || !reasoningField.Valid() {
		return ""
	}

	raw := reasoningField.Raw()
	var reasoning string
	if err := json.Unmarshal([]byte(raw), &reasoning); err != nil {
		slog.Warn("ollama: failed to unmarshal reasoning field", "raw", raw, "error", err)
		return ""
	}

	if reasoning != "" {
		slog.Info("ollama: using reasoning field as content (thinking mode workaround)")
	}
	return reasoning
}

func toChatCompletionMessages(messages []llm.Message) ([]openaisdk.ChatCompletionMessageParamUnion, error) {
	chatMessages := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		switch role := strings.ToLower(strings.TrimSpace(message.Role)); role {
		case "system":
			chatMessages = append(chatMessages, openaisdk.SystemMessage(message.Content))
		case "user":
			chatMessages = append(chatMessages, openaisdk.UserMessage(message.Content))
		case "assistant":
			chatMessages = append(chatMessages, openaisdk.AssistantMessage(message.Content))
		default:
			return nil, fmt.Errorf("ollama: unsupported message role %q", message.Role)
		}
	}

	return chatMessages, nil
}

func toResponseFormat(format *llm.ResponseFormat) (*openaisdk.ChatCompletionNewParamsResponseFormatUnion, error) {
	if format == nil {
		return nil, nil
	}

	switch format.Type {
	case "", llm.ResponseFormatText:
		return nil, nil
	case llm.ResponseFormatJSONObject:
		jsonObject := shared.NewResponseFormatJSONObjectParam()
		return &openaisdk.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &jsonObject,
		}, nil
	default:
		return nil, fmt.Errorf("ollama: unsupported response format type %q", format.Type)
	}
}
