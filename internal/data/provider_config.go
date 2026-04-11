package data

import "log/slog"

// ProviderConfig holds the configuration passed to every provider factory.
// Fields not applicable to a specific provider are ignored.
type ProviderConfig struct {
	APIKey             string
	RateLimitPerMinute int    // 0 = unlimited
	BaseURL            string // provider-specific base or CLOB URL
	Logger             *slog.Logger
}

// ProviderFactory is the uniform constructor signature for all data providers.
type ProviderFactory func(cfg ProviderConfig) DataProvider
