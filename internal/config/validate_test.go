package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadParsesEnvironmentValues(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "https://openai.example.com/v1")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	t.Setenv("OPENROUTER_BASE_URL", "https://openrouter.example.com/api/v1")
	t.Setenv("XAI_BASE_URL", "https://xai.example.com/v1")
	t.Setenv("APP_HOST", "127.0.0.1")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("JWT_SECRET", "super-secret")
	t.Setenv("DATABASE_POOL_SIZE", "25")
	t.Setenv("DATABASE_SSL_MODE", "require")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("LLM_TIMEOUT", "45s")
	t.Setenv("LLM_FALLBACK_PROVIDER", "openrouter")
	t.Setenv("LLM_FALLBACK_MODEL", "openai/gpt-4.1-mini")
	t.Setenv("LLM_RETRY_MAX_ATTEMPTS", "3")
	t.Setenv("LLM_CALL_TIMEOUT", "90s")
	t.Setenv("LLM_BUDGET_REQUESTS_DAY", "123")
	t.Setenv("LLM_BUDGET_TOKENS_DAY", "456789")
	t.Setenv("LLM_THROTTLE_CONCURRENCY", "8")
	t.Setenv("POLYGON_API_KEY", "polygon-key")
	t.Setenv("ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE", "7")
	t.Setenv("FINNHUB_RATE_LIMIT_PER_MINUTE", "20")
	t.Setenv("ALPACA_PAPER_MODE", "false")
	t.Setenv("NOTIFY_TELEGRAM_BOT_TOKEN", "telegram-token")
	t.Setenv("NOTIFY_TELEGRAM_CHAT_ID", "12345")
	t.Setenv("NOTIFY_SMTP_HOST", "smtp.example.com")
	t.Setenv("NOTIFY_SMTP_PORT", "2525")
	t.Setenv("NOTIFY_SMTP_USERNAME", "smtp-user")
	t.Setenv("NOTIFY_SMTP_PASSWORD", "smtp-pass")
	t.Setenv("NOTIFY_EMAIL_FROM", "alerts@example.com")
	t.Setenv("NOTIFY_EMAIL_TO", "ops@example.com,dev@example.com")
	t.Setenv("N8N_WEBHOOK_URL", "https://hooks.example.com/alerts")
	t.Setenv("N8N_WEBHOOK_SECRET", "webhook-secret")
	t.Setenv("NOTIFY_PAGERDUTY_WEBHOOK_URL", "https://events.pagerduty.com/v2/enqueue")
	t.Setenv("ALERT_PIPELINE_FAILURE_THRESHOLD", "4")
	t.Setenv("ALERT_PIPELINE_FAILURE_CHANNELS", "telegram,email")
	t.Setenv("ALERT_LLM_PROVIDER_DOWN_ERROR_RATE_THRESHOLD", "0.75")
	t.Setenv("ALERT_LLM_PROVIDER_DOWN_WINDOW", "10m")
	t.Setenv("ALERT_HIGH_LATENCY_THRESHOLD", "2m30s")
	t.Setenv("ALERT_DB_CONNECTION_CHANNELS", "email,pagerduty")
	t.Setenv("ENABLE_SCHEDULER", "true")
	t.Setenv("ENABLE_REDIS_CACHE", "false")
	t.Setenv("LIVE_TRADING_ALLOWED_STRATEGIES", "11111111-1111-1111-1111-111111111111,22222222-2222-2222-2222-222222222222")
	t.Setenv("LIVE_TRADING_ALLOWED_BROKERS", "Alpaca,Binance")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("cfg.Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}

	if cfg.Server.Port != 9090 {
		t.Fatalf("cfg.Server.Port = %d, want %d", cfg.Server.Port, 9090)
	}

	if cfg.Server.JWTSecret != "super-secret" {
		t.Fatalf("cfg.Server.JWTSecret = %q, want %q", cfg.Server.JWTSecret, "super-secret")
	}

	if cfg.Database.PoolSize != 25 {
		t.Fatalf("cfg.Database.PoolSize = %d, want %d", cfg.Database.PoolSize, 25)
	}

	if cfg.Database.SSLMode != "require" {
		t.Fatalf("cfg.Database.SSLMode = %q, want %q", cfg.Database.SSLMode, "require")
	}

	if cfg.LLM.Timeout != 45*time.Second {
		t.Fatalf("cfg.LLM.Timeout = %s, want %s", cfg.LLM.Timeout, 45*time.Second)
	}
	if cfg.LLM.FallbackProvider != "openrouter" {
		t.Fatalf("cfg.LLM.FallbackProvider = %q, want %q", cfg.LLM.FallbackProvider, "openrouter")
	}
	if cfg.LLM.FallbackModel != "openai/gpt-4.1-mini" {
		t.Fatalf("cfg.LLM.FallbackModel = %q, want %q", cfg.LLM.FallbackModel, "openai/gpt-4.1-mini")
	}
	if cfg.LLM.RetryMaxAttempts != 3 {
		t.Fatalf("cfg.LLM.RetryMaxAttempts = %d, want %d", cfg.LLM.RetryMaxAttempts, 3)
	}
	if cfg.LLM.CallTimeout != 90*time.Second {
		t.Fatalf("cfg.LLM.CallTimeout = %s, want %s", cfg.LLM.CallTimeout, 90*time.Second)
	}
	if cfg.LLM.BudgetRequestsPerDay != 123 {
		t.Fatalf("cfg.LLM.BudgetRequestsPerDay = %d, want %d", cfg.LLM.BudgetRequestsPerDay, 123)
	}
	if cfg.LLM.BudgetTokensPerDay != 456789 {
		t.Fatalf("cfg.LLM.BudgetTokensPerDay = %d, want %d", cfg.LLM.BudgetTokensPerDay, 456789)
	}
	if cfg.LLM.ThrottleConcurrency != 8 {
		t.Fatalf("cfg.LLM.ThrottleConcurrency = %d, want %d", cfg.LLM.ThrottleConcurrency, 8)
	}

	if cfg.LLM.Providers.OpenAI.BaseURL != "https://openai.example.com/v1" {
		t.Fatalf("cfg.LLM.Providers.OpenAI.BaseURL = %q, want %q", cfg.LLM.Providers.OpenAI.BaseURL, "https://openai.example.com/v1")
	}

	if cfg.LLM.Providers.OpenRouter.BaseURL != "https://openrouter.example.com/api/v1" {
		t.Fatalf("cfg.LLM.Providers.OpenRouter.BaseURL = %q, want %q", cfg.LLM.Providers.OpenRouter.BaseURL, "https://openrouter.example.com/api/v1")
	}

	if cfg.LLM.Providers.XAI.BaseURL != "https://xai.example.com/v1" {
		t.Fatalf("cfg.LLM.Providers.XAI.BaseURL = %q, want %q", cfg.LLM.Providers.XAI.BaseURL, "https://xai.example.com/v1")
	}

	if cfg.DataProviders.AlphaVantage.RateLimitPerMinute != 7 {
		t.Fatalf("cfg.DataProviders.AlphaVantage.RateLimitPerMinute = %d, want %d", cfg.DataProviders.AlphaVantage.RateLimitPerMinute, 7)
	}

	if cfg.DataProviders.Polygon.APIKey != "polygon-key" {
		t.Fatalf("cfg.DataProviders.Polygon.APIKey = %q, want %q", cfg.DataProviders.Polygon.APIKey, "polygon-key")
	}

	if cfg.DataProviders.Finnhub.RateLimitPerMinute != 20 {
		t.Fatalf("cfg.DataProviders.Finnhub.RateLimitPerMinute = %d, want %d", cfg.DataProviders.Finnhub.RateLimitPerMinute, 20)
	}

	if cfg.Brokers.Alpaca.PaperMode {
		t.Fatal("cfg.Brokers.Alpaca.PaperMode = true, want false")
	}

	if cfg.Notifications.Telegram.BotToken != "telegram-token" {
		t.Fatalf("cfg.Notifications.Telegram.BotToken = %q, want %q", cfg.Notifications.Telegram.BotToken, "telegram-token")
	}

	if cfg.Notifications.Email.SMTPPort != 2525 {
		t.Fatalf("cfg.Notifications.Email.SMTPPort = %d, want %d", cfg.Notifications.Email.SMTPPort, 2525)
	}

	if len(cfg.Notifications.Email.To) != 2 {
		t.Fatalf("len(cfg.Notifications.Email.To) = %d, want %d", len(cfg.Notifications.Email.To), 2)
	}

	if cfg.Notifications.N8N.URL != "https://hooks.example.com/alerts" {
		t.Fatalf("cfg.Notifications.N8N.URL = %q, want %q", cfg.Notifications.N8N.URL, "https://hooks.example.com/alerts")
	}
	if cfg.Notifications.N8N.Secret != "webhook-secret" {
		t.Fatalf("cfg.Notifications.N8N.Secret = %q, want %q", cfg.Notifications.N8N.Secret, "webhook-secret")
	}

	if cfg.Notifications.Alerts.PipelineFailure.Threshold != 4 {
		t.Fatalf("cfg.Notifications.Alerts.PipelineFailure.Threshold = %d, want %d", cfg.Notifications.Alerts.PipelineFailure.Threshold, 4)
	}

	if cfg.Notifications.Alerts.LLMProviderDown.ErrorRateThreshold != 0.75 {
		t.Fatalf("cfg.Notifications.Alerts.LLMProviderDown.ErrorRateThreshold = %f, want %f", cfg.Notifications.Alerts.LLMProviderDown.ErrorRateThreshold, 0.75)
	}

	if cfg.Notifications.Alerts.LLMProviderDown.Window != 10*time.Minute {
		t.Fatalf("cfg.Notifications.Alerts.LLMProviderDown.Window = %s, want %s", cfg.Notifications.Alerts.LLMProviderDown.Window, 10*time.Minute)
	}

	if cfg.Notifications.Alerts.HighLatency.Threshold != 150*time.Second {
		t.Fatalf("cfg.Notifications.Alerts.HighLatency.Threshold = %s, want %s", cfg.Notifications.Alerts.HighLatency.Threshold, 150*time.Second)
	}

	if !cfg.Features.EnableScheduler {
		t.Fatal("cfg.Features.EnableScheduler = false, want true")
	}

	if cfg.Features.EnableRedisCache {
		t.Fatal("cfg.Features.EnableRedisCache = true, want false")
	}

	if len(cfg.LiveTradingAllowedStrategies) != 2 {
		t.Fatalf("len(cfg.LiveTradingAllowedStrategies) = %d, want %d", len(cfg.LiveTradingAllowedStrategies), 2)
	}
	if cfg.LiveTradingAllowedStrategies[0] != "11111111-1111-1111-1111-111111111111" || cfg.LiveTradingAllowedStrategies[1] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("cfg.LiveTradingAllowedStrategies = %v, want parsed UUID strings", cfg.LiveTradingAllowedStrategies)
	}
	if len(cfg.LiveTradingAllowedBrokers) != 2 {
		t.Fatalf("len(cfg.LiveTradingAllowedBrokers) = %d, want %d", len(cfg.LiveTradingAllowedBrokers), 2)
	}
	if cfg.LiveTradingAllowedBrokers[0] != "Alpaca" || cfg.LiveTradingAllowedBrokers[1] != "Binance" {
		t.Fatalf("cfg.LiveTradingAllowedBrokers = %v, want parsed broker labels", cfg.LiveTradingAllowedBrokers)
	}
}

