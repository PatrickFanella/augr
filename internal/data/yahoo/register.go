package yahoo

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the Yahoo provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.Yahoo = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(cfg.Logger)
	}
}
