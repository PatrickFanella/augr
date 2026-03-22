package analysts

import (
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// BaseAnalyst holds the common dependencies shared by all analyst nodes.
type BaseAnalyst struct {
	provider llm.Provider
	model    string
	logger   *slog.Logger
}

// NewBaseAnalyst creates a BaseAnalyst with the given LLM provider, model name,
// and logger. A nil logger is replaced with the default logger.
func NewBaseAnalyst(provider llm.Provider, model string, logger *slog.Logger) BaseAnalyst {
	if logger == nil {
		logger = slog.Default()
	}
	return BaseAnalyst{
		provider: provider,
		model:    model,
		logger:   logger,
	}
}
