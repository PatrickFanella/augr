package polygon

import "github.com/PatrickFanella/get-rich-quick/internal/data"

// Register adds the Polygon provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	RegisterWithLimiter(reg, data.GetGlobalLimiter())
}

// RegisterWithLimiter adds Polygon provider factories sharing the supplied
// limiter across every client created by the registry.
func RegisterWithLimiter(reg *data.ProviderRegistry, limiter *data.RateLimiter) {
	reg.Polygon = func(cfg data.ProviderConfig) data.DataProvider {
		return NewProvider(NewClient(cfg.APIKey, cfg.Logger, limiter))
	}
}
