package newsapi

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the NewsAPI provider factory to the given registry.
// NewsAPI is a news-only provider: GetOHLCV and GetFundamentals return
// ErrNotImplemented; only GetNews is supported.
func Register(reg *data.ProviderRegistry) {
	reg.NewsAPI = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(NewClient(cfg.APIKey, cfg.Logger))
	}
}