func TestLoadReturnsTypeConversionErrors(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("APP_PORT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "APP_PORT must be an integer") {
		t.Fatalf("Load() error = %q, want APP_PORT parse message", err)
	}
}

func TestLoadAppliesResilienceDefaults(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("POLYGON_API_KEY", "test-polygon-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.LLM.FallbackProvider != "" {
		t.Fatalf("cfg.LLM.FallbackProvider = %q, want empty", cfg.LLM.FallbackProvider)
	}
	if cfg.LLM.FallbackModel != "" {
		t.Fatalf("cfg.LLM.FallbackModel = %q, want empty", cfg.LLM.FallbackModel)
	}
	if cfg.LLM.RetryMaxAttempts != 2 {
		t.Fatalf("cfg.LLM.RetryMaxAttempts = %d, want %d", cfg.LLM.RetryMaxAttempts, 2)
	}
	if cfg.LLM.CallTimeout != 5*time.Minute {
		t.Fatalf("cfg.LLM.CallTimeout = %s, want %s", cfg.LLM.CallTimeout, 5*time.Minute)
	}
	if cfg.LLM.BudgetRequestsPerDay != 0 {
		t.Fatalf("cfg.LLM.BudgetRequestsPerDay = %d, want %d", cfg.LLM.BudgetRequestsPerDay, 0)
	}
	if cfg.LLM.BudgetTokensPerDay != 0 {
		t.Fatalf("cfg.LLM.BudgetTokensPerDay = %d, want %d", cfg.LLM.BudgetTokensPerDay, 0)
	}
	if cfg.LLM.ThrottleConcurrency != 4 {
		t.Fatalf("cfg.LLM.ThrottleConcurrency = %d, want %d", cfg.LLM.ThrottleConcurrency, 4)
	}
	if len(cfg.LiveTradingAllowedStrategies) != 0 {
		t.Fatalf("len(cfg.LiveTradingAllowedStrategies) = %d, want 0", len(cfg.LiveTradingAllowedStrategies))
	}
	if len(cfg.LiveTradingAllowedBrokers) != 0 {
		t.Fatalf("len(cfg.LiveTradingAllowedBrokers) = %d, want 0", len(cfg.LiveTradingAllowedBrokers))
	}
}

