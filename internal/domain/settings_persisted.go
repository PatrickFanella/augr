package domain

// LLMPersisted holds the non-secret LLM settings that are safe to persist to DB.
// API keys are never stored here.
type LLMPersisted struct {
	DefaultProvider string                `json:"default_provider"`
	DeepThinkModel  string                `json:"deep_think_model"`
	QuickThinkModel string                `json:"quick_think_model"`
	Providers       LLMProvidersPersisted `json:"providers"`
}

// LLMProvidersPersisted holds per-provider non-secret settings.
type LLMProvidersPersisted struct {
	OpenAI     ProviderPersisted `json:"openai"`
	Anthropic  ProviderPersisted `json:"anthropic"`
	Google     ProviderPersisted `json:"google"`
	OpenRouter ProviderPersisted `json:"openrouter"`
	XAI        ProviderPersisted `json:"xai"`
	Ollama     OllamaSettings    `json:"ollama"`
}

// ProviderPersisted holds non-secret per-provider settings safe to store in the DB.
type ProviderPersisted struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model"`
}

// OllamaSettings contains local model settings.
type OllamaSettings struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model"`
}

// RiskSettings contains configurable risk thresholds.
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
