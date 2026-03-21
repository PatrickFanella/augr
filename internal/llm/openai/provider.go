package openai

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
	ModelGPT54    = "gpt-5.4"
	ModelGPT52    = "gpt-5.2"
	ModelGPT5Mini = "gpt-5-mini"
)

// Config contains the settings required to create an OpenAI-compatible provider.
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// Provider implements llm.Provider using the official OpenAI Go SDK.
type Provider struct {
	client openaisdk.Client
	model  string
}

var _ llm.Provider = (*Provider)(nil)

// DefaultModelsByTier returns the default OpenAI model mapping for the registry.
func DefaultModelsByTier() map[llm.ModelTier]string {
	return map[llm.ModelTier]string{
		llm.ModelTierDeepThink:  ModelGPT52,
		llm.ModelTierQuickThink: ModelGPT5Mini,
	}
}

// NewProvider constructs an OpenAI-compatible completion provider.
func NewProvider(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("openai: api key is required")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithMaxRetries(0),
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return &Provider{
		client: openaisdk.NewClient(opts...),
		model:  strings.TrimSpace(cfg.Model),
	}, nil
}

// Complete sends a chat completion request and returns the first response choice.
func (p *Provider) Complete(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if p == nil {
		return nil, errors.New("openai: provider is nil")
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("openai: at least one message is required")
	}

	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, errors.New("openai: model is required")
	}

	messages, err := toChatCompletionMessages(request.Messages)
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

	responseFormat, err := toResponseFormat(request.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if responseFormat != nil {
		params.ResponseFormat = *responseFormat
	}

	startedAt := time.Now()
	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai: complete request: %w", err)
	}
	if len(completion.Choices) == 0 {
		return nil, errors.New("openai: completion response did not include any choices")
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
			return nil, fmt.Errorf("openai: unsupported message role %q", message.Role)
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
		return nil, fmt.Errorf("openai: unsupported response format type %q", format.Type)
	}
}
