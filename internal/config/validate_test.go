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
	t.Setenv("OPENROUTER_BASE_URL", "https://openrouter.example.com/api/v1")
	t.Setenv("XAI_BASE_URL", "https://xai.example.com/v1")
	t.Setenv("APP_HOST", "127.0.0.1")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("JWT_SECRET", "super-secret")
	t.Setenv("DATABASE_POOL_SIZE", "25")
	t.Setenv("DATABASE_SSL_MODE", "require")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("LLM_TIMEOUT", "45s")
	t.Setenv("POLYGON_API_KEY", "polygon-key")
	t.Setenv("ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE", "7")
	t.Setenv("FINNHUB_RATE_LIMIT_PER_MINUTE", "20")
	t.Setenv("ALPACA_PAPER_MODE", "false")
	t.Setenv("ENABLE_SCHEDULER", "true")
	t.Setenv("ENABLE_REDIS_CACHE", "false")

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

	if !cfg.Features.EnableScheduler {
		t.Fatal("cfg.Features.EnableScheduler = false, want true")
	}

	if cfg.Features.EnableRedisCache {
		t.Fatal("cfg.Features.EnableRedisCache = true, want false")
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

func TestValidateAllowsOllamaWithoutAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.Providers.OpenAI.APIKey = ""
	cfg.LLM.Providers.Anthropic.APIKey = ""
	cfg.LLM.Providers.Google.APIKey = ""
	cfg.LLM.Providers.OpenRouter.APIKey = ""
	cfg.LLM.Providers.XAI.APIKey = ""
	cfg.LLM.Providers.Ollama.BaseURL = "http://localhost:11434/v1"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil (Ollama requires no API key)", err)
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

func TestValidateDefaultProviderOllamaNoKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.DefaultProvider = "ollama"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v, want nil (Ollama needs no key)", err)
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
		"ENABLE_SCHEDULER",
		"ENABLE_REDIS_CACHE",
		"ENABLE_AGENT_MEMORY",
		"ENABLE_LIVE_TRADING",
	} {
		t.Setenv(key, "")
	}
}
