package fmp

import (
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

// Register adds the FMP provider factory to the given registry.
func Register(reg *data.ProviderRegistry) {
	reg.FMP = func(apiKey string, rateLimitPerMinute int, logger *slog.Logger) data.DataProvider {
		var limiters []*data.RateLimiter
		if rateLimitPerMinute > 0 {
			limiters = append(limiters, data.NewRateLimiter(rateLimitPerMinute, time.Minute))
		}
		if gl := data.GetGlobalLimiter(); gl != nil {
			limiters = append(limiters, gl)
		}
		return NewProvider(NewClient(apiKey, logger, limiters...))
	}
}
