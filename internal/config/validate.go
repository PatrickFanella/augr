package config

import (
	"fmt"
	"net/url"
	"slices"
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

	if !hasDataProvider(cfg.DataProviders) {
		errs = append(errs, "at least one data provider must be configured (POLYGON_API_KEY, ALPHA_VANTAGE_API_KEY, or FINNHUB_API_KEY)")
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

	validateNotificationConfig(&errs, cfg.Notifications)

	if cfg.LLM.DeepThinkModel != "" && strings.TrimSpace(cfg.LLM.DeepThinkModel) == "" {
		errs = append(errs, "LLM_DEEP_THINK_MODEL must not be whitespace-only when set")
	}
	if cfg.LLM.QuickThinkModel != "" && strings.TrimSpace(cfg.LLM.QuickThinkModel) == "" {
		errs = append(errs, "LLM_QUICK_THINK_MODEL must not be whitespace-only when set")
	}

	// Cross-field: live trading requires at least one fully configured broker.
	if cfg.Features.EnableLiveTrading {
		hasAlpaca := strings.TrimSpace(cfg.Brokers.Alpaca.APIKey) != "" &&
			strings.TrimSpace(cfg.Brokers.Alpaca.APISecret) != ""
		hasBinance := strings.TrimSpace(cfg.Brokers.Binance.APIKey) != "" &&
			strings.TrimSpace(cfg.Brokers.Binance.APISecret) != ""
		if !hasAlpaca && !hasBinance {
			errs = append(errs, "ENABLE_LIVE_TRADING requires at least one broker (Alpaca or Binance) to be fully configured")
		}
	}

	// Cross-field: selected default LLM provider must have its API key set.
	if msg := validateSelectedProvider(cfg.LLM); msg != "" {
		errs = append(errs, msg)
	}

	// Database URL must be parseable.
	if cfg.Database.URL != "" {
		if _, err := url.Parse(cfg.Database.URL); err != nil {
			errs = append(errs, fmt.Sprintf("DATABASE_URL is not a valid URL: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(errs, "; "))
	}

	return nil
}

func hasDataProvider(providers DataProviderConfigs) bool {
	return strings.TrimSpace(providers.Polygon.APIKey) != "" ||
		strings.TrimSpace(providers.AlphaVantage.APIKey) != "" ||
		strings.TrimSpace(providers.Finnhub.APIKey) != ""
}

func hasLLMProvider(providers LLMProviderConfigs) bool {
	return strings.TrimSpace(providers.OpenAI.APIKey) != "" ||
		strings.TrimSpace(providers.Anthropic.APIKey) != "" ||
		strings.TrimSpace(providers.Google.APIKey) != "" ||
		strings.TrimSpace(providers.OpenRouter.APIKey) != "" ||
		strings.TrimSpace(providers.XAI.APIKey) != "" ||
		strings.TrimSpace(providers.Ollama.BaseURL) != ""
}

func validateSelectedProvider(llmCfg LLMConfig) string {
	provider := strings.TrimSpace(strings.ToLower(llmCfg.DefaultProvider))
	if provider == "" {
		return ""
	}
	switch provider {
	case "openai":
		if strings.TrimSpace(llmCfg.Providers.OpenAI.APIKey) == "" {
			return "LLM_DEFAULT_PROVIDER is openai but OPENAI_API_KEY is not set"
		}
	case "anthropic":
		if strings.TrimSpace(llmCfg.Providers.Anthropic.APIKey) == "" {
			return "LLM_DEFAULT_PROVIDER is anthropic but ANTHROPIC_API_KEY is not set"
		}
	case "google":
		if strings.TrimSpace(llmCfg.Providers.Google.APIKey) == "" {
			return "LLM_DEFAULT_PROVIDER is google but GOOGLE_API_KEY is not set"
		}
	case "openrouter":
		if strings.TrimSpace(llmCfg.Providers.OpenRouter.APIKey) == "" {
			return "LLM_DEFAULT_PROVIDER is openrouter but OPENROUTER_API_KEY is not set"
		}
	case "xai":
		if strings.TrimSpace(llmCfg.Providers.XAI.APIKey) == "" {
			return "LLM_DEFAULT_PROVIDER is xai but XAI_API_KEY is not set"
		}
	case "ollama":
		// Ollama doesn't require an API key.
	}
	return ""
}

func validateBrokerCredentials(errs *[]string, keyName, keyValue, secretName, secretValue string) {
	hasKey := strings.TrimSpace(keyValue) != ""
	hasSecret := strings.TrimSpace(secretValue) != ""
	if hasKey == hasSecret {
		return
	}

	*errs = append(*errs, fmt.Sprintf("%s and %s must both be set when configuring broker credentials", keyName, secretName))
}

func validateNotificationConfig(errs *[]string, cfg NotificationConfig) {
	if hasAnyNotificationEmailField(cfg.Email) {
		if strings.TrimSpace(cfg.Email.SMTPHost) == "" {
			*errs = append(*errs, "NOTIFY_SMTP_HOST is required when email notifications are configured")
		}
		if cfg.Email.SMTPPort <= 0 {
			*errs = append(*errs, "NOTIFY_SMTP_PORT must be greater than 0 when email notifications are configured")
		}
		if strings.TrimSpace(cfg.Email.From) == "" {
			*errs = append(*errs, "NOTIFY_EMAIL_FROM is required when email notifications are configured")
		}
		if len(cfg.Email.To) == 0 {
			*errs = append(*errs, "NOTIFY_EMAIL_TO must include at least one recipient when email notifications are configured")
		}
	}

	if hasAnyTelegramField(cfg.Telegram) {
		if strings.TrimSpace(cfg.Telegram.BotToken) == "" {
			*errs = append(*errs, "NOTIFY_TELEGRAM_BOT_TOKEN is required when Telegram notifications are configured")
		}
		if strings.TrimSpace(cfg.Telegram.ChatID) == "" {
			*errs = append(*errs, "NOTIFY_TELEGRAM_CHAT_ID is required when Telegram notifications are configured")
		}
	}

	validateWebhookNotification(errs, cfg.N8N, "N8N_WEBHOOK_URL")
	validateWebhookNotification(errs, cfg.PagerDuty, "NOTIFY_PAGERDUTY_WEBHOOK_URL")
	validateDiscordNotification(errs, cfg.Discord)
	validateAlertRules(errs, cfg.Alerts)
}

func hasAnyNotificationEmailField(cfg EmailNotificationConfig) bool {
	return strings.TrimSpace(cfg.SMTPHost) != "" ||
		strings.TrimSpace(cfg.Username) != "" ||
		strings.TrimSpace(cfg.Password) != "" ||
		strings.TrimSpace(cfg.From) != "" ||
		len(cfg.To) > 0
}

func hasAnyTelegramField(cfg TelegramNotificationConfig) bool {
	return strings.TrimSpace(cfg.BotToken) != "" || strings.TrimSpace(cfg.ChatID) != ""
}

func validateWebhookNotification(errs *[]string, cfg WebhookNotificationConfig, envName string) {
	validateURLIfSet(errs, cfg.URL, envName)
}

func validateDiscordNotification(errs *[]string, cfg DiscordNotificationConfig) {
	validateURLIfSet(errs, cfg.SignalWebhookURL, "Discord signal webhook URL")
	validateURLIfSet(errs, cfg.DecisionWebhookURL, "Discord decision webhook URL")
	validateURLIfSet(errs, cfg.AlertWebhookURL, "Discord alert webhook URL")
}

func validateURLIfSet(errs *[]string, rawURL, envName string) {
	if strings.TrimSpace(rawURL) == "" {
		return
	}
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		*errs = append(*errs, fmt.Sprintf("%s must be a valid URL: %v", envName, err))
	}
}

func validateAlertRules(errs *[]string, cfg AlertRulesConfig) {
	if cfg.PipelineFailure.Threshold <= 0 {
		*errs = append(*errs, "ALERT_PIPELINE_FAILURE_THRESHOLD must be greater than 0")
	}
	if cfg.LLMProviderDown.ErrorRateThreshold <= 0 || cfg.LLMProviderDown.ErrorRateThreshold > 1 {
		*errs = append(*errs, "ALERT_LLM_PROVIDER_DOWN_ERROR_RATE_THRESHOLD must be between 0 and 1")
	}
	if cfg.LLMProviderDown.Window <= 0 {
		*errs = append(*errs, "ALERT_LLM_PROVIDER_DOWN_WINDOW must be greater than 0")
	}
	if cfg.HighLatency.Threshold <= 0 {
		*errs = append(*errs, "ALERT_HIGH_LATENCY_THRESHOLD must be greater than 0")
	}

	for envName, channels := range map[string][]string{
		"ALERT_PIPELINE_FAILURE_CHANNELS":  cfg.PipelineFailure.Channels,
		"ALERT_CIRCUIT_BREAKER_CHANNELS":   cfg.CircuitBreaker.Channels,
		"ALERT_LLM_PROVIDER_DOWN_CHANNELS": cfg.LLMProviderDown.Channels,
		"ALERT_HIGH_LATENCY_CHANNELS":      cfg.HighLatency.Channels,
		"ALERT_KILL_SWITCH_CHANNELS":       cfg.KillSwitch.Channels,
		"ALERT_DB_CONNECTION_CHANNELS":     cfg.DBConnection.Channels,
	} {
		for _, channel := range channels {
			if !slices.Contains([]string{"telegram", "email", "n8n", "pagerduty", "discord"}, strings.ToLower(strings.TrimSpace(channel))) {
				*errs = append(*errs, fmt.Sprintf("%s contains unsupported channel %q", envName, channel))
			}
		}
	}
}
