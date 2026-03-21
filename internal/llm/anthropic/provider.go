package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

const (
	// ModelClaudeOpus is the latest Claude Opus model.
	ModelClaudeOpus = anthropicsdk.ModelClaudeOpus4_6
	// ModelClaudeSonnet is the latest Claude Sonnet model.
	ModelClaudeSonnet = anthropicsdk.ModelClaudeSonnet4_6
	// ModelClaudeHaiku is the latest Claude Haiku model.
	ModelClaudeHaiku = anthropicsdk.ModelClaudeHaiku4_5

	// defaultMaxTokens is the maximum number of output tokens when none is specified in
	// the request. Anthropic's Messages API requires this field, so a reasonable default
	// is used to avoid request failures.
	defaultMaxTokens = int64(4096)

	// blockTypeText is the content block type identifier for plain text responses.
	blockTypeText = "text"
)

// Config contains the settings required to create an Anthropic provider.
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// Provider implements llm.Provider using the official Anthropic Go SDK.
type Provider struct {
	client anthropicsdk.Client
	model  string
}

var _ llm.Provider = (*Provider)(nil)

// DefaultModelsByTier returns the default Anthropic model mapping for the registry.
func DefaultModelsByTier() map[llm.ModelTier]string {
	return map[llm.ModelTier]string{
		llm.ModelTierDeepThink:  ModelClaudeSonnet,
		llm.ModelTierQuickThink: ModelClaudeHaiku,
	}
}

// NewProvider constructs an Anthropic completion provider.
func NewProvider(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("anthropic: api key is required")
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
		client: anthropicsdk.NewClient(opts...),
		model:  strings.TrimSpace(cfg.Model),
	}, nil
}

// Complete sends a messages request to Anthropic and returns the text response.
func (p *Provider) Complete(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if p == nil {
		return nil, errors.New("anthropic: provider is nil")
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("anthropic: at least one message is required")
	}

	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, errors.New("anthropic: model is required")
	}

	if err := validateResponseFormat(request.ResponseFormat); err != nil {
		return nil, err
	}

	systemBlocks, messages, err := buildMessages(request.Messages)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("anthropic: at least one user or assistant message is required")
	}

	maxTokens := int64(request.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropicsdk.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  messages,
		System:    systemBlocks,
	}
	if request.Temperature != 0 {
		params.Temperature = param.NewOpt(request.Temperature)
	}

	startedAt := time.Now()
	message, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic: complete request: %w", err)
	}

	content := extractTextContent(message.Content)
	if content == "" && len(message.Content) > 0 {
		return nil, errors.New("anthropic: completion response did not include any text content")
	}

	return &llm.CompletionResponse{
		Content: content,
		Usage: llm.CompletionUsage{
			PromptTokens:     int(message.Usage.InputTokens),
			CompletionTokens: int(message.Usage.OutputTokens),
		},
		Model:     string(message.Model),
		LatencyMS: int(time.Since(startedAt).Milliseconds()),
	}, nil
}

func buildMessages(messages []llm.Message) ([]anthropicsdk.TextBlockParam, []anthropicsdk.MessageParam, error) {
	var systemBlocks []anthropicsdk.TextBlockParam
	var chatMessages []anthropicsdk.MessageParam

	for _, msg := range messages {
		switch role := strings.ToLower(strings.TrimSpace(msg.Role)); role {
		case "system":
			systemBlocks = append(systemBlocks, anthropicsdk.TextBlockParam{Text: msg.Content})
		case "user":
			chatMessages = append(chatMessages, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(msg.Content)))
		case "assistant":
			chatMessages = append(chatMessages, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(msg.Content)))
		default:
			return nil, nil, fmt.Errorf("anthropic: unsupported message role %q", msg.Role)
		}
	}

	return systemBlocks, chatMessages, nil
}

func extractTextContent(blocks []anthropicsdk.ContentBlockUnion) string {
	var sb strings.Builder
	for _, block := range blocks {
		if block.Type == blockTypeText {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

func validateResponseFormat(format *llm.ResponseFormat) error {
	if format == nil {
		return nil
	}
	switch format.Type {
	case "", llm.ResponseFormatText:
		return nil
	default:
		return fmt.Errorf("anthropic: unsupported response format type %q", format.Type)
	}
}
