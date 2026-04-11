package alphavantage

import (
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

// Register adds the Alpha Vantage provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.AlphaVantage = func(cfg data.ProviderConfig) data.DataProvider {
		if cfg.RateLimitPerMinute > 0 {
			return NewProvider(NewClient(cfg.APIKey, cfg.Logger, data.NewRateLimiter(cfg.RateLimitPerMinute, time.Minute)))
		}
		return NewProvider(NewClient(cfg.APIKey, cfg.Logger))
	}
}
