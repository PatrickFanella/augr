package analysts

import (
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// BaseAnalyst holds the common dependencies shared by all analyst nodes.
type BaseAnalyst struct {
	provider     llm.Provider
	providerName string
	model        string
	logger       *slog.Logger
}

// NewBaseAnalyst creates a BaseAnalyst with the given LLM provider,
// provider name (e.g. "openai"), model name, and logger. A nil logger is
// replaced with the default logger.
func NewBaseAnalyst(provider llm.Provider, providerName, model string, logger *slog.Logger) BaseAnalyst {
	if logger == nil {
		logger = slog.Default()
	}
	return BaseAnalyst{
		provider:     provider,
		providerName: providerName,
		model:        model,
		logger:       logger,
	}
}
