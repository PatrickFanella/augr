package binance

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the Binance provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.Binance = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(cfg.Logger)
	}
}
