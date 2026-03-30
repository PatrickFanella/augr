package agent

import (
	"fmt"
	"strings"
)

// knownLLMModels is the unexported set of model identifiers that
// ValidateStrategyConfig accepts. Use isKnownLLMModel to query it.
var knownLLMModels = map[string]bool{
	// OpenAI
	"gpt-5-mini":          true,
	"gpt-5.2":             true,
	"gpt-5.4":             true,
	"gpt-4.1-mini":        true,
	"openai/gpt-4.1-mini": true,
	// Anthropic
	"claude-3-7-sonnet-latest": true,
	// Google
	"gemini-2.5-flash": true,
	// XAI
	"grok-3-mini": true,
	// Ollama
	"llama3.2": true,
}

// isKnownLLMModel reports whether model is in the allowlist.
func isKnownLLMModel(model string) bool {
	return knownLLMModels[model]
}

// knownLLMProviders is the set of provider identifiers that
// ValidateStrategyConfig accepts for StrategyLLMConfig.Provider.
var knownLLMProviders = map[string]bool{
	"openai":     true,
	"anthropic":  true,
	"google":     true,
	"openrouter": true,
	"xai":        true,
	"ollama":     true,
}

// providerModelAllowlist maps provider names to their accepted model identifiers.
// Providers omitted from this map (openrouter, xai, ollama) are unconstrained and
// may route to many underlying models; their model names are not further validated.
var providerModelAllowlist = map[string]map[string]bool{
	"openai": {
		"gpt-5-mini":          true,
		"gpt-5.2":             true,
		"gpt-5.4":             true,
		"gpt-4.1-mini":        true,
		"openai/gpt-4.1-mini": true,
	},
	"anthropic": {
		"claude-3-7-sonnet-latest": true,
	},
	"google": {
		"gemini-2.5-flash": true,
	},
}

// isModelValidForProvider reports whether model is valid for the given provider.
// For providers without an explicit allowlist (openrouter, xai, ollama) it returns true.
func isModelValidForProvider(provider, model string) bool {
	if models, ok := providerModelAllowlist[provider]; ok {
		return models[model]
	}
	return true
}

// StrategyLLMConfig holds per-tier model overrides for a strategy.
type StrategyLLMConfig struct {
	// Provider overrides the default LLM provider (e.g. "openai", "anthropic").
	Provider *string `json:"provider,omitempty"`
	// DeepThinkModel overrides the model used for deep-reasoning tasks.
	DeepThinkModel *string `json:"deep_think_model,omitempty"`
	// QuickThinkModel overrides the model used for fast-response tasks.
	QuickThinkModel *string `json:"quick_think_model,omitempty"`
}

// StrategyPipelineConfig holds debate and timeout configuration for a strategy.
type StrategyPipelineConfig struct {
	// DebateRounds is the number of research/risk debate rounds (must be >= 1 if set).
	DebateRounds *int `json:"debate_rounds,omitempty"`
	// AnalysisTimeoutSeconds is the per-agent analysis timeout in seconds.
	AnalysisTimeoutSeconds *int `json:"analysis_timeout_seconds,omitempty"`
	// DebateTimeoutSeconds is the per-round debate timeout in seconds.
	DebateTimeoutSeconds *int `json:"debate_timeout_seconds,omitempty"`
}

// StrategyRiskConfig holds position-sizing and risk-limit overrides for a strategy.
type StrategyRiskConfig struct {
	// PositionSizePct is the fraction of portfolio to allocate (0–100, percent).
	PositionSizePct *float64 `json:"position_size_pct,omitempty"`
	// StopLossMultiplier scales the default stop-loss distance (must be > 0 if set).
	StopLossMultiplier *float64 `json:"stop_loss_multiplier,omitempty"`
	// TakeProfitMultiplier scales the default take-profit distance (must be > 0 if set).
	TakeProfitMultiplier *float64 `json:"take_profit_multiplier,omitempty"`
	// MinConfidence is the minimum signal confidence required to enter a trade (0–1).
	MinConfidence *float64 `json:"min_confidence,omitempty"`
}

// StrategyConfig is the strongly-typed representation of the strategy_config JSONB
// column. All sub-sections are optional (pointer fields); a nil pointer means
// "use the system default". AnalystSelection and PromptOverrides are slices/maps
// whose nil zero-values already carry the "use default" semantic.
type StrategyConfig struct {
	// LLMConfig overrides LLM provider and model selection for this strategy.
	LLMConfig *StrategyLLMConfig `json:"llm_config,omitempty"`
	// PipelineConfig overrides debate rounds and timeouts for this strategy.
	PipelineConfig *StrategyPipelineConfig `json:"pipeline_config,omitempty"`
	// RiskConfig overrides position-sizing and risk limits for this strategy.
	RiskConfig *StrategyRiskConfig `json:"risk_config,omitempty"`
	// AnalystSelection restricts which analyst roles run in the pipeline.
	// A nil slice means all analysts are enabled.
	AnalystSelection []AgentRole `json:"analyst_selection,omitempty"`
	// PromptOverrides replaces the default system prompt for specific roles.
	// A nil map means no overrides are applied.
	PromptOverrides map[AgentRole]string `json:"prompt_overrides,omitempty"`
}