func TestValidateRequiresDatabaseURL(t *testing.T) {
	cfg := validConfig()
	cfg.Database.URL = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("Validate() error = %q, want DATABASE_URL message", err)
	}
}

func TestValidateRequiresLLMAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.Providers.OpenAI.APIKey = ""
	cfg.LLM.Providers.Anthropic.APIKey = ""
	cfg.LLM.Providers.Google.APIKey = ""
	cfg.LLM.Providers.OpenRouter.APIKey = ""
	cfg.LLM.Providers.XAI.APIKey = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "at least one LLM provider must be configured") {
		t.Fatalf("Validate() error = %q, want LLM provider message", err)
	}
}

func TestValidateOllamaSelectedWithoutAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DefaultProvider = "ollama"
	cfg.LLM.Providers.Ollama.BaseURL = "http://localhost:11434/v1"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_DEFAULT_PROVIDER is ollama but OLLAMA_API_KEY is not set") {
		t.Fatalf("Validate() error = %q, want Ollama API key message", err)
	}
}

func TestValidateLiveTradingRequiresBroker(t *testing.T) {
	cfg := validConfig()
	cfg.Features.EnableLiveTrading = true
	cfg.Brokers.Alpaca.APIKey = ""
	cfg.Brokers.Alpaca.APISecret = ""
	cfg.Brokers.Binance.APIKey = ""
	cfg.Brokers.Binance.APISecret = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "ENABLE_LIVE_TRADING requires") {
		t.Fatalf("Validate() error = %q, want ENABLE_LIVE_TRADING message", err)
	}
}

