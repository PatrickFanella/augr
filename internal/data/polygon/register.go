package polygon

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the Polygon provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.Polygon = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(NewClient(cfg.APIKey, cfg.Logger))
	}
}
