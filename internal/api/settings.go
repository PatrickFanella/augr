package api

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
)

// SettingsService provides the editable settings surfaced by the frontend.
type SettingsService interface {
	Get(context.Context) (SettingsResponse, error)
	Update(context.Context, SettingsUpdateRequest) (SettingsResponse, error)
}

// SettingsResponse is the API payload returned to the settings page.
type SettingsResponse struct {
	LLM    LLMSettingsResponse `json:"llm"`
	Risk   RiskSettings        `json:"risk"`
	System SystemInfo          `json:"system"`
}

// LLMSettingsResponse contains provider configuration and model selection state.
type LLMSettingsResponse struct {
	DefaultProvider string               `json:"default_provider"`
	DeepThinkModel  string               `json:"deep_think_model"`
	QuickThinkModel string               `json:"quick_think_model"`
	Providers       LLMProvidersResponse `json:"providers"`
}

// LLMProvidersResponse groups provider-specific settings.
type LLMProvidersResponse struct {
	OpenAI     LLMProviderResponse `json:"openai"`
	Anthropic  LLMProviderResponse `json:"anthropic"`
	Google     LLMProviderResponse `json:"google"`
	OpenRouter LLMProviderResponse `json:"openrouter"`
	XAI        LLMProviderResponse `json:"xai"`
	Ollama     OllamaSettings      `json:"ollama"`
}

// LLMProviderResponse represents a provider without exposing the raw secret.
type LLMProviderResponse struct {
	APIKeyConfigured bool   `json:"api_key_configured"`
	APIKeyLast4      string `json:"api_key_last4,omitempty"`
	BaseURL          string `json:"base_url,omitempty"`
	Model            string `json:"model"`
}

// OllamaSettings contains local model settings.
type OllamaSettings struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model"`
}

// RiskSettings contains configurable risk thresholds shown in the settings UI.
type RiskSettings struct {
	MaxPositionSizePct         float64 `json:"max_position_size_pct"`
	MaxDailyLossPct            float64 `json:"max_daily_loss_pct"`
	MaxDrawdownPct             float64 `json:"max_drawdown_pct"`
	MaxOpenPositions           int     `json:"max_open_positions"`
	MaxTotalExposurePct        float64 `json:"max_total_exposure_pct"`
	MaxPerMarketExposurePct    float64 `json:"max_per_market_exposure_pct"`
	CircuitBreakerThresholdPct float64 `json:"circuit_breaker_threshold_pct"`
	CircuitBreakerCooldownMin  int     `json:"circuit_breaker_cooldown_min"`
}

// SystemInfo provides non-editable system metadata for the settings page.
type SystemInfo struct {
	Environment      string             `json:"environment"`
	Version          string             `json:"version"`
	UptimeSeconds    int64              `json:"uptime_seconds"`
	ConnectedBrokers []BrokerConnection `json:"connected_brokers"`
}

// BrokerConnection summarizes broker connectivity/configuration.
type BrokerConnection struct {
	Name       string `json:"name"`
	PaperMode  bool   `json:"paper_mode"`
	Configured bool   `json:"configured"`
}

// SettingsUpdateRequest is the payload accepted by the settings update endpoint.
type SettingsUpdateRequest struct {
	LLM  LLMSettingsUpdateRequest `json:"llm"`
	Risk RiskSettings             `json:"risk"`
}

// LLMSettingsUpdateRequest contains updated provider and tier selections.
type LLMSettingsUpdateRequest struct {
	DefaultProvider string                    `json:"default_provider"`
	DeepThinkModel  string                    `json:"deep_think_model"`
	QuickThinkModel string                    `json:"quick_think_model"`
	Providers       LLMProvidersUpdateRequest `json:"providers"`
}

// LLMProvidersUpdateRequest groups per-provider updates.
type LLMProvidersUpdateRequest struct {
	OpenAI     LLMProviderUpdateRequest `json:"openai"`
	Anthropic  LLMProviderUpdateRequest `json:"anthropic"`
	Google     LLMProviderUpdateRequest `json:"google"`
	OpenRouter LLMProviderUpdateRequest `json:"openrouter"`
	XAI        LLMProviderUpdateRequest `json:"xai"`
	Ollama     OllamaSettings           `json:"ollama"`
}

