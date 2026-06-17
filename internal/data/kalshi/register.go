package kalshi

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the Kalshi provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.Kalshi = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(cfg.BaseURL, cfg.Logger)
	}
}
