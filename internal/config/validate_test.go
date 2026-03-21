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
	t.Setenv("DATABASE_POOL_SIZE", "25")
	t.Setenv("DATABASE_SSL_MODE", "require")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("LLM_TIMEOUT", "45s")
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

	if !strings.Contains(err.Error(), "at least one LLM API key must be configured") {
		t.Fatalf("Validate() error = %q, want LLM API key message", err)
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
