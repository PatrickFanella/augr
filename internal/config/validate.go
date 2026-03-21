package config

import (
	"fmt"
	"strings"
)

// Validate validates the configuration required to start the application.
func Validate(cfg Config) error {
	var errs []string

	if strings.TrimSpace(cfg.Database.URL) == "" {
		errs = append(errs, "DATABASE_URL is required")
	}

	if cfg.Server.Port <= 0 {
		errs = append(errs, "APP_PORT must be greater than 0")
	}

	if cfg.Database.PoolSize <= 0 {
		errs = append(errs, "DATABASE_POOL_SIZE must be greater than 0")
	}

	if cfg.LLM.Timeout <= 0 {
		errs = append(errs, "LLM_TIMEOUT must be greater than 0")
	}

	if !hasLLMProvider(cfg.LLM.Providers) {
		errs = append(errs, "at least one LLM provider must be configured (OPENAI_API_KEY, ANTHROPIC_API_KEY, GOOGLE_API_KEY, OPENROUTER_API_KEY, XAI_API_KEY, or OLLAMA_BASE_URL)")
	}

	if cfg.DataProviders.AlphaVantage.RateLimitPerMinute <= 0 {
		errs = append(errs, "ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE must be greater than 0")
	}

	if cfg.DataProviders.Finnhub.RateLimitPerMinute <= 0 {
		errs = append(errs, "FINNHUB_RATE_LIMIT_PER_MINUTE must be greater than 0")
	}

	validateBrokerCredentials(&errs, "ALPACA_API_KEY", cfg.Brokers.Alpaca.APIKey, "ALPACA_API_SECRET", cfg.Brokers.Alpaca.APISecret)
	validateBrokerCredentials(&errs, "BINANCE_API_KEY", cfg.Brokers.Binance.APIKey, "BINANCE_API_SECRET", cfg.Brokers.Binance.APISecret)

	if cfg.Risk.MaxPositionSizePct <= 0 || cfg.Risk.MaxPositionSizePct > 1 {
		errs = append(errs, "RISK_MAX_POSITION_SIZE_PCT must be between 0 and 1")
	}

	if cfg.Risk.MaxDailyLossPct <= 0 || cfg.Risk.MaxDailyLossPct > 1 {
		errs = append(errs, "RISK_MAX_DAILY_LOSS_PCT must be between 0 and 1")
	}

	if cfg.Risk.MaxDrawdownPct <= 0 || cfg.Risk.MaxDrawdownPct > 1 {
		errs = append(errs, "RISK_MAX_DRAWDOWN_PCT must be between 0 and 1")
	}

	if cfg.Risk.MaxOpenPositions <= 0 {
		errs = append(errs, "RISK_MAX_OPEN_POSITIONS must be greater than 0")
	}

	if cfg.Risk.CircuitBreakerThreshold <= 0 || cfg.Risk.CircuitBreakerThreshold > 1 {
		errs = append(errs, "RISK_CIRCUIT_BREAKER_THRESHOLD must be between 0 and 1")
	}

	if cfg.Risk.CircuitBreakerCooldown <= 0 {
		errs = append(errs, "RISK_CIRCUIT_BREAKER_COOLDOWN must be greater than 0")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(errs, "; "))
	}

	return nil
}

func hasLLMProvider(providers LLMProviderConfigs) bool {
	return strings.TrimSpace(providers.OpenAI.APIKey) != "" ||
		strings.TrimSpace(providers.Anthropic.APIKey) != "" ||
		strings.TrimSpace(providers.Google.APIKey) != "" ||
		strings.TrimSpace(providers.OpenRouter.APIKey) != "" ||
		strings.TrimSpace(providers.XAI.APIKey) != "" ||
		strings.TrimSpace(providers.Ollama.BaseURL) != ""
}

func validateBrokerCredentials(errs *[]string, keyName, keyValue, secretName, secretValue string) {
	hasKey := strings.TrimSpace(keyValue) != ""
	hasSecret := strings.TrimSpace(secretValue) != ""
	if hasKey == hasSecret {
		return
	}

	*errs = append(*errs, fmt.Sprintf("%s and %s must both be set when configuring broker credentials", keyName, secretName))
}
