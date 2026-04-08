package newsapi

import (
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

// Register adds the NewsAPI provider factory to the given registry.
// NewsAPI is a news-only provider: GetOHLCV and GetFundamentals return
// ErrNotImplemented; only GetNews is supported.
func Register(reg *data.ProviderRegistry) {
	reg.NewsAPI = func(apiKey string, logger *slog.Logger) data.DataProvider {
		return NewProvider(NewClient(apiKey, logger))
	}
}