func TestValidateLiveTradingAllowedWithAlpaca(t *testing.T) {
	cfg := validConfig()
	cfg.Features.EnableLiveTrading = true
	cfg.Brokers.Alpaca.APIKey = "key"
	cfg.Brokers.Alpaca.APISecret = "secret"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateLiveTradingAllowedWithPolymarket(t *testing.T) {
	cfg := validConfig()
	cfg.Features.EnableLiveTrading = true
	cfg.Brokers.Polymarket.Address = "0x0000000000000000000000000000000000000001"
	cfg.Brokers.Polymarket.KeyID = "key-id"
	cfg.Brokers.Polymarket.SecretKey = "secret-key"
	cfg.Brokers.Polymarket.Passphrase = "passphrase"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateDefaultProviderMustHaveKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DefaultProvider = "anthropic"
	cfg.LLM.Providers.Anthropic.APIKey = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_DEFAULT_PROVIDER is anthropic but ANTHROPIC_API_KEY is not set") {
		t.Fatalf("Validate() error = %q, want provider key message", err)
	}
}

func TestValidateDefaultProviderOllamaWithBaseURLAndAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DefaultProvider = "ollama"
	cfg.LLM.Providers.Ollama.BaseURL = "http://localhost:11434/v1"
	cfg.LLM.Providers.Ollama.APIKey = "ollama-key"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateDefaultProviderUnknown(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DefaultProvider = "deepseek"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_DEFAULT_PROVIDER") || !strings.Contains(err.Error(), "not a known provider") {
		t.Fatalf("Validate() error = %q, want unknown default provider message", err)
	}
}

