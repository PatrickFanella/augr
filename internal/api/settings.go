package api

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// SettingsPersister is an optional persistence layer for the MemorySettingsService.
// Implementations store and restore non-secret runtime settings (model selections,
// risk thresholds) so that UI edits survive process restarts.
// API keys are never stored through this interface.
type SettingsPersister interface {
	// Load retrieves persisted settings. Returns zero values without error when
	// no settings have been saved yet.
	Load(ctx context.Context) (domain.LLMPersisted, domain.RiskSettings, error)
	// Save persists the current non-secret settings.
	Save(ctx context.Context, llm domain.LLMPersisted, risk domain.RiskSettings) error
}

// SettingsService provides the editable settings surfaced by the frontend.
type SettingsService interface {
	Get(context.Context) (SettingsResponse, error)
	Update(context.Context, SettingsUpdateRequest) (SettingsResponse, error)
}

// SettingsResponse is the API payload returned to the settings page.
type SettingsResponse struct {
	LLM    LLMSettingsResponse `json:"llm"`
	Risk   domain.RiskSettings `json:"risk"`
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
	OpenAI     LLMProviderResponse   `json:"openai"`
	Anthropic  LLMProviderResponse   `json:"anthropic"`
	Google     LLMProviderResponse   `json:"google"`
	OpenRouter LLMProviderResponse   `json:"openrouter"`
	XAI        LLMProviderResponse   `json:"xai"`
	Ollama     domain.OllamaSettings `json:"ollama"`
}

// LLMProviderResponse represents a provider without exposing the raw secret.
type LLMProviderResponse struct {
	APIKeyConfigured bool   `json:"api_key_configured"`
	APIKeyLast4      string `json:"api_key_last4,omitempty"`
	BaseURL          string `json:"base_url,omitempty"`
	Model            string `json:"model"`
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
	Risk domain.RiskSettings      `json:"risk"`
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
	Ollama     domain.OllamaSettings    `json:"ollama"`
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
	Risk             domain.RiskSettings
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
	Ollama     domain.OllamaSettings
}

type providerState struct {
	APIKey  string
	BaseURL string
	Model   string
}

// MemorySettingsService stores settings in memory for authenticated UI editing.
// If a SettingsPersister is provided, non-secret settings are loaded on startup
// and saved on every successful Update call.
type MemorySettingsService struct {
	mu        sync.RWMutex
	llm       llmSettingsState
	risk      domain.RiskSettings
	system    SystemInfo
	started   time.Time
	persister SettingsPersister
	logger    *slog.Logger
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

// WithPersister attaches a SettingsPersister to the service and immediately
// loads any previously persisted settings, overriding the bootstrap values for
// non-secret fields. The logger is used to warn on load errors without crashing.
// Returns the receiver for chaining.
func (s *MemorySettingsService) WithPersister(ctx context.Context, p SettingsPersister, logger *slog.Logger) *MemorySettingsService {
	s.persister = p
	if logger == nil {
		logger = slog.Default()
	}
	s.logger = logger

	llmP, risk, err := p.Load(ctx)
	if err != nil {
		logger.Warn("settings: failed to load persisted settings, using bootstrap values",
			slog.String("error", err.Error()))
		return s
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply only non-empty persisted values so the service starts correctly
	// even when the DB row was seeded but never written.
	if llmP.DefaultProvider != "" {
		s.llm.DefaultProvider = llmP.DefaultProvider
	}
	if llmP.DeepThinkModel != "" {
		s.llm.DeepThinkModel = llmP.DeepThinkModel
	}
	if llmP.QuickThinkModel != "" {
		s.llm.QuickThinkModel = llmP.QuickThinkModel
	}
	applyPersistedProvider(&s.llm.Providers.OpenAI, llmP.Providers.OpenAI)
	applyPersistedProvider(&s.llm.Providers.Anthropic, llmP.Providers.Anthropic)
	applyPersistedProvider(&s.llm.Providers.Google, llmP.Providers.Google)
	applyPersistedProvider(&s.llm.Providers.OpenRouter, llmP.Providers.OpenRouter)
	applyPersistedProvider(&s.llm.Providers.XAI, llmP.Providers.XAI)

	if llmP.Providers.Ollama.Model != "" {
		s.llm.Providers.Ollama = llmP.Providers.Ollama
	}
	if risk.MaxPositionSizePct > 0 {
		s.risk = risk
	}

	return s
}

func applyPersistedProvider(target *providerState, p domain.ProviderPersisted) {
	if p.BaseURL != "" {
		target.BaseURL = p.BaseURL
	}
	if p.Model != "" {
		target.Model = p.Model
	}
	// API key is deliberately not restored from DB.
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
				Ollama: domain.OllamaSettings{
					BaseURL: cfg.LLM.Providers.Ollama.BaseURL,
					Model:   cfg.LLM.Providers.Ollama.Model,
				},
			},
		},
		Risk: domain.RiskSettings{
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
// Non-secret fields are persisted to the SettingsPersister (if configured) after the in-memory
// update succeeds.
func (s *MemorySettingsService) Update(ctx context.Context, req SettingsUpdateRequest) (SettingsResponse, error) {
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
	s.llm.Providers.Ollama = domain.OllamaSettings{
		BaseURL: strings.TrimSpace(req.LLM.Providers.Ollama.BaseURL),
		Model:   strings.TrimSpace(req.LLM.Providers.Ollama.Model),
	}
	s.risk = req.Risk
	response := s.getLocked()

	// Snapshot non-secret persisted fields while still holding the lock.
	llmP := domain.LLMPersisted{
		DefaultProvider: s.llm.DefaultProvider,
		DeepThinkModel:  s.llm.DeepThinkModel,
		QuickThinkModel: s.llm.QuickThinkModel,
		Providers: domain.LLMProvidersPersisted{
			OpenAI:     domain.ProviderPersisted{BaseURL: s.llm.Providers.OpenAI.BaseURL, Model: s.llm.Providers.OpenAI.Model},
			Anthropic:  domain.ProviderPersisted{Model: s.llm.Providers.Anthropic.Model},
			Google:     domain.ProviderPersisted{Model: s.llm.Providers.Google.Model},
			OpenRouter: domain.ProviderPersisted{BaseURL: s.llm.Providers.OpenRouter.BaseURL, Model: s.llm.Providers.OpenRouter.Model},
			XAI:        domain.ProviderPersisted{BaseURL: s.llm.Providers.XAI.BaseURL, Model: s.llm.Providers.XAI.Model},
			Ollama:     s.llm.Providers.Ollama,
		},
	}
	risk := s.risk
	s.mu.Unlock()

	if s.persister != nil {
		if err := s.persister.Save(ctx, llmP, risk); err != nil {
			logger := s.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Warn("settings: failed to persist update", slog.String("error", err.Error()))
		}
	}

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
