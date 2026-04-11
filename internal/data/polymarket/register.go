package polymarket

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the Polymarket data provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.Polymarket = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(cfg.BaseURL, cfg.Logger)
	}
}