func TestLoadFloat64Field_ValidValue(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("POLYGON_API_KEY", "test-polygon-key")
	t.Setenv("RISK_MAX_POSITION_SIZE_PCT", "0.25")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Risk.MaxPositionSizePct != 0.25 {
		t.Fatalf("cfg.Risk.MaxPositionSizePct = %f, want %f", cfg.Risk.MaxPositionSizePct, 0.25)
	}
}

func TestLoadFloat64Field_InvalidReturnsError(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("RISK_MAX_POSITION_SIZE_PCT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "must be a number") {
		t.Fatalf("Load() error = %q, want 'must be a number' message", err)
	}
}

func TestSetDefaultLogger_ReturnsNonNil(t *testing.T) {
	logger := SetDefaultLogger("development", "debug")
	if logger == nil {
		t.Fatal("SetDefaultLogger() returned nil, want non-nil logger")
	}
}

func TestSetDefaultLogger_ProductionJSON(t *testing.T) {
	logger := SetDefaultLogger("production", "info")
	if logger == nil {
		t.Fatal("SetDefaultLogger() returned nil, want non-nil logger")
	}
}

func TestLoadDotEnv_NonDevDoesNotFail(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("POLYGON_API_KEY", "test-polygon-key")

	_, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (loadDotEnv should skip in non-dev)", err)
	}
}

func validConfig() Config {
	return Config{
		Environment: "test",
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		Database: DatabaseConfig{
			URL:      "postgres://postgres:postgres@localhost:5432/tradingagent?sslmode=disable",
			PoolSize: 10,
			SSLMode:  "disable",
		},
		LLM: LLMConfig{
			Timeout: 30 * time.Second,
			Providers: LLMProviderConfigs{
				OpenAI: LLMProviderConfig{
					APIKey: "test-key",
				},
			},
			RetryMaxAttempts:    2,
			CallTimeout:         5 * time.Minute,
			ThrottleConcurrency: 4,
		},
		DataProviders: DataProviderConfigs{
			Polygon:      DataProviderConfig{APIKey: "test-polygon-key"},
			AlphaVantage: DataProviderConfig{RateLimitPerMinute: 5},
			Finnhub:      DataProviderConfig{RateLimitPerMinute: 60},
		},
		Risk: RiskConfig{
			MaxPositionSizePct:      0.10,
			MaxDailyLossPct:         0.02,
			MaxDrawdownPct:          0.10,
			MaxOpenPositions:        10,
			CircuitBreakerThreshold: 0.05,
			CircuitBreakerCooldown:  15 * time.Minute,
		},
		Notifications: NotificationConfig{
			Alerts: AlertRulesConfig{
				PipelineFailure: PipelineFailureAlertRuleConfig{
					Threshold: 3,
					Channels:  []string{"telegram", "email"},
				},
				CircuitBreaker: ImmediateAlertRuleConfig{
					Channels: []string{"telegram"},
				},
				LLMProviderDown: LLMProviderDownAlertRuleConfig{
					ErrorRateThreshold: 0.5,
					Window:             5 * time.Minute,
					Channels:           []string{"telegram"},
				},
				HighLatency: HighLatencyAlertRuleConfig{
					Threshold: 120 * time.Second,
					Channels:  []string{"email"},
				},
				KillSwitch: ImmediateAlertRuleConfig{
					Channels: []string{"telegram"},
				},
				DBConnection: ImmediateAlertRuleConfig{
					Channels: []string{"email", "pagerduty"},
				},
			},
		},
	}
}

func TestValidateRequiresDataProvider(t *testing.T) {
	cfg := validConfig()
	cfg.DataProviders.Polygon.APIKey = ""
	cfg.DataProviders.AlphaVantage.APIKey = ""
	cfg.DataProviders.Finnhub.APIKey = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "at least one data provider must be configured") {
		t.Fatalf("Validate() error = %q, want data provider message", err)
	}
}

