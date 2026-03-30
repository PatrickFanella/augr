package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config contains application configuration loaded from the environment.
type Config struct {
	Environment   string
	Server        ServerConfig
	Database      DatabaseConfig
	Redis         RedisConfig
	LLM           LLMConfig
	DataProviders DataProviderConfigs
	Brokers       BrokerConfigs
	Risk          RiskConfig
	Notifications NotificationConfig
	Features      FeatureFlags
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host      string
	Port      int
	JWTSecret string
}

// DatabaseConfig contains database connection settings.
type DatabaseConfig struct {
	URL      string
	PoolSize int
	SSLMode  string
}

// RedisConfig contains Redis settings.
type RedisConfig struct {
	URL string
}

// LLMConfig contains model selection and provider settings.
type LLMConfig struct {
	DefaultProvider string
	DeepThinkModel  string
	QuickThinkModel string
	Timeout         time.Duration
	Providers       LLMProviderConfigs
}

// LLMProviderConfigs contains provider-specific settings.
type LLMProviderConfigs struct {
	OpenAI     LLMProviderConfig
	Anthropic  LLMProviderConfig
	Google     LLMProviderConfig
	OpenRouter LLMProviderConfig
	XAI        LLMProviderConfig
	Ollama     OllamaConfig
}

// LLMProviderConfig contains settings for API-backed LLM providers.
type LLMProviderConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// OllamaConfig contains local model settings.
type OllamaConfig struct {
	BaseURL string
	Model   string
}

// DataProviderConfigs contains external data provider settings.
type DataProviderConfigs struct {
	Polygon      DataProviderConfig
	AlphaVantage DataProviderConfig
	Finnhub      DataProviderConfig
}

// DataProviderConfig contains settings for a market data provider.
type DataProviderConfig struct {
	APIKey             string
	RateLimitPerMinute int
}

// BrokerConfigs contains broker integration settings.
type BrokerConfigs struct {
	Alpaca  BrokerConfig
	Binance BrokerConfig
}

// BrokerConfig contains broker credentials and execution mode.
type BrokerConfig struct {
	APIKey    string
	APISecret string
	PaperMode bool
}

// RiskConfig contains application-wide risk management defaults.
type RiskConfig struct {
	MaxPositionSizePct      float64
	MaxDailyLossPct         float64
	MaxDrawdownPct          float64
	MaxOpenPositions        int
	CircuitBreakerThreshold float64
	CircuitBreakerCooldown  time.Duration
}

// NotificationConfig contains outbound notifier credentials and alert rule thresholds.
type NotificationConfig struct {
	Telegram  TelegramNotificationConfig
	Email     EmailNotificationConfig
	Webhook   WebhookNotificationConfig
	PagerDuty WebhookNotificationConfig
	Alerts    AlertRulesConfig
}

// TelegramNotificationConfig contains Telegram bot delivery settings.
type TelegramNotificationConfig struct {
	BotToken string
	ChatID   string
}

// EmailNotificationConfig contains SMTP delivery settings.
type EmailNotificationConfig struct {
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	From     string
	To       []string
}

// WebhookNotificationConfig contains generic webhook delivery settings.
type WebhookNotificationConfig struct {
	URL    string
	Secret string
}

// AlertRulesConfig contains alert thresholds and channel routing.
type AlertRulesConfig struct {
	PipelineFailure PipelineFailureAlertRuleConfig
	CircuitBreaker  ImmediateAlertRuleConfig
	LLMProviderDown LLMProviderDownAlertRuleConfig
	HighLatency     HighLatencyAlertRuleConfig
	KillSwitch      ImmediateAlertRuleConfig
	DBConnection    ImmediateAlertRuleConfig
}

// PipelineFailureAlertRuleConfig contains configuration for consecutive pipeline failures.
type PipelineFailureAlertRuleConfig struct {
	Threshold int
	Channels  []string
}

// ImmediateAlertRuleConfig contains routing for immediate alerts.
type ImmediateAlertRuleConfig struct {
	Channels []string
}