// LLMProviderUpdateRequest captures editable fields for API-backed providers.
type LLMProviderUpdateRequest struct {
	APIKey  *string `json:"api_key,omitempty"`
	BaseURL string  `json:"base_url,omitempty"`
	Model   string  `json:"model"`
}

// SettingsBootstrap contains the initial values used to seed the settings service.
type SettingsBootstrap struct {
	LLM              llmSettingsState
	Risk             RiskSettings
	Environment      string
	Version          string
	ConnectedBrokers []BrokerConnection
	StartedAt        time.Time
}

type llmSettingsState struct {
	DefaultProvider string
	DeepThinkModel  string
	QuickThinkModel string
	Providers       llmProvidersState
}

type llmProvidersState struct {
	OpenAI     providerState
	Anthropic  providerState
	Google     providerState
	OpenRouter providerState
	XAI        providerState
	Ollama     OllamaSettings
}

type providerState struct {
	APIKey  string
	BaseURL string
	Model   string
}

// MemorySettingsService stores settings in memory for authenticated UI editing.
type MemorySettingsService struct {
	mu      sync.RWMutex
	llm     llmSettingsState
	risk    RiskSettings
	system  SystemInfo
	started time.Time
}

// NewMemorySettingsService creates an in-memory settings service.
func NewMemorySettingsService(bootstrap SettingsBootstrap) *MemorySettingsService {
	startedAt := bootstrap.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	version := strings.TrimSpace(bootstrap.Version)
	if version == "" {
		version = detectBuildVersion()
	}

	return &MemorySettingsService{
		llm:  bootstrap.LLM,
		risk: bootstrap.Risk,
		system: SystemInfo{
			Environment:      strings.TrimSpace(bootstrap.Environment),
			Version:          version,
			ConnectedBrokers: append([]BrokerConnection(nil), bootstrap.ConnectedBrokers...),
		},
		started: startedAt,
	}
}

// NewMemorySettingsServiceFromConfig seeds the settings API from application config.
func NewMemorySettingsServiceFromConfig(cfg config.Config) *MemorySettingsService {
	return NewMemorySettingsService(SettingsBootstrap{
		LLM: llmSettingsState{
			DefaultProvider: cfg.LLM.DefaultProvider,
			DeepThinkModel:  cfg.LLM.DeepThinkModel,
			QuickThinkModel: cfg.LLM.QuickThinkModel,
			Providers: llmProvidersState{
				OpenAI: providerState{
					APIKey:  cfg.LLM.Providers.OpenAI.APIKey,
					BaseURL: cfg.LLM.Providers.OpenAI.BaseURL,
					Model:   cfg.LLM.Providers.OpenAI.Model,
				},
				Anthropic: providerState{
					APIKey: cfg.LLM.Providers.Anthropic.APIKey,
					Model:  cfg.LLM.Providers.Anthropic.Model,
				},
				Google: providerState{
					APIKey: cfg.LLM.Providers.Google.APIKey,
					Model:  cfg.LLM.Providers.Google.Model,
				},
				OpenRouter: providerState{
					APIKey:  cfg.LLM.Providers.OpenRouter.APIKey,
					BaseURL: cfg.LLM.Providers.OpenRouter.BaseURL,
					Model:   cfg.LLM.Providers.OpenRouter.Model,
				},
				XAI: providerState{
					APIKey:  cfg.LLM.Providers.XAI.APIKey,
					BaseURL: cfg.LLM.Providers.XAI.BaseURL,
					Model:   cfg.LLM.Providers.XAI.Model,
				},
				Ollama: OllamaSettings{
					BaseURL: cfg.LLM.Providers.Ollama.BaseURL,
					Model:   cfg.LLM.Providers.Ollama.Model,
				},
			},
		},
		Risk: RiskSettings{
			MaxPositionSizePct:         cfg.Risk.MaxPositionSizePct * 100,
			MaxDailyLossPct:            cfg.Risk.MaxDailyLossPct * 100,
			MaxDrawdownPct:             cfg.Risk.MaxDrawdownPct * 100,
			MaxOpenPositions:           cfg.Risk.MaxOpenPositions,
			MaxTotalExposurePct:        100,
			MaxPerMarketExposurePct:    50,
			CircuitBreakerThresholdPct: cfg.Risk.CircuitBreakerThreshold * 100,
			CircuitBreakerCooldownMin:  int(cfg.Risk.CircuitBreakerCooldown / time.Minute),
		},
		Environment: cfg.Environment,
		Version:     detectBuildVersion(),
		ConnectedBrokers: []BrokerConnection{
			{
				Name:       "alpaca",
				PaperMode:  cfg.Brokers.Alpaca.PaperMode,
				Configured: strings.TrimSpace(cfg.Brokers.Alpaca.APIKey) != "" && strings.TrimSpace(cfg.Brokers.Alpaca.APISecret) != "",
			},
			{
				Name:       "binance",
				PaperMode:  cfg.Brokers.Binance.PaperMode,
				Configured: strings.TrimSpace(cfg.Brokers.Binance.APIKey) != "" && strings.TrimSpace(cfg.Brokers.Binance.APISecret) != "",
			},
		},
		StartedAt: time.Now().UTC(),
	})
}

