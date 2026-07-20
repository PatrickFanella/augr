package data

import (
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	provgov "github.com/PatrickFanella/get-rich-quick/internal/providergovernor"
)

// ProviderConfig holds the configuration passed to every provider factory.
// Fields not applicable to a specific provider are ignored.
type ProviderConfig struct {
	APIKey             string
	RateLimitPerMinute int    // 0 = unlimited
	BaseURL            string // provider-specific base or CLOB URL
	Logger             *slog.Logger
	LLMProvider        llm.Provider // optional; used by providers that need LLM triage (e.g. Reddit)
	LLMModel           string       // model name for LLM triage calls
	Governor           *provgov.ProviderGovernor
	Metrics            *metrics.Metrics
	ClientType         string
}

// ProviderFactory is the uniform constructor signature for all data providers.
type ProviderFactory func(cfg ProviderConfig) DataProvider