// LLMProviderDownAlertRuleConfig contains rolling-window LLM provider health thresholds.
type LLMProviderDownAlertRuleConfig struct {
	ErrorRateThreshold float64
	Window             time.Duration
	Channels           []string
}

// HighLatencyAlertRuleConfig contains pipeline latency thresholds.
type HighLatencyAlertRuleConfig struct {
	Threshold time.Duration
	Channels  []string
}

// FeatureFlags contains boolean feature toggles.
type FeatureFlags struct {
	EnableScheduler   bool
	EnableRedisCache  bool
	EnableAgentMemory bool
	EnableLiveTrading bool
}

// Load loads configuration from the environment and validates it.
func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	cfg, err := loadFromEnvironment()
	if err != nil {
		return Config{}, err
	}

	if err := Validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func loadDotEnv() error {
	environment := firstNonEmpty(os.Getenv("APP_ENV"), "development")
	if !strings.EqualFold(environment, "development") {
		return nil
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load .env file: %w", err)
	}

	return nil
}

func loadFromEnvironment() (Config, error) {
	serverPort, err := getEnvInt("APP_PORT", 8080)
	if err != nil {
		return Config{}, err
	}

	databasePoolSize, err := getEnvInt("DATABASE_POOL_SIZE", 10)
	if err != nil {
		return Config{}, err
	}

	llmTimeout, err := getEnvDuration("LLM_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	alphaVantageRateLimit, err := getEnvInt("ALPHA_VANTAGE_RATE_LIMIT_PER_MINUTE", 5)
	if err != nil {
		return Config{}, err
	}

	finnhubRateLimit, err := getEnvInt("FINNHUB_RATE_LIMIT_PER_MINUTE", 60)
	if err != nil {
		return Config{}, err
	}

	alpacaPaperMode, err := getEnvBool("ALPACA_PAPER_MODE", true)
	if err != nil {
		return Config{}, err
	}

	binancePaperMode, err := getEnvBool("BINANCE_PAPER_MODE", true)
	if err != nil {
		return Config{}, err
	}

	maxPositionSizePct, err := getEnvFloat64("RISK_MAX_POSITION_SIZE_PCT", 0.10)
	if err != nil {
		return Config{}, err
	}

	maxDailyLossPct, err := getEnvFloat64("RISK_MAX_DAILY_LOSS_PCT", 0.02)
	if err != nil {
		return Config{}, err
	}

	maxDrawdownPct, err := getEnvFloat64("RISK_MAX_DRAWDOWN_PCT", 0.10)
	if err != nil {
		return Config{}, err
	}

	maxOpenPositions, err := getEnvInt("RISK_MAX_OPEN_POSITIONS", 10)
	if err != nil {
		return Config{}, err
	}

	circuitBreakerThreshold, err := getEnvFloat64("RISK_CIRCUIT_BREAKER_THRESHOLD", 0.05)
	if err != nil {
		return Config{}, err
	}

	circuitBreakerCooldown, err := getEnvDuration("RISK_CIRCUIT_BREAKER_COOLDOWN", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}

	smtpPort, err := getEnvInt("NOTIFY_SMTP_PORT", 587)
	if err != nil {
		return Config{}, err
	}

	pipelineFailureThreshold, err := getEnvInt("ALERT_PIPELINE_FAILURE_THRESHOLD", 3)
	if err != nil {
		return Config{}, err
	}

	llmProviderDownErrorRateThreshold, err := getEnvFloat64("ALERT_LLM_PROVIDER_DOWN_ERROR_RATE_THRESHOLD", 0.5)
	if err != nil {
		return Config{}, err
	}

	llmProviderDownWindow, err := getEnvDuration("ALERT_LLM_PROVIDER_DOWN_WINDOW", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}

	highLatencyThreshold, err := getEnvDuration("ALERT_HIGH_LATENCY_THRESHOLD", 120*time.Second)
	if err != nil {
		return Config{}, err
	}

	enableScheduler, err := getEnvBool("ENABLE_SCHEDULER", false)
	if err != nil {
		return Config{}, err
	}

	enableRedisCache, err := getEnvBool("ENABLE_REDIS_CACHE", true)
	if err != nil {
		return Config{}, err
	}

	enableAgentMemory, err := getEnvBool("ENABLE_AGENT_MEMORY", true)
	if err != nil {
		return Config{}, err
	}

	enableLiveTrading, err := getEnvBool("ENABLE_LIVE_TRADING", false)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment: getEnvString("APP_ENV", "development"),
		Server: ServerConfig{
			Host:      getEnvString("APP_HOST", "0.0.0.0"),
			Port:      serverPort,
			JWTSecret: os.Getenv("JWT_SECRET"),
		},
		Database: DatabaseConfig{
			URL:      os.Getenv("DATABASE_URL"),
			PoolSize: databasePoolSize,
			SSLMode:  getEnvString("DATABASE_SSL_MODE", "disable"),
		},
		Redis: RedisConfig{
			URL: os.Getenv("REDIS_URL"),
		},
		LLM: LLMConfig{
			DefaultProvider: getEnvString("LLM_DEFAULT_PROVIDER", "openai"),
			DeepThinkModel:  getEnvString("LLM_DEEP_THINK_MODEL", "gpt-5.2"),
			QuickThinkModel: getEnvString("LLM_QUICK_THINK_MODEL", "gpt-5-mini"),
			Timeout:         llmTimeout,
			Providers: LLMProviderConfigs{
				OpenAI: LLMProviderConfig{
					APIKey:  os.Getenv("OPENAI_API_KEY"),
					BaseURL: os.Getenv("OPENAI_BASE_URL"),
					Model:   getEnvString("OPENAI_MODEL", "gpt-5-mini"),
				},
				Anthropic: LLMProviderConfig{
					APIKey: os.Getenv("ANTHROPIC_API_KEY"),
					Model:  getEnvString("ANTHROPIC_MODEL", "claude-3-7-sonnet-latest"),
				},
				Google: LLMProviderConfig{
					APIKey: os.Getenv("GOOGLE_API_KEY"),
					Model:  getEnvString("GOOGLE_MODEL", "gemini-2.5-flash"),
				},
				OpenRouter: LLMProviderConfig{
					APIKey:  os.Getenv("OPENROUTER_API_KEY"),
					BaseURL: os.Getenv("OPENROUTER_BASE_URL"),
					Model:   getEnvString("OPENROUTER_MODEL", "openai/gpt-4.1-mini"),
				},
				XAI: LLMProviderConfig{
					APIKey:  os.Getenv("XAI_API_KEY"),
					BaseURL: os.Getenv("XAI_BASE_URL"),
					Model:   getEnvString("XAI_MODEL", "grok-3-mini"),
				},
				Ollama: OllamaConfig{
					BaseURL: getEnvString("OLLAMA_BASE_URL", "http://localhost:11434"),
					Model:   getEnvString("OLLAMA_MODEL", "llama3.2"),
				},
			},
		},
		DataProviders: DataProviderConfigs{
			Polygon: DataProviderConfig{
				APIKey: os.Getenv("POLYGON_API_KEY"),
			},
			AlphaVantage: DataProviderConfig{
				APIKey:             os.Getenv("ALPHA_VANTAGE_API_KEY"),
				RateLimitPerMinute: alphaVantageRateLimit,
			},
			Finnhub: DataProviderConfig{
				APIKey:             os.Getenv("FINNHUB_API_KEY"),
				RateLimitPerMinute: finnhubRateLimit,
			},
		},
		Brokers: BrokerConfigs{
			Alpaca: BrokerConfig{
				APIKey:    os.Getenv("ALPACA_API_KEY"),
				APISecret: os.Getenv("ALPACA_API_SECRET"),
				PaperMode: alpacaPaperMode,
			},
			Binance: BrokerConfig{
				APIKey:    os.Getenv("BINANCE_API_KEY"),
				APISecret: os.Getenv("BINANCE_API_SECRET"),
				PaperMode: binancePaperMode,
			},
		},
		Risk: RiskConfig{
			MaxPositionSizePct:      maxPositionSizePct,
			MaxDailyLossPct:         maxDailyLossPct,
			MaxDrawdownPct:          maxDrawdownPct,
			MaxOpenPositions:        maxOpenPositions,
			CircuitBreakerThreshold: circuitBreakerThreshold,
			CircuitBreakerCooldown:  circuitBreakerCooldown,
		},
		Notifications: NotificationConfig{
			Telegram: TelegramNotificationConfig{
				BotToken: os.Getenv("NOTIFY_TELEGRAM_BOT_TOKEN"),
				ChatID:   os.Getenv("NOTIFY_TELEGRAM_CHAT_ID"),
			},
			Email: EmailNotificationConfig{
				SMTPHost: os.Getenv("NOTIFY_SMTP_HOST"),
				SMTPPort: smtpPort,
				Username: os.Getenv("NOTIFY_SMTP_USERNAME"),
				Password: os.Getenv("NOTIFY_SMTP_PASSWORD"),
				From:     os.Getenv("NOTIFY_EMAIL_FROM"),
				To:       getEnvCSV("NOTIFY_EMAIL_TO"),
			},
			Webhook: WebhookNotificationConfig{
				URL:    os.Getenv("NOTIFY_WEBHOOK_URL"),
				Secret: os.Getenv("NOTIFY_WEBHOOK_SECRET"),
			},
			PagerDuty: WebhookNotificationConfig{
				URL:    os.Getenv("NOTIFY_PAGERDUTY_WEBHOOK_URL"),
				Secret: os.Getenv("NOTIFY_PAGERDUTY_WEBHOOK_SECRET"),
			},
			Alerts: AlertRulesConfig{
				PipelineFailure: PipelineFailureAlertRuleConfig{
					Threshold: pipelineFailureThreshold,
					Channels:  getEnvCSVWithDefault("ALERT_PIPELINE_FAILURE_CHANNELS", []string{"telegram", "email"}),
				},
				CircuitBreaker: ImmediateAlertRuleConfig{
					Channels: getEnvCSVWithDefault("ALERT_CIRCUIT_BREAKER_CHANNELS", []string{"telegram"}),
				},
				LLMProviderDown: LLMProviderDownAlertRuleConfig{
					ErrorRateThreshold: llmProviderDownErrorRateThreshold,
					Window:             llmProviderDownWindow,
					Channels:           getEnvCSVWithDefault("ALERT_LLM_PROVIDER_DOWN_CHANNELS", []string{"telegram"}),
				},
				HighLatency: HighLatencyAlertRuleConfig{
					Threshold: highLatencyThreshold,
					Channels:  getEnvCSVWithDefault("ALERT_HIGH_LATENCY_CHANNELS", []string{"email"}),
				},
				KillSwitch: ImmediateAlertRuleConfig{
					Channels: getEnvCSVWithDefault("ALERT_KILL_SWITCH_CHANNELS", []string{"telegram"}),
				},
				DBConnection: ImmediateAlertRuleConfig{
					Channels: getEnvCSVWithDefault("ALERT_DB_CONNECTION_CHANNELS", []string{"email", "pagerduty"}),
				},
			},
		},
		Features: FeatureFlags{
			EnableScheduler:   enableScheduler,
			EnableRedisCache:  enableRedisCache,
			EnableAgentMemory: enableAgentMemory,
			EnableLiveTrading: enableLiveTrading,
		},
	}

	return cfg, nil
}

func getEnvString(key, defaultValue string) string {
	return firstNonEmpty(os.Getenv(key), defaultValue)
}

func getEnvInt(key string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}

	return parsed, nil
}

func getEnvFloat64(key string, defaultValue float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}

	return parsed, nil
}

func getEnvBool(key string, defaultValue bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}

	return parsed, nil
}

func getEnvDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}

	return parsed, nil
}

func getEnvCSV(key string) []string {
	return getEnvCSVWithDefault(key, nil)
}

func getEnvCSVWithDefault(key string, defaultValue []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), defaultValue...)
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}