func TestValidateAllowsPolygonOnly(t *testing.T) {
	cfg := validConfig()
	cfg.DataProviders.AlphaVantage.APIKey = ""
	cfg.DataProviders.Finnhub.APIKey = ""

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateWhitespaceOnlyDeepThinkModel(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DeepThinkModel = "   "

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_DEEP_THINK_MODEL must not be whitespace-only") {
		t.Fatalf("Validate() error = %q, want model message", err)
	}
}

func TestValidateEmptyDeepThinkModelAllowed(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DeepThinkModel = ""

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil (empty is allowed)", err)
	}
}

func TestValidateRequiresTelegramChatIDWhenTelegramConfigured(t *testing.T) {
	cfg := validConfig()
	cfg.Notifications.Telegram.BotToken = "telegram-token"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "NOTIFY_TELEGRAM_CHAT_ID is required") {
		t.Fatalf("Validate() error = %q, want Telegram chat id message", err)
	}
}

func TestValidateRejectsUnsupportedNotificationChannel(t *testing.T) {
	cfg := validConfig()
	cfg.Notifications.Alerts.PipelineFailure.Channels = []string{"sms"}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "ALERT_PIPELINE_FAILURE_CHANNELS contains unsupported channel") {
		t.Fatalf("Validate() error = %q, want unsupported channel message", err)
	}
}

func TestValidateAllowsN8NAlertChannelWithoutConfiguredWebhook(t *testing.T) {
	cfg := validConfig()
	cfg.Notifications.Alerts.PipelineFailure.Channels = []string{"n8n"}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

// --- LLM resilience config tests (PR 2) ---

func TestValidateFallbackProviderWithoutKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.FallbackProvider = "anthropic"
	cfg.LLM.Providers.Anthropic.APIKey = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_FALLBACK_PROVIDER is anthropic but ANTHROPIC_API_KEY is not set") {
		t.Fatalf("Validate() error = %q, want fallback key message", err)
	}
}

func TestValidateFallbackProviderOllamaWithBaseURLAndAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.FallbackProvider = "ollama"
	cfg.LLM.Providers.Ollama.BaseURL = "http://localhost:11434/v1"
	cfg.LLM.Providers.Ollama.APIKey = "ollama-key"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestHasLLMProviderReturnsFalseWhenOnlyOllamaBaseURLSet(t *testing.T) {
	if hasLLMProvider(LLMProviderConfigs{Ollama: OllamaConfig{BaseURL: "http://localhost:11434/v1"}}) {
		t.Fatal("hasLLMProvider() = true, want false when Ollama API key is missing")
	}
}

func TestValidateFallbackProviderEmpty(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.FallbackProvider = ""

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil (no fallback)", err)
	}
}

func TestValidateFallbackModelWithoutProvider(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.FallbackModel = "gpt-4.1-mini"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_FALLBACK_MODEL is set but LLM_FALLBACK_PROVIDER is not set") {
		t.Fatalf("Validate() error = %q, want fallback model/provider message", err)
	}
}

func TestValidateFallbackProviderUnknown(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.FallbackProvider = "deepseek"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not a known provider") {
		t.Fatalf("Validate() error = %q, want unknown provider message", err)
	}
}

func TestValidateFallbackProviderWithKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.FallbackProvider = "openrouter"
	cfg.LLM.Providers.OpenRouter.APIKey = "or-key"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateRetryMaxAttemptsZero(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.RetryMaxAttempts = 0

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_RETRY_MAX_ATTEMPTS must be >= 1") {
		t.Fatalf("Validate() error = %q, want retry message", err)
	}
}

func TestValidateCallTimeoutZero(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.CallTimeout = 0

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_CALL_TIMEOUT must be greater than 0") {
		t.Fatalf("Validate() error = %q, want call timeout message", err)
	}
}

func TestValidateThrottleConcurrencyZero(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.ThrottleConcurrency = 0

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_THROTTLE_CONCURRENCY must be >= 1") {
		t.Fatalf("Validate() error = %q, want throttle message", err)
	}
}

func TestValidateBudgetRequestsPerDayNegative(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.BudgetRequestsPerDay = -1

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_BUDGET_REQUESTS_DAY must be >= 0") {
		t.Fatalf("Validate() error = %q, want budget requests message", err)
	}
}

