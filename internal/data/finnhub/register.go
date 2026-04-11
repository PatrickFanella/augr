package finnhub

import (
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

// Register adds the Finnhub provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.Finnhub = func(cfg data.ProviderConfig) data.DataProvider {
		var limiters []*data.RateLimiter
		if cfg.RateLimitPerMinute > 0 {
			limiters = append(limiters, data.NewRateLimiter(cfg.RateLimitPerMinute, time.Minute))
		}
		if gl := data.GetGlobalLimiter(); gl != nil {
			limiters = append(limiters, gl)
		}
		return NewProvider(NewClient(cfg.APIKey, cfg.Logger, limiters...))
	}
}