// Get returns the current editable settings snapshot.
func (s *MemorySettingsService) Get(context.Context) (SettingsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getLocked(), nil
}

func (s *MemorySettingsService) getLocked() SettingsResponse {
	return SettingsResponse{
		LLM: LLMSettingsResponse{
			DefaultProvider: s.llm.DefaultProvider,
			DeepThinkModel:  s.llm.DeepThinkModel,
			QuickThinkModel: s.llm.QuickThinkModel,
			Providers: LLMProvidersResponse{
				OpenAI:     redactProvider(s.llm.Providers.OpenAI),
				Anthropic:  redactProvider(s.llm.Providers.Anthropic),
				Google:     redactProvider(s.llm.Providers.Google),
				OpenRouter: redactProvider(s.llm.Providers.OpenRouter),
				XAI:        redactProvider(s.llm.Providers.XAI),
				Ollama:     s.llm.Providers.Ollama,
			},
		},
		Risk: s.risk,
		System: SystemInfo{
			Environment:      s.system.Environment,
			Version:          s.system.Version,
			UptimeSeconds:    int64(time.Since(s.started).Seconds()),
			ConnectedBrokers: append([]BrokerConnection(nil), s.system.ConnectedBrokers...),
		},
	}
}

// Update replaces editable settings while preserving existing secrets unless a new one is supplied.
func (s *MemorySettingsService) Update(_ context.Context, req SettingsUpdateRequest) (SettingsResponse, error) {
	if err := validateSettingsUpdate(req); err != nil {
		return SettingsResponse{}, err
	}

	s.mu.Lock()
	s.llm.DefaultProvider = strings.TrimSpace(req.LLM.DefaultProvider)
	s.llm.DeepThinkModel = strings.TrimSpace(req.LLM.DeepThinkModel)
	s.llm.QuickThinkModel = strings.TrimSpace(req.LLM.QuickThinkModel)
	applyProviderUpdate(&s.llm.Providers.OpenAI, req.LLM.Providers.OpenAI)
	applyProviderUpdate(&s.llm.Providers.Anthropic, req.LLM.Providers.Anthropic)
	applyProviderUpdate(&s.llm.Providers.Google, req.LLM.Providers.Google)
	applyProviderUpdate(&s.llm.Providers.OpenRouter, req.LLM.Providers.OpenRouter)
	applyProviderUpdate(&s.llm.Providers.XAI, req.LLM.Providers.XAI)
	s.llm.Providers.Ollama = OllamaSettings{
		BaseURL: strings.TrimSpace(req.LLM.Providers.Ollama.BaseURL),
		Model:   strings.TrimSpace(req.LLM.Providers.Ollama.Model),
	}
	s.risk = req.Risk
	response := s.getLocked()
	s.mu.Unlock()

	return response, nil
}