func TestValidateBudgetTokensPerDayNegative(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.BudgetTokensPerDay = -1

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "LLM_BUDGET_TOKENS_DAY must be >= 0") {
		t.Fatalf("Validate() error = %q, want budget tokens message", err)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"APP_ENV",
		"APP_HOST",
		"APP_PORT",
		"DATABASE_URL",
		"DATABASE_POOL_SIZE",
		"DATABASE_SSL_MODE",
		"REDIS_URL",
		"LLM_DEFAULT_PROVIDER",
		"LLM_DEEP_THINK_MODEL",
		"LLM_QUICK_THINK_MODEL",
		"LLM_TIMEOUT",
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"OPENAI_MODEL",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_MODEL",
		"GOOGLE_API_KEY",
		"GOOGLE_MODEL",
		"OPENROUTER_API_KEY",
		"OPENROUTER_BASE_URL",
		"OPENROUTER_MODEL",
		"XAI_API_KEY",
		"XAI_BASE_URL",
		"XAI_MODEL",
		"OLLAMA_BASE_URL",
		"OLLAMA_API_KEY",
		"OLLAMA_MODEL",
		"POLYGON_API_KEY",
		"ALPHA_VANTAGE_API_KEY",
		"ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE",
		"FINNHUB_API_KEY",
		"FINNHUB_RATE_LIMIT_PER_MINUTE",
		"ALPACA_API_KEY",
		"ALPACA_API_SECRET",
		"ALPACA_PAPER_MODE",
		"BINANCE_API_KEY",
		"BINANCE_API_SECRET",
		"BINANCE_PAPER_MODE",
		"RISK_MAX_POSITION_SIZE_PCT",
		"RISK_MAX_DAILY_LOSS_PCT",
		"RISK_MAX_DRAWDOWN_PCT",
		"RISK_MAX_OPEN_POSITIONS",
		"RISK_CIRCUIT_BREAKER_THRESHOLD",
		"RISK_CIRCUIT_BREAKER_COOLDOWN",
		"NOTIFY_TELEGRAM_BOT_TOKEN",
		"NOTIFY_TELEGRAM_CHAT_ID",
		"NOTIFY_SMTP_HOST",
		"NOTIFY_SMTP_PORT",
		"NOTIFY_SMTP_USERNAME",
		"NOTIFY_SMTP_PASSWORD",
		"NOTIFY_EMAIL_FROM",
		"NOTIFY_EMAIL_TO",
		"N8N_WEBHOOK_URL",
		"N8N_WEBHOOK_SECRET",
		"NOTIFY_PAGERDUTY_WEBHOOK_URL",
		"NOTIFY_PAGERDUTY_WEBHOOK_SECRET",
		"ALERT_PIPELINE_FAILURE_THRESHOLD",
		"ALERT_PIPELINE_FAILURE_CHANNELS",
		"ALERT_CIRCUIT_BREAKER_CHANNELS",
		"ALERT_LLM_PROVIDER_DOWN_ERROR_RATE_THRESHOLD",
		"ALERT_LLM_PROVIDER_DOWN_WINDOW",
		"ALERT_LLM_PROVIDER_DOWN_CHANNELS",
		"ALERT_HIGH_LATENCY_THRESHOLD",
		"ALERT_HIGH_LATENCY_CHANNELS",
		"ALERT_KILL_SWITCH_CHANNELS",
		"ALERT_DB_CONNECTION_CHANNELS",
		"ENABLE_SCHEDULER",
		"ENABLE_REDIS_CACHE",
		"ENABLE_AGENT_MEMORY",
		"ENABLE_LIVE_TRADING",
		"LIVE_TRADING_ALLOWED_STRATEGIES",
		"LIVE_TRADING_ALLOWED_BROKERS",
		"LLM_FALLBACK_PROVIDER",
		"LLM_FALLBACK_MODEL",
		"LLM_RETRY_MAX_ATTEMPTS",
		"LLM_CALL_TIMEOUT",
		"LLM_BUDGET_REQUESTS_DAY",
		"LLM_BUDGET_TOKENS_DAY",
		"LLM_THROTTLE_CONCURRENCY",
	} {
		t.Setenv(key, "")
	}
}