// ValidateStrategyConfig checks that all fields within cfg contain valid values.
// It returns an error describing the first invalid field encountered, or nil if
// the config is fully valid.
func ValidateStrategyConfig(cfg StrategyConfig) error {
	if err := validateLLMConfig(cfg.LLMConfig); err != nil {
		return err
	}
	if err := validatePipelineConfig(cfg.PipelineConfig); err != nil {
		return err
	}
	if err := validateRiskConfig(cfg.RiskConfig); err != nil {
		return err
	}
	for i, role := range cfg.AnalystSelection {
		if !role.IsValid() {
			return fmt.Errorf("analyst_selection[%d]: unknown agent role %q", i, role)
		}
	}
	for role := range cfg.PromptOverrides {
		if !role.IsValid() {
			return fmt.Errorf("prompt_overrides: unknown agent role %q", role)
		}
	}
	return nil
}

func validateLLMConfig(c *StrategyLLMConfig) error {
	if c == nil {
		return nil
	}
	var provider string
	if c.Provider != nil {
		provider = strings.ToLower(strings.TrimSpace(*c.Provider))
		if !knownLLMProviders[provider] {
			return fmt.Errorf("llm_config.provider: unknown provider %q (valid: openai, anthropic, google, openrouter, xai, ollama)", provider)
		}
	}
	if c.DeepThinkModel != nil {
		model := strings.TrimSpace(*c.DeepThinkModel)
		if !isKnownLLMModel(model) {
			return fmt.Errorf("llm_config.deep_think_model: unknown model %q", model)
		}
		if provider != "" && !isModelValidForProvider(provider, model) {
			return fmt.Errorf("llm_config.deep_think_model: model %q is not valid for provider %q", model, provider)
		}
	}
	if c.QuickThinkModel != nil {
		model := strings.TrimSpace(*c.QuickThinkModel)
		if !isKnownLLMModel(model) {
			return fmt.Errorf("llm_config.quick_think_model: unknown model %q", model)
		}
		if provider != "" && !isModelValidForProvider(provider, model) {
			return fmt.Errorf("llm_config.quick_think_model: model %q is not valid for provider %q", model, provider)
		}
	}
	return nil
}

func validatePipelineConfig(c *StrategyPipelineConfig) error {
	if c == nil {
		return nil
	}
	if c.DebateRounds != nil && *c.DebateRounds < 1 {
		return fmt.Errorf("pipeline_config.debate_rounds: must be >= 1, got %d", *c.DebateRounds)
	}
	if c.AnalysisTimeoutSeconds != nil && *c.AnalysisTimeoutSeconds < 1 {
		return fmt.Errorf("pipeline_config.analysis_timeout_seconds: must be >= 1, got %d", *c.AnalysisTimeoutSeconds)
	}
	if c.DebateTimeoutSeconds != nil && *c.DebateTimeoutSeconds < 1 {
		return fmt.Errorf("pipeline_config.debate_timeout_seconds: must be >= 1, got %d", *c.DebateTimeoutSeconds)
	}
	return nil
}

func validateRiskConfig(c *StrategyRiskConfig) error {
	if c == nil {
		return nil
	}
	if c.PositionSizePct != nil && (*c.PositionSizePct < 0 || *c.PositionSizePct > 100) {
		return fmt.Errorf("risk_config.position_size_pct: must be in [0, 100], got %g", *c.PositionSizePct)
	}
	if c.StopLossMultiplier != nil && *c.StopLossMultiplier <= 0 {
		return fmt.Errorf("risk_config.stop_loss_multiplier: must be > 0, got %g", *c.StopLossMultiplier)
	}
	if c.TakeProfitMultiplier != nil && *c.TakeProfitMultiplier <= 0 {
		return fmt.Errorf("risk_config.take_profit_multiplier: must be > 0, got %g", *c.TakeProfitMultiplier)
	}
	if c.MinConfidence != nil && (*c.MinConfidence < 0 || *c.MinConfidence > 1) {
		return fmt.Errorf("risk_config.min_confidence: must be in [0, 1], got %g", *c.MinConfidence)
	}
	return nil
}


