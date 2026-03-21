package ollama

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const (
	// DefaultBaseURL is the default Ollama server address.
	DefaultBaseURL = "http://localhost:11434"

	// ModelLlama3 is the default Llama 3 model served by Ollama.
	ModelLlama3 = "llama3.2"
	// ModelMistral is the Mistral model available on Ollama.
	ModelMistral = "mistral"

	// ollamaAPIKey is a placeholder key used to satisfy the OpenAI-compatible API
	// client, which requires a non-empty API key even though Ollama does not
	// authenticate requests.
	ollamaAPIKey = "ollama"
)

// Config contains the settings required to create an Ollama provider.
type Config struct {
	// BaseURL is the address of the locally running Ollama server.
	// Defaults to DefaultBaseURL if empty.
	BaseURL    string
	Model      string
	HTTPClient *http.Client
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

	opts := []option.RequestOption{
		option.WithAPIKey(ollamaAPIKey),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(0),
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
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
	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("ollama: complete request: %w", err)
	}
	if len(completion.Choices) == 0 {
		return nil, errors.New("ollama: completion response did not include any choices")
	}

	return &llm.CompletionResponse{
		Content: completion.Choices[0].Message.Content,
		Usage: llm.CompletionUsage{
			PromptTokens:     int(completion.Usage.PromptTokens),
			CompletionTokens: int(completion.Usage.CompletionTokens),
		},
		Model:     completion.Model,
		LatencyMS: int(time.Since(startedAt).Milliseconds()),
	}, nil
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
