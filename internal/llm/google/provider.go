package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"google.golang.org/genai"
)

const (
	// ModelGemini31Pro is the Gemini 3.1 model variant intended for deeper reasoning.
	ModelGemini31Pro = "gemini-3.1-pro"
	// ModelGemini31Flash is the Gemini 3.1 model variant intended for faster responses.
	ModelGemini31Flash = "gemini-3.1-flash"

	defaultAPIVersion = "v1beta"
)

// Config contains the settings required to create a Google Gemini provider.
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// Provider implements llm.Provider using the official Google GenAI SDK.
type Provider struct {
	client *genai.Client
	model  string
}

var _ llm.Provider = (*Provider)(nil)

// DefaultModelsByTier returns the default Gemini model mapping for the registry.
func DefaultModelsByTier() map[llm.ModelTier]string {
	return map[llm.ModelTier]string{
		llm.ModelTierDeepThink:  ModelGemini31Pro,
		llm.ModelTierQuickThink: ModelGemini31Flash,
	}
}

// NewProvider constructs a Google Gemini completion provider.
func NewProvider(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("google: api key is required")
	}

	clientCfg := &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{
			APIVersion: defaultAPIVersion,
		},
	}
	if cfg.HTTPClient != nil {
		clientCfg.HTTPClient = cfg.HTTPClient
	}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		clientCfg.HTTPOptions.BaseURL = baseURL
	}

	client, err := genai.NewClient(context.Background(), clientCfg)
	if err != nil {
		return nil, fmt.Errorf("google: create client: %w", err)
	}

	return &Provider{
		client: client,
		model:  strings.TrimSpace(cfg.Model),
	}, nil
}

// Complete sends a generate content request and returns the text response.
func (p *Provider) Complete(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if p == nil {
		return nil, errors.New("google: provider is nil")
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("google: at least one message is required")
	}

	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, errors.New("google: model is required")
	}

	systemInstruction, contents, err := buildContents(request.Messages)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 {
		return nil, errors.New("google: at least one user or assistant message is required")
	}

	config, err := buildGenerateContentConfig(request, systemInstruction)
	if err != nil {
		return nil, err
	}

	startedAt := time.Now()
	response, err := p.client.Models.GenerateContent(ctx, model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("google: complete request: %w", err)
	}
	if len(response.Candidates) == 0 {
		return nil, errors.New("google: completion response did not include any candidates")
	}

	content := strings.TrimSpace(response.Text())
	if content == "" {
		return nil, errors.New("google: completion response did not include any text content")
	}

	var promptTokens int
	var completionTokens int
	if response.UsageMetadata != nil {
		promptTokens = int(response.UsageMetadata.PromptTokenCount)
		completionTokens = int(response.UsageMetadata.CandidatesTokenCount)
	}

	resolvedModel := strings.TrimSpace(response.ModelVersion)
	if resolvedModel == "" {
		resolvedModel = model
	}

	return &llm.CompletionResponse{
		Content: content,
		Usage: llm.CompletionUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
		},
		Model:     resolvedModel,
		LatencyMS: int(time.Since(startedAt).Milliseconds()),
	}, nil
}

func buildContents(messages []llm.Message) (*genai.Content, []*genai.Content, error) {
	var systemMessages []string
	contents := make([]*genai.Content, 0, len(messages))

	for _, msg := range messages {
		switch role := strings.ToLower(strings.TrimSpace(msg.Role)); role {
		case "system":
			systemMessages = append(systemMessages, msg.Content)
		case "user":
			contents = append(contents, genai.NewContentFromText(msg.Content, genai.RoleUser))
		case "assistant":
			contents = append(contents, genai.NewContentFromText(msg.Content, genai.RoleModel))
		default:
			return nil, nil, fmt.Errorf("google: unsupported message role %q", msg.Role)
		}
	}

	var systemInstruction *genai.Content
	if len(systemMessages) > 0 {
		systemInstruction = genai.NewContentFromText(strings.Join(systemMessages, "\n\n"), genai.RoleUser)
	}

	return systemInstruction, contents, nil
}

func buildGenerateContentConfig(request llm.CompletionRequest, systemInstruction *genai.Content) (*genai.GenerateContentConfig, error) {
	config := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
	}

	if request.Temperature != 0 {
		temperature := float32(request.Temperature)
		config.Temperature = &temperature
	}
	if request.MaxTokens > 0 {
		config.MaxOutputTokens = int32(request.MaxTokens)
	}

	if err := applyResponseFormat(config, request.ResponseFormat); err != nil {
		return nil, err
	}

	return config, nil
}

func applyResponseFormat(config *genai.GenerateContentConfig, format *llm.ResponseFormat) error {
	if format == nil {
		return nil
	}

	switch format.Type {
	case "", llm.ResponseFormatText:
		return nil
	case llm.ResponseFormatJSONObject:
		config.ResponseMIMEType = "application/json"
		if len(format.Schema) > 0 {
			var schema any
			if err := json.Unmarshal(format.Schema, &schema); err != nil {
				return fmt.Errorf("google: parse response format schema: %w", err)
			}
			config.ResponseJsonSchema = schema
		}
		return nil
	default:
		return fmt.Errorf("google: unsupported response format type %q", format.Type)
	}
}