func applyProviderUpdate(target *providerState, update LLMProviderUpdateRequest) {
	target.BaseURL = strings.TrimSpace(update.BaseURL)
	target.Model = strings.TrimSpace(update.Model)
	if update.APIKey != nil {
		target.APIKey = strings.TrimSpace(*update.APIKey)
	}
}

func redactProvider(provider providerState) LLMProviderResponse {
	return LLMProviderResponse{
		APIKeyConfigured: strings.TrimSpace(provider.APIKey) != "",
		APIKeyLast4:      last4(provider.APIKey),
		BaseURL:          provider.BaseURL,
		Model:            provider.Model,
	}
}

func last4(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return trimmed
	}
	return trimmed[len(trimmed)-4:]
}

func validateSettingsUpdate(req SettingsUpdateRequest) error {
	provider := strings.TrimSpace(req.LLM.DefaultProvider)
	if provider == "" {
		return fmt.Errorf("default provider is required")
	}
	switch provider {
	case "openai", "anthropic", "google", "openrouter", "xai", "ollama":
	default:
		return fmt.Errorf("invalid default provider: %s", provider)
	}
	if strings.TrimSpace(req.LLM.DeepThinkModel) == "" {
		return fmt.Errorf("deep think model is required")
	}
	if strings.TrimSpace(req.LLM.QuickThinkModel) == "" {
		return fmt.Errorf("quick think model is required")
	}

	if err := validateProviderModel("openai", req.LLM.Providers.OpenAI.Model); err != nil {
		return err
	}
	if err := validateProviderModel("anthropic", req.LLM.Providers.Anthropic.Model); err != nil {
		return err
	}
	if err := validateProviderModel("google", req.LLM.Providers.Google.Model); err != nil {
		return err
	}
	if err := validateProviderModel("openrouter", req.LLM.Providers.OpenRouter.Model); err != nil {
		return err
	}
	if err := validateProviderModel("xai", req.LLM.Providers.XAI.Model); err != nil {
		return err
	}
	if strings.TrimSpace(req.LLM.Providers.Ollama.Model) == "" {
		return fmt.Errorf("ollama model is required")
	}

	if req.Risk.MaxPositionSizePct < 0 || req.Risk.MaxPositionSizePct > 100 {
		return fmt.Errorf("max position size must be between 0 and 100")
	}
	if req.Risk.MaxDailyLossPct < 0 || req.Risk.MaxDailyLossPct > 100 {
		return fmt.Errorf("max daily loss must be between 0 and 100")
	}
	if req.Risk.MaxDrawdownPct < 0 || req.Risk.MaxDrawdownPct > 100 {
		return fmt.Errorf("max drawdown must be between 0 and 100")
	}
	if req.Risk.MaxOpenPositions < 0 {
		return fmt.Errorf("max open positions must be non-negative")
	}
	if req.Risk.MaxTotalExposurePct < 0 || req.Risk.MaxTotalExposurePct > 100 {
		return fmt.Errorf("max total exposure must be between 0 and 100")
	}
	if req.Risk.MaxPerMarketExposurePct < 0 || req.Risk.MaxPerMarketExposurePct > 100 {
		return fmt.Errorf("max per market exposure must be between 0 and 100")
	}
	if req.Risk.CircuitBreakerThresholdPct < 0 || req.Risk.CircuitBreakerThresholdPct > 100 {
		return fmt.Errorf("circuit breaker threshold must be between 0 and 100")
	}
	if req.Risk.CircuitBreakerCooldownMin < 0 {
		return fmt.Errorf("circuit breaker cooldown must be non-negative")
	}

	return nil
}

func validateProviderModel(provider, model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("%s model is required", provider)
	}
	return nil
}

func detectBuildVersion() string {
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		version := strings.TrimSpace(buildInfo.Main.Version)
		if version != "" && version != "(devel)" {
			return version
		}
	}
	return "development"
}
